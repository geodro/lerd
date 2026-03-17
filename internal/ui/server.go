package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	_ "embed"

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
		opErr = podman.StartUnit(unit)
	case "stop":
		opErr = podman.StopUnit(unit)
	default:
		http.NotFound(w, r)
		return
	}

	if opErr != nil {
		http.Error(w, opErr.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, buildServiceResponse(name))
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
