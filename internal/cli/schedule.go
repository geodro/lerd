package cli

import (
	"fmt"
	"os"

	"github.com/geodro/lerd/internal/config"
	phpDet "github.com/geodro/lerd/internal/php"
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
			if site, err := config.FindSite(siteName); err == nil && !site.Paused {
				_ = config.SetProjectWorkers(site.Path, CollectRunningWorkerNames(site))
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
			if site, err := config.FindSite(siteName); err == nil && !site.Paused {
				_ = config.SetProjectWorkers(site.Path, CollectRunningWorkerNames(site))
			}
			return nil
		},
	}
}

// ScheduleStartForSite starts the task scheduler for the named site using
// the "schedule" worker from the framework definition. The version-aware
// loader (GetFrameworkForDir) is used so per-version overrides — like
// Laravel 10's `schedule: minutely` cron entry — actually take effect
// instead of falling back to the version-less built-in defaults.
func ScheduleStartForSite(siteName, sitePath, phpVersion string) error {
	fw, ok := config.GetFrameworkForDir(siteFrameworkName(siteName), sitePath)
	if !ok {
		return fmt.Errorf("no framework found for site %q", siteName)
	}
	worker, ok := fw.Workers["schedule"]
	if !ok {
		return fmt.Errorf("framework %q has no worker named \"schedule\"", fw.Label)
	}
	return WorkerStartForSite(siteName, sitePath, phpVersion, "schedule", worker)
}

// ScheduleStopForSite stops and removes the scheduler unit for the named site.
func ScheduleStopForSite(siteName string) error {
	return WorkerStopForSite(siteName, "schedule")
}
