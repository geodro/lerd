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
	"regexp"
	"strings"
	"time"

	_ "embed"

	"github.com/geodro/lerd/internal/certs"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dns"
	phpPkg "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
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
	mux.HandleFunc("/api/update", withCORS(func(w http.ResponseWriter, r *http.Request) {
		handleUpdate(w, r, currentVersion)
	}))
	mux.HandleFunc("/api/sites/", withCORS(handleSiteAction))
	mux.HandleFunc("/api/logs/", withCORS(handleLogs))
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
	DNS     DNSStatus    `json:"dns"`
	Nginx   ServiceCheck `json:"nginx"`
	PHPFPMs []PHPStatus  `json:"php_fpms"`
}

type DNSStatus struct {
	OK  bool   `json:"ok"`
	TLD string `json:"tld"`
}

type ServiceCheck struct {
	Running bool `json:"running"`
}

type PHPStatus struct {
	Version string `json:"version"`
	Running bool   `json:"running"`
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
		phpStatuses = append(phpStatuses, PHPStatus{Version: v, Running: running})
	}

	writeJSON(w, StatusResponse{
		DNS:     DNSStatus{OK: dnsOK, TLD: tld},
		Nginx:   ServiceCheck{Running: nginxRunning},
		PHPFPMs: phpStatuses,
	})
}

// SiteResponse is the response for GET /api/sites.
type SiteResponse struct {
	Domain      string `json:"domain"`
	Path        string `json:"path"`
	PHPVersion  string `json:"php_version"`
	NodeVersion string `json:"node_version"`
	TLS         bool   `json:"tls"`
	FPMRunning  bool   `json:"fpm_running"`
}

func handleSites(w http.ResponseWriter, _ *http.Request) {
	reg, err := config.LoadSites()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var sites []SiteResponse
	for _, s := range reg.Sites {
		fpmRunning := false
		if s.PHPVersion != "" {
			short := strings.ReplaceAll(s.PHPVersion, ".", "")
			fpmRunning, _ = podman.ContainerRunning("lerd-php" + short + "-fpm")
		}
		sites = append(sites, SiteResponse{
			Domain:      s.Domain,
			Path:        s.Path,
			PHPVersion:  s.PHPVersion,
			NodeVersion: s.NodeVersion,
			TLS:         s.Secured,
			FPMRunning:  fpmRunning,
		})
	}
	if sites == nil {
		sites = []SiteResponse{}
	}
	writeJSON(w, sites)
}

// ServiceResponse is the response for GET /api/services.
type ServiceResponse struct {
	Name    string            `json:"name"`
	Status  string            `json:"status"`
	EnvVars map[string]string `json:"env_vars"`
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
		Name:    name,
		Status:  status,
		EnvVars: envMap,
	}
}

func handleServices(w http.ResponseWriter, _ *http.Request) {
	services := make([]ServiceResponse, 0, len(knownServices))
	for _, name := range knownServices {
		services = append(services, buildServiceResponse(name))
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

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validate service name
	valid := false
	for _, s := range knownServices {
		if s == name {
			valid = true
			break
		}
	}
	if !valid {
		http.Error(w, "unknown service", http.StatusNotFound)
		return
	}

	unit := "lerd-" + name
	var opErr error

	switch action {
	case "start":
		// Ensure quadlet file exists and systemd knows about it before starting
		if err := ensureServiceQuadlet(name); err != nil {
			resp := ServiceActionResponse{
				ServiceResponse: buildServiceResponse(name),
				OK:              false,
				Error:           err.Error(),
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

// ensureServiceQuadlet writes the quadlet for a service and reloads systemd.
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

// UpdateResponse is the response for POST /api/update.
type UpdateResponse struct {
	OK     bool   `json:"ok"`
	Output string `json:"output"`
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

	site, err := config.FindSite(domain)
	if err != nil {
		writeJSON(w, SiteActionResponse{Error: "site not found"})
		return
	}

	switch action {
	case "secure":
		if err := certs.SecureSite(site.Domain, site.PHPVersion); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		site.Secured = true
	case "unsecure":
		if err := certs.UnsecureSite(site.Domain, site.PHPVersion); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		site.Secured = false
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

func handleUpdate(w http.ResponseWriter, r *http.Request, _ string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	exe, err := os.Executable()
	if err != nil {
		writeJSON(w, UpdateResponse{OK: false, Output: err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, exe, "update")
	out, err := cmd.CombinedOutput()
	if err != nil {
		writeJSON(w, UpdateResponse{OK: false, Output: string(out)})
		return
	}
	writeJSON(w, UpdateResponse{OK: true, Output: string(out)})
}
