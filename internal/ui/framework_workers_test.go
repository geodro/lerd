package ui

import (
	"sort"
	"testing"

	"github.com/geodro/lerd/internal/config"
	gitpkg "github.com/geodro/lerd/internal/git"
)

// fakeStatus returns a status function that maps unit name to "active" or "inactive".
func fakeStatus(active map[string]bool) func(string) (string, error) {
	return func(name string) (string, error) {
		if active[name] {
			return "active", nil
		}
		return "inactive", nil
	}
}

// fakeWorktrees returns a detect function that returns the given list,
// ignoring the path/domain inputs.
func fakeWorktrees(wts []gitpkg.Worktree) func(string, string) ([]gitpkg.Worktree, error) {
	return func(string, string) ([]gitpkg.Worktree, error) { return wts, nil }
}

func vitePerWT() config.FrameworkWorker {
	tr := true
	return config.FrameworkWorker{Label: "Vite", Command: "npm run dev", PerWorktree: &tr}
}

func TestFrameworkWorkerServicesForSite_parentOnly(t *testing.T) {
	site := config.Site{
		Name:    "rapids",
		Domains: []string{"harborlist.test"},
		Path:    "/projects/rapids",
	}
	fw := &config.Framework{
		Workers: map[string]config.FrameworkWorker{
			"vite": {Label: "Vite", Command: "npm run dev"},
		},
	}
	got := frameworkWorkerServicesForSite(
		site, fw,
		fakeStatus(map[string]bool{"lerd-vite-rapids": true}),
		fakeWorktrees(nil),
	)
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1: %+v", len(got), got)
	}
	r := got[0]
	if r.Name != "vite-rapids" {
		t.Errorf("Name = %q, want vite-rapids", r.Name)
	}
	if r.WorkerSite != "rapids" || r.WorkerName != "vite" || r.WorkerLabel != "Vite" {
		t.Errorf("worker fields = %+v", r)
	}
	if r.WorkerWorktree != "" || r.WorkerWorktreeDomain != "" {
		t.Errorf("parent should not carry worktree fields, got %+v", r)
	}
}

func TestFrameworkWorkerServicesForSite_parentInactiveWorktreeActive(t *testing.T) {
	// Worktree vite unit is running but the parent's is not. Vite must opt
	// into per_worktree:true for the worktree variant to enumerate.
	site := config.Site{
		Name:    "rapids",
		Domains: []string{"harborlist.test"},
		Path:    "/projects/rapids",
	}
	fw := &config.Framework{
		Workers: map[string]config.FrameworkWorker{
			"vite": vitePerWT(),
		},
	}
	wts := []gitpkg.Worktree{{
		Branch: "main",
		Path:   "/projects/rapids/main",
		Domain: "main.harborlist.test",
	}}
	active := map[string]bool{
		"lerd-vite-rapids-main": true, // worktree only
	}
	got := frameworkWorkerServicesForSite(site, fw, fakeStatus(active), fakeWorktrees(wts))
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1: %+v", len(got), got)
	}
	r := got[0]
	if r.Name != "vite-rapids-main" {
		t.Errorf("Name = %q, want vite-rapids-main", r.Name)
	}
	if r.WorkerSite != "rapids" {
		t.Errorf("WorkerSite = %q, want rapids (parent for grouping)", r.WorkerSite)
	}
	if r.WorkerWorktree != "main" {
		t.Errorf("WorkerWorktree = %q, want main", r.WorkerWorktree)
	}
	if r.WorkerWorktreeDomain != "main.harborlist.test" {
		t.Errorf("WorkerWorktreeDomain = %q, want main.harborlist.test", r.WorkerWorktreeDomain)
	}
}

func TestFrameworkWorkerServicesForSite_parentAndWorktreeActive(t *testing.T) {
	site := config.Site{
		Name:    "rapids",
		Domains: []string{"harborlist.test"},
		Path:    "/projects/rapids",
	}
	fw := &config.Framework{
		Workers: map[string]config.FrameworkWorker{
			"vite": vitePerWT(),
		},
	}
	wts := []gitpkg.Worktree{{
		Branch: "main",
		Path:   "/projects/rapids/main",
		Domain: "main.harborlist.test",
	}}
	active := map[string]bool{
		"lerd-vite-rapids":      true,
		"lerd-vite-rapids-main": true,
	}
	got := frameworkWorkerServicesForSite(site, fw, fakeStatus(active), fakeWorktrees(wts))
	names := make([]string, len(got))
	for i, r := range got {
		names[i] = r.Name
	}
	sort.Strings(names)
	want := []string{"vite-rapids", "vite-rapids-main"}
	if len(names) != len(want) || names[0] != want[0] || names[1] != want[1] {
		t.Errorf("names = %v, want %v", names, want)
	}
}

func TestFrameworkWorkerServicesForSite_skipsBuiltinWorkers(t *testing.T) {
	// queue/schedule/reverb are surfaced through dedicated lerd-queue-*
	// listing helpers; this loop must not double-list them.
	site := config.Site{
		Name:    "rapids",
		Domains: []string{"harborlist.test"},
		Path:    "/projects/rapids",
	}
	fw := &config.Framework{
		Workers: map[string]config.FrameworkWorker{
			"queue":    {Command: "x"},
			"schedule": {Command: "x"},
			"reverb":   {Command: "x"},
			"vite":     {Command: "npm run dev"},
		},
	}
	active := map[string]bool{
		"lerd-queue-rapids":    true,
		"lerd-schedule-rapids": true,
		"lerd-reverb-rapids":   true,
		"lerd-vite-rapids":     true,
	}
	got := frameworkWorkerServicesForSite(site, fw, fakeStatus(active), fakeWorktrees(nil))
	if len(got) != 1 || got[0].WorkerName != "vite" {
		t.Fatalf("expected only vite entry, got %+v", got)
	}
}

func TestFrameworkWorkerServicesForSite_inactiveOmitted(t *testing.T) {
	site := config.Site{
		Name:    "rapids",
		Domains: []string{"harborlist.test"},
		Path:    "/projects/rapids",
	}
	fw := &config.Framework{
		Workers: map[string]config.FrameworkWorker{
			"vite": {Command: "npm run dev"},
		},
	}
	got := frameworkWorkerServicesForSite(
		site, fw,
		fakeStatus(map[string]bool{}), // nothing active
		fakeWorktrees(nil),
	)
	if len(got) != 0 {
		t.Errorf("expected no entries, got %+v", got)
	}
}
