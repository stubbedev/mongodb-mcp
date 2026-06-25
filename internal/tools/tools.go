// Package tools registers the MongoDB MCP tools on a server, wiring each tool
// to the source registry and enforcing the per-source read-only policy.
package tools

import (
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/stubbedev/mongodb-mcp/internal/source"
)

// collection resolves the *mongo.Collection for a source, applying the source's
// default database when dbName is empty.
func collection(src *source.Source, dbName, collName string) (*mongo.Collection, error) {
	db, err := src.Database(dbName)
	if err != nil {
		return nil, err
	}
	if collName == "" {
		return nil, fmt.Errorf("collection name is required")
	}
	return db.Collection(collName), nil
}

// Register adds every MongoDB tool to the server.
//
// Read tools are always available. Write and admin tools are always registered
// (so clients can discover them) but refuse at call time when the selected
// source is read-only — see source.Registry.RequireWritable.
func Register(server *mcp.Server, reg *source.Registry) {
	registerRead(server, reg)
	registerWrite(server, reg)
	registerAdmin(server, reg)
}

// parseDoc parses a JSON / MongoDB Extended JSON object into a bson.D. An empty
// string yields an empty document (e.g. a match-everything filter).
func parseDoc(s string) (bson.D, error) {
	if s == "" {
		return bson.D{}, nil
	}
	var d bson.D
	if err := bson.UnmarshalExtJSON([]byte(s), false, &d); err != nil {
		return nil, fmt.Errorf("invalid JSON document: %w", err)
	}
	return d, nil
}

// parsePipeline parses a JSON array of stage objects into an aggregation
// pipeline.
func parsePipeline(s string) ([]bson.D, error) {
	if s == "" {
		return []bson.D{}, nil
	}
	var p []bson.D
	if err := bson.UnmarshalExtJSON([]byte(s), false, &p); err != nil {
		return nil, fmt.Errorf("invalid JSON pipeline (expected an array of stage objects): %w", err)
	}
	return p, nil
}

// textResult renders v as indented JSON in a single text content block, which
// every MCP client can display.
func textResult(v any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
	}, nil
}

// errResult turns an error into a tool-level error result so the model sees a
// readable message rather than a transport failure.
func errResult(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}
}
