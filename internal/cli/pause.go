package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/geodro/lerd/internal/config"
	gitpkg "github.com/geodro/lerd/internal/git"
	"github.com/geodro/lerd/internal/nginx"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	lerdSystemd "github.com/geodro/lerd/internal/systemd"
	"github.com/spf13/cobra"
)

// NewPauseCmd returns the pause command.
func NewPauseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pause [site]",
		Short: "Pause a site: stop its workers and replace the vhost with a landing page",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name, err := resolveSiteName(args)
			if err != nil {
				return err
			}
			return PauseSite(name)
		},
	}
}

// NewUnpauseCmd returns the unpause command.
func NewUnpauseCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "unpause [site]",
		Aliases: []string{"resume"},
		Short:   "Resume a paused site: restore its vhost and restart previously running workers",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name, err := resolveSiteName(args)
			if err != nil {
				return err
			}
			return UnpauseSite(name)
		},
	}
}

// PauseSite stops all running workers for the site, replaces its nginx vhost with a
// landing page, and marks it paused in the registry.
func PauseSite(name string) error {
	site, err := config.FindSite(name)
	if err != nil {
		return fmt.Errorf("site %q not found", name)
	}
	if site.Paused {
		fmt.Printf("%s is already paused.\n", name)
		return nil
	}

	running := collectRunningWorkers(site)

	for _, w := range running {
		stopWorkerByName(site, w)
	}

	// Stop the custom container when pausing a custom container site.
	if site.IsCustomContainer() {
		_ = podman.StopUnit(podman.CustomContainerName(site.Name))
	}

	if err := writePausedHTML(site); err != nil {
		return fmt.Errorf("writing paused page: %w", err)
	}

	if err := nginx.GeneratePausedVhost(*site); err != nil {
		return fmt.Errorf("generating paused vhost: %w", err)
	}

	site.Paused = true
	site.PausedWorkers = running
	if err := config.AddSite(*site); err != nil {
		return fmt.Errorf("updating registry: %w", err)
	}

	pauseWorktrees(site)

	if err := nginx.Reload(); err != nil {
		fmt.Printf("[WARN] nginx reload: %v\n", err)
	}

	fmt.Printf("Paused: %s (%s)\n", name, site.PrimaryDomain())
	if len(running) > 0 {
		fmt.Printf("  Workers stopped: %s\n", strings.Join(running, ", "))
	}

	autoStopUnusedServices()

	return nil
}

// UnpauseSite restores the site's nginx vhost, restarts any workers that were
// running when the site was paused, and clears the paused state.
func UnpauseSite(name string) error {
	site, err := config.FindSite(name)
	if err != nil {
		return fmt.Errorf("site %q not found", name)
	}
	if !site.Paused {
		fmt.Printf("%s is not paused.\n", name)
		return nil
	}

	phpVersion := site.PHPVersion

	if site.IsCustomContainer() {
		// Start the custom container and restore the proxy vhost.
		_ = podman.StartUnit(podman.CustomContainerName(site.Name))
		if site.Secured {
			if err := nginx.GenerateCustomSSLVhost(*site); err != nil {
				return fmt.Errorf("generating custom SSL vhost: %w", err)
			}
			sslConf := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+"-ssl.conf")
			mainConf := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+".conf")
			_ = os.Remove(mainConf)
			if err := os.Rename(sslConf, mainConf); err != nil {
				return fmt.Errorf("installing SSL vhost: %w", err)
			}
		} else {
			if err := nginx.GenerateCustomVhost(*site); err != nil {
				return fmt.Errorf("generating custom vhost: %w", err)
			}
		}
	} else {
		if detected, err := phpDet.DetectVersion(site.Path); err == nil && detected != "" {
			phpVersion = detected
		}

		if phpVersion != "" {
			if err := ensureFPMQuadlet(phpVersion); err != nil {
				fmt.Printf("[WARN] ensuring FPM for PHP %s: %v\n", phpVersion, err)
			}
		}

		if site.Secured {
			if err := nginx.GenerateSSLVhost(*site, phpVersion); err != nil {
				return fmt.Errorf("generating SSL vhost: %w", err)
			}
			sslConf := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+"-ssl.conf")
			mainConf := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+".conf")
			_ = os.Remove(mainConf)
			if err := os.Rename(sslConf, mainConf); err != nil {
				return fmt.Errorf("installing SSL vhost: %w", err)
			}
		} else {
			if err := nginx.GenerateVhost(*site, phpVersion); err != nil {
				return fmt.Errorf("generating vhost: %w", err)
			}
		}

		unpauseWorktrees(site, phpVersion)
	}

	if err := nginx.Reload(); err != nil {
		fmt.Printf("[WARN] nginx reload: %v\n", err)
	}

	startServicesForSite(site.Path)

	resumed := site.PausedWorkers
	for _, w := range resumed {
		resumeWorkerByName(site, w, phpVersion)
	}

	site.Paused = false
	site.PausedWorkers = nil
	if err := config.AddSite(*site); err != nil {
		return fmt.Errorf("updating registry: %w", err)
	}

	// The shared paused.html is left in place for other paused sites.

	fmt.Printf("Resumed: %s (%s)\n", name, site.PrimaryDomain())
	if len(resumed) > 0 {
		fmt.Printf("  Workers restarted: %s\n", strings.Join(resumed, ", "))
	}
	return nil
}

