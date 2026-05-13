//go:build darwin

package watcher

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// TestExpectedExecWorkers_filtersUnsupportedShapes pins the
// platform-gate filter so the watcher doesn't burn cooldown windows
// trying to heal worker shapes the macOS path can't run. The darwin
// gate rejects Schedule != "" (launchd StartCalendarInterval isn't
// wired through services.Mgr yet); host:true is now supported.
//
// Without the filter the heal loop logs "self-healing exec-mode
// worker" + the WARN from WorkerSupportedOnPlatform every 2 minutes
// for the same unsupported unit.
func TestExpectedExecWorkers_filtersUnsupportedShapes(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))

	sitePath := filepath.Join(tmp, "acme")
	if err := os.MkdirAll(sitePath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sitePath, ".lerd.yaml"), []byte(
		"framework: laravel\nworkers:\n  - vite\n  - schedule\n  - queue\n",
	), 0644); err != nil {
		t.Fatal(err)
	}

	if err := config.AddSite(config.Site{
		Name: "acme", Domains: []string{"acme.test"},
		Path: sitePath, Framework: "laravel", PHPVersion: "8.4",
	}); err != nil {
		t.Fatal(err)
	}

	// Inject synthetic worker definitions via the project's
	// CustomWorkers so the test doesn't depend on the live laravel
	// framework yaml. tr is a pointer for the *bool PerWorktree field.
	proj, _ := config.LoadProjectConfig(sitePath)
	tr := true
	proj.CustomWorkers = map[string]config.FrameworkWorker{
		"vite":     {Command: "npm run dev", Host: true, PerWorktree: &tr},
		"schedule": {Command: "php artisan schedule:run", Schedule: "minutely"},
		"queue":    {Command: "php artisan queue:work"},
	}
	if err := config.SaveProjectConfig(sitePath, proj); err != nil {
		t.Fatal(err)
	}

	expected := expectedExecWorkers()

	hasUnit := func(unit string) bool {
		for _, w := range expected {
			if w.unit == unit {
				return true
			}
		}
		return false
	}
	if !hasUnit("lerd-vite-acme") {
		t.Errorf("expected vite (host:true, supported) to be enumerated; got %+v", units(expected))
	}
	if !hasUnit("lerd-queue-acme") {
		t.Errorf("expected queue (plain exec-mode worker) to be enumerated; got %+v", units(expected))
	}
	if hasUnit("lerd-schedule-acme") {
		t.Errorf("expected schedule (Schedule != \"\") to be filtered by WorkerSupportedOnPlatform; got %+v", units(expected))
	}
}

func units(ws []expectedExecWorker) []string {
	out := make([]string, 0, len(ws))
	for _, w := range ws {
		out = append(out, w.unit)
	}
	return out
}

// Locks in the LaunchAgents file-name convention: workerNeedsHealing must
// read `~/Library/LaunchAgents/<unit>.plist`, NOT `lerd.<unit>.plist`. The
// `com.lerd.` prefix lives only on the launchd Label inside the plist (see
// services.plistPath / plistLabel). The earlier prefixed form mistook every
// healthy worker for a missing plist, so the heal loop restarted them each
// cooldown.
func TestWorkerNeedsHealing_PlistFileName(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	laDir := filepath.Join(tmp, "Library", "LaunchAgents")
	if err := os.MkdirAll(laDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const unit = "lerd-horizon-acme"

	if got := workerNeedsHealing(unit); got != "plist missing" {
		t.Fatalf("no plist present: got %q, want \"plist missing\"", got)
	}

	// Old buggy form only — the function must NOT match this, otherwise the
	// path-prefix regression slips back in unnoticed.
	legacy := filepath.Join(laDir, "lerd."+unit+".plist")
	if err := os.WriteFile(legacy, []byte("<plist/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := workerNeedsHealing(unit); got != "plist missing" {
		t.Fatalf("legacy lerd.<unit>.plist only: got %q, want \"plist missing\"", got)
	}

	// Correct form. The function should advance past the plist check —
	// launchctl won't have our fake unit registered so we expect either
	// "not loaded in launchd" or "loaded but no live process", but never
	// "plist missing".
	correct := filepath.Join(laDir, unit+".plist")
	if err := os.WriteFile(correct, []byte("<plist/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := workerNeedsHealing(unit); got == "plist missing" {
		t.Fatalf("<unit>.plist present: got \"plist missing\", expected to advance past the plist check")
	}
}

// sweepOrphanWorkerArtifacts must keep .sh / .pid files whose plist still
// exists on disk under the unit-name convention. The earlier `lerd.`+unit
// path looked for a file that never existed, so the sweep happily deleted
// guard scripts for healthy workers mid-launch.
func TestSweepOrphanWorkerArtifacts_KeepsArtifactsWhenPlistPresent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))

	workersDir := filepath.Join(config.RunDir(), "workers")
	if err := os.MkdirAll(workersDir, 0o755); err != nil {
		t.Fatal(err)
	}
	laDir := filepath.Join(tmp, "Library", "LaunchAgents")
	if err := os.MkdirAll(laDir, 0o755); err != nil {
		t.Fatal(err)
	}

	const unit = "lerd-horizon-acme"
	shPath := filepath.Join(workersDir, unit+".sh")
	pidPath := filepath.Join(workersDir, unit+".pid")
	for _, p := range []string{shPath, pidPath} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(laDir, unit+".plist"), []byte("<plist/>"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Empty expected set + plist present → both artifacts must survive.
	sweepOrphanWorkerArtifacts(map[string]bool{})
	for _, p := range []string{shPath, pidPath} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("artifact %q deleted despite plist present: %v", filepath.Base(p), err)
		}
	}

	// Plist gone → with empty expected set, both artifacts should be swept.
	if err := os.Remove(filepath.Join(laDir, unit+".plist")); err != nil {
		t.Fatal(err)
	}
	sweepOrphanWorkerArtifacts(map[string]bool{})
	for _, p := range []string{shPath, pidPath} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("artifact %q survived sweep with plist absent and unit not expected: stat err = %v", filepath.Base(p), err)
		}
	}
}
