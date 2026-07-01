package tools

import (
	"context"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/stubbedev/mongodb-mcp/internal/source"
)

// docs is the common output shape for tools that return documents.
type docsOut struct {
	Count     int      `json:"count"`
	HasMore   bool     `json:"hasMore,omitempty"`
	Documents []bson.M `json:"documents"`
}

func registerRead(server *mcp.Server, res *source.Resolver) {
	type sourceInfo struct {
		Name            string `json:"name"`
		Description     string `json:"description,omitempty"`
		ReadOnly        bool   `json:"readonly"`
		DefaultDatabase string `json:"defaultDatabase,omitempty"`
		Remote          bool   `json:"remote"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "listSources",
		Description: "List the configured sources available to this client, with an optional description of what each is for (use it to pick the right source for a task without being told which one), whether it is read-only, its default database, and whether it is reached over an SSH tunnel. Sources are resolved from the client's workspace root.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		reg, err := res.Registry(ctx, req)
		if err != nil {
			return errResult(err), nil, nil
		}
		names := reg.Names()
		sort.Strings(names)
		infos := make([]sourceInfo, 0, len(names))
		for _, n := range names {
			c, ok := reg.Config(n)
			if !ok {
				continue
			}
			infos = append(infos, sourceInfo{
				Name:            n,
				Description:     c.Description,
				ReadOnly:        c.ReadOnly,
				DefaultDatabase: c.DefaultDatabase,
				Remote:          c.SSH != nil,
			})
		}
		out := struct {
			Sources []sourceInfo `json:"sources"`
		}{Sources: infos}
		result, err := textResult(out)
		return result, out, err
	})

	type findIn struct {
		Source     string `json:"source" jsonschema:"Name of the configured source to query."`
		Database   string `json:"database,omitempty" jsonschema:"Database name (defaults to the source's default_database)."`
		Collection string `json:"collection" jsonschema:"Collection name."`
		Filter     string `json:"filter,omitempty" jsonschema:"Query filter as a JSON object. Empty matches all documents."`
		Projection string `json:"projection,omitempty" jsonschema:"Projection as a JSON object, e.g. {\"_id\":0,\"name\":1}."`
		Sort       string `json:"sort,omitempty" jsonschema:"Sort spec as a JSON object, e.g. {\"age\":-1}."`
		Limit      int64  `json:"limit,omitempty" jsonschema:"Maximum documents to return. 0 means the server default; capped at 1000."`
		Skip       int64  `json:"skip,omitempty" jsonschema:"Number of documents to skip."`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "find",
		Description: "Query documents in a collection with an optional filter, projection, sort, limit and skip.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in findIn) (*mcp.CallToolResult, any, error) {
		reg, err := res.Registry(ctx, req)
		if err != nil {
			return errResult(err), nil, nil
		}
		src, err := reg.Get(ctx, in.Source)
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
		opts := options.Find()
		limit := in.Limit
		if limit <= 0 || limit > maxDocs {
			limit = maxDocs
		}
		// Fetch one extra so HasMore reflects whether the next page exists.
		opts.SetLimit(limit + 1)
		if in.Skip > 0 {
			opts.SetSkip(in.Skip)
		}
		if in.Projection != "" {
			p, err := parseDoc(in.Projection)
			if err != nil {
				return errResult(err), nil, nil
			}
			opts.SetProjection(p)
		}
		if in.Sort != "" {
			s, err := parseDoc(in.Sort)
			if err != nil {
				return errResult(err), nil, nil
			}
			opts.SetSort(s)
		}
		cur, err := coll.Find(ctx, filter, opts)
		if err != nil {
			return errResult(err), nil, nil
		}
		var out docsOut
		if err := cur.All(ctx, &out.Documents); err != nil {
			return errResult(err), nil, nil
		}
		capDocs(&out, int(limit))
		res, err := textResult(out)
		return res, out, err
	})

	type aggregateIn struct {
		Source     string `json:"source" jsonschema:"Name of the configured source to query."`
		Database   string `json:"database,omitempty" jsonschema:"Database name (defaults to the source's default_database)."`
		Collection string `json:"collection" jsonschema:"Collection name."`
		Pipeline   string `json:"pipeline" jsonschema:"Aggregation pipeline as a JSON array of stage objects."`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "aggregate",
		Description: "Run an aggregation pipeline against a collection. Pipelines using $out or $merge write data and are refused on read-only sources.",
		// Not ReadOnlyHint: a pipeline may write via $out / $merge.
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in aggregateIn) (*mcp.CallToolResult, any, error) {
		reg, err := res.Registry(ctx, req)
		if err != nil {
			return errResult(err), nil, nil
		}
		src, err := reg.Get(ctx, in.Source)
		if err != nil {
			return errResult(err), nil, nil
		}
		coll, err := collection(src, in.Database, in.Collection)
		if err != nil {
			return errResult(err), nil, nil
		}
		pipeline, err := parsePipeline(in.Pipeline)
		if err != nil {
			return errResult(err), nil, nil
		}
		// $out/$merge persist data — gate them behind the writable policy so an
		// aggregate cannot bypass a read-only source.
		writes := pipelineWrites(pipeline)
		if writes && src.ReadOnly {
			return errResult(source.ErrReadOnly{Source: in.Source}), nil, nil
		}
		// Bound non-writing pipelines so they cannot pull an unbounded result
		// set into memory and the model's context.
		if !writes {
			pipeline = append(pipeline, bson.D{{Key: "$limit", Value: int64(maxDocs + 1)}})
		}
		cur, err := coll.Aggregate(ctx, pipeline)
		if err != nil {
			return errResult(err), nil, nil
		}
		var out docsOut
		if err := cur.All(ctx, &out.Documents); err != nil {
			return errResult(err), nil, nil
		}
		capDocs(&out, maxDocs)
		res, err := textResult(out)
		return res, out, err
	})

	type countIn struct {
		Source     string `json:"source" jsonschema:"Name of the configured source to query."`
		Database   string `json:"database,omitempty" jsonschema:"Database name (defaults to the source's default_database)."`
		Collection string `json:"collection" jsonschema:"Collection name."`
		Filter     string `json:"filter,omitempty" jsonschema:"Query filter as a JSON object. Empty counts all documents."`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "count",
		Description: "Count documents in a collection matching an optional filter.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in countIn) (*mcp.CallToolResult, any, error) {
		reg, err := res.Registry(ctx, req)
		if err != nil {
			return errResult(err), nil, nil
		}
		src, err := reg.Get(ctx, in.Source)
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
		n, err := coll.CountDocuments(ctx, filter)
		if err != nil {
			return errResult(err), nil, nil
		}
		out := struct {
			Count int64 `json:"count"`
		}{Count: n}
		res, err := textResult(out)
		return res, out, err
	})

	type distinctIn struct {
		Source     string `json:"source" jsonschema:"Name of the configured source to query."`
		Database   string `json:"database,omitempty" jsonschema:"Database name (defaults to the source's default_database)."`
		Collection string `json:"collection" jsonschema:"Collection name."`
		Field      string `json:"field" jsonschema:"Field name to return distinct values for."`
		Filter     string `json:"filter,omitempty" jsonschema:"Query filter as a JSON object."`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "distinct",
		Description: "Return the distinct values for a field across matching documents.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in distinctIn) (*mcp.CallToolResult, any, error) {
		reg, err := res.Registry(ctx, req)
		if err != nil {
			return errResult(err), nil, nil
		}
		src, err := reg.Get(ctx, in.Source)
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
		var values []any
		if err := coll.Distinct(ctx, in.Field, filter).Decode(&values); err != nil {
			return errResult(err), nil, nil
		}
		hasMore := false
		if len(values) > maxDocs {
			values = values[:maxDocs]
			hasMore = true
		}
		out := struct {
			Values  []any `json:"values"`
			HasMore bool  `json:"hasMore,omitempty"`
		}{Values: values, HasMore: hasMore}
		res, err := textResult(out)
		return res, out, err
	})

	type listDatabasesIn struct {
		Source string `json:"source" jsonschema:"Name of the configured source to query."`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "listDatabases",
		Description: "List database names on the source.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in listDatabasesIn) (*mcp.CallToolResult, any, error) {
		reg, err := res.Registry(ctx, req)
		if err != nil {
			return errResult(err), nil, nil
		}
		src, err := reg.Get(ctx, in.Source)
		if err != nil {
			return errResult(err), nil, nil
		}
		names, err := src.Client().ListDatabaseNames(ctx, bson.D{})
		if err != nil {
			return errResult(err), nil, nil
		}
		out := struct {
			Databases []string `json:"databases"`
		}{Databases: names}
		res, err := textResult(out)
		return res, out, err
	})

	type listCollectionsIn struct {
		Source   string `json:"source" jsonschema:"Name of the configured source to query."`
		Database string `json:"database,omitempty" jsonschema:"Database name (defaults to the source's default_database)."`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "listCollections",
		Description: "List collection names in a database.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in listCollectionsIn) (*mcp.CallToolResult, any, error) {
		reg, err := res.Registry(ctx, req)
		if err != nil {
			return errResult(err), nil, nil
		}
		src, err := reg.Get(ctx, in.Source)
		if err != nil {
			return errResult(err), nil, nil
		}
		db, err := src.Database(in.Database)
		if err != nil {
			return errResult(err), nil, nil
		}
		names, err := db.ListCollectionNames(ctx, bson.D{})
		if err != nil {
			return errResult(err), nil, nil
		}
		out := struct {
			Collections []string `json:"collections"`
		}{Collections: names}
		res, err := textResult(out)
		return res, out, err
	})

	type indexesIn struct {
		Source     string `json:"source" jsonschema:"Name of the configured source to query."`
		Database   string `json:"database,omitempty" jsonschema:"Database name (defaults to the source's default_database)."`
		Collection string `json:"collection" jsonschema:"Collection name."`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "indexes",
		Description: "List the indexes on a collection.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in indexesIn) (*mcp.CallToolResult, any, error) {
		reg, err := res.Registry(ctx, req)
		if err != nil {
			return errResult(err), nil, nil
		}
		src, err := reg.Get(ctx, in.Source)
		if err != nil {
			return errResult(err), nil, nil
		}
		coll, err := collection(src, in.Database, in.Collection)
		if err != nil {
			return errResult(err), nil, nil
		}
		cur, err := coll.Indexes().List(ctx)
		if err != nil {
			return errResult(err), nil, nil
		}
		var out docsOut
		if err := cur.All(ctx, &out.Documents); err != nil {
			return errResult(err), nil, nil
		}
		capDocs(&out, maxDocs)
		res, err := textResult(out)
		return res, out, err
	})
}
