package cli

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dns"
	phpPkg "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
	lerdUpdate "github.com/geodro/lerd/internal/update"
	"github.com/geodro/lerd/internal/version"
	"github.com/spf13/cobra"
)

const (
	colorGreen  = "\033[32m"
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
	colorReset  = "\033[0m"
)

func ok2(label string) { fmt.Printf("  %s%-30s%s OK\n", colorGreen, label, colorReset) }
func fail2(label, msg, hint string) {
	fmt.Printf("  %s%-30s%s FAIL (%s)\n    hint: %s\n", colorRed, label, colorReset, msg, hint)
}
func warn2(label, msg string) {
	fmt.Printf("  %s%-30s%s WARN (%s)\n", colorYellow, label, colorReset, msg)
}

// NewStatusCmd returns the status command.
func NewStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show overall Lerd health status",
		RunE:  runStatus,
	}
}

func runStatus(_ *cobra.Command, _ []string) error {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}

	fmt.Println("Lerd Status")
	fmt.Println("═══════════════════════════════════════")

	// DNS check
	fmt.Println("\n[DNS]")
	ok, _ := dns.Check(cfg.DNS.TLD)
	if ok {
		ok2(fmt.Sprintf(".%s resolution", cfg.DNS.TLD))
	} else {
		fail2(fmt.Sprintf(".%s resolution", cfg.DNS.TLD),
			"not resolving",
			dnsRestartHint())
	}

	// Nginx
	fmt.Println("\n[Nginx]")
	running, _ := podman.ContainerRunning("lerd-nginx")
	if running {
		ok2("lerd-nginx container")
	} else {
		fail2("lerd-nginx container",
			"not running",
			serviceStatusHint("lerd-nginx"))
	}

	// PHP FPM
	fmt.Println("\n[PHP FPM]")
	versions, _ := phpPkg.ListInstalled()
	if len(versions) == 0 {
		warn2("PHP versions", "none installed — run: lerd use 8.4")
	}
	for _, v := range versions {
		short := ""
		for _, c := range v {
			if c != '.' {
				short += string(c)
			}
		}
		image := "lerd-php" + short + "-fpm:local"
		containerName := "lerd-php" + short + "-fpm"
		if err := podman.RunSilent("image", "exists", image); err != nil {
			fail2("PHP "+v+" FPM",
				"image missing",
				"lerd php:rebuild "+v)
			continue
		}
		running, _ := podman.ContainerRunning(containerName)
		if running {
			ok2("PHP " + v + " FPM")
		} else if activePHPVersions()[v] {
			fail2("PHP "+v+" FPM",
				containerName+" not running",
				"systemctl --user start "+containerName)
		} else {
			warn2("PHP "+v+" FPM", "stopped (no sites using this version)")
		}
	}

	// Watcher
	fmt.Println("\n[Watcher]")
	if services.Mgr.IsActive("lerd-watcher") {
		ok2("lerd-watcher")
	} else {
		fail2("lerd-watcher", "not running", serviceStartHint("lerd-watcher"))
	}

	// Services — only show services that have a quadlet file installed
	fmt.Println("\n[Services]")
	installedCount := 0
	for _, svc := range knownServices {
		unit := "lerd-" + svc
		if !services.Mgr.ContainerUnitInstalled(unit) {
			continue
		}
		installedCount++
		status, _ := services.Mgr.UnitStatus(unit)
		switch status {
		case "active":
			ok2(svc)
		case "inactive":
			if config.CountSitesUsingService(svc) == 0 {
				warn2(svc, "no sites using this service")
			} else {
				warn2(svc, "inactive — start with: lerd service start "+svc)
			}
		default:
			fail2(svc, status, serviceStatusHint(unit))
		}
	}
	customs, _ := config.ListCustomServices()
	for _, svc := range customs {
		unit := "lerd-" + svc.Name
		if !services.Mgr.ContainerUnitInstalled(unit) {
			continue
		}
		installedCount++
		status, _ := services.Mgr.UnitStatus(unit)
		label := svc.Name + " [custom]"
		switch status {
		case "active":
			ok2(label)
		case "inactive":
			if config.CountSitesUsingService(svc.Name) == 0 {
				warn2(label, "no sites using this service")
			} else {
				warn2(label, "inactive — start with: lerd service start "+svc.Name)
			}
		default:
			fail2(label, status, serviceStatusHint(unit))
		}
	}
	if installedCount == 0 {
		fmt.Println("  No services installed. Start one with: lerd service start <name>")
	}

	// Workers
	fmt.Println("\n[Workers]")
	{
		workerReg, wErr := config.LoadSites()
		if wErr == nil {
			hasWorkers := false
			for _, s := range workerReg.Sites {
				if s.Ignored || s.Paused {
					continue
				}
				// Check built-in worker types.
				for _, w := range []string{"queue", "schedule", "reverb", "horizon"} {
					unit := "lerd-" + w + "-" + s.Name
					status, _ := podman.UnitStatusFn(unit)
					switch status {
					case "active":
						ok2(fmt.Sprintf("%s/%s", s.Name, w))
						hasWorkers = true
					case "activating":
						warn2(s.Name+"/"+w, "restarting — check logs: journalctl --user -u "+unit+" -n 20")
						hasWorkers = true
					case "failed":
						fail2(s.Name+"/"+w, "failed", "journalctl --user -u "+unit+" -n 20")
						hasWorkers = true
					}
				}
				// Check custom framework workers.
				fwName := s.Framework
				if fwName == "" {
					fwName, _ = config.DetectFramework(s.Path)
				}
				if fw, ok := config.GetFramework(fwName); ok && fw.Workers != nil {
					for wName, wDef := range fw.Workers {
						switch wName {
						case "queue", "schedule", "reverb":
							continue
						}
						if wDef.Check != nil && !config.MatchesRule(s.Path, *wDef.Check) {
							continue
						}
						unit := "lerd-" + wName + "-" + s.Name
						status, _ := podman.UnitStatusFn(unit)
						switch status {
						case "active":
							ok2(fmt.Sprintf("%s/%s", s.Name, wName))
							hasWorkers = true
						case "activating":
							warn2(s.Name+"/"+wName, "restarting — check logs: journalctl --user -u "+unit+" -n 20")
							hasWorkers = true
						case "failed":
							fail2(s.Name+"/"+wName, "failed", "journalctl --user -u "+unit+" -n 20")
							hasWorkers = true
						}
					}
				}
				// Stripe listener.
				if stripeStatus, _ := podman.UnitStatusFn("lerd-stripe-" + s.Name); stripeStatus == "active" {
					ok2(fmt.Sprintf("%s/stripe", s.Name))
					hasWorkers = true
				} else if stripeStatus == "failed" || stripeStatus == "activating" {
					label := s.Name + "/stripe"
					if stripeStatus == "activating" {
						warn2(label, "restarting")
					} else {
						fail2(label, "failed", "journalctl --user -u lerd-stripe-"+s.Name+" -n 20")
					}
					hasWorkers = true
				}
			}
			if !hasWorkers {
				fmt.Println("  No workers running.")
			}
		}
	}

	// Certificate expiry for secured sites
	fmt.Println("\n[TLS Certificates]")
	reg, err := config.LoadSites()
	if err == nil {
		hasSecured := false
		for _, s := range reg.Sites {
			if s.Ignored || !s.Secured {
				continue
			}
			hasSecured = true
			certPath := filepath.Join(config.CertsDir(), "sites", s.PrimaryDomain()+".crt")
			if exp, err := certExpiry(certPath); err != nil {
				fail2(s.PrimaryDomain(), "cannot read cert", "run: lerd secure "+s.PrimaryDomain())
			} else {
				remaining := time.Until(exp)
				days := int(remaining.Hours() / 24)
				if days < 30 {
					warn2(s.PrimaryDomain(), fmt.Sprintf("expires in %d days", days))
				} else {
					ok2(fmt.Sprintf("%s (expires in %d days)", s.PrimaryDomain(), days))
				}
			}
		}
		if !hasSecured {
			fmt.Println("  No secured sites.")
		}
	}

	// LAN exposure + remote dashboard access
	fmt.Println("\n[Remote Access]")
	lanIP, _ := detectPrimaryLANIP()
	printRemoteAccessStatus(cfg, lanIP)

	// Update notice
	if info, _ := lerdUpdate.CachedUpdateCheck(version.Version); info != nil {
		printUpdateNotice(info)
	}

	fmt.Println()
	return nil
}

