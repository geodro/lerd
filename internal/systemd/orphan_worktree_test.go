package systemd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// setupParentWithWorktree creates a fake parent repo with a single worktree
// checkout, returning the parent path. Mirrors what gitpkg.DetectWorktrees
// expects: .git/ as a real dir with a worktrees/<wtBase>/ entry that has HEAD
// + gitdir files pointing to a real checkout dir.
func setupParentWithWorktree(t *testing.T, parentName, wtBase string) string {
	t.Helper()
	root := t.TempDir()
	parent := filepath.Join(root, parentName)
	if err := os.MkdirAll(filepath.Join(parent, ".git", "worktrees", wtBase), 0755); err != nil {
		t.Fatal(err)
	}
	wtPath := filepath.Join(parent, wtBase)
	if err := os.MkdirAll(wtPath, 0755); err != nil {
		t.Fatal(err)
	}
	wtMeta := filepath.Join(parent, ".git", "worktrees", wtBase)
	if err := os.WriteFile(filepath.Join(wtMeta, "HEAD"), []byte("ref: refs/heads/"+wtBase+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtMeta, "gitdir"), []byte(filepath.Join(wtPath, ".git")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return parent
}

func TestUnitBelongsToOtherSiteWorktree_noHyphen(t *testing.T) {
	if UnitBelongsToOtherSiteWorktree("vite", "main", nil) {
		t.Error("non-hyphenated workerName must never be flagged as worktree")
	}
}

func TestUnitBelongsToOtherSiteWorktree_unknownParent(t *testing.T) {
	sites := []config.Site{{Name: "other", Path: "/tmp/nope"}}
	if UnitBelongsToOtherSiteWorktree("vite-whitewaters", "main", sites) {
		t.Error("must not match when no registered site is named 'whitewaters'")
	}
}

func TestUnitBelongsToOtherSiteWorktree_realWorktree(t *testing.T) {
	parent := setupParentWithWorktree(t, "whitewaters", "main")
	sites := []config.Site{{Name: "whitewaters", Path: parent}}
	if !UnitBelongsToOtherSiteWorktree("vite-whitewaters", "main", sites) {
		t.Error("expected match: lerd-vite-whitewaters-main belongs to whitewaters")
	}
}

func TestUnitBelongsToOtherSiteWorktree_hyphenatedParentName(t *testing.T) {
	parent := setupParentWithWorktree(t, "admin-astrolov", "feature-foo")
	sites := []config.Site{{Name: "admin-astrolov", Path: parent}}
	if !UnitBelongsToOtherSiteWorktree("vite-admin-astrolov", "feature-foo", sites) {
		t.Error("expected match across multiple hyphens in parent name")
	}
}

func TestUnitBelongsToOtherSiteWorktree_parentExistsButNoMatchingWorktree(t *testing.T) {
	parent := setupParentWithWorktree(t, "whitewaters", "branch-other")
	sites := []config.Site{{Name: "whitewaters", Path: parent}}
	if UnitBelongsToOtherSiteWorktree("vite-whitewaters", "main", sites) {
		t.Error("must not match when the parent has no worktree named 'main'")
	}
}
