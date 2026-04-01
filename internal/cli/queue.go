package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
	"github.com/spf13/cobra"
)

// NewQueueCmd returns the queue parent command with start/stop subcommands.
func NewQueueCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "queue",
		Short: "Manage queue workers for the current site",
	}
	cmd.AddCommand(newQueueStartCmd("start"))
	cmd.AddCommand(newQueueStopCmd("stop"))
	return cmd
}

// NewQueueStartCmd returns the standalone queue:start command.
func NewQueueStartCmd() *cobra.Command { return newQueueStartCmd("queue:start") }

// NewQueueStopCmd returns the standalone queue:stop command.
func NewQueueStopCmd() *cobra.Command { return newQueueStopCmd("queue:stop") }

func newQueueStartCmd(use string) *cobra.Command {
	var queue string
	var tries int
	var timeout int

	cmd := &cobra.Command{
		Use:   use,
		Short: "Start a queue worker for the current site as a systemd service",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runQueueStart(queue, tries, timeout)
		},
	}
	cmd.Flags().StringVar(&queue, "queue", "default", "Queue name to process")
	cmd.Flags().IntVar(&tries, "tries", 3, "Number of times to attempt a job before logging it as failed")
	cmd.Flags().IntVar(&timeout, "timeout", 60, "Seconds a job may run before timing out")
	return cmd
}

func newQueueStopCmd(use string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: "Stop the queue worker for the current site",
		RunE:  func(_ *cobra.Command, _ []string) error { return runQueueStop() },
	}
}

func queueSiteName(cwd string) (string, error) {
	reg, err := config.LoadSites()
	if err != nil {
		return "", err
	}
	for _, s := range reg.Sites {
		if s.Path == cwd {
			return s.Name, nil
		}
	}
	// Fall back to directory name.
	name, _ := siteNameAndDomain(filepath.Base(cwd), "test")
	return name, nil
}

func runQueueStart(queue string, tries, timeout int) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	if err := requireFrameworkWorker(cwd, "queue"); err != nil {
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

	return queueStartExplicit(siteName, cwd, phpVersion, queue, tries, timeout)
}

func runQueueStop() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	if err := requireFrameworkWorker(cwd, "queue"); err != nil {
		return err
	}

	siteName, err := queueSiteName(cwd)
	if err != nil {
		return err
	}

	return QueueStopForSite(siteName)
}

func queueStartExplicit(siteName, sitePath, phpVersion, queue string, tries, timeout int) error {
	// Pre-flight: if the site uses Redis as its queue connection, make sure
	// lerd-redis is actually running. Without it the queue worker fails immediately
	// with a cryptic PHP "getaddrinfo for lerd-redis failed" DNS error.
	envPath := filepath.Join(sitePath, ".env")
	if envfile.ReadKey(envPath, "QUEUE_CONNECTION") == "redis" {
		if running, _ := podman.ContainerRunning("lerd-redis"); !running {
			return fmt.Errorf("queue worker requires Redis (QUEUE_CONNECTION=redis in .env) but lerd-redis is not running\nStart it first: lerd services start redis")
		}
	}

	versionShort := strings.ReplaceAll(phpVersion, ".", "")
	fpmUnit := "lerd-php" + versionShort + "-fpm"
	container := "lerd-php" + versionShort + "-fpm"
	unitName := "lerd-queue-" + siteName

	artisanArgs := fmt.Sprintf("queue:work --queue=%s --tries=%d --timeout=%d", queue, tries, timeout)

	unit := fmt.Sprintf(`[Unit]
Description=Lerd Queue Worker (%s)
After=network.target %s.service
BindsTo=%s.service

[Service]
Type=simple
Restart=always
RestartSec=5
ExecStart=%s exec -w %s %s php artisan %s

[Install]
WantedBy=default.target
`, siteName, fpmUnit, fpmUnit, podman.PodmanBin(), sitePath, container, artisanArgs)

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
	killArtisanInContainer(container, "php artisan queue:")
	if err := services.Mgr.Start(unitName); err != nil {
		return fmt.Errorf("starting queue worker: %w", err)
	}

	fmt.Printf("Queue worker started for %s (queue: %s)\n", siteName, queue)
	fmt.Printf("  Logs: journalctl --user -u %s -f\n", unitName)
	return nil
}

