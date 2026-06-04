package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/spf13/cobra"
)

// NewHorizonCmd returns the horizon parent command with start/stop subcommands.
func NewHorizonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "horizon",
		Short: "Manage Laravel Horizon for the current site",
	}
	cmd.AddCommand(newHorizonStartCmd("start"))
	cmd.AddCommand(newHorizonStopCmd("stop"))
	cmd.AddCommand(newHorizonReloadCmd("reload"))
	return cmd
}

// NewHorizonStartCmd returns the standalone horizon:start command.
func NewHorizonStartCmd() *cobra.Command { return newHorizonStartCmd("horizon:start") }

// NewHorizonStopCmd returns the standalone horizon:stop command.
func NewHorizonStopCmd() *cobra.Command { return newHorizonStopCmd("horizon:stop") }

// NewHorizonReloadCmd returns the standalone horizon:reload command.
func NewHorizonReloadCmd() *cobra.Command { return newHorizonReloadCmd("horizon:reload") }

// newHorizonReloadCmd toggles auto-reload mode (`horizon:listen`) on or off for
// the current site. With no argument it prints the current state. When toggled
// it persists the per-project preference and, if a Horizon worker is running,
// restarts it so the new command takes effect immediately.
func newHorizonReloadCmd(use string) *cobra.Command {
	return &cobra.Command{
		Use:   use + " [on|off]",
		Short: "Toggle Horizon auto-reload on file changes (horizon:listen) for the current site",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			if !SiteHasHorizon(cwd) {
				return fmt.Errorf("laravel/horizon is not installed in this project\nInstall it with: composer require laravel/horizon\nSee https://laravel.com/docs/13.x/horizon")
			}

			if len(args) == 0 {
				state := "off"
				if config.ProjectReloadsWorker(cwd, "horizon") {
					state = "on"
				}
				fmt.Printf("Horizon auto-reload (horizon:listen): %s\n", state)
				return nil
			}

			var enable bool
			switch strings.ToLower(args[0]) {
			case "on", "true", "1", "enable", "enabled":
				enable = true
			case "off", "false", "0", "disable", "disabled":
				enable = false
			default:
				return fmt.Errorf("expected 'on' or 'off', got %q", args[0])
			}

			siteName, err := queueSiteName(cwd)
			if err != nil {
				return err
			}
			phpVersion, err := phpDet.DetectVersion(cwd)
			if err != nil {
				cfg, _ := config.LoadGlobal()
				phpVersion = cfg.PHP.DefaultVersion
			}
			if err := ApplyHorizonReload(siteName, cwd, phpVersion, enable); err != nil {
				return err
			}

			if enable {
				fmt.Println("Horizon auto-reload enabled, Horizon will restart workers on file changes.")
			} else {
				fmt.Println("Horizon auto-reload disabled, Horizon runs in standard mode.")
			}
			return nil
		},
	}
}

// ApplyHorizonReload persists the per-project Horizon auto-reload preference and,
// when a Horizon worker is currently running for the site, restarts it so the
// new command takes effect immediately. The restart reuses HorizonStartForSite,
// which resolves the command (standard or horizon:listen) from the freshly
// persisted preference.
func ApplyHorizonReload(siteName, sitePath, phpVersion string, enabled bool) error {
	// Refuse to enable when the watcher prerequisite is missing rather than
	// persisting a preference the worker can't honour. Without this the toggle
	// would read "on" while resolveWorkerCommand quietly ran the standard
	// command, so the displayed state would diverge from reality.
	if enabled && !projectHasChokidar(sitePath) {
		return fmt.Errorf("Horizon auto-reload needs the chokidar npm package, which is not installed in this project\nInstall it with: npm install -D chokidar\n(Vite 8 no longer ships it, so a plain npm install is not enough)")
	}
	if err := config.SetProjectWorkerReload(sitePath, "horizon", enabled); err != nil {
		return err
	}
	if !horizonRunningForSite(siteName) {
		return nil
	}
	if err := HorizonStopForSite(siteName); err != nil {
		return fmt.Errorf("stop horizon: %w", err)
	}
	if err := HorizonStartForSite(siteName, sitePath, phpVersion); err != nil {
		return fmt.Errorf("restart horizon: %w", err)
	}
	return nil
}

