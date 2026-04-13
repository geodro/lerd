package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
	"github.com/spf13/cobra"
)

// NewWorkerCmd returns the worker parent command with start/stop/list subcommands.
func NewWorkerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Manage framework-defined workers for the current site",
	}
	cmd.AddCommand(newWorkerStartCmd())
	cmd.AddCommand(newWorkerStopCmd())
	cmd.AddCommand(newWorkerListCmd())
	cmd.AddCommand(newWorkerAddCmd())
	cmd.AddCommand(newWorkerRemoveCmd())
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
			if worker.Check != nil && !config.MatchesRule(cwd, *worker.Check) {
				return fmt.Errorf("worker %q requires a dependency that is not installed\nCheck the framework definition for required packages", workerName)
			}
			if err := WorkerStartForSite(site.Name, cwd, phpVersion, workerName, worker); err != nil {
				return err
			}
			if !site.Paused {
				_ = config.SetProjectWorkers(site.Path, CollectRunningWorkerNames(site))
			}
			return nil
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
			// Allow stopping orphaned workers that have a running unit
			// but are no longer in the framework definition.
			if _, ok := fw.Workers[workerName]; !ok {
				unitName := "lerd-" + workerName + "-" + site.Name
				if !isServiceActiveOrRestarting(unitName) {
					return fmt.Errorf("framework %q has no worker named %q\nRun 'lerd worker list' to see available workers", fw.Label, workerName)
				}
			}
			if err := WorkerStopForSite(site.Name, workerName); err != nil {
				return err
			}
			if !site.Paused {
				_ = config.SetProjectWorkers(site.Path, CollectRunningWorkerNames(site))
			}
			return nil
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
			site, fw, _, err := resolveSiteAndFramework(cwd)
			if err != nil {
				return err
			}
			known := make(map[string]bool)
			if len(fw.Workers) == 0 {
				fmt.Printf("Framework %q has no workers defined.\n", fw.Label)
			} else {
				names := make([]string, 0, len(fw.Workers))
				for n, wDef := range fw.Workers {
					if wDef.Check != nil && !config.MatchesRule(cwd, *wDef.Check) {
						continue
					}
					names = append(names, n)
				}
				sort.Strings(names)
				fmt.Printf("Workers for %s:\n", fw.Label)
				for _, name := range names {
					known[name] = true
					w := fw.Workers[name]
					label := w.Label
					if label == "" {
						label = name
					}
					fmt.Printf("  %-15s %s\n", name, label)
					fmt.Printf("  %-15s command: %s\n", "", w.Command)
				}
			}

			// Detect orphaned workers — running units with no definition.
			orphans := findOrphanedWorkers(site.Name, known)
			if len(orphans) > 0 {
				fmt.Println("\nOrphaned workers (running but not defined):")
				for _, name := range orphans {
					fmt.Printf("  %-15s (stop with: lerd worker stop %s)\n", name, name)
				}
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
		return nil, nil, "", fmt.Errorf("site %q has no framework assigned — run 'lerd link' first", site.Name)
	}

	fw, ok := config.GetFrameworkForDir(fwName, cwd)
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
// If the worker has a Proxy config, the proxy port is auto-assigned and the
// nginx vhost is regenerated to include the WebSocket/HTTP proxy block.
func WorkerStartForSite(siteName, sitePath, phpVersion, workerName string, w config.FrameworkWorker) error {
	// Stop conflicting workers before starting.
	for _, conflict := range w.ConflictsWith {
		WorkerStopForSite(siteName, conflict) //nolint:errcheck
	}

	command := w.Command

	// Handle proxy port assignment and command augmentation.
	if w.Proxy != nil && w.Proxy.PortEnvKey != "" {
		envPath := filepath.Join(sitePath, ".env")
		port := envfile.ReadKey(envPath, w.Proxy.PortEnvKey)
		if port == "" {
			port = strconv.Itoa(assignWorkerProxyPort(sitePath, w.Proxy.PortEnvKey, w.Proxy.DefaultPort))
			_ = envfile.ApplyUpdates(envPath, map[string]string{w.Proxy.PortEnvKey: port})
		}
		command = command + " --port=" + port
	}

	versionShort := strings.ReplaceAll(phpVersion, ".", "")
	fpmUnit := "lerd-php" + versionShort + "-fpm"
	unitName := "lerd-" + workerName + "-" + siteName

	restart := w.Restart
	if restart == "" {
		restart = "always"
	}
	label := w.Label
	if label == "" {
		label = workerName
	}

	changed, err := writeWorkerUnitFile(unitName, label, siteName, sitePath, phpVersion, command, restart, w.Schedule, fpmUnit)
	if err != nil {
		return fmt.Errorf("writing worker unit: %w", err)
	}

	// Scheduled workers run via a sibling .timer that systemd starts on
	// the configured cadence; the .service is a Type=oneshot triggered by
	// the timer, so we enable and start the .timer rather than the
	// .service. The non-scheduled (daemon) path keeps the original
	// .service-based lifecycle.
	lifecycleTarget := unitName
	if w.Schedule != "" {
		lifecycleTarget = unitName + ".timer"
	}

	if changed {
		if err := services.Mgr.Enable(lifecycleTarget); err != nil {
			fmt.Printf("[WARN] enable: %v\n", err)
		}
	}

	if err := services.Mgr.Start(lifecycleTarget); err != nil {
		return fmt.Errorf("starting %s worker: %w", workerName, err)
	}

	fmt.Printf("%s started for %s\n", label, siteName)
	fmt.Printf("  Logs: %s\n", workerLogHint(unitName))

	// Regenerate nginx vhost if the worker has proxy config.
	if w.Proxy != nil {
		regenNginxVhost(siteName, sitePath)
	}

	// Persist this worker to .lerd.yaml so lerd install can restore it.
	// Additive: other workers already in the list are not removed. This covers
	// callers like setup and pause that start workers sequentially and must not
	// clobber each other's entries.
	_ = config.AddProjectWorker(sitePath, workerName)

	return nil
}

