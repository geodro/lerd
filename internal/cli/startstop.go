package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dns"
	phpPkg "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	lerdSystemd "github.com/geodro/lerd/internal/systemd"
	"github.com/spf13/cobra"
)

// ensureFPMImages rebuilds any PHP FPM images that have been removed.
func ensureFPMImages() {
	versions, _ := phpPkg.ListInstalled()
	for _, v := range versions {
		short := strings.ReplaceAll(v, ".", "")
		image := "lerd-php" + short + "-fpm:local"
		if err := podman.RunSilent("image", "exists", image); err != nil {
			fmt.Printf("  PHP %s image missing — rebuilding...\n", v)
			if err := podman.BuildFPMImage(v); err != nil {
				fmt.Printf("  WARN: could not rebuild PHP %s image: %v\n", v, err)
			}
		}
	}
}

// NewStartCmd returns the start command.
func NewStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start Lerd (DNS, nginx, PHP-FPM, and installed services)",
		RunE:  runStart,
	}
}

// NewStopCmd returns the stop command.
func NewStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop Lerd (DNS, nginx, PHP-FPM, and running services)",
		RunE:  runStop,
	}
}

func coreUnits() []string {
	units := []string{"lerd-dns", "lerd-nginx", "lerd-ui"}
	versions, _ := phpPkg.ListInstalled()
	for _, v := range versions {
		short := strings.ReplaceAll(v, ".", "")
		units = append(units, "lerd-php"+short+"-fpm")
	}
	return units
}

// installedServiceUnits returns service units that have a quadlet file installed.
func installedServiceUnits() []string {
	var units []string
	for _, svc := range knownServices {
		if podman.QuadletInstalled("lerd-" + svc) {
			units = append(units, "lerd-"+svc)
		}
	}
	customs, _ := config.ListCustomServices()
	for _, svc := range customs {
		if podman.QuadletInstalled("lerd-" + svc.Name) {
			units = append(units, "lerd-"+svc.Name)
		}
	}
	return units
}

type startResult struct {
	unit string
	err  error
}

func runStart(_ *cobra.Command, _ []string) error {
	// Rebuild missing FPM images in the background so they don't delay startup.
	go ensureFPMImages()

	units := append(coreUnits(), installedServiceUnits()...)
	fmt.Println("Starting Lerd...")

	results := make([]startResult, len(units))
	var wg sync.WaitGroup
	for i, u := range units {
		wg.Add(1)
		go func(idx int, unit string) {
			defer wg.Done()
			results[idx] = startResult{unit: unit, err: podman.StartUnit(unit)}
		}(i, u)
	}
	wg.Wait()

	for _, r := range results {
		fmt.Printf("  --> %s ... ", r.unit)
		if r.err != nil {
			fmt.Printf("WARN (%v)\n", r.err)
		} else {
			fmt.Println("OK")
		}
	}

	// Wait for lerd-dns to be ready before configuring the resolver.
	// systemctl start returns when the unit is active, but dnsmasq inside the
	// container may not be listening yet. If we set resolvectl to use port 5300
	// before it's up, systemd-resolved marks it failed and falls back to the
	// upstream DNS server, breaking .test resolution until manually fixed.
	if err := dns.WaitReady(10 * time.Second); err != nil {
		fmt.Printf("  WARN: %v\n", err)
	}

	// Re-apply DNS routing so .test resolves via lerd-dns on every start.
	// resolvectl settings are ephemeral and reset on reboot; the NM dispatcher
	// script fires on interface "up" but that event precedes lerd-dns starting.
	if err := dns.ConfigureResolver(); err != nil {
		fmt.Printf("  WARN: DNS resolver config: %v\n", err)
	}

	// Start the tray applet if it is not already running.
	// Prefer the systemd service when enabled; otherwise launch directly.
	if lerdSystemd.IsServiceEnabled("lerd-tray") {
		fmt.Print("  --> lerd-tray ... ")
		if err := lerdSystemd.StartService("lerd-tray"); err != nil {
			fmt.Printf("WARN (%v)\n", err)
		} else {
			fmt.Println("OK")
		}
	} else if !trayRunning() {
		fmt.Print("  --> lerd-tray ... ")
		exe, err := os.Executable()
		if err == nil {
			err = exec.Command(exe, "tray").Start()
		}
		if err != nil {
			fmt.Printf("WARN (%v)\n", err)
		} else {
			fmt.Println("OK")
		}
	}

	return nil
}

// trayRunning returns true if a lerd tray daemon process is already running.
func trayRunning() bool {
	out, err := exec.Command("pgrep", "-f", "lerd tray").Output()
	return err == nil && len(strings.TrimSpace(string(out))) > 0
}

func runStop(_ *cobra.Command, _ []string) error {
	units := append(coreUnits(), installedServiceUnits()...)
	fmt.Println("Stopping Lerd...")

	results := make([]startResult, len(units))
	var wg sync.WaitGroup
	for i, u := range units {
		wg.Add(1)
		go func(idx int, unit string) {
			defer wg.Done()
			results[idx] = startResult{unit: unit, err: podman.StopUnit(unit)}
		}(i, u)
	}
	wg.Wait()

	for _, r := range results {
		fmt.Printf("  --> %s ... ", r.unit)
		if r.err != nil {
			fmt.Printf("WARN (%v)\n", r.err)
		} else {
			fmt.Println("OK")
		}
	}
	return nil
}
