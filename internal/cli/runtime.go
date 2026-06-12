package cli

import (
	"fmt"
	"os"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/nginx"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/siteops"
	"github.com/spf13/cobra"
)

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
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	site, err := config.FindSiteByPath(cwd)
	if err != nil {
		return fmt.Errorf("not a registered site — run 'lerd link' first")
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

	_ = podman.StopUnit(podman.FrankenPHPContainerName(site.Name))
	_ = podman.RemoveFrankenPHPQuadlet(site.Name)
	_ = podman.DaemonReloadFn()

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
