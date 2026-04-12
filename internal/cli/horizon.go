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
	return cmd
}

// NewHorizonStartCmd returns the standalone horizon:start command.
func NewHorizonStartCmd() *cobra.Command { return newHorizonStartCmd("horizon:start") }

// NewHorizonStopCmd returns the standalone horizon:stop command.
func NewHorizonStopCmd() *cobra.Command { return newHorizonStopCmd("horizon:stop") }

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
	fw, ok := config.GetFramework(siteFrameworkName(siteName))
	if !ok {
		return fmt.Errorf("no framework found for site %q", siteName)
	}
	worker, ok := fw.Workers["horizon"]
	if !ok {
		return fmt.Errorf("framework %q has no worker named \"horizon\"", fw.Label)
	}
	return WorkerStartForSite(siteName, sitePath, phpVersion, "horizon", worker)
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
	return WorkerStopForSite(siteName, "horizon")
}

// SiteHasHorizon returns true if composer.json lists laravel/horizon as a dependency.
func SiteHasHorizon(sitePath string) bool {
	data, err := os.ReadFile(filepath.Join(sitePath, "composer.json"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), `"laravel/horizon"`)
}
