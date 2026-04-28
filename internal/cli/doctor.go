package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dns"
	phpPkg "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
	lerdUpdate "github.com/geodro/lerd/internal/update"
	"github.com/geodro/lerd/internal/version"
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
	_, _, err := RunDoctorTo(os.Stdout, true)
	return err
}

// RunDoctorTo runs the full doctor diagnostic, writing human-readable output
// to w. When useColor is false ANSI escapes are stripped so the output is
// safe to embed in a plain-text file (used by `lerd bug-report`). Returns
// the failure and warning counts for callers that want to summarise.
func RunDoctorTo(w io.Writer, useColor bool) (fails, warns int, err error) {
	cR, cG, cY, cReset := colorRed, colorGreen, colorYellow, colorReset
	if !useColor {
		cR, cG, cY, cReset = "", "", "", ""
	}

	ok := func(label string) {
		fmt.Fprintf(w, "  %s%-34s%s OK\n", cG, label, cReset)
	}
	fail := func(label, msg, hint string) {
		fails++
		fmt.Fprintf(w, "  %s%-34s%s FAIL  %s\n    hint: %s\n", cR, label, cReset, msg, hint)
	}
	warn := func(label, msg string) {
		warns++
		fmt.Fprintf(w, "  %s%-34s%s WARN  %s\n", cY, label, cReset, msg)
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

	tld := "test"
	if cfg != nil && cfg.DNS.TLD != "" {
		tld = cfg.DNS.TLD
	}

	if tld == "" {
		fail("DNS TLD configured", "empty TLD in config", "set dns.tld in "+cfgFile)
	} else {
		ok(fmt.Sprintf("DNS TLD (.%s)", tld))
		if resolved, _ := dns.Check(tld); resolved {
			ok(fmt.Sprintf(".%s resolution working", tld))
		} else {
			fail(fmt.Sprintf(".%s resolution", tld), "not resolving to 127.0.0.1",
				dnsRestartHint())
		}
	}

	dnsRunning := services.Mgr.IsActive("lerd-dns")
	if !dnsRunning {
		if cr, _ := podman.ContainerRunning("lerd-dns"); cr {
			dnsRunning = true
		}
	}
	if !dnsRunning && portInUse("5300") {
		warn("DNS port 5300", "port in use by another process — lerd-dns may fail to start")
	}

	// ── Ports ────────────────────────────────────────────────────────────────
	fmt.Fprintln(w, "\n[Ports]")

	nginxRunning, _ := podman.ContainerRunning("lerd-nginx")
	if nginxRunning {
		ok("port 80  (nginx running)")
		ok("port 443 (nginx running)")
	} else {
		if portInUse("80") {
			fail("port 80", "in use by another process", "find the process: ss -tlnp sport = :80")
		} else {
			ok("port 80  (free)")
		}
		if portInUse("443") {
			fail("port 443", "in use by another process", "find the process: ss -tlnp sport = :443")
		} else {
			ok("port 443 (free)")
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
			if site.Ignored || site.IsCustomContainer() || site.IsFrankenPHP() {
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
		fmt.Fprintf(w, "%s%d failure(s), %d warning(s) found.%s\n", cR, fails, warns, cReset)
	case fails > 0:
		fmt.Fprintf(w, "%s%d failure(s) found.%s\n", cR, fails, cReset)
	case warns > 0:
		fmt.Fprintf(w, "%s%d warning(s) found.%s  All critical checks passed.\n", cY, warns, cReset)
	default:
		fmt.Fprintf(w, "%sAll checks passed.%s\n", cG, cReset)
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

// portInUse is implemented per-platform in doctor_linux.go / doctor_darwin.go.
//
// portInUseIn checks whether the given TCP port appears in pre-fetched output
// from a port listing command (ss on Linux, lsof on macOS). Used by
// checkPortConflicts in startstop.go for batch checks.
func portInUseIn(port, output string) bool {
	return strings.Contains(output, ":"+port+" ")
}
