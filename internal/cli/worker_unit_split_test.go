package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// TestSplitWorkerUnit_parentUnit is the simple case the original macOS
// migration code already handled: lerd-vite-mysite parses to (vite, mysite, "").
func TestSplitWorkerUnit_parentUnit(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	if err := config.AddSite(config.Site{
		Name: "mysite", Path: filepath.Join(tmp, "mysite"), Domains: []string{"mysite.test"},
	}); err != nil {
		t.Fatal(err)
	}

	kind, site, wt, ok := splitWorkerUnit("vite-mysite")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if kind != "vite" || site != "mysite" || wt != "" {
		t.Errorf("got (%q,%q,%q), want (vite,mysite,)", kind, site, wt)
	}
}

// TestSplitWorkerUnit_worktreeUnit pins the regression fix: a unit named
// lerd-vite-mysite-feat-x must parse to (vite, mysite, feat-x), not get
// mis-anchored on `feat-x` (which would happen if a stray site named
// "feat-x" existed) or fall back to (vite-mysite, feat-x, ""). Pre-fix the
// migrate flow ran restartWorkerByUnitName with the wrong site, corrupting
// the worktree worker.
func TestSplitWorkerUnit_worktreeUnit(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	sitePath := filepath.Join(tmp, "mysite")
	if err := os.MkdirAll(sitePath, 0755); err != nil {
		t.Fatal(err)
	}
	checkout := filepath.Join(tmp, "checkouts", "feat-x")
	if err := os.MkdirAll(checkout, 0755); err != nil {
		t.Fatal(err)
	}
	wtMeta := filepath.Join(sitePath, ".git", "worktrees", "feat-x")
	os.MkdirAll(wtMeta, 0755)
	os.WriteFile(filepath.Join(wtMeta, "HEAD"), []byte("ref: refs/heads/feat-x\n"), 0644)
	os.WriteFile(filepath.Join(wtMeta, "gitdir"), []byte(filepath.Join(checkout, ".git")+"\n"), 0644)

	if err := config.AddSite(config.Site{
		Name: "mysite", Path: sitePath, Domains: []string{"mysite.test"},
	}); err != nil {
		t.Fatal(err)
	}

	kind, site, wt, ok := splitWorkerUnit("vite-mysite-feat-x")
	if !ok {
		t.Fatal("expected ok=true for worktree unit name")
	}
	if kind != "vite" || site != "mysite" || wt != "feat-x" {
		t.Errorf("got (%q,%q,%q), want (vite,mysite,feat-x)", kind, site, wt)
	}
}

// TestSplitWorkerUnit_hyphenatedKind_parent ensures kinds containing
// hyphens (e.g. custom framework workers like "custom-worker") still
// resolve correctly when no worktree is present.
func TestSplitWorkerUnit_hyphenatedKind_parent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	if err := config.AddSite(config.Site{
		Name: "mysite", Path: filepath.Join(tmp, "mysite"), Domains: []string{"mysite.test"},
	}); err != nil {
		t.Fatal(err)
	}

	kind, site, wt, ok := splitWorkerUnit("custom-worker-mysite")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if kind != "custom-worker" || site != "mysite" || wt != "" {
		t.Errorf("got (%q,%q,%q), want (custom-worker,mysite,)", kind, site, wt)
	}
}

// TestSplitWorkerUnit_unknownSite returns ok=false so the caller surfaces
// "could not parse" instead of randomly anchoring on a substring.
func TestSplitWorkerUnit_unknownSite(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	if _, _, _, ok := splitWorkerUnit("vite-stranger"); ok {
		t.Error("expected ok=false for unknown site")
	}
}

// TestSplitWorkerUnit_longestSuffixWins guards against partial-match
// false positives: with sites "alpha" and "alpha-beta" registered, the
// unit lerd-vite-alpha-beta must resolve to site "alpha-beta", not site
// "alpha" with kind "vite-alpha".
func TestSplitWorkerUnit_longestSuffixWins(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	for _, name := range []string{"alpha", "alpha-beta"} {
		if err := config.AddSite(config.Site{
			Name: name, Path: filepath.Join(tmp, name), Domains: []string{name + ".test"},
		}); err != nil {
			t.Fatal(err)
		}
	}

	kind, site, wt, ok := splitWorkerUnit("vite-alpha-beta")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if site != "alpha-beta" || kind != "vite" || wt != "" {
		t.Errorf("got (%q,%q,%q), want (vite,alpha-beta,)", kind, site, wt)
	}
}
