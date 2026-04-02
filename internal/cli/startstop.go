package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dns"
	"github.com/geodro/lerd/internal/nginx"
	phpPkg "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	lerdSystemd "github.com/geodro/lerd/internal/systemd"
	"github.com/spf13/cobra"
)

// quadletImage reads the Image= value from an installed quadlet file.
// Returns "" if the file cannot be read or has no Image= line.
func quadletImage(unit string) string {
	path := filepath.Join(config.QuadletDir(), unit+".container")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if after, ok := strings.CutPrefix(line, "Image="); ok {
			return strings.TrimSpace(after)
		}
	}
	return ""
}

// ensureImages checks all images required by units that are about to start and
// builds or pulls any that are missing, using the parallel spinner UI.
func ensureImages() {
	units := append(coreUnits(), installedServiceUnits()...)
	var jobs []BuildJob
	seen := map[string]bool{}

	for _, unit := range units {
		image := quadletImage(unit)
		if image == "" || seen[image] {
			continue
		}
		seen[image] = true

		if podman.RunSilent("image", "exists", image) == nil {
			continue // already present
		}

		img := image
		switch {
		case img == "lerd-dnsmasq:local":
			jobs = append(jobs, BuildJob{
				Label: "Building dnsmasq",
				Run: func(w io.Writer) error {
					containerfile := "FROM docker.io/library/alpine:latest\nRUN apk add --no-cache dnsmasq\n"
					cmd := exec.Command("podman", "build", "-t", "lerd-dnsmasq:local", "-")
					cmd.Stdin = strings.NewReader(containerfile)
					cmd.Stdout = w
					cmd.Stderr = w
					return cmd.Run()
				},
			})

		case strings.HasPrefix(img, "lerd-php") && strings.HasSuffix(img, "-fpm:local"):
			// Extract version from image name, e.g. lerd-php84-fpm:local → 8.4
			short := strings.TrimSuffix(strings.TrimPrefix(img, "lerd-php"), "-fpm:local")
			ver := short[:1] + "." + short[1:]
			v := ver
			jobs = append(jobs, BuildJob{
				Label: "PHP " + v,
				Run:   func(w io.Writer) error { return podman.BuildFPMImageTo(v, false, w) },
			})

		default:
			label := img
			jobs = append(jobs, BuildJob{
				Label: "Pulling " + label,
				Run: func(w io.Writer) error {
					cmd := exec.Command("podman", "pull", label)
					cmd.Stdout = w
					cmd.Stderr = w
					return cmd.Run()
				},
			})
		}
	}

	if len(jobs) > 0 {
		RunParallel(jobs) //nolint:errcheck
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
		if podman.QuadletInstalled("lerd-"+svc) && !config.ServiceIsPaused(svc) {
			units = append(units, "lerd-"+svc)
		}
	}
	customs, _ := config.ListCustomServices()
	for _, svc := range customs {
		if podman.QuadletInstalled("lerd-"+svc.Name) && !config.ServiceIsPaused(svc.Name) {
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

func runStart(_ *cobra.Command, _ []string) error {
	// Build or pull any missing images before starting containers.
	ensureImages()

	// Rewrite nginx.conf so any config changes in new binary versions take effect.
	if err := nginx.EnsureNginxConfig(); err != nil {
		fmt.Printf("  WARN: nginx config: %v\n", err)
	}
	if err := nginx.EnsureLerdVhost(); err != nil {
		fmt.Printf("  WARN: lerd vhost: %v\n", err)
	}

	// Refresh dnsmasq upstream config from the current system DNS before lerd-dns starts.
	// This ensures the config reflects any DNS changes (new servers added, DHCP change)
	// that occurred since the last run without requiring a full reinstall.
	if err := dns.WriteDnsmasqConfig(config.DnsmasqDir()); err != nil {
		fmt.Printf("  WARN: dns config: %v\n", err)
	}

	// Write the shared hosts file mounted into PHP containers at /etc/hosts.
	if err := podman.WriteContainerHosts(); err != nil {
		fmt.Printf("  WARN: container hosts file: %v\n", err)
	}

	units := append(coreUnits(), installedServiceUnits()...)
	units = append(units, "lerd-ui", "lerd-watcher")
	units = append(units, registeredQueueUnits()...)
	units = append(units, registeredStripeUnits()...)
	units = append(units, registeredScheduleUnits()...)
	units = append(units, registeredReverbUnits()...)

	fmt.Println("Starting Lerd...")

	jobs := make([]BuildJob, len(units))
	for i, u := range units {
		unit := u
		label := strings.TrimPrefix(unit, "lerd-")
		jobs[i] = BuildJob{
			Label: label,
			Run: func(w io.Writer) error {
				if unit == "lerd-dns" {
					// Always restart lerd-dns to pick up the refreshed dnsmasq config
					// and clear any stale cached DNS entries.
					return podman.RestartUnit(unit)
				}
				return podman.StartUnit(unit)
			},
		}
	}
	RunParallel(jobs) //nolint:errcheck

	// Sync the pasta DNS proxy (169.254.1.1) as the aardvark-dns upstream for the lerd
	// network. This address chains through systemd-resolved, which resolves both .test
	// domains (via lerd-dns) and internet domains. Using 169.254.1.1 instead of the
	// host's real upstream avoids NXDOMAIN for .test while retaining internet access.
	if err := podman.EnsureNetworkDNS("lerd", dns.ReadContainerDNS()); err != nil {
		fmt.Printf("  WARN: network DNS: %v\n", err)
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
	if lerdSystemd.IsServiceEnabled("lerd-tray") {
		if err := lerdSystemd.RestartService("lerd-tray"); err != nil {
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

// registeredStripeUnits returns unit names for all lerd-stripe-* service files
// present in the systemd user dir (i.e. started via `lerd stripe:listen`).
func registeredStripeUnits() []string {
	entries, _ := filepath.Glob(filepath.Join(config.SystemdUserDir(), "lerd-stripe-*.service"))
	units := make([]string, 0, len(entries))
	for _, e := range entries {
		units = append(units, strings.TrimSuffix(filepath.Base(e), ".service"))
	}
	return units
}

// registeredQueueUnits returns unit names for all lerd-queue-* service files
// present in the systemd user dir (i.e. started via `lerd queue:start`).
func registeredQueueUnits() []string {
	entries, _ := filepath.Glob(filepath.Join(config.SystemdUserDir(), "lerd-queue-*.service"))
	units := make([]string, 0, len(entries))
	for _, e := range entries {
		units = append(units, strings.TrimSuffix(filepath.Base(e), ".service"))
	}
	return units
}

// registeredScheduleUnits returns unit names for all lerd-schedule-* service files.
func registeredScheduleUnits() []string {
	entries, _ := filepath.Glob(filepath.Join(config.SystemdUserDir(), "lerd-schedule-*.service"))
	units := make([]string, 0, len(entries))
	for _, e := range entries {
		units = append(units, strings.TrimSuffix(filepath.Base(e), ".service"))
	}
	return units
}

// registeredReverbUnits returns unit names for all lerd-reverb-* service files.
func registeredReverbUnits() []string {
	entries, _ := filepath.Glob(filepath.Join(config.SystemdUserDir(), "lerd-reverb-*.service"))
	units := make([]string, 0, len(entries))
	for _, e := range entries {
		units = append(units, strings.TrimSuffix(filepath.Base(e), ".service"))
	}
	return units
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

	jobs := make([]BuildJob, len(units))
	for i, u := range units {
		unit := u
		label := strings.TrimPrefix(unit, "lerd-")
		jobs[i] = BuildJob{
			Label: label,
			Run:   func(w io.Writer) error { return podman.StopUnit(unit) },
		}
	}
	RunParallel(jobs) //nolint:errcheck
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
		if err := podman.StopUnit(unit); err != nil {
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
