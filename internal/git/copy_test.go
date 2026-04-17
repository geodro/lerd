package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyTreeNative_RegularFilesAndDirs(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "out")

	if err := os.MkdirAll(filepath.Join(src, "sub/nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "top.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub/nested/inner.txt"), []byte("world"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := copyTreeNative(src, dst); err != nil {
		t.Fatalf("copyTreeNative: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dst, "top.txt"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("top.txt: got %q err %v, want %q nil", got, err, "hello")
	}

	got, err = os.ReadFile(filepath.Join(dst, "sub/nested/inner.txt"))
	if err != nil || string(got) != "world" {
		t.Fatalf("inner.txt: got %q err %v, want %q nil", got, err, "world")
	}

	info, err := os.Stat(filepath.Join(dst, "sub/nested/inner.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("inner.txt perm: got %o want 600", perm)
	}
}

func TestCopyTreeNative_PreservesInnerSymlinks(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "out")

	if err := os.WriteFile(filepath.Join(src, "real.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.txt", filepath.Join(src, "link.txt")); err != nil {
		t.Fatal(err)
	}

	if err := copyTreeNative(src, dst); err != nil {
		t.Fatal(err)
	}

	info, err := os.Lstat(filepath.Join(dst, "link.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("link.txt is not a symlink in dst")
	}
	target, err := os.Readlink(filepath.Join(dst, "link.txt"))
	if err != nil || target != "real.txt" {
		t.Errorf("link target: got %q err %v, want %q", target, err, "real.txt")
	}
}

func TestCopyTree_CP(t *testing.T) {
	// Covers the cp fast path end-to-end when the host supports it.
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "out")

	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub/file"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := CopyTree(src, dst); err != nil {
		t.Fatalf("CopyTree: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dst, "sub/file"))
	if err != nil || string(got) != "x" {
		t.Fatalf("copied file: got %q err %v", got, err)
	}
}
