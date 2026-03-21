package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
	phpDet "github.com/geodro/lerd/internal/php"
	lerdSystemd "github.com/geodro/lerd/internal/systemd"
	"github.com/geodro/lerd/internal/podman"
	"github.com/spf13/cobra"
)

// NewReverbCmd returns the reverb parent command with start/stop subcommands.
func NewReverbCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reverb",
		Short: "Manage the Laravel Reverb WebSocket server for the current site",
	}
	cmd.AddCommand(newReverbStartCmd("start"))
	cmd.AddCommand(newReverbStopCmd("stop"))
	return cmd
}

// NewReverbStartCmd returns the standalone reverb:start command.
func NewReverbStartCmd() *cobra.Command { return newReverbStartCmd("reverb:start") }

// NewReverbStopCmd returns the standalone reverb:stop command.
func NewReverbStopCmd() *cobra.Command { return newReverbStopCmd("reverb:stop") }

func newReverbStartCmd(use string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: "Start the Laravel Reverb WebSocket server for the current site as a systemd service",
		RunE: func(_ *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
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
			return ReverbStartForSite(siteName, cwd, phpVersion)
		},
	}
}

func newReverbStopCmd(use string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: "Stop the Laravel Reverb WebSocket server for the current site",
		RunE: func(_ *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			siteName, err := queueSiteName(cwd)
			if err != nil {
				return err
			}
			return ReverbStopForSite(siteName)
		},
	}
}

// ReverbStartForSite starts the Reverb WebSocket server for the named site.
func ReverbStartForSite(siteName, sitePath, phpVersion string) error {
	versionShort := strings.ReplaceAll(phpVersion, ".", "")
	fpmUnit := "lerd-php" + versionShort + "-fpm"
	container := "lerd-php" + versionShort + "-fpm"
	unitName := "lerd-reverb-" + siteName

	unit := fmt.Sprintf(`[Unit]
Description=Lerd Reverb (%s)
After=network.target %s.service
BindsTo=%s.service

[Service]
Type=simple
Restart=on-failure
RestartSec=5
ExecStart=podman exec -w %s %s php artisan reverb:start

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
		return fmt.Errorf("starting reverb: %w", err)
	}
	fmt.Printf("Reverb started for %s\n", siteName)
	fmt.Printf("  Logs: journalctl --user -u %s -f\n", unitName)
	return nil
}

// ReverbStopForSite stops and removes the Reverb unit for the named site.
func ReverbStopForSite(siteName string) error {
	unitName := "lerd-reverb-" + siteName
	unitFile := filepath.Join(config.SystemdUserDir(), unitName+".service")

	_ = lerdSystemd.DisableService(unitName)
	podman.StopUnit(unitName) //nolint:errcheck

	if err := os.Remove(unitFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing unit file: %w", err)
	}
	if err := podman.DaemonReload(); err != nil {
		fmt.Printf("[WARN] daemon-reload: %v\n", err)
	}
	fmt.Printf("Reverb stopped for %s\n", siteName)
	return nil
}

// SiteHasReverb returns true if composer.json lists laravel/reverb as a dependency.
func SiteHasReverb(sitePath string) bool {
	data, err := os.ReadFile(filepath.Join(sitePath, "composer.json"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), `"laravel/reverb"`)
}

// SiteUsesReverb returns true if the site uses Laravel Reverb — either as a
// composer dependency or with BROADCAST_CONNECTION=reverb in .env or .env.example.
func SiteUsesReverb(sitePath string) bool {
	if SiteHasReverb(sitePath) {
		return true
	}
	for _, name := range []string{".env", ".env.example"} {
		if envfile.ReadKey(filepath.Join(sitePath, name), "BROADCAST_CONNECTION") == "reverb" {
			return true
		}
	}
	return false
}
