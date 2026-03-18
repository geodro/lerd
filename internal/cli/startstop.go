package cli

import (
	"fmt"
	"strings"
	"sync"

	"github.com/geodro/lerd/internal/dns"
	phpPkg "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
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

	// Re-apply DNS routing so .test resolves via lerd-dns on every start.
	// resolvectl settings are ephemeral and reset on reboot; the NM dispatcher
	// script fires on interface "up" but that event precedes lerd-dns starting.
	if err := dns.ConfigureResolver(); err != nil {
		fmt.Printf("  WARN: DNS resolver config: %v\n", err)
	}

	return nil
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
