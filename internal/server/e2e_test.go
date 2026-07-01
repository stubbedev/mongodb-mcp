package server

import (
	"context"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stubbedev/mongodb-mcp/internal/config"
	"github.com/stubbedev/mongodb-mcp/internal/source"
)

// TestServerExposesTools connects a client to the server over an in-memory
// transport and verifies the expected MongoDB tools are advertised. No MongoDB
// is required: tool listing does not open a connection.
func TestServerExposesTools(t *testing.T) {
	cfg := &config.Config{
		Server:  config.ServerConfig{Name: "mongodb-mcp", Version: "test"},
		Sources: map[string]config.SourceConfig{"local": {URI: "mongodb://localhost:27017"}},
	}
	reg := source.NewRegistry(cfg.Sources)
	srv := New(cfg, reg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientT, serverT := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer func() { _ = ss.Close() }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer func() { _ = cs.Close() }()

	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	got := make(map[string]bool, len(res.Tools))
	for _, tool := range res.Tools {
		got[tool.Name] = true
	}
	want := []string{
		"listSources",
		"find", "aggregate", "count", "distinct",
		"listDatabases", "listCollections", "indexes",
		"insertOne", "insertMany", "updateOne", "updateMany",
		"deleteOne", "deleteMany",
		"createIndex", "dropIndex", "createCollection", "dropCollection",
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("tool %q not advertised", name)
		}
	}
	if len(res.Tools) != len(want) {
		t.Errorf("tool count = %d, want %d (%v)", len(res.Tools), len(want), got)
	}
}
