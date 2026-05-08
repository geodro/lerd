package config

import "testing"

func TestIsPerWorktree_defaultFalse(t *testing.T) {
	// Per-worktree is opt-in: framework yamls set per_worktree:true on
	// workers that genuinely run independently per checkout (vite). Anything
	// unset stays parent-only.
	if (FrameworkWorker{}).IsPerWorktree() {
		t.Error("default must be false; opt in via per_worktree:true")
	}
}

func TestIsPerWorktree_explicitOverride(t *testing.T) {
	tr, fa := true, false
	if !(FrameworkWorker{PerWorktree: &tr}).IsPerWorktree() {
		t.Error("explicit per_worktree:true must report true")
	}
	if (FrameworkWorker{PerWorktree: &fa}).IsPerWorktree() {
		t.Error("explicit per_worktree:false must report false")
	}
}

func TestBuiltinLaravel_workersStayParentOnly(t *testing.T) {
	for n, w := range laravelFramework.Workers {
		if w.IsPerWorktree() {
			t.Errorf("builtin laravel %s must default to parent-only (no opt-in)", n)
		}
	}
}

func TestBuiltinSymfony_workersStayParentOnly(t *testing.T) {
	for n, w := range symfonyFramework.Workers {
		if w.IsPerWorktree() {
			t.Errorf("builtin symfony %s must default to parent-only (no opt-in)", n)
		}
	}
}
