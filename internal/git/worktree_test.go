package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── SanitizeBranch ───────────────────────────────────────────────────────────

func TestSanitizeBranch(t *testing.T) {
	cases := []struct{ in, want string }{
		{"main", "main"},
		{"feature/my-thing", "feature-my-thing"},
		{"fix_bug_123", "fix-bug-123"},
		{"HOTFIX/ABC", "hotfix-abc"},
		{"v1.2.3", "v1-2-3"},
		{"feature//double-slash", "feature-double-slash"},
		{"---leading", "leading"},
		{"trailing---", "trailing"},
		{"", "branch"},
		{"system", "system"},
		{"feat/add_new.thing", "feat-add-new-thing"},
	}
	for _, c := range cases {
		got := SanitizeBranch(c.in)
		if got != c.want {
			t.Errorf("SanitizeBranch(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSanitizeBranch_truncatesAt50(t *testing.T) {
	long := strings.Repeat("a", 60)
	got := SanitizeBranch(long)
	if len(got) > 50 {
		t.Errorf("expected len <= 50, got %d (%q)", len(got), got)
	}
}

// ── IsMainRepo ───────────────────────────────────────────────────────────────

func TestIsMainRepo_directory(t *testing.T) {
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, ".git"), 0755)
	if !IsMainRepo(tmp) {
		t.Error("expected IsMainRepo=true when .git is a directory")
	}
}

func TestIsMainRepo_file(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, ".git"), []byte("gitdir: ../.git/worktrees/feat"), 0644)
	if IsMainRepo(tmp) {
		t.Error("expected IsMainRepo=false when .git is a file (worktree checkout)")
	}
}

func TestIsMainRepo_missing(t *testing.T) {
	tmp := t.TempDir()
	if IsMainRepo(tmp) {
		t.Error("expected IsMainRepo=false when .git is absent")
	}
}

// ── DetectWorktrees ──────────────────────────────────────────────────────────

func TestDetectWorktrees_noGitDir(t *testing.T) {
	tmp := t.TempDir()
	wts, err := DetectWorktrees(tmp, "mysite.test")
	if err != nil {
		t.Fatal(err)
	}
	if len(wts) != 0 {
		t.Errorf("expected empty, got %v", wts)
	}
}

func TestDetectWorktrees_worktreeCheckout(t *testing.T) {
	// .git is a file → this is itself a worktree checkout, not the main repo
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, ".git"), []byte("gitdir: ../.git/worktrees/feat"), 0644)
	wts, err := DetectWorktrees(tmp, "mysite.test")
	if err != nil {
		t.Fatal(err)
	}
	if len(wts) != 0 {
		t.Errorf("expected empty for worktree checkout, got %v", wts)
	}
}

func TestDetectWorktrees_noWorktreesDir(t *testing.T) {
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, ".git"), 0755)
	wts, err := DetectWorktrees(tmp, "mysite.test")
	if err != nil {
		t.Fatal(err)
	}
	if len(wts) != 0 {
		t.Errorf("expected empty when no worktrees dir, got %v", wts)
	}
}

func TestDetectWorktrees_oneWorktree(t *testing.T) {
	main := t.TempDir()
	checkout := t.TempDir()

	wtDir := filepath.Join(main, ".git", "worktrees", "feat")
	os.MkdirAll(wtDir, 0755)
	os.WriteFile(filepath.Join(wtDir, "HEAD"), []byte("ref: refs/heads/feature/my-thing\n"), 0644)
	os.WriteFile(filepath.Join(wtDir, "gitdir"), []byte(filepath.Join(checkout, ".git")+"\n"), 0644)

	wts, err := DetectWorktrees(main, "mysite.test")
	if err != nil {
		t.Fatalf("DetectWorktrees: %v", err)
	}
	if len(wts) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(wts))
	}
	wt := wts[0]
	if wt.Branch != "feature-my-thing" {
		t.Errorf("Branch = %q, want %q", wt.Branch, "feature-my-thing")
	}
	if wt.Domain != "feature-my-thing.mysite.test" {
		t.Errorf("Domain = %q, want %q", wt.Domain, "feature-my-thing.mysite.test")
	}
	if wt.Path != checkout {
		t.Errorf("Path = %q, want %q", wt.Path, checkout)
	}
	if wt.Name != "feat" {
		t.Errorf("Name = %q, want %q", wt.Name, "feat")
	}
}

func TestDetectWorktrees_detachedHEAD(t *testing.T) {
	main := t.TempDir()
	checkout := t.TempDir()

	wtDir := filepath.Join(main, ".git", "worktrees", "det")
	os.MkdirAll(wtDir, 0755)
	os.WriteFile(filepath.Join(wtDir, "HEAD"), []byte("abc1234defgh5678\n"), 0644)
	os.WriteFile(filepath.Join(wtDir, "gitdir"), []byte(filepath.Join(checkout, ".git")+"\n"), 0644)

	wts, err := DetectWorktrees(main, "mysite.test")
	if err != nil {
		t.Fatalf("DetectWorktrees: %v", err)
	}
	if len(wts) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(wts))
	}
	if wts[0].Branch != "detached-abc1234" {
		t.Errorf("Branch = %q, want %q", wts[0].Branch, "detached-abc1234")
	}
}

