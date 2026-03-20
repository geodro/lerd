package ui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "embed"

	"github.com/geodro/lerd/internal/certs"
	"github.com/geodro/lerd/internal/cli"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dns"
	"github.com/geodro/lerd/internal/envfile"
	"github.com/geodro/lerd/internal/nginx"
	nodePkg "github.com/geodro/lerd/internal/node"
	phpPkg "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	lerdSystemd "github.com/geodro/lerd/internal/systemd"
)

//go:embed index.html
var indexHTML []byte

const listenAddr = "0.0.0.0:7073"

var knownServices = []string{"mysql", "redis", "postgres", "meilisearch", "minio", "mailpit", "soketi"}

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
	"minio": {
		"FILESYSTEM_DISK=s3",
		"AWS_ACCESS_KEY_ID=lerd",
		"AWS_SECRET_ACCESS_KEY=lerdpassword",
		"AWS_DEFAULT_REGION=us-east-1",
		"AWS_BUCKET=lerd",
		"AWS_URL=http://lerd-minio:9000",
		"AWS_ENDPOINT=http://lerd-minio:9000",
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
	"soketi": {
		"BROADCAST_CONNECTION=pusher",
		"PUSHER_APP_ID=lerd",
		"PUSHER_APP_KEY=lerd-key",
		"PUSHER_APP_SECRET=lerd-secret",
		"PUSHER_HOST=lerd-soketi",
		"PUSHER_PORT=6001",
		"PUSHER_SCHEME=http",
		"PUSHER_APP_CLUSTER=mt1",
		`VITE_PUSHER_APP_KEY="${PUSHER_APP_KEY}"`,
		`VITE_PUSHER_HOST="${PUSHER_HOST}"`,
		`VITE_PUSHER_PORT="${PUSHER_PORT}"`,
		`VITE_PUSHER_SCHEME="${PUSHER_SCHEME}"`,
		`VITE_PUSHER_APP_CLUSTER="${PUSHER_APP_CLUSTER}"`,
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
	mux.HandleFunc("/api/settings", withCORS(handleSettings))
	mux.HandleFunc("/api/settings/autostart", withCORS(handleSettingsAutostart))
	mux.HandleFunc("/api/xdebug/", withCORS(handleXdebugAction))
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

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

// StatusResponse is the response for GET /api/status.
type StatusResponse struct {
	DNS         DNSStatus    `json:"dns"`
	Nginx       ServiceCheck `json:"nginx"`
	PHPFPMs     []PHPStatus  `json:"php_fpms"`
	PHPDefault  string       `json:"php_default"`
	NodeDefault string       `json:"node_default"`
}

type DNSStatus struct {
	OK  bool   `json:"ok"`
	TLD string `json:"tld"`
}

type ServiceCheck struct {
	Running bool `json:"running"`
}

type PHPStatus struct {
	Version        string `json:"version"`
	Running        bool   `json:"running"`
	XdebugEnabled  bool   `json:"xdebug_enabled"`
}

func handleStatus(w http.ResponseWriter, _ *http.Request) {
	cfg, _ := config.LoadGlobal()
	tld := "test"
	if cfg != nil {
		tld = cfg.DNS.TLD
	}

	dnsOK, _ := dns.Check(tld)
	nginxRunning, _ := podman.ContainerRunning("lerd-nginx")

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
		DNS:         DNSStatus{OK: dnsOK, TLD: tld},
		Nginx:       ServiceCheck{Running: nginxRunning},
		PHPFPMs:     phpStatuses,
		PHPDefault:  phpDefault,
		NodeDefault: nodeDefault,
	})
}

// SiteResponse is the response for GET /api/sites.
type SiteResponse struct {
	Name         string `json:"name"`
	Domain       string `json:"domain"`
	Path         string `json:"path"`
	PHPVersion   string `json:"php_version"`
	NodeVersion  string `json:"node_version"`
	TLS          bool   `json:"tls"`
	FPMRunning   bool   `json:"fpm_running"`
	QueueRunning bool   `json:"queue_running"`
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
		queueStatus, _ := podman.UnitStatus("lerd-queue-" + s.Name)
		sites = append(sites, SiteResponse{
			Name:         s.Name,
			Domain:       s.Domain,
			Path:         s.Path,
			PHPVersion:   phpVersion,
			NodeVersion:  nodeVersion,
			TLS:          s.Secured,
			FPMRunning:   fpmRunning,
			QueueRunning: queueStatus == "active",
		})
	}
	if sites == nil {
		sites = []SiteResponse{}
	}
	writeJSON(w, sites)
}

// ServiceResponse is the response for GET /api/services.
type ServiceResponse struct {
	Name      string            `json:"name"`
	Status    string            `json:"status"`
	EnvVars   map[string]string `json:"env_vars"`
	Dashboard string            `json:"dashboard,omitempty"`
	Custom    bool              `json:"custom,omitempty"`
	SiteCount int               `json:"site_count"`
	QueueSite string            `json:"queue_site,omitempty"`
}

// builtinDashboards maps built-in service names to their dashboard URLs.
var builtinDashboards = map[string]string{
	"mailpit":     "http://localhost:8025",
	"minio":       "http://localhost:9001",
	"meilisearch": "http://localhost:7700",
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
		Name:      name,
		Status:    status,
		EnvVars:   envMap,
		Dashboard: builtinDashboards[name],
		SiteCount: countSitesUsingService(name),
	}
}

