package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	lerdSystemd "github.com/geodro/lerd/internal/systemd"
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
				return fmt.Errorf("laravel/horizon is not installed in this project")
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
			if site, err := config.FindSite(siteName); err == nil {
				SyncLerdYAMLWorkers(site)
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
				return fmt.Errorf("laravel/horizon is not installed in this project")
			}
			siteName, err := queueSiteName(cwd)
			if err != nil {
				return err
			}
			if err := HorizonStopForSite(siteName); err != nil {
				return err
			}
			if site, err := config.FindSite(siteName); err == nil {
				SyncLerdYAMLWorkers(site)
			}
			return nil
		},
	}
}

// HorizonStartForSite starts Laravel Horizon for the named site as a systemd service.
// If a queue worker is running for the same site it is stopped first, since Horizon
// manages queues and the two must not run simultaneously.
func HorizonStartForSite(siteName, sitePath, phpVersion string) error {
	if status, _ := podman.UnitStatus("lerd-queue-" + siteName); status == "active" {
		if err := QueueStopForSite(siteName); err != nil {
			fmt.Printf("[WARN] stopping queue worker before horizon: %v\n", err)
		}
	}
	versionShort := strings.ReplaceAll(phpVersion, ".", "")
	fpmUnit := "lerd-php" + versionShort + "-fpm"
	container := "lerd-php" + versionShort + "-fpm"
	unitName := "lerd-horizon-" + siteName

	unit := fmt.Sprintf(`[Unit]
Description=Lerd Horizon (%s)
After=network.target %s.service
BindsTo=%s.service

[Service]
Type=simple
Restart=always
RestartSec=5
ExecStart=podman exec -w %s %s php artisan horizon

[Install]
WantedBy=default.target
`, siteName, fpmUnit, fpmUnit, sitePath, container)

	changed, err := lerdSystemd.WriteServiceIfChanged(unitName, unit)
	if err != nil {
		return fmt.Errorf("writing service unit: %w", err)
	}
	if changed {
		if err := podman.DaemonReload(); err != nil {
			return fmt.Errorf("daemon-reload: %w", err)
		}
		if err := lerdSystemd.EnableService(unitName); err != nil {
			fmt.Printf("[WARN] enable: %v\n", err)
		}
	}
	if err := lerdSystemd.StartService(unitName); err != nil {
		return fmt.Errorf("starting horizon: %w", err)
	}
	fmt.Printf("Horizon started for %s\n", siteName)
	fmt.Printf("  Logs: journalctl --user -u %s -f\n", unitName)
	return nil
}

// HorizonStopForSite stops and removes the Horizon unit for the named site.
func HorizonStopForSite(siteName string) error {
	unitName := "lerd-horizon-" + siteName
	unitFile := filepath.Join(config.SystemdUserDir(), unitName+".service")

	_ = lerdSystemd.DisableService(unitName)
	podman.StopUnit(unitName) //nolint:errcheck

	if err := os.Remove(unitFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing unit file: %w", err)
	}
	if err := podman.DaemonReload(); err != nil {
		fmt.Printf("[WARN] daemon-reload: %v\n", err)
	}
	fmt.Printf("Horizon stopped for %s\n", siteName)
	return nil
}

// SiteHasHorizon returns true if composer.json lists laravel/horizon as a dependency.
func SiteHasHorizon(sitePath string) bool {
	data, err := os.ReadFile(filepath.Join(sitePath, "composer.json"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), `"laravel/horizon"`)
}
