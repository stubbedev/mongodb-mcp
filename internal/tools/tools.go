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

// maxDocs caps how many documents/values any tool returns, so a single query
// cannot flood the model's context (or this process's memory).
const maxDocs = 1000

// boolPtr returns a pointer to b, for the *bool hint fields in ToolAnnotations.
func boolPtr(b bool) *bool { return &b }

// capDocs truncates a result to limit and sets HasMore when more documents were
// available than returned, then sets Count to the number actually returned.
// Callers that want HasMore to be accurate should fetch limit+1 documents.
func capDocs(out *docsOut, limit int) {
	if len(out.Documents) > limit {
		out.Documents = out.Documents[:limit]
		out.HasMore = true
	}
	out.Count = len(out.Documents)
}

// pipelineWrites reports whether an aggregation pipeline persists data via a
// $out or $merge stage. Such pipelines write to a collection even though
// aggregate is otherwise a read tool, so they must be gated like a write.
// $out/$merge are required to be the final stage; each stage doc has exactly
// one key.
func pipelineWrites(pipeline []bson.D) bool {
	for _, stage := range pipeline {
		if len(stage) > 0 {
			switch stage[0].Key {
			case "$out", "$merge":
				return true
			}
		}
	}
	return false
}

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
//
// Each handler resolves its registry from the resolver per call, so the target
// database follows the calling client's MCP workspace root.
func Register(server *mcp.Server, res *source.Resolver) {
	registerRead(server, res)
	registerWrite(server, res)
	registerAdmin(server, res)
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
