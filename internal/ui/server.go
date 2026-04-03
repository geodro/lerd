package ui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	_ "embed"

	"github.com/geodro/lerd/internal/certs"
	"github.com/geodro/lerd/internal/cli"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dns"
	"github.com/geodro/lerd/internal/envfile"
	gitpkg "github.com/geodro/lerd/internal/git"
	"github.com/geodro/lerd/internal/nginx"
	nodePkg "github.com/geodro/lerd/internal/node"
	phpPkg "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	lerdSystemd "github.com/geodro/lerd/internal/systemd"
	lerdUpdate "github.com/geodro/lerd/internal/update"
)

//go:embed index.html
var indexHTML []byte

//go:embed icons/icon.svg
var iconSVG []byte

//go:embed icons/icon-maskable.svg
var iconMaskableSVG []byte

//go:embed icons/icon-192.png
var icon192PNG []byte

//go:embed icons/icon-512.png
var icon512PNG []byte

//go:embed icons/icon-maskable-192.png
var iconMaskable192PNG []byte

//go:embed icons/icon-maskable-512.png
var iconMaskable512PNG []byte

const listenAddr = "0.0.0.0:7073"

var knownServices = []string{"mysql", "redis", "postgres", "meilisearch", "rustfs", "mailpit"}

var serviceEnvVars = map[string][]string{
	"mysql": {
		"DB_CONNECTION=mysql",
		"DB_HOST=lerd-mysql",
		"DB_PORT=3306",
		"DB_DATABASE=lerd",
		"DB_USERNAME=root",
		"DB_PASSWORD=lerd",
	},
	"postgres": {
		"DB_CONNECTION=pgsql",
		"DB_HOST=lerd-postgres",
		"DB_PORT=5432",
		"DB_DATABASE=lerd",
		"DB_USERNAME=postgres",
		"DB_PASSWORD=lerd",
	},
	"redis": {
		"REDIS_HOST=lerd-redis",
		"REDIS_PORT=6379",
		"REDIS_PASSWORD=null",
		"CACHE_STORE=redis",
		"SESSION_DRIVER=redis",
		"QUEUE_CONNECTION=redis",
	},
	"meilisearch": {
		"SCOUT_DRIVER=meilisearch",
		"MEILISEARCH_HOST=http://lerd-meilisearch:7700",
	},
	"rustfs": {
		"FILESYSTEM_DISK=s3",
		"AWS_ACCESS_KEY_ID=lerd",
		"AWS_SECRET_ACCESS_KEY=lerdpassword",
		"AWS_DEFAULT_REGION=us-east-1",
		"AWS_BUCKET=lerd",
		"AWS_URL=http://localhost:9000",
		"AWS_ENDPOINT=http://lerd-rustfs:9000",
		"AWS_USE_PATH_STYLE_ENDPOINT=true",
	},
	"mailpit": {
		"MAIL_MAILER=smtp",
		"MAIL_HOST=lerd-mailpit",
		"MAIL_PORT=1025",
		"MAIL_USERNAME=null",
		"MAIL_PASSWORD=null",
		"MAIL_ENCRYPTION=null",
	},
}

// Start starts the HTTP server on listenAddr.
func Start(currentVersion string) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/status", withCORS(handleStatus))
	mux.HandleFunc("/api/sites", withCORS(handleSites))
	mux.HandleFunc("/api/services", withCORS(handleServices))
	mux.HandleFunc("/api/services/", withCORS(func(w http.ResponseWriter, r *http.Request) {
		handleServiceAction(w, r)
	}))
	mux.HandleFunc("/api/version", withCORS(func(w http.ResponseWriter, r *http.Request) {
		handleVersion(w, r, currentVersion)
	}))
	mux.HandleFunc("/api/php-versions", withCORS(handlePHPVersions))
	mux.HandleFunc("/api/php-versions/", withCORS(handlePHPVersionAction))
	mux.HandleFunc("/api/node-versions", withCORS(handleNodeVersions))
	mux.HandleFunc("/api/node-versions/install", withCORS(handleInstallNodeVersion))
	mux.HandleFunc("/api/node-versions/", withCORS(handleNodeVersionAction))
	mux.HandleFunc("/api/sites/", withCORS(handleSiteAction))
	mux.HandleFunc("/api/logs/", withCORS(handleLogs))
	mux.HandleFunc("/api/queue/", withCORS(handleQueueLogs))
	mux.HandleFunc("/api/horizon/", withCORS(handleHorizonLogs))
	mux.HandleFunc("/api/stripe/", withCORS(handleStripeLogs))
	mux.HandleFunc("/api/schedule/", withCORS(handleScheduleLogs))
	mux.HandleFunc("/api/reverb/", withCORS(handleReverbLogs))
	mux.HandleFunc("/api/worker/", withCORS(handleWorkerLogs))
	mux.HandleFunc("/api/watcher/logs", withCORS(handleWatcherLogs))
	mux.HandleFunc("/api/watcher/start", withCORS(handleWatcherStart))
	mux.HandleFunc("/api/settings", withCORS(handleSettings))
	mux.HandleFunc("/api/settings/autostart", withCORS(handleSettingsAutostart))
	mux.HandleFunc("/api/xdebug/", withCORS(handleXdebugAction))
	mux.HandleFunc("/api/lerd/start", withCORS(handleLerdStart))
	mux.HandleFunc("/api/lerd/stop", withCORS(handleLerdStop))
	mux.HandleFunc("/api/lerd/quit", withCORS(handleLerdQuit))
	mux.HandleFunc("/manifest.webmanifest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json")
		base := "http://" + r.Host
		w.Write([]byte(`{"name":"Lerd","short_name":"Lerd","description":"Local Laravel development environment","start_url":"` + base + `/","display":"standalone","background_color":"#0d0d0d","theme_color":"#FF2D20","icons":[{"src":"` + base + `/icons/icon-192.png","sizes":"192x192","type":"image/png","purpose":"any"},{"src":"` + base + `/icons/icon-512.png","sizes":"512x512","type":"image/png","purpose":"any"},{"src":"` + base + `/icons/icon-maskable-192.png","sizes":"192x192","type":"image/png","purpose":"maskable"},{"src":"` + base + `/icons/icon-maskable-512.png","sizes":"512x512","type":"image/png","purpose":"maskable"},{"src":"` + base + `/icons/icon.svg","sizes":"any","type":"image/svg+xml","purpose":"any"}]}`)) //nolint:errcheck
	})
	mux.HandleFunc("/icons/icon.svg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write(iconSVG) //nolint:errcheck
	})
	mux.HandleFunc("/icons/icon-maskable.svg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write(iconMaskableSVG) //nolint:errcheck
	})
	mux.HandleFunc("/icons/icon-192.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(icon192PNG) //nolint:errcheck
	})
	mux.HandleFunc("/icons/icon-512.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(icon512PNG) //nolint:errcheck
	})
	mux.HandleFunc("/icons/icon-maskable-192.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(iconMaskable192PNG) //nolint:errcheck
	})
	mux.HandleFunc("/icons/icon-maskable-512.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(iconMaskable512PNG) //nolint:errcheck
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML) //nolint:errcheck
	})

	fmt.Printf("Lerd UI listening on http://%s\n", listenAddr)
	return http.ListenAndServe(listenAddr, mux)
}

