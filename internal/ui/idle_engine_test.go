package ui

import (
	"testing"

	"github.com/geodro/lerd/internal/config"
	gitpkg "github.com/geodro/lerd/internal/git"
	"github.com/geodro/lerd/internal/idle"
)

// TestTick_pinnedSiteStillTicksWorktrees is the regression guard for a pinned
// site stranding its worktrees. Pinning used to `continue` past tickWorktrees, so
// a pinned site's worktree was never re-detected: its domain dropped out of the
// access-feed lookup (no wake) and a suspended worktree was never resumed. The
// tick must still process the worktree, proven here by its domain landing in the
// engine's worktreeKeyByDomain map.
func TestTick_pinnedSiteStillTicksWorktrees(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := config.AddSite(config.Site{
		Name: "myapp", Path: "/srv/myapp", PHPVersion: "8.4",
		Domains: []string{"myapp.test"}, Pinned: true,
	}); err != nil {
		t.Fatalf("seed site: %v", err)
	}

	prev := detectWorktrees
	detectWorktrees = func(string, string) ([]gitpkg.Worktree, error) {
		return []gitpkg.Worktree{{
			Branch: "feature-x", Path: "/srv/myapp/feature-x", Domain: "feature-x.myapp.test",
		}}, nil
	}
	t.Cleanup(func() { detectWorktrees = prev })

	e := newIdleEngine(idle.NewTracker(nil))
	e.tick()

	key := wtKey("myapp", config.WorktreeUnitSlug("feature-x"))
	if got := e.worktreeKeyByDomain["feature-x.myapp.test"]; got != key {
		t.Errorf("pinned site's worktree domain = %q, want %q (worktree was skipped)", got, key)
	}
}

// TestPruneStaleWorktrees clears suspended state for a worktree that no longer
// exists while leaving a still-present one untouched, so a removed worktree stops
// showing as suspended forever.
func TestPruneStaleWorktrees(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := config.AddSite(config.Site{
		Name: "myapp", Path: "/srv/myapp", PHPVersion: "8.4", Domains: []string{"myapp.test"},
		WorktreeIdleSuspended: map[string][]string{"gone": {"vite"}, "alive": {"vite"}},
	}); err != nil {
		t.Fatalf("seed site: %v", err)
	}

	e := newIdleEngine(idle.NewTracker(nil))
	goneKey := wtKey("myapp", "gone")
	aliveKey := wtKey("myapp", "alive")
	e.suspended[goneKey] = true
	e.suspended[aliveKey] = true

	// Only "alive" is detected this tick.
	e.pruneStaleWorktrees("myapp", map[string]bool{aliveKey: true})

	if e.suspended[goneKey] {
		t.Error("deleted worktree should be pruned from the suspended map")
	}
	if !e.suspended[aliveKey] {
		t.Error("still-present worktree must not be pruned")
	}
	reg, err := config.LoadSites()
	if err != nil {
		t.Fatalf("reload sites: %v", err)
	}
	s := reg.Sites[0]
	if _, ok := s.WorktreeIdleSuspended["gone"]; ok {
		t.Error("deleted worktree's persisted suspend slot should be cleared")
	}
	if _, ok := s.WorktreeIdleSuspended["alive"]; !ok {
		t.Error("present worktree's persisted suspend slot must remain")
	}
}

func TestWtKeyRoundTrip(t *testing.T) {
	key := wtKey("myapp", "feature-x")
	if key != "myapp/feature-x" {
		t.Fatalf("wtKey = %q, want myapp/feature-x", key)
	}
	site, wtBase, isWt := splitWtKey(key)
	if !isWt || site != "myapp" || wtBase != "feature-x" {
		t.Errorf("splitWtKey(%q) = (%q, %q, %v), want (myapp, feature-x, true)", key, site, wtBase, isWt)
	}
}

// TestTick_reconcilesStaleSuspendedCache is the regression guard for workers
// staying up on an idle site after an install. The engine boots from the
// persisted idle_suspended_workers list; if an install (re)started the workers it
// cleared that list, but the engine's in-memory cache still said suspended and it
// never re-suspended. The tick must trust the now-empty persisted list and drop
// the stale cache entry.
func TestTick_reconcilesStaleSuspendedCache(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// Persisted list is empty (an install cleared it after starting the workers).
	if err := config.AddSite(config.Site{
		Name: "myapp", Path: "/srv/myapp", PHPVersion: "8.4", Domains: []string{"myapp.test"},
	}); err != nil {
		t.Fatalf("seed site: %v", err)
	}
	prev := detectWorktrees
	detectWorktrees = func(string, string) ([]gitpkg.Worktree, error) { return nil, nil }
	t.Cleanup(func() { detectWorktrees = prev })

	e := newIdleEngine(idle.NewTracker(nil))
	e.suspended["myapp"] = true // stale cache the install couldn't clear

	e.tick()

	if e.suspended["myapp"] {
		t.Error("stale suspended cache should be reconciled to false against the empty persisted list")
	}
}

// The reconcile must be skipped while a suspend/resume goroutine is mid-flight, so
// a slow build isn't second-guessed before it has persisted its result.
func TestTick_reconcileSkippedWhileInFlight(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := config.AddSite(config.Site{
		Name: "myapp", Path: "/srv/myapp", PHPVersion: "8.4", Domains: []string{"myapp.test"},
	}); err != nil {
		t.Fatalf("seed site: %v", err)
	}
	prev := detectWorktrees
	detectWorktrees = func(string, string) ([]gitpkg.Worktree, error) { return nil, nil }
	t.Cleanup(func() { detectWorktrees = prev })

	e := newIdleEngine(idle.NewTracker(nil))
	e.suspended["myapp"] = true
	e.inFlight["myapp"] = true // a suspend goroutine is still running

	e.tick()

	if !e.suspended["myapp"] {
		t.Error("reconcile must not run while the site is in-flight")
	}
}

// A worktree whose persisted slot was cleared (its worker restarted outside the
// engine) must likewise have its stale suspended cache reconciled away.
func TestTickWorktrees_reconcilesStaleSuspendedCache(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := config.AddSite(config.Site{
		Name: "myapp", Path: "/srv/myapp", PHPVersion: "8.4", Domains: []string{"myapp.test"},
	}); err != nil {
		t.Fatalf("seed site: %v", err)
	}
	prev := detectWorktrees
	detectWorktrees = func(string, string) ([]gitpkg.Worktree, error) {
		return []gitpkg.Worktree{{
			Branch: "feature-x", Path: "/srv/myapp/feature-x", Domain: "feature-x.myapp.test",
		}}, nil
	}
	t.Cleanup(func() { detectWorktrees = prev })

	e := newIdleEngine(idle.NewTracker(nil))
	key := wtKey("myapp", config.WorktreeUnitSlug("feature-x"))
	e.suspended[key] = true // stale: persisted WorktreeIdleSuspended has no entry

	e.tick()

	if e.suspended[key] {
		t.Error("stale worktree suspended cache should be reconciled to false")
	}
}

func TestSplitWtKey_mainSite(t *testing.T) {
	site, wtBase, isWt := splitWtKey("myapp")
	if isWt || site != "myapp" || wtBase != "" {
		t.Errorf("splitWtKey(myapp) = (%q, %q, %v), want (myapp, \"\", false)", site, wtBase, isWt)
	}
}
