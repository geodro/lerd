package siteinfo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// TestEnrichWorkers_keepsPerWorktreeOnParentWhenCheckPasses pins that
// per_worktree workers (vite) are kept on the parent row when their
// check rule matches at the parent path. The previous gate that skipped
// IsPerWorktree() unconditionally stripped the toggle from sites without
// worktrees that legitimately ran the worker at the parent, and stripped
// the log tab from parents that did have worktrees.
func TestEnrichWorkers_keepsPerWorktreeOnParentWhenCheckPasses(t *testing.T) {
	origUnit := unitStatusFn
	unitStatusFn = func(name string) (string, error) {
		if name == "lerd-vite-rapids" {
			return "active", nil
		}
		return "inactive", nil
	}
	defer func() { unitStatusFn = origUnit }()

	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "node_modules", "vite"), 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	tr := true
	fw := &config.Framework{
		Workers: map[string]config.FrameworkWorker{
			"vite": {
				Label:       "Vite",
				Command:     "npm run dev",
				PerWorktree: &tr,
				Check:       &config.FrameworkRule{File: "node_modules/vite"},
			},
		},
	}

	e := &EnrichedSite{Name: "rapids", Path: tmp}
	e.enrichWorkers(fw, true)

	var got *WorkerInfo
	for i, w := range e.FrameworkWorkers {
		if w.Name == "vite" {
			got = &e.FrameworkWorkers[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("expected vite on parent FrameworkWorkers, got %+v", e.FrameworkWorkers)
	}
	if !got.Running {
		t.Errorf("parent unit was active, want running=true: %+v", *got)
	}
}

// TestEnrichWorkers_dropsPerWorktreeWhenCheckFails pins that the check
// rule still gates parent visibility: a per_worktree worker whose check
// file is absent at the parent path must not leak into FrameworkWorkers,
// otherwise every Laravel project would show a vite toggle even without
// vite installed.
func TestEnrichWorkers_dropsPerWorktreeWhenCheckFails(t *testing.T) {
	origUnit := unitStatusFn
	unitStatusFn = func(string) (string, error) { return "inactive", nil }
	defer func() { unitStatusFn = origUnit }()

	tr := true
	fw := &config.Framework{
		Workers: map[string]config.FrameworkWorker{
			"vite": {
				Label:       "Vite",
				Command:     "npm run dev",
				PerWorktree: &tr,
				Check:       &config.FrameworkRule{File: "node_modules/vite"},
			},
		},
	}

	e := &EnrichedSite{Name: "rapids", Path: t.TempDir()}
	e.enrichWorkers(fw, true)

	for _, w := range e.FrameworkWorkers {
		if w.Name == "vite" {
			t.Errorf("vite leaked into parent when node_modules/vite was absent: %+v", e.FrameworkWorkers)
		}
	}
}

// TestEnrichWorkers_keepsNonPerWorktreeOnParent makes sure a custom
// non-per-worktree worker that the framework yaml ships (e.g. a
// "search-indexer" daemon) still reports correctly on the parent.
func TestEnrichWorkers_keepsNonPerWorktreeOnParent(t *testing.T) {
	origUnit := unitStatusFn
	unitStatusFn = func(name string) (string, error) {
		if name == "lerd-search-indexer-rapids" {
			return "active", nil
		}
		return "inactive", nil
	}
	defer func() { unitStatusFn = origUnit }()

	fw := &config.Framework{
		Workers: map[string]config.FrameworkWorker{
			"search-indexer": {Label: "Search indexer", Command: "php artisan scout:work"},
		},
	}

	e := &EnrichedSite{Name: "rapids", Path: "/projects/rapids"}
	e.enrichWorkers(fw, true)

	if len(e.FrameworkWorkers) != 1 || e.FrameworkWorkers[0].Name != "search-indexer" {
		t.Fatalf("expected search-indexer on parent, got %+v", e.FrameworkWorkers)
	}
	if !e.FrameworkWorkers[0].Running {
		t.Errorf("parent worker status not propagated, want running=true: %+v", e.FrameworkWorkers[0])
	}
}
