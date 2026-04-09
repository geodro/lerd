package podman

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
)

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
	content = StripInstallSection(content, autostartDisabled)
	path := filepath.Join(dir, name+".container")
	if existing, err := os.ReadFile(path); err == nil && string(existing) == content {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(content), 0644)
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
	_ = exec.Command("podman", "rm", "-f", name).Run()
}

// DaemonReload runs systemctl --user daemon-reload.
func DaemonReload() error {
	cmd := exec.Command("systemctl", "--user", "daemon-reload")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("daemon-reload failed: %w\n%s", err, out)
	}
	return nil
}

// StartUnit starts a systemd user unit.
func StartUnit(name string) error {
	cmd := exec.Command("systemctl", "--user", "start", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("start %s failed: %w\n%s", name, err, out)
	}
	return nil
}

// StopUnit stops a systemd user unit.
func StopUnit(name string) error {
	cmd := exec.Command("systemctl", "--user", "stop", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("stop %s failed: %w\n%s", name, err, out)
	}
	// Clear any failed state so the unit shows as inactive rather than failed.
	_ = exec.Command("systemctl", "--user", "reset-failed", name).Run()
	return nil
}

// RestartUnit restarts a systemd user unit.
func RestartUnit(name string) error {
	cmd := exec.Command("systemctl", "--user", "restart", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restart %s failed: %w\n%s", name, err, out)
	}
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
			cmd := exec.Command("podman", "exec", "lerd-mysql",
				"mysqladmin", "ping", "-uroot", "-plerd", "--silent")
			return cmd.Run() == nil
		}
	case "postgres":
		probe = func() bool {
			cmd := exec.Command("podman", "exec", "lerd-postgres",
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

// UnitStatus returns the active state of a systemd user unit.
func UnitStatus(name string) (string, error) {
	cmd := exec.Command("systemctl", "--user", "is-active", name)
	out, err := cmd.Output()
	status := strings.TrimSpace(string(out))
	if status == "" {
		if err != nil {
			return "unknown", nil
		}
	}
	return status, nil
}
