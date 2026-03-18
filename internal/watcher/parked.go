package watcher

import (
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

// Watch monitors the given directories for new and deleted project subdirectories.
// onNew is called when an artisan file appears in a subdirectory.
// onRemoved is called when a watched subdirectory is deleted.
func Watch(dirs []string, onNew func(path string), onRemoved func(path string)) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()

	for _, dir := range dirs {
		expanded := expandHome(dir)
		if err := os.MkdirAll(expanded, 0755); err != nil {
			continue
		}
		if err := w.Add(expanded); err != nil {
			continue
		}
		// Also watch existing subdirectories so we catch artisan creation inside them.
		entries, _ := os.ReadDir(expanded)
		for _, e := range entries {
			if e.IsDir() {
				_ = w.Add(filepath.Join(expanded, e.Name()))
			}
		}
	}

	for {
		select {
		case event, ok := <-w.Events:
			if !ok {
				return nil
			}
			switch {
			case event.Op&fsnotify.Remove != 0:
				onRemoved(event.Name)
			case event.Op&(fsnotify.Create|fsnotify.Write) != 0:
				if filepath.Base(event.Name) == "artisan" {
					onNew(filepath.Dir(event.Name))
				} else if event.Op&fsnotify.Create != 0 {
					// New subdirectory in a parked dir — watch it for artisan.
					if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
						_ = w.Add(event.Name)
					}
				}
			}
		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			_ = err // log if needed
		}
	}
}

func expandHome(path string) string {
	if len(path) > 1 && path[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
