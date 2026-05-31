//go:build darwin

package services

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

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

// podmanStartSem limits concurrent `podman run` executions to avoid
// overwhelming the Podman Machine SSH connection with parallel requests.
var podmanStartSem = make(chan struct{}, 4)

func init() {
	mgr := &darwinServiceManager{}
	Mgr = mgr
	// Override service-manager hooks to use launchd instead of systemd.
	podman.WriteContainerUnitFn = mgr.WriteContainerUnit
	podman.DaemonReloadFn = mgr.DaemonReload
	podman.SkipQuadletUpToDateCheck = true
	// Bind the concrete type so UnitLifecycle picks up AllUnitStates, which
	// isn't part of services.ServiceManager (Linux has no need for it — the
	// systemctl batched-list path covers Linux callers).
	podman.UnitLifecycle = mgr
	podman.RemoveContainerUnitFn = mgr.RemoveContainerUnit
	// Keep launchd plists in sync when WriteQuadletDiff updates a .container file.
	podman.AfterQuadletWriteFn = func(name, content string) error {
		return mgr.WriteContainerUnit(name, content)
	}
}

// plistArgs parses the ProgramArguments array from a plist file and returns
// the argument strings. Used to run container units directly from Go code
// rather than via launchctl kickstart, so we control launch concurrency.
func plistArgs(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// Find the ProgramArguments array using simple string search.
	s := string(data)
	const key = "<key>ProgramArguments</key>"
	idx := strings.Index(s, key)
	if idx < 0 {
		return nil, fmt.Errorf("ProgramArguments not found in %s", path)
	}
	s = s[idx+len(key):]
	start := strings.Index(s, "<array>")
	end := strings.Index(s, "</array>")
	if start < 0 || end < 0 {
		return nil, fmt.Errorf("ProgramArguments array not found in %s", path)
	}
	block := s[start+len("<array>") : end]
	var args []string
	for {
		open := strings.Index(block, "<string>")
		close := strings.Index(block, "</string>")
		if open < 0 || close < 0 {
			break
		}
		args = append(args, block[open+len("<string>"):close])
		block = block[close+len("</string>"):]
	}
	return args, nil
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
	// Only escape characters that are truly unsafe in XML text nodes.
	// xml.EscapeText also escapes ' → &#39; and " → &#34;, but Apple's plist
	// parser passes those numeric character references through literally
	// rather than decoding them, corrupting env var values like X_FRAME_OPTIONS = ''
	// into invalid Python. Single and double quotes are valid in XML PCDATA
	// without escaping.
	var buf strings.Builder
	for _, c := range s {
		switch c {
		case '&':
			buf.WriteString("&amp;")
		case '<':
			buf.WriteString("&lt;")
		case '>':
			buf.WriteString("&gt;")
		default:
			buf.WriteRune(c)
		}
	}
	return buf.String()
}

// keepAlivePolicy mirrors the subset of systemd Restart= values we care
// about; bare KeepAlive=true respawns on clean exit, which is wrong for
// Restart=on-failure (was breaking the tray Quit button).
type keepAlivePolicy int

const (
	keepAliveNever keepAlivePolicy = iota
	keepAliveAlways
	keepAliveOnFailure
)

