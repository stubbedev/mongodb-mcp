package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/stubbedev/mongodb-mcp/internal/source"
)

func registerWrite(server *mcp.Server, res *source.Resolver) {
	type insertOneIn struct {
		Source     string `json:"source" jsonschema:"Name of the configured source (must not be read-only)."`
		Database   string `json:"database,omitempty" jsonschema:"Database name (defaults to the source's default_database)."`
		Collection string `json:"collection" jsonschema:"Collection name."`
		Document   string `json:"document" jsonschema:"Document to insert, as a JSON object."`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "insertOne",
		Description: "Insert a single document into a collection. Refused on read-only sources.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in insertOneIn) (*mcp.CallToolResult, any, error) {
		reg, err := res.Registry(ctx, req)
		if err != nil {
			return errResult(err), nil, nil
		}
		src, err := reg.RequireWritable(ctx, in.Source)
		if err != nil {
			return errResult(err), nil, nil
		}
		coll, err := collection(src, in.Database, in.Collection)
		if err != nil {
			return errResult(err), nil, nil
		}
		doc, err := parseDoc(in.Document)
		if err != nil {
			return errResult(err), nil, nil
		}
		r, err := coll.InsertOne(ctx, doc)
		if err != nil {
			return errResult(err), nil, nil
		}
		out := struct {
			InsertedID any `json:"insertedId"`
		}{InsertedID: r.InsertedID}
		res, err := textResult(out)
		return res, out, err
	})

	type insertManyIn struct {
		Source     string `json:"source" jsonschema:"Name of the configured source (must not be read-only)."`
		Database   string `json:"database,omitempty" jsonschema:"Database name (defaults to the source's default_database)."`
		Collection string `json:"collection" jsonschema:"Collection name."`
		Documents  string `json:"documents" jsonschema:"Documents to insert, as a JSON array of objects."`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "insertMany",
		Description: "Insert multiple documents into a collection. Refused on read-only sources.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in insertManyIn) (*mcp.CallToolResult, any, error) {
		reg, err := res.Registry(ctx, req)
		if err != nil {
			return errResult(err), nil, nil
		}
		src, err := reg.RequireWritable(ctx, in.Source)
		if err != nil {
			return errResult(err), nil, nil
		}
		coll, err := collection(src, in.Database, in.Collection)
		if err != nil {
			return errResult(err), nil, nil
		}
		docs, err := parsePipeline(in.Documents) // []bson.D from a JSON array
		if err != nil {
			return errResult(fmt.Errorf("invalid documents array: %w", err)), nil, nil
		}
		anyDocs := make([]any, len(docs))
		for i := range docs {
			anyDocs[i] = docs[i]
		}
		r, err := coll.InsertMany(ctx, anyDocs)
		if err != nil {
			return errResult(err), nil, nil
		}
		out := struct {
			InsertedIDs []any `json:"insertedIds"`
		}{InsertedIDs: r.InsertedIDs}
		res, err := textResult(out)
		return res, out, err
	})

	registerUpdate(server, res, "updateOne", "Update a single matching document. Refused on read-only sources.", false)
	registerUpdate(server, res, "updateMany", "Update all matching documents. Refused on read-only sources.", true)

	registerDelete(server, res, "deleteOne", "Delete a single matching document. Refused on read-only sources.", false)
	registerDelete(server, res, "deleteMany", "Delete all matching documents. Refused on read-only sources.", true)
}

func registerUpdate(server *mcp.Server, res *source.Resolver, name, desc string, many bool) {
	type updateIn struct {
		Source     string `json:"source" jsonschema:"Name of the configured source (must not be read-only)."`
		Database   string `json:"database,omitempty" jsonschema:"Database name (defaults to the source's default_database)."`
		Collection string `json:"collection" jsonschema:"Collection name."`
		Filter     string `json:"filter" jsonschema:"Filter selecting documents to update, as a JSON object."`
		Update     string `json:"update" jsonschema:"Update document using update operators, e.g. {\"$set\":{\"x\":1}}."`
		Upsert     bool   `json:"upsert,omitempty" jsonschema:"Insert a new document when no document matches."`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        name,
		Description: desc,
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)},
	},
		func(ctx context.Context, req *mcp.CallToolRequest, in updateIn) (*mcp.CallToolResult, any, error) {
			reg, err := res.Registry(ctx, req)
			if err != nil {
				return errResult(err), nil, nil
			}
			src, err := reg.RequireWritable(ctx, in.Source)
			if err != nil {
				return errResult(err), nil, nil
			}
			coll, err := collection(src, in.Database, in.Collection)
			if err != nil {
				return errResult(err), nil, nil
			}
			filter, err := parseDoc(in.Filter)
			if err != nil {
				return errResult(err), nil, nil
			}
			update, err := parseDoc(in.Update)
			if err != nil {
				return errResult(err), nil, nil
			}
			opts := options.UpdateMany().SetUpsert(in.Upsert)
			var matched, modified, upserted int64
			var upsertedID any
			if many {
				r, err := coll.UpdateMany(ctx, filter, update, opts)
				if err != nil {
					return errResult(err), nil, nil
				}
				matched, modified, upserted, upsertedID = r.MatchedCount, r.ModifiedCount, r.UpsertedCount, r.UpsertedID
			} else {
				r, err := coll.UpdateOne(ctx, filter, update, options.UpdateOne().SetUpsert(in.Upsert))
				if err != nil {
					return errResult(err), nil, nil
				}
				matched, modified, upserted, upsertedID = r.MatchedCount, r.ModifiedCount, r.UpsertedCount, r.UpsertedID
			}
			out := struct {
				MatchedCount  int64 `json:"matchedCount"`
				ModifiedCount int64 `json:"modifiedCount"`
				UpsertedCount int64 `json:"upsertedCount"`
				UpsertedID    any   `json:"upsertedId,omitempty"`
			}{matched, modified, upserted, upsertedID}
			res, err := textResult(out)
			return res, out, err
		})
}

func registerDelete(server *mcp.Server, res *source.Resolver, name, desc string, many bool) {
	type deleteIn struct {
		Source     string `json:"source" jsonschema:"Name of the configured source (must not be read-only)."`
		Database   string `json:"database,omitempty" jsonschema:"Database name (defaults to the source's default_database)."`
		Collection string `json:"collection" jsonschema:"Collection name."`
		Filter     string `json:"filter" jsonschema:"Filter selecting documents to delete, as a JSON object."`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        name,
		Description: desc,
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)},
	},
		func(ctx context.Context, req *mcp.CallToolRequest, in deleteIn) (*mcp.CallToolResult, any, error) {
			reg, err := res.Registry(ctx, req)
			if err != nil {
				return errResult(err), nil, nil
			}
			src, err := reg.RequireWritable(ctx, in.Source)
			if err != nil {
				return errResult(err), nil, nil
			}
			coll, err := collection(src, in.Database, in.Collection)
			if err != nil {
				return errResult(err), nil, nil
			}
			filter, err := parseDoc(in.Filter)
			if err != nil {
				return errResult(err), nil, nil
			}
			var deleted int64
			if many {
				r, err := coll.DeleteMany(ctx, filter)
				if err != nil {
					return errResult(err), nil, nil
				}
				deleted = r.DeletedCount
			} else {
				r, err := coll.DeleteOne(ctx, filter)
				if err != nil {
					return errResult(err), nil, nil
				}
				deleted = r.DeletedCount
			}
			out := struct {
				DeletedCount int64 `json:"deletedCount"`
			}{DeletedCount: deleted}
			res, err := textResult(out)
			return res, out, err
		})
}
