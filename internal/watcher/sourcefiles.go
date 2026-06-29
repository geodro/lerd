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

// maxSourceDirEntries caps how many direct entries a directory may have before
// the source watcher skips it. fsnotify's macOS (kqueue) backend opens a file
// descriptor per file in every watched directory, so a generated or vendored
// asset dump checked into a source root (e.g. a Font Awesome icon set with
// thousands of SVGs) can exhaust the per-process fd limit. Such a directory is
// not hand-edited source, so skipping it costs nothing for activity detection
// while keeping fd use bounded. Real source dirs hold tens to a few hundred
// files; this is far above that and far below an asset dump.
const maxSourceDirEntries = 2000

// maxWatchedSourceFiles is a hard ceiling on the files the source watcher will
// register across every target, a backstop under the per-directory cap so no
// combination of trees can drive the watcher into EMFILE (kern.maxfilesperproc
// is ~92k on macOS). Once reached, further trees are left to the nginx access
// feed for activity, which is the primary signal anyway.
const maxWatchedSourceFiles = 32768

// WatchSourceFiles watches each target's source directories recursively and
// calls onActivity(key), debounced per key, when a source file under them is
// written, created, or renamed — i.e. when you save while coding. Heavy or
// generated subtrees (node_modules, vendor, hidden dirs, ...) are never
// descended, so the watch set stays small, which also keeps macOS kqueue
// descriptor use bounded. Targets are re-scanned periodically so new
// sites/worktrees and newly-created subdirectories are picked up. It runs until
// stop is closed (idle-suspend disabled), then releases all fsnotify watches.
func WatchSourceFiles(getTargets func() []SourceTarget, debounce time.Duration, onActivity func(key string), stop <-chan struct{}) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()

	var mu sync.Mutex
	dirKey := map[string]string{}      // watched directory → activity key
	timers := map[string]*time.Timer{} // pending debounce timer per key
	skipped := map[string]bool{}       // oversized dirs already logged as skipped
	watchedFiles := 0                  // running estimate of fds the watch set holds

	// addTree recursively watches root and its subdirs (skipping excludes and
	// oversized asset dumps), tagging each watched directory with key. It bounds
	// the total files watched so a pathological tree can't exhaust the process fd
	// limit (see maxSourceDirEntries / maxWatchedSourceFiles).
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
			entries, derr := os.ReadDir(p)
			if derr != nil {
				return nil
			}
			// A directory dense enough to be a generated/vendored asset dump is
			// not source: skip it (and its subtree) so kqueue's per-file fds don't
			// pile up. Log once per dir so 30s rescans don't spam.
			if len(entries) > maxSourceDirEntries {
				mu.Lock()
				if !skipped[p] {
					skipped[p] = true
					logger.Warn("source watcher: skipping oversized dir (assets, not source)", "path", p, "entries", len(entries))
				}
				mu.Unlock()
				return filepath.SkipDir
			}
			files := 0
			for _, e := range entries {
				if !e.IsDir() {
					files++
				}
			}
			mu.Lock()
			if watchedFiles+files > maxWatchedSourceFiles {
				mu.Unlock()
				// Hard fd budget reached: stop adding trees this pass. The nginx
				// access feed still drives activity for the unwatched remainder.
				return filepath.SkipAll
			}
			mu.Unlock()
			if err := w.Add(p); err != nil {
				logger.Error("failed to watch source dir", "path", p, "err", err)
				return nil
			}
			mu.Lock()
			dirKey[p] = key
			watchedFiles += files
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
		case <-stop:
			return nil

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