func newWorkerAddCmd() *cobra.Command {
	var (
		command       string
		label         string
		restart       string
		checkFile     string
		checkComposer string
		conflictsWith []string
		proxyPath     string
		proxyPortKey  string
		proxyDefPort  int
		global        bool
	)

	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a custom worker to this project or global framework overlay",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			site, err := config.FindSiteByPath(cwd)
			if err != nil {
				return fmt.Errorf("not a registered site — run 'lerd link' first")
			}

			w := config.FrameworkWorker{
				Label:         label,
				Command:       command,
				Restart:       restart,
				ConflictsWith: conflictsWith,
			}
			if checkFile != "" || checkComposer != "" {
				w.Check = &config.FrameworkRule{File: checkFile, Composer: checkComposer}
			}
			if proxyPath != "" {
				w.Proxy = &config.WorkerProxy{
					Path:        proxyPath,
					PortEnvKey:  proxyPortKey,
					DefaultPort: proxyDefPort,
				}
			}

			action := "added"
			if global {
				fwName := site.Framework
				if fwName == "" {
					return fmt.Errorf("site %q has no framework assigned", site.Name)
				}
				fw := config.LoadUserFramework(fwName)
				if fw == nil {
					fw = &config.Framework{Name: fwName}
				}
				if fw.Workers == nil {
					fw.Workers = make(map[string]config.FrameworkWorker)
				}
				if _, exists := fw.Workers[name]; exists {
					action = "updated"
				}
				fw.Workers[name] = w
				if err := config.SaveFramework(fw); err != nil {
					return fmt.Errorf("saving framework overlay: %w", err)
				}
				fmt.Printf("Custom worker %q %s in global %s overlay\n", name, action, fwName)
			} else {
				if proj, _ := config.LoadProjectConfig(cwd); proj.CustomWorkers != nil {
					if _, exists := proj.CustomWorkers[name]; exists {
						action = "updated"
					}
				}
				if err := config.SetProjectCustomWorker(cwd, name, w); err != nil {
					return fmt.Errorf("saving .lerd.yaml: %w", err)
				}
				fmt.Printf("Custom worker %q %s in .lerd.yaml\n", name, action)
			}
			fmt.Printf("Start it with: lerd worker start %s\n", name)
			return nil
		},
	}

	cmd.Flags().StringVar(&command, "command", "", "Command to run (required)")
	cmd.Flags().StringVar(&label, "label", "", "Human-readable label")
	cmd.Flags().StringVar(&restart, "restart", "", "Restart policy: always or on-failure")
	cmd.Flags().StringVar(&checkFile, "check-file", "", "Only show worker when this file exists")
	cmd.Flags().StringVar(&checkComposer, "check-composer", "", "Only show worker when this Composer package is installed")
	cmd.Flags().StringSliceVar(&conflictsWith, "conflicts-with", nil, "Workers to stop before starting this one")
	cmd.Flags().StringVar(&proxyPath, "proxy-path", "", "URL path to proxy (e.g. /app)")
	cmd.Flags().StringVar(&proxyPortKey, "proxy-port-env-key", "", "Env key holding the worker port")
	cmd.Flags().IntVar(&proxyDefPort, "proxy-default-port", 0, "Default port if env key is missing")
	cmd.Flags().BoolVar(&global, "global", false, "Save to global framework overlay instead of .lerd.yaml")
	_ = cmd.MarkFlagRequired("command")

	return cmd
}