func withCORS(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h(w, r)
	}
}

// openTerminalAt opens the user's preferred terminal emulator in dir.
// It checks $TERMINAL first, then falls back to a list of common emulators.
func openTerminalAt(dir string) error {
	type termCmd struct {
		bin  string
		args []string
	}

	candidates := []termCmd{}

	if t := os.Getenv("TERMINAL"); t != "" {
		candidates = append(candidates, termCmd{t, []string{}})
	}

	candidates = append(candidates,
		termCmd{"kitty", []string{"--directory", dir}},
		termCmd{"foot", []string{"--working-directory", dir}},
		termCmd{"alacritty", []string{"--working-directory", dir}},
		termCmd{"wezterm", []string{"start", "--cwd", dir}},
		termCmd{"ghostty", []string{"--working-directory=" + dir}},
		termCmd{"konsole", []string{"--workdir", dir}},
		termCmd{"gnome-terminal", []string{"--working-directory", dir}},
		termCmd{"xfce4-terminal", []string{"--working-directory", dir}},
		termCmd{"tilix", []string{"--working-directory", dir}},
		termCmd{"terminator", []string{"--working-directory", dir}},
		termCmd{"xterm", []string{"-e", "cd " + dir + " && exec $SHELL"}},
	)

	for _, t := range candidates {
		bin, err := exec.LookPath(t.bin)
		if err != nil {
			continue
		}
		args := t.args
		// For $TERMINAL with no preset args, just pass the dir via cd wrapper
		if t.bin == os.Getenv("TERMINAL") && len(args) == 0 {
			args = []string{"-e", "sh", "-c", "cd " + dir + " && exec $SHELL"}
		}
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		return cmd.Start()
	}
	return fmt.Errorf("no terminal emulator found; set $TERMINAL or install kitty, foot, alacritty, wezterm, ghostty, konsole, or gnome-terminal")
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

// StatusResponse is the response for GET /api/status.
type StatusResponse struct {
	DNS            DNSStatus    `json:"dns"`
	Nginx          ServiceCheck `json:"nginx"`
	PHPFPMs        []PHPStatus  `json:"php_fpms"`
	PHPDefault     string       `json:"php_default"`
	NodeDefault    string       `json:"node_default"`
	WatcherRunning bool         `json:"watcher_running"`
}

type DNSStatus struct {
	OK  bool   `json:"ok"`
	TLD string `json:"tld"`
}

type ServiceCheck struct {
	Running bool `json:"running"`
}

type PHPStatus struct {
	Version       string `json:"version"`
	Running       bool   `json:"running"`
	XdebugEnabled bool   `json:"xdebug_enabled"`
}

func handleStatus(w http.ResponseWriter, _ *http.Request) {
	cfg, _ := config.LoadGlobal()
	tld := "test"
	if cfg != nil {
		tld = cfg.DNS.TLD
	}

	dnsOK, _ := dns.Check(tld)
	nginxRunning, _ := podman.ContainerRunning("lerd-nginx")
	watcherCmd := exec.Command("systemctl", "--user", "is-active", "--quiet", "lerd-watcher")
	watcherRunning := watcherCmd.Run() == nil

	versions, _ := phpPkg.ListInstalled()
	var phpStatuses []PHPStatus
	for _, v := range versions {
		short := strings.ReplaceAll(v, ".", "")
		running, _ := podman.ContainerRunning("lerd-php" + short + "-fpm")
		xdebugEnabled := cfg != nil && cfg.IsXdebugEnabled(v)
		phpStatuses = append(phpStatuses, PHPStatus{Version: v, Running: running, XdebugEnabled: xdebugEnabled})
	}

	phpDefault := ""
	nodeDefault := ""
	if cfg != nil {
		phpDefault = cfg.PHP.DefaultVersion
		nodeDefault = cfg.Node.DefaultVersion
	}
	writeJSON(w, StatusResponse{
		DNS:            DNSStatus{OK: dnsOK, TLD: tld},
		Nginx:          ServiceCheck{Running: nginxRunning},
		PHPFPMs:        phpStatuses,
		PHPDefault:     phpDefault,
		NodeDefault:    nodeDefault,
		WatcherRunning: watcherRunning,
	})
}

// WorktreeResponse is embedded in SiteResponse for each git worktree.
type WorktreeResponse struct {
	Branch string `json:"branch"`
	Domain string `json:"domain"`
	Path   string `json:"path"`
}

// WorkerStatus represents a single framework worker and its running state.
type WorkerStatus struct {
	Name    string `json:"name"`
	Label   string `json:"label"`
	Running bool   `json:"running"`
}

// SiteResponse is the response for GET /api/sites.
type SiteResponse struct {
	Name              string             `json:"name"`
	Domain            string             `json:"domain"`
	Path              string             `json:"path"`
	PHPVersion        string             `json:"php_version"`
	NodeVersion       string             `json:"node_version"`
	TLS               bool               `json:"tls"`
	Framework         string             `json:"framework"`
	FPMRunning        bool               `json:"fpm_running"`
	IsLaravel         bool               `json:"is_laravel"`
	FrameworkLabel    string             `json:"framework_label"`
	QueueRunning      bool               `json:"queue_running"`
	StripeRunning     bool               `json:"stripe_running"`
	StripeSecretSet   bool               `json:"stripe_secret_set"`
	ScheduleRunning   bool               `json:"schedule_running"`
	ReverbRunning     bool               `json:"reverb_running"`
	HasReverb         bool               `json:"has_reverb"`
	HasHorizon        bool               `json:"has_horizon"`
	HorizonRunning    bool               `json:"horizon_running"`
	HasQueueWorker    bool               `json:"has_queue_worker"`
	HasScheduleWorker bool               `json:"has_schedule_worker"`
	FrameworkWorkers  []WorkerStatus     `json:"framework_workers,omitempty"`
	Paused            bool               `json:"paused"`
	Branch            string             `json:"branch"`
	Worktrees         []WorktreeResponse `json:"worktrees"`
}

func handleSites(w http.ResponseWriter, _ *http.Request) {
	reg, err := config.LoadSites()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var sites []SiteResponse
	for _, s := range reg.Sites {
		if s.Ignored {
			continue
		}
		// Always detect the live version from disk so .php-version / .node-version
		// files are reflected without needing a re-link.
		phpVersion := s.PHPVersion
		if detected, err := phpPkg.DetectVersion(s.Path); err == nil && detected != "" {
			phpVersion = detected
			if phpVersion != s.PHPVersion {
				s.PHPVersion = phpVersion
				_ = config.AddSite(s) // keep sites.yaml in sync
			}
		}

		nodeVersion := s.NodeVersion
		if detected, err := nodePkg.DetectVersion(s.Path); err == nil && detected != "" {
			nodeVersion = detected
			if nodeVersion != s.NodeVersion {
				s.NodeVersion = nodeVersion
				_ = config.AddSite(s)
			}
		}
		if strings.Trim(nodeVersion, "0123456789") != "" {
			nodeVersion = "" // discard non-numeric values like "system"
		}

		fpmRunning := false
		if phpVersion != "" {
			short := strings.ReplaceAll(phpVersion, ".", "")
			fpmRunning, _ = podman.ContainerRunning("lerd-php" + short + "-fpm")
		}
		fwName := s.Framework
		if fwName == "" {
			fwName, _ = config.DetectFramework(s.Path)
		}
		isLaravel := fwName == "laravel"
		fw, hasFw := config.GetFramework(fwName)

		var queueStatus, stripeStatus, scheduleStatus, reverbStatus string
		var stripeSecretSet, hasReverb, hasQueueWorker, hasScheduleWorker bool

		// Stripe is Laravel-only
		if isLaravel {
			stripeStatus, _ = podman.UnitStatus("lerd-stripe-" + s.Name)
			stripeSecretSet = cli.StripeSecretSet(s.Path)
		}

		// queue/schedule/reverb: driven by framework worker definitions
		if hasFw && fw.Workers != nil {
			if _, ok := fw.Workers["queue"]; ok {
				hasQueueWorker = true
				queueStatus, _ = podman.UnitStatus("lerd-queue-" + s.Name)
			}
			if _, ok := fw.Workers["schedule"]; ok {
				hasScheduleWorker = true
				scheduleStatus, _ = podman.UnitStatus("lerd-schedule-" + s.Name)
			}
			if _, ok := fw.Workers["reverb"]; ok {
				// For Laravel, reverb toggle still requires the package/env to be present.
				// For other frameworks, defining the worker is enough to show the toggle.
				if isLaravel {
					hasReverb = cli.SiteUsesReverb(s.Path)
				} else {
					hasReverb = true
				}
				if hasReverb {
					reverbStatus, _ = podman.UnitStatus("lerd-reverb-" + s.Name)
				}
			}
		}
		// For Laravel without reverb in workers map (shouldn't happen with built-in, but guard anyway)
		if isLaravel && !hasReverb {
			reverbStatus, _ = podman.UnitStatus("lerd-reverb-" + s.Name)
			hasReverb = cli.SiteUsesReverb(s.Path)
		}

		// Horizon: auto-detected from composer.json; replaces the queue toggle.
		var horizonStatus string
		var hasHorizon bool
		if isLaravel && cli.SiteHasHorizon(s.Path) {
			hasHorizon = true
			horizonStatus, _ = podman.UnitStatus("lerd-horizon-" + s.Name)
			hasQueueWorker = false // Horizon manages queues; suppress the plain queue toggle
		}

		// Collect custom framework workers (non-builtin names)
		var fwWorkers []WorkerStatus
		if hasFw && fw.Workers != nil {
			names := make([]string, 0, len(fw.Workers))
			for n := range fw.Workers {
				switch n {
				case "queue", "schedule", "reverb":
					continue
				}
				names = append(names, n)
			}
			sort.Strings(names)
			for _, wname := range names {
				w := fw.Workers[wname]
				unitStatus, _ := podman.UnitStatus("lerd-" + wname + "-" + s.Name)
				label := w.Label
				if label == "" {
					label = wname
				}
				fwWorkers = append(fwWorkers, WorkerStatus{
					Name:    wname,
					Label:   label,
					Running: unitStatus == "active",
				})
			}
		}

		worktreeResponses := []WorktreeResponse{}
		mainBranch := gitpkg.MainBranch(s.Path)
		if wts, err := gitpkg.DetectWorktrees(s.Path, s.Domain); err == nil {
			for _, wt := range wts {
				worktreeResponses = append(worktreeResponses, WorktreeResponse{
					Branch: wt.Branch,
					Domain: wt.Domain,
					Path:   wt.Path,
				})
			}
		}

		sites = append(sites, SiteResponse{
			Name:              s.Name,
			Domain:            s.Domain,
			Path:              s.Path,
			PHPVersion:        phpVersion,
			NodeVersion:       nodeVersion,
			TLS:               s.Secured,
			Framework:         s.Framework,
			IsLaravel:         isLaravel,
			FrameworkLabel:    frameworkLabel(fwName),
			FPMRunning:        fpmRunning,
			QueueRunning:      queueStatus == "active",
			StripeRunning:     stripeStatus == "active",
			StripeSecretSet:   stripeSecretSet,
			ScheduleRunning:   scheduleStatus == "active",
			ReverbRunning:     reverbStatus == "active",
			HasReverb:         hasReverb,
			HasHorizon:        hasHorizon,
			HorizonRunning:    horizonStatus == "active",
			HasQueueWorker:    hasQueueWorker,
			HasScheduleWorker: hasScheduleWorker,
			FrameworkWorkers:  fwWorkers,
			Paused:            s.Paused,
			Branch:            mainBranch,
			Worktrees:         worktreeResponses,
		})
	}
	if sites == nil {
		sites = []SiteResponse{}
	}
	writeJSON(w, sites)
}

// ServiceResponse is the response for GET /api/services.
type ServiceResponse struct {
	Name               string            `json:"name"`
	Status             string            `json:"status"`
	EnvVars            map[string]string `json:"env_vars"`
	Dashboard          string            `json:"dashboard,omitempty"`
	ConnectionURL      string            `json:"connection_url,omitempty"`
	Custom             bool              `json:"custom,omitempty"`
	SiteCount          int               `json:"site_count"`
	SiteDomains        []string          `json:"site_domains,omitempty"`
	Pinned             bool              `json:"pinned"`
	DependsOn          []string          `json:"depends_on,omitempty"`
	QueueSite          string            `json:"queue_site,omitempty"`
	StripeListenerSite string            `json:"stripe_listener_site,omitempty"`
	ScheduleWorkerSite string            `json:"schedule_worker_site,omitempty"`
	ReverbSite         string            `json:"reverb_site,omitempty"`
	HorizonSite        string            `json:"horizon_site,omitempty"`
	WorkerSite         string            `json:"worker_site,omitempty"`
	WorkerName         string            `json:"worker_name,omitempty"`
}

// builtinDashboards maps built-in service names to their dashboard URLs.
var builtinDashboards = map[string]string{
	"mailpit":     "http://localhost:8025",
	"rustfs":      "http://localhost:9001",
	"meilisearch": "http://localhost:7700",
}

// builtinConnectionURLs maps built-in service names to clickable connection URLs
// using localhost (for use with DB clients and tools on the host machine).
var builtinConnectionURLs = map[string]string{
	"mysql":    "mysql://root:lerd@127.0.0.1:3306/lerd",
	"postgres": "postgresql://postgres:lerd@127.0.0.1:5432/lerd",
	"redis":    "redis://127.0.0.1:6379",
}

func buildServiceResponse(name string) ServiceResponse {
	unit := "lerd-" + name
	status, _ := podman.UnitStatus(unit)
	if status == "" {
		status = "inactive"
	}

	envMap := map[string]string{}
	for _, kv := range serviceEnvVars[name] {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	return ServiceResponse{
		Name:          name,
		Status:        status,
		EnvVars:       envMap,
		Dashboard:     builtinDashboards[name],
		ConnectionURL: builtinConnectionURLs[name],
		SiteCount:    countSitesUsingService(name),
		SiteDomains:  sitesUsingService(name),
		Pinned:       config.ServiceIsPinned(name),
	}
}

// listActiveQueueWorkers returns the site names of active lerd-queue-* systemd units.
// frameworkLabel returns the display label for a framework name.
// Returns the Label field from the framework definition, or an empty string if not found.
func frameworkLabel(name string) string {
	if name == "" {
		return ""
	}
	if fw, ok := config.GetFramework(name); ok {
		return fw.Label
	}
	return name
}

func listActiveQueueWorkers() []string {
	return listActiveUnitsBySuffix("lerd-queue-*.service", "lerd-queue-")
}

// listActiveScheduleWorkers returns site names of active lerd-schedule-* units.
func listActiveScheduleWorkers() []string {
	return listActiveUnitsBySuffix("lerd-schedule-*.service", "lerd-schedule-")
}

// listActiveReverbServers returns site names of active lerd-reverb-* units.
func listActiveReverbServers() []string {
	return listActiveUnitsBySuffix("lerd-reverb-*.service", "lerd-reverb-")
}

// listActiveHorizonWorkers returns site names of active lerd-horizon-* units.
func listActiveHorizonWorkers() []string {
	return listActiveUnitsBySuffix("lerd-horizon-*.service", "lerd-horizon-")
}

// listActiveStripeListeners returns the site names of active lerd-stripe-* units
// that were started by `lerd stripe:listen` (i.e. have a .service file in the
// systemd user dir, as opposed to quadlet-based services like stripe-mock).
func listActiveStripeListeners() []string {
	all := listActiveUnitsBySuffix("lerd-stripe-*.service", "lerd-stripe-")
	var result []string
	for _, name := range all {
		unitFile := filepath.Join(config.SystemdUserDir(), "lerd-stripe-"+name+".service")
		if _, err := os.Stat(unitFile); err == nil {
			result = append(result, name)
		}
	}
	return result
}

func listActiveUnitsBySuffix(pattern, prefix string) []string {
	out, err := exec.Command("systemctl", "--user", "list-units", "--state=active",
		"--no-legend", "--plain", pattern).Output()
	if err != nil {
		return nil
	}
	var sites []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		unit := strings.TrimSuffix(fields[0], ".service")
		siteName := strings.TrimPrefix(unit, prefix)
		if siteName != unit && siteName != "" {
			sites = append(sites, siteName)
		}
	}
	return sites
}

func handleServices(w http.ResponseWriter, _ *http.Request) {
	services := make([]ServiceResponse, 0, len(knownServices))
	for _, name := range knownServices {
		services = append(services, buildServiceResponse(name))
	}
	customs, _ := config.ListCustomServices()
	for _, svc := range customs {
		unit := "lerd-" + svc.Name
		status, _ := podman.UnitStatus(unit)
		if status == "" {
			status = "inactive"
		}
		displayHandle := "lerd-" + svc.Name
		envMap := map[string]string{}
		for _, kv := range svc.EnvVars {
			parts := strings.SplitN(kv, "=", 2)
			if len(parts) == 2 {
				v := strings.ReplaceAll(parts[1], "{{site}}", displayHandle)
				v = strings.ReplaceAll(v, "{{site_testing}}", displayHandle+"_testing")
				envMap[parts[0]] = v
			}
		}
		services = append(services, ServiceResponse{
			Name:        svc.Name,
			Status:      status,
			EnvVars:     envMap,
			Dashboard:   svc.Dashboard,
			Custom:      true,
			SiteCount:   countSitesUsingService(svc.Name),
			SiteDomains: sitesUsingService(svc.Name),
			Pinned:      config.ServiceIsPinned(svc.Name),
			DependsOn:   svc.DependsOn,
		})
	}
	for _, siteName := range listActiveQueueWorkers() {
		services = append(services, ServiceResponse{
			Name:      "queue-" + siteName,
			Status:    "active",
			EnvVars:   map[string]string{},
			QueueSite: siteName,
		})
	}
	for _, siteName := range listActiveStripeListeners() {
		services = append(services, ServiceResponse{
			Name:               "stripe-" + siteName,
			Status:             "active",
			EnvVars:            map[string]string{},
			StripeListenerSite: siteName,
		})
	}
	for _, siteName := range listActiveScheduleWorkers() {
		services = append(services, ServiceResponse{
			Name:               "schedule-" + siteName,
			Status:             "active",
			EnvVars:            map[string]string{},
			ScheduleWorkerSite: siteName,
		})
	}
	for _, siteName := range listActiveReverbServers() {
		services = append(services, ServiceResponse{
			Name:       "reverb-" + siteName,
			Status:     "active",
			EnvVars:    map[string]string{},
			ReverbSite: siteName,
		})
	}
	for _, siteName := range listActiveHorizonWorkers() {
		services = append(services, ServiceResponse{
			Name:        "horizon-" + siteName,
			Status:      "active",
			EnvVars:     map[string]string{},
			HorizonSite: siteName,
		})
	}
	// Custom framework workers (non-builtin: not queue/schedule/reverb)
	if reg2, err2 := config.LoadSites(); err2 == nil {
		for _, s := range reg2.Sites {
			if s.Ignored {
				continue
			}
			fwN := s.Framework
			if fwN == "" {
				fwN, _ = config.DetectFramework(s.Path)
			}
			fw2, ok2 := config.GetFramework(fwN)
			if !ok2 || fw2.Workers == nil {
				continue
			}
			for wname, w := range fw2.Workers {
				switch wname {
				case "queue", "schedule", "reverb":
					continue
				}
				unitStatus, _ := podman.UnitStatus("lerd-" + wname + "-" + s.Name)
				if unitStatus == "active" {
					label := w.Label
					if label == "" {
						label = wname
					}
					services = append(services, ServiceResponse{
						Name:       wname + "-" + s.Name,
						Status:     "active",
						EnvVars:    map[string]string{},
						WorkerSite: s.Name,
						WorkerName: wname,
					})
				}
			}
		}
	}
	writeJSON(w, services)
}

// ServiceActionResponse wraps the service state plus any error details.
type ServiceActionResponse struct {
	ServiceResponse
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Logs  string `json:"logs,omitempty"`
}

func handleServiceAction(w http.ResponseWriter, r *http.Request) {
	// path: /api/services/{name}/start or /api/services/{name}/stop
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/services/"), "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	name, action := parts[0], parts[1]

	// Allow GET for logs sub-resource
	if action == "logs" {
		writeJSON(w, map[string]string{"logs": serviceRecentLogs("lerd-" + name)})
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Handle queue worker services (queue-{sitename})
	if strings.HasPrefix(name, "queue-") {
		siteName := strings.TrimPrefix(name, "queue-")
		if action == "stop" {
			opErr := podman.StopUnit("lerd-queue-" + siteName)
			resp := ServiceActionResponse{
				ServiceResponse: ServiceResponse{Name: name, Status: "inactive", EnvVars: map[string]string{}, QueueSite: siteName},
				OK:              opErr == nil,
			}
			if opErr != nil {
				resp.Error = opErr.Error()
				resp.Status = "active"
			}
			writeJSON(w, resp)
		} else {
			http.Error(w, "unsupported action for queue worker", http.StatusBadRequest)
		}
		return
	}

	// Handle stripe listener services (stripe-{sitename})
	if strings.HasPrefix(name, "stripe-") {
		siteName := strings.TrimPrefix(name, "stripe-")
		if action == "stop" {
			opErr := cli.StripeStopForSite(siteName)
			resp := ServiceActionResponse{
				ServiceResponse: ServiceResponse{Name: name, Status: "inactive", EnvVars: map[string]string{}, StripeListenerSite: siteName},
				OK:              opErr == nil,
			}
			if opErr != nil {
				resp.Error = opErr.Error()
				resp.Status = "active"
			}
			writeJSON(w, resp)
		} else {
			writeJSON(w, ServiceActionResponse{OK: false, Error: "unsupported action for stripe listener"})
		}
		return
	}

	// Handle schedule worker services (schedule-{sitename})
	if strings.HasPrefix(name, "schedule-") {
		siteName := strings.TrimPrefix(name, "schedule-")
		if action == "stop" {
			opErr := cli.ScheduleStopForSite(siteName)
			resp := ServiceActionResponse{
				ServiceResponse: ServiceResponse{Name: name, Status: "inactive", EnvVars: map[string]string{}, ScheduleWorkerSite: siteName},
				OK:              opErr == nil,
			}
			if opErr != nil {
				resp.Error = opErr.Error()
				resp.Status = "active"
			}
			writeJSON(w, resp)
		} else {
			writeJSON(w, ServiceActionResponse{OK: false, Error: "unsupported action for schedule worker"})
		}
		return
	}

	// Handle horizon worker services (horizon-{sitename})
	if strings.HasPrefix(name, "horizon-") {
		siteName := strings.TrimPrefix(name, "horizon-")
		if action == "stop" {
			opErr := cli.HorizonStopForSite(siteName)
			resp := ServiceActionResponse{
				ServiceResponse: ServiceResponse{Name: name, Status: "inactive", EnvVars: map[string]string{}, HorizonSite: siteName},
				OK:              opErr == nil,
			}
			if opErr != nil {
				resp.Error = opErr.Error()
				resp.Status = "active"
			}
			writeJSON(w, resp)
		} else {
			writeJSON(w, ServiceActionResponse{OK: false, Error: "unsupported action for horizon worker"})
		}
		return
	}

	// Handle reverb server services (reverb-{sitename})
	if strings.HasPrefix(name, "reverb-") {
		siteName := strings.TrimPrefix(name, "reverb-")
		if action == "stop" {
			opErr := cli.ReverbStopForSite(siteName)
			resp := ServiceActionResponse{
				ServiceResponse: ServiceResponse{Name: name, Status: "inactive", EnvVars: map[string]string{}, ReverbSite: siteName},
				OK:              opErr == nil,
			}
			if opErr != nil {
				resp.Error = opErr.Error()
				resp.Status = "active"
			}
			writeJSON(w, resp)
		} else {
			writeJSON(w, ServiceActionResponse{OK: false, Error: "unsupported action for reverb server"})
		}
		return
	}

	// Handle custom framework worker services: name is {workerName}-{siteName}.
	// Detect by looking for a matching registered site + framework worker.
	if action == "stop" {
		if reg3, err3 := config.LoadSites(); err3 == nil {
			for _, s := range reg3.Sites {
				if s.Ignored {
					continue
				}
				fwN3 := s.Framework
				if fwN3 == "" {
					fwN3, _ = config.DetectFramework(s.Path)
				}
				fw3, ok3 := config.GetFramework(fwN3)
				if !ok3 || fw3.Workers == nil {
					continue
				}
				for wname := range fw3.Workers {
					switch wname {
					case "queue", "schedule", "reverb":
						continue
					}
					if wname+"-"+s.Name == name {
						opErr := cli.WorkerStopForSite(s.Name, wname)
						resp := ServiceActionResponse{
							ServiceResponse: ServiceResponse{Name: name, Status: "inactive", EnvVars: map[string]string{}, WorkerSite: s.Name, WorkerName: wname},
							OK:              opErr == nil,
						}
						if opErr != nil {
							resp.Error = opErr.Error()
							resp.Status = "active"
						}
						writeJSON(w, resp)
						return
					}
				}
			}
		}
	}

	// Validate service name — built-in or custom
	isBuiltin := false
	for _, s := range knownServices {
		if s == name {
			isBuiltin = true
			break
		}
	}
	var customSvc *config.CustomService
	if !isBuiltin {
		var loadErr error
		customSvc, loadErr = config.LoadCustomService(name)
		if loadErr != nil {
			http.Error(w, "unknown service", http.StatusNotFound)
			return
		}
	}

	unit := "lerd-" + name
	var opErr error

	switch action {
	case "start":
		// Ensure quadlet file exists and systemd knows about it before starting
		var quadletErr error
		if isBuiltin {
			quadletErr = ensureServiceQuadlet(name)
		} else {
			quadletErr = ensureCustomServiceQuadlet(customSvc)
		}
		if quadletErr != nil {
			resp := ServiceActionResponse{
				ServiceResponse: buildServiceResponse(name),
				OK:              false,
				Error:           quadletErr.Error(),
				Logs:            serviceRecentLogs(unit),
			}
			writeJSON(w, resp)
			return
		}
		// Retry to handle Quadlet generator latency after daemon-reload.
		for attempt := range 5 {
			opErr = podman.StartUnit(unit)
			if opErr == nil || !strings.Contains(opErr.Error(), "not found") {
				break
			}
			time.Sleep(time.Duration(attempt+1) * 300 * time.Millisecond)
		}
		if opErr == nil {
			_ = config.SetServicePaused(name, false)
			_ = config.SetServiceManuallyStarted(name, true)
		}
	case "stop":
		opErr = podman.StopUnit(unit)
		if opErr == nil {
			_ = config.SetServicePaused(name, true)
			_ = config.SetServiceManuallyStarted(name, false)
		}
	case "remove":
		if isBuiltin {
			http.Error(w, "cannot remove built-in service", http.StatusForbidden)
			return
		}
		if err := podman.RemoveQuadlet(unit); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		_ = podman.DaemonReload()
		if err := config.RemoveCustomService(name); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true})
		return
	case "pin":
		if opErr = config.SetServicePinned(name, true); opErr == nil {
			status, _ := podman.UnitStatus(unit)
			if status != "active" {
				if isBuiltin {
					_ = ensureServiceQuadlet(name)
				} else {
					_ = ensureCustomServiceQuadlet(customSvc)
				}
				for attempt := range 5 {
					opErr = podman.StartUnit(unit)
					if opErr == nil || !strings.Contains(opErr.Error(), "not found") {
						break
					}
					time.Sleep(time.Duration(attempt+1) * 300 * time.Millisecond)
				}
				if opErr == nil {
					_ = config.SetServicePaused(name, false)
				}
			}
		}
	case "unpin":
		opErr = config.SetServicePinned(name, false)
	default:
		http.NotFound(w, r)
		return
	}

	if opErr != nil {
		writeJSON(w, ServiceActionResponse{
			ServiceResponse: buildServiceResponse(name),
			OK:              false,
			Error:           opErr.Error(),
			Logs:            serviceRecentLogs(unit),
		})
		return
	}

	writeJSON(w, ServiceActionResponse{
		ServiceResponse: buildServiceResponse(name),
		OK:              true,
	})
}

