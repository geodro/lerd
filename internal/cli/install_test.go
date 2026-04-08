package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddShellShims_LaravelShim(t *testing.T) {
	// Stub out installCompletionFn to avoid spawning the test binary, which
	// would re-run all tests and cause infinite recursion.
	orig := installCompletionFn
	installCompletionFn = func(_, _, _, _ string) {}
	t.Cleanup(func() { installCompletionFn = orig })

	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("HOME", tmp)
	// Clear COMPOSER_HOME so the default path is used.
	t.Setenv("COMPOSER_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	binDir := filepath.Join(tmp, "lerd", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}

	// addShellShims expects a shell env var for the PATH block.
	t.Setenv("SHELL", "/bin/sh")

	if err := addShellShims(); err != nil {
		t.Fatalf("addShellShims: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(binDir, "laravel"))
	if err != nil {
		t.Fatalf("laravel shim not created: %v", err)
	}

	shim := string(data)
	if !strings.HasPrefix(shim, "#!/bin/sh\n") {
		t.Errorf("laravel shim missing shebang, got: %q", shim)
	}
	expectedComposerHome := filepath.Join(tmp, ".config", "composer")
	expectedPath := expectedComposerHome + "/vendor/bin/laravel"
	if !strings.Contains(shim, expectedPath) {
		t.Errorf("laravel shim does not reference %q, got:\n%s", expectedPath, shim)
	}
}

func TestAddShellShims_LaravelShimRespectsComposerHome(t *testing.T) {
	orig := installCompletionFn
	installCompletionFn = func(_, _, _, _ string) {}
	t.Cleanup(func() { installCompletionFn = orig })

	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("HOME", tmp)

	customHome := filepath.Join(tmp, "custom-composer")
	t.Setenv("COMPOSER_HOME", customHome)

	binDir := filepath.Join(tmp, "lerd", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SHELL", "/bin/sh")

	if err := addShellShims(); err != nil {
		t.Fatalf("addShellShims: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(binDir, "laravel"))
	if err != nil {
		t.Fatalf("laravel shim not created: %v", err)
	}

	shim := string(data)
	expectedPath := customHome + "/vendor/bin/laravel"
	if !strings.Contains(shim, expectedPath) {
		t.Errorf("laravel shim should use COMPOSER_HOME=%q, got:\n%s", customHome, shim)
	}
}
