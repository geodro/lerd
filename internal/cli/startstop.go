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
	"github.com/geodro/lerd/internal/nginx"
	phpPkg "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
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
		Short: "Stop Lerd containers (DNS, nginx, PHP-FPM, and running services)",
		RunE:  runStop,
	}
}

// NewQuitCmd returns the quit command.
func NewQuitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "quit",
		Short: "Stop all Lerd processes and containers (including UI, watcher, and tray)",
		RunE:  runQuit,
	}
}

// coreUnits returns the container units managed by lerd start/stop.
// Does not include lerd-ui or lerd-watcher — those are added separately in runStart.
func coreUnits() []string {
	units := []string{"lerd-dns", "lerd-nginx"}
	versions, _ := phpPkg.ListInstalled()
	for _, v := range versions {
		short := strings.ReplaceAll(v, ".", "")
		units = append(units, "lerd-php"+short+"-fpm")
	}
	return units
}

// installedServiceUnits returns service units that have a quadlet file installed
// and have not been manually stopped by the user. Used for lerd start.
func installedServiceUnits() []string {
	var units []string
	for _, svc := range knownServices {
		if services.Mgr.ContainerUnitInstalled("lerd-"+svc) && !config.ServiceIsPaused(svc) {
			units = append(units, "lerd-"+svc)
		}
	}
	customs, _ := config.ListCustomServices()
	for _, svc := range customs {
		if services.Mgr.ContainerUnitInstalled("lerd-"+svc.Name) && !config.ServiceIsPaused(svc.Name) {
			units = append(units, "lerd-"+svc.Name)
		}
	}
	return units
}

// allInstalledServiceUnits returns all service units that have a quadlet file
// installed, regardless of paused state. Used for lerd stop.
func allInstalledServiceUnits() []string {
	var units []string
	for _, svc := range knownServices {
		if services.Mgr.ContainerUnitInstalled("lerd-" + svc) {
			units = append(units, "lerd-"+svc)
		}
	}
	customs, _ := config.ListCustomServices()
	for _, svc := range customs {
		if services.Mgr.ContainerUnitInstalled("lerd-" + svc.Name) {
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
	// On macOS, Podman runs inside a Linux VM. Start it before any containers.
	ensurePodmanMachineRunning()

	// Rebuild missing FPM images in the background so they don't delay startup.
	go ensureFPMImages()

	// Ensure the lerd Podman network and DNS image exist — both can be lost
	// after a Podman Machine restart when the VM's storage is reset.
	if err := podman.EnsureNetwork("lerd"); err != nil {
		fmt.Printf("  WARN: podman network: %v\n", err)
	}
	ensureDNSImageForStart()

	// Rewrite nginx.conf so any config changes in new binary versions take effect.
	if err := nginx.EnsureNginxConfig(); err != nil {
		fmt.Printf("  WARN: nginx config: %v\n", err)
	}
	if err := nginx.EnsureLerdVhost(); err != nil {
		fmt.Printf("  WARN: lerd vhost: %v\n", err)
	}

	units := append(coreUnits(), installedServiceUnits()...)
	units = append(units, "lerd-ui", "lerd-watcher")
	units = append(units, registeredQueueUnits()...)
	units = append(units, registeredStripeUnits()...)
	units = append(units, registeredScheduleUnits()...)
	units = append(units, registeredReverbUnits()...)
	fmt.Println("Starting Lerd...")

	results := make([]startResult, len(units))
	var wg sync.WaitGroup
	for i, u := range units {
		wg.Add(1)
		go func(idx int, unit string) {
			defer wg.Done()
			results[idx] = startResult{unit: unit, err: services.Mgr.Start(unit)}
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

	// Restart the tray applet, stopping any existing instance first.
	// Prefer the systemd service when enabled; otherwise launch directly.
	fmt.Print("  --> lerd-tray ... ")
	if services.Mgr.IsEnabled("lerd-tray") {
		if err := services.Mgr.Restart("lerd-tray"); err != nil {
			fmt.Printf("WARN (%v)\n", err)
		} else {
			fmt.Println("OK")
		}
	} else {
		killTray()
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

// killTray kills any running lerd tray process (launched directly or as lerd-tray binary).
func killTray() {
	exec.Command("pkill", "-f", "lerd tray").Run()
	exec.Command("pkill", "-f", "lerd-tray").Run()
}

// registeredStripeUnits returns unit names for all lerd-stripe-* service files.
func registeredStripeUnits() []string {
	return services.Mgr.ListServiceUnits("lerd-stripe-*")
}

// registeredQueueUnits returns unit names for all lerd-queue-* service files.
func registeredQueueUnits() []string {
	return services.Mgr.ListServiceUnits("lerd-queue-*")
}

// registeredScheduleUnits returns unit names for all lerd-schedule-* service files.
func registeredScheduleUnits() []string {
	return services.Mgr.ListServiceUnits("lerd-schedule-*")
}

// registeredReverbUnits returns unit names for all lerd-reverb-* service files.
func registeredReverbUnits() []string {
	return services.Mgr.ListServiceUnits("lerd-reverb-*")
}

// RunStart starts all lerd services (exported for use by the UI server).
func RunStart() error { return runStart(nil, nil) }

// RunStop stops lerd containers (exported for use by the UI server).
func RunStop() error { return runStop(nil, nil) }

// RunQuit stops all lerd processes and containers (exported for use by the UI server).
func RunQuit() error { return runQuit(nil, nil) }

func runStop(_ *cobra.Command, _ []string) error {
	units := append(coreUnits(), allInstalledServiceUnits()...)
	units = append(units, registeredQueueUnits()...)
	units = append(units, registeredStripeUnits()...)
	units = append(units, registeredScheduleUnits()...)
	units = append(units, registeredReverbUnits()...)
	fmt.Println("Stopping Lerd...")

	results := make([]startResult, len(units))
	var wg sync.WaitGroup
	for i, u := range units {
		wg.Add(1)
		go func(idx int, unit string) {
			defer wg.Done()
			results[idx] = startResult{unit: unit, err: services.Mgr.Stop(unit)}
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

func runQuit(_ *cobra.Command, _ []string) error {
	// Stop containers and services (same as stop).
	if err := runStop(nil, nil); err != nil {
		return err
	}

	// Stop process units.
	for _, unit := range []string{"lerd-ui", "lerd-watcher"} {
		fmt.Printf("  --> %s ... ", unit)
		if err := services.Mgr.Stop(unit); err != nil {
			fmt.Printf("WARN (%v)\n", err)
		} else {
			fmt.Println("OK")
		}
	}

	// Kill the tray.
	killTray()
	fmt.Println("  --> lerd-tray ... OK")

	return nil
}
