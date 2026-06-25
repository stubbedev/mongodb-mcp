// Command gen-schema writes the JSON Schema for the mongodb-mcp config file to
// config.schema.json, derived from the Go config types. It is run by CI and by
// `go generate` so the schema never drifts from the code.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/invopop/jsonschema"

	"github.com/stubbedev/mongodb-mcp/internal/config"
)

func main() {
	out := flag.String("out", "config.schema.json", "output path for the JSON schema")
	flag.Parse()

	r := &jsonschema.Reflector{
		// Emit field descriptions from struct comments where present.
		ExpandedStruct: true,
	}
	schema := r.Reflect(&config.Config{})
	schema.ID = "https://github.com/stubbedev/mongodb-mcp/config.schema.json"
	schema.Title = "mongodb-mcp configuration"

	b, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "gen-schema: marshal: %v\n", err)
		os.Exit(1)
	}
	b = append(b, '\n')

	if err := os.WriteFile(*out, b, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "gen-schema: write %s: %v\n", *out, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s\n", *out)
}
