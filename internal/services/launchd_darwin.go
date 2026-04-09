//go:build darwin

package services

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// launchctlExitCode returns the numeric exit code from a launchctl error,
// or -1 if err is nil or not an *exec.ExitError.
func launchctlExitCode(err error) int {
	var e *exec.ExitError
	if errors.As(err, &e) {
		return e.ExitCode()
	}
	return -1
}

// launchctl runs a launchctl command with a 15-second timeout so a throttled
// or unresponsive service can never hang lerd indefinitely.
func launchctl(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "launchctl", args...).CombinedOutput()
}

// uidDomain returns the launchd GUI domain for the current user, e.g. "gui/501".
func uidDomain() string {
	return fmt.Sprintf("gui/%d", os.Getuid())
}

func init() {
	Mgr = &darwinServiceManager{}
	// Override WriteFPMQuadlet to use launchd plists instead of systemd quadlets.
	podman.WriteContainerUnitFn = Mgr.WriteContainerUnit
	podman.AfterQuadletWriteFn = Mgr.WriteContainerUnit
	podman.DaemonReloadFn = Mgr.DaemonReload
	podman.StartUnitFn = Mgr.Start
	podman.StopUnitFn = Mgr.Stop
	podman.RestartUnitFn = Mgr.Restart
	podman.UnitStatusFn = Mgr.UnitStatus
	podman.SkipQuadletUpToDateCheck = true
}

type darwinServiceManager struct{}

// --- Path helpers ---

func launchAgentsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents")
}

func lerdLogsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Logs", "lerd")
}

func plistPath(name string) string {
	return filepath.Join(launchAgentsDir(), name+".plist")
}

func plistLabel(name string) string {
	return "com.lerd." + name
}

// --- Plist generation ---

func xmlEscStr(s string) string {
	var buf bytes.Buffer
	xml.EscapeText(&buf, []byte(s)) //nolint:errcheck
	return buf.String()
}

