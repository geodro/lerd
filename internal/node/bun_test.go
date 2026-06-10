package node

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUsesBunAutoDetect(t *testing.T) {
	cases := []struct {
		name string
		file string
		want bool
	}{
		{"bun.lockb", "bun.lockb", true},
		{"bun.lock text", "bun.lock", true},
		{"bunfig.toml", "bunfig.toml", true},
		{"package-lock.json", "package-lock.json", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, tc.file), []byte("x"), 0644); err != nil {
				t.Fatal(err)
			}
			if got := UsesBun(dir); got != tc.want {
				t.Errorf("UsesBun(%s) = %v, want %v", tc.file, got, tc.want)
			}
		})
	}
}

func TestUsesBunEmptyDir(t *testing.T) {
	if UsesBun(t.TempDir()) {
		t.Error("UsesBun on empty dir = true, want false")
	}
}

func TestUsesBunJSRuntimeOverride(t *testing.T) {
	// js_runtime: node forces npm even when bun.lockb is present.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bun.lockb"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".lerd.yaml"), []byte("js_runtime: node\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if UsesBun(dir) {
		t.Error("js_runtime: node should force Node even with bun.lockb present")
	}

	// js_runtime: bun forces bun with no lockfile present.
	dir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir2, ".lerd.yaml"), []byte("js_runtime: bun\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if !UsesBun(dir2) {
		t.Error("js_runtime: bun should force bun with no lockfile")
	}

	// Node aliases (nodejs/npm/uppercase) all normalize to forcing Node.
	for _, val := range []string{"nodejs", "npm", "Node", "NODE"} {
		d := t.TempDir()
		if err := os.WriteFile(filepath.Join(d, "bun.lockb"), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, ".lerd.yaml"), []byte("js_runtime: "+val+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if UsesBun(d) {
			t.Errorf("js_runtime: %q should normalize to Node and force npm", val)
		}
	}
}

func TestUsesBunPackageManagerField(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"packageManager":"bun@1.1.0"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if !UsesBun(dir) {
		t.Error("UsesBun with packageManager bun = false, want true")
	}
}

func TestBunify(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"npm run dev", "bun run dev"},
		{"npm install", "bun install"},
		{"npx vite", "bunx vite"},
		{"node server.js", "bun server.js"},
		{"  npm run build", "  bun run build"},
		{"php artisan serve", "php artisan serve"},
		{"npmlike thing", "npmlike thing"},
		{"npm", "bun"},
		// host-proxy commands are wrapped with an `env PORT=N` prefix.
		{"env PORT=3000 npm run start:dev", "env PORT=3000 bun run start:dev"},
		{"PORT=3000 npm run dev", "PORT=3000 bun run dev"},
		{"env FOO=a BAR=b npx tsx watch", "env FOO=a BAR=b bunx tsx watch"},
		{"env", "env"},
		{"PORT=3000 python app.py", "PORT=3000 python app.py"},
	}
	for _, tc := range cases {
		if got := Bunify(tc.in); got != tc.want {
			t.Errorf("Bunify(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBunPathResolvesHomeBun(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	binDir := filepath.Join(home, ".bun", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	bunBin := filepath.Join(binDir, "bun")
	if err := os.WriteFile(bunBin, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if got := BunPath(); got != bunBin {
		t.Errorf("BunPath() = %q, want %q", got, bunBin)
	}
}