// ensureServiceQuadlet writes the quadlet for a built-in service and reloads systemd.
func ensureServiceQuadlet(name string) error {
	quadletName := "lerd-" + name
	content, err := podman.GetQuadletTemplate(quadletName + ".container")
	if err != nil {
		return fmt.Errorf("unknown service %q", name)
	}
	if err := podman.WriteQuadlet(quadletName, content); err != nil {
		return fmt.Errorf("writing quadlet for %s: %w", name, err)
	}
	return podman.DaemonReload()
}

// ensureCustomServiceQuadlet writes the quadlet for a custom service and reloads systemd.
func ensureCustomServiceQuadlet(svc *config.CustomService) error {
	if svc.DataDir != "" {
		if err := os.MkdirAll(config.DataSubDir(svc.Name), 0755); err != nil {
			return fmt.Errorf("creating data directory for %s: %w", svc.Name, err)
		}
	}
	content := podman.GenerateCustomQuadlet(svc)
	quadletName := "lerd-" + svc.Name
	if err := podman.WriteQuadlet(quadletName, content); err != nil {
		return fmt.Errorf("writing quadlet for %s: %w", svc.Name, err)
	}
	return podman.DaemonReload()
}

// countSitesUsingService counts how many active site .env files reference lerd-{name}.
func countSitesUsingService(name string) int {
	return config.CountSitesUsingService(name)
}

