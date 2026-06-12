package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/geodro/lerd/internal/config"
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

	EnsureWorktreeDeps(main, wt, "branch.main.test", false, nil)

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

// When the worktree's composer.lock differs from main's, copying main's
// vendor would leave stale autoload entries pointing at packages the
// worktree's lock doesn't list (or vice-versa). Skip the copy so composer
// install rebuilds vendor from scratch.
func TestEnsureWorktreeDeps_skipsVendorCopyWhenComposerLockDiffers(t *testing.T) {
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
	if err := os.MkdirAll(filepath.Join(main, "vendor", "ryangjchandler"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(main, "vendor", "ryangjchandler", "marker"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(main, "composer.lock"), []byte(`{"packages":["a"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "composer.lock"), []byte(`{"packages":["b"]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	EnsureWorktreeDeps(main, wt, "branch.main.test", false, nil)

	if _, err := os.Stat(filepath.Join(wt, "vendor")); !os.IsNotExist(err) {
		t.Errorf("vendor should NOT be copied when composer.lock differs, got err=%v", err)
	}
}

// When lockfiles match byte-for-byte, the copy proceeds — the package set is
// identical so no stale-state risk.
func TestEnsureWorktreeDeps_copiesVendorWhenComposerLockMatches(t *testing.T) {
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
	lock := []byte(`{"packages":["a"]}`)
	if err := os.WriteFile(filepath.Join(main, "composer.lock"), lock, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "composer.lock"), lock, 0o644); err != nil {
		t.Fatal(err)
	}

	EnsureWorktreeDeps(main, wt, "branch.main.test", false, nil)

	if _, err := os.Stat(filepath.Join(wt, "vendor", "autoload.php")); err != nil {
		t.Errorf("vendor not copied even though composer.lock matches: %v", err)
	}
}

// public/build/ is a build artefact of the source tree — copying it from main
// would cause the worktree to silently serve main's compiled UI even when the
// branch has touched assets. EnsureWorktreeDeps must never seed it; users run
// `npm run dev` / `npm run build` to produce the worktree's manifest.
func TestEnsureWorktreeDeps_skipsPublicBuild(t *testing.T) {
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
	if err := os.MkdirAll(filepath.Join(main, "public", "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(main, "public", "build", "manifest.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	EnsureWorktreeDeps(main, wt, "branch.main.test", false, nil)

	if _, err := os.Stat(filepath.Join(wt, "public", "build", "manifest.json")); !os.IsNotExist(err) {
		t.Errorf("public/build/manifest.json should NOT be copied into the worktree, got err=%v", err)
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

	EnsureWorktreeDeps(main, wt, "branch.main.test", false, nil)

	info, err := os.Lstat(filepath.Join(wt, "vendor"))
	if err != nil {
		t.Fatalf("vendor missing: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("legacy symlink should have been replaced with a real copy")
	}
}

func TestEnsureWorktreeDeps_updatesExistingEnvAppURL(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(wt, ".env"), []byte("APP_NAME=Worktree\nAPP_URL=http://stale.test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	EnsureWorktreeDeps(main, wt, "branch.main.test", true, nil)

	data, err := os.ReadFile(filepath.Join(wt, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "APP_URL=https://branch.main.test") {
		t.Fatalf("expected APP_URL to match worktree vhost domain, got:\n%s", content)
	}
	if strings.Contains(content, "http://stale.test") {
		t.Fatalf("stale APP_URL should have been replaced, got:\n%s", content)
	}
}

// EnsureWorktreeDeps must not downgrade the .env file mode when rewriting
// APP_URL. .env holds APP_KEY and DB credentials and users routinely chmod it
// to 0600.
func TestEnsureWorktreeDeps_preservesEnvMode(t *testing.T) {
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
	envPath := filepath.Join(wt, ".env")
	if err := os.WriteFile(envPath, []byte("APP_URL=http://stale.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	EnsureWorktreeDeps(main, wt, "branch.main.test", true, nil)

	info, err := os.Stat(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("env mode = %o, want 0600", got)
	}
}

// EnsureWorktreeDeps must not bump .env mtime when APP_URL already matches
// the worktree domain. Dev-side watchers (vite, IDE indexers, opcache) react
// to mtime changes, so a no-op scan should be a no-op on disk.
func TestEnsureWorktreeDeps_skipsWriteWhenAppURLUnchanged(t *testing.T) {
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
	envPath := filepath.Join(wt, ".env")
	if err := os.WriteFile(envPath, []byte("APP_NAME=Worktree\nAPP_URL=https://branch.main.test"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(envPath, old, old); err != nil {
		t.Fatal(err)
	}

	EnsureWorktreeDeps(main, wt, "branch.main.test", true, nil)

	info, err := os.Stat(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(old) {
		t.Fatalf("env mtime bumped from %v to %v on a no-op rewrite", old, info.ModTime())
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

	EnsureWorktreeDeps(main, wt, "branch.main.test", false, nil)

	if _, err := os.Lstat(filepath.Join(wt, "vendor")); err == nil {
		t.Error("vendor should not be created when main has none")
	}
}

// ── FilterReservedWorktrees ──────────────────────────────────────────────────

func TestFilterReservedWorktrees_dropsSecondaryDomain(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	// A group secondary owns admin.starlane.test.
	if err := config.AddSite(config.Site{
		Name: "admin", Domains: []string{"admin.starlane.test"}, Path: "/srv/admin",
		Group: "starlane", GroupSubdomain: "admin",
	}); err != nil {
		t.Fatal(err)
	}

	wts := []Worktree{
		{Name: "admin", Branch: "admin", Domain: "admin.starlane.test", Path: "/wt/admin"},
		{Name: "feat", Branch: "feat", Domain: "feat.starlane.test", Path: "/wt/feat"},
	}
	got := FilterReservedWorktrees(wts)
	if len(got) != 1 || got[0].Domain != "feat.starlane.test" {
		t.Errorf("expected only feat worktree to survive, got %+v", got)
	}
}

func TestFilterReservedWorktrees_noReservations(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	wts := []Worktree{{Name: "feat", Branch: "feat", Domain: "feat.starlane.test", Path: "/wt/feat"}}
	got := FilterReservedWorktrees(wts)
	if len(got) != 1 {
		t.Errorf("expected worktree to survive when no secondaries exist, got %+v", got)
	}
}
