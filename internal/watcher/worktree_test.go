package watcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatchWorktrees_HEADWriteTriggersChangedForExistingWorktree(t *testing.T) {
	site := t.TempDir()
	checkout := t.TempDir()

	wtDir := filepath.Join(site, ".git", "worktrees", "feat")
	if err := os.MkdirAll(wtDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtDir, "HEAD"), []byte("ref: refs/heads/old-name\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtDir, "gitdir"), []byte(filepath.Join(checkout, ".git")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	changed := make(chan string, 1)
	errs := make(chan error, 1)
	go func() {
		errs <- WatchWorktrees(
			func() []string { return []string{site} },
			func(_, _ string) {},
			func(_, name string) { changed <- name },
			func(_, _ string) {},
		)
	}()

	headPath := filepath.Join(wtDir, "HEAD")
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case name := <-changed:
			if name != "feat" {
				t.Fatalf("changed name = %q, want feat", name)
			}
			return
		case err := <-errs:
			t.Fatalf("WatchWorktrees returned: %v", err)
		case <-ticker.C:
			if err := os.WriteFile(headPath, []byte("ref: refs/heads/new-name\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		case <-deadline:
			t.Fatal("timed out waiting for HEAD write to trigger worktree change")
		}
	}
}
