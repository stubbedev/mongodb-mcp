// Package server wires configuration and the source registry into an MCP
// server, and exposes helpers to serve it over stdio or streamable HTTP.
package server

import (
	"context"
	"log"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stubbedev/mongodb-mcp/internal/config"
	"github.com/stubbedev/mongodb-mcp/internal/source"
	"github.com/stubbedev/mongodb-mcp/internal/tools"
)

// New builds an MCP server with all MongoDB tools registered.
func New(cfg *config.Config, reg *source.Registry) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    cfg.Server.Name,
		Version: cfg.Server.Version,
	}, nil)
	tools.Register(s, reg)
	return s
}

// RunStdio serves the MCP server over stdin/stdout until ctx is cancelled.
func RunStdio(ctx context.Context, s *mcp.Server) error {
	return s.Run(ctx, &mcp.StdioTransport{})
}

// HTTPHandler returns an http.Handler serving the MCP server over streamable
// HTTP at cfg.Path, with Origin allow-listing suitable for running behind a
// reverse proxy or MCP proxy.
func HTTPHandler(s *mcp.Server, cfg config.HTTPConfig) http.Handler {
	streamable := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, streamableOptions(cfg))

	mux := http.NewServeMux()
	mux.Handle(cfg.Path, streamable)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	return crossOriginProtection(cfg.AllowedOrigins, mux)
}

// crossOriginProtection wraps the handler with the standard library's
// cross-origin protection (Sec-Fetch-Site / Origin vs Host). Non-browser and
// same-origin requests pass; cross-origin browser requests are denied unless
// their Origin is in the trusted list.
//
//   - empty list -> default protection
//   - ["*"]      -> protection disabled (trust the fronting proxy)
//   - other      -> each entry added as a trusted Origin
func crossOriginProtection(allowed []string, next http.Handler) http.Handler {
	for _, o := range allowed {
		if o == "*" {
			return next
		}
	}
	p := http.NewCrossOriginProtection()
	for _, o := range allowed {
		if err := p.AddTrustedOrigin(o); err != nil {
			log.Printf("ignoring invalid allowed_origin %q: %v", o, err)
		}
	}
	return p.Handler(next)
}
