//go:build linux

package systemd

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-systemd/v22/daemon"
	"github.com/coreos/go-systemd/v22/dbus"
)

// userBus holds the lazily-initialised systemd user bus connection. Long-lived:
// the library handles reconnection internally and the process lifetime is the
// natural owner. sync.Once guards the first-dial race.
var (
	userBusOnce sync.Once
	userBusConn *dbus.Conn
	userBusErr  error
)

func userConn() (*dbus.Conn, error) {
	userBusOnce.Do(func() {
		// Dial context must not be cancellable: go-systemd ties the
		// conn lifetime to this ctx, so cancelling it invalidates the
		// underlying socket for every subsequent op in this process.
		userBusConn, userBusErr = dbus.NewUserConnectionContext(context.Background())
	})
	return userBusConn, userBusErr
}

// dbusUnitOp runs one of StartUnit / StopUnit / RestartUnit / ReloadUnit by
// name and waits for systemd to report the result on the internal channel.
// Returns an error whose message mirrors the old systemctl shell-out for
// drop-in compatibility with existing error strings.
func dbusUnitOp(op, verb, name string) error {
	conn, err := userConn()
	if err != nil {
		return fmt.Errorf("%s %s: dbus connect: %w", verb, name, err)
	}
	ch := make(chan string, 1)
	unit := withServiceSuffix(name)
	var jobID int
	var opErr error
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	switch op {
	case "start":
		jobID, opErr = conn.StartUnitContext(ctx, unit, "replace", ch)
	case "stop":
		jobID, opErr = conn.StopUnitContext(ctx, unit, "replace", ch)
	case "restart":
		jobID, opErr = conn.RestartUnitContext(ctx, unit, "replace", ch)
	default:
		return fmt.Errorf("unknown unit op %q", op)
	}
	_ = jobID
	if opErr != nil {
		return fmt.Errorf("%s %s failed: %w", verb, name, opErr)
	}
	select {
	case result := <-ch:
		if result != "done" {
			return fmt.Errorf("%s %s failed: %s", verb, name, result)
		}
	case <-ctx.Done():
		return fmt.Errorf("%s %s timed out after 30s", verb, name)
	}
	return nil
}

// DBusDaemonReload runs systemctl --user daemon-reload over DBus.
func DBusDaemonReload() error {
	conn, err := userConn()
	if err != nil {
		return fmt.Errorf("daemon-reload: dbus connect: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := conn.ReloadContext(ctx); err != nil {
		return fmt.Errorf("daemon-reload failed: %w", err)
	}
	return nil
}

// DBusStartUnit starts a user unit via DBus and waits for the job to finish.
func DBusStartUnit(name string) error {
	_ = DBusResetFailed(name)
	return dbusUnitOp("start", "start", name)
}

// DBusStopUnit stops a user unit via DBus.
func DBusStopUnit(name string) error {
	if err := dbusUnitOp("stop", "stop", name); err != nil {
		return err
	}
	_ = DBusResetFailed(name)
	return nil
}

// DBusRestartUnit restarts a user unit via DBus.
func DBusRestartUnit(name string) error {
	return dbusUnitOp("restart", "restart", name)
}

// DBusResetFailed clears any "failed" state for the named unit so the next
// start is not blocked by Restart= rate-limits.
func DBusResetFailed(name string) error {
	conn, err := userConn()
	if err != nil {
		return err
	}
	return conn.ResetFailedUnitContext(context.Background(), withServiceSuffix(name))
}

// DBusEnableService marks a user service to start at login.
func DBusEnableService(name string) error {
	conn, err := userConn()
	if err != nil {
		return fmt.Errorf("enable %s: dbus connect: %w", name, err)
	}
	_, _, err = conn.EnableUnitFilesContext(
		context.Background(),
		[]string{withServiceSuffix(name)},
		false, true,
	)
	if err != nil {
		return fmt.Errorf("enable %s: %w", name, err)
	}
	return nil
}

// DBusDisableService removes a user service from the login start set.
func DBusDisableService(name string) error {
	conn, err := userConn()
	if err != nil {
		return fmt.Errorf("disable %s: dbus connect: %w", name, err)
	}
	if _, err := conn.DisableUnitFilesContext(
		context.Background(),
		[]string{withServiceSuffix(name)},
		false,
	); err != nil {
		return fmt.Errorf("disable %s: %w", name, err)
	}
	return nil
}

// DBusActiveState returns the ActiveState property ("active", "inactive",
// "failed", "activating", …) for the named unit, or "" when the unit is
// unknown. Unit name may be bare (e.g. "lerd-foo") or fully-qualified
// ("lerd-foo.service", "lerd-foo.timer").
func DBusActiveState(name string) string {
	conn, err := userConn()
	if err != nil {
		return ""
	}
	props, err := conn.GetUnitPropertiesContext(context.Background(), withDefaultSuffix(name))
	if err != nil {
		return ""
	}
	s, _ := props["ActiveState"].(string)
	return s
}

// DBusIsEnabled returns true when the unit-file state resolves to "enabled".
func DBusIsEnabled(name string) bool {
	conn, err := userConn()
	if err != nil {
		return false
	}
	props, err := conn.GetUnitPropertiesContext(context.Background(), withServiceSuffix(name))
	if err != nil {
		return false
	}
	s, _ := props["UnitFileState"].(string)
	return s == "enabled"
}

// withServiceSuffix ensures the unit name ends in ".service" which DBus
// requires for enable/disable and for unit-property lookups. Bare names are
// what callers pass today when they shell out to systemctl.
func withServiceSuffix(name string) string {
	if strings.Contains(name, ".") {
		return name
	}
	return name + ".service"
}

// withDefaultSuffix keeps an explicit .timer / .service suffix when the
// caller passed one, and otherwise assumes .service. Used by property
// lookups where a bare name could legitimately refer to either unit type.
func withDefaultSuffix(name string) string {
	if strings.Contains(name, ".") {
		return name
	}
	return name + ".service"
}

// NotifyReady tells systemd the current process has finished its startup
// work and is ready to serve. Used by Type=notify units so systemctl start
// blocks until the service is actually up, not just spawned. No-op outside
// a systemd-managed process (returns false without error).
func NotifyReady() {
	_, _ = daemon.SdNotify(false, daemon.SdNotifyReady)
}

// NotifyStopping tells systemd the process is winding down, letting
// dependent units start their own teardown early instead of waiting for
// the process to actually exit.
func NotifyStopping() {
	_, _ = daemon.SdNotify(false, daemon.SdNotifyStopping)
}