// printRemoteAccessStatus renders the [Remote Access] section of `lerd status`.
// Split out from runStatus so it can be tested without mocking podman/DNS/sites.
// lanIP may be empty — the caller is responsible for detection so tests can
// inject a deterministic value.
func printRemoteAccessStatus(cfg *config.GlobalConfig, lanIP string) {
	if cfg.LAN.Exposed {
		ip := lanIP
		if ip == "" {
			ip = "(unknown)"
		}
		ok2(fmt.Sprintf("LAN exposure (%s)", ip))
	} else {
		warn2("LAN exposure", "loopback only — enable with: lerd lan expose")
	}
	if cfg.UI.PasswordHash != "" {
		ok2(fmt.Sprintf("Dashboard remote access (user: %s)", cfg.UI.Username))
	} else {
		warn2("Dashboard remote access", "LAN clients get 403 — enable with: lerd remote-control on")
	}
}

// printUpdateNotice prints a highlighted banner when a new lerd version is available.
func printUpdateNotice(info *lerdUpdate.UpdateInfo) {
	bar := "══════════════════════════════════════════════"
	fmt.Println()
	fmt.Printf("%s%s%s\n", colorYellow, bar, colorReset)
	fmt.Printf("%s  Update available: %s  →  run: lerd update%s\n", colorYellow, info.LatestVersion, colorReset)
	fmt.Printf("%s  Run lerd whatsnew to see what changed.%s\n", colorYellow, colorReset)
	fmt.Printf("%s%s%s\n", colorYellow, bar, colorReset)
}

// certExpiry reads the expiry date from a PEM certificate file.
func certExpiry(path string) (time.Time, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return time.Time{}, fmt.Errorf("no PEM block found")
	}
	parsed, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.NotAfter, nil
}
