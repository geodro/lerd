package siteinfo

import (
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func vitePerWT() config.FrameworkWorker {
	tr := true
	return config.FrameworkWorker{Label: "Vite", Command: "npm run dev", PerWorktree: &tr}
}

func TestEnrichWorktreeWorkers_skipsParentOnlyWorkers(t *testing.T) {
	// Default is parent-only. Only workers explicitly opted in with
	// per_worktree:true surface on the worktree row.
	origUnit := unitStatusFn
	unitStatusFn = func(string) (string, error) { return "active", nil }
	defer func() { unitStatusFn = origUnit }()

	fw := &config.Framework{
		Workers: map[string]config.FrameworkWorker{
			"queue":    {Command: "x"},
			"schedule": {Command: "x"},
			"reverb":   {Command: "x"},
			"horizon":  {Command: "x"},
			"vite":     vitePerWT(),
		},
	}
	got := enrichWorktreeWorkers("whitewaters", "/projects/whitewaters/main", fw)
	if len(got) != 1 || got[0].Name != "vite" {
		t.Fatalf("expected only vite worker, got %+v", got)
	}
}

func TestEnrichWorktreeWorkers_unitNamePerWorktree(t *testing.T) {
	origUnit := unitStatusFn
	unitStatusFn = func(name string) (string, error) {
		if name == "lerd-vite-whitewaters-main" {
			return "active", nil
		}
		return "inactive", nil
	}
	defer func() { unitStatusFn = origUnit }()

	fw := &config.Framework{
		Workers: map[string]config.FrameworkWorker{
			"vite": vitePerWT(),
		},
	}
	got := enrichWorktreeWorkers("whitewaters", "/projects/whitewaters/main", fw)
	if len(got) != 1 {
		t.Fatalf("expected 1 worker, got %d", len(got))
	}
	if got[0].Name != "vite" || !got[0].Running || got[0].Failing {
		t.Errorf("vite worker not reported running: %+v", got[0])
	}
	if got[0].Label != "Vite" {
		t.Errorf("label = %q, want Vite", got[0].Label)
	}
}

func TestEnrichWorktreeWorkers_inactiveOmitted(t *testing.T) {
	origUnit := unitStatusFn
	unitStatusFn = func(string) (string, error) { return "inactive", nil }
	defer func() { unitStatusFn = origUnit }()

	fw := &config.Framework{
		Workers: map[string]config.FrameworkWorker{
			"vite": vitePerWT(),
		},
	}
	got := enrichWorktreeWorkers("site", "/wt/path", fw)
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got))
	}
	if got[0].Running || got[0].Failing {
		t.Errorf("expected inactive flags, got %+v", got[0])
	}
}

func TestEnrichWorktreeWorkers_failedReported(t *testing.T) {
	origUnit := unitStatusFn
	unitStatusFn = func(string) (string, error) { return "failed", nil }
	defer func() { unitStatusFn = origUnit }()

	fw := &config.Framework{
		Workers: map[string]config.FrameworkWorker{
			"vite": vitePerWT(),
		},
	}
	got := enrichWorktreeWorkers("site", "/wt/path", fw)
	if len(got) != 1 || got[0].Running || !got[0].Failing {
		t.Errorf("expected failing flag, got %+v", got)
	}
}
