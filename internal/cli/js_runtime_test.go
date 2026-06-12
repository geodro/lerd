package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
	nodeDet "github.com/geodro/lerd/internal/node"
)

func TestJSRuntimeValue(t *testing.T) {
	cases := []struct {
		choice string
		value  string
		ok     bool
	}{
		{"bun", "bun", true},
		{"node", "node", true},
		{"npm", "node", true},
		{"auto", "", true},
		{"", "", true},
		{"deno", "", false},
	}
	for _, c := range cases {
		value, _, ok := jsRuntimeValue(c.choice)
		if ok != c.ok || value != c.value {
			t.Errorf("jsRuntimeValue(%q) = (%q, %v), want (%q, %v)", c.choice, value, ok, c.value, c.ok)
		}
	}
}

// setJSRuntime should write the chosen js_runtime into the site's .lerd.yaml,
// and "auto" should clear it back to unset so detection takes over again.
func TestSetJSRuntime_WritesAndClears(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))

	site := config.Site{Name: "demo", Path: t.TempDir()}

	var out bytes.Buffer
	if err := setJSRuntime(site, "bun", &out); err != nil {
		t.Fatalf("setJSRuntime bun: %v", err)
	}
	if got := nodeDet.JSRuntime(site.Path); got != "bun" {
		t.Fatalf("after pinning bun, JSRuntime = %q, want bun", got)
	}
	if !strings.Contains(out.String(), "set to bun") {
		t.Errorf("unexpected output: %q", out.String())
	}

	if err := setJSRuntime(site, "auto", &out); err != nil {
		t.Fatalf("setJSRuntime auto: %v", err)
	}
	if got := nodeDet.JSRuntime(site.Path); got != "" {
		t.Errorf("after auto, JSRuntime = %q, want empty", got)
	}
}

func TestSetJSRuntime_RejectsUnknown(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))

	site := config.Site{Name: "demo", Path: t.TempDir()}
	var out bytes.Buffer
	if err := setJSRuntime(site, "deno", &out); err == nil {
		t.Fatal("expected error for unknown runtime, got nil")
	}
}

func TestShowJSRuntime(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))

	site := config.Site{Name: "demo", Path: t.TempDir()}

	var out bytes.Buffer
	if err := showJSRuntime(site, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "auto-detect") {
		t.Errorf("unset site should report auto-detect, got: %q", out.String())
	}

	if err := config.SetProjectJSRuntime(site.Path, "bun"); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := showJSRuntime(site, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "pinned to bun") {
		t.Errorf("pinned site should report bun, got: %q", out.String())
	}
}
