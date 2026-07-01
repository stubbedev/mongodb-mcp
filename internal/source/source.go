// Package source manages the configured MongoDB connections ("sources").
//
// Each source is connected lazily on first use and cached. Sources may be
// reached directly or tunnelled through SSH, and may be marked read-only, in
// which case the registry rejects write operations before they reach MongoDB.
package source

import (
	"context"
	"fmt"
	"sync"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/stubbedev/mongodb-mcp/internal/config"
)

// ErrReadOnly is returned when a write/admin operation targets a read-only
// source.
type ErrReadOnly struct{ Source string }

func (e ErrReadOnly) Error() string {
	return fmt.Sprintf("source %q is read-only: write and admin operations are not permitted", e.Source)
}

// ErrUnknownSource is returned when a tool references a source name that is not
// in the configuration.
type ErrUnknownSource struct{ Source string }

func (e ErrUnknownSource) Error() string {
	return fmt.Sprintf("unknown source %q", e.Source)
}

// Source is a live, connected MongoDB source.
type Source struct {
	Name            string
	ReadOnly        bool
	DefaultDatabase string

	client *mongo.Client
	dialer *sshDialer // non-nil when tunnelled
}

// Client returns the underlying MongoDB client.
func (s *Source) Client() *mongo.Client { return s.client }

// Registry holds the configuration and lazily-connected sources.
type Registry struct {
	cfg map[string]config.SourceConfig

	mu      sync.Mutex
	sources map[string]*Source
}

// NewRegistry builds a registry from the configured sources.
func NewRegistry(cfg map[string]config.SourceConfig) *Registry {
	return &Registry{
		cfg:     cfg,
		sources: make(map[string]*Source),
	}
}

// Names returns the configured source names.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.cfg))
	for name := range r.cfg {
		names = append(names, name)
	}
	return names
}

// Config returns the static configuration for a source, if present.
func (r *Registry) Config(name string) (config.SourceConfig, bool) {
	c, ok := r.cfg[name]
	return c, ok
}

// Get returns a connected source by name, connecting on first use.
func (r *Registry) Get(ctx context.Context, name string) (*Source, error) {
	scfg, ok := r.cfg[name]
	if !ok {
		return nil, ErrUnknownSource{Source: name}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if s, ok := r.sources[name]; ok {
		return s, nil
	}

	s, err := connect(ctx, name, scfg)
	if err != nil {
		return nil, err
	}
	r.sources[name] = s
	return s, nil
}

// RequireWritable returns a connected source and errors if it is read-only.
func (r *Registry) RequireWritable(ctx context.Context, name string) (*Source, error) {
	s, err := r.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if s.ReadOnly {
		return nil, ErrReadOnly{Source: name}
	}
	return s, nil
}

// Database resolves the database to use: the explicit name, or the source
// default. Errors if neither is available.
func (s *Source) Database(name string) (*mongo.Database, error) {
	if name == "" {
		name = s.DefaultDatabase
	}
	if name == "" {
		return nil, fmt.Errorf("no database specified and source %q has no default_database", s.Name)
	}
	return s.client.Database(name), nil
}

func connect(ctx context.Context, name string, scfg config.SourceConfig) (*Source, error) {
	opts := options.Client().ApplyURI(scfg.URI)

	// Client-side operation timeout (CSOT): bounds every op (find, aggregate,
	// write, ...) that lacks its own deadline. "0s" in config disables it.
	if d := scfg.OperationTimeoutOrDefault(); d > 0 {
		opts.SetTimeout(d)
	}

	var dialer *sshDialer
	if scfg.SSH != nil {
		d, err := newSSHDialer(scfg.SSH)
		if err != nil {
			return nil, fmt.Errorf("source %q: %w", name, err)
		}
		opts.SetDialer(d)
		dialer = d
	}

	client, err := mongo.Connect(opts)
	if err != nil {
		if dialer != nil {
			_ = dialer.Close()
		}
		return nil, fmt.Errorf("source %q: connect: %w", name, err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, scfg.ConnectTimeoutOrDefault())
	defer cancel()
	if err := client.Ping(pingCtx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		if dialer != nil {
			_ = dialer.Close()
		}
		return nil, fmt.Errorf("source %q: ping: %w", name, err)
	}

	return &Source{
		Name:            name,
		ReadOnly:        scfg.ReadOnly,
		DefaultDatabase: scfg.DefaultDatabase,
		client:          client,
		dialer:          dialer,
	}, nil
}

// Close disconnects all connected sources and tears down SSH tunnels.
func (r *Registry) Close(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var firstErr error
	for name, s := range r.sources {
		if err := s.client.Disconnect(ctx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("disconnect %q: %w", name, err)
		}
		if s.dialer != nil {
			_ = s.dialer.Close()
		}
		delete(r.sources, name)
	}
	return firstErr
}
