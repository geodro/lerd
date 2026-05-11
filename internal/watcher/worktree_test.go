package watcher

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestHandleNewEntry_DedupsConcurrentCalls pins the regression that produced
// "unzip: can't open vendor/composer/tmp-*.zip" failures: git worktree add
// fires multiple fsnotify events so three to four handleNewEntry goroutines
// would run concurrently on the same worktree, racing each other through
// onAdded → composer install. The first goroutine must claim the slot and
// the rest must return without invoking onAdded.
func TestHandleNewEntry_DedupsConcurrentCalls(t *testing.T) {
	site := t.TempDir()
	checkout := t.TempDir()
	entryDir := filepath.Join(site, ".git", "worktrees", "feat")
	if err := os.MkdirAll(entryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(entryDir, "gitdir"), []byte(filepath.Join(checkout, ".git")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(entryDir, "HEAD"), []byte("ref: refs/heads/feat\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32
	slowOnAdded := func(_, _ string) {
		n := concurrent.Add(1)
		for {
			cur := maxConcurrent.Load()
			if n <= cur || maxConcurrent.CompareAndSwap(cur, n) {
				break
			}
		}
		calls.Add(1)
		// Hold the slot long enough that the other goroutines reach the
		// LoadOrStore guard while this one is still running. Mirrors a
		// real composer install which takes seconds.
		time.Sleep(50 * time.Millisecond)
		concurrent.Add(-1)
	}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			handleNewEntry(entryDir, site, "feat", slowOnAdded)
		}()
	}
	wg.Wait()

	if got := maxConcurrent.Load(); got != 1 {
		t.Errorf("max concurrent onAdded = %d, want 1 (dedup failed)", got)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("onAdded called %d times, want exactly 1 (the four events should collapse)", got)
	}

	// After the first call returns, the in-flight slot is released so a
	// subsequent event (e.g. HEAD rewrite on rename) re-fires onAdded.
	handleNewEntry(entryDir, site, "feat", slowOnAdded)
	if got := calls.Load(); got != 2 {
		t.Errorf("onAdded after slot release: total = %d, want 2", got)
	}
}

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
