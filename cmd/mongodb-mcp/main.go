// Command mongodb-mcp is an MCP server exposing MongoDB over stdio or
// streamable HTTP. It is designed to run on the same machine as its consumer
// and to sit cleanly behind an MCP/reverse proxy.
package main

//go:generate go run ../gen-schema -out ../../config.schema.json

import (
	"context"
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
		configPath = flag.String("config", "", "path to config JSON (default: XDG search for "+config.DefaultConfigName+")")
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

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if *httpAddr != "" {
		cfg.HTTP.Addr = *httpAddr
	}
	if *httpPath != "" {
		cfg.HTTP.Path = *httpPath
	}

	// Logs go to stderr so they never corrupt the stdio JSON-RPC stream.
	log.SetOutput(os.Stderr)

	reg := source.NewRegistry(cfg.Sources)
	srv := server.New(cfg, reg)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = reg.Close(shutdownCtx)
	}()

	switch *transport {
	case "stdio":
		log.Printf("serving MCP over stdio (sources: %v)", reg.Names())
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
