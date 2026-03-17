package cli

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dns"
	phpPkg "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/spf13/cobra"
)

const (
	colorGreen  = "\033[32m"
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
	colorReset  = "\033[0m"
)

func ok2(label string)              { fmt.Printf("  %s%-30s%s OK\n", colorGreen, label, colorReset) }
func fail2(label, msg, hint string) { fmt.Printf("  %s%-30s%s FAIL (%s)\n    hint: %s\n", colorRed, label, colorReset, msg, hint) }
func warn2(label, msg string)       { fmt.Printf("  %s%-30s%s WARN (%s)\n", colorYellow, label, colorReset, msg) }

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
			"run 'lerd install' to reconfigure, or: sudo systemctl restart NetworkManager")
	}

	// Nginx
	fmt.Println("\n[Nginx]")
	running, _ := podman.ContainerRunning("lerd-nginx")
	if running {
		ok2("lerd-nginx container")
	} else {
		fail2("lerd-nginx container",
			"not running",
			"systemctl --user start lerd-nginx  |  check: systemctl --user status lerd-nginx")
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
		containerName := "lerd-php" + short + "-fpm"
		running, _ := podman.ContainerRunning(containerName)
		if running {
			ok2("PHP " + v + " FPM")
		} else {
			fail2("PHP "+v+" FPM",
				containerName+" not running",
				"systemctl --user start "+containerName)
		}
	}

	// Services — only show services that have a quadlet file installed
	fmt.Println("\n[Services]")
	installedCount := 0
	for _, svc := range knownServices {
		unit := "lerd-" + svc
		if !podman.QuadletInstalled(unit) {
			continue
		}
		installedCount++
		status, _ := podman.UnitStatus(unit)
		switch status {
		case "active":
			ok2(svc)
		case "inactive":
			warn2(svc, "inactive — start with: lerd service start "+svc)
		default:
			fail2(svc, status, "systemctl --user status "+unit)
		}
	}
	if installedCount == 0 {
		fmt.Println("  No services installed. Start one with: lerd service start <name>")
	}

	// Certificate expiry for secured sites
	fmt.Println("\n[TLS Certificates]")
	reg, err := config.LoadSites()
	if err == nil {
		hasSecured := false
		for _, s := range reg.Sites {
			if !s.Secured {
				continue
			}
			hasSecured = true
			certPath := filepath.Join(config.CertsDir(), "sites", s.Domain+".crt")
			if exp, err := certExpiry(certPath); err != nil {
				fail2(s.Domain, "cannot read cert", "run: lerd secure "+s.Domain)
			} else {
				remaining := time.Until(exp)
				days := int(remaining.Hours() / 24)
				if days < 30 {
					warn2(s.Domain, fmt.Sprintf("expires in %d days", days))
				} else {
					ok2(fmt.Sprintf("%s (expires in %d days)", s.Domain, days))
				}
			}
		}
		if !hasSecured {
			fmt.Println("  No secured sites.")
		}
	}

	fmt.Println()
	return nil
}

// certExpiry reads the expiry date from a PEM certificate file.
func certExpiry(path string) (time.Time, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, err
	}
	cert, err := tls.X509KeyPair(data, data)
	if err != nil {
		// Try parsing as just a cert
		parsed, err2 := x509.ParseCertificate(data)
		if err2 != nil {
			return time.Time{}, err
		}
		return parsed.NotAfter, nil
	}
	if len(cert.Certificate) == 0 {
		return time.Time{}, fmt.Errorf("no certificate found")
	}
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return time.Time{}, err
	}
	return parsed.NotAfter, nil
}