// sitesUsingService returns the domains of active sites whose .env references lerd-{name}.
func sitesUsingService(name string) []string {
	reg, err := config.LoadSites()
	if err != nil {
		return nil
	}
	needle := "lerd-" + name
	var domains []string
	for _, s := range reg.Sites {
		if s.Ignored {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.Path, ".env"))
		if err != nil {
			continue
		}
		if strings.Contains(string(data), needle) {
			domains = append(domains, s.Domain)
		}
	}
	return domains
}

// serviceRecentLogs returns the last 20 lines of journalctl output for a unit.
func serviceRecentLogs(unit string) string {
	cmd := exec.Command("journalctl", "--user", "-u", unit+".service", "-n", "20", "--no-pager", "--output=short")
	out, _ := cmd.CombinedOutput()
	return strings.TrimSpace(string(out))
}

// VersionResponse is the response for GET /api/version.
type VersionResponse struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	HasUpdate bool   `json:"has_update"`
	Changelog string `json:"changelog,omitempty"`
}

func handleVersion(w http.ResponseWriter, _ *http.Request, currentVersion string) {
	info, _ := lerdUpdate.CachedUpdateCheck(currentVersion)
	resp := VersionResponse{Current: currentVersion}
	if info != nil {
		resp.Latest = info.LatestVersion
		resp.HasUpdate = true
		resp.Changelog = info.Changelog
	}
	writeJSON(w, resp)
}

