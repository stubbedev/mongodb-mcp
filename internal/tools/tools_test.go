package tools

import (
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestPipelineWrites(t *testing.T) {
	mustParse := func(s string) []bson.D {
		p, err := parsePipeline(s)
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}
		return p
	}
	if pipelineWrites(mustParse(`[{"$match":{"a":1}},{"$count":"n"}]`)) {
		t.Fatal("read-only pipeline reported as writing")
	}
	if !pipelineWrites(mustParse(`[{"$match":{"a":1}},{"$out":"dst"}]`)) {
		t.Fatal("$out pipeline not detected as writing")
	}
	if !pipelineWrites(mustParse(`[{"$merge":{"into":"dst"}}]`)) {
		t.Fatal("$merge pipeline not detected as writing")
	}
}

func TestCapDocs(t *testing.T) {
	out := docsOut{Documents: make([]bson.M, maxDocs+5)}
	capDocs(&out)
	if !out.Truncated || out.Count != maxDocs || len(out.Documents) != maxDocs {
		t.Fatalf("over-cap: truncated=%v count=%d len=%d", out.Truncated, out.Count, len(out.Documents))
	}
	out = docsOut{Documents: make([]bson.M, 3)}
	capDocs(&out)
	if out.Truncated || out.Count != 3 {
		t.Fatalf("under-cap: truncated=%v count=%d", out.Truncated, out.Count)
	}
}

func TestParseDoc(t *testing.T) {
	d, err := parseDoc("")
	if err != nil || d == nil || len(d) != 0 {
		t.Fatalf("empty: %v %v", d, err)
	}
	d, err = parseDoc(`{"a":1,"b":"x"}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(d) != 2 || d[0].Key != "a" {
		t.Fatalf("unexpected doc: %v", d)
	}
	if _, err := parseDoc(`{bad json}`); err == nil {
		t.Fatal("expected error for bad json")
	}
}

func TestParsePipeline(t *testing.T) {
	p, err := parsePipeline("")
	if err != nil || len(p) != 0 {
		t.Fatalf("empty: %v %v", p, err)
	}
	p, err = parsePipeline(`[{"$match":{"a":1}},{"$count":"n"}]`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(p) != 2 {
		t.Fatalf("want 2 stages, got %d", len(p))
	}
	if _, err := parsePipeline(`{"not":"array"}`); err == nil {
		t.Fatal("expected error for non-array pipeline")
	}
}
