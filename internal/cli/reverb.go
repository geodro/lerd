package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
	"github.com/geodro/lerd/internal/nginx"
	phpDet "github.com/geodro/lerd/internal/php"
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
			if site, err := config.FindSite(siteName); err == nil && !site.Paused {
				_ = config.SetProjectWorkers(site.Path, CollectRunningWorkerNames(site))
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
			if site, err := config.FindSite(siteName); err == nil && !site.Paused {
				_ = config.SetProjectWorkers(site.Path, CollectRunningWorkerNames(site))
			}
			return nil
		},
	}
}

// ReverbStartForSite starts the Reverb WebSocket server for the named site.
// This is an alias that looks up the "reverb" worker from the framework
// definition and delegates to the generic WorkerStartForSite, which handles
// proxy port assignment and nginx regeneration automatically.
func ReverbStartForSite(siteName, sitePath, phpVersion string) error {
	fw, ok := config.GetFrameworkForDir(siteFrameworkName(siteName), sitePath)
	if !ok {
		return fmt.Errorf("no framework found for site %q", siteName)
	}
	worker, ok := fw.Workers["reverb"]
	if !ok {
		return fmt.Errorf("framework %q has no worker named \"reverb\"", fw.Label)
	}
	return WorkerStartForSite(siteName, sitePath, phpVersion, "reverb", worker)
}

// ReverbStopForSite stops and removes the Reverb unit for the named site.
func ReverbStopForSite(siteName string) error {
	return WorkerStopForSite(siteName, "reverb")
}

// regenNginxVhost regenerates the nginx vhost for the site so proxy blocks are updated.
func regenNginxVhost(siteName, sitePath string) {
	site, err := config.FindSite(siteName)
	if err != nil {
		return
	}

	// Custom container sites handle proxying through the main container
	// template, so the PHP-specific vhost regeneration is not needed.
	if site.IsCustomContainer() {
		return
	}

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

// SiteHasProxyWorker returns true if the site's framework defines a worker with
// a proxy configuration and that worker's check rule passes.
func SiteHasProxyWorker(sitePath, workerName string) bool {
	site, err := config.FindSiteByPath(sitePath)
	if err != nil || site.Framework == "" {
		return false
	}
	fw, ok := config.GetFrameworkForDir(site.Framework, sitePath)
	if !ok {
		return false
	}
	return fw.HasWorker(workerName, sitePath)
}

// SiteHasReverb returns true if the site's framework defines a "reverb" worker
// and the worker's check rule passes (e.g. laravel/reverb is in composer.json).
func SiteHasReverb(sitePath string) bool {
	return SiteHasProxyWorker(sitePath, "reverb")
}

// SiteUsesReverb returns true if the site has a reverb worker configured and
// optionally checks for BROADCAST_CONNECTION=reverb in the env file.
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

// assignWorkerProxyPort finds the lowest unused port >= defaultPort for the given
// env key across all linked sites.
// assignWorkerProxyPort finds the lowest unused port >= defaultPort.
// It scans ALL proxy port env keys across ALL sites to prevent collisions
// between different workers and different frameworks.
func assignWorkerProxyPort(sitePath, envKey string, defaultPort int) int {
	if defaultPort == 0 {
		defaultPort = 8080
	}
	used := map[int]bool{}
	reg, err := config.LoadSites()
	if err != nil {
		return defaultPort
	}

	// Collect all proxy port env key names from every framework definition.
	proxyPortKeys := map[string]bool{envKey: true}
	for _, s := range reg.Sites {
		if s.Framework == "" {
			continue
		}
		fw, ok := config.GetFramework(s.Framework)
		if !ok {
			continue
		}
		for _, w := range fw.Workers {
			if w.Proxy != nil && w.Proxy.PortEnvKey != "" {
				proxyPortKeys[w.Proxy.PortEnvKey] = true
			}
		}
	}

	// Scan all sites for all proxy port values to build the used set.
	for _, s := range reg.Sites {
		if filepath.Clean(s.Path) == filepath.Clean(sitePath) {
			continue
		}
		for key := range proxyPortKeys {
			if v := envfile.ReadKey(filepath.Join(s.Path, ".env"), key); v != "" {
				if p, err := strconv.Atoi(v); err == nil {
					used[p] = true
				}
			}
		}
	}

	port := defaultPort
	for used[port] {
		port++
	}
	return port
}