// ensureServicesForCwd checks whether the site at cwd is paused and, if so,
// starts any services its .env references that are not already running. Only
// prints a notice when at least one service actually needs to be started.
func ensureServicesForCwd(cwd string) {
	site, err := config.FindSiteByPath(cwd)
	if err != nil || !site.Paused {
		return
	}
	startServicesForSiteNoticed(cwd, site.Name)
}

// startServicesForSite reads the site's .env file and ensures every lerd service
// it references is running. Called when resuming a paused site.
func startServicesForSite(sitePath string) {
	startServicesForSiteNoticed(sitePath, "")
}

// startServicesForSiteNoticed is like startServicesForSite but prints a header
// notice (using siteName) only when at least one service actually needs starting.
// Pass an empty siteName to suppress the header.
func startServicesForSiteNoticed(sitePath, siteName string) {
	envData, err := os.ReadFile(filepath.Join(sitePath, ".env"))
	if err != nil {
		return
	}
	envContent := string(envData)

	candidates := make([]string, len(knownServices))
	copy(candidates, knownServices)
	if customs, cErr := config.ListCustomServices(); cErr == nil {
		for _, c := range customs {
			candidates = append(candidates, c.Name)
		}
	}

	headerPrinted := false
	for _, name := range candidates {
		if !strings.Contains(envContent, "lerd-"+name) {
			continue
		}
		if siteName != "" && !headerPrinted && !lerdSystemd.IsServiceActive("lerd-"+name) {
			fmt.Printf("[lerd] site %q is paused — starting required services...\n", siteName)
			headerPrinted = true
		}
		if err := ensureServiceRunning(name); err != nil {
			fmt.Printf("  [WARN] could not start %s: %v\n", name, err)
		}
	}
}

// CollectRunningWorkerNames returns the names of active workers for the site,
// including stripe. Used to sync .lerd.yaml.
func CollectRunningWorkerNames(site *config.Site) []string {
	return collectRunningWorkers(site)
}

// collectRunningWorkers returns the names of all active or restarting workers
// for the site. Uses IsServiceActiveOrRestarting so crash-looping workers are
// also detected and can be stopped on unlink/pause.
func collectRunningWorkers(site *config.Site) []string {
	var active []string

	// Enumerate all workers from the framework definition.
	if fw, ok := config.GetFrameworkForDir(site.Framework, site.Path); ok && fw.Workers != nil {
		names := make([]string, 0, len(fw.Workers))
		for wName := range fw.Workers {
			names = append(names, wName)
		}
		sort.Strings(names)
		for _, wName := range names {
			unit := "lerd-" + wName + "-" + site.Name
			// Scheduled workers' .service sits at inactive between timer firings.
			if lerdSystemd.IsServiceActiveOrRestarting(unit) ||
				lerdSystemd.IsTimerActive(unit) {
				active = append(active, wName)
			}
		}
	}

	// Stripe is not a framework worker — check it separately.
	if lerdSystemd.IsServiceActiveOrRestarting("lerd-stripe-" + site.Name) {
		active = append(active, "stripe")
	}

	// Detect orphaned workers — running units with no framework definition.
	known := make(map[string]bool, len(active))
	for _, a := range active {
		known[a] = true
	}
	active = append(active, lerdSystemd.FindOrphanedWorkers(site.Name, known)...)

	return active
}

// stopWorkerByName stops a single named worker for the site.
func stopWorkerByName(site *config.Site, workerName string) {
	if workerName == "stripe" {
		StripeStopForSite(site.Name) //nolint:errcheck
		return
	}
	WorkerStopForSite(site.Name, workerName) //nolint:errcheck
}

// resumeWorkerByName restarts a single named worker for the site.
func resumeWorkerByName(site *config.Site, workerName, phpVersion string) {
	if workerName == "stripe" {
		scheme := "http"
		if site.Secured {
			scheme = "https"
		}
		StripeStartForSite(site.Name, site.Path, scheme+"://"+site.PrimaryDomain()) //nolint:errcheck
		return
	}
	fw, ok := config.GetFrameworkForDir(site.Framework, site.Path)
	if !ok || fw.Workers == nil {
		return
	}
	worker, ok := fw.Workers[workerName]
	if !ok {
		return
	}
	WorkerStartForSite(site.Name, site.Path, phpVersion, workerName, worker) //nolint:errcheck
}

