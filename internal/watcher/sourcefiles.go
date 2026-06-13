package watcher

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/geodro/lerd/internal/config"
)

// SourceTarget is one checkout whose source tree is watched for edits. Key is
// the idle-activity key — a site name, or "site/wtBase" for a worktree — and
// Dirs are the absolute source roots to watch recursively.
type SourceTarget struct {
	Key  string
	Dirs []string
}

// WatchSourceFiles watches each target's source directories recursively and
// calls onActivity(key), debounced per key, when a source file under them is
// written, created, or renamed — i.e. when you save while coding. Heavy or
// generated subtrees (node_modules, vendor, hidden dirs, ...) are never
// descended, so the watch set stays small, which also keeps macOS kqueue
// descriptor use bounded. Targets are re-scanned periodically so new
// sites/worktrees and newly-created subdirectories are picked up.
func WatchSourceFiles(getTargets func() []SourceTarget, debounce time.Duration, onActivity func(key string)) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()

	var mu sync.Mutex
	dirKey := map[string]string{}      // watched directory → activity key
	timers := map[string]*time.Timer{} // pending debounce timer per key

	// addTree recursively watches root and its subdirs (skipping excludes),
	// tagging each watched directory with key.
	addTree := func(root, key string) {
		_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			if p != root && config.SkipSourceDir(d.Name()) {
				return filepath.SkipDir
			}
			mu.Lock()
			_, already := dirKey[p]
			mu.Unlock()
			if already {
				return nil
			}
			if err := w.Add(p); err != nil {
				logger.Error("failed to watch source dir", "path", p, "err", err)
				return nil
			}
			mu.Lock()
			dirKey[p] = key
			mu.Unlock()
			return nil
		})
	}

	scan := func() {
		for _, t := range getTargets() {
			for _, dir := range t.Dirs {
				addTree(dir, t.Key)
			}
		}
	}
	scan()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			scan()

		case event, ok := <-w.Events:
			if !ok {
				return nil
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}
			// A new directory created under a watched tree must be watched too, so
			// edits inside it count from now on.
			if event.Op&fsnotify.Create != 0 {
				if st, statErr := os.Stat(event.Name); statErr == nil && st.IsDir() &&
					!config.SkipSourceDir(filepath.Base(event.Name)) {
					mu.Lock()
					parentKey := dirKey[filepath.Dir(event.Name)]
					mu.Unlock()
					if parentKey != "" {
						addTree(event.Name, parentKey)
					}
				}
			}
			mu.Lock()
			key := dirKey[filepath.Dir(event.Name)]
			if key != "" {
				if t, exists := timers[key]; exists {
					t.Reset(debounce)
				} else {
					k := key
					timers[k] = time.AfterFunc(debounce, func() {
						onActivity(k)
						mu.Lock()
						delete(timers, k)
						mu.Unlock()
					})
				}
			}
			mu.Unlock()

		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			logger.Error("source watcher error", "err", err)
		}
	}
}
