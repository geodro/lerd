package cli

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/geodro/lerd/internal/config"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
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
		Short: "Start the Laravel task scheduler for the current site as a background service",
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
	unitName := "lerd-schedule-" + siteName

	if runtime.GOOS == "darwin" {
		image := "lerd-php" + versionShort + "-fpm:local"
		if !podman.ImageExists(image) {
			return fmt.Errorf("PHP %s image not found — run `lerd use %s` to build it", phpVersion, phpVersion)
		}
		_ = podman.EnsureUserIni(phpVersion)
		unit := fmt.Sprintf(`[Container]
ContainerName=%s
Image=%s
Network=lerd
Volume=%s:%s
Volume=%s:/usr/local/etc/php/conf.d/99-xdebug.ini
Volume=%s:/usr/local/etc/php/conf.d/99-user.ini
Volume=%s:/etc/hosts
WorkingDir=%s
Exec=php artisan schedule:work
`, unitName, image,
			sitePath, sitePath,
			config.PHPConfFile(phpVersion),
			config.PHPUserIniFile(phpVersion),
			config.ContainerHostsFile(),
			sitePath)
		if err := services.Mgr.WriteContainerUnit(unitName, unit); err != nil {
			return fmt.Errorf("writing container unit: %w", err)
		}
		if err := services.Mgr.Start(unitName); err != nil {
			return fmt.Errorf("starting scheduler: %w", err)
		}
		fmt.Printf("Scheduler started for %s\n", siteName)
		fmt.Printf("  Logs: podman logs -f %s\n", unitName)
		return nil
	}

	fpmUnit := "lerd-php" + versionShort + "-fpm"
	container := fpmUnit
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

	if err := services.Mgr.RemoveServiceUnit(unitName); err != nil {
		return fmt.Errorf("removing unit file: %w", err)
	}
	if err := services.Mgr.DaemonReload(); err != nil {
		fmt.Printf("[WARN] daemon-reload: %v\n", err)
	}
	fmt.Printf("Scheduler stopped for %s\n", siteName)
	return nil
}
