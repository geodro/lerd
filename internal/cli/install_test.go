package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsShell_fish(t *testing.T) {
	if !isShell("/usr/bin/fish", "fish") {
		t.Error("expected /usr/bin/fish to match fish")
	}
}

func TestIsShell_zsh(t *testing.T) {
	if !isShell("/bin/zsh", "zsh") {
		t.Error("expected /bin/zsh to match zsh")
	}
}

func TestIsShell_mismatch(t *testing.T) {
	if isShell("/bin/bash", "zsh") {
		t.Error("expected /bin/bash not to match zsh")
	}
}

func TestIsShell_empty(t *testing.T) {
	if isShell("", "bash") {
		t.Error("expected empty shell not to match")
	}
}

func TestEnsurePortForwarding_linux(t *testing.T) {
	// On Linux, ensurePortForwarding is a no-op
	if err := ensurePortForwarding(); err != nil {
		t.Errorf("ensurePortForwarding should be nil on linux, got: %v", err)
	}
}

func TestNeedsDNSServiceInstall_linux(t *testing.T) {
	if needsDNSServiceInstall() {
		t.Error("needsDNSServiceInstall should return false on linux")
	}
}

func TestIsDNSContainerUnit_linux(t *testing.T) {
	if !isDNSContainerUnit() {
		t.Error("isDNSContainerUnit should return true on linux")
	}
}

func TestPullDNSImages_linux(t *testing.T) {
	jobs := pullDNSImages()
	if len(jobs) == 0 {
		t.Error("pullDNSImages should return build jobs on linux")
	}
}

func TestInstallAutostart_linux(t *testing.T) {
	// On Linux, installAutostart is a no-op — should not panic
	installAutostart()
}

func TestInstallCleanupScript_linux(t *testing.T) {
	// On Linux, installCleanupScript is a no-op — should not panic
	installCleanupScript()
}

func TestAddShellShims_LaravelShim(t *testing.T) {
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
