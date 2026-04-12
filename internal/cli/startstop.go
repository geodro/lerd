package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dns"
	"github.com/geodro/lerd/internal/nginx"
	phpPkg "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
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
					cmd := podman.Cmd("build", "-t", "lerd-dnsmasq:local", "-")
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
					cmd := podman.Cmd("pull", label)
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
// The configured default PHP version is ALWAYS included so the `php`, `composer`,
// and `laravel new` shims have a working FPM container even on a fresh install
// with zero registered sites. Other installed versions are only started when
// at least one site references them; unused versions are left stopped.
func coreUnits() []string {
	units := []string{"lerd-dns", "lerd-nginx"}
	active := activePHPVersions()
	if cfg, err := config.LoadGlobal(); err == nil && cfg != nil && cfg.PHP.DefaultVersion != "" {
		active[cfg.PHP.DefaultVersion] = true
	}
	versions, _ := phpPkg.ListInstalled()
	for _, v := range versions {
		if !active[v] {
			continue
		}
		short := strings.ReplaceAll(v, ".", "")
		units = append(units, "lerd-php"+short+"-fpm")
	}
	return units
}

// installedServiceUnits returns service units that have a unit file installed
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

// allInstalledServiceUnits returns all service units that have a unit file
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

// portCheck pairs a host port with a human-readable label and container name.
type portCheck struct {
	port      string // host port number
	label     string // e.g. "nginx HTTP", "mysql"
	container string // lerd container name
}

// builtinExtraPorts lists secondary host ports for built-in services that are
// hardcoded in the quadlet files but not reflected in config.ServiceConfig.Port.
var builtinExtraPorts = map[string][]string{
	"rustfs":  {"9001"},
	"mailpit": {"8025"},
}

// hostPort extracts the host port from a port mapping string ("host:container").
// If no colon is present the whole string is returned.
func hostPort(mapping string) string {
	if i := strings.Index(mapping, ":"); i >= 0 {
		return mapping[:i]
	}
	return mapping
}

// collectPortChecks builds the list of ports to verify for the given units.
func collectPortChecks(units []string) []portCheck {
	unitSet := make(map[string]bool, len(units))
	for _, u := range units {
		unitSet[u] = true
	}

	var checks []portCheck

	// Nginx ports (configurable).
	if unitSet["lerd-nginx"] {
		cfg, err := config.LoadGlobal()
		httpPort := 80
		httpsPort := 443
		if err == nil {
			if cfg.Nginx.HTTPPort > 0 {
				httpPort = cfg.Nginx.HTTPPort
			}
			if cfg.Nginx.HTTPSPort > 0 {
				httpsPort = cfg.Nginx.HTTPSPort
			}
		}
		checks = append(checks,
			portCheck{strconv.Itoa(httpPort), "nginx HTTP", "lerd-nginx"},
			portCheck{strconv.Itoa(httpsPort), "nginx HTTPS", "lerd-nginx"},
		)
	}

	// DNS port.
	if unitSet["lerd-dns"] {
		checks = append(checks, portCheck{"5300", "dns", "lerd-dns"})
	}

	// Built-in services.
	cfg, _ := config.LoadGlobal()
	for _, svc := range knownServices {
		if !unitSet["lerd-"+svc] {
			continue
		}
		container := "lerd-" + svc
		if cfg != nil {
			if sc, ok := cfg.Services[svc]; ok && sc.Port > 0 {
				checks = append(checks, portCheck{strconv.Itoa(sc.Port), svc, container})
			}
			if sc, ok := cfg.Services[svc]; ok {
				for _, ep := range sc.ExtraPorts {
					checks = append(checks, portCheck{hostPort(ep), svc, container})
				}
			}
		}
		for _, ep := range builtinExtraPorts[svc] {
			checks = append(checks, portCheck{ep, svc, container})
		}
	}

	// Custom services.
	customs, _ := config.ListCustomServices()
	for _, svc := range customs {
		if !unitSet["lerd-"+svc.Name] {
			continue
		}
		container := "lerd-" + svc.Name
		for _, p := range svc.Ports {
			checks = append(checks, portCheck{hostPort(p), svc.Name, container})
		}
	}

	return checks
}

