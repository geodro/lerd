package ui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectFavicon(t *testing.T) {
	t.Run("finds favicon.ico in public dir", func(t *testing.T) {
		dir := t.TempDir()
		pub := filepath.Join(dir, "public")
		os.MkdirAll(pub, 0755)
		os.WriteFile(filepath.Join(pub, "index.php"), []byte("<?php"), 0644)
		os.WriteFile(filepath.Join(pub, "favicon.ico"), []byte("icon"), 0644)

		got := detectFavicon(dir, "public")
		if got != filepath.Join(pub, "favicon.ico") {
			t.Errorf("got %q, want %q", got, filepath.Join(pub, "favicon.ico"))
		}
	})

	t.Run("finds favicon.svg over ico when ico missing", func(t *testing.T) {
		dir := t.TempDir()
		pub := filepath.Join(dir, "public")
		os.MkdirAll(pub, 0755)
		os.WriteFile(filepath.Join(pub, "favicon.svg"), []byte("<svg/>"), 0644)

		got := detectFavicon(dir, "public")
		if got != filepath.Join(pub, "favicon.svg") {
			t.Errorf("got %q, want %q", got, filepath.Join(pub, "favicon.svg"))
		}
	})

	t.Run("prefers ico over svg", func(t *testing.T) {
		dir := t.TempDir()
		pub := filepath.Join(dir, "public")
		os.MkdirAll(pub, 0755)
		os.WriteFile(filepath.Join(pub, "favicon.ico"), []byte("icon"), 0644)
		os.WriteFile(filepath.Join(pub, "favicon.svg"), []byte("<svg/>"), 0644)

		got := detectFavicon(dir, "public")
		if got != filepath.Join(pub, "favicon.ico") {
			t.Errorf("got %q, want %q", got, filepath.Join(pub, "favicon.ico"))
		}
	})

	t.Run("returns empty when no favicon exists", func(t *testing.T) {
		dir := t.TempDir()
		os.MkdirAll(filepath.Join(dir, "public"), 0755)

		got := detectFavicon(dir, "public")
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("uses project root when publicDir is dot", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "favicon.png"), []byte("png"), 0644)

		got := detectFavicon(dir, ".")
		if got != filepath.Join(dir, "favicon.png") {
			t.Errorf("got %q, want %q", got, filepath.Join(dir, "favicon.png"))
		}
	})

	t.Run("auto-detects public dir when empty", func(t *testing.T) {
		dir := t.TempDir()
		pub := filepath.Join(dir, "public")
		os.MkdirAll(pub, 0755)
		os.WriteFile(filepath.Join(pub, "index.php"), []byte("<?php"), 0644)
		os.WriteFile(filepath.Join(pub, "favicon.ico"), []byte("icon"), 0644)

		got := detectFavicon(dir, "")
		if got != filepath.Join(pub, "favicon.ico") {
			t.Errorf("got %q, want %q", got, filepath.Join(pub, "favicon.ico"))
		}
	})
}
