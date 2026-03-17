package cli

import (
	"fmt"
	"strings"

	phpPkg "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/spf13/cobra"
)

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
	units := []string{"lerd-dns", "lerd-nginx"}
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

func runStart(_ *cobra.Command, _ []string) error {
	units := append(coreUnits(), installedServiceUnits()...)
	fmt.Println("Starting Lerd...")
	for _, u := range units {
		fmt.Printf("  --> %s ... ", u)
		if err := podman.StartUnit(u); err != nil {
			fmt.Printf("WARN (%v)\n", err)
		} else {
			fmt.Println("OK")
		}
	}
	return nil
}

func runStop(_ *cobra.Command, _ []string) error {
	units := append(coreUnits(), installedServiceUnits()...)
	fmt.Println("Stopping Lerd...")
	for _, u := range units {
		fmt.Printf("  --> %s ... ", u)
		if err := podman.StopUnit(u); err != nil {
			fmt.Printf("WARN (%v)\n", err)
		} else {
			fmt.Println("OK")
		}
	}
	return nil
}