func buildPlist(lbl string, args []string, runAtLoad, keepAlive bool, stdoutPath, stderrPath string) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>`)
	sb.WriteString(xmlEscStr(lbl))
	sb.WriteString("</string>\n\t<key>ProgramArguments</key>\n\t<array>\n")
	for _, a := range args {
		sb.WriteString("\t\t<string>")
		sb.WriteString(xmlEscStr(a))
		sb.WriteString("</string>\n")
	}
	sb.WriteString("\t</array>\n")
	if runAtLoad {
		sb.WriteString("\t<key>RunAtLoad</key>\n\t<true/>\n")
	}
	if keepAlive {
		sb.WriteString("\t<key>KeepAlive</key>\n\t<true/>\n")
	}
	if stdoutPath != "" {
		sb.WriteString("\t<key>StandardOutPath</key>\n\t<string>")
		sb.WriteString(xmlEscStr(stdoutPath))
		sb.WriteString("</string>\n")
	}
	if stderrPath != "" {
		sb.WriteString("\t<key>StandardErrorPath</key>\n\t<string>")
		sb.WriteString(xmlEscStr(stderrPath))
		sb.WriteString("</string>\n")
	}
	sb.WriteString("</dict>\n</plist>\n")
	return sb.String()
}

func ensurePlistDirs(name string) error {
	if err := os.MkdirAll(launchAgentsDir(), 0755); err != nil {
		return err
	}
	return os.MkdirAll(lerdLogsDir(), 0755)
}

// --- INI / Quadlet parser ---

// parseSection returns key → []values for a named INI section in content.
func parseSection(content, section string) map[string][]string {
	result := map[string][]string{}
	inSection := false
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inSection = line[1:len(line)-1] == section
			continue
		}
		if !inSection {
			continue
		}
		if idx := strings.IndexByte(line, '='); idx >= 0 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			result[key] = append(result[key], val)
		}
	}
	return result
}

// expandSpecifiers replaces Quadlet path specifiers (%h → home dir).
func expandSpecifiers(s string) string {
	home, _ := os.UserHomeDir()
	return strings.ReplaceAll(s, "%h", home)
}

// podmanBinPath returns the path to the podman binary.
func podmanBinPath() string {
	if p, err := exec.LookPath("podman"); err == nil {
		return p
	}
	// Homebrew default locations
	for _, candidate := range []string{"/opt/homebrew/bin/podman", "/usr/local/bin/podman"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "podman"
}

// stripSELinuxVolOpts removes SELinux relabelling flags (:z, :Z) from a
// volume mount spec. On macOS with Podman Machine the source path is a
// virtiofs mount and SELinux relabelling is unsupported; passing :z causes
// the container to fail to start.
func stripSELinuxVolOpts(vol string) string {
	// volume format: src:dst[:opts]
	parts := strings.SplitN(vol, ":", 3)
	if len(parts) < 3 {
		return vol
	}
	opts := strings.Split(parts[2], ",")
	filtered := opts[:0]
	for _, o := range opts {
		if o != "z" && o != "Z" {
			filtered = append(filtered, o)
		}
	}
	if len(filtered) == 0 {
		return parts[0] + ":" + parts[1]
	}
	return parts[0] + ":" + parts[1] + ":" + strings.Join(filtered, ",")
}

// stripPrivilegedIPBind removes the host-IP prefix from a PublishPort value
// (e.g. "127.0.0.1:80:80" → "80:80") when the host port is privileged (< 1024).
// On macOS, gvproxy handles port forwarding through podman-mac-helper and does
// not support explicit IP binds for privileged ports — trying them causes
// "bind: permission denied". Non-privileged ports (3306, 6379, etc.) do
// support IP binding and are left untouched so LAN restriction still works.
func stripPrivilegedIPBind(port string) string {
	// Expected formats: "hostIP:hostPort:containerPort[/proto]" or "hostPort:containerPort[/proto]"
	parts := strings.SplitN(port, ":", 3)
	if len(parts) != 3 {
		return port // bare "containerPort" or "hostPort:containerPort" — no IP prefix
	}
	// parts[0] is the host IP, parts[1] is the host port, parts[2] is container port
	hostPortStr := strings.SplitN(parts[1], "/", 2)[0] // strip "/tcp" etc.
	if n := 0; len(hostPortStr) > 0 {
		for _, c := range hostPortStr {
			if c < '0' || c > '9' {
				return port // non-numeric, leave as-is
			}
			n = n*10 + int(c-'0')
		}
		if n < 1024 {
			return parts[1] + ":" + parts[2] // drop the IP prefix
		}
	}
	return port
}

// containerToPodmanArgs builds a podman run argument list from a parsed [Container] section.
// On macOS we run detached (-d) so that launchctl bootstrap sees an immediate
// exit 0 (success); podman's own --restart=always policy handles crash recovery.
func containerToPodmanArgs(c map[string][]string) ([]string, error) {
	args := []string{podmanBinPath(), "run", "-d", "--restart=always"}

	if names := c["ContainerName"]; len(names) > 0 {
		// --replace removes any stale container with this name before starting.
		// -t 5 limits the stop grace period so restart isn't slow.
		args = append(args, "--name", names[0], "--replace", "--stop-timeout=5")
	}
	for _, net := range c["Network"] {
		args = append(args, "--network", net)
	}
	for _, port := range c["PublishPort"] {
		args = append(args, "-p", stripPrivilegedIPBind(port))
	}
	for _, vol := range c["Volume"] {
		args = append(args, "-v", stripSELinuxVolOpts(expandSpecifiers(vol)))
	}
	for _, env := range c["Environment"] {
		args = append(args, "-e", env)
	}
	if dirs := c["WorkingDir"]; len(dirs) > 0 {
		args = append(args, "--workdir", expandSpecifiers(dirs[0]))
	}
	for _, extra := range c["PodmanArgs"] {
		args = append(args, strings.Fields(expandSpecifiers(extra))...)
	}

	images := c["Image"]
	if len(images) == 0 {
		return nil, fmt.Errorf("no Image= found in [Container] section")
	}
	args = append(args, images[0])

	for _, cmd := range c["Exec"] {
		args = append(args, strings.Fields(cmd)...)
	}

	return args, nil
}

// --- Service unit files ---

func (m *darwinServiceManager) WriteServiceUnit(name, content string) error {
	svc := parseSection(content, "Service")
	execStarts := svc["ExecStart"]
	if len(execStarts) == 0 {
		return fmt.Errorf("no ExecStart= found in service unit %s", name)
	}
	// Expand %h and split into argv
	args := strings.Fields(expandSpecifiers(execStarts[0]))
	if len(args) == 0 {
		return fmt.Errorf("empty ExecStart in service unit %s", name)
	}

	// On macOS the lerd binary is managed by Homebrew and lives at
	// /opt/homebrew/bin/lerd, not at ~/.local/bin/lerd as on Linux.
	// If the binary referenced in ExecStart doesn't exist, substitute
	// the path of the currently running lerd executable.
	if _, err := os.Stat(args[0]); err != nil {
		if self, err := os.Executable(); err == nil {
			args[0] = self
		}
	}

	if err := ensurePlistDirs(name); err != nil {
		return err
	}
	logPath := filepath.Join(lerdLogsDir(), name+".log")
	plist := buildPlist(plistLabel(name), args, true, false, logPath, logPath)
	return os.WriteFile(plistPath(name), []byte(plist), 0644)
}

func (m *darwinServiceManager) WriteServiceUnitIfChanged(name, content string) (bool, error) {
	svc := parseSection(content, "Service")
	execStarts := svc["ExecStart"]
	if len(execStarts) == 0 {
		return false, fmt.Errorf("no ExecStart= found in service unit %s", name)
	}
	args := strings.Fields(expandSpecifiers(execStarts[0]))
	if len(args) == 0 {
		return false, fmt.Errorf("empty ExecStart in service unit %s", name)
	}
	if _, err := os.Stat(args[0]); err != nil {
		if self, err := os.Executable(); err == nil {
			args[0] = self
		}
	}

	logPath := filepath.Join(lerdLogsDir(), name+".log")
	newPlist := buildPlist(plistLabel(name), args, true, false, logPath, logPath)

	if existing, err := os.ReadFile(plistPath(name)); err == nil && string(existing) == newPlist {
		return false, nil
	}
	if err := ensurePlistDirs(name); err != nil {
		return false, err
	}
	return true, os.WriteFile(plistPath(name), []byte(newPlist), 0644)
}

func (m *darwinServiceManager) RemoveServiceUnit(name string) error {
	if err := os.Remove(plistPath(name)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (m *darwinServiceManager) ListServiceUnits(nameGlob string) []string {
	pattern := filepath.Join(launchAgentsDir(), nameGlob+".plist")
	entries, _ := filepath.Glob(pattern)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(filepath.Base(e), ".plist"))
	}
	return names
}

// --- Container unit files ---

func (m *darwinServiceManager) WriteContainerUnit(name, content string) error {
	// Apply BindForLAN so the plist always reflects the current LAN-exposure
	// setting, regardless of whether the caller went through WriteQuadlet or
	// called WriteContainerUnit directly. BindForLAN is idempotent.
	lanExposed := false
	if cfg, err := config.LoadGlobal(); err == nil && cfg != nil {
		lanExposed = cfg.LAN.Exposed
	}
	content = podman.BindForLAN(content, lanExposed)

	c := parseSection(content, "Container")
	args, err := containerToPodmanArgs(c)
	if err != nil {
		return fmt.Errorf("container unit %s: %w", name, err)
	}

	// Pre-create volume source directories so podman doesn't fail with statfs.
	for _, vol := range c["Volume"] {
		parts := strings.SplitN(expandSpecifiers(vol), ":", 3)
		if len(parts) >= 2 {
			os.MkdirAll(parts[0], 0755) //nolint:errcheck
		}
	}

	if err := ensurePlistDirs(name); err != nil {
		return err
	}
	logPath := filepath.Join(lerdLogsDir(), name+".log")
	// RunAtLoad=false: container units are started by `lerd start` (via lerd-autostart),
	// which first ensures Podman Machine is running. Firing podman run at login before
	// the machine is up causes silent failures, so we let lerd-autostart sequence it.
	// Stdout is suppressed (/dev/null) because `podman run -d` only prints the container
	// ID there; real container output is accessible via `podman logs <name>`.
	plist := buildPlist(plistLabel(name), args, false, false, "/dev/null", logPath)
	return os.WriteFile(plistPath(name), []byte(plist), 0644)
}

func (m *darwinServiceManager) ContainerUnitInstalled(name string) bool {
	_, err := os.Stat(plistPath(name))
	return err == nil
}

func (m *darwinServiceManager) RemoveContainerUnit(name string) error {
	if err := os.Remove(plistPath(name)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (m *darwinServiceManager) ListContainerUnits(nameGlob string) []string {
	// Container units share the same plist directory; no separate extension.
	// We use the same glob pattern as service units — callers are expected to
	// pass a glob that uniquely identifies containers (e.g. "lerd-*").
	return m.ListServiceUnits(nameGlob)
}

// --- Service lifecycle ---

// DaemonReload is a no-op on macOS; launchd picks up plist changes on bootstrap.
func (m *darwinServiceManager) DaemonReload() error { return nil }

// bootstrap registers and starts the service plist in the user's GUI domain.
// If already bootstrapped, it kicks (restarts) the service instead.
func (m *darwinServiceManager) Start(name string) error {
	p := plistPath(name)
	if _, err := os.Stat(p); err != nil {
		return fmt.Errorf("plist not found for %s", name)
	}
	domain := uidDomain()
	label := plistLabel(name)

	// If the service is already in the domain, bootout first so the subsequent
	// bootstrap always picks up the current plist on disk. kickstart -k would
	// restart the job but launchd would use its cached plist, missing any
	// changes written by WriteServiceUnit / WriteContainerUnit.
	if out, err := launchctl("print", domain+"/"+label); err == nil {
		// Parse the PID from launchctl print output and kill the process
		// directly so the port is released before we bootstrap the new instance.
		// bootout sends SIGTERM but returns before the process exits, causing
		// the next bootstrap to fail with "address already in use".
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "pid = ") {
				if pid, err := strconv.Atoi(strings.TrimPrefix(line, "pid = ")); err == nil {
					if proc, err := os.FindProcess(pid); err == nil {
						proc.Kill() //nolint:errcheck
					}
				}
				break
			}
		}
		launchctl("bootout", domain+"/"+label) //nolint:errcheck
	}

	// Enable AFTER bootout — on macOS Ventura+, bootout marks the service as
	// disabled in launchd's persistent database, causing the next bootstrap to
	// fail with exit 5. Re-enabling here ensures bootstrap always succeeds.
	launchctl("enable", domain+"/"+label) //nolint:errcheck

	out, err := launchctl("bootstrap", domain, p)
	if err != nil {
		s := string(out)
		bcode := launchctlExitCode(err)
		// 36 = already bootstrapped; "Bootstrap failed: 5" = EBUSY / I-O error
		// (macOS Ventura+ race after a rapid bootout+bootstrap) — both mean the
		// job is already in the domain, kick it to (re)start with the current plist.
		// 113 = launchd can't find the service in its database yet (timing race on
		// first-write); kickstart will also fail — treat as a successful write and
		// let the caller's subsequent Restart() bring the service up.
		if bcode == 113 || strings.Contains(s, "Could not find service") {
			return nil
		}
		if bcode == 36 || strings.Contains(s, "Bootstrap failed: 5") ||
			strings.Contains(s, "already bootstrapped") ||
			strings.Contains(s, "service already loaded") {
			if kout, kerr := launchctl("kickstart", "-k", domain+"/"+label); kerr != nil {
				kcode := launchctlExitCode(kerr)
				// 37 = EALREADY — job is already running, treat as success.
				if kcode == 37 || strings.Contains(string(kout), "already running") {
					return nil
				}
				return fmt.Errorf("launchctl kickstart %s: %w\n%s", name, kerr, kout)
			}
			return nil
		}
		return fmt.Errorf("launchctl bootstrap %s: %w\n%s", name, err, out)
	}
	// Container units use RunAtLoad=false so bootstrap alone doesn't start them.
	// Service units use RunAtLoad=true so bootstrap already started them — no kick needed.
	content, _ := os.ReadFile(p)
	if !strings.Contains(string(content), "<key>RunAtLoad</key>") {
		launchctl("kickstart", domain+"/"+label) //nolint:errcheck
	}
	return nil
}

// Stop removes the service from the user's GUI domain (bootout) and also stops
// any detached podman container running under the same name. The podman stop is
// needed because container units use -d (detached) + --restart=always, so the
// container keeps running independently of launchd after the plist is booted out.
func (m *darwinServiceManager) Stop(name string) error {
	// Derive the container name: plist name IS the container name (e.g. lerd-dns).
	exec.Command(podmanBinPath(), "stop", "-t", "5", name).Run() //nolint:errcheck
	exec.Command(podmanBinPath(), "rm", "-f", name).Run()        //nolint:errcheck

	domain := uidDomain()
	label := plistLabel(name)

	out, err := launchctl("bootout", domain+"/"+label)
	if err != nil {
		s := string(out)
		code := launchctlExitCode(err)
		// 36 = not loaded / already gone — treat as success
		if code == 36 || code == 113 || strings.Contains(s, "No such process") ||
			strings.Contains(s, "Could not find") || strings.Contains(s, "not bootstrapped") {
			return nil
		}
		return fmt.Errorf("launchctl bootout %s: %w\n%s", name, err, out)
	}
	return nil
}

// Restart does a full bootout + bootstrap cycle so the new binary on disk is
// always picked up. kickstart -k would reuse launchd's cached program path and
// keep the old binary running after an in-place binary replacement (e.g. after
// `lerd install` / `brew upgrade`). Start already handles killing the running
// PID, booting out, and re-bootstrapping from the current plist on disk.
func (m *darwinServiceManager) Restart(name string) error {
	return m.Start(name)
}

// Enable marks the service as enabled so launchd starts it on the next login.
// It does NOT start the service immediately — callers that want the service
// running right now should follow up with Restart or Start.
func (m *darwinServiceManager) Enable(name string) error {
	domain := uidDomain()
	label := plistLabel(name)
	launchctl("enable", domain+"/"+label) //nolint:errcheck
	return nil
}

// Disable stops the service and marks it disabled so it won't start at login.
func (m *darwinServiceManager) Disable(name string) error {
	domain := uidDomain()
	label := plistLabel(name)

	_ = m.Stop(name)
	launchctl("disable", domain+"/"+label) //nolint:errcheck
	return nil
}

// IsActive returns true if the service is currently running.
// For container units we also check the container directly.
func (m *darwinServiceManager) IsActive(name string) bool {
	if running, _ := podman.ContainerRunning(name); running {
		return true
	}
	domain := uidDomain()
	label := plistLabel(name)
	out, err := launchctl("print", domain+"/"+label)
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "state = running")
}

// IsEnabled returns true if the plist exists in LaunchAgents.
// On macOS, placing a plist in ~/Library/LaunchAgents is the equivalent of "enabled".
func (m *darwinServiceManager) IsEnabled(name string) bool {
	_, err := os.Stat(plistPath(name))
	return err == nil
}

// UnitStatus returns a status string similar to systemd's active state.
// Container units (podman run -d) exit immediately with code 0 once the
// container is detached, so we fall back to checking whether the container
// is actually running rather than trusting launchd's "state = waiting/exited".
func (m *darwinServiceManager) UnitStatus(name string) (string, error) {
	domain := uidDomain()
	label := plistLabel(name)
	out, err := launchctl("print", domain+"/"+label)
	if err != nil {
		// Not loaded at all — check container directly before giving up.
		if running, _ := podman.ContainerRunning(name); running {
			return "active", nil
		}
		if _, statErr := os.Stat(plistPath(name)); statErr == nil {
			return "inactive", nil
		}
		return "unknown", nil
	}
	s := string(out)
	if strings.Contains(s, "state = running") {
		return "active", nil
	}
	// For exited-0 or waiting: the job may be a container launcher that
	// succeeded (-d detach). Check the actual container state.
	if running, _ := podman.ContainerRunning(name); running {
		return "active", nil
	}
	if strings.Contains(s, "state = waiting") || strings.Contains(s, "last exit code = 0") {
		return "inactive", nil
	}
	return "failed", nil
}
