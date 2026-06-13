package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSkipSourceDir(t *testing.T) {
	skip := []string{"node_modules", "vendor", "storage", "public", "dist", "build", "bootstrap", ".git", ".vite", ".next", ".idea"}
	for _, n := range skip {
		if !SkipSourceDir(n) {
			t.Errorf("SkipSourceDir(%q) = false, want true", n)
		}
	}
	keep := []string{"app", "src", "resources", "routes", "components", "pages"}
	for _, n := range keep {
		if SkipSourceDir(n) {
			t.Errorf("SkipSourceDir(%q) = true, want false", n)
		}
	}
}

func TestSourceWatchRoots(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"app", "resources", "node_modules", "vendor"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Default set: only the existing source dirs are returned (a file/absent dir
	// is skipped), and node_modules/vendor aren't in the defaults anyway.
	got := SourceWatchRoots(nil, root)
	want := map[string]bool{filepath.Join(root, "app"): true, filepath.Join(root, "resources"): true}
	if len(got) != len(want) {
		t.Fatalf("default roots = %v, want app+resources", got)
	}
	for _, p := range got {
		if !want[p] {
			t.Errorf("unexpected watched root %q", p)
		}
	}

	// Framework override: only its declared, existing dirs.
	fw := &Framework{SourceDirs: []string{"app", "missing"}}
	got = SourceWatchRoots(fw, root)
	if len(got) != 1 || got[0] != filepath.Join(root, "app") {
		t.Errorf("override roots = %v, want [app]", got)
	}
}
