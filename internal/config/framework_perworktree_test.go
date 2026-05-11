package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

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

// TestFrameworkWorker_YAMLRoundtrip pins that per_worktree and replaces_build
// survive marshal -> unmarshal. The pointer-typed PerWorktree is the easy one
// to break since gopkg.in/yaml.v3 has different rules for pointers vs scalars.
func TestFrameworkWorker_YAMLRoundtrip(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want FrameworkWorker
	}{
		{
			name: "per_worktree true + replaces_build true",
			yaml: "command: x\nper_worktree: true\nreplaces_build: true\n",
			want: FrameworkWorker{Command: "x", PerWorktree: ptr(true), ReplacesBuild: true},
		},
		{
			name: "per_worktree false explicit",
			yaml: "command: x\nper_worktree: false\n",
			want: FrameworkWorker{Command: "x", PerWorktree: ptr(false)},
		},
		{
			name: "fields omitted",
			yaml: "command: x\n",
			want: FrameworkWorker{Command: "x"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got FrameworkWorker
			if err := yaml.Unmarshal([]byte(tc.yaml), &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got.Command != tc.want.Command {
				t.Errorf("Command = %q, want %q", got.Command, tc.want.Command)
			}
			if got.ReplacesBuild != tc.want.ReplacesBuild {
				t.Errorf("ReplacesBuild = %v, want %v", got.ReplacesBuild, tc.want.ReplacesBuild)
			}
			switch {
			case tc.want.PerWorktree == nil && got.PerWorktree != nil:
				t.Errorf("PerWorktree = %v, want nil", *got.PerWorktree)
			case tc.want.PerWorktree != nil && got.PerWorktree == nil:
				t.Errorf("PerWorktree = nil, want %v", *tc.want.PerWorktree)
			case tc.want.PerWorktree != nil && *got.PerWorktree != *tc.want.PerWorktree:
				t.Errorf("PerWorktree = %v, want %v", *got.PerWorktree, *tc.want.PerWorktree)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }
