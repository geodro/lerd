package watcher

import (
	"os"
	"path/filepath"
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
	go func() {
		_ = WatchSourceFiles(
			func() []SourceTarget {
				return []SourceTarget{{Key: "mysite", Dirs: []string{root}}}
			},
			40*time.Millisecond,
			func(key string) { activity <- key },
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
