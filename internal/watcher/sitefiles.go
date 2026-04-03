package watcher

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// watchedSiteFiles lists the file names in a site root that trigger queue:restart.
var watchedSiteFiles = map[string]bool{
	".env":          true,
	".lerd.yaml":    true,
	"composer.json": true,
	"composer.lock": true,
	".php-version":  true,
	".node-version": true,
	".nvmrc":        true,
}

// WatchSiteFiles monitors key config files in each site directory returned by
// getSites. onChanged is called at most once per debounce period per site when
// any of the watched files are written or replaced.
func WatchSiteFiles(getSites func() []string, debounce time.Duration, onChanged func(sitePath string)) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()

	// watched tracks directories already added to fsnotify.
	watched := map[string]bool{}

	var mu sync.Mutex
	// timers holds a pending debounce timer per site path.
	timers := map[string]*time.Timer{}
	// fileToSite maps absolute file path → site path.
	fileToSite := map[string]string{}

	addSite := func(sitePath string) {
		if watched[sitePath] {
			return
		}
		if err := w.Add(sitePath); err != nil {
			logger.Error("failed to watch site directory", "path", sitePath, "err", err)
			return
		}
		watched[sitePath] = true
		logger.Debug("watching site directory", "path", sitePath)
		for name := range watchedSiteFiles {
			fileToSite[filepath.Join(sitePath, name)] = sitePath
		}
	}

	for _, s := range getSites() {
		addSite(s)
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			for _, s := range getSites() {
				addSite(s)
			}

		case event, ok := <-w.Events:
			if !ok {
				return nil
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}
			sitePath, ok := fileToSite[event.Name]
			if !ok {
				continue
			}
			mu.Lock()
			if t, exists := timers[sitePath]; exists {
				t.Reset(debounce)
			} else {
				sp := sitePath
				timers[sp] = time.AfterFunc(debounce, func() {
					onChanged(sp)
					mu.Lock()
					delete(timers, sp)
					mu.Unlock()
				})
			}
			mu.Unlock()

		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			logger.Error("fsnotify error", "err", err)
		}
	}
}
