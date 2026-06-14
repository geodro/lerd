package cli

import (
	"testing"
	"time"

	"github.com/geodro/lerd/internal/config"
)

func TestIdleTimingStatus(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	timeout := time.Hour
	cases := []struct {
		name      string
		lastUnix  int64
		suspended []string
		want      string
	}{
		{"suspended reads idle", now.Add(-5 * time.Minute).Unix(), []string{"vite"}, "idle 5m"},
		{"recent reads active", now.Add(-5 * time.Minute).Unix(), nil, "active 5m ago"},
		{"past timeout reads idle", now.Add(-2 * time.Hour).Unix(), nil, "idle 2h"},
		{"no record", 0, nil, "no activity yet"},
	}
	for _, c := range cases {
		if got := idleTimingStatus(c.lastUnix, c.suspended, timeout, now); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestIdleWorktreeStatus_inheritsSitePauseAndPin(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	wt := idleWtState{Branch: "dev", LastActive: now.Unix()}
	if got := idleWorktreeStatus(config.Site{Paused: true}, wt, nil, time.Hour, now); got != "paused" {
		t.Errorf("paused site worktree = %q, want paused", got)
	}
	if got := idleWorktreeStatus(config.Site{Pinned: true}, wt, nil, time.Hour, now); got != "pinned" {
		t.Errorf("pinned site worktree = %q, want pinned", got)
	}
	if got := idleWorktreeStatus(config.Site{}, wt, nil, time.Hour, now); got != "active 0s ago" {
		t.Errorf("active worktree = %q, want active 0s ago", got)
	}
}

// TestWorktreeWorkersToStart_skipsSuspended guards the fix for a suspended
// worktree worker being resurrected by the boot/onAdded autostart scan: a worker
// the engine recorded under worktree_idle_suspended must be filtered out so it
// stays asleep until activity resumes it through the engine.
func TestWorktreeWorkersToStart_skipsSuspended(t *testing.T) {
	site := &config.Site{
		Name: "rental",
		WorktreeIdleSuspended: map[string][]string{
			"rental-main": {"vite"},
		},
	}
	got := worktreeWorkersToStart(site, "rental-main", []string{"vite", "queue"})
	if len(got) != 1 || got[0] != "queue" {
		t.Errorf("worktreeWorkersToStart = %v, want [queue]", got)
	}

	// A worktree with nothing suspended passes the list through untouched.
	all := worktreeWorkersToStart(site, "other", []string{"vite", "queue"})
	if len(all) != 2 {
		t.Errorf("unsuspended worktree = %v, want both workers", all)
	}
}

// TestEnsureViteSleepable_buildsAtMostOnce guards against the engine re-running
// `npm run build` on every tick for a checkout whose build never produces a
// manifest (a common worktree case), which would otherwise thrash CPU forever.
func TestEnsureViteSleepable_buildsAtMostOnce(t *testing.T) {
	dir := t.TempDir() // no built manifest here, so the build is "attempted"
	site := &config.Site{Name: "x", Path: dir}

	var builds int
	prev := runViteBuildAt
	runViteBuildAt = func(*config.Site, string) { builds++ } // never writes a manifest
	t.Cleanup(func() { runViteBuildAt = prev })

	if ensureViteSleepableAt(site, dir) {
		t.Fatal("expected not sleepable without a manifest")
	}
	ensureViteSleepableAt(site, dir) // second tick must not rebuild
	if builds != 1 {
		t.Errorf("build ran %d times, want 1 (memoized after first attempt)", builds)
	}
}

// TestWorktreeWorkerUnitNaming pins that the unit name collectRunningWorktreeWorkers
// checks (lerd-<w>-<site>-<wtBase>) is exactly what workerNames produces for a
// worktree checkout, so idle-suspend detects, stops, and restarts the same unit.
func TestWorktreeWorkerUnitNaming(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	if err := config.AddSite(config.Site{Name: "myapp", Path: "/srv/myapp", PHPVersion: "8.4"}); err != nil {
		t.Fatalf("seed site: %v", err)
	}

	// A worktree checkout path differs from the main path, so the unit gets the
	// worktree-base suffix.
	unit, _ := workerNames("myapp", "/srv/myapp/.worktrees/feature-x", "vite")
	if unit != "lerd-vite-myapp-feature-x" {
		t.Errorf("worktree unit = %q, want lerd-vite-myapp-feature-x", unit)
	}

	// The main checkout keeps the plain unit name.
	mainUnit, _ := workerNames("myapp", "/srv/myapp", "vite")
	if mainUnit != "lerd-vite-myapp" {
		t.Errorf("main unit = %q, want lerd-vite-myapp", mainUnit)
	}
}
