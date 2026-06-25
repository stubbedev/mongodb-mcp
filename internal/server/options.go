package server

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stubbedev/mongodb-mcp/internal/config"
)

// streamableOptions maps our HTTP config onto the SDK's streamable HTTP
// options.
func streamableOptions(cfg config.HTTPConfig) *mcp.StreamableHTTPOptions {
	return &mcp.StreamableHTTPOptions{
		Stateless:                  cfg.Stateless,
		JSONResponse:               cfg.JSONResponse,
		DisableLocalhostProtection: cfg.TrustProxy,
	}
}