func TestDetectWorktrees_skipsGoneCheckout(t *testing.T) {
	main := t.TempDir()

	wtDir := filepath.Join(main, ".git", "worktrees", "gone")
	os.MkdirAll(wtDir, 0755)
	os.WriteFile(filepath.Join(wtDir, "HEAD"), []byte("ref: refs/heads/gone\n"), 0644)
	os.WriteFile(filepath.Join(wtDir, "gitdir"), []byte("/nonexistent/path/.git\n"), 0644)

	wts, err := DetectWorktrees(main, "mysite.test")
	if err != nil {
		t.Fatal(err)
	}
	if len(wts) != 0 {
		t.Errorf("expected gone worktree to be skipped, got %v", wts)
	}
}

func TestDetectWorktrees_multipleWorktrees(t *testing.T) {
	main := t.TempDir()
	co1 := t.TempDir()
	co2 := t.TempDir()

	for _, tc := range []struct {
		name, head, checkout string
	}{
		{"feat-a", "ref: refs/heads/feat-a\n", co1},
		{"feat-b", "ref: refs/heads/feat-b\n", co2},
	} {
		wtDir := filepath.Join(main, ".git", "worktrees", tc.name)
		os.MkdirAll(wtDir, 0755)
		os.WriteFile(filepath.Join(wtDir, "HEAD"), []byte(tc.head), 0644)
		os.WriteFile(filepath.Join(wtDir, "gitdir"), []byte(filepath.Join(tc.checkout, ".git")+"\n"), 0644)
	}

	wts, err := DetectWorktrees(main, "site.test")
	if err != nil {
		t.Fatal(err)
	}
	if len(wts) != 2 {
		t.Errorf("expected 2 worktrees, got %d", len(wts))
	}
}

// ── rewriteAppURL ────────────────────────────────────────────────────────────

func TestRewriteAppURL_replacesExisting(t *testing.T) {
	tmp := t.TempDir()
	envFile := filepath.Join(tmp, ".env")
	os.WriteFile(envFile, []byte("APP_NAME=MyApp\nAPP_URL=http://old.test\nAPP_ENV=local\n"), 0644)

	if err := rewriteAppURL(envFile, "https://new.test"); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(envFile)
	content := string(data)
	if !strings.Contains(content, "APP_URL=https://new.test") {
		t.Errorf("expected new APP_URL in:\n%s", content)
	}
	if strings.Contains(content, "APP_URL=http://old.test") {
		t.Error("old APP_URL should have been replaced")
	}
	if !strings.Contains(content, "APP_NAME=MyApp") {
		t.Error("unrelated lines should be preserved")
	}
}

func TestRewriteAppURL_appendsWhenMissing(t *testing.T) {
	tmp := t.TempDir()
	envFile := filepath.Join(tmp, ".env")
	os.WriteFile(envFile, []byte("APP_NAME=MyApp\n"), 0644)

	if err := rewriteAppURL(envFile, "https://new.test"); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(envFile)
	if !strings.Contains(string(data), "APP_URL=https://new.test") {
		t.Errorf("expected APP_URL to be appended, got:\n%s", string(data))
	}
}

func TestRewriteAppURL_missingFile(t *testing.T) {
	err := rewriteAppURL("/nonexistent/.env", "https://x.test")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

// EnsureWorktreeDeps copies vendor/ and node_modules/ from the main repo
// rather than symlinking, because PHP resolves __DIR__ through symlinks
// and Composer's ClassLoader otherwise initialises against the main
// checkout instead of the worktree.
func TestEnsureWorktreeDeps_copiesInsteadOfSymlinking(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local/share"))

	main := filepath.Join(home, "main")
	wt := filepath.Join(home, "wt")
	for _, d := range []string{main, wt} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(main, "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(main, "vendor", "autoload.php"), []byte("<?php"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(main, ".env"), []byte("APP_URL=http://main.test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	EnsureWorktreeDeps(main, wt, "branch.main.test", false)

	info, err := os.Lstat(filepath.Join(wt, "vendor"))
	if err != nil {
		t.Fatalf("vendor missing in worktree: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("vendor should be a real directory, not a symlink")
	}
	if !info.IsDir() {
		t.Fatal("vendor should be a directory")
	}
	if _, err := os.Stat(filepath.Join(wt, "vendor", "autoload.php")); err != nil {
		t.Errorf("vendor/autoload.php not copied: %v", err)
	}
}

// EnsureWorktreeDeps replaces a legacy symlink left by an older lerd
// version with a real copy.
func TestEnsureWorktreeDeps_migratesLegacySymlink(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local/share"))

	main := filepath.Join(home, "main")
	wt := filepath.Join(home, "wt")
	if err := os.MkdirAll(filepath.Join(main, "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(main, "vendor", "autoload.php"), []byte("<?php"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(main, "vendor"), filepath.Join(wt, "vendor")); err != nil {
		t.Fatal(err)
	}

	EnsureWorktreeDeps(main, wt, "branch.main.test", false)

	info, err := os.Lstat(filepath.Join(wt, "vendor"))
	if err != nil {
		t.Fatalf("vendor missing: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("legacy symlink should have been replaced with a real copy")
	}
}

// When main has no vendor/ yet, EnsureWorktreeDeps must not create an
// empty directory in the worktree; it should simply do nothing for that
// tree.
func TestEnsureWorktreeDeps_mainMissingVendor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local/share"))

	main := filepath.Join(home, "main")
	wt := filepath.Join(home, "wt")
	for _, d := range []string{main, wt} {
		_ = os.MkdirAll(d, 0o755)
	}

	EnsureWorktreeDeps(main, wt, "branch.main.test", false)

	if _, err := os.Lstat(filepath.Join(wt, "vendor")); err == nil {
		t.Error("vendor should not be created when main has none")
	}
}
