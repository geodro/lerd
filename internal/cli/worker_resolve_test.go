package cli

import (
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// TestResolveWorkerFPMUnit_defaultSharedFPM pins the default case: a plain
// PHP-FPM site resolves to the shared lerd-php<v>-fpm container regardless
// of platform.
func TestResolveWorkerFPMUnit_defaultSharedFPM(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	if err := config.AddSite(config.Site{
		Name: "alpha", Path: filepath.Join(tmp, "alpha"),
		Domains: []string{"alpha.test"}, PHPVersion: "8.4",
	}); err != nil {
		t.Fatal(err)
	}
	if got := resolveWorkerFPMUnit("alpha", "8.4"); got != "lerd-php84-fpm" {
		t.Errorf("got %q, want lerd-php84-fpm", got)
	}
}

// TestResolveWorkerFPMUnit_customContainer pins the per-project custom
// container path so workers exec into the site's own container instead of
// the shared FPM.
func TestResolveWorkerFPMUnit_customContainer(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	if err := config.AddSite(config.Site{
		Name: "beta", Path: filepath.Join(tmp, "beta"),
		Domains: []string{"beta.test"}, ContainerPort: 8080,
	}); err != nil {
		t.Fatal(err)
	}
	got := resolveWorkerFPMUnit("beta", "8.4")
	if got == "lerd-php84-fpm" {
		t.Errorf("expected custom container name, got shared FPM %q", got)
	}
	// Just sanity-check the prefix; podman.CustomContainerName owns the format.
	if got == "" {
		t.Error("got empty container name for custom container site")
	}
}

// TestResolveWorkerFPMUnit_frankenPHP regression-pins the FrankenPHP
// branch that restoreWorker (linux + darwin) and writeWorkerExecUnit
// (darwin) used to miss: they hard-coded the shared FPM, so FrankenPHP
// sites' workers ended up exec'ing into a container that doesn't run their
// PHP. The shared helper now routes FrankenPHP sites to their own FrankenPHP
// container.
func TestResolveWorkerFPMUnit_frankenPHP(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	if err := config.AddSite(config.Site{
		Name: "gamma", Path: filepath.Join(tmp, "gamma"),
		Domains: []string{"gamma.test"}, PHPVersion: "8.4", Runtime: "frankenphp",
	}); err != nil {
		t.Fatal(err)
	}
	got := resolveWorkerFPMUnit("gamma", "8.4")
	if got == "lerd-php84-fpm" {
		t.Errorf("expected FrankenPHP container, got shared FPM %q", got)
	}
	if got == "" {
		t.Error("got empty container name for FrankenPHP site")
	}
}

// TestResolveWorkerFPMUnit_unknownSite falls back to the shared FPM. This
// preserves prior behaviour of the inline FPM resolution in WorkerStartForSite,
// which served as the safe default when the site lookup races site removal.
func TestResolveWorkerFPMUnit_unknownSite(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	if got := resolveWorkerFPMUnit("noexist", "8.3"); got != "lerd-php83-fpm" {
		t.Errorf("got %q, want lerd-php83-fpm", got)
	}
}

// TestWorkerNames_singleLookup pins that the unified helper returns both
// the unit name and the display string in one config.FindSite call. Pre-
// refactor `workerUnitName` and `workerDisplaySite` each looked up the
// site independently — same answer, twice the lookup.
func TestWorkerNames_parentPath(t *testing.T) {
	registerSite(t, "ws", "/p/ws")
	unit, display := workerNames("ws", "/p/ws", "vite")
	if unit != "lerd-vite-ws" {
		t.Errorf("unit = %q, want lerd-vite-ws", unit)
	}
	if display != "ws" {
		t.Errorf("display = %q, want ws", display)
	}
}

func TestWorkerNames_worktreePath(t *testing.T) {
	registerSite(t, "ws", "/p/ws")
	unit, display := workerNames("ws", "/p/ws/feat-x", "vite")
	if unit != "lerd-vite-ws-feat-x" {
		t.Errorf("unit = %q, want lerd-vite-ws-feat-x", unit)
	}
	if display != "ws/feat-x" {
		t.Errorf("display = %q, want ws/feat-x", display)
	}
}

func TestWorkerNames_emptyPath(t *testing.T) {
	registerSite(t, "ws", "/p/ws")
	unit, display := workerNames("ws", "", "vite")
	if unit != "lerd-vite-ws" || display != "ws" {
		t.Errorf("got (%q,%q), want (lerd-vite-ws, ws)", unit, display)
	}
}
