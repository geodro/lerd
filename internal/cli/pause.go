package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/nginx"
	phpDet "github.com/geodro/lerd/internal/php"
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

	if err := nginx.Reload(); err != nil {
		fmt.Printf("[WARN] nginx reload: %v\n", err)
	}

	fmt.Printf("Paused: %s (%s)\n", name, site.Domain)
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
	if detected, err := phpDet.DetectVersion(site.Path); err == nil && detected != "" {
		phpVersion = detected
	}

	if site.Secured {
		if err := nginx.GenerateSSLVhost(*site, phpVersion); err != nil {
			return fmt.Errorf("generating SSL vhost: %w", err)
		}
		sslConf := filepath.Join(config.NginxConfD(), site.Domain+"-ssl.conf")
		mainConf := filepath.Join(config.NginxConfD(), site.Domain+".conf")
		_ = os.Remove(mainConf)
		if err := os.Rename(sslConf, mainConf); err != nil {
			return fmt.Errorf("installing SSL vhost: %w", err)
		}
	} else {
		if err := nginx.GenerateVhost(*site, phpVersion); err != nil {
			return fmt.Errorf("generating vhost: %w", err)
		}
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

	_ = os.Remove(filepath.Join(config.PausedDir(), site.Domain+".html"))

	fmt.Printf("Resumed: %s (%s)\n", name, site.Domain)
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

// collectRunningWorkers returns the names of all active workers for the site.
func collectRunningWorkers(site *config.Site) []string {
	var active []string

	for _, w := range []string{"queue", "schedule", "reverb"} {
		if lerdSystemd.IsServiceActive("lerd-" + w + "-" + site.Name) {
			active = append(active, w)
		}
	}
	if lerdSystemd.IsServiceActive("lerd-stripe-" + site.Name) {
		active = append(active, "stripe")
	}

	// Framework-defined custom workers (skip built-ins already checked above).
	if fw, ok := config.GetFramework(site.Framework); ok && fw.Workers != nil {
		names := make([]string, 0, len(fw.Workers))
		for wName := range fw.Workers {
			switch wName {
			case "queue", "schedule", "reverb":
				continue
			}
			names = append(names, wName)
		}
		sort.Strings(names)
		for _, wName := range names {
			if lerdSystemd.IsServiceActive("lerd-" + wName + "-" + site.Name) {
				active = append(active, wName)
			}
		}
	}

	return active
}

// stopWorkerByName stops a single named worker for the site.
func stopWorkerByName(site *config.Site, workerName string) {
	switch workerName {
	case "queue":
		QueueStopForSite(site.Name) //nolint:errcheck
	case "schedule":
		ScheduleStopForSite(site.Name) //nolint:errcheck
	case "reverb":
		ReverbStopForSite(site.Name) //nolint:errcheck
	case "stripe":
		StripeStopForSite(site.Name) //nolint:errcheck
	default:
		WorkerStopForSite(site.Name, workerName) //nolint:errcheck
	}
}

// resumeWorkerByName restarts a single named worker for the site.
func resumeWorkerByName(site *config.Site, workerName, phpVersion string) {
	switch workerName {
	case "queue":
		QueueStartForSite(site.Name, site.Path, phpVersion) //nolint:errcheck
	case "schedule":
		ScheduleStartForSite(site.Name, site.Path, phpVersion) //nolint:errcheck
	case "reverb":
		ReverbStartForSite(site.Name, site.Path, phpVersion) //nolint:errcheck
	case "stripe":
		scheme := "http"
		if site.Secured {
			scheme = "https"
		}
		StripeStartForSite(site.Name, site.Path, scheme+"://"+site.Domain) //nolint:errcheck
	default:
		fw, ok := config.GetFramework(site.Framework)
		if !ok || fw.Workers == nil {
			return
		}
		worker, ok := fw.Workers[workerName]
		if !ok {
			return
		}
		WorkerStartForSite(site.Name, site.Path, phpVersion, workerName, worker) //nolint:errcheck
	}
}

// pausedPageTmpl is the HTML template for the paused-site landing page.
var pausedPageTmpl = template.Must(template.New("paused").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Domain}} — Paused</title>
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
      max-width: 400px;
      width: calc(100% - 2rem);
      text-align: center;
    }
    .icon { font-size: 2.5rem; margin-bottom: 1.25rem; }
    h1 { font-size: 1.2rem; font-weight: 600; margin: 0 0 0.3rem; }
    .domain {
      font-size: 0.8rem;
      color: #6b7280;
      font-family: ui-monospace, 'Cascadia Code', monospace;
      margin: 0 0 2rem;
    }
    button {
      background: #4f46e5;
      color: #fff;
      border: none;
      border-radius: 8px;
      padding: 0.7rem 0;
      font-size: 0.95rem;
      font-weight: 500;
      cursor: pointer;
      width: 100%;
      transition: background 0.15s;
    }
    button:hover:not(:disabled) { background: #4338ca; }
    button:disabled { background: #374151; cursor: not-allowed; }
    .status {
      margin-top: 1rem;
      font-size: 0.78rem;
      color: #9ca3af;
      min-height: 1.1em;
    }
    .error { color: #ef4444; }
  </style>
</head>
<body>
  <div class="card">
    <div class="icon">⏸</div>
    <h1>{{.Name}}</h1>
    <p class="domain">{{.Domain}}</p>
    <button id="btn" onclick="resume()">Resume Site</button>
    <p class="status" id="status"></p>
  </div>
  <script>
    async function resume() {
      const btn = document.getElementById('btn');
      const status = document.getElementById('status');
      btn.disabled = true;
      btn.textContent = 'Resuming\u2026';
      status.textContent = '';
      status.className = 'status';
      try {
        const r = await fetch('http://127.0.0.1:7073/api/sites/{{.Domain}}/unpause', { method: 'POST' });
        const data = await r.json();
        if (data.ok) {
          status.textContent = 'Resumed! Redirecting\u2026';
          setTimeout(() => { window.location.href = '{{.Scheme}}://{{.Domain}}'; }, 1200);
        } else {
          throw new Error(data.error || 'unknown error');
        }
      } catch (e) {
        btn.disabled = false;
        btn.textContent = 'Resume Site';
        status.textContent = 'Error: ' + e.message;
        status.className = 'status error';
      }
    }
  </script>
</body>
</html>
`))

type pausedPageData struct {
	Name   string
	Domain string
	Scheme string
}

// writePausedHTML renders and writes the landing page HTML for a paused site.
func writePausedHTML(site *config.Site) error {
	if err := os.MkdirAll(config.PausedDir(), 0755); err != nil {
		return err
	}

	scheme := "http"
	if site.Secured {
		scheme = "https"
	}

	var buf bytes.Buffer
	if err := pausedPageTmpl.Execute(&buf, pausedPageData{
		Name:   site.Name,
		Domain: site.Domain,
		Scheme: scheme,
	}); err != nil {
		return err
	}

	htmlPath := filepath.Join(config.PausedDir(), site.Domain+".html")
	return os.WriteFile(htmlPath, buf.Bytes(), 0644)
}
