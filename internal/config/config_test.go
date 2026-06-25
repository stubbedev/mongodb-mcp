package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadValid(t *testing.T) {
	p := writeTemp(t, `{
		"sources": {
			"local": {"uri": "mongodb://localhost:27017", "default_database": "test"}
		}
	}`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Name != "mongodb-mcp" {
		t.Errorf("default server name not applied: %q", cfg.Server.Name)
	}
	if cfg.HTTP.Addr != "127.0.0.1:8080" || cfg.HTTP.Path != "/mcp" {
		t.Errorf("http defaults not applied: %+v", cfg.HTTP)
	}
	if _, ok := cfg.Sources["local"]; !ok {
		t.Errorf("source not loaded")
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	p := writeTemp(t, `{"sources":{"x":{"uri":"mongodb://h"}},"bogus":1}`)
	if _, err := Load(p); err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestValidate(t *testing.T) {
	cases := map[string]Config{
		"no sources": {Sources: map[string]SourceConfig{}},
		"missing uri": {Sources: map[string]SourceConfig{
			"a": {URI: ""},
		}},
		"ssh without auth": {Sources: map[string]SourceConfig{
			"a": {URI: "mongodb://h", SSH: &SSHConfig{Host: "h", User: "u"}},
		}},
		"ssh missing host": {Sources: map[string]SourceConfig{
			"a": {URI: "mongodb://h", SSH: &SSHConfig{User: "u", Password: "p"}},
		}},
		"bad timeout": {Sources: map[string]SourceConfig{
			"a": {URI: "mongodb://h", ConnectTimeout: "ten"},
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if err := c.Validate(); err == nil {
				t.Errorf("expected validation error for %q", name)
			}
		})
	}
}

func TestValidateSSHWithAuthPasses(t *testing.T) {
	c := Config{Sources: map[string]SourceConfig{
		"a": {URI: "mongodb://h", SSH: &SSHConfig{Host: "h", User: "u", UseAgent: true}},
	}}
	if err := c.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestConnectTimeoutOrDefault(t *testing.T) {
	if d := (SourceConfig{}).ConnectTimeoutOrDefault(); d != 10*time.Second {
		t.Errorf("default = %v", d)
	}
	if d := (SourceConfig{ConnectTimeout: "3s"}).ConnectTimeoutOrDefault(); d != 3*time.Second {
		t.Errorf("parsed = %v", d)
	}
	if d := (SourceConfig{ConnectTimeout: "garbage"}).ConnectTimeoutOrDefault(); d != 10*time.Second {
		t.Errorf("fallback = %v", d)
	}
}

func TestExpandPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	if got := ExpandPath("~/x"); got != filepath.Join(home, "x") {
		t.Errorf("ExpandPath(~/x) = %q", got)
	}
	if got := ExpandPath("/abs"); got != "/abs" {
		t.Errorf("ExpandPath(/abs) = %q", got)
	}
	if got := ExpandPath(""); got != "" {
		t.Errorf("ExpandPath(empty) = %q", got)
	}
}
