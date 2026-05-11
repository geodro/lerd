package watcher

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// inFlightWorktrees serializes handleNewEntry per (sitePath, name). Git
// worktree add fires multiple fsnotify events (worktrees/ create, entry
// create, gitdir write) so without dedup three to four goroutines race
// onAdded → composer install on the same vendor/ tree and corrupt the
// extraction by clobbering each other's vendor/composer/tmp-*.zip files.
var inFlightWorktrees sync.Map

// WatchWorktrees monitors the .git/worktrees/ directory for each site returned
// by getSites and calls onAdded/onRemoved when entries appear or disappear.
// It calls onChanged when worktree metadata changes, such as HEAD being
// rewritten by a branch rename.
// It also watches .git/ itself so it can re-attach to .git/worktrees/ if it is
// deleted (last worktree removed) and then re-created (first new worktree added).
// It re-polls getSites every 30 seconds to pick up newly registered sites.
func WatchWorktrees(
	getSites func() []string,
	onAdded func(sitePath, name string),
	onChanged func(sitePath, name string),
	onRemoved func(sitePath, name string),
) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()

	// siteForGitDir maps <site>/.git/ → site path (always watched)
	siteForGitDir := map[string]string{}
	// siteForWorktreesDir maps <site>/.git/worktrees/ → site path
	siteForWorktreesDir := map[string]string{}
	// siteForEntryDir maps <site>/.git/worktrees/<name>/ → site path
	siteForEntryDir := map[string]string{}

	addWorktreesWatch := func(sitePath string) {
		worktreesDir := filepath.Join(sitePath, ".git", "worktrees")
		if _, already := siteForWorktreesDir[worktreesDir]; already {
			return
		}
		if _, err := os.Stat(worktreesDir); err != nil {
			return
		}
		if err := w.Add(worktreesDir); err == nil {
			siteForWorktreesDir[worktreesDir] = sitePath
		}
		entries, _ := os.ReadDir(worktreesDir)
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			entryDir := filepath.Join(worktreesDir, e.Name())
			if _, already := siteForEntryDir[entryDir]; already {
				continue
			}
			if err := w.Add(entryDir); err == nil {
				siteForEntryDir[entryDir] = sitePath
			}
		}
	}

	addSite := func(sitePath string) {
		gitDir := filepath.Join(sitePath, ".git")
		if _, already := siteForGitDir[gitDir]; already {
			return
		}
		if info, err := os.Stat(gitDir); err != nil || !info.IsDir() {
			return
		}
		if err := w.Add(gitDir); err == nil {
			siteForGitDir[gitDir] = sitePath
		}
		addWorktreesWatch(sitePath)
	}

	for _, sitePath := range getSites() {
		addSite(sitePath)
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			for _, sitePath := range getSites() {
				addSite(sitePath)
			}

		case event, ok := <-w.Events:
			if !ok {
				return nil
			}
			dir := filepath.Dir(event.Name)

			// Event inside .git/ — watch for worktrees/ being (re-)created.
			if sitePath, known := siteForGitDir[dir]; known {
				if filepath.Base(event.Name) == "worktrees" && event.Op&fsnotify.Create != 0 {
					addWorktreesWatch(sitePath)
					// Git may have already written entries by the time we receive
					// this event (race: worktrees/ created and populated before our
					// watch fires). Scan immediately.
					worktreesDir := event.Name
					entries, _ := os.ReadDir(worktreesDir)
					for _, e := range entries {
						if !e.IsDir() {
							continue
						}
						entryDir := filepath.Join(worktreesDir, e.Name())
						name := e.Name()
						if _, already := siteForEntryDir[entryDir]; !already {
							if err := w.Add(entryDir); err == nil {
								siteForEntryDir[entryDir] = sitePath
							}
						}
						go handleNewEntry(entryDir, sitePath, name, onAdded)
					}
				}
				continue
			}

			// Event inside .git/worktrees/ — new entry dir created or removed.
			if sitePath, known := siteForWorktreesDir[dir]; known {
				name := filepath.Base(event.Name)
				switch {
				case event.Op&fsnotify.Create != 0:
					entryDir := event.Name
					info, err := os.Stat(entryDir)
					if err != nil || !info.IsDir() {
						continue
					}
					// Watch the entry dir for the case gitdir isn't written yet.
					if _, already := siteForEntryDir[entryDir]; !already {
						if err := w.Add(entryDir); err == nil {
							siteForEntryDir[entryDir] = sitePath
						}
					}
					// Also immediately try: git may have already finished writing
					// by the time we receive the Create event.
					go handleNewEntry(entryDir, sitePath, name, onAdded)

				case event.Op&fsnotify.Remove != 0:
					onRemoved(sitePath, name)
					// If the worktrees dir itself was deleted (fsnotify fires a
					// Remove for the watched dir), remove it from our map so we
					// can re-watch it when git re-creates it.
					delete(siteForWorktreesDir, event.Name)
				}
				continue
			}

			// Event inside .git/worktrees/<name>/ — gitdir written after we
			// already set up the watch (slow git or large checkout).
			if sitePath, known := siteForEntryDir[dir]; known {
				base := filepath.Base(event.Name)
				if base == "gitdir" && event.Op&(fsnotify.Create|fsnotify.Write) != 0 {
					name := filepath.Base(dir)
					go handleNewEntry(dir, sitePath, name, onAdded)
				} else if base == "HEAD" && event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) != 0 {
					onChanged(sitePath, filepath.Base(dir))
				}
				continue
			}

		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			logger.Error("fsnotify error", "err", err)
		}
	}
}