func handlePHPVersions(w http.ResponseWriter, _ *http.Request) {
	versions, _ := phpPkg.ListInstalled()
	if versions == nil {
		versions = []string{}
	}
	writeJSON(w, versions)
}

func handleNodeVersions(w http.ResponseWriter, _ *http.Request) {
	fnmPath := config.BinDir() + "/fnm"
	cmd := exec.Command(fnmPath, "list")
	out, err := cmd.Output()
	if err != nil {
		writeJSON(w, []string{})
		return
	}
	seen := map[string]bool{}
	var versions []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		// fnm list output: "* v20.0.0 default" or "  v18.0.0"
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "* ")
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		v := strings.TrimPrefix(fields[0], "v")
		if v == "" {
			continue
		}
		major := strings.SplitN(v, ".", 2)[0]
		if !seen[major] && strings.Trim(major, "0123456789") == "" {
			seen[major] = true
			versions = append(versions, major)
		}
	}
	writeJSON(w, versions)
}

// SiteActionResponse is returned by POST /api/sites/{domain}/secure|unsecure.
type SiteActionResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func handleSiteAction(w http.ResponseWriter, r *http.Request) {
	// path: /api/sites/{domain}/secure or /api/sites/{domain}/unsecure
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/sites/"), "/")
	if len(parts) != 2 || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	domain, action := parts[0], parts[1]

	site, err := config.FindSiteByDomain(domain)
	if err != nil {
		writeJSON(w, SiteActionResponse{Error: "site not found: " + domain})
		return
	}

	needsReload := false
	switch action {
	case "secure":
		if err := certs.SecureSite(*site); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		site.Secured = true
		envfile.UpdateAppURL(site.Path, "https", site.Domain) //nolint:errcheck
		syncLerdYAMLSecured(site.Path, true)
		needsReload = true
	case "unsecure":
		if err := certs.UnsecureSite(*site); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		site.Secured = false
		envfile.UpdateAppURL(site.Path, "http", site.Domain) //nolint:errcheck
		syncLerdYAMLSecured(site.Path, false)
		needsReload = true
	case "php":
		version := r.URL.Query().Get("version")
		if version == "" {
			writeJSON(w, SiteActionResponse{Error: "version parameter required"})
			return
		}
		// Write .php-version into project directory (keeps CLI php and other tools in sync).
		if err := os.WriteFile(filepath.Join(site.Path, ".php-version"), []byte(version+"\n"), 0644); err != nil {
			writeJSON(w, SiteActionResponse{Error: "writing .php-version: " + err.Error()})
			return
		}
		// Also update .lerd.yaml when it already exists so lerd's priority-1
		// override stays in sync with .php-version.
		if _, statErr := os.Stat(filepath.Join(site.Path, ".lerd.yaml")); statErr == nil {
			proj, _ := config.LoadProjectConfig(site.Path)
			proj.PHPVersion = version
			_ = config.SaveProjectConfig(site.Path, proj)
		}
		site.PHPVersion = version
		// Regenerate vhost with new PHP version
		if site.Secured {
			if err := certs.SecureSite(*site); err != nil {
				writeJSON(w, SiteActionResponse{Error: "regenerating SSL vhost: " + err.Error()})
				return
			}
		} else {
			if err := nginx.GenerateVhost(*site, version); err != nil {
				writeJSON(w, SiteActionResponse{Error: "regenerating vhost: " + err.Error()})
				return
			}
		}
		needsReload = true
	case "node":
		version := r.URL.Query().Get("version")
		if version == "" {
			writeJSON(w, SiteActionResponse{Error: "version parameter required"})
			return
		}
		if err := os.WriteFile(filepath.Join(site.Path, ".node-version"), []byte(version+"\n"), 0644); err != nil {
			writeJSON(w, SiteActionResponse{Error: "writing .node-version: " + err.Error()})
			return
		}
		site.NodeVersion = version
	case "unlink":
		if err := cli.UnlinkSite(site.Name); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "pause":
		if err := cli.PauseSite(site.Name); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "unpause":
		if err := cli.UnpauseSite(site.Name); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "horizon:start":
		phpVersion := site.PHPVersion
		if detected, err := phpPkg.DetectVersion(site.Path); err == nil && detected != "" {
			phpVersion = detected
		}
		go cli.HorizonStartForSite(site.Name, site.Path, phpVersion) //nolint:errcheck
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "horizon:stop":
		if err := cli.HorizonStopForSite(site.Name); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "queue:start":
		phpVersion := site.PHPVersion
		if detected, err := phpPkg.DetectVersion(site.Path); err == nil && detected != "" {
			phpVersion = detected
		}
		go cli.QueueStartForSite(site.Name, site.Path, phpVersion) //nolint:errcheck
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "queue:stop":
		if err := cli.QueueStopForSite(site.Name); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "stripe:start":
		scheme := "http"
		if site.Secured {
			scheme = "https"
		}
		go cli.StripeStartForSite(site.Name, site.Path, scheme+"://"+site.Domain) //nolint:errcheck
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "stripe:stop":
		if err := cli.StripeStopForSite(site.Name); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "schedule:start":
		phpVersion := site.PHPVersion
		if detected, err := phpPkg.DetectVersion(site.Path); err == nil && detected != "" {
			phpVersion = detected
		}
		go cli.ScheduleStartForSite(site.Name, site.Path, phpVersion) //nolint:errcheck
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "schedule:stop":
		if err := cli.ScheduleStopForSite(site.Name); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "reverb:start":
		phpVersion := site.PHPVersion
		if detected, err := phpPkg.DetectVersion(site.Path); err == nil && detected != "" {
			phpVersion = detected
		}
		go cli.ReverbStartForSite(site.Name, site.Path, phpVersion) //nolint:errcheck
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "reverb:stop":
		if err := cli.ReverbStopForSite(site.Name); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "terminal":
		if err := openTerminalAt(site.Path); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	default:
		// Handle framework worker actions: worker:{name}:start or worker:{name}:stop
		if strings.HasPrefix(action, "worker:") {
			parts := strings.SplitN(action, ":", 3)
			if len(parts) == 3 && (parts[2] == "start" || parts[2] == "stop") {
				workerName := parts[1]
				fwN := site.Framework
				if fwN == "" {
					fwN, _ = config.DetectFramework(site.Path)
				}
				fw, ok := config.GetFramework(fwN)
				if !ok || fw.Workers == nil {
					writeJSON(w, SiteActionResponse{Error: "framework has no workers defined"})
					return
				}
				worker, ok := fw.Workers[workerName]
				if !ok {
					writeJSON(w, SiteActionResponse{Error: "worker " + workerName + " not defined for this framework"})
					return
				}
				phpVersion := site.PHPVersion
				if detected, err := phpPkg.DetectVersion(site.Path); err == nil && detected != "" {
					phpVersion = detected
				}
				if parts[2] == "start" {
					go cli.WorkerStartForSite(site.Name, site.Path, phpVersion, workerName, worker) //nolint:errcheck
				} else {
					if err := cli.WorkerStopForSite(site.Name, workerName); err != nil {
						writeJSON(w, SiteActionResponse{Error: err.Error()})
						return
					}
				}
				writeJSON(w, SiteActionResponse{OK: true})
				return
			}
		}
		http.NotFound(w, r)
		return
	}

	if err := config.AddSite(*site); err != nil {
		writeJSON(w, SiteActionResponse{Error: "updating site registry: " + err.Error()})
		return
	}
	if needsReload {
		if err := nginx.Reload(); err != nil {
			writeJSON(w, SiteActionResponse{Error: "reloading nginx: " + err.Error()})
			return
		}
	}
	writeJSON(w, SiteActionResponse{OK: true})
}

