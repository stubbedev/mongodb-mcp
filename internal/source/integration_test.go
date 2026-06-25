//go:build integration

// Integration tests require Docker. Run with: go test -tags=integration ./...
package source_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/stubbedev/mongodb-mcp/internal/config"
	"github.com/stubbedev/mongodb-mcp/internal/source"
)

func startMongo(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	ctr, err := mongodb.Run(ctx, "mongo:7")
	if err != nil {
		t.Fatalf("start mongo: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(ctr) })
	uri, err := ctr.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	return uri
}

func TestRegistryReadWriteAndReadonly(t *testing.T) {
	uri := startMongo(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	reg := source.NewRegistry(map[string]config.SourceConfig{
		"rw": {URI: uri, DefaultDatabase: "testdb"},
		"ro": {URI: uri, DefaultDatabase: "testdb", ReadOnly: true},
	})
	defer func() { _ = reg.Close(context.Background()) }()

	// Writable source: insert and read back.
	rw, err := reg.RequireWritable(ctx, "rw")
	if err != nil {
		t.Fatalf("RequireWritable(rw): %v", err)
	}
	db, err := rw.Database("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Collection("c").InsertOne(ctx, bson.D{{Key: "x", Value: 1}}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	n, err := db.Collection("c").CountDocuments(ctx, bson.D{})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}

	// Read-only source: reads connect fine, writes are refused by the registry.
	if _, err := reg.Get(ctx, "ro"); err != nil {
		t.Fatalf("Get(ro): %v", err)
	}
	_, err = reg.RequireWritable(ctx, "ro")
	var roErr source.ErrReadOnly
	if !errors.As(err, &roErr) {
		t.Fatalf("RequireWritable(ro) error = %v, want ErrReadOnly", err)
	}

	// Unknown source.
	_, err = reg.Get(ctx, "nope")
	var unknown source.ErrUnknownSource
	if !errors.As(err, &unknown) {
		t.Fatalf("Get(nope) error = %v, want ErrUnknownSource", err)
	}
}
