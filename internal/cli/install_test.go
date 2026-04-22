package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
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

func TestRefreshUnreferencedCustomQuadlets_globalCustomServiceGetsV6Pair(t *testing.T) {
	// Simulates a preset like mongo-express installed globally via
	// `lerd service preset install`: a yaml in CustomServicesDir() with
	// loopback publish ports, but no site .lerd.yaml references it.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))

	origReload := podman.DaemonReloadFn
	t.Cleanup(func() { podman.DaemonReloadFn = origReload })
	podman.DaemonReloadFn = func() error { return nil }

	svc := &config.CustomService{
		Name:  "mongo-express",
		Image: "docker.io/library/mongo-express:latest",
		Ports: []string{"127.0.0.1:8082:8081"},
	}
	if err := config.SaveCustomService(svc); err != nil {
		t.Fatalf("SaveCustomService: %v", err)
	}

	refreshUnreferencedCustomQuadlets(map[string]bool{}, nil)

	path := filepath.Join(config.QuadletDir(), "lerd-mongo-express.container")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("quadlet not written at %s: %v", path, err)
	}
	got := string(data)
	if !strings.Contains(got, "PublishPort=127.0.0.1:8082:8081") {
		t.Errorf("v4 bind missing from rewritten quadlet:\n%s", got)
	}
	if !strings.Contains(got, "PublishPort=[::1]:8082:8081") {
		t.Errorf("v6 pair missing — PairIPv6Binds did not apply during refresh:\n%s", got)
	}
}

func TestRefreshUnreferencedCustomQuadlets_skipsSeenServices(t *testing.T) {
	// If the per-site loop already handled a service, the second pass must
	// not overwrite its quadlet, so an overridden image or extra port is
	// preserved.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))

	origReload := podman.DaemonReloadFn
	t.Cleanup(func() { podman.DaemonReloadFn = origReload })
	podman.DaemonReloadFn = func() error { return nil }

	svc := &config.CustomService{
		Name:  "mongo-express",
		Image: "docker.io/library/mongo-express:latest",
		Ports: []string{"127.0.0.1:8082:8081"},
	}
	if err := config.SaveCustomService(svc); err != nil {
		t.Fatalf("SaveCustomService: %v", err)
	}

	seen := map[string]bool{"mongo-express": true}
	refreshUnreferencedCustomQuadlets(seen, nil)

	path := filepath.Join(config.QuadletDir(), "lerd-mongo-express.container")
	if _, err := os.Stat(path); err == nil {
		t.Errorf("quadlet at %s should not be written when service is already in seenSvc", path)
	}
}

func TestRefreshUnreferencedCustomQuadlets_rewritesCustomContainerSite(t *testing.T) {
	// Custom-container sites (ContainerPort > 0) don't publish ports, so
	// PairIPv6Binds is a no-op. The refresh must still write a fresh
	// quadlet on disk so a migrated container restarts from current state.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))

	projectDir := filepath.Join(tmp, "my-app")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}

	reg := &config.SiteRegistry{
		Sites: []config.Site{
			{
				Name:          "my-app",
				Domains:       []string{"my-app.test"},
				Path:          projectDir,
				ContainerPort: 3000,
			},
		},
	}

	refreshUnreferencedCustomQuadlets(map[string]bool{}, reg)

	path := filepath.Join(config.QuadletDir(), "lerd-custom-my-app.container")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("custom-container quadlet not written at %s: %v", path, err)
	}
	got := string(data)
	if !strings.Contains(got, "Network=lerd") {
		t.Errorf("expected Network=lerd in custom-container quadlet:\n%s", got)
	}
	if !strings.Contains(got, "ContainerName=lerd-custom-my-app") {
		t.Errorf("expected ContainerName=lerd-custom-my-app:\n%s", got)
	}
}

func TestRefreshUnreferencedCustomQuadlets_rewritesFrankenPHPSite(t *testing.T) {
	// FrankenPHP sites (Runtime=="frankenphp") don't publish ports either, but
	// the per-site loop that generates vhosts does not rewrite their quadlets.
	// The refresh pass must emit a fresh lerd-fp-<name>.container on disk so
	// the v4→v6 network migration (and any other quadlet-schema change) lands
	// on the FrankenPHP container when install restarts it.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))

	projectDir := filepath.Join(tmp, "my-app")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}

	reg := &config.SiteRegistry{
		Sites: []config.Site{
			{
				Name:       "my-app",
				Domains:    []string{"my-app.test"},
				Path:       projectDir,
				PHPVersion: "8.4",
				Runtime:    "frankenphp",
			},
		},
	}

	refreshUnreferencedCustomQuadlets(map[string]bool{}, reg)

	path := filepath.Join(config.QuadletDir(), "lerd-fp-my-app.container")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("frankenphp quadlet not written at %s: %v", path, err)
	}
	got := string(data)
	wantFragments := []string{
		"ContainerName=lerd-fp-my-app",
		"Image=docker.io/dunglas/frankenphp:php8.4-alpine",
		"Network=lerd",
		"Volume=" + projectDir + ":" + projectDir + ":rw",
	}
	for _, s := range wantFragments {
		if !strings.Contains(got, s) {
			t.Errorf("quadlet missing %q:\n%s", s, got)
		}
	}
}

func TestRefreshUnreferencedCustomQuadlets_skipsPausedAndIgnoredSites(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))

	reg := &config.SiteRegistry{
		Sites: []config.Site{
			{Name: "paused-app", Path: tmp, ContainerPort: 3001, Paused: true},
			{Name: "ignored-app", Path: tmp, ContainerPort: 3002, Ignored: true},
		},
	}
	refreshUnreferencedCustomQuadlets(map[string]bool{}, reg)

	for _, name := range []string{"lerd-custom-paused-app", "lerd-custom-ignored-app"} {
		path := filepath.Join(config.QuadletDir(), name+".container")
		if _, err := os.Stat(path); err == nil {
			t.Errorf("quadlet %s should not be written for paused/ignored site", path)
		}
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