// QueueStartForSite starts a queue worker for the given site with default settings.
func QueueStartForSite(siteName, sitePath, phpVersion string) error {
	return queueStartExplicit(siteName, sitePath, phpVersion, "default", 3, 60)
}

// QueueRestartForSite signals the Laravel queue worker to gracefully restart by
// running php artisan queue:restart inside the PHP-FPM container. It is a no-op
// when no queue unit exists for the site. systemd restarts the worker after the
// graceful exit because the unit uses Restart=always.
func QueueRestartForSite(siteName, sitePath, phpVersion string) error {
	if phpVersion == "" {
		cfg, _ := config.LoadGlobal()
		phpVersion = cfg.PHP.DefaultVersion
	}

	unitName := "lerd-queue-" + siteName
	unitFile := filepath.Join(config.SystemdUserDir(), unitName+".service")
	if _, err := os.Stat(unitFile); os.IsNotExist(err) {
		return nil // no queue worker for this site
	}

	// Upgrade legacy units that used Restart=on-failure — queue:restart causes a
	// clean exit (code 0) which on-failure does not restart.
	if data, err := os.ReadFile(unitFile); err == nil {
		if updated := strings.ReplaceAll(string(data), "Restart=on-failure", "Restart=always"); updated != string(data) {
			if writeErr := os.WriteFile(unitFile, []byte(updated), 0644); writeErr == nil {
				_ = services.Mgr.DaemonReload()
			}
		}
	}

	versionShort := strings.ReplaceAll(phpVersion, ".", "")
	container := "lerd-php" + versionShort + "-fpm"

	if running, _ := podman.ContainerRunning(container); !running {
		return nil
	}

	if _, err := podman.Run("exec", "-w", sitePath, container, "php", "artisan", "queue:restart"); err != nil {
		return fmt.Errorf("queue:restart for %s: %w", siteName, err)
	}
	fmt.Printf("Queue worker signaled to restart for %s\n", siteName)
	return nil
}

// QueueStopForSite stops and removes the queue worker for the named site.
func QueueStopForSite(siteName string) error {
	unitName := "lerd-queue-" + siteName

	// Stop and disable — ignore errors if already stopped.
	_ = services.Mgr.Disable(unitName)
	services.Mgr.Stop(unitName) //nolint:errcheck

	// Kill any lingering in-container queue processes. On macOS, launchd only
	// kills the host-side `podman exec` process when the plist is unloaded;
	// the PHP worker inside the container keeps running and duplicates accumulate.
	killArtisanInContainer(fpmContainerForSite(siteName), "php artisan queue:")

	if err := services.Mgr.RemoveServiceUnit(unitName); err != nil {
		return fmt.Errorf("removing unit file: %w", err)
	}

	if err := services.Mgr.DaemonReload(); err != nil {
		fmt.Printf("[WARN] daemon-reload: %v\n", err)
	}

	fmt.Printf("Queue worker stopped for %s\n", siteName)
	return nil
}

// fpmContainerForSite returns the PHP-FPM container name for the given site by
// looking up its PHP version from the site config.
func fpmContainerForSite(siteName string) string {
	site, err := config.FindSite(siteName)
	phpVersion := ""
	if err == nil && site != nil {
		phpVersion = site.PHPVersion
	}
	if phpVersion == "" {
		cfg, _ := config.LoadGlobal()
		phpVersion = cfg.PHP.DefaultVersion
	}
	return "lerd-php" + strings.ReplaceAll(phpVersion, ".", "") + "-fpm"
}

// killArtisanInContainer kills PHP artisan processes matching the given pattern
// inside the container. This is a macOS-only workaround: launchd kills only the
// host-side `podman exec` process when a plist is unloaded, leaving the PHP
// worker running inside the Podman Machine VM. On Linux, systemd stops the full
// cgroup so the in-container process is already gone.
func killArtisanInContainer(container, pattern string) {
	if runtime.GOOS != "darwin" {
		return
	}
	if running, _ := podman.ContainerRunning(container); !running {
		return
	}
	podman.RunSilent("exec", container, "pkill", "-f", pattern) //nolint:errcheck
}
