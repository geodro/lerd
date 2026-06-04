package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// horizonWorker mirrors the framework's horizon definition: the standard
// command plus the reload variant core selects when a project opts in.
var horizonWorker = config.FrameworkWorker{
	Command:       "php artisan horizon",
	ReloadCommand: "php artisan horizon:listen",
}

// siteWithReload returns a temp site dir seeded with a .lerd.yaml. When
// reloadOn is true the horizon worker is opted into auto-reload; when
// withChokidar is true node_modules/chokidar is created so the watcher
// prerequisite is satisfied. HOME and the XDG roots are pointed at a temp dir
// so nothing reads or writes the developer's real machine.
func siteWithReload(t *testing.T, reloadOn, withChokidar bool) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	site := t.TempDir()
	cfg := &config.ProjectConfig{}
	if reloadOn {
		cfg.ReloadWorkers = []string{"horizon"}
	}
	if err := config.SaveProjectConfig(site, cfg); err != nil {
		t.Fatalf("write .lerd.yaml: %v", err)
	}
	if withChokidar {
		if err := os.MkdirAll(filepath.Join(site, "node_modules", "chokidar"), 0o755); err != nil {
			t.Fatalf("seed chokidar: %v", err)
		}
	}
	return site
}

// withPoll appends the polling flag the way resolveWorkerCommand does where the
// container cannot observe host filesystem events (macOS, or WSL2 under /mnt).
// On native Linux CI this is a no-op, matching the resolved command.
func withPoll(sitePath, cmd string) string {
	if watcherNeedsPolling(sitePath) {
		return cmd + " --poll"
	}
	return cmd
}

func TestResolveWorkerCommand(t *testing.T) {
	t.Run("reload off keeps the standard command", func(t *testing.T) {
		site := siteWithReload(t, false, true)
		if got := resolveWorkerCommand(site, "horizon", horizonWorker); got != horizonWorker.Command {
			t.Errorf("got %q, want %q", got, horizonWorker.Command)
		}
	})

	t.Run("reload on with chokidar selects the reload command", func(t *testing.T) {
		site := siteWithReload(t, true, true)
		want := withPoll(site, "php artisan horizon:listen")
		if got := resolveWorkerCommand(site, "horizon", horizonWorker); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("reload on without chokidar falls back to the standard command", func(t *testing.T) {
		site := siteWithReload(t, true, false)
		if got := resolveWorkerCommand(site, "horizon", horizonWorker); got != horizonWorker.Command {
			t.Errorf("got %q, want %q (should fall back when chokidar missing)", got, horizonWorker.Command)
		}
	})

	t.Run("workers without a reload command are never rewritten", func(t *testing.T) {
		site := siteWithReload(t, true, true)
		queue := config.FrameworkWorker{Command: "php artisan queue:work --queue=default"}
		if got := resolveWorkerCommand(site, "queue", queue); got != queue.Command {
			t.Errorf("got %q, want %q", got, queue.Command)
		}
	})
}

func TestWatcherNeedsPolling_WSLMntGate(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("macOS always polls regardless of path")
	}
	// Force the WSL fast-path so the test exercises the /mnt gate, not the
	// host's real environment.
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	if !watcherNeedsPolling("/mnt/c/code/app") {
		t.Error("a WSL project under /mnt (9p) should poll")
	}
	if watcherNeedsPolling("/home/user/code/app") {
		t.Error("a WSL project on the native filesystem should not poll")
	}
}

func TestApplyHorizonReload_ChokidarGate(t *testing.T) {
	t.Run("enabling without chokidar refuses and does not persist", func(t *testing.T) {
		site := siteWithReload(t, false, false)
		err := ApplyHorizonReload("nosite", site, "8.3", true)
		if err == nil {
			t.Fatal("expected an error when enabling reload without chokidar")
		}
		if !strings.Contains(err.Error(), "chokidar") {
			t.Errorf("error %q should mention chokidar", err)
		}
		if config.ProjectReloadsWorker(site, "horizon") {
			t.Error("preference must not be persisted when the prerequisite is missing")
		}
	})

	t.Run("enabling with chokidar persists the preference", func(t *testing.T) {
		site := siteWithReload(t, false, true)
		if err := ApplyHorizonReload("nosite", site, "8.3", true); err != nil {
			t.Fatalf("ApplyHorizonReload: %v", err)
		}
		if !config.ProjectReloadsWorker(site, "horizon") {
			t.Error("horizon should be opted into reload after enabling with chokidar")
		}
	})

	t.Run("disabling never requires chokidar", func(t *testing.T) {
		site := siteWithReload(t, true, false)
		if err := ApplyHorizonReload("nosite", site, "8.3", false); err != nil {
			t.Fatalf("ApplyHorizonReload disable: %v", err)
		}
		if config.ProjectReloadsWorker(site, "horizon") {
			t.Error("horizon should be off after disabling")
		}
	})
}
