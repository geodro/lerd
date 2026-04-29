package podman

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/systemd"
)

// quadletReloadPending records that a previous DaemonReloadIfNeeded call
// failed without being retried. The next caller forces a reload even when
// nothing else changed so a transient DBus failure does not leave
// systemd's cache stale until an external trigger heals it.
var quadletReloadPending atomic.Bool

// DaemonReloadIfNeeded reloads systemd when the caller wrote new quadlet
// content (changed=true) or when a previous reload failed and was never
// retried. Failures set a sticky flag so the next caller forces the
// retry; success clears it.
func DaemonReloadIfNeeded(changed bool) error {
	if !changed && !quadletReloadPending.Load() {
		return nil
	}
	if err := DaemonReloadFn(); err != nil {
		quadletReloadPending.Store(true)
		return err
	}
	quadletReloadPending.Store(false)
	return nil
}

// WriteQuadlet writes a Podman quadlet container unit file. Before writing
// it applies BindForLAN to rewrite PublishPort= lines according to the
// current cfg.LAN.Exposed setting. This is done centrally here so callers
// (install, services, MCP server, custom-service generator) all get the
// same loopback-by-default treatment without each having to remember.
func WriteQuadlet(name, content string) error {
	_, err := WriteQuadletDiff(name, content)
	return err
}

// WriteQuadletDiff writes a quadlet like WriteQuadlet, but also reports
// whether the on-disk file actually changed. Callers can use this to
// daemon-reload + restart only the units that need it (e.g. lerd install
// rewriting binds from 0.0.0.0 to 127.0.0.1 when migrating to a build
// where lan:expose defaults to off — without a restart the running
// container would silently keep its old bind).
func WriteQuadletDiff(name, content string) (changed bool, err error) {
	dir := config.QuadletDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false, err
	}
	lanExposed := false
	autostartDisabled := false
	if cfg, err := config.LoadGlobal(); err == nil && cfg != nil {
		lanExposed = cfg.LAN.Exposed
		autostartDisabled = cfg.Autostart.Disabled
	}
	content = BindForLAN(content, lanExposed)
	content = PairIPv6Binds(content)
	content = StripInstallSection(content, autostartDisabled)
	path := filepath.Join(dir, name+".container")
	fileChanged := true
	if existing, err := os.ReadFile(path); err == nil && string(existing) == content {
		fileChanged = false
	}
	if fileChanged {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return false, err
		}
	}
	// Always sync the platform unit (e.g. macOS launchd plist) so it stays
	// consistent with the .container file — even if the file didn't change,
	// the plist may be stale (e.g. after a config change like LAN exposure).
	if AfterQuadletWriteFn != nil {
		if err := AfterQuadletWriteFn(name, content); err != nil {
			return fileChanged, err
		}
	}
	return fileChanged, nil
}

// QuadletInstalled returns true if a quadlet .container file exists for the given unit name.
func QuadletInstalled(name string) bool {
	path := filepath.Join(config.QuadletDir(), name+".container")
	_, err := os.Stat(path)
	return err == nil
}

// RemoveQuadlet removes a Podman quadlet container unit file.
func RemoveQuadlet(name string) error {
	path := filepath.Join(config.QuadletDir(), name+".container")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// RemoveContainer removes a stopped Podman container by name, ignoring errors
// if the container does not exist.
func RemoveContainer(name string) {
	_ = exec.Command(PodmanBin(), "rm", "-f", name).Run()
}

// AfterQuadletWriteFn, if non-nil, is called by WriteQuadletDiff after
// writing the .container file. On macOS it is set to the launchd plist
// writer so both formats stay in sync (the .container file is the
// canonical source of truth; the plist is the live runtime unit).
var AfterQuadletWriteFn func(name, content string) error

// UnitLifecycle is the interface for starting, stopping, restarting, and
// querying service units. Set by the platform service manager on macOS so that
// StartUnit/StopUnit/RestartUnit/UnitStatus route through launchd instead of
// systemctl. Nil on Linux (the systemctl fallback is used).
var UnitLifecycle interface {
	Start(name string) error
	Stop(name string) error
	Restart(name string) error
	UnitStatus(name string) (string, error)
}

// DaemonReload runs the equivalent of systemctl --user daemon-reload.
// On Linux it goes through systemd DBus. On macOS the DBus stub returns
// a sentinel and we fall through to the historical shell-out so launchd
// users still get the legacy path (a no-op for non-systemd systems).
func DaemonReload() error {
	if err := systemd.DBusDaemonReload(); err == nil {
		return nil
	}
	cmd := exec.Command("systemctl", "--user", "daemon-reload")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("daemon-reload failed: %w\n%s", err, out)
	}
	return nil
}

// StartUnit starts a service unit. On Linux it first clears any lingering
// failed state from a previous run so that units which hit Restart=
// rate-limit (e.g. workers that raced container readiness in a buggy
// upgrade) recover automatically on the next `lerd start` instead of
// staying stuck in `failed`.
// AfterUnitChange is fired after every successful StartUnit / StopUnit /
// RestartUnit call. lerd-ui wires this at startup to invalidate the
// systemctl unit cache and publish "sites"/"services" events to the
// eventbus so every browser tab updates in real time — regardless of
// whether the mutation came from an HTTP handler, the CLI, the MCP
// server, or the file watcher. Nil by default so unit tests and binaries
// that don't run the UI don't pay the cost.
var AfterUnitChange func(name string)

