package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
	"github.com/geodro/lerd/internal/version"
	"github.com/spf13/cobra"
)

// NewBugReportCmd returns the bug-report command. It writes a single
// plain-text file aggregating the diagnostics a maintainer needs to triage a
// GitHub issue: doctor output, config files, systemd/podman state, recent
// logs, network state, and a curated set of environment variables.
func NewBugReportCmd() *cobra.Command {
	var outputPath string
	var logLines int
	cmd := &cobra.Command{
		Use:   "bug-report",
		Short: "Collect diagnostics into a single file for GitHub bug reports",
		Long: "Generates a plain-text report containing the lerd doctor output, " +
			"config files, systemd unit state, recent service logs, network " +
			"state and the relevant environment variables. Attach the file to " +
			"your GitHub issue.",
		RunE: func(_ *cobra.Command, _ []string) error {
			path, err := writeBugReport(outputPath, logLines)
			if err != nil {
				return err
			}
			fmt.Printf("Bug report written to: %s\n", path)
			fmt.Println("Skim it for anything sensitive before posting, then attach it to your GitHub issue.")
			return nil
		},
	}
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "output file path (default: ./lerd-bug-report-<timestamp>.txt)")
	cmd.Flags().IntVar(&logLines, "log-lines", 200, "number of recent log lines to include per service/container")
	return cmd
}