// handleNewEntry waits for the gitdir file to appear in entryDir, reads the
// checkout path from it, then polls until the checkout directory exists AND
// HEAD has been written with a real ref/SHA before calling onAdded. The HEAD
// poll closes a race where fsnotify fires Create on the entry dir before git
// has finalised HEAD: lerd would otherwise read an empty HEAD, treat the
// worktree as detached, and write a `detached.<site>.conf` vhost that
// shadows the eventual `<branch>.<site>.conf`.
//
// Concurrent invocations for the same (sitePath, name) are deduped: the
// first goroutine claims the in-flight slot and runs to completion; the
// rest return immediately. Late HEAD changes after the first goroutine
// returned are caught by the entryDir HEAD watcher (onChanged path).
func handleNewEntry(entryDir, sitePath, name string, onAdded func(string, string)) {
	key := sitePath + "\x00" + name
	if _, busy := inFlightWorktrees.LoadOrStore(key, struct{}{}); busy {
		return
	}
	defer inFlightWorktrees.Delete(key)

	// Wait up to 5s for gitdir to be written.
	gitdirPath := filepath.Join(entryDir, "gitdir")
	var checkoutPath string
	for i := 0; i < 10; i++ {
		cp := checkoutPathFromGitdir(gitdirPath)
		if cp != "" {
			checkoutPath = cp
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if checkoutPath == "" {
		logger.Warn("timed out waiting for gitdir in worktree entry", "entry", entryDir)
		return
	}
	// Wait up to 10s for the checkout directory to be fully created.
	checkoutReady := false
	for i := 0; i < 20; i++ {
		if _, err := os.Stat(checkoutPath); err == nil {
			checkoutReady = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !checkoutReady {
		logger.Warn("timed out waiting for worktree checkout directory", "path", checkoutPath)
		return
	}
	// Wait up to 5s for HEAD to be a non-empty ref or SHA.
	headPath := filepath.Join(entryDir, "HEAD")
	for i := 0; i < 10; i++ {
		data, err := os.ReadFile(headPath)
		if err == nil {
			line := strings.TrimSpace(string(data))
			if strings.HasPrefix(line, "ref: refs/heads/") || len(line) >= 7 {
				onAdded(sitePath, name)
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	// HEAD never settled; call onAdded anyway so we don't lose the worktree
	// entirely — vhost will use whatever readBranch returns (detached) and
	// the user can resync after.
	logger.Warn("timed out waiting for HEAD to settle in worktree entry", "entry", entryDir)
	onAdded(sitePath, name)
}

// checkoutPathFromGitdir reads the gitdir file and returns the checkout path.
func checkoutPathFromGitdir(gitdirPath string) string {
	data, err := os.ReadFile(gitdirPath)
	if err != nil {
		return ""
	}
	gitFile := strings.TrimSpace(string(data))
	if !filepath.IsAbs(gitFile) {
		gitFile = filepath.Join(filepath.Dir(gitdirPath), gitFile)
	}
	return filepath.Dir(filepath.Clean(gitFile))
}