// UnitOpDebug controls whether unit-lifecycle calls log a one-line caller
// trace. Defaults to on; set LERD_UNIT_OP_DEBUG=0 to disable. Cheap when
// off — runtime.Caller is only invoked when the flag is set. The log line
// is one line per unit op, so volume is low in normal operation but jumps
// out immediately during a "who keeps stopping FPM?" cascade.
var UnitOpDebug = os.Getenv("LERD_UNIT_OP_DEBUG") != "0"

func notifyUnitChange(name string) {
	InvalidateUnitStatusCache(name)
	if AfterUnitChange != nil {
		AfterUnitChange(name)
	}
}

func logUnitOp(action, unit string) {
	if !UnitOpDebug {
		return
	}
	caller := unitOpCaller()
	fmt.Fprintf(os.Stderr, "[lerd] unit-op action=%s unit=%s caller=%s\n", action, unit, caller)
}

// unitOpCaller returns the closest frame outside the podman package — that's
// the lerd-internal site that asked for the unit op. Falls back to "?" if
// the stack walk fails.
func unitOpCaller() string {
	pc := make([]uintptr, 16)
	n := runtime.Callers(3, pc)
	frames := runtime.CallersFrames(pc[:n])
	for {
		frame, more := frames.Next()
		if !strings.Contains(frame.Function, "geodro/lerd/internal/podman") {
			return fmt.Sprintf("%s (%s:%d)", frame.Function, filepath.Base(frame.File), frame.Line)
		}
		if !more {
			return frame.Function
		}
	}
}

func StartUnit(name string) error {
	logUnitOp("start", name)
	if UnitLifecycle != nil {
		err := UnitLifecycle.Start(name)
		if err == nil {
			notifyUnitChange(name)
		}
		return err
	}
	if err := systemd.DBusStartUnit(name); err != nil {
		return err
	}
	notifyUnitChange(name)
	return nil
}

// StopUnit stops a service unit.
func StopUnit(name string) error {
	logUnitOp("stop", name)
	if UnitLifecycle != nil {
		err := UnitLifecycle.Stop(name)
		if err == nil {
			notifyUnitChange(name)
		}
		return err
	}
	if err := systemd.DBusStopUnit(name); err != nil {
		return err
	}
	notifyUnitChange(name)
	return nil
}

// RestartUnit restarts a service unit.
func RestartUnit(name string) error {
	logUnitOp("restart", name)
	if UnitLifecycle != nil {
		err := UnitLifecycle.Restart(name)
		if err == nil {
			notifyUnitChange(name)
		}
		return err
	}
	if err := systemd.DBusRestartUnit(name); err != nil {
		return err
	}
	notifyUnitChange(name)
	return nil
}

// WaitReady polls until the named service is ready to accept connections, or
// timeout is reached. Readiness is tested by running a lightweight probe inside
// the container: mysqladmin ping for mysql, pg_isready for postgres. For other
// services it falls back to waiting until the systemd unit is "active".
func WaitReady(service string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	unit := "lerd-" + service

	var probe func() bool
	switch service {
	case "mysql":
		probe = func() bool {
			cmd := exec.Command(PodmanBin(), "exec", "lerd-mysql",
				"mysqladmin", "ping", "-uroot", "-plerd", "--silent")
			return cmd.Run() == nil
		}
	case "postgres":
		probe = func() bool {
			cmd := exec.Command(PodmanBin(), "exec", "lerd-postgres",
				"pg_isready", "-U", "postgres")
			return cmd.Run() == nil
		}
	case "rustfs":
		probe = func() bool {
			conn, err := net.DialTimeout("tcp", "localhost:9000", time.Second)
			if err != nil {
				return false
			}
			conn.Close()
			return true
		}
	default:
		probe = func() bool {
			status, _ := UnitStatus(unit)
			return status == "active"
		}
	}

	for time.Now().Before(deadline) {
		if probe() {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("%s did not become ready within %s", service, timeout)
}

// unitStatusCache memoises DBusActiveState calls for a short window so
// dashboard snapshot rebuilds don't issue 100+ DBus round-trips per refresh.
// 2 seconds is short enough that a unit toggle in lerd-ui (which runs the
// AfterUnitChange hook anyway) is reflected promptly, while long enough to
// absorb burst rebuilds during systemd state-change storms.
const unitStatusCacheTTL = 2 * time.Second

type unitStatusEntry struct {
	state string
	at    time.Time
}

var (
	unitStatusCacheMu sync.Mutex
	unitStatusCache   = map[string]unitStatusEntry{}
)

// InvalidateUnitStatusCache drops the cached DBus state for name. Called from
// AfterUnitChange so explicit mutations are visible to the next snapshot
// rebuild without waiting for the TTL.
func InvalidateUnitStatusCache(name string) {
	unitStatusCacheMu.Lock()
	delete(unitStatusCache, name)
	unitStatusCacheMu.Unlock()
}

// UnitStatus returns the active state of a service unit.
func UnitStatus(name string) (string, error) {
	if UnitLifecycle != nil {
		return UnitLifecycle.UnitStatus(name)
	}

	unitStatusCacheMu.Lock()
	if entry, ok := unitStatusCache[name]; ok && time.Since(entry.at) < unitStatusCacheTTL {
		state := entry.state
		unitStatusCacheMu.Unlock()
		return state, nil
	}
	unitStatusCacheMu.Unlock()

	state := systemd.DBusActiveState(name)
	if state == "" {
		state = "unknown"
	}

	unitStatusCacheMu.Lock()
	unitStatusCache[name] = unitStatusEntry{state: state, at: time.Now()}
	unitStatusCacheMu.Unlock()

	return state, nil
}
