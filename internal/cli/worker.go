package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
	"github.com/spf13/cobra"
)

// waitForFPMContainer blocks until the named FPM container is running and
// accepting exec sessions, or until the timeout is reached.
// This prevents "container state improper" errors when launchd starts worker
// services in parallel with the FPM container on boot/machine restart.
func waitForFPMContainer(container string) {
	for range 30 {
		if running, _ := podman.ContainerRunning(container); running {
			// Container is running — verify exec works before returning.
			if err := podman.RunSilent("exec", container, "true"); err == nil {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// NewWorkerCmd returns the worker parent command with start/stop/list subcommands.
func NewWorkerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Manage framework-defined workers for the current site",
	}
	cmd.AddCommand(newWorkerStartCmd())
	cmd.AddCommand(newWorkerStopCmd())
	cmd.AddCommand(newWorkerListCmd())
	return cmd
}

func newWorkerStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start <name>",
		Short: "Start a framework worker as a systemd service",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			workerName := args[0]
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			site, fw, phpVersion, err := resolveSiteAndFramework(cwd)
			if err != nil {
				return err
			}
			worker, ok := fw.Workers[workerName]
			if !ok {
				return fmt.Errorf("framework %q has no worker named %q\nRun 'lerd worker list' to see available workers", fw.Label, workerName)
			}
			return WorkerStartForSite(site.Name, cwd, phpVersion, workerName, worker)
		},
	}
}

func newWorkerStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <name>",
		Short: "Stop a framework worker",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			workerName := args[0]
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			site, fw, _, err := resolveSiteAndFramework(cwd)
			if err != nil {
				return err
			}
			if _, ok := fw.Workers[workerName]; !ok {
				return fmt.Errorf("framework %q has no worker named %q\nRun 'lerd worker list' to see available workers", fw.Label, workerName)
			}
			return WorkerStopForSite(site.Name, workerName)
		},
	}
}

func newWorkerListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List workers defined for the current site's framework",
		RunE: func(_ *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			_, fw, _, err := resolveSiteAndFramework(cwd)
			if err != nil {
				return err
			}
			if len(fw.Workers) == 0 {
				fmt.Printf("Framework %q has no workers defined.\n", fw.Label)
				return nil
			}
			names := make([]string, 0, len(fw.Workers))
			for n := range fw.Workers {
				names = append(names, n)
			}
			sort.Strings(names)
			fmt.Printf("Workers for %s:\n", fw.Label)
			for _, name := range names {
				w := fw.Workers[name]
				label := w.Label
				if label == "" {
					label = name
				}
				fmt.Printf("  %-15s %s\n", name, label)
				fmt.Printf("  %-15s command: %s\n", "", w.Command)
			}
			return nil
		},
	}
}

// resolveSiteAndFramework finds the registered site and its framework for cwd.
// Falls back to framework detection if the site has no Framework set.
func resolveSiteAndFramework(cwd string) (*config.Site, *config.Framework, string, error) {
	site, err := config.FindSiteByPath(cwd)
	if err != nil {
		return nil, nil, "", fmt.Errorf("not a registered site — run 'lerd link' first")
	}

	fwName := site.Framework
	if fwName == "" {
		if detected, ok := config.DetectFramework(cwd); ok {
			fwName = detected
		}
	}

	fw, ok := config.GetFramework(fwName)
	if !ok {
		return nil, nil, "", fmt.Errorf("site %q has no framework assigned — run 'lerd link' or 'lerd framework add'", site.Name)
	}

	phpVersion := site.PHPVersion
	if phpVersion == "" {
		phpVersion, err = phpDet.DetectVersion(cwd)
		if err != nil {
			cfg, _ := config.LoadGlobal()
			phpVersion = cfg.PHP.DefaultVersion
		}
	}

	return site, fw, phpVersion, nil
}

// requireFrameworkWorker returns an error if the site's framework doesn't define the named worker.
func requireFrameworkWorker(cwd, workerName string) error {
	_, fw, _, err := resolveSiteAndFramework(cwd)
	if err != nil {
		return err
	}
	if fw.Workers == nil {
		return fmt.Errorf("framework %q has no workers defined", fw.Label)
	}
	if _, ok := fw.Workers[workerName]; !ok {
		return fmt.Errorf("framework %q has no worker named %q\nRun 'lerd worker list' to see available workers", fw.Label, workerName)
	}
	return nil
}

// WorkerStartForSite writes a systemd unit for the given framework worker and starts it.
// The unit name is lerd-{workerName}-{siteName}.
func WorkerStartForSite(siteName, sitePath, phpVersion, workerName string, w config.FrameworkWorker) error {
	versionShort := strings.ReplaceAll(phpVersion, ".", "")
	fpmUnit := "lerd-php" + versionShort + "-fpm"
	container := "lerd-php" + versionShort + "-fpm"
	unitName := "lerd-" + workerName + "-" + siteName

	restart := w.Restart
	if restart == "" {
		restart = "always"
	}
	label := w.Label
	if label == "" {
		label = workerName
	}

	unit := fmt.Sprintf(`[Unit]
Description=Lerd %s (%s)
After=network.target %s.service
BindsTo=%s.service

[Service]
Type=simple
Restart=%s
RestartSec=5
ExecStart=%s exec -w %s %s %s

[Install]
WantedBy=default.target
`, label, siteName, fpmUnit, fpmUnit, restart, podman.PodmanBin(), sitePath, container, w.Command)

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
	if err := services.Mgr.Start(unitName); err != nil {
		return fmt.Errorf("starting %s worker: %w", workerName, err)
	}

	fmt.Printf("%s started for %s\n", label, siteName)
	fmt.Printf("  Logs: journalctl --user -u %s -f\n", unitName)
	return nil
}

// WorkerStopForSite stops and removes the named worker unit for the given site.
func WorkerStopForSite(siteName, workerName string) error {
	unitName := "lerd-" + workerName + "-" + siteName

	_ = services.Mgr.Disable(unitName)
	services.Mgr.Stop(unitName) //nolint:errcheck

	if err := services.Mgr.RemoveServiceUnit(unitName); err != nil {
		return fmt.Errorf("removing unit file: %w", err)
	}
	if err := services.Mgr.DaemonReload(); err != nil {
		fmt.Printf("[WARN] daemon-reload: %v\n", err)
	}

	label := workerName
	fmt.Printf("%s stopped for %s\n", label, siteName)
	return nil
}
