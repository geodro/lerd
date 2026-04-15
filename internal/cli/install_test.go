package cli

import (
	"os"
	"path/filepath"
	"runtime"
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

func TestEnsurePortForwarding(t *testing.T) {
	// Should not error on any platform
	if err := ensurePortForwarding(); err != nil {
		t.Errorf("ensurePortForwarding error: %v", err)
	}
}

func TestNeedsDNSServiceInstall(t *testing.T) {
	if runtime.GOOS == "linux" {
		if needsDNSServiceInstall() {
			t.Error("needsDNSServiceInstall should return false on linux")
		}
	}
	// On macOS the result depends on whether plists exist — skip assertion
}

func TestIsDNSContainerUnit(t *testing.T) {
	if runtime.GOOS == "linux" {
		if !isDNSContainerUnit() {
			t.Error("isDNSContainerUnit should return true on linux")
		}
	} else {
		if isDNSContainerUnit() {
			t.Error("isDNSContainerUnit should return false on macOS")
		}
	}
}

func TestPullDNSImages(t *testing.T) {
	jobs := pullDNSImages()
	if runtime.GOOS == "linux" {
		if len(jobs) == 0 {
			t.Error("pullDNSImages should return build jobs on linux")
		}
	} else {
		if len(jobs) != 0 {
			t.Error("pullDNSImages should return nil on macOS")
		}
	}
}

func TestInstallAutostart(t *testing.T) {
	installAutostart()
}

func TestInstallCleanupScript(t *testing.T) {
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

	if err := addShellShims(false); err != nil {
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

	if err := addShellShims(false); err != nil {
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
