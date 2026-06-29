package watcher

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// TestWatchSourceFiles_savesFireActivity asserts a write under a watched source
// tree reports activity for the right key, while a write under an excluded
// subtree (node_modules) is ignored.
func TestWatchSourceFiles_savesFireActivity(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	nmDir := filepath.Join(root, "node_modules")
	for _, d := range []string{srcDir, nmDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	activity := make(chan string, 8)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		_ = WatchSourceFiles(
			func() []SourceTarget {
				return []SourceTarget{{Key: "mysite", Dirs: []string{root}}}
			},
			40*time.Millisecond,
			func(key string) { activity <- key },
			stop,
		)
	}()
	// Let the initial scan register the directory watches.
	time.Sleep(300 * time.Millisecond)

	// A save in the source tree reports activity for the target's key.
	if err := os.WriteFile(filepath.Join(srcDir, "Foo.vue"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case key := <-activity:
		if key != "mysite" {
			t.Errorf("activity key = %q, want mysite", key)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no activity reported for a source-file save")
	}

	// A write under node_modules is excluded and must not report activity.
	if err := os.WriteFile(filepath.Join(nmDir, "dep.js"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case key := <-activity:
		t.Errorf("node_modules write reported activity (%q); it must be excluded", key)
	case <-time.After(600 * time.Millisecond):
		// no activity — correct
	}
}

// TestWatchSourceFiles_skipsOversizedDir asserts a directory dense enough to be
// an asset dump (more than maxSourceDirEntries files, e.g. a checked-in icon
// set) is not watched, so kqueue's per-file fds can't exhaust the process limit.
func TestWatchSourceFiles_skipsOversizedDir(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	assets := filepath.Join(root, "src", "icons") // dense asset dump under a source root
	for _, d := range []string{srcDir, assets} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i <= maxSourceDirEntries; i++ { // > maxSourceDirEntries entries
		if err := os.WriteFile(filepath.Join(assets, "i"+strconv.Itoa(i)+".svg"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	activity := make(chan string, 8)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		_ = WatchSourceFiles(
			func() []SourceTarget { return []SourceTarget{{Key: "mysite", Dirs: []string{root}}} },
			40*time.Millisecond,
			func(key string) { activity <- key },
			stop,
		)
	}()
	time.Sleep(300 * time.Millisecond)

	// A write in the oversized dir must not report activity: it was skipped.
	if err := os.WriteFile(filepath.Join(assets, "new.svg"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case key := <-activity:
		t.Errorf("oversized-dir write reported activity (%q); it must be skipped", key)
	case <-time.After(600 * time.Millisecond):
		// no activity, correct
	}

	// The normal source root is still watched.
	if err := os.WriteFile(filepath.Join(srcDir, "App.vue"), []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case key := <-activity:
		if key != "mysite" {
			t.Errorf("activity key = %q, want mysite", key)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no activity for a normal source-file save (over-broad skip?)")
	}
}
