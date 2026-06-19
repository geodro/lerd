package cli

import (
	"fmt"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/nginx"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/siteops"
	"github.com/spf13/cobra"
)

// Wire the siteops demote helper to the cli worker lifecycle so the UI, MCP,
// and install-refresh paths recreate workers when a FrankenPHP site falls back
// to FPM, the same way switchToFPM does for `lerd runtime fpm`.
func init() {
	siteops.StopRuntimeWorkers = func(site *config.Site) []string {
		running := collectRunningWorkers(site)
		for _, w := range running {
			WorkerStopForSite(site.Name, site.Path, w) //nolint:errcheck
		}
		return running
	}
	siteops.RecreateFPMWorkers = func(site *config.Site, workers []string) {
		startWorkersForSite(site, workers, site.PHPVersion)
	}
}

// NewRuntimeCmd returns the `lerd runtime` parent command.
func NewRuntimeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runtime [fpm|frankenphp]",
		Short: "Switch the PHP runtime for the current site (fpm or frankenphp)",
		Long: `Switch the PHP runtime for the current site. Writes to .lerd.yaml so the
choice is committed with the project.

  lerd runtime                     # print the current runtime
  lerd runtime frankenphp          # enable FrankenPHP (non-worker)
  lerd runtime frankenphp --worker # enable FrankenPHP worker mode
  lerd runtime fpm                 # back to shared PHP-FPM (clears .lerd.yaml)`,
		Args: cobra.MaximumNArgs(1),
		RunE: runRuntime,
	}
	cmd.Flags().Bool("worker", false, "Enable FrankenPHP worker mode")
	cmd.Flags().Bool("no-worker", false, "Disable FrankenPHP worker mode")
	return cmd
}

func runRuntime(cmd *cobra.Command, args []string) error {
	site, err := ensureSiteForCwd()
	if err != nil {
		return err
	}
	if site.IsCustomContainer() {
		return fmt.Errorf("site uses a custom Containerfile; the runtime is defined by your Containerfile.lerd")
	}
	if site.IsHostProxy() {
		return fmt.Errorf("site is a host-proxy site; it runs your dev command on the host, not a PHP runtime")
	}

	if len(args) == 0 {
		fmt.Printf("Runtime: %s\n", runtimeLabel(site))
		return nil
	}

	target := args[0]
	worker, _ := cmd.Flags().GetBool("worker")
	noWorker, _ := cmd.Flags().GetBool("no-worker")

	switch target {
	case "fpm":
		if !site.IsFrankenPHP() {
			fmt.Println("Already on FPM runtime.")
			return nil
		}
		return switchToFPM(site)
	case "frankenphp":
		// dunglas/frankenphp only publishes images for PHP >= 8.2; without this
		// guard the build would normalize the version up (e.g. 8.1 -> 8.5) and
		// silently run a different PHP than the site reports, with its ini files
		// still mounted from the old version's path.
		if !config.IsFrankenPHPVersion(site.PHPVersion) {
			return fmt.Errorf("FrankenPHP requires PHP %s or newer; this site is on PHP %s — bump it first with 'lerd isolate %s' (or higher)",
				config.FrankenPHPMinVersion, site.PHPVersion, config.FrankenPHPMinVersion)
		}
		fw, ok := config.GetFrameworkForDir(site.Framework, site.Path)
		if !ok {
			return fmt.Errorf("site has no framework assigned — FrankenPHP needs a framework entrypoint or the generic public/ fallback")
		}
		if fw.FrankenPHP == nil && fw.PublicDir == "" {
			fmt.Println("[INFO] framework has no FrankenPHP adapter, falling back to the generic `frankenphp php-server` entrypoint")
		}
		wantWorker := site.RuntimeWorker
		if worker {
			wantWorker = true
		}
		if noWorker {
			wantWorker = false
		}
		return switchToFrankenPHP(site, wantWorker)
	default:
		return fmt.Errorf("unknown runtime %q — use 'fpm' or 'frankenphp'", target)
	}
}

// removeFrankenPHPContainer stops a site's per-site FrankenPHP container,
// removes its quadlet, and reloads systemd so the generated unit disappears.
// Shared by switchToFPM and link's stale-quadlet reconcile.
func removeFrankenPHPContainer(siteName string) {
	podman.RemoveFrankenPHPContainer(siteName)
}