// pausedPageHTML is the static HTML for the shared paused-site landing page.
// A single file is served for all paused sites; JavaScript reads the hostname
// and calls the correct unpause API endpoint.
const pausedPageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Site Paused — Lerd</title>
  <style>
    *, *::before, *::after { box-sizing: border-box; }
    body {
      background: #0f1117;
      color: #e5e7eb;
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
      display: flex;
      align-items: center;
      justify-content: center;
      min-height: 100vh;
      margin: 0;
    }
    .card {
      background: #1a1d27;
      border: 1px solid #2d3142;
      border-radius: 14px;
      padding: 2.5rem 3rem;
      max-width: 420px;
      width: calc(100% - 2rem);
      text-align: center;
    }
    .logo {
      width: 48px;
      height: 48px;
      margin: 0 auto 1.25rem;
      background: #FF2D20;
      border-radius: 12px;
      display: flex;
      align-items: center;
      justify-content: center;
      font-weight: 700;
      font-size: 1.2rem;
      color: #fff;
    }
    h1 { font-size: 1.2rem; font-weight: 600; margin: 0 0 0.5rem; }
    .host {
      font-size: 0.85rem;
      color: #FF2D20;
      font-family: ui-monospace, 'Cascadia Code', monospace;
      margin: 0 0 1rem;
      word-break: break-all;
    }
    p {
      font-size: 0.85rem;
      color: #9ca3af;
      margin: 0 0 1.5rem;
      line-height: 1.5;
    }
    .actions { display: flex; gap: 0.5rem; }
    a, button {
      flex: 1;
      display: inline-block;
      text-decoration: none;
      text-align: center;
      border-radius: 8px;
      padding: 0.6rem 0;
      font-size: 0.85rem;
      font-weight: 500;
      cursor: pointer;
      transition: background 0.15s;
      border: none;
    }
    .btn-primary { background: #FF2D20; color: #fff; }
    .btn-primary:hover:not(:disabled) { background: #e02419; }
    .btn-primary:disabled { background: #374151; cursor: not-allowed; color: #9ca3af; }
    .btn-secondary { background: #262a36; color: #e5e7eb; border: 1px solid #2d3142; }
    .btn-secondary:hover { background: #2d3142; }
  </style>
</head>
<body>
  <div class="card">
    <div class="logo">L</div>
    <h1>Site Paused</h1>
    <p class="host" id="host"></p>
    <p>This site has been paused. Resume it to restore the application and restart any workers.</p>
    <div class="actions">
      <button id="btn" class="btn-primary" onclick="resume()">Resume</button>
      <a href="http://lerd.localhost" class="btn-secondary">Dashboard</a>
    </div>
  </div>
  <script>
    document.getElementById('host').textContent = location.hostname;
    async function resume() {
      const btn = document.getElementById('btn');
      btn.disabled = true;
      btn.textContent = 'Resuming\u2026';
      try {
        const r = await fetch('http://127.0.0.1:7073/api/sites/' + location.hostname + '/unpause', { method: 'POST' });
        const data = await r.json();
        if (data.ok) {
          btn.textContent = 'Redirecting\u2026';
          setTimeout(() => location.reload(), 1200);
        } else {
          throw new Error(data.error || 'unknown error');
        }
      } catch (e) {
        btn.disabled = false;
        btn.textContent = 'Resume';
        alert('Error: ' + e.message);
      }
    }
  </script>
</body>
</html>
`

// writePausedHTML ensures the shared paused landing page exists.
func writePausedHTML(_ *config.Site) error {
	dir := config.PausedDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "paused.html"), []byte(pausedPageHTML), 0644)
}

// pauseWorktrees generates paused HTML and nginx vhosts for every worktree of
// a site that is being paused. The resume button on each worktree page unpauses
// the parent site (which restores all worktree vhosts as well).
func pauseWorktrees(site *config.Site) {
	worktrees, err := gitpkg.DetectWorktrees(site.Path, site.PrimaryDomain())
	if err != nil || len(worktrees) == 0 {
		return
	}
	for _, wt := range worktrees {
		if err := writePausedWorktreeHTML(wt, site); err != nil {
			fmt.Printf("  [WARN] paused page for worktree %s: %v\n", wt.Domain, err)
			continue
		}
		if err := nginx.GeneratePausedWorktreeVhost(wt.Domain, site.PrimaryDomain(), config.PausedDir(), site.Secured); err != nil {
			fmt.Printf("  [WARN] paused vhost for worktree %s: %v\n", wt.Domain, err)
		}
	}
}

// unpauseWorktrees restores the normal nginx vhosts for every worktree of a
// site that has just been unpaused and removes their paused HTML files.
func unpauseWorktrees(site *config.Site, phpVersion string) {
	worktrees, err := gitpkg.DetectWorktrees(site.Path, site.PrimaryDomain())
	if err != nil || len(worktrees) == 0 {
		return
	}
	for _, wt := range worktrees {
		var vhostErr error
		if site.Secured {
			vhostErr = nginx.GenerateWorktreeSSLVhost(wt.Domain, wt.Path, phpVersion, site.PrimaryDomain())
		} else {
			vhostErr = nginx.GenerateWorktreeVhost(wt.Domain, wt.Path, phpVersion)
		}
		if vhostErr != nil {
			fmt.Printf("  [WARN] restoring worktree vhost %s: %v\n", wt.Domain, vhostErr)
		}
	}
}

// writePausedWorktreeHTML ensures the shared paused landing page exists (same file).
func writePausedWorktreeHTML(_ gitpkg.Worktree, parent *config.Site) error {
	return writePausedHTML(parent)
}
