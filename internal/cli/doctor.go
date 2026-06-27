package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/geodro/lerd/internal/cleanup"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dns"
	"github.com/geodro/lerd/internal/feedback"
	phpPkg "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
	lerdUpdate "github.com/geodro/lerd/internal/update"
	"github.com/geodro/lerd/internal/version"
	"github.com/geodro/lerd/internal/wsl"
	"github.com/spf13/cobra"
)

// NewDoctorCmd returns the doctor command.
func NewDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose your Lerd environment and report issues",
		RunE:  runDoctor,
	}
}

func runDoctor(_ *cobra.Command, _ []string) error {
	_, _, err := RunDoctorTo(os.Stdout, feedback.Animated())
	return err
}

// RunDoctorTo runs the full doctor diagnostic, writing human-readable output
// to w. When useColor is false ANSI escapes are stripped so the output is
// safe to embed in a plain-text file (used by `lerd bug-report`). Returns
// the failure and warning counts for callers that want to summarise.
func RunDoctorTo(w io.Writer, useColor bool) (fails, warns int, err error) {
	ok := func(label string) {
		fmt.Fprintf(w, "  %s %s\n", feedback.GreenIf(useColor, feedback.GlyphOK), label)
	}
	fail := func(label, msg, hint string) {
		fails++
		fmt.Fprintf(w, "  %s %s  %s\n    hint: %s\n", feedback.RedIf(useColor, feedback.GlyphFail), label, msg, hint)
	}
	warn := func(label, msg string) {
		warns++
		fmt.Fprintf(w, "  %s %s  %s\n", feedback.AmberIf(useColor, feedback.GlyphWarn), label, msg)
	}
	info := func(label, val string) {
		fmt.Fprintf(w, "  %-34s %s\n", label, val)
	}

	fmt.Fprintf(w, "Lerd Doctor  (version %s)\n", version.String())
	fmt.Fprintln(w, "══════════════════════════════════════════════")

	// ── Prerequisites ───────────────────────────────────────────────────────
	fmt.Fprintln(w, "\n[Prerequisites]")

	if _, lookErr := exec.LookPath("podman"); lookErr != nil {
		fail("podman binary", "not found in PATH", "install podman: https://podman.io/docs/installation")
	} else if runErr := podman.RunSilent("info"); runErr != nil {
		fail("podman", "podman info failed — daemon not running?", podmanDaemonHint())
	} else {
		ok("podman")
	}

	if runtime.GOOS == "linux" {
		if _, lookErr := exec.LookPath("crun"); lookErr != nil {
			warn("OCI runtime", "crun not found — recommended for rootless podman (install: sudo pacman -S crun / sudo apt install crun / sudo dnf install crun)")
		} else {
			ok("OCI runtime (crun)")
		}

		if out, runErr := exec.Command("systemctl", "--user", "is-system-running").Output(); runErr != nil {
			state := strings.TrimSpace(string(out))
			if state == "degraded" {
				warn("systemd user session", "degraded — some units have failed")
			} else {
				fail("systemd user session", fmt.Sprintf("state=%q", state), "log in as a real user (not su); run: systemctl --user status")
			}
		} else {
			ok("systemd user session")
		}

		currentUser := os.Getenv("USER")
		if currentUser == "" {
			currentUser = os.Getenv("LOGNAME")
		}
		if currentUser != "" {
			out, runErr := exec.Command("loginctl", "show-user", currentUser).Output()
			if runErr != nil || !strings.Contains(string(out), "Linger=yes") {
				warn("linger enabled", "services won't survive logout — fix: loginctl enable-linger "+currentUser)
			} else {
				ok("linger enabled")
			}
		}
	}

	quadletDir := config.QuadletDir()
	if dirErr := checkDirWritable(quadletDir); dirErr != nil {
		fail("service config dir writable", dirErr.Error(), "mkdir -p "+quadletDir)
	} else {
		ok("service config dir writable")
	}

	dataDir := config.DataDir()
	if dirErr := checkDirWritable(dataDir); dirErr != nil {
		fail("data dir writable", dirErr.Error(), "mkdir -p "+dataDir)
	} else {
		ok("data dir writable")
	}

	// ── WSL2 ─────────────────────────────────────────────────────────────────
	// Only on WSL: the failure modes here (podman log driver, no tray host, slow
	// 9P bind mounts) don't exist on a native Linux or macOS host. `lerd wsl:setup`
	// fixes the first two in one shot.
	if wsl.IsWSL() {
		fmt.Fprintln(w, "\n[WSL2]")

		home, _ := os.UserHomeDir()
		cc := filepath.Join(home, ".config", "containers", "containers.conf")
		if b, readErr := os.ReadFile(cc); readErr == nil && wsl.HasEventsLoggerJournald(string(b)) {
			ok("podman events_logger journald")
		} else {
			warn("podman events_logger journald", "log views fail with --follow on WSL, run lerd wsl:setup")
		}

		out, _ := exec.Command("systemctl", "--user", "is-enabled", "lerd-tray.service").Output()
		if strings.TrimSpace(string(out)) == "masked" {
			ok("lerd-tray masked (no WSL tray host)")
		} else {
			warn("lerd-tray on WSL", "no tray host on WSL2 so the unit fails, run lerd wsl:setup")
		}

		if reg, regErr := config.LoadSites(); regErr == nil {
			var onMnt []string
			for _, s := range reg.Sites {
				if strings.HasPrefix(s.Path, "/mnt/") {
					onMnt = append(onMnt, s.Name)
				}
			}
			if len(onMnt) > 0 {
				warn("project paths on the WSL fs", "slow 9P mounts for: "+strings.Join(onMnt, ", ")+", move them under $HOME")
			} else {
				ok("project paths on the WSL fs")
			}
		}
	}

	// ── Configuration ────────────────────────────────────────────────────────
	fmt.Fprintln(w, "\n[Configuration]")

	cfgFile := config.GlobalConfigFile()
	if _, statErr := os.Stat(cfgFile); os.IsNotExist(statErr) {
		warn("config file", "not found — defaults will be used ("+cfgFile+")")
	} else {
		ok("config file exists")
	}

	cfg, cfgErr := config.LoadGlobal()
	if cfgErr != nil {
		fail("config loads", cfgErr.Error(), "check "+cfgFile+" for YAML syntax errors")
		cfg = nil
	} else {
		ok("config valid")
	}

	if cfg != nil {
		if cfg.PHP.DefaultVersion == "" {
			warn("PHP default version", "not set in config")
		} else {
			ok(fmt.Sprintf("PHP default version (%s)", cfg.PHP.DefaultVersion))
		}

		if cfg.Nginx.HTTPPort <= 0 || cfg.Nginx.HTTPSPort <= 0 {
			fail("nginx ports", fmt.Sprintf("http=%d https=%d", cfg.Nginx.HTTPPort, cfg.Nginx.HTTPSPort), "set valid ports in "+cfgFile)
		} else {
			ok(fmt.Sprintf("nginx ports (%d / %d)", cfg.Nginx.HTTPPort, cfg.Nginx.HTTPSPort))
		}

		for _, dir := range cfg.ParkedDirectories {
			if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
				warn(fmt.Sprintf("parked dir: %s", truncate(dir, 26)), "directory does not exist — run: mkdir -p "+dir)
			} else {
				ok(fmt.Sprintf("parked dir: %s", truncate(dir, 26)))
			}
		}
	}

	// ── DNS ──────────────────────────────────────────────────────────────────
	fmt.Fprintln(w, "\n[DNS]")

	dnsManaged := cfg == nil || cfg.DNS.Enabled
	tld := "test"
	if cfg != nil && cfg.DNS.TLD != "" {
		tld = cfg.DNS.TLD
	}

	if !dnsManaged {
		ok(fmt.Sprintf("DNS managed externally (lerd-dns disabled, TLD .%s)", tld))
	} else if tld == "" {
		fail("DNS TLD configured", "empty TLD in config", "set dns.tld in "+cfgFile)
	} else {
		ok(fmt.Sprintf("DNS TLD (.%s)", tld))
		// Layered diagnostic: walk the chain (container, config, port,
		// dig at 5300, resolver hookup, interface routing, system
		// lookup) so a one-line failure points at exactly which rung
		// broke instead of the historical "not resolving to 127.0.0.1".
		diag := dns.Diagnose(tld)
		for _, s := range diag.Steps {
			label := "  " + s.Name
			switch s.Status {
			case dns.StepOK:
				if s.Detail != "" {
					info(label, s.Detail)
				} else {
					ok(label)
				}
			case dns.StepFail:
				fail(label, s.Detail, s.Hint)
			case dns.StepWarn:
				warn(label, s.Detail)
			case dns.StepSkip:
				info(label, "skipped — "+s.Detail)
			}
		}
	}

	if dnsManaged {
		dnsRunning := services.Mgr.IsActive("lerd-dns")
		if !dnsRunning {
			if cr, _ := podman.ContainerRunning("lerd-dns"); cr {
				dnsRunning = true
			}
		}
		if !dnsRunning && PortInUse("5300") {
			warn("DNS port 5300", "port in use by another process, lerd-dns may fail to start (find: "+FindListenerCmd("5300")+")")
		}
	}

	// ── Ports ────────────────────────────────────────────────────────────────
	fmt.Fprintln(w, "\n[Ports]")

	nginxRunning, _ := podman.ContainerRunning("lerd-nginx")
	if nginxRunning {
		ok("port 80  (nginx running)")
		ok("port 443 (nginx running)")
	} else {
		if PortInUse("80") {
			fail("port 80", "in use by another process", "find the process: "+FindListenerCmd("80"))
		} else {
			ok("port 80  (free)")
		}
		if PortInUse("443") {
			fail("port 443", "in use by another process", "find the process: "+FindListenerCmd("443"))
		} else {
			ok("port 443 (free)")
		}
	}

	// ── Stopped service ports ────────────────────────────────────────────────
	// Surfaces the same diagnosis the UI shows on inactive service cards: if
	// a service unit is installed but stopped and its host port is already
	// bound by another process (a system-installed postgres, a stray docker
	// container, etc.), Start will fail with a generic bind error. List those
	// upfront so the user sees the conflict before clicking anything.
	fmt.Fprintln(w, "\n[Stopped service ports]")
	{
		var stoppedUnits []string
		for _, name := range append([]string{}, knownServices()...) {
			unit := "lerd-" + name
			if !services.Mgr.ContainerUnitInstalled(unit) {
				continue
			}
			if services.Mgr.IsActive(unit) {
				continue
			}
			stoppedUnits = append(stoppedUnits, unit)
		}
		customs, _ := config.ListCustomServices()
		for _, svc := range customs {
			unit := "lerd-" + svc.Name
			if !services.Mgr.ContainerUnitInstalled(unit) {
				continue
			}
			if services.Mgr.IsActive(unit) {
				continue
			}
			stoppedUnits = append(stoppedUnits, unit)
		}

		if len(stoppedUnits) == 0 {
			ok("no stopped services to check")
		} else {
			ssOut := PortListOutput()
			conflictsFound := 0
			for _, unit := range stoppedUnits {
				for _, c := range CollectPortChecks([]string{unit}) {
					if PortInUseIn(c.Port, ssOut) {
						conflictsFound++
						warn(fmt.Sprintf("%s port %s", c.Label, c.Port),
							fmt.Sprintf("in use by another process, %s start may fail (find: %s)", c.Label, FindListenerCmd(c.Port)))
					}
				}
			}
			if conflictsFound == 0 {
				ok(fmt.Sprintf("%d stopped service(s), no port conflicts", len(stoppedUnits)))
			}
		}
	}

	// ── Containers & Images ──────────────────────────────────────────────────
	fmt.Fprintln(w, "\n[Containers & Images]")

	if !services.Mgr.ContainerUnitInstalled("lerd-nginx") {
		fail("lerd-nginx service", "not installed", "run: lerd install")
	} else {
		ok("lerd-nginx service installed")
	}

	phpVersions, _ := phpPkg.ListInstalled()
	if len(phpVersions) == 0 {
		warn("PHP versions", "none installed — run: lerd use 8.4")
	}
	for _, v := range phpVersions {
		short := strings.ReplaceAll(v, ".", "")
		image := "lerd-php" + short + "-fpm:local"
		if !podman.ImageExists(image) {
			fail(fmt.Sprintf("PHP %s image", v), "missing", "lerd php:rebuild "+v)
		} else {
			ok(fmt.Sprintf("PHP %s image", v))
		}
	}

	if plan, planErr := cleanup.Inspect(false); planErr == nil && plan.ReclaimBytes() > 0 {
		info("Reclaimable disk", fmt.Sprintf("about %s (run: lerd cleanup)", humanSize(plan.ReclaimBytes())))
	}

	// ── Container → Host Connectivity ────────────────────────────────────────
	// The PHP-FPM containers reach the host (Xdebug, host-side services)
	// via the host.containers.internal /etc/hosts entry. lerd writes that
	// IP based on a real reachability probe — TCP-connect each candidate
	// from inside lerd-nginx to lerd-ui's :7073. If no candidate works,
	// Xdebug times out silently with no error in the FPM logs other than
	// "Time-out connecting to debugging client" (issue #186 redux). This
	// check surfaces the failure so the user gets a real diagnosis.
	fmt.Fprintln(w, "\n[Container → Host connectivity]")
	if !services.Mgr.IsActive("lerd-nginx") {
		warn("host reachability probe", "skipped — lerd-nginx not running (start lerd first)")
	} else if !services.Mgr.IsActive("lerd-ui") {
		warn("host reachability probe", "skipped — lerd-ui not running (the probe targets its :7073 listener)")
	} else if ip := podman.DetectHostGatewayIPProbeOnly(); ip != "" {
		ok(fmt.Sprintf("host reachable from containers (%s)", ip))
	} else {
		fail("host reachable from containers",
			"no candidate routed back to the host (Xdebug, inter-container → host calls will time out)",
			"check rootless podman / netavark / pasta routing; run: podman unshare --rootless-netns ip addr (expected: 169.254.1.2 on podman bridge or DNAT for it)")
	}

	if reg, regErr := config.LoadSites(); regErr == nil {
		for _, site := range reg.Sites {
			if site.Ignored || site.IsCustomContainer() || site.IsFrankenPHP() || site.IsHostProxy() {
				continue
			}
			hints := config.DetectFrankenPHPHints(site.Path)
			if len(hints) == 0 {
				continue
			}
			warn(fmt.Sprintf("site %s", site.Name),
				fmt.Sprintf("%s; switch with: lerd runtime frankenphp", hints[0].Reason))
		}
	}

	// ── Version Info ─────────────────────────────────────────────────────────
	fmt.Fprintln(w, "\n[Version Info]")

	info("lerd", version.String())

	if len(phpVersions) > 0 {
		info("PHP installed", strings.Join(phpVersions, ", "))
	} else {
		info("PHP installed", "(none)")
	}

	if cfg != nil {
		info("PHP default", cfg.PHP.DefaultVersion)
		info("Node default", cfg.Node.DefaultVersion)
	}

	if updateInfo, _ := lerdUpdate.CachedUpdateCheck(version.Version); updateInfo != nil {
		warn("lerd update available", updateInfo.LatestVersion+" — run: lerd update, lerd whatsnew to see changes")
	} else {
		ok("lerd up to date")
	}

	// ── Summary ──────────────────────────────────────────────────────────────
	fmt.Fprintln(w, "\n══════════════════════════════════════════════")
	switch {
	case fails > 0 && warns > 0:
		fmt.Fprintln(w, feedback.RedIf(useColor, fmt.Sprintf("%d failure(s), %d warning(s) found.", fails, warns)))
	case fails > 0:
		fmt.Fprintln(w, feedback.RedIf(useColor, fmt.Sprintf("%d failure(s) found.", fails)))
	case warns > 0:
		fmt.Fprintf(w, "%s  All critical checks passed.\n", feedback.AmberIf(useColor, fmt.Sprintf("%d warning(s) found.", warns)))
	default:
		fmt.Fprintln(w, feedback.GreenIf(useColor, "All checks passed."))
	}

	return fails, warns, nil
}

// checkDirWritable returns an error if the directory doesn't exist or isn't writable.
func checkDirWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot create: %v", err)
	}
	tmp, err := os.CreateTemp(dir, ".lerd-doctor-*")
	if err != nil {
		return fmt.Errorf("not writable: %v", err)
	}
	tmp.Close()
	os.Remove(tmp.Name())
	return nil
}

// PortInUse is implemented per-platform in doctor_linux.go / doctor_darwin.go.
//
// PortInUseIn checks whether the given TCP port appears in pre-fetched output
// from a port listing command (ss on Linux, lsof on macOS). Used by
// checkPortConflicts in startstop.go for batch checks.
func PortInUseIn(port, output string) bool {
	return strings.Contains(output, ":"+port+" ")
}
