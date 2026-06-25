package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/stubbedev/mongodb-mcp/internal/source"
)

func registerAdmin(server *mcp.Server, reg *source.Registry) {
	type createIndexIn struct {
		Source     string `json:"source" jsonschema:"Name of the configured source (must not be read-only)."`
		Database   string `json:"database,omitempty" jsonschema:"Database name (defaults to the source's default_database)."`
		Collection string `json:"collection" jsonschema:"Collection name."`
		Keys       string `json:"keys" jsonschema:"Index key spec as a JSON object, e.g. {\"name\":1,\"age\":-1}."`
		Unique     bool   `json:"unique,omitempty" jsonschema:"Create a unique index."`
		Name       string `json:"name,omitempty" jsonschema:"Optional index name."`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "createIndex",
		Description: "Create an index on a collection. Refused on read-only sources.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createIndexIn) (*mcp.CallToolResult, any, error) {
		src, err := reg.RequireWritable(ctx, in.Source)
		if err != nil {
			return errResult(err), nil, nil
		}
		coll, err := collection(src, in.Database, in.Collection)
		if err != nil {
			return errResult(err), nil, nil
		}
		keys, err := parseDoc(in.Keys)
		if err != nil {
			return errResult(err), nil, nil
		}
		idxOpts := options.Index()
		if in.Unique {
			idxOpts.SetUnique(true)
		}
		if in.Name != "" {
			idxOpts.SetName(in.Name)
		}
		name, err := coll.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: keys, Options: idxOpts})
		if err != nil {
			return errResult(err), nil, nil
		}
		out := struct {
			Name string `json:"name"`
		}{Name: name}
		res, err := textResult(out)
		return res, out, err
	})

	type dropIndexIn struct {
		Source     string `json:"source" jsonschema:"Name of the configured source (must not be read-only)."`
		Database   string `json:"database,omitempty" jsonschema:"Database name (defaults to the source's default_database)."`
		Collection string `json:"collection" jsonschema:"Collection name."`
		Name       string `json:"name" jsonschema:"Name of the index to drop."`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "dropIndex",
		Description: "Drop an index from a collection by name. Refused on read-only sources.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in dropIndexIn) (*mcp.CallToolResult, any, error) {
		src, err := reg.RequireWritable(ctx, in.Source)
		if err != nil {
			return errResult(err), nil, nil
		}
		coll, err := collection(src, in.Database, in.Collection)
		if err != nil {
			return errResult(err), nil, nil
		}
		if err := coll.Indexes().DropOne(ctx, in.Name); err != nil {
			return errResult(err), nil, nil
		}
		out := struct {
			Dropped string `json:"dropped"`
		}{Dropped: in.Name}
		res, err := textResult(out)
		return res, out, err
	})

	type createCollectionIn struct {
		Source     string `json:"source" jsonschema:"Name of the configured source (must not be read-only)."`
		Database   string `json:"database,omitempty" jsonschema:"Database name (defaults to the source's default_database)."`
		Collection string `json:"collection" jsonschema:"Name of the collection to create."`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "createCollection",
		Description: "Create a collection. Refused on read-only sources.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createCollectionIn) (*mcp.CallToolResult, any, error) {
		src, err := reg.RequireWritable(ctx, in.Source)
		if err != nil {
			return errResult(err), nil, nil
		}
		db, err := src.Database(in.Database)
		if err != nil {
			return errResult(err), nil, nil
		}
		if err := db.CreateCollection(ctx, in.Collection); err != nil {
			return errResult(err), nil, nil
		}
		out := struct {
			Created string `json:"created"`
		}{Created: in.Collection}
		res, err := textResult(out)
		return res, out, err
	})

	type dropCollectionIn struct {
		Source     string `json:"source" jsonschema:"Name of the configured source (must not be read-only)."`
		Database   string `json:"database,omitempty" jsonschema:"Database name (defaults to the source's default_database)."`
		Collection string `json:"collection" jsonschema:"Name of the collection to drop."`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "dropCollection",
		Description: "Drop a collection and all its documents. Refused on read-only sources.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in dropCollectionIn) (*mcp.CallToolResult, any, error) {
		src, err := reg.RequireWritable(ctx, in.Source)
		if err != nil {
			return errResult(err), nil, nil
		}
		coll, err := collection(src, in.Database, in.Collection)
		if err != nil {
			return errResult(err), nil, nil
		}
		if err := coll.Drop(ctx); err != nil {
			return errResult(err), nil, nil
		}
		out := struct {
			Dropped string `json:"dropped"`
		}{Dropped: in.Collection}
		res, err := textResult(out)
		return res, out, err
	})
}