func newWorkerRemoveCmd() *cobra.Command {
	var global bool

	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a custom worker from .lerd.yaml or global framework overlay",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			site, err := config.FindSiteByPath(cwd)
			if err != nil {
				return fmt.Errorf("not a registered site — run 'lerd link' first")
			}

			// Stop the worker if running.
			unitName := "lerd-" + name + "-" + site.Name
			if isServiceActiveOrRestarting(unitName) {
				_ = WorkerStopForSite(site.Name, name)
			}

			if global {
				fwName := site.Framework
				if fwName == "" {
					return fmt.Errorf("site %q has no framework assigned", site.Name)
				}
				fw := config.LoadUserFramework(fwName)
				if fw == nil || fw.Workers == nil {
					return fmt.Errorf("no global overlay for framework %q", fwName)
				}
				if _, exists := fw.Workers[name]; !exists {
					return fmt.Errorf("worker %q not found in global %s overlay", name, fwName)
				}
				delete(fw.Workers, name)
				if len(fw.Workers) == 0 {
					fw.Workers = nil
				}
				if err := config.SaveFramework(fw); err != nil {
					return fmt.Errorf("saving framework overlay: %w", err)
				}
				fmt.Printf("Custom worker %q removed from global %s overlay\n", name, fwName)
			} else {
				if err := config.RemoveProjectCustomWorker(cwd, name); err != nil {
					return err
				}
				fmt.Printf("Custom worker %q removed from .lerd.yaml\n", name)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&global, "global", false, "Remove from global framework overlay instead of .lerd.yaml")
	return cmd
}

// siteFrameworkName returns the saved framework name for the given site, or "".
// Does not auto-detect — framework should already be set at link time.
func siteFrameworkName(siteName string) string {
	site, err := config.FindSite(siteName)
	if err != nil {
		return ""
	}
	return site.Framework
}

// WorkerStopForSite stops and removes the named worker unit for the given site.
func WorkerStopForSite(siteName, workerName string) error {
	unitName := "lerd-" + workerName + "-" + siteName

	// Stop and disable both possible shapes (daemon .service and
	// scheduled .timer + oneshot .service). We don't know up-front
	// whether the worker was scheduled, and the framework yaml may
	// have flipped between the two shapes since the unit was written,
	// so we tear down both unconditionally — missing units are no-ops
	// at this layer.
	_ = services.Mgr.Disable(unitName + ".timer")
	podman.StopUnit(unitName + ".timer") //nolint:errcheck
	_ = services.Mgr.Disable(unitName)
	podman.StopUnit(unitName) //nolint:errcheck

	if err := services.Mgr.RemoveTimerUnit(unitName); err != nil {
		return fmt.Errorf("removing timer unit file: %w", err)
	}
	if err := services.Mgr.RemoveServiceUnit(unitName); err != nil {
		return fmt.Errorf("removing unit file: %w", err)
	}
	if err := podman.DaemonReloadFn(); err != nil {
		fmt.Printf("[WARN] daemon-reload: %v\n", err)
	}

	label := workerName
	fmt.Printf("%s stopped for %s\n", label, siteName)
	return nil
}

// isServiceActiveOrRestarting returns true if the unit is active or activating.
func isServiceActiveOrRestarting(name string) bool {
	status, _ := podman.UnitStatus(name)
	return status == "active" || status == "activating"
}

// findOrphanedWorkers returns worker names that are running but not in the known set.
func findOrphanedWorkers(siteName string, known map[string]bool) []string {
	suffix := "-" + siteName
	prefix := "lerd-"
	units := services.Mgr.ListServiceUnits("lerd-*-" + siteName)
	var orphans []string
	for _, unit := range units {
		workerName := strings.TrimPrefix(unit, prefix)
		workerName = strings.TrimSuffix(workerName, suffix)
		if workerName == "" || known[workerName] {
			continue
		}
		switch workerName {
		case "php84-fpm", "php83-fpm", "php82-fpm", "php81-fpm", "php80-fpm",
			"nginx", "dns", "dns-forwarder", "watcher", "ui", "stripe":
			continue
		}
		if isServiceActiveOrRestarting(unit) {
			orphans = append(orphans, workerName)
		}
	}
	return orphans
}
