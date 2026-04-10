package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
	"github.com/geodro/lerd/internal/nginx"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
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
		Short: "Start the Laravel Reverb WebSocket server for the current site as a background service",
		RunE: func(_ *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			if !SiteHasReverb(cwd) {
				return fmt.Errorf("laravel/reverb is not installed in this project\nInstall it with: composer require laravel/reverb\nSee https://laravel.com/docs/13.x/broadcasting")
			}
			if err := requireFrameworkWorker(cwd, "reverb"); err != nil {
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
			if err := ReverbStartForSite(siteName, cwd, phpVersion); err != nil {
				return err
			}
			if site, err := config.FindSite(siteName); err == nil {
				SyncLerdYAMLWorkers(site)
			}
			return nil
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
			if !SiteHasReverb(cwd) {
				return fmt.Errorf("laravel/reverb is not installed in this project\nInstall it with: composer require laravel/reverb\nSee https://laravel.com/docs/13.x/broadcasting")
			}
			if err := requireFrameworkWorker(cwd, "reverb"); err != nil {
				return err
			}
			siteName, err := queueSiteName(cwd)
			if err != nil {
				return err
			}
			if err := ReverbStopForSite(siteName); err != nil {
				return err
			}
			if site, err := config.FindSite(siteName); err == nil {
				SyncLerdYAMLWorkers(site)
			}
			return nil
		},
	}
}

// ReverbStartForSite starts the Reverb WebSocket server for the named site.
func ReverbStartForSite(siteName, sitePath, phpVersion string) error {
	versionShort := strings.ReplaceAll(phpVersion, ".", "")
	unitName := "lerd-reverb-" + siteName

	// Read the port Reverb should listen on from the site's .env.
	envPath := filepath.Join(sitePath, ".env")
	reverbPort := envfile.ReadKey(envPath, "REVERB_SERVER_PORT")
	if reverbPort == "" {
		reverbPort = strconv.Itoa(assignReverbServerPort(sitePath))
		_ = envfile.ApplyUpdates(envPath, map[string]string{"REVERB_SERVER_PORT": reverbPort})
	}

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
Exec=php artisan reverb:start --port=%s
`, unitName, image,
			sitePath, sitePath,
			config.PHPConfFile(phpVersion),
			config.PHPUserIniFile(phpVersion),
			config.ContainerHostsFile(),
			sitePath, reverbPort)
		if err := services.Mgr.WriteContainerUnit(unitName, unit); err != nil {
			return fmt.Errorf("writing container unit: %w", err)
		}
		if err := services.Mgr.Start(unitName); err != nil {
			return fmt.Errorf("starting reverb: %w", err)
		}
		fmt.Printf("Reverb started for %s\n", siteName)
		fmt.Printf("  Logs: podman logs -f %s\n", unitName)
	} else {
		fpmUnit := "lerd-php" + versionShort + "-fpm"
		container := fpmUnit
		unit := fmt.Sprintf(`[Unit]
Description=Lerd Reverb (%s)
After=network.target %s.service
BindsTo=%s.service

[Service]
Type=simple
Restart=on-failure
RestartSec=5
ExecStart=%s exec -w %s %s php artisan reverb:start --port=%s

[Install]
WantedBy=default.target
`, siteName, fpmUnit, fpmUnit, podman.PodmanBin(), sitePath, container, reverbPort)

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
			return fmt.Errorf("starting reverb: %w", err)
		}
		fmt.Printf("Reverb started for %s\n", siteName)
		fmt.Printf("  Logs: journalctl --user -u %s -f\n", unitName)
	}

	// Regenerate the nginx vhost so the /app WebSocket proxy block is added.
	if site, err := config.FindSite(siteName); err == nil {
		phpVer := site.PHPVersion
		if detected, detErr := phpDet.DetectVersion(sitePath); detErr == nil && detected != "" {
			phpVer = detected
		}
		var vhostErr error
		if site.Secured {
			vhostErr = nginx.GenerateSSLVhost(*site, phpVer)
		} else {
			vhostErr = nginx.GenerateVhost(*site, phpVer)
		}
		if vhostErr == nil {
			_ = nginx.Reload()
		}
	}
	return nil
}

// ReverbStopForSite stops and removes the Reverb unit for the named site.
func ReverbStopForSite(siteName string) error {
	unitName := "lerd-reverb-" + siteName

	_ = services.Mgr.Disable(unitName)
	services.Mgr.Stop(unitName) //nolint:errcheck

	if err := services.Mgr.RemoveServiceUnit(unitName); err != nil {
		return fmt.Errorf("removing unit file: %w", err)
	}
	if err := services.Mgr.DaemonReload(); err != nil {
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
