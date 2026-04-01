package cli

import (
	"fmt"
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
	var fails, warns int

	ok := func(label string) {
		fmt.Printf("  %s%-34s%s OK\n", colorGreen, label, colorReset)
	}
	fail := func(label, msg, hint string) {
		fails++
		fmt.Printf("  %s%-34s%s FAIL  %s\n    hint: %s\n", colorRed, label, colorReset, msg, hint)
	}
	warn := func(label, msg string) {
		warns++
		fmt.Printf("  %s%-34s%s WARN  %s\n", colorYellow, label, colorReset, msg)
	}
	info := func(label, val string) {
		fmt.Printf("  %-34s %s\n", label, val)
	}

	fmt.Printf("Lerd Doctor  (version %s)\n", version.String())
	fmt.Println("══════════════════════════════════════════════")

	// ── Prerequisites ───────────────────────────────────────────────────────
	fmt.Println("\n[Prerequisites]")

	if _, err := exec.LookPath("podman"); err != nil {
		fail("podman binary", "not found in PATH", "install podman: https://podman.io/docs/installation")
	} else if err := podman.RunSilent("info"); err != nil {
		fail("podman", "podman info failed — daemon not running?", podmanDaemonHint())
	} else {
		ok("podman")
	}

	if runtime.GOOS == "linux" {
		if out, err := exec.Command("systemctl", "--user", "is-system-running").Output(); err != nil {
			// exit non-zero but "degraded" is acceptable
			state := strings.TrimSpace(string(out))
			if state == "degraded" {
				warn("systemd user session", "degraded — some units have failed")
			} else {
				fail("systemd user session", fmt.Sprintf("state=%q", state), "log in as a real user (not su); run: systemctl --user status")
			}
		} else {
			ok("systemd user session")
		}
	}

	if runtime.GOOS == "linux" {
		currentUser := os.Getenv("USER")
		if currentUser == "" {
			currentUser = os.Getenv("LOGNAME")
		}
		if currentUser != "" {
			out, err := exec.Command("loginctl", "show-user", currentUser).Output()
			if err != nil || !strings.Contains(string(out), "Linger=yes") {
				warn("linger enabled", "services won't survive logout — fix: loginctl enable-linger "+currentUser)
			} else {
				ok("linger enabled")
			}
		}
	}

	quadletDir := config.QuadletDir()
	if err := checkDirWritable(quadletDir); err != nil {
		fail("service config dir writable", err.Error(), "mkdir -p "+quadletDir)
	} else {
		ok("service config dir writable")
	}

	dataDir := config.DataDir()
	if err := checkDirWritable(dataDir); err != nil {
		fail("data dir writable", err.Error(), "mkdir -p "+dataDir)
	} else {
		ok("data dir writable")
	}

	// ── Configuration ────────────────────────────────────────────────────────
	fmt.Println("\n[Configuration]")

	cfgFile := config.GlobalConfigFile()
	if _, err := os.Stat(cfgFile); os.IsNotExist(err) {
		warn("config file", "not found — defaults will be used ("+cfgFile+")")
	} else {
		ok("config file exists")
	}

	cfg, cfgErr := config.LoadGlobal()
	if cfgErr != nil {
		fail("config loads", cfgErr.Error(), "check "+cfgFile+" for YAML syntax errors")
		// Can't proceed with config-dependent checks
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
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				warn(fmt.Sprintf("parked dir: %s", truncate(dir, 26)), "directory does not exist — run: mkdir -p "+dir)
			} else {
				ok(fmt.Sprintf("parked dir: %s", truncate(dir, 26)))
			}
		}
	}

	// ── DNS ──────────────────────────────────────────────────────────────────
	fmt.Println("\n[DNS]")

	tld := "test"
	if cfg != nil && cfg.DNS.TLD != "" {
		tld = cfg.DNS.TLD
	}

	if tld == "" {
		fail("DNS TLD configured", "empty TLD in config", "set dns.tld in "+cfgFile)
	} else {
		ok(fmt.Sprintf("DNS TLD (.%s)", tld))
		if resolved, _ := dns.Check(tld); resolved {
			ok(fmt.Sprintf(".%s resolves to 127.0.0.1", tld))
		} else {
			fail(fmt.Sprintf(".%s resolution", tld), "not resolving to 127.0.0.1",
				dnsRestartHint())
		}
	}

	// Port 5300 conflict (only warn when lerd-dns is not actively managing port 5300)
	dnsRunning := services.Mgr.IsActive("lerd-dns")
	if !dnsRunning {
		// Also accept a running container (Linux path).
		if cr, _ := podman.ContainerRunning("lerd-dns"); cr {
			dnsRunning = true
		}
	}
	if !dnsRunning && portInUse("5300") {
		warn("DNS port 5300", "port in use by another process — lerd-dns may fail to start")
	}

	// ── Ports ────────────────────────────────────────────────────────────────
	fmt.Println("\n[Ports]")

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
	fmt.Println("\n[Containers & Images]")

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

	// ── Version Info ─────────────────────────────────────────────────────────
	fmt.Println("\n[Version Info]")

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
	fmt.Println("\n══════════════════════════════════════════════")
	switch {
	case fails > 0 && warns > 0:
		fmt.Printf("%s%d failure(s), %d warning(s) found.%s\n", colorRed, fails, warns, colorReset)
	case fails > 0:
		fmt.Printf("%s%d failure(s) found.%s\n", colorRed, fails, colorReset)
	case warns > 0:
		fmt.Printf("%s%d warning(s) found.%s  All critical checks passed.\n", colorYellow, warns, colorReset)
	default:
		fmt.Printf("%sAll checks passed.%s\n", colorGreen, colorReset)
	}

	return nil
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