func writeBugReport(outputPath string, logLines int) (string, error) {
	if logLines <= 0 {
		logLines = 200
	}
	if outputPath == "" {
		ts := time.Now().Format("20060102-150405")
		outputPath = fmt.Sprintf("lerd-bug-report-%s.txt", ts)
	}
	abs, err := filepath.Abs(outputPath)
	if err != nil {
		return "", fmt.Errorf("resolving output path: %w", err)
	}
	f, err := os.Create(abs)
	if err != nil {
		return "", fmt.Errorf("creating output file: %w", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	collectBugReport(&buf, logLines)

	scrubbed := scrubHomePath(buf.String())
	if _, err := io.WriteString(f, scrubbed); err != nil {
		return "", fmt.Errorf("writing report: %w", err)
	}
	return abs, nil
}

func collectBugReport(w io.Writer, logLines int) {
	writeBugReportHeader(w)

	section(w, "Doctor")
	if _, _, err := RunDoctorTo(w, false); err != nil {
		fmt.Fprintf(w, "doctor failed: %v\n", err)
	}

	section(w, "Config files")
	dumpConfigFiles(w)

	section(w, "Installed runtimes")
	dumpRuntimes(w)

	section(w, "Systemd / launchd unit state")
	dumpUnitState(w)

	section(w, "Containers")
	dumpContainers(w)

	section(w, fmt.Sprintf("Recent service logs (last %d lines)", logLines))
	dumpServiceLogs(w, logLines)

	section(w, fmt.Sprintf("Recent container logs (last %d lines)", logLines))
	dumpContainerLogs(w, logLines)

	section(w, "Network")
	dumpNetwork(w)

	section(w, "Environment")
	dumpEnvironment(w)
}

func writeBugReportHeader(w io.Writer) {
	fmt.Fprintln(w, "Lerd bug report")
	fmt.Fprintln(w, "════════════════════════════════════════════════════════")
	fmt.Fprintln(w, "Review this file before posting publicly. It contains")
	fmt.Fprintln(w, "your config and recent service logs but no .env contents.")
	fmt.Fprintln(w, "Home paths have been replaced with $HOME.")
	fmt.Fprintln(w, "════════════════════════════════════════════════════════")
	fmt.Fprintf(w, "Generated:  %s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(w, "lerd:       %s\n", version.String())
	fmt.Fprintf(w, "OS:         %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(w, "Go runtime: %s\n", runtime.Version())
	if runtime.GOOS == "linux" {
		if name := readOSRelease(); name != "" {
			fmt.Fprintf(w, "Distro:     %s\n", name)
		}
		if out, err := exec.Command("uname", "-r").Output(); err == nil {
			fmt.Fprintf(w, "Kernel:     %s\n", strings.TrimSpace(string(out)))
		}
	}
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("sw_vers", "-productVersion").Output(); err == nil {
			fmt.Fprintf(w, "macOS:      %s\n", strings.TrimSpace(string(out)))
		}
	}
}

func section(w io.Writer, title string) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "════════════════════════════════════════════════════════")
	fmt.Fprintf(w, "  %s\n", title)
	fmt.Fprintln(w, "════════════════════════════════════════════════════════")
}

func dumpConfigFiles(w io.Writer) {
	for _, p := range []string{config.GlobalConfigFile(), config.SitesFile()} {
		fmt.Fprintf(w, "── %s\n", p)
		data, err := os.ReadFile(p)
		if err != nil {
			fmt.Fprintf(w, "(unreadable: %v)\n\n", err)
			continue
		}
		fmt.Fprintln(w, string(data))
		if !bytes.HasSuffix(data, []byte("\n")) {
			fmt.Fprintln(w)
		}
	}
}

func dumpRuntimes(w io.Writer) {
	if out, err := exec.Command("podman", "--version").Output(); err == nil {
		fmt.Fprintf(w, "podman:    %s", string(out))
	} else {
		fmt.Fprintln(w, "podman:    (not found)")
	}
	if out, err := exec.Command("podman", "info", "--format", "{{.Host.OCIRuntime.Name}} {{.Host.OCIRuntime.Version}}").Output(); err == nil {
		fmt.Fprintf(w, "OCI:       %s", string(out))
	}
	if cfg, err := config.LoadGlobal(); err == nil {
		fmt.Fprintf(w, "PHP default: %s\n", cfg.PHP.DefaultVersion)
		fmt.Fprintf(w, "Node default: %s\n", cfg.Node.DefaultVersion)
	}
}

func dumpUnitState(w io.Writer) {
	units := lerdUnits()
	if len(units) == 0 {
		fmt.Fprintln(w, "(no lerd units found)")
		return
	}
	for _, name := range units {
		state, err := services.Mgr.UnitStatus(name)
		if err != nil {
			state = fmt.Sprintf("error: %v", err)
		}
		enabled := "no"
		if services.Mgr.IsEnabled(name) {
			enabled = "yes"
		}
		fmt.Fprintf(w, "%-30s  state=%s  enabled=%s\n", name, state, enabled)
	}
}

// lerdUnits returns the union of installed container units and service units
// matching the lerd-* prefix, deduplicated and sorted.
func lerdUnits() []string {
	seen := map[string]struct{}{}
	for _, n := range services.Mgr.ListContainerUnits("lerd-*") {
		seen[n] = struct{}{}
	}
	for _, n := range services.Mgr.ListServiceUnits("lerd-*") {
		seen[n] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func dumpContainers(w io.Writer) {
	out, err := podman.Run("ps", "-a", "--filter", "name=lerd-", "--format",
		"{{.Names}}\t{{.Status}}\t{{.Image}}")
	if err != nil {
		fmt.Fprintf(w, "(podman ps failed: %v)\n", err)
		return
	}
	if strings.TrimSpace(out) == "" {
		fmt.Fprintln(w, "(no lerd containers)")
		return
	}
	fmt.Fprintln(w, out)
}

func dumpServiceLogs(w io.Writer, n int) {
	if runtime.GOOS != "linux" {
		fmt.Fprintln(w, "(skipped: journalctl is Linux-only)")
		return
	}
	for _, unit := range lerdUnits() {
		fmt.Fprintf(w, "── journalctl --user -u %s --no-pager -n %d\n", unit, n)
		out, err := exec.Command("journalctl", "--user", "-u", unit,
			"--no-pager", "-n", fmt.Sprintf("%d", n)).CombinedOutput()
		if err != nil {
			fmt.Fprintf(w, "(failed: %v)\n", err)
		}
		fmt.Fprintln(w, strings.TrimRight(string(out), "\n"))
		fmt.Fprintln(w)
	}
}

func dumpContainerLogs(w io.Writer, n int) {
	out, err := podman.Run("ps", "-a", "--filter", "name=lerd-", "--format", "{{.Names}}")
	if err != nil {
		fmt.Fprintf(w, "(podman ps failed: %v)\n", err)
		return
	}
	names := strings.Fields(out)
	if len(names) == 0 {
		fmt.Fprintln(w, "(no lerd containers)")
		return
	}
	for _, name := range names {
		fmt.Fprintf(w, "── podman logs --tail %d %s\n", n, name)
		logs, err := podman.Run("logs", "--tail", fmt.Sprintf("%d", n), name)
		if err != nil {
			fmt.Fprintf(w, "(failed: %v)\n\n", err)
			continue
		}
		fmt.Fprintln(w, logs)
		fmt.Fprintln(w)
	}
}

func dumpNetwork(w io.Writer) {
	fmt.Fprintln(w, "── listening sockets (lerd-relevant ports)")
	listing := portListOutput()
	for _, port := range []string{"53", "80", "443", "5300", "7073"} {
		for _, line := range strings.Split(listing, "\n") {
			if strings.Contains(line, ":"+port+" ") {
				fmt.Fprintf(w, "  port %-4s  %s\n", port, strings.TrimSpace(line))
			}
		}
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, "── /etc/resolv.conf")
	if data, err := os.ReadFile("/etc/resolv.conf"); err == nil {
		fmt.Fprintln(w, strings.TrimRight(string(data), "\n"))
	} else {
		fmt.Fprintf(w, "(unreadable: %v)\n", err)
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, "── host gateway probe (host.containers.internal)")
	if !services.Mgr.IsActive("lerd-nginx") || !services.Mgr.IsActive("lerd-ui") {
		fmt.Fprintln(w, "skipped: lerd-nginx and/or lerd-ui not running")
	} else if ip := podman.DetectHostGatewayIPProbeOnly(); ip != "" {
		fmt.Fprintf(w, "reachable via %s\n", ip)
	} else {
		fmt.Fprintln(w, "no candidate routed back to host (Xdebug + container→host calls will fail)")
	}
}

// envAllowlist is the curated set of environment variables included in the
// bug report. We intentionally avoid dumping the full environment because
// CI/CD or shell setups frequently contain secrets in unrelated variables.
var envAllowlist = []string{
	"PATH",
	"SHELL",
	"USER",
	"LOGNAME",
	"LANG",
	"LC_ALL",
	"TERM",
	"XDG_CONFIG_HOME",
	"XDG_DATA_HOME",
	"XDG_RUNTIME_DIR",
	"XDG_SESSION_TYPE",
	"DBUS_SESSION_BUS_ADDRESS",
	"DOCKER_HOST",
	"CONTAINER_HOST",
}

func dumpEnvironment(w io.Writer) {
	for _, key := range envAllowlist {
		val := os.Getenv(key)
		if val == "" {
			continue
		}
		fmt.Fprintf(w, "%s=%s\n", key, val)
	}
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "LERD_") {
			fmt.Fprintln(w, kv)
		}
	}
}

// scrubHomePath replaces occurrences of the user's home directory with the
// literal string "$HOME". Pure cosmetic privacy: it does not constitute a
// security boundary, just a courtesy so the report is shorter and doesn't
// expose the username repeatedly.
func scrubHomePath(s string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || home == "/" {
		return s
	}
	return strings.ReplaceAll(s, home, "$HOME")
}

// readOSRelease reads /etc/os-release and returns the PRETTY_NAME value.
// Returns empty string on any error.
func readOSRelease() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			val := strings.TrimPrefix(line, "PRETTY_NAME=")
			return strings.Trim(val, `"`)
		}
	}
	return ""
}
