// Package services provides a platform-agnostic abstraction over the
// underlying service manager (systemd on Linux, launchd on macOS).
//
// All service lifecycle operations and unit file management go through the
// package-level Mgr variable, which is set to the appropriate implementation
// at init time via build-tag-selected files.
package services

// ServiceManager is the interface for managing lerd's user-space services and
// container units. On Linux it is backed by systemd + Podman Quadlets; on
// macOS it will be backed by launchd.
type ServiceManager interface {
	// --- Service unit files (lerd-watcher, lerd-ui, lerd-queue-*, …) ---

	// WriteServiceUnit writes a named service unit file.
	WriteServiceUnit(name, content string) error

	// WriteServiceUnitIfChanged writes the unit file only when the content has
	// changed. Returns true if the file was written (caller should DaemonReload).
	WriteServiceUnitIfChanged(name, content string) (bool, error)

	// RemoveServiceUnit removes the unit file for the named service.
	RemoveServiceUnit(name string) error

	// WriteTimerUnitIfChanged writes a timer unit file (paired with a
	// matching oneshot service) only when the content has changed. Used
	// for scheduled framework workers like Laravel 10's `schedule:run`,
	// which is a one-shot cron-style command rather than a daemon and
	// would otherwise restart-loop under the always-on worker model.
	// On platforms that lack a native timer concept the implementation
	// may be a no-op that returns (false, nil).
	WriteTimerUnitIfChanged(name, content string) (bool, error)

	// RemoveTimerUnit removes the timer unit file for the named worker.
	// May be a no-op on platforms without timer support.
	RemoveTimerUnit(name string) error

	// ListTimerUnits returns timer unit names whose files match nameGlob,
	// each suffixed with `.timer` (so callers can pass them straight to
	// systemctl without ambiguity against the sibling .service unit).
	// Platforms without native timer support return an empty slice.
	ListTimerUnits(nameGlob string) []string

	// ListServiceUnits returns unit names whose files match nameGlob.
	// e.g. nameGlob="lerd-queue-*" → ["lerd-queue-myapp", …]
	ListServiceUnits(nameGlob string) []string

	// --- Container unit files (lerd-dns, lerd-nginx, lerd-php*-fpm, …) ---

	// WriteContainerUnit writes a named container unit file.
	WriteContainerUnit(name, content string) error

	// ContainerUnitInstalled returns true if the container unit file exists.
	ContainerUnitInstalled(name string) bool

	// RemoveContainerUnit removes the container unit file for the named unit.
	RemoveContainerUnit(name string) error

	// ListContainerUnits returns container unit names whose files match nameGlob.
	// e.g. nameGlob="lerd-*" → ["lerd-dns", "lerd-nginx", …]
	ListContainerUnits(nameGlob string) []string

	// --- Service lifecycle ---

	// DaemonReload reloads the service manager configuration.
	DaemonReload() error

	// Start starts a named unit.
	Start(name string) error

	// Stop stops a named unit.
	Stop(name string) error

	// Restart restarts a named unit.
	Restart(name string) error

	// Enable marks a named unit to start at login.
	Enable(name string) error

	// Disable removes a named unit from the login start set.
	Disable(name string) error

	// IsActive returns true if the named unit is currently running.
	IsActive(name string) bool

	// IsEnabled returns true if the named unit is set to start at login.
	IsEnabled(name string) bool

	// UnitStatus returns the active state string (e.g. "active", "inactive", "failed").
	UnitStatus(name string) (string, error)
}

// Mgr is the package-level ServiceManager instance. It is initialised at
// program start by the platform-specific init() in systemd_linux.go or
// stub_darwin.go.
var Mgr ServiceManager
