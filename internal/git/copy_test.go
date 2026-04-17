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

func TestJSPackageManager_DetectsLockfile(t *testing.T) {
	cases := []struct {
		name     string
		lockfile string
		wantBin  string
		wantArg0 string
	}{
		{"pnpm", "pnpm-lock.yaml", "pnpm", "install"},
		{"yarn", "yarn.lock", "yarn", "install"},
		{"bun-binary", "bun.lockb", "bun", "install"},
		{"bun-text", "bun.lock", "bun", "install"},
		{"npm-lockfile", "package-lock.json", "npm", "ci"},
		{"npm-shrinkwrap", "npm-shrinkwrap.json", "npm", "ci"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, c.lockfile), []byte(""), 0o644); err != nil {
				t.Fatal(err)
			}
			gotBin, gotArgs := jsPackageManager(dir)
			if gotBin != c.wantBin {
				t.Errorf("binary: got %q want %q", gotBin, c.wantBin)
			}
			if len(gotArgs) == 0 || gotArgs[0] != c.wantArg0 {
				t.Errorf("first arg: got %v want %q", gotArgs, c.wantArg0)
			}
		})
	}
}

func TestJSPackageManager_DefaultsToNpmInstallWithoutLockfile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin, args := jsPackageManager(dir)
	if bin != "npm" {
		t.Errorf("binary: got %q want npm", bin)
	}
	if len(args) == 0 || args[0] != "install" {
		t.Errorf("first arg: got %v want install", args)
	}
}

func TestJSPackageManager_PnpmBeatsOtherLockfiles(t *testing.T) {
	// Defensive: if a repo somehow has both pnpm-lock.yaml and
	// package-lock.json, the pnpm lockfile is the modern one — prefer it.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, lf := range []string{"pnpm-lock.yaml", "package-lock.json"} {
		if err := os.WriteFile(filepath.Join(dir, lf), []byte(""), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bin, _ := jsPackageManager(dir)
	if bin != "pnpm" {
		t.Errorf("expected pnpm to win over npm lockfile, got %q", bin)
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
