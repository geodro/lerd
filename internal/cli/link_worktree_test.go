package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// makeWorktreeLayout creates the minimal .git/worktrees/<wt>/ structure that
// gitpkg.DetectWorktrees walks. Returns parent path and worktree path.
func makeWorktreeLayout(t *testing.T, parentName, wtBase string) (string, string) {
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
	meta := filepath.Join(parent, ".git", "worktrees", wtBase)
	if err := os.WriteFile(filepath.Join(meta, "HEAD"), []byte("ref: refs/heads/"+wtBase+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(meta, "gitdir"), []byte(filepath.Join(wtPath, ".git")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return parent, wtPath
}

// writeSitesYAML writes a minimal sites.yaml into the current XDG_DATA_HOME so
// config.LoadSites returns the supplied sites.
func writeSitesYAML(t *testing.T, sites []config.Site) {
	t.Helper()
	dir := filepath.Join(os.Getenv("XDG_DATA_HOME"), "lerd")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	body := "sites:\n"
	for _, s := range sites {
		body += "  - name: " + s.Name + "\n"
		body += "    path: " + s.Path + "\n"
		body += "    domains:\n      - " + s.Name + ".test\n"
		body += "    php_version: \"8.4\"\n    node_version: \"22\"\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "sites.yaml"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestFindOwningWorktree_match(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	parent, wtPath := makeWorktreeLayout(t, "whitewaters", "main")
	writeSitesYAML(t, []config.Site{{Name: "whitewaters", Path: parent}})

	owner, branch, ok := findOwningWorktree(wtPath)
	if !ok {
		t.Fatal("expected worktree to be matched to parent")
	}
	if owner.Name != "whitewaters" || branch != "main" {
		t.Errorf("got %s/%s, want whitewaters/main", owner.Name, branch)
	}
}

func TestFindOwningWorktree_noMatchForUnrelatedDir(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	parent, _ := makeWorktreeLayout(t, "whitewaters", "main")
	writeSitesYAML(t, []config.Site{{Name: "whitewaters", Path: parent}})

	if _, _, ok := findOwningWorktree(t.TempDir()); ok {
		t.Error("unrelated dir must not match a worktree")
	}
}

func TestFindOwningWorktree_skipsParentItself(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	parent, _ := makeWorktreeLayout(t, "whitewaters", "main")
	writeSitesYAML(t, []config.Site{{Name: "whitewaters", Path: parent}})

	if _, _, ok := findOwningWorktree(parent); ok {
		t.Error("the parent path itself must not register as one of its own worktrees")
	}
}
