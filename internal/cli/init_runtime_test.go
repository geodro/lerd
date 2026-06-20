package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectProjectRuntime(t *testing.T) {
	cases := []struct {
		name      string
		manifest  string
		wantLabel string
		wantFound bool
	}{
		{"node package.json", "package.json", "Node", true},
		{"node nvmrc only", ".nvmrc", "Node", true},
		{"go", "go.mod", "Go", true},
		{"python pyproject", "pyproject.toml", "Python", true},
		{"python requirements", "requirements.txt", "Python", true},
		{"python manage.py", "manage.py", "Python", true},
		{"ruby", "Gemfile", "Ruby", true},
		{"rust", "Cargo.toml", "Rust", true},
		{"empty dir", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if c.manifest != "" {
				if err := os.WriteFile(filepath.Join(dir, c.manifest), []byte("x"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			rt, found := detectProjectRuntime(dir)
			label := ""
			if rt != nil {
				label = rt.label
			}
			if label != c.wantLabel || found != c.wantFound {
				t.Errorf("detectProjectRuntime(%s) = (%q, %v), want (%q, %v)",
					c.manifest, label, found, c.wantLabel, c.wantFound)
			}
		})
	}
}

func TestDefaultDevCommand(t *testing.T) {
	cases := []struct {
		manifest string
		want     string
	}{
		{"go.mod", "go run ."},
		{"requirements.txt", "python app.py"},
		{"manage.py", "python manage.py runserver"}, // Django: not python app.py
		{"Gemfile", "ruby app.rb"},
		{"Cargo.toml", "cargo run"},
		{"package.json", "npm run dev"},
		{"", ""}, // unknown runtime -> blank (proxy-only)
	}
	for _, c := range cases {
		dir := t.TempDir()
		if c.manifest != "" {
			if err := os.WriteFile(filepath.Join(dir, c.manifest), []byte("x"), 0644); err != nil {
				t.Fatal(err)
			}
		}
		if got := defaultDevCommand(dir); got != c.want {
			t.Errorf("defaultDevCommand(%q) = %q, want %q", c.manifest, got, c.want)
		}
	}
}

func TestStarterContainerfile(t *testing.T) {
	t.Run("runtime-specific base image, bind-mount model", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x"), 0644); err != nil {
			t.Fatal(err)
		}
		out := starterContainerfile(dir, 8080)
		if !strings.Contains(out, "FROM golang:") {
			t.Errorf("expected a Go base image, got:\n%s", out)
		}
		if !strings.Contains(out, "port 8080") {
			t.Errorf("expected the chosen port noted in the header, got:\n%s", out)
		}
		// Project is bind-mounted, so no instruction line should COPY the source
		// or set a WORKDIR (comment lines mentioning them are fine).
		for _, line := range strings.Split(out, "\n") {
			instr := strings.TrimSpace(line)
			if strings.HasPrefix(instr, "COPY") || strings.HasPrefix(instr, "WORKDIR") {
				t.Errorf("starter should not COPY/WORKDIR with a bind-mount, got line: %q", instr)
			}
		}
	})

	t.Run("unknown runtime falls back to a generic skeleton", func(t *testing.T) {
		out := starterContainerfile(t.TempDir(), 3000)
		if !strings.Contains(out, "FROM alpine:") {
			t.Errorf("expected a generic alpine base, got:\n%s", out)
		}
		if !strings.Contains(out, "port 3000") {
			t.Errorf("expected the chosen port noted in the header, got:\n%s", out)
		}
	})
}
