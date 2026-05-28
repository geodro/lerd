package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// isolateLerdHome points both XDG roots at temp dirs so the command's
// LoadCustomService / MaterializeServiceTuning never touch the real home.
func isolateLerdHome(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
}

// fakeQuadletOnDisk satisfies serviceops.ServiceInstalled (which checks for
// lerd-<name>.container in QuadletDir) without spinning up podman.
func fakeQuadletOnDisk(t *testing.T, name string) {
	t.Helper()
	dir := config.QuadletDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir quadlet dir: %v", err)
	}
	path := filepath.Join(dir, "lerd-"+name+".container")
	if err := os.WriteFile(path, []byte("[Container]\n"), 0o644); err != nil {
		t.Fatalf("write fake quadlet: %v", err)
	}
}

func runServiceConfig(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newServiceConfigCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestServiceConfig_PathFlagSeedsAndPrints(t *testing.T) {
	isolateLerdHome(t)
	if err := config.SaveCustomService(&config.CustomService{
		Name:   "mariadb-10-11",
		Image:  "docker.io/library/mariadb:10.11",
		Family: "mariadb",
	}); err != nil {
		t.Fatalf("SaveCustomService: %v", err)
	}
	fakeQuadletOnDisk(t, "mariadb-10-11")

	out, err := runServiceConfig(t, "mariadb-10-11", "--path")
	if err != nil {
		t.Fatalf("config --path: %v", err)
	}
	want := config.ServiceTuningFile("mariadb-10-11")
	if strings.TrimSpace(out) != want {
		t.Errorf("printed path = %q, want %q", strings.TrimSpace(out), want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("tuning file should be materialized by --path, stat err = %v", err)
	}
}

func TestServiceConfig_RejectsUntunedFamily(t *testing.T) {
	isolateLerdHome(t)
	if err := config.SaveCustomService(&config.CustomService{
		Name:   "meilisearch",
		Image:  "docker.io/getmeili/meilisearch:v1",
		Family: "meilisearch",
	}); err != nil {
		t.Fatalf("SaveCustomService: %v", err)
	}
	fakeQuadletOnDisk(t, "meilisearch")

	_, err := runServiceConfig(t, "meilisearch", "--path")
	if err == nil || !strings.Contains(err.Error(), "does not support tuning") {
		t.Errorf("expected untuned-family error, got: %v", err)
	}
}

func TestServiceConfig_RejectsUninstalledService(t *testing.T) {
	isolateLerdHome(t)
	_, err := runServiceConfig(t, "ghost", "--path")
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("expected not-installed error, got: %v", err)
	}
}

// TestServiceConfig_RejectsRemovedDefaultPreset covers the regression dev
// flagged: ResolveServiceForTuning succeeds for built-in default presets even
// when the user has explicitly `lerd service remove`d them (no quadlet on
// disk), which would otherwise let `service config` silently reinstall the
// service via EnsureDefaultPresetQuadlet + RestartUnit as a side effect of an
// edit. The install-presence guard must block this path.
func TestServiceConfig_RejectsRemovedDefaultPreset(t *testing.T) {
	isolateLerdHome(t)
	// "mysql" is a default preset (resolves via LoadPreset fallback) but no
	// quadlet on disk — i.e. the post-`service remove` state.
	_, err := runServiceConfig(t, "mysql", "--path")
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("expected not-installed error for removed default preset, got: %v", err)
	}
	if err != nil && !strings.Contains(err.Error(), "lerd service preset install mysql") {
		t.Errorf("expected install hint in error, got: %v", err)
	}
}
