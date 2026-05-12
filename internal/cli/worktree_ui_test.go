package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func gitInitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-qm", "init")
	run("branch", "feat-existing")
	return dir
}

func TestNormalizeAddRequest(t *testing.T) {
	repo := gitInitRepo(t)

	// A local branch passes through untouched.
	in := WorktreeAddRequest{ExistingBranch: "feat-existing", DBChoice: "share", Build: "auto"}
	if got := normalizeAddRequest(repo, in); !reflect.DeepEqual(got, in) {
		t.Fatalf("local branch should pass through: got %+v", got)
	}

	// A non-local ref becomes a new-branch request named after its basename.
	got := normalizeAddRequest(repo, WorktreeAddRequest{ExistingBranch: "origin/feat-remote", DBChoice: "empty", RunMigrations: true, Build: "skip"})
	want := WorktreeAddRequest{NewBranch: "feat-remote", BaseRef: "origin/feat-remote", DBChoice: "empty", RunMigrations: true, Build: "skip"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("remote ref:\n got %+v\nwant %+v", got, want)
	}

	// New-branch requests are left alone.
	nb := WorktreeAddRequest{NewBranch: "feat-x", BaseRef: "main"}
	if got := normalizeAddRequest(repo, nb); !reflect.DeepEqual(got, nb) {
		t.Fatalf("new branch should pass through: got %+v", got)
	}
}

func TestBuildWorktreeAddGitArgs(t *testing.T) {
	cases := []struct {
		name    string
		req     WorktreeAddRequest
		path    string
		want    []string
		wantErr bool
	}{
		{
			name: "new branch, no base ref",
			req:  WorktreeAddRequest{NewBranch: "feat/x"},
			path: "/code/app-feat-x",
			want: []string{"worktree", "add", "-b", "feat/x", "/code/app-feat-x"},
		},
		{
			name: "new branch with base ref",
			req:  WorktreeAddRequest{NewBranch: "feat/x", BaseRef: "main"},
			path: "/code/app-feat-x",
			want: []string{"worktree", "add", "-b", "feat/x", "/code/app-feat-x", "main"},
		},
		{
			name: "existing branch",
			req:  WorktreeAddRequest{ExistingBranch: "hotfix"},
			path: "/code/app-hotfix",
			want: []string{"worktree", "add", "/code/app-hotfix", "hotfix"},
		},
		{
			name:    "neither branch set",
			req:     WorktreeAddRequest{},
			path:    "/code/app-x",
			wantErr: true,
		},
		{
			name:    "both branches set",
			req:     WorktreeAddRequest{NewBranch: "a", ExistingBranch: "b"},
			path:    "/code/app-x",
			wantErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := buildWorktreeAddGitArgs(c.req, c.path)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got args %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("args mismatch:\n got %v\nwant %v", got, c.want)
			}
		})
	}
}

func TestWorktreeCheckoutPath(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "myapp")

	// Branch name gets sanitized; base path is "<parent>-<slug>".
	got := WorktreeCheckoutPath(parent, "feature/auth")
	want := parent + "-feature-auth"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}

	// When the default path already exists, it bumps a numeric suffix.
	if err := os.MkdirAll(want, 0o755); err != nil {
		t.Fatal(err)
	}
	got = WorktreeCheckoutPath(parent, "feature/auth")
	if got != want+"-2" {
		t.Fatalf("got %q want %q", got, want+"-2")
	}
}

func TestApplyWorktreeDBChoice_noopChoices(t *testing.T) {
	// "share" and "" must not touch the registry or any database, so they
	// succeed even for a site that doesn't exist.
	site := &config.Site{Name: "ghost", Path: t.TempDir(), Domains: []string{"ghost.test"}}
	for _, c := range []string{"", "share"} {
		if err := ApplyWorktreeDBChoice(site, "feat-x", c, nil); err != nil {
			t.Fatalf("choice %q: unexpected error %v", c, err)
		}
	}
	if err := ApplyWorktreeDBChoice(site, "feat-x", "bogus", nil); err == nil {
		t.Fatal("expected error for unknown db choice")
	}
}

func TestRemoveWorktreeAndCleanup_unknownBranch(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	site := &config.Site{Name: "acme", Path: t.TempDir(), Domains: []string{"acme.test"}}
	if err := RemoveWorktreeAndCleanup(site, "nope", false, false, nil); err == nil {
		t.Fatal("expected error removing a branch with no worktree")
	}
}

func TestResolveBuildChoice(t *testing.T) {
	eligible := []string{"vite", "mix"}
	optedIn := []string{"vite"}
	scripts := []string{"build", "prod"}

	cases := []struct {
		name      string
		requested string
		eligible  []string
		optedIn   []string
		scripts   []string
		wantKind  string
		wantVal   string
	}{
		{"skip", "skip", eligible, optedIn, scripts, "skip", ""},
		{"auto picks opted-in worker", "auto", eligible, optedIn, scripts, "worker", "vite"},
		{"empty string == auto", "", eligible, optedIn, scripts, "worker", "vite"},
		{"auto falls back to first script", "auto", nil, nil, scripts, "script", "build"},
		{"auto falls back to skip", "auto", nil, nil, nil, "skip", ""},
		{"explicit eligible worker", "worker:mix", eligible, optedIn, scripts, "worker", "mix"},
		{"explicit script", "script:prod", eligible, optedIn, scripts, "script", "prod"},
		{"unavailable worker falls back to auto", "worker:gone", eligible, optedIn, scripts, "worker", "vite"},
		{"unavailable script falls back to auto", "script:gone", nil, nil, scripts, "script", "build"},
		{"garbage falls back to auto", "???", eligible, optedIn, scripts, "worker", "vite"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			k, v := resolveBuildChoice(c.requested, c.eligible, c.optedIn, c.scripts)
			if k != c.wantKind || v != c.wantVal {
				t.Fatalf("got (%q,%q) want (%q,%q)", k, v, c.wantKind, c.wantVal)
			}
		})
	}
}