// horizonRunningForSite reports whether the named site currently has a running
// Horizon worker.
func horizonRunningForSite(siteName string) bool {
	site, err := config.FindSite(siteName)
	if err != nil || site == nil {
		return false
	}
	for _, n := range CollectRunningWorkerNames(site) {
		if n == "horizon" {
			return true
		}
	}
	return false
}

func newHorizonStartCmd(use string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: "Start Laravel Horizon for the current site as a systemd service",
		RunE: func(_ *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			if !SiteHasHorizon(cwd) {
				return fmt.Errorf("laravel/horizon is not installed in this project\nInstall it with: composer require laravel/horizon\nSee https://laravel.com/docs/13.x/horizon")
			}
			siteName, err := queueSiteName(cwd)
			if err != nil {
				return err
			}
			phpVersion, err := phpDet.DetectVersion(cwd)
			if err != nil {
				cfg, _ := config.LoadGlobal()
				phpVersion = cfg.PHP.DefaultVersion
			}
			if err := HorizonStartForSite(siteName, cwd, phpVersion); err != nil {
				return err
			}
			if site, err := config.FindSite(siteName); err == nil && !site.Paused {
				_ = config.SetProjectWorkers(site.Path, CollectRunningWorkerNames(site))
			}
			return nil
		},
	}
}

func newHorizonStopCmd(use string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: "Stop Laravel Horizon for the current site",
		RunE: func(_ *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			if !SiteHasHorizon(cwd) {
				return fmt.Errorf("laravel/horizon is not installed in this project\nInstall it with: composer require laravel/horizon\nSee https://laravel.com/docs/13.x/horizon")
			}
			siteName, err := queueSiteName(cwd)
			if err != nil {
				return err
			}
			if err := HorizonStopForSite(siteName); err != nil {
				return err
			}
			if site, err := config.FindSite(siteName); err == nil && !site.Paused {
				_ = config.SetProjectWorkers(site.Path, CollectRunningWorkerNames(site))
			}
			return nil
		},
	}
}

// HorizonStartForSite starts Horizon for the named site. Conflicting workers
// (defined via ConflictsWith in the framework definition) are stopped first.
func HorizonStartForSite(siteName, sitePath, phpVersion string) error {
	fw, ok := config.GetFrameworkForDir(siteFrameworkName(siteName), sitePath)
	if !ok {
		return fmt.Errorf("no framework found for site %q", siteName)
	}
	worker, ok := fw.Workers["horizon"]
	if !ok {
		return fmt.Errorf("framework %q has no worker named \"horizon\"", fw.Label)
	}
	return WorkerStartForSite(siteName, sitePath, phpVersion, "horizon", worker, true)
}

// buildHorizonUnit renders the Horizon systemd unit body. Horizon always
// uses Redis so lerd-redis is in After=/Wants= alongside the FPM container.
func buildHorizonUnit(siteName, sitePath, phpVersion string) string {
	versionShort := strings.ReplaceAll(phpVersion, ".", "")
	fpmUnit := "lerd-php" + versionShort + "-fpm"
	container := "lerd-php" + versionShort + "-fpm"

	return fmt.Sprintf(`[Unit]
Description=Lerd Horizon (%s)
After=network.target %s.service lerd-redis.service
Wants=%s.service lerd-redis.service
BindsTo=%s.service

[Service]
Type=simple
Restart=always
RestartSec=5
ExecStart=%s exec -w %s %s php artisan horizon

[Install]
WantedBy=default.target
`, siteName, fpmUnit, fpmUnit, fpmUnit, podman.PodmanBin(), sitePath, container)
}

// HorizonStopForSite stops and removes the Horizon unit for the named site.
func HorizonStopForSite(siteName string) error {
	return WorkerStopForSite(siteName, "", "horizon")
}

// SiteHasHorizon returns true if composer.json lists laravel/horizon as a dependency.
func SiteHasHorizon(sitePath string) bool {
	data, err := os.ReadFile(filepath.Join(sitePath, "composer.json"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), `"laravel/horizon"`)
}