func buildPlist(lbl string, args []string, runAtLoad bool, keepAlive keepAlivePolicy, stdoutPath, stderrPath string) string {
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
	switch keepAlive {
	case keepAliveAlways:
		sb.WriteString("\t<key>KeepAlive</key>\n\t<true/>\n")
	case keepAliveOnFailure:
		sb.WriteString("\t<key>KeepAlive</key>\n\t<dict>\n\t\t<key>SuccessfulExit</key>\n\t\t<false/>\n\t</dict>\n")
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

// unquoteSystemdValue strips the outer double-quotes that systemd / quadlet
// uses for values that contain spaces or special chars (e.g. Environment=
// "KEY=hello world"). The shell never processes these args, so the quotes
// must be removed before passing to exec.Command.
func unquoteSystemdValue(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return strings.ReplaceAll(s[1:len(s)-1], `\"`, `"`)
	}
	return s
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
// when the host port is privileged (< 1024). gvproxy on macOS rejects
// explicit IP binds for privileged ports with "bind: permission denied".
// Handles both v4 ("127.0.0.1:80:80" → "80:80") and bracketed v6
// ("[::1]:443:443" → "443:443"). Non-privileged ports keep their bind so
// LAN restriction is preserved.
func stripPrivilegedIPBind(port string) string {
	var rest string
	if strings.HasPrefix(port, "[") {
		end := strings.Index(port, "]")
		if end < 0 || end+1 >= len(port) || port[end+1] != ':' {
			return port
		}
		rest = port[end+2:]
	} else {
		parts := strings.SplitN(port, ":", 3)
		if len(parts) != 3 {
			return port
		}
		rest = parts[1] + ":" + parts[2]
	}
	hostPortStr := strings.SplitN(strings.SplitN(rest, ":", 2)[0], "/", 2)[0]
	n := 0
	for _, c := range hostPortStr {
		if c < '0' || c > '9' {
			return port
		}
		n = n*10 + int(c-'0')
	}
	if n > 0 && n < 1024 {
		return rest
	}
	return port
}

// stripIPv6PublishPorts removes PublishPort= lines that start with a bracketed
// IPv6 address (e.g. "[::1]:3306:3306"). gvproxy cannot bind both an IPv4 and
// an IPv6 loopback address on the same port simultaneously.
func stripIPv6PublishPorts(content string) string {
	lines := strings.Split(content, "\n")
	out := lines[:0]
	for _, l := range lines {
		val := strings.TrimPrefix(strings.TrimSpace(l), "PublishPort=")
		if val != strings.TrimSpace(l) && strings.HasPrefix(val, "[") {
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
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
		args = append(args, "-e", unquoteSystemdValue(env))
	}
	if userns := c["UserNS"]; len(userns) > 0 {
		args = append(args, "--userns", userns[0])
	}
	if hns := c["HostName"]; len(hns) > 0 {
		args = append(args, "--hostname", hns[0])
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

// parseServiceUnit parses a systemd-format service unit and returns the argv
// and keepAlive policy for the launchd plist.
//
// Binary resolution rules for args[0]:
//   - Absolute path that exists → use as-is.
//   - Absolute path that doesn't exist → substitute the running lerd binary
//     (handles Homebrew → ~/.local/bin migration).
//   - Bare command name (no '/') → resolve via PATH; if not found, substitute
//     the running lerd binary (should not normally happen).
func parseServiceUnit(name, content string) (args []string, keepAlive keepAlivePolicy, err error) {
	svc := parseSection(content, "Service")
	execStarts := svc["ExecStart"]
	if len(execStarts) == 0 {
		return nil, keepAliveNever, fmt.Errorf("no ExecStart= found in service unit %s", name)
	}
	args = strings.Fields(expandSpecifiers(execStarts[0]))
	if len(args) == 0 {
		return nil, keepAliveNever, fmt.Errorf("empty ExecStart in service unit %s", name)
	}

	// Resolve args[0] to an absolute path suitable for a launchd plist.
	if filepath.IsAbs(args[0]) {
		// Absolute path: substitute if missing (e.g. old Homebrew install).
		if _, statErr := os.Stat(args[0]); statErr != nil {
			if self, selfErr := os.Executable(); selfErr == nil {
				args[0] = self
			}
		}
	} else {
		// Bare command (e.g. "podman"): resolve via PATH first, then well-known
		// Homebrew locations. Never fall back to the lerd binary — if the command
		// cannot be found, return an error so the caller can surface a clear message.
		resolved := ""
		if p, lookErr := exec.LookPath(args[0]); lookErr == nil {
			resolved = p
		} else {
			for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin"} {
				candidate := filepath.Join(dir, args[0])
				if _, statErr := os.Stat(candidate); statErr == nil {
					resolved = candidate
					break
				}
			}
		}
		if resolved == "" {
			return nil, keepAliveNever, fmt.Errorf("command %q in ExecStart of %s not found; use an absolute path", args[0], name)
		}
		args[0] = resolved
	}

	// Map Restart= to a launchd policy. `Restart=on-failure` translates to
	// KeepAlive: SuccessfulExit=false so a clean exit (e.g. tray Quit) is
	// honoured; only crashes or non-zero exits trigger a respawn.
	restart := ""
	if restarts := svc["Restart"]; len(restarts) > 0 {
		restart = restarts[0]
	}
	switch restart {
	case "always":
		keepAlive = keepAliveAlways
	case "on-failure":
		keepAlive = keepAliveOnFailure
	default:
		keepAlive = keepAliveNever
	}
	return args, keepAlive, nil
}

func (m *darwinServiceManager) WriteServiceUnit(name, content string) error {
	args, keepAlive, err := parseServiceUnit(name, content)
	if err != nil {
		return err
	}
	if err := ensurePlistDirs(name); err != nil {
		return err
	}
	logPath := filepath.Join(lerdLogsDir(), name+".log")
	plist := buildPlist(plistLabel(name), args, true, keepAlive, logPath, logPath)
	return os.WriteFile(plistPath(name), []byte(plist), 0644)
}

func (m *darwinServiceManager) WriteServiceUnitIfChanged(name, content string) (bool, error) {
	args, keepAlive, err := parseServiceUnit(name, content)
	if err != nil {
		return false, err
	}

	logPath := filepath.Join(lerdLogsDir(), name+".log")
	newPlist := buildPlist(plistLabel(name), args, true, keepAlive, logPath, logPath)

	if existing, err := os.ReadFile(plistPath(name)); err == nil && string(existing) == newPlist {
		return false, nil
	}
	if err := ensurePlistDirs(name); err != nil {
		return false, err
	}
	return true, os.WriteFile(plistPath(name), []byte(newPlist), 0644)
}

// WriteTimerUnitIfChanged is a no-op on macOS until launchd
// StartCalendarInterval support is added. Scheduled framework workers
// (like Laravel 10's `schedule:run`) currently log a warning and skip
// on macOS rather than restart-loop as long-running daemons.
func (m *darwinServiceManager) WriteTimerUnitIfChanged(_, _ string) (bool, error) {
	return false, nil
}

// RemoveTimerUnit is a no-op on macOS — see WriteTimerUnitIfChanged.
func (m *darwinServiceManager) RemoveTimerUnit(_ string) error { return nil }

// ListTimerUnits returns no entries on macOS until launchd
// StartCalendarInterval support lands.
func (m *darwinServiceManager) ListTimerUnits(_ string) []string { return nil }

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
	// Apply LAN binding restriction before parsing — mirrors WriteQuadletDiff on Linux.
	lanExposed := false
	if cfg, err := config.LoadGlobal(); err == nil && cfg != nil {
		lanExposed = cfg.LAN.Exposed
	}
	content = podman.BindForLAN(content, lanExposed)
	// gvproxy (macOS) cannot bind two specific host IPs on the same port;
	// drop IPv6 PublishPort lines so only IPv4 bindings reach podman run.
	content = stripIPv6PublishPorts(content)

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
	plist := buildPlist(plistLabel(name), args, false, keepAliveNever, "/dev/null", logPath)
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
	alreadyInDomain := false
	if _, err := launchctl("print", domain+"/"+label); err == nil {
		alreadyInDomain = true
		launchctl("bootout", domain+"/"+label) //nolint:errcheck
		// Brief pause so macOS Sequoia+ doesn't reject the immediately-following
		// bootstrap with a spurious "already bootstrapped" (36) or EBUSY (5) error.
		time.Sleep(200 * time.Millisecond)
	}

	// Enable AFTER bootout — on macOS Ventura+, bootout marks the service as
	// disabled in launchd's persistent database, causing the next bootstrap to
	// fail with exit 5. Re-enabling here ensures bootstrap always succeeds.
	launchctl("enable", domain+"/"+label) //nolint:errcheck

	out, err := launchctl("bootstrap", domain, p)
	if err != nil {
		s := string(out)
		// 36 = already bootstrapped; "Bootstrap failed: 5" = EBUSY / I-O error
		// (macOS Ventura+ race after a rapid bootout+bootstrap) — both mean the
		// job is already in the domain, kick it to (re)start with the current plist.
		if strings.Contains(s, "36") || strings.Contains(s, "Bootstrap failed: 5") ||
			strings.Contains(s, "already bootstrapped") ||
			strings.Contains(s, "service already loaded") {
			// Already in domain — run container directly if it's a container unit.
			content2, _ := os.ReadFile(p)
			if !strings.Contains(string(content2), "<key>RunAtLoad</key>") {
				if args, aerr := plistArgs(p); aerr == nil && len(args) > 0 {
					podmanStartSem <- struct{}{}
					rerr := runPodmanWithError(args)
					<-podmanStartSem
					if rerr != nil {
						return fmt.Errorf("podman run %s: %w", name, rerr)
					}
					return nil
				}
			}
			if kout, kerr := launchctl("kickstart", "-k", domain+"/"+label); kerr != nil {
				ks := string(kout)
				// 37 = EALREADY — job is already running, treat as success.
				if strings.Contains(ks, "37") || strings.Contains(ks, "already running") {
					return nil
				}
				return fmt.Errorf("launchctl kickstart %s: %w\n%s", name, kerr, kout)
			}
			return nil
		}
		// If bootstrap failed and we just did a bootout, retry once — launchd on
		// Sequoia can transiently reject a re-bootstrap immediately after bootout.
		if alreadyInDomain {
			time.Sleep(300 * time.Millisecond)
			launchctl("enable", domain+"/"+label) //nolint:errcheck
			if out2, err2 := launchctl("bootstrap", domain, p); err2 != nil {
				return fmt.Errorf("launchctl bootstrap %s: %w\n%s", name, err2, out2)
			}
			return nil
		}
		return fmt.Errorf("launchctl bootstrap %s: %w\n%s", name, err, out)
	}
	// Container units use RunAtLoad=false so bootstrap alone doesn't start them.
	// Service units use RunAtLoad=true so bootstrap already started them — no kick needed.
	content, _ := os.ReadFile(p)
	if strings.Contains(string(content), "<key>RunAtLoad</key>") {
		return nil // service unit already started by bootstrap
	}
	// Container unit: run podman directly (with concurrency limit) instead of
	// launchctl kickstart. kickstart lets launchd fire all podman run processes
	// simultaneously, which overwhelms the Podman Machine SSH connection when
	// N services start in parallel. Running directly lets us gate on podmanStartSem.
	args, err := plistArgs(p)
	if err != nil || len(args) == 0 {
		// Fallback to kickstart if plist parsing fails.
		launchctl("kickstart", domain+"/"+label) //nolint:errcheck
		return nil
	}
	podmanStartSem <- struct{}{}
	rerr := runPodmanWithError(args)
	<-podmanStartSem
	if rerr != nil {
		return fmt.Errorf("podman run %s: %w", name, rerr)
	}
	return nil
}

// runPodmanWithError invokes the podman command and surfaces stderr in the
// returned error. The launcher process detaches once `podman run -d` accepts
// the request, so a non-nil error here means podman itself rejected the run
// (image missing, port collision, machine down) — exactly the cases that
// were previously masked under //nolint:errcheck and made the heal loop
// report success on a unit that never started.
func runPodmanWithError(args []string) error {
	cmd := exec.Command(args[0], args[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(out))
		if trimmed == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, trimmed)
	}
	return nil
}

// Stop removes the service from the user's GUI domain (bootout) and also stops
// any detached podman container running under the same name. The podman stop is
// needed because container units use -d (detached) + --restart=always, so the
// container keeps running independently of launchd after the plist is booted out.
func (m *darwinServiceManager) Stop(name string) error {
	// Stop and remove the container only if it is actually running.
	// Skipping the podman calls when the container is absent avoids flooding the
	// Podman Machine SSH socket with N parallel no-op requests during lerd stop.
	if running, _ := podman.ContainerRunning(name); running {
		exec.Command(podmanBinPath(), "stop", "-t", "5", name).Run() //nolint:errcheck
		exec.Command(podmanBinPath(), "rm", "-f", name).Run()        //nolint:errcheck
	}

	domain := uidDomain()
	label := plistLabel(name)

	out, err := launchctl("bootout", domain+"/"+label)
	if err != nil {
		s := string(out)
		// 36 = not loaded / already gone — treat as success
		if strings.Contains(s, "36") || strings.Contains(s, "No such process") ||
			strings.Contains(s, "Could not find") || strings.Contains(s, "not bootstrapped") {
			return nil
		}
		return fmt.Errorf("launchctl bootout %s: %w\n%s", name, err, out)
	}
	return nil
}

// Restart kicks the service if loaded, otherwise bootstraps it fresh.
func (m *darwinServiceManager) Restart(name string) error {
	// For container units, the detached podman container runs independently
	// of launchd. Stop it explicitly so the restart is clean even if
	// --replace is ever removed from the podman run args.
	if running, _ := podman.ContainerRunning(name); running {
		exec.Command(podmanBinPath(), "stop", "-t", "5", name).Run() //nolint:errcheck
		exec.Command(podmanBinPath(), "rm", "-f", name).Run()        //nolint:errcheck
	}

	domain := uidDomain()
	label := plistLabel(name)

	// Bootout so the subsequent Start (bootstrap) picks up the current
	// plist on disk. kickstart -k would use launchd's cached copy.
	if _, err := launchctl("print", domain+"/"+label); err == nil {
		launchctl("bootout", domain+"/"+label) //nolint:errcheck
		time.Sleep(200 * time.Millisecond)
	}
	return m.Start(name)
}

// Enable marks the service as enabled (persists across logins) and bootstraps it.
func (m *darwinServiceManager) Enable(name string) error {
	domain := uidDomain()
	label := plistLabel(name)

	// enable records the intent; bootstrap actually starts it now
	launchctl("enable", domain+"/"+label) //nolint:errcheck
	return m.Start(name)
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
	if podman.Cache.Running(name) {
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
		if podman.Cache.Running(name) {
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
	if podman.Cache.Running(name) {
		return "active", nil
	}
	// Universal failure signal: an explicit non-zero last exit code is
	// "this is broken" regardless of whether the plist is a container
	// launcher (mysql, postgres, …) or a runtime-mode worker (queue,
	// schedule, horizon — `/bin/sh worker.sh` → `podman exec ... php
	// artisan ...`). Without this, runtime workers between retries would
	// fall through to the state=waiting/exit=0 branch and surface as
	// "inactive" even though the previous run aborted with exit != 0.
	if hasNonZeroExitCode(s) {
		return "failed", nil
	}
	// Container units that exited cleanly (last exit code = 0) but whose
	// detached container isn't currently running are crashed post-detach —
	// `podman run -d` returned 0, the container died after, --restart=always
	// can't bring it back (data dir / image / port issue). Treat as failed
	// so workerheal.Detect picks them up. Skip when the launcher hasn't
	// completed yet ("(never exited)"): that's the brief window between
	// Start() returning and ContainerCache picking up the new state, and
	// reporting "failed" there would be a false positive that could trigger
	// spurious heal cycles on every fresh start.
	if isContainerPlist(out) && strings.Contains(s, "last exit code = 0") {
		return "failed", nil
	}
	if strings.Contains(s, "state = waiting") || strings.Contains(s, "last exit code = 0") {
		return "inactive", nil
	}
	return "failed", nil
}

// hasNonZeroExitCode reports whether the launchctl print output has a
// "last exit code = N" line where N is neither 0 nor "(never exited)".
// Returns false when the field is absent so newly-bootstrapped units that
// haven't run yet aren't misreported as failed.
func hasNonZeroExitCode(s string) bool {
	const key = "last exit code = "
	idx := strings.Index(s, key)
	if idx < 0 {
		return false
	}
	rest := s[idx+len(key):]
	end := strings.IndexByte(rest, '\n')
	if end < 0 {
		end = len(rest)
	}
	val := strings.TrimSpace(rest[:end])
	if val == "" || val == "0" || val == "(never exited)" {
		return false
	}
	return true
}

// isContainerPlist reports whether the launchctl print output describes a
// container-unit plist (i.e. the launcher exec'd `podman run`). Runtime-mode
// workers launch via `/bin/sh worker.sh` so the launchctl-visible program
// path doesn't include `/podman`; the embedded `podman exec` call lives
// inside the script and is invisible here. Used by UnitStatus to
// differentiate container units that crashed post-detach from runtime
// workers in transient inactive states.
func isContainerPlist(out []byte) bool {
	s := string(out)
	return strings.Contains(s, "/podman") && strings.Contains(s, "run")
}

// AllUnitStates enumerates every lerd-* plist in ~/Library/LaunchAgents and
// returns a snapshot keyed by unit name → systemd-style state string. Both
// "lerd-foo" and "lerd-foo.service" forms are populated so cross-platform
// callers (workerheal, dashboard banner) can use a single suffix-based lookup.
//
// This is the launchd analogue of `systemctl --user list-units lerd-*` and
// is wired onto siteinfo.AllUnitStates from siteinfo/unitcache_darwin.go.
func (m *darwinServiceManager) AllUnitStates() map[string]string {
	pattern := filepath.Join(launchAgentsDir(), "lerd-*.plist")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(matches)*2)
	for _, path := range matches {
		name := strings.TrimSuffix(filepath.Base(path), ".plist")
		if !strings.HasPrefix(name, "lerd-") {
			continue
		}
		state, _ := m.UnitStatus(name)
		if state == "" || state == "unknown" {
			continue
		}
		out[name] = state
		out[name+".service"] = state
	}
	return out
}
