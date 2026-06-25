package tools

import "testing"

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
