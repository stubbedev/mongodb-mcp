package source

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stubbedev/mongodb-mcp/internal/config"
)

// Registry resolution with no request (unit-test path) and no client roots falls
// back to the global registry.
func TestResolveFallback(t *testing.T) {
	base := NewRegistry(map[string]config.SourceConfig{"a": {URI: "mongodb://h"}})
	r := NewResolver(base)
	got, err := r.Registry(context.Background(), nil)
	if err != nil {
		t.Fatalf("Registry: %v", err)
	}
	if got != base {
		t.Fatal("expected fallback to the base registry")
	}
}

// Registry resolution errors when there is neither a root config nor a global fallback.
func TestResolveNoConfig(t *testing.T) {
	r := NewResolver(nil)
	if _, err := r.Registry(context.Background(), nil); err == nil {
		t.Fatal("expected error when no config resolves")
	}
}

// rootRegistry builds a per-root registry, serves it from cache on an unchanged
// mtime, and rebuilds it when the config file's mtime advances.
func TestRootRegistryCacheAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), config.RootConfigName)
	if err := os.WriteFile(path, []byte(`{"sources":{"repo":{"uri":"mongodb://h"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(nil)
	mt := time.Unix(1000, 0)

	reg1, err := r.rootRegistry(path, mt)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, ok := reg1.Config("repo"); !ok {
		t.Fatal("expected source 'repo'")
	}
	if reg2, _ := r.rootRegistry(path, mt); reg2 != reg1 {
		t.Fatal("same mtime should hit the cache")
	}

	// Edit the config and advance the mtime: expect a rebuild with new sources.
	if err := os.WriteFile(path, []byte(`{"sources":{"other":{"uri":"mongodb://h"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	reg3, err := r.rootRegistry(path, mt.Add(time.Second))
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reg3 == reg1 {
		t.Fatal("advanced mtime should rebuild")
	}
	if _, ok := reg3.Config("other"); !ok {
		t.Fatal("expected reloaded source 'other'")
	}
}

// Header-injected roots are request-scoped: two calls carrying different
// X-Mcp-Roots headers resolve to different per-workspace configs, with no bleed
// through shared session state (the multi-tenant / multiplexing-proxy case).
func TestHeaderRootsRequestScoped(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	write := func(dir, src string) {
		p := filepath.Join(dir, config.RootConfigName)
		if err := os.WriteFile(p, []byte(`{"sources":{"`+src+`":{"uri":"mongodb://h"}}}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(dirA, "aonly")
	write(dirB, "bonly")

	r := NewResolver(nil)
	reqFor := func(dir string) *mcp.CallToolRequest {
		return &mcp.CallToolRequest{Extra: &mcp.RequestExtra{
			Header: http.Header{"X-Mcp-Roots": []string{"file://" + dir}},
		}}
	}

	regA, err := r.Registry(context.Background(), reqFor(dirA))
	if err != nil {
		t.Fatalf("resolve A: %v", err)
	}
	regB, err := r.Registry(context.Background(), reqFor(dirB))
	if err != nil {
		t.Fatalf("resolve B: %v", err)
	}

	if _, ok := regA.Config("aonly"); !ok {
		t.Error("A should see aonly")
	}
	if _, ok := regA.Config("bonly"); ok {
		t.Error("A must NOT see B's source (cross-contamination)")
	}
	if _, ok := regB.Config("bonly"); !ok {
		t.Error("B should see bonly")
	}
	if _, ok := regB.Config("aonly"); ok {
		t.Error("B must NOT see A's source (cross-contamination)")
	}
}

func TestFileURIToPath(t *testing.T) {
	cases := map[string]string{
		"file:///home/u/repo": "/home/u/repo",
		"/home/u/repo":        "/home/u/repo",
		"file:///C:/x":        "C:/x",
		"https://example.com": "",
		"relative/path":       "",
		"":                    "",
	}
	for in, want := range cases {
		if got := fileURIToPath(in); got != want {
			t.Errorf("fileURIToPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRootsFromHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("X-Mcp-Roots", "file:///a, /b")
	h.Add("X-Mcp-Root", "not-a-root") // relative → dropped
	got := rootsFromHeaders(h)
	if len(got) != 2 || got[0] != "/a" || got[1] != "/b" {
		t.Fatalf("got %v, want [/a /b]", got)
	}
}