func handlePHPVersionAction(w http.ResponseWriter, r *http.Request) {
	// path: /api/php-versions/{version}/{remove|set-default}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/php-versions/"), "/")
	if len(parts) != 2 || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	version, action := parts[0], parts[1]
	if !validVersion.MatchString(version) {
		http.NotFound(w, r)
		return
	}
	switch action {
	case "set-default":
		cfg, err := config.LoadGlobal()
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		cfg.PHP.DefaultVersion = version
		if err := config.SaveGlobal(cfg); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "php_default": version})
	case "remove":
		short := strings.ReplaceAll(version, ".", "")
		unit := "lerd-php" + short + "-fpm"
		_ = podman.StopUnit(unit)
		if err := podman.RemoveQuadlet(unit); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		_ = podman.DaemonReload()
		writeJSON(w, map[string]any{"ok": true})
	default:
		http.NotFound(w, r)
	}
}

func handleNodeVersionAction(w http.ResponseWriter, r *http.Request) {
	// path: /api/node-versions/{version}/{remove|set-default}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/node-versions/"), "/")
	if len(parts) != 2 || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	version, action := parts[0], parts[1]
	if !validVersion.MatchString(version) {
		http.NotFound(w, r)
		return
	}
	switch action {
	case "set-default":
		fnmPath := config.BinDir() + "/fnm"
		if out, err := exec.Command(fnmPath, "default", version).CombinedOutput(); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": strings.TrimSpace(string(out))})
			return
		}
		cfg, err := config.LoadGlobal()
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		cfg.Node.DefaultVersion = version
		if err := config.SaveGlobal(cfg); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "node_default": version})
	case "remove":
		fnmPath := config.BinDir() + "/fnm"
		// Collect all full versions that belong to this major
		listOut, _ := exec.Command(fnmPath, "list").Output()
		var toRemove []string
		for _, line := range strings.Split(strings.TrimSpace(string(listOut)), "\n") {
			line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "* "))
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			v := strings.TrimPrefix(fields[0], "v")
			if strings.SplitN(v, ".", 2)[0] == version {
				toRemove = append(toRemove, v)
			}
		}
		var lastErr error
		for _, v := range toRemove {
			out, err := exec.Command(fnmPath, "uninstall", v).CombinedOutput()
			if err != nil {
				lastErr = fmt.Errorf("fnm uninstall %s: %s", v, strings.TrimSpace(string(out)))
			}
		}
		if lastErr != nil {
			writeJSON(w, map[string]any{"ok": false, "error": lastErr.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	default:
		http.NotFound(w, r)
	}
}

var validVersion = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*$`)

func handleInstallNodeVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	version := r.URL.Query().Get("version")
	if version == "" || !validVersion.MatchString(version) {
		writeJSON(w, map[string]any{"ok": false, "error": "invalid version"})
		return
	}
	major := strings.SplitN(version, ".", 2)[0]
	fnmPath := config.BinDir() + "/fnm"
	cmd := exec.Command(fnmPath, "install", major)
	out, err := cmd.CombinedOutput()
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": strings.TrimSpace(string(out))})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// allowedContainer validates that a container name is a known lerd container.
var allowedContainer = regexp.MustCompile(`^lerd-[a-z0-9-]+$`)

func handleLogs(w http.ResponseWriter, r *http.Request) {
	container := strings.TrimPrefix(r.URL.Path, "/api/logs/")
	if !allowedContainer.MatchString(container) {
		http.Error(w, "unknown container", http.StatusNotFound)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // tell nginx not to buffer

	if exists, _ := podman.ContainerExists(container); !exists {
		fmt.Fprintf(w, "data: container %s is not running\n\n", container)
		flusher.Flush()
		return
	}

	tail := "100"
	if r.Header.Get("Last-Event-ID") != "" {
		tail = "0"
	}

	pr, pw := io.Pipe()
	cmd := exec.CommandContext(r.Context(), "podman", "logs", "-f", "--tail", tail, container)
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(w, "data: error starting logs: %s\n\n", err.Error())
		flusher.Flush()
		return
	}

	go func() {
		cmd.Wait() //nolint:errcheck
		pw.Close()
	}()

	var lineID int
	scanner := bufio.NewScanner(pr)
	for scanner.Scan() {
		line := scanner.Text()
		// Escape backslashes and encode as a single SSE data line.
		escaped := strings.ReplaceAll(line, "\\", "\\\\")
		lineID++
		fmt.Fprintf(w, "id: %d\ndata: %s\n\n", lineID, escaped)
		flusher.Flush()
		if r.Context().Err() != nil {
			break
		}
	}
	if cmd.Process != nil {
		cmd.Process.Kill() //nolint:errcheck
	}
}

var allowedQueueUnit = regexp.MustCompile(`^[a-z0-9-]+$`)

func handleHorizonLogs(w http.ResponseWriter, r *http.Request) {
	// path: /api/horizon/<sitename>/logs
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/horizon/"), "/")
	if len(parts) != 2 || parts[1] != "logs" || !allowedQueueUnit.MatchString(parts[0]) {
		http.NotFound(w, r)
		return
	}
	streamUnitLogs(w, r, "lerd-horizon-"+parts[0])
}

func handleQueueLogs(w http.ResponseWriter, r *http.Request) {
	// path: /api/queue/<sitename>/logs
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/queue/"), "/")
	if len(parts) != 2 || parts[1] != "logs" || !allowedQueueUnit.MatchString(parts[0]) {
		http.NotFound(w, r)
		return
	}
	streamUnitLogs(w, r, "lerd-queue-"+parts[0])
}

// SettingsResponse is the response for GET /api/settings.
type SettingsResponse struct {
	AutostartOnLogin bool `json:"autostart_on_login"`
}

func handleSettings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, SettingsResponse{
		AutostartOnLogin: lerdSystemd.IsServiceEnabled("lerd-autostart"),
	})
}

func handleSettingsAutostart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	if body.Enabled {
		content, err := lerdSystemd.GetUnit("lerd-autostart")
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if err := lerdSystemd.WriteService("lerd-autostart", content); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if err := lerdSystemd.EnableService("lerd-autostart"); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	} else {
		if err := lerdSystemd.DisableService("lerd-autostart"); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	}
	writeJSON(w, map[string]any{"ok": true, "autostart_on_login": body.Enabled})
}

func handleLerdStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := cli.RunStart(); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func handleLerdStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := cli.RunStop(); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func handleLerdQuit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Respond before quitting so the browser receives the response.
	writeJSON(w, map[string]any{"ok": true})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go cli.RunQuit() //nolint:errcheck
}

func handleXdebugAction(w http.ResponseWriter, r *http.Request) {
	// path: /api/xdebug/{version}/on or /api/xdebug/{version}/off
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/xdebug/"), "/")
	if len(parts) != 2 || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	version, action := parts[0], parts[1]
	if !validVersion.MatchString(version) || (action != "on" && action != "off") {
		http.NotFound(w, r)
		return
	}
	enable := action == "on"

	cfg, err := config.LoadGlobal()
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	if cfg.IsXdebugEnabled(version) == enable {
		writeJSON(w, map[string]any{"ok": true, "xdebug_enabled": enable})
		return
	}

	cfg.SetXdebug(version, enable)
	if err := config.SaveGlobal(cfg); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": "saving config: " + err.Error()})
		return
	}

	if err := podman.WriteXdebugIni(version, enable); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": "writing xdebug ini: " + err.Error()})
		return
	}

	// Update quadlet (adds volume mount if not already present).
	if err := podman.WriteFPMQuadlet(version); err != nil {
		fmt.Printf("[WARN] updating quadlet for PHP %s: %v\n", version, err)
	}

	short := strings.ReplaceAll(version, ".", "")
	unit := "lerd-php" + short + "-fpm"
	if err := podman.RestartUnit(unit); err != nil {
		fmt.Printf("[WARN] restart %s: %v\n", unit, err)
	}

	writeJSON(w, map[string]any{"ok": true, "xdebug_enabled": enable})
}

func handleScheduleLogs(w http.ResponseWriter, r *http.Request) {
	// path: /api/schedule/<sitename>/logs
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/schedule/"), "/")
	if len(parts) != 2 || parts[1] != "logs" || !allowedQueueUnit.MatchString(parts[0]) {
		http.NotFound(w, r)
		return
	}
	streamUnitLogs(w, r, "lerd-schedule-"+parts[0])
}

func handleReverbLogs(w http.ResponseWriter, r *http.Request) {
	// path: /api/reverb/<sitename>/logs
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/reverb/"), "/")
	if len(parts) != 2 || parts[1] != "logs" || !allowedQueueUnit.MatchString(parts[0]) {
		http.NotFound(w, r)
		return
	}
	streamUnitLogs(w, r, "lerd-reverb-"+parts[0])
}

func handleWorkerLogs(w http.ResponseWriter, r *http.Request) {
	// path: /api/worker/<sitename>/<workername>/logs
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/worker/"), "/")
	if len(parts) != 3 || parts[2] != "logs" || !allowedQueueUnit.MatchString(parts[0]) || !allowedQueueUnit.MatchString(parts[1]) {
		http.NotFound(w, r)
		return
	}
	// unit: lerd-{workerName}-{siteName}
	streamUnitLogs(w, r, "lerd-"+parts[1]+"-"+parts[0])
}

func handleStripeLogs(w http.ResponseWriter, r *http.Request) {
	// path: /api/stripe/<sitename>/logs
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/stripe/"), "/")
	if len(parts) != 2 || parts[1] != "logs" || !allowedQueueUnit.MatchString(parts[0]) {
		http.NotFound(w, r)
		return
	}
	streamUnitLogs(w, r, "lerd-stripe-"+parts[0])
}

func handleWatcherStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := lerdSystemd.StartService("lerd-watcher"); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func handleWatcherLogs(w http.ResponseWriter, r *http.Request) {
	streamUnitLogs(w, r, "lerd-watcher")
}

func streamUnitLogs(w http.ResponseWriter, r *http.Request, unit string) {

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	cursor := r.Header.Get("Last-Event-ID")
	args := []string{"--user", "-u", unit, "-f", "--no-pager", "--output=json"}
	if cursor != "" {
		args = append(args, "--after-cursor="+cursor)
	} else {
		args = append(args, "-n", "100")
	}

	pr, pw := io.Pipe()
	cmd := exec.CommandContext(r.Context(), "journalctl", args...)
	cmd.Stdout = pw
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(w, "data: error starting logs: %s\n\n", err.Error())
		flusher.Flush()
		return
	}

	go func() {
		cmd.Wait() //nolint:errcheck
		pw.Close()
	}()

	type journalEntry struct {
		Cursor  string          `json:"__CURSOR"`
		Message json.RawMessage `json:"MESSAGE"`
	}

	scanner := bufio.NewScanner(pr)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var entry journalEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		var msg string
		if len(entry.Message) > 0 && entry.Message[0] == '"' {
			json.Unmarshal(entry.Message, &msg) //nolint:errcheck
		} else {
			// journalctl encodes binary messages as a JSON array of bytes
			var b []byte
			if json.Unmarshal(entry.Message, &b) == nil {
				msg = string(b)
			}
		}
		fmt.Fprintf(w, "id: %s\ndata: %s\n\n", entry.Cursor, msg)
		flusher.Flush()
	}
}

// syncLerdYAMLSecured updates the secured field in .lerd.yaml when the file
// already exists, keeping the saved config in sync with UI toggles.
func syncLerdYAMLSecured(projectPath string, secured bool) {
	lerdYAML := filepath.Join(projectPath, ".lerd.yaml")
	if _, err := os.Stat(lerdYAML); err != nil {
		return
	}
	proj, _ := config.LoadProjectConfig(projectPath)
	proj.Secured = secured
	_ = config.SaveProjectConfig(projectPath, proj)
}
