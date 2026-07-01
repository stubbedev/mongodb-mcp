// Command mongodb-mcp is an MCP server exposing MongoDB over stdio or
// streamable HTTP. It is designed to run on the same machine as its consumer
// and to sit cleanly behind an MCP/reverse proxy.
package main

//go:generate go run ../gen-schema -out ../../config.schema.json

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stubbedev/mongodb-mcp/internal/config"
	"github.com/stubbedev/mongodb-mcp/internal/server"
	"github.com/stubbedev/mongodb-mcp/internal/source"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("mongodb-mcp: %v", err)
	}
}

func run() error {
	var (
		configPath = flag.String("config", "", "path to global config JSON (default: XDG search for "+config.DefaultConfigName+"; optional when clients supply per-workspace "+config.RootConfigName+" via MCP roots)")
		transport  = flag.String("transport", "stdio", "transport: stdio or http")
		httpAddr   = flag.String("http-addr", "", "HTTP listen address (overrides config http.addr)")
		httpPath   = flag.String("http-path", "", "HTTP mount path (overrides config http.path)")
		showVer    = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *showVer {
		fmt.Println(version)
		return nil
	}

	// Logs go to stderr so they never corrupt the stdio JSON-RPC stream.
	log.SetOutput(os.Stderr)

	// The global config is optional: without one the server runs in roots-only
	// mode, serving each client from its workspace's RootConfigName file. An
	// explicit --config that fails to load is still fatal.
	cfg, err := config.Load(*configPath)
	if err != nil {
		if *configPath != "" || !errors.Is(err, config.ErrNotFound) {
			return err
		}
		cfg = config.Default()
		log.Printf("no global config; serving in roots-only mode (each client supplies %s in its workspace root)", config.RootConfigName)
	}
	if *httpAddr != "" {
		cfg.HTTP.Addr = *httpAddr
	}
	if *httpPath != "" {
		cfg.HTTP.Path = *httpPath
	}

	var reg *source.Registry
	if len(cfg.Sources) > 0 {
		reg = source.NewRegistry(cfg.Sources)
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = reg.Close(shutdownCtx)
		}()
	}
	srv := server.New(cfg, reg)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch *transport {
	case "stdio":
		log.Printf("serving MCP over stdio (sources: %v)", sourceNames(reg))
		if err := server.RunStdio(ctx, srv); err != nil && ctx.Err() == nil {
			return fmt.Errorf("stdio transport: %w", err)
		}
		return nil
	case "http":
		return runHTTP(ctx, srv, cfg)
	default:
		return fmt.Errorf("unknown transport %q (want stdio or http)", *transport)
	}
}

// sourceNames returns the global registry's source names for a startup log
// line; a nil registry (roots-only mode) yields none.
func sourceNames(reg *source.Registry) []string {
	if reg == nil {
		return nil
	}
	return reg.Names()
}

func runHTTP(ctx context.Context, srv *mcp.Server, cfg *config.Config) error {
	httpSrv := &http.Server{
		Addr:              cfg.HTTP.Addr,
		Handler:           server.HTTPHandler(srv, cfg.HTTP),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("serving MCP over HTTP on %s%s (stateless=%t, sources: %v)",
			cfg.HTTP.Addr, cfg.HTTP.Path, cfg.HTTP.Stateless, len(cfg.Sources))
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return fmt.Errorf("http transport: %w", err)
	}
}
