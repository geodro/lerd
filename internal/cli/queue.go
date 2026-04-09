package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	lerdSystemd "github.com/geodro/lerd/internal/systemd"
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

	if err := queueStartExplicit(siteName, cwd, phpVersion, queue, tries, timeout); err != nil {
		return err
	}
	if site, err := config.FindSite(siteName); err == nil {
		SyncLerdYAMLWorkers(site)
	}
	return nil
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

	if err := QueueStopForSite(siteName); err != nil {
		return err
	}
	if site, err := config.FindSite(siteName); err == nil {
		SyncLerdYAMLWorkers(site)
	}
	return nil
}

func queueStartExplicit(siteName, sitePath, phpVersion, queue string, tries, timeout int) error {
	unitName := "lerd-queue-" + siteName
	unit := buildQueueUnit(siteName, sitePath, phpVersion, queue, tries, timeout)

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

// buildQueueUnit renders the systemd unit body for a queue worker. Pure
// function so the dep wiring can be exercised in tests.
func buildQueueUnit(siteName, sitePath, phpVersion, queue string, tries, timeout int) string {
	versionShort := strings.ReplaceAll(phpVersion, ".", "")
	fpmUnit := "lerd-php" + versionShort + "-fpm"
	container := "lerd-php" + versionShort + "-fpm"
	artisanArgs := fmt.Sprintf("queue:work --queue=%s --tries=%d --timeout=%d", queue, tries, timeout)

	// Wants= the backing service so systemd pulls it in; Restart=always covers
	// the ready-window between activation and the container accepting connections.
	depUnits := append([]string{fpmUnit + ".service"}, queueDependencyUnits(sitePath)...)
	afterLine := "After=network.target " + strings.Join(depUnits, " ")
	wantsLine := "Wants=" + strings.Join(depUnits, " ")

	return fmt.Sprintf(`[Unit]
Description=Lerd Queue Worker (%s)
%s
%s
BindsTo=%s.service

[Service]
Type=simple
Restart=always
RestartSec=5
ExecStart=podman exec -w %s %s php artisan %s

[Install]
WantedBy=default.target
`, siteName, afterLine, wantsLine, fpmUnit, sitePath, container, artisanArgs)
}

// queueDependencyUnits returns the lerd service unit(s) the queue worker
// needs based on QUEUE_CONNECTION (and DB_CONNECTION). FPM is added by the
// caller. Returns nil for sync / sqs / sqlite / unreadable .env.
func queueDependencyUnits(sitePath string) []string {
	envPath := filepath.Join(sitePath, ".env")
	switch envfile.ReadKey(envPath, "QUEUE_CONNECTION") {
	case "redis":
		return []string{"lerd-redis.service"}
	case "database":
		switch envfile.ReadKey(envPath, "DB_CONNECTION") {
		case "mysql", "mariadb":
			return []string{"lerd-mysql.service"}
		case "pgsql", "pgsql_pdo", "postgres":
			return []string{"lerd-postgres.service"}
		}
	}
	return nil
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
				_ = podman.DaemonReload()
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
	unitFile := filepath.Join(config.SystemdUserDir(), unitName+".service")

	// Stop and disable — ignore errors if already stopped.
	_ = lerdSystemd.DisableService(unitName)
	podman.StopUnit(unitName) //nolint:errcheck

	if err := os.Remove(unitFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing unit file: %w", err)
	}

	if err := podman.DaemonReload(); err != nil {
		fmt.Printf("[WARN] daemon-reload: %v\n", err)
	}

	fmt.Printf("Queue worker stopped for %s\n", siteName)
	return nil
}