// checkPortConflicts warns about ports already in use by non-lerd processes.
func checkPortConflicts(units []string) {
	checks := collectPortChecks(units)
	if len(checks) == 0 {
		return
	}

	ss := portListOutput()
	if ss == "" {
		return
	}

	var conflicts []string
	for _, c := range checks {
		running, _ := podman.ContainerRunning(c.container)
		if running {
			continue
		}
		if portInUseIn(c.port, ss) {
			conflicts = append(conflicts,
				fmt.Sprintf("  WARN: port %s (%s) already in use — may fail to start (check: ss -tlnp sport = :%s)", c.port, c.label, c.port))
		}
	}
	if len(conflicts) > 0 {
		fmt.Println("Port conflicts detected:")
		for _, msg := range conflicts {
			fmt.Println(msg)
		}
		fmt.Println()
	}
}

func runStart(_ *cobra.Command, _ []string) error {
	// On macOS, ensure Podman Machine is up and migrate any stale plists.
	ensurePodmanMachineRunning()
	migrateExecWorkerPlists()

	// Ensure the lerd bridge network exists. On macOS the network is stored
	// inside the Podman Machine VM; it may be absent after a fresh machine
	// init or if it was pruned. All service containers use --network lerd so
	// this must succeed before any container is started.
	if err := podman.EnsureNetwork("lerd"); err != nil {
		fmt.Printf("  WARN: ensure lerd network: %v\n", err)
	}

	// Restore quadlets and worker units that may be missing after an
	// uninstall/reinstall cycle. Reads .lerd.yaml from each active site.
	restoreSiteInfrastructure()

	// Pre-flight port conflict check.
	units := append(coreUnits(), installedServiceUnits()...)
	checkPortConflicts(units)

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

	// Pre-flight: repair SSL vhosts with missing cert files so nginx can start.
	if repairs := nginx.RepairVhosts(); len(repairs) > 0 {
		for _, r := range repairs {
			switch r.Reason {
			case "missing-cert":
				fmt.Printf("  WARN: missing TLS certificate for %s — switched to HTTP\n", r.Domain)
			case "orphan-ssl":
				fmt.Printf("  WARN: removed orphan SSL vhost for %s\n", r.Domain)
			}
		}
	}

	// Phase 1: start all infrastructure (containers, FPM, UI, watcher) before workers.
	// Workers exec into FPM containers, so the containers must be up first.
	serviceUnits := append(coreUnits(), installedServiceUnits()...)
	serviceUnits = append(serviceUnits, "lerd-ui", "lerd-watcher")

	// Phase 2: worker units that depend on running containers.
	workerUnits := append(registeredQueueUnits(), registeredStripeUnits()...)
	workerUnits = append(workerUnits, registeredScheduleUnits()...)
	workerUnits = append(workerUnits, registeredReverbUnits()...)
	// Also include non-standard framework workers (horizon, vite-dev, etc.)
	// declared in the site registry, so restored unit files get started here
	// rather than waiting for the next session.
	workerUnits = append(workerUnits, registeredFrameworkWorkerUnits()...)
	workerUnits = dedupeStrings(workerUnits)

	fmt.Println("Starting Lerd...")

	makeJobs := func(us []string) []BuildJob {
		jobs := make([]BuildJob, len(us))
		for i, u := range us {
			unit := u
			label := strings.TrimPrefix(unit, "lerd-")
			jobs[i] = BuildJob{
				Label: label,
				Run: func(w io.Writer) error {
					if unit == "lerd-dns" {
						return podman.RestartUnit(unit)
					}
					return podman.StartUnit(unit)
				},
			}
		}
		return jobs
	}

	RunParallel(makeJobs(serviceUnits)) //nolint:errcheck
	if len(workerUnits) > 0 {
		RunParallel(makeJobs(workerUnits)) //nolint:errcheck
	}

	// Regenerate the browser-testing hosts file now that nginx has its IP.
	// The file was written earlier with a possibly stale address; update it
	// so containers like Selenium resolve .test domains to the current
	// lerd-nginx container IP.
	if err := podman.WriteContainerHosts(); err != nil {
		fmt.Printf("  WARN: browser hosts file: %v\n", err)
	}

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

	autoStopUnusedFPMs()

	// Restart the tray applet, stopping any existing instance first.
	// Prefer the systemd service when enabled; otherwise launch directly.
	fmt.Print("  --> lerd-tray ... ")
	if services.Mgr.IsEnabled("lerd-tray") {
		// Use Start (bootout+bootstrap) instead of Restart (kickstart -k) to
		// avoid launchctl hanging while waiting for the tray process to die.
		killTray()
		if err := services.Mgr.Start("lerd-tray"); err != nil {
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

// startRestoredServices pulls images and starts service units that have a quadlet
// installed but are not yet running. Called from lerd install to bring back services
// (mysql, redis, etc.) that were restored from .lerd.yaml.
func startRestoredServices() {
	units := installedServiceUnits()
	if len(units) == 0 {
		return
	}

	// Pull missing images first.
	var pullJobs []BuildJob
	seen := map[string]bool{}
	for _, unit := range units {
		image := quadletImage(unit)
		if image == "" || seen[image] {
			continue
		}
		seen[image] = true
		if podman.RunSilent("image", "exists", image) == nil {
			continue
		}
		img := image
		pullJobs = append(pullJobs, BuildJob{
			Label: "Pulling " + img,
			Run: func(w io.Writer) error {
				cmd := podman.Cmd("pull", img)
				cmd.Stdout = w
				cmd.Stderr = w
				return cmd.Run()
			},
		})
	}
	if len(pullJobs) > 0 {
		RunParallel(pullJobs) //nolint:errcheck
	}

	// Start the services.
	var startJobs []BuildJob
	for _, u := range units {
		unit := u
		label := strings.TrimPrefix(unit, "lerd-")
		startJobs = append(startJobs, BuildJob{
			Label: label,
			Run:   func(_ io.Writer) error { return podman.StartUnit(unit) },
		})
	}
	RunParallel(startJobs) //nolint:errcheck

	// Workers exec into the FPM containers and depend on lerd-redis et al.
	// Start them after the service containers are up — same ordering as
	// runStart's phase 1 → phase 2 split. Without this, `lerd install` would
	// leave workers enabled-but-stopped after restoreSiteInfrastructure, since
	// restoreWorker only writes the unit file and defers Start to here.
	workerUnits := append(registeredQueueUnits(), registeredStripeUnits()...)
	workerUnits = append(workerUnits, registeredScheduleUnits()...)
	workerUnits = append(workerUnits, registeredReverbUnits()...)
	workerUnits = append(workerUnits, registeredFrameworkWorkerUnits()...)
	workerUnits = dedupeStrings(workerUnits)
	if len(workerUnits) == 0 {
		return
	}
	var workerJobs []BuildJob
	for _, u := range workerUnits {
		unit := u
		label := strings.TrimPrefix(unit, "lerd-")
		workerJobs = append(workerJobs, BuildJob{
			Label: label,
			Run:   func(_ io.Writer) error { return podman.StartUnit(unit) },
		})
	}
	RunParallel(workerJobs) //nolint:errcheck
}

// killTray kills any running lerd tray process (launched directly or as lerd-tray binary).
func killTray() {
	exec.Command("pkill", "-f", "lerd tray").Run()
	exec.Command("pkill", "-f", "lerd-tray").Run()
}

// registeredStripeUnits returns unit names for all lerd-stripe-* service files
// present in the systemd user dir (i.e. started via `lerd stripe:listen`).
// restoreSiteInfrastructure ensures FPM quadlets, service quadlets, and worker
// units exist for all registered (non-paused) sites. This repairs state after
// an uninstall/reinstall cycle where unit files were deleted but site configs
// (sites.yaml, .lerd.yaml) were preserved.
func restoreSiteInfrastructure() {
	reg, err := config.LoadSites()
	if err != nil {
		return
	}

	seenPHP := map[string]bool{}
	seenSvc := map[string]bool{}
	dirty := false

	// Backfill framework for all sites (including paused) that were linked
	// before detection was added.
	for i, s := range reg.Sites {
		if s.Ignored || s.Framework != "" {
			continue
		}
		if name, ok := config.DetectFrameworkForDir(s.Path); ok {
			reg.Sites[i].Framework = name
			dirty = true
		}
	}

	for _, s := range reg.Sites {
		if s.Paused || s.Ignored {
			continue
		}

		// Restore FPM quadlet for this site's PHP version.
		phpVer := s.PHPVersion
		if phpVer == "" {
			cfg, _ := config.LoadGlobal()
			phpVer = cfg.PHP.DefaultVersion
		}
		if phpVer != "" && !seenPHP[phpVer] {
			seenPHP[phpVer] = true
			ensureFPMQuadlet(phpVer) //nolint:errcheck
		}

		// Read .lerd.yaml for service and worker info.
		proj, _ := config.LoadProjectConfig(s.Path)
		if proj == nil {
			continue
		}

		// Resolve() returns the rendered CustomService for inline + preset
		// references (e.g. mariadb-11) and (nil, nil) for built-ins. Without
		// it, preset references slipped through to the built-in template path.
		for _, svc := range proj.Services {
			if seenSvc[svc.Name] {
				continue
			}
			seenSvc[svc.Name] = true
			cs, err := svc.Resolve()
			if err != nil {
				fmt.Printf("[WARN] resolving service %q for %s: %v\n", svc.Name, s.Name, err)
				continue
			}
			if cs != nil {
				ensureCustomServiceQuadlet(cs) //nolint:errcheck
			} else {
				ensureServiceQuadlet(svc.Name) //nolint:errcheck
			}
		}

		// Restore worker units from saved worker names. The platform helper
		// decides whether to start immediately (Linux) or just write the unit
		// file and let phase 2 of runStart launch it (macOS).
		for _, w := range proj.Workers {
			unitName := "lerd-" + w + "-" + s.Name
			if services.Mgr.IsEnabled(unitName) {
				continue
			}
			phpVersion := s.PHPVersion
			if phpVersion == "" {
				cfg, _ := config.LoadGlobal()
				phpVersion = cfg.PHP.DefaultVersion
			}
			if w == "stripe" {
				base := siteURL(s.Path)
				if base != "" {
					StripeStartForSite(s.Name, s.Path, base) //nolint:errcheck
				}
				continue
			}
			fwName := s.Framework
			if fw, ok := config.GetFrameworkForDir(fwName, s.Path); ok && fw.Workers != nil {
				if wDef, ok := fw.Workers[w]; ok {
					restoreWorker(s.Name, s.Path, phpVersion, w, wDef)
				}
			}
		}
	}
	if dirty {
		config.SaveSites(reg) //nolint:errcheck
	}

	// Restore unit files for standalone custom services (installed globally via
	// `lerd service add`) whose config exists in ~/.config/lerd/services/ but
	// whose unit file (plist on macOS, quadlet on Linux) is missing — e.g. after
	// a reinstall that wiped ~/Library/LaunchAgents or ~/.config/containers/systemd/.
	if customs, err := config.ListCustomServices(); err == nil {
		for _, svc := range customs {
			if !services.Mgr.ContainerUnitInstalled("lerd-" + svc.Name) {
				ensureCustomServiceQuadlet(svc) //nolint:errcheck
			}
		}
	}

	podman.DaemonReloadFn() //nolint:errcheck
}

func registeredStripeUnits() []string {
	return services.Mgr.ListServiceUnits("lerd-stripe-*")
}

// registeredQueueUnits returns unit names for all lerd-queue-* service units
// (i.e. started via `lerd queue:start`).
func registeredQueueUnits() []string {
	return services.Mgr.ListServiceUnits("lerd-queue-*")
}

// registeredScheduleUnits returns unit names for all lerd-schedule-* service units.
func registeredScheduleUnits() []string {
	return services.Mgr.ListServiceUnits("lerd-schedule-*")
}

// registeredReverbUnits returns unit names for all lerd-reverb-* service units.
func registeredReverbUnits() []string {
	return services.Mgr.ListServiceUnits("lerd-reverb-*")
}

// registeredFrameworkWorkerUnits returns lerd-{worker}-{site} unit names for
// every site/worker pair declared in the site registry. Used to make sure
// non-standard workers (horizon, vite-dev, etc.) get started in phase 2 of
// runStart, not just the queue/stripe/schedule/reverb glob.
func registeredFrameworkWorkerUnits() []string {
	reg, err := config.LoadSites()
	if err != nil || reg == nil {
		return nil
	}
	out := make([]string, 0)
	for _, s := range reg.Sites {
		proj, err := config.LoadProjectConfig(s.Path)
		if err != nil || proj == nil {
			continue
		}
		for _, w := range proj.Workers {
			if w == "stripe" {
				continue
			}
			out = append(out, "lerd-"+w+"-"+s.Name)
		}
	}
	return out
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
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

	// On macOS: stop all containers in one podman call before the parallel
	// per-unit jobs run. This avoids serialising N individual podman stop
	// requests through the Podman Machine socket (which can take 5s × N).
	batchStopContainers(units)

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