// listActiveQueueWorkers returns the site names of active lerd-queue-* systemd units.
func listActiveQueueWorkers() []string {
	out, err := exec.Command("systemctl", "--user", "list-units", "--state=active",
		"--no-legend", "--plain", "lerd-queue-*.service").Output()
	if err != nil {
		return nil
	}
	var sites []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		// line format: "lerd-queue-sitename.service  active running ..."
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		unit := strings.TrimSuffix(fields[0], ".service")
		siteName := strings.TrimPrefix(unit, "lerd-queue-")
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
			Name:      svc.Name,
			Status:    status,
			EnvVars:   envMap,
			Dashboard: svc.Dashboard,
			Custom:    true,
			SiteCount: countSitesUsingService(svc.Name),
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
	case "stop":
		opErr = podman.StopUnit(unit)
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

// countSitesUsingService counts how many site .env files reference lerd-{name}.
func countSitesUsingService(name string) int {
	reg, err := config.LoadSites()
	if err != nil {
		return 0
	}
	needle := "lerd-" + name
	count := 0
	for _, s := range reg.Sites {
		if s.Ignored {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.Path, ".env"))
		if err != nil {
			continue
		}
		if strings.Contains(string(data), needle) {
			count++
		}
	}
	return count
}

// serviceRecentLogs returns the last 20 lines of journalctl output for a unit.
func serviceRecentLogs(unit string) string {
	cmd := exec.Command("journalctl", "--user", "-u", unit+".service", "-n", "20", "--no-pager", "--output=short")
	out, _ := cmd.CombinedOutput()
	return strings.TrimSpace(string(out))
}

// VersionResponse is the response for GET /api/version.
type VersionResponse struct {
	Current string `json:"current"`
	Latest  string `json:"latest"`
	HasUpdate bool `json:"has_update"`
}

func handleVersion(w http.ResponseWriter, _ *http.Request, currentVersion string) {
	latest := fetchLatestRelease()
	hasUpdate := latest != "" && latest != currentVersion && latest != "v"+currentVersion

	writeJSON(w, VersionResponse{
		Current:   currentVersion,
		Latest:    latest,
		HasUpdate: hasUpdate,
	})
}

func fetchLatestRelease() string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/repos/geodro/lerd/releases/latest", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "lerd-ui")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return payload.TagName
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

	switch action {
	case "secure":
		if err := certs.SecureSite(*site); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		site.Secured = true
		envfile.UpdateAppURL(site.Path, "https", site.Domain) //nolint:errcheck
	case "unsecure":
		if err := certs.UnsecureSite(*site); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		site.Secured = false
		envfile.UpdateAppURL(site.Path, "http", site.Domain) //nolint:errcheck
	case "php":
		version := r.URL.Query().Get("version")
		if version == "" {
			writeJSON(w, SiteActionResponse{Error: "version parameter required"})
			return
		}
		// Write .php-version into project directory
		if err := os.WriteFile(filepath.Join(site.Path, ".php-version"), []byte(version+"\n"), 0644); err != nil {
			writeJSON(w, SiteActionResponse{Error: "writing .php-version: " + err.Error()})
			return
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
			if err := nginx.Reload(); err != nil {
				writeJSON(w, SiteActionResponse{Error: "reloading nginx: " + err.Error()})
				return
			}
		}
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
	case "queue:start":
		phpVersion := site.PHPVersion
		if detected, err := phpPkg.DetectVersion(site.Path); err == nil && detected != "" {
			phpVersion = detected
		}
		if err := cli.QueueStartForSite(site.Name, site.Path, phpVersion); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "queue:stop":
		if err := cli.QueueStopForSite(site.Name); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	default:
		http.NotFound(w, r)
		return
	}

	if err := config.AddSite(*site); err != nil {
		writeJSON(w, SiteActionResponse{Error: "updating site registry: " + err.Error()})
		return
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

	pr, pw := io.Pipe()
	cmd := exec.CommandContext(r.Context(), "podman", "logs", "-f", "--tail", "100", container)
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

	scanner := bufio.NewScanner(pr)
	for scanner.Scan() {
		line := scanner.Text()
		// Escape backslashes and encode as a single SSE data line.
		escaped := strings.ReplaceAll(line, "\\", "\\\\")
		fmt.Fprintf(w, "data: %s\n\n", escaped)
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

func handleQueueLogs(w http.ResponseWriter, r *http.Request) {
	// path: /api/queue/<sitename>/logs
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/queue/"), "/")
	if len(parts) != 2 || parts[1] != "logs" || !allowedQueueUnit.MatchString(parts[0]) {
		http.NotFound(w, r)
		return
	}
	unit := "lerd-queue-" + parts[0]

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	pr, pw := io.Pipe()
	cmd := exec.CommandContext(r.Context(), "journalctl", "--user", "-u", unit, "-f", "--no-pager", "-n", "100", "--output=cat")
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

	scanner := bufio.NewScanner(pr)
	for scanner.Scan() {
		line := scanner.Text()
		escaped := strings.ReplaceAll(line, "\\", "\\\\")
		fmt.Fprintf(w, "data: %s\n\n", escaped)
		flusher.Flush()
		if r.Context().Err() != nil {
			break
		}
	}
	if cmd.Process != nil {
		cmd.Process.Kill() //nolint:errcheck
	}
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

