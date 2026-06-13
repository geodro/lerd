package mcp

import (
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func TestSiteForToolArgs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	if err := config.AddSite(config.Site{Name: "myapp", Path: "/srv/myapp", PHPVersion: "8.4"}); err != nil {
		t.Fatalf("seed site: %v", err)
	}

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"explicit site name", map[string]any{"site": "myapp"}, "myapp"},
		{"site beats path", map[string]any{"site": "myapp", "path": "/elsewhere"}, "myapp"},
		{"path resolves to site", map[string]any{"path": "/srv/myapp"}, "myapp"},
		{"unknown path", map[string]any{"path": "/no/such/site"}, ""},
		{"no site or path", map[string]any{"action": "list"}, ""},
	}
	for _, c := range cases {
		if got := siteForToolArgs(c.args); got != c.want {
			t.Errorf("%s: siteForToolArgs = %q, want %q", c.name, got, c.want)
		}
	}
}
