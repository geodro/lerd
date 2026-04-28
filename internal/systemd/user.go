package systemd

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/geodro/lerd/internal/config"
)

// WriteService writes a systemd user service unit file.
func WriteService(name, content string) error {
	dir := config.SystemdUserDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dir, name+".service")
	return os.WriteFile(path, []byte(content), 0644)
}

// WriteServiceIfChanged writes the unit file only when the content differs from
// what is already on disk. Returns true if the file was written (caller should
// run daemon-reload), false if it was unchanged (daemon-reload not needed).
func WriteServiceIfChanged(name, content string) (bool, error) {
	dir := config.SystemdUserDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false, err
	}
	path := filepath.Join(dir, name+".service")
	if existing, err := os.ReadFile(path); err == nil && string(existing) == content {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(content), 0644)
}

// WriteTimerIfChanged writes a systemd user timer unit file when its
// content differs from what is already on disk. Returns true when the
// file was written so the caller knows to run daemon-reload.
func WriteTimerIfChanged(name, content string) (bool, error) {
	dir := config.SystemdUserDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false, err
	}
	path := filepath.Join(dir, name+".timer")
	if existing, err := os.ReadFile(path); err == nil && string(existing) == content {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(content), 0644)
}

// RemoveTimer removes a systemd user timer unit file.
func RemoveTimer(name string) error {
	path := filepath.Join(config.SystemdUserDir(), name+".timer")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// EnableService enables a systemd user service.
func EnableService(name string) error { return DBusEnableService(name) }

// StartService starts a systemd user service.
func StartService(name string) error { return DBusStartUnit(name) }

// DisableService disables a systemd user service.
func DisableService(name string) error { return DBusDisableService(name) }

// RestartService restarts a systemd user service.
func RestartService(name string) error { return DBusRestartUnit(name) }

// IsServiceEnabled returns true if the systemd user service is enabled.
func IsServiceEnabled(name string) bool { return DBusIsEnabled(name) }

// IsServiceActive returns true if the systemd user service is currently active.
func IsServiceActive(name string) bool { return DBusActiveState(name) == "active" }

// IsServiceActiveOrRestarting returns true if the service is active or in a
// restart loop (activating). Used to detect workers that should be stopped on unlink.
func IsServiceActiveOrRestarting(name string) bool {
	state := DBusActiveState(name)
	return state == "active" || state == "activating"
}

// IsTimerActive returns true if the worker's sibling .timer is active.
func IsTimerActive(name string) bool { return DBusActiveState(name+".timer") == "active" }

// FindOrphanedWorkers scans systemd unit files for worker units belonging to
// the given site that are running but not present in the known workers set.
func FindOrphanedWorkers(siteName string, known map[string]bool) []string {
	suffix := "-" + siteName + ".service"
	prefix := "lerd-"
	entries, err := os.ReadDir(config.SystemdUserDir())
	if err != nil {
		return nil
	}
	var orphans []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			continue
		}
		workerName := strings.TrimPrefix(name, prefix)
		workerName = strings.TrimSuffix(workerName, suffix)
		if workerName == "" {
			continue
		}
		// Skip non-worker units.
		switch workerName {
		case "php84-fpm", "php83-fpm", "php82-fpm", "php81-fpm", "php80-fpm",
			"nginx", "dns", "dns-forwarder", "watcher", "ui", "stripe":
			continue
		}
		if known[workerName] {
			continue
		}
		unitName := strings.TrimSuffix(name, ".service")
		if IsServiceActiveOrRestarting(unitName) {
			orphans = append(orphans, workerName)
		}
	}
	sort.Strings(orphans)
	return orphans
}
