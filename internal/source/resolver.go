package source

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stubbedev/mongodb-mcp/internal/config"
)

// Resolver picks the registry a tool call runs against. A client that exposes a
// workspace root containing a RootConfigName file gets a registry built from
// that file; everything else falls back to the server's global registry (base),
// which may be nil in roots-only mode. This lets one server serve several
// clients, each with its own sources, over both stdio and HTTP.
type Resolver struct {
	base *Registry // global fallback; nil when started with no config

	srv *mcp.Server

	mu       sync.Mutex
	sessions map[string]*sessionState // SDK session id → roots cache
	regs     map[string]*cachedReg    // per-root config path → built registry
}

// cachedReg is a per-root registry cached by config file path, keyed on the
// file's mtime so an edited config reloads on the next call.
type cachedReg struct {
	mtime time.Time
	reg   *Registry
}

// NewResolver builds a resolver over the given global fallback registry (nil for
// roots-only mode).
func NewResolver(base *Registry) *Resolver {
	return &Resolver{
		base:     base,
		sessions: map[string]*sessionState{},
		regs:     map[string]*cachedReg{},
	}
}

// AttachServer wires the resolver to its server: it must be the same *mcp.Server
// whose ServerOptions.RootsListChangedHandler is set to r.OnRootsChanged. It
// starts the background sweep that drops state for ended sessions.
func (r *Resolver) AttachServer(srv *mcp.Server) {
	r.srv = srv
	go r.sweepSessions()
}

// Registry resolves the registry for a tool call. Header-injected roots are
// checked first and are request-scoped — never cached on shared session state,
// so a proxy multiplexing several clients over one session may vary them per
// request without cross-contamination. Next comes the client's roots/list
// (cached per session), then the global fallback. A nil request or a client
// without roots resolves straight to the fallback.
func (r *Resolver) Registry(ctx context.Context, req *mcp.CallToolRequest) (*Registry, error) {
	if path, mtime, ok := firstRootConfig(headerRootsOf(req)); ok {
		return r.rootRegistry(path, mtime)
	}
	if path, mtime, ok := firstRootConfig(r.sessionRoots(ctx, req)); ok {
		return r.rootRegistry(path, mtime)
	}
	if r.base != nil {
		return r.base, nil
	}
	return nil, fmt.Errorf("no config resolved: add %s to your workspace root, or start the server with --config", config.RootConfigName)
}

// sessionRoots returns the calling client's workspace roots from roots/list,
// cached per session. Clients that did not advertise the roots capability
// (including every stateless request, which re-initializes with default state)
// get no cache entry at all — this keeps the session map from growing under
// stateless HTTP load.
func (r *Resolver) sessionRoots(ctx context.Context, req *mcp.CallToolRequest) []string {
	if req == nil || req.Session == nil || !rootsCapable(req.Session) {
		return nil
	}
	return r.sessionFor(req.Session).rootPaths(ctx)
}

// sessionFor returns the roots/list cache for the calling client, creating it on
// first use. Only reached for roots-capable sessions (see sessionRoots).
func (r *Resolver) sessionFor(ss *mcp.ServerSession) *sessionState {
	id := ss.ID()
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.sessions[id]
	if st == nil {
		st = &sessionState{ss: ss}
		r.sessions[id] = st
	}
	return st
}

// rootRegistry returns the registry built from the config at path, cached by
// (path, mtime). An edited config (new mtime) rebuilds and closes the old one.
//
// ponytail: distinct workspaces each retain a registry for the life of the
// process (bounded by how many configs one server sees — a handful locally). Add
// refcount/LRU eviction only if a long-lived multi-tenant server proves it needs it.
func (r *Resolver) rootRegistry(path string, mtime time.Time) (*Registry, error) {
	r.mu.Lock()
	if c := r.regs[path]; c != nil && c.mtime.Equal(mtime) {
		r.mu.Unlock()
		return c.reg, nil
	}
	r.mu.Unlock()

	cfg, err := config.Load(path)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", path, err)
	}
	reg := NewRegistry(cfg.Sources)

	r.mu.Lock()
	if c := r.regs[path]; c != nil && c.mtime.Equal(mtime) {
		r.mu.Unlock()
		closeRegistryAsync(reg) // lost a race; the winner is cached, drop ours
		return c.reg, nil
	}
	old := r.regs[path]
	r.regs[path] = &cachedReg{mtime: mtime, reg: reg}
	r.mu.Unlock()
	if old != nil {
		closeRegistryAsync(old.reg) // config changed on disk — retire the stale connections
	}
	return reg, nil
}

// closeRegistryAsync tears a registry's connections down off the request path;
// a lazily-connected registry that was never used closes immediately.
func closeRegistryAsync(reg *Registry) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = reg.Close(ctx)
	}()
}

// OnRootsChanged invalidates the cached roots for the signalling client. Wire it
// as ServerOptions.RootsListChangedHandler.
func (r *Resolver) OnRootsChanged(_ context.Context, req *mcp.RootsListChangedRequest) {
	if req.Session == nil {
		return
	}
	r.mu.Lock()
	st := r.sessions[req.Session.ID()]
	r.mu.Unlock()
	if st != nil {
		st.invalidate()
	}
}

// sweepSessions drops roots caches whose SDK session has ended so a long-lived
// server does not leak sessionStates.
func (r *Resolver) sweepSessions() {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for range t.C {
		live := map[string]struct{}{}
		for ss := range r.srv.Sessions() {
			live[ss.ID()] = struct{}{}
		}
		r.mu.Lock()
		for id := range r.sessions {
			if _, ok := live[id]; !ok {
				delete(r.sessions, id)
			}
		}
		r.mu.Unlock()
	}
}
