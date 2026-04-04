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
			if err := ScheduleStartForSite(siteName, cwd, phpVersion); err != nil {
				return err
			}
			if site, err := config.FindSite(siteName); err == nil {
				SyncLerdYAMLWorkers(site)
			}
			return nil
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
			if err := ScheduleStopForSite(siteName); err != nil {
				return err
			}
			if site, err := config.FindSite(siteName); err == nil {
				SyncLerdYAMLWorkers(site)
			}
			return nil
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
ExecStart=podman exec -w %s %s php artisan schedule:work

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
		return fmt.Errorf("starting scheduler: %w", err)
	}
	fmt.Printf("Scheduler started for %s\n", siteName)
	fmt.Printf("  Logs: journalctl --user -u %s -f\n", unitName)
	return nil
}

// ScheduleStopForSite stops and removes the scheduler unit for the named site.
func ScheduleStopForSite(siteName string) error {
	unitName := "lerd-schedule-" + siteName
	unitFile := filepath.Join(config.SystemdUserDir(), unitName+".service")

	_ = lerdSystemd.DisableService(unitName)
	podman.StopUnit(unitName) //nolint:errcheck

	if err := os.Remove(unitFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing unit file: %w", err)
	}
	if err := podman.DaemonReload(); err != nil {
		fmt.Printf("[WARN] daemon-reload: %v\n", err)
	}
	fmt.Printf("Scheduler stopped for %s\n", siteName)
	return nil
}
