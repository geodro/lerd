package config

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestIdleSuspendTimeout_default(t *testing.T) {
	if got := (&GlobalConfig{}).IdleSuspendTimeout(); got != DefaultIdleSuspendTimeout {
		t.Errorf("unset timeout = %v, want default %v", got, DefaultIdleSuspendTimeout)
	}
}

func TestIdleSuspendTimeout_set(t *testing.T) {
	g := &GlobalConfig{}
	g.IdleSuspend.Timeout = "15m"
	if got := g.IdleSuspendTimeout(); got != 15*time.Minute {
		t.Errorf("timeout = %v, want 15m", got)
	}
}

func TestIdleSuspendTimeout_badFallsBack(t *testing.T) {
	g := &GlobalConfig{}
	g.IdleSuspend.Timeout = "not-a-duration"
	if got := g.IdleSuspendTimeout(); got != DefaultIdleSuspendTimeout {
		t.Errorf("bad timeout = %v, want default", got)
	}
	g.IdleSuspend.Timeout = "0s"
	if got := g.IdleSuspendTimeout(); got != DefaultIdleSuspendTimeout {
		t.Errorf("zero timeout = %v, want default", got)
	}
}

// TestSiteIdleSuspendedWorkers_roundtrip pins that the engine's persisted
// suspended-worker list survives a sites.yaml save/load, since lerd-ui relies on
// it to resume the right workers after a restart.
func TestSiteIdleSuspendedWorkers_roundtrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	want := Site{Name: "rt", Path: "/x", PHPVersion: "8.4", IdleSuspendedWorkers: []string{"queue", "horizon"}}
	if err := SaveSites(&SiteRegistry{Sites: []Site{want}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	reg, err := LoadSites()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := reg.Sites[0].IdleSuspendedWorkers
	if len(got) != 2 || got[0] != "queue" || got[1] != "horizon" {
		t.Errorf("IdleSuspendedWorkers = %v, want [queue horizon]", got)
	}
}

// TestSetSiteIdleSuspendedWorkers_preservesOtherFields pins that the atomic
// single-field setter doesn't clobber unrelated fields, the way a stale
// FindSite -> mutate -> AddSite full-record write could.
func TestSetSiteIdleSuspendedWorkers_preservesOtherFields(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	seed := Site{Name: "a", Path: "/x", PHPVersion: "8.4", Paused: true, Pinned: true}
	if err := SaveSites(&SiteRegistry{Sites: []Site{seed}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := SetSiteIdleSuspendedWorkers("a", []string{"queue"}); err != nil {
		t.Fatalf("set: %v", err)
	}
	reg, err := LoadSites()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	s := reg.Sites[0]
	if !s.Paused || !s.Pinned {
		t.Errorf("Paused=%v Pinned=%v, want both preserved", s.Paused, s.Pinned)
	}
	if len(s.IdleSuspendedWorkers) != 1 || s.IdleSuspendedWorkers[0] != "queue" {
		t.Errorf("IdleSuspendedWorkers=%v, want [queue]", s.IdleSuspendedWorkers)
	}
}

// TestConcurrentAddSite_noLostUpdate guards the write-mutex fix: many goroutines
// each adding a distinct site must all land, with no interleaved read-modify-write
// silently dropping one (the race that let one site's worker list bleed onto
// another). Run with -race to also catch data races on the shared registry.
func TestConcurrentAddSite_noLostUpdate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	if err := SaveSites(&SiteRegistry{}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	const n = 24
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = AddSite(Site{Name: fmt.Sprintf("s%02d", i), Path: "/x", PHPVersion: "8.4"})
		}(i)
	}
	wg.Wait()

	reg, err := LoadSites()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(reg.Sites) != n {
		t.Errorf("got %d sites, want %d: concurrent AddSite lost updates", len(reg.Sites), n)
	}
}

// TestSetWorktreeIdleSuspendedWorkers covers the per-worktree persistence: set,
// add a second worktree, clear one, and clearing the last drops the map so a
// fully-resumed site leaves no residue. The main-site list must be untouched.
func TestSetWorktreeIdleSuspendedWorkers(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	seed := Site{Name: "app", Path: "/x", PHPVersion: "8.4", IdleSuspendedWorkers: []string{"queue"}}
	if err := SaveSites(&SiteRegistry{Sites: []Site{seed}}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := SetWorktreeIdleSuspendedWorkers("app", "feature-x", []string{"vite"}); err != nil {
		t.Fatalf("set feature-x: %v", err)
	}
	if err := SetWorktreeIdleSuspendedWorkers("app", "bugfix", []string{"vite"}); err != nil {
		t.Fatalf("set bugfix: %v", err)
	}
	reg, _ := LoadSites()
	got := reg.Sites[0].WorktreeIdleSuspended
	if len(got) != 2 || got["feature-x"][0] != "vite" || got["bugfix"][0] != "vite" {
		t.Fatalf("after two sets = %v, want feature-x and bugfix both [vite]", got)
	}
	if len(reg.Sites[0].IdleSuspendedWorkers) != 1 || reg.Sites[0].IdleSuspendedWorkers[0] != "queue" {
		t.Errorf("main IdleSuspendedWorkers clobbered = %v", reg.Sites[0].IdleSuspendedWorkers)
	}

	if err := SetWorktreeIdleSuspendedWorkers("app", "feature-x", nil); err != nil {
		t.Fatalf("clear feature-x: %v", err)
	}
	reg, _ = LoadSites()
	if got := reg.Sites[0].WorktreeIdleSuspended; len(got) != 1 || got["feature-x"] != nil {
		t.Fatalf("after clearing feature-x = %v, want only bugfix", got)
	}

	if err := SetWorktreeIdleSuspendedWorkers("app", "bugfix", nil); err != nil {
		t.Fatalf("clear bugfix: %v", err)
	}
	reg, _ = LoadSites()
	if reg.Sites[0].WorktreeIdleSuspended != nil {
		t.Errorf("clearing last worktree should drop the map, got %v", reg.Sites[0].WorktreeIdleSuspended)
	}
}

func TestCloneSiteRegistry_worktreeIdleSuspendedDeepCopy(t *testing.T) {
	reg := &SiteRegistry{Sites: []Site{{
		Name:                  "a",
		WorktreeIdleSuspended: map[string][]string{"feat": {"vite"}},
	}}}
	cl := cloneSiteRegistry(reg)
	cl.Sites[0].WorktreeIdleSuspended["feat"][0] = "mutated"
	cl.Sites[0].WorktreeIdleSuspended["new"] = []string{"x"}
	if reg.Sites[0].WorktreeIdleSuspended["feat"][0] != "vite" {
		t.Error("clone shares the worktree worker slice with the original")
	}
	if _, ok := reg.Sites[0].WorktreeIdleSuspended["new"]; ok {
		t.Error("clone shares the worktree map with the original")
	}
}

func TestCloneSiteRegistry_idleSuspendedWorkersDeepCopy(t *testing.T) {
	reg := &SiteRegistry{Sites: []Site{{Name: "a", IdleSuspendedWorkers: []string{"queue"}}}}
	cl := cloneSiteRegistry(reg)
	cl.Sites[0].IdleSuspendedWorkers[0] = "mutated"
	if reg.Sites[0].IdleSuspendedWorkers[0] != "queue" {
		t.Error("clone shares IdleSuspendedWorkers slice with original")
	}
}