// reconcileStaleFrankenPHP removes a leftover per-site FrankenPHP quadlet when a
// (re)linked site is no longer FrankenPHP. That quadlet is WantedBy=default.target
// with Restart=always, so podman's generator keeps auto-starting an orphan that
// lerd start/stop never enumerate.
func reconcileStaleFrankenPHP(site config.Site) {
	if site.IsFrankenPHP() || !podman.QuadletInstalled(podman.FrankenPHPContainerName(site.Name)) {
		return
	}
	removeFrankenPHPContainer(site.Name)
}

// removeCustomFPMContainer stops a site's per-site custom-FPM container, removes
// its quadlet, and reloads systemd so the generated unit disappears. Mirrors
// removeFrankenPHPContainer for the fpm-custom runtime.
func removeCustomFPMContainer(siteName string) {
	_ = podman.StopUnit(podman.CustomFPMContainerName(siteName))
	_ = podman.RemoveCustomFPMQuadlet(siteName)
	_ = podman.DaemonReloadFn()
}

// reconcileStaleCustomFPM removes a leftover per-site custom-FPM quadlet when a
// (re)linked site is no longer fpm-custom (e.g. the Containerfile was removed, or
// a port was added so it became a reverse-proxied custom container). Like the
// FrankenPHP one, that quadlet is WantedBy=default.target with Restart=always, so
// podman's generator keeps auto-starting an orphan that lerd start/stop miss.
func reconcileStaleCustomFPM(site config.Site) {
	if site.IsCustomFPM() || !podman.QuadletInstalled(podman.CustomFPMContainerName(site.Name)) {
		return
	}
	removeCustomFPMContainer(site.Name)
}

// reconcileStaleRuntimeQuadlets clears any per-site FrankenPHP or custom-FPM
// quadlet a site no longer uses, so changing a project's runtime (or dropping its
// Containerfile) on re-link never strands an auto-starting orphan container.
func reconcileStaleRuntimeQuadlets(site config.Site) {
	reconcileStaleFrankenPHP(site)
	reconcileStaleCustomFPM(site)
}

func runtimeLabel(site *config.Site) string {
	if site.IsFrankenPHP() {
		if site.RuntimeWorker {
			return "frankenphp (worker mode)"
		}
		return "frankenphp"
	}
	return "fpm"
}

func switchToFPM(site *config.Site) error {
	// Capture the running workers before teardown: they exec into (and BindsTo)
	// the FrankenPHP container, so removing it would both stop them and lose
	// the list we need to re-establish on the shared FPM unit.
	running := collectRunningWorkers(site)
	for _, w := range running {
		WorkerStopForSite(site.Name, site.Path, w) //nolint:errcheck
	}

	removeFrankenPHPContainer(site.Name)

	site.Runtime = ""
	site.RuntimeWorker = false
	if err := config.AddSite(*site); err != nil {
		return fmt.Errorf("updating site: %w", err)
	}
	_ = config.SetProjectRuntime(site.Path, "", false)

	if site.Secured {
		if err := nginx.GenerateSSLVhost(*site, site.PHPVersion); err != nil {
			return fmt.Errorf("regenerating SSL vhost: %w", err)
		}
	} else {
		if err := nginx.GenerateVhost(*site, site.PHPVersion); err != nil {
			return fmt.Errorf("regenerating vhost: %w", err)
		}
	}
	_ = nginx.Reload()

	// Recreate the workers so their units exec into the shared FPM container
	// instead of the per-site FrankenPHP one that no longer exists.
	startWorkersForSite(site, running, site.PHPVersion)

	fmt.Printf("Runtime: fpm (switched from FrankenPHP)\n")
	return nil
}

func switchToFrankenPHP(site *config.Site, worker bool) error {
	// Workers currently exec into the shared FPM container; stop them so they
	// can be recreated against the per-site FrankenPHP container below.
	running := collectRunningWorkers(site)
	for _, w := range running {
		WorkerStopForSite(site.Name, site.Path, w) //nolint:errcheck
	}

	site.Runtime = "frankenphp"
	site.RuntimeWorker = worker
	if err := config.AddSite(*site); err != nil {
		return fmt.Errorf("updating site: %w", err)
	}
	_ = config.SetProjectRuntime(site.Path, "frankenphp", worker)

	if err := siteops.FinishFrankenPHPLink(*site); err != nil {
		return err
	}

	// Re-point the site's workers at the new per-site FrankenPHP container.
	startWorkersForSite(site, running, site.PHPVersion)

	label := "frankenphp"
	if worker {
		label = "frankenphp (worker mode)"
	}
	fmt.Printf("Runtime: %s\n", label)
	return nil
}
