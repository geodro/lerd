package cli

import (
	"fmt"
	"github.com/geodro/lerd/internal/podman"
	"os"
	"strings"

	"github.com/geodro/lerd/internal/config"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/services"
	"github.com/spf13/cobra"
)

// NewScheduleCmd returns the schedule parent command with start/stop subcommands.
func NewScheduleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schedule",
		Short: "Manage the Laravel task scheduler for the current site",
	}
	cmd.AddCommand(newScheduleStartCmd("start"))
	cmd.AddCommand(newScheduleStopCmd("stop"))
	return cmd
}

// NewScheduleStartCmd returns the standalone schedule:start command.
func NewScheduleStartCmd() *cobra.Command { return newScheduleStartCmd("schedule:start") }

// NewScheduleStopCmd returns the standalone schedule:stop command.
func NewScheduleStopCmd() *cobra.Command { return newScheduleStopCmd("schedule:stop") }

func newScheduleStartCmd(use string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: "Start the Laravel task scheduler for the current site as a systemd service",
		RunE: func(_ *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			if err := requireFrameworkWorker(cwd, "schedule"); err != nil {
				return err
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
			return ScheduleStartForSite(siteName, cwd, phpVersion)
		},
	}
}

func newScheduleStopCmd(use string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: "Stop the Laravel task scheduler for the current site",
		RunE: func(_ *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			if err := requireFrameworkWorker(cwd, "schedule"); err != nil {
				return err
			}
			siteName, err := queueSiteName(cwd)
			if err != nil {
				return err
			}
			return ScheduleStopForSite(siteName)
		},
	}
}

// ScheduleStartForSite starts the Laravel task scheduler for the named site.
func ScheduleStartForSite(siteName, sitePath, phpVersion string) error {
	versionShort := strings.ReplaceAll(phpVersion, ".", "")
	fpmUnit := "lerd-php" + versionShort + "-fpm"
	container := "lerd-php" + versionShort + "-fpm"
	unitName := "lerd-schedule-" + siteName

	unit := fmt.Sprintf(`[Unit]
Description=Lerd Scheduler (%s)
After=network.target %s.service
BindsTo=%s.service

[Service]
Type=simple
Restart=always
RestartSec=5
ExecStart=%s exec -w %s %s php artisan schedule:work

[Install]
WantedBy=default.target
`, siteName, fpmUnit, fpmUnit, podman.PodmanBin(), sitePath, container)

	changed, err := services.Mgr.WriteServiceUnitIfChanged(unitName, unit)
	if err != nil {
		return fmt.Errorf("writing service unit: %w", err)
	}
	if changed {
		if err := services.Mgr.DaemonReload(); err != nil {
			return fmt.Errorf("daemon-reload: %w", err)
		}
		if err := services.Mgr.Enable(unitName); err != nil {
			fmt.Printf("[WARN] enable: %v\n", err)
		}
	}
	waitForFPMContainer(container)
	if running, _ := podman.ContainerRunning(container); !running {
		return fmt.Errorf("%s container is not running — run `lerd start` first", container)
	}
	// Kill any leftover in-container worker before starting so we never have duplicates.
	killArtisanInContainer(container, "php artisan schedule:")
	if err := services.Mgr.Start(unitName); err != nil {
		return fmt.Errorf("starting scheduler: %w", err)
	}
	fmt.Printf("Scheduler started for %s\n", siteName)
	fmt.Printf("  Logs: journalctl --user -u %s -f\n", unitName)
	return nil
}

// ScheduleStopForSite stops and removes the scheduler unit for the named site.
func ScheduleStopForSite(siteName string) error {
	unitName := "lerd-schedule-" + siteName

	_ = services.Mgr.Disable(unitName)
	services.Mgr.Stop(unitName) //nolint:errcheck

	// Kill any lingering in-container schedule process (macOS launchd only kills
	// the host-side podman exec; the PHP process inside the container survives).
	killArtisanInContainer(fpmContainerForSite(siteName), "php artisan schedule:")

	if err := services.Mgr.RemoveServiceUnit(unitName); err != nil {
		return fmt.Errorf("removing unit file: %w", err)
	}
	if err := services.Mgr.DaemonReload(); err != nil {
		fmt.Printf("[WARN] daemon-reload: %v\n", err)
	}
	fmt.Printf("Scheduler stopped for %s\n", siteName)
	return nil
}
