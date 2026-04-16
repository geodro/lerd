package ui

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	_ "embed"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/geodro/lerd/internal/applog"
	"github.com/geodro/lerd/internal/certs"
	"github.com/geodro/lerd/internal/cli"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dns"
	"github.com/geodro/lerd/internal/envfile"
	"github.com/geodro/lerd/internal/eventbus"
	"github.com/geodro/lerd/internal/nginx"
	phpPkg "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/serviceops"
	"github.com/geodro/lerd/internal/services"
	"github.com/geodro/lerd/internal/siteinfo"
	"github.com/geodro/lerd/internal/siteops"
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

// listenAddr is the TCP address lerd-ui binds to. It listens on 0.0.0.0:7073
// so browsers can hit it directly and LAN clients (gated by the remote-control
// middleware) can reach it when lan:expose is on. The gate — not the bind
// address — is the security boundary.
//
// lerd-ui ALSO listens on a unix socket at config.UISocketPath() for the
// lerd.localhost nginx vhost. Bind-mounting a socket into lerd-nginx is more
// reliable than reaching the host over TCP via host.containers.internal,
// which depends on netavark / pasta / rootless routing wiring up 169.254.1.2
// (something that differs across podman versions and breaks silently).
const listenAddr = "0.0.0.0:7073"

// ctxKeyUnixSocket marks HTTP requests that arrived over the unix socket
// listener. These are treated as loopback by isLoopbackRequest since only
// processes with filesystem access to the socket can connect.
type ctxKeyUnixSocket struct{}

var knownServices = siteinfo.KnownServices

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
	// Every unit lifecycle change (from CLI, MCP, HTTP handlers, or the
	// file watcher) funnels through podman.StartUnit/StopUnit/RestartUnit.
	// Hook into that choke point so any mutation — regardless of which
	// surface triggered it — invalidates the systemctl unit cache and
	// pushes a fresh snapshot to every connected browser. The bus debounces,
	// so bursty mutations (e.g. restarting a set of workers) still collapse
	// into one broadcast.
	// Start the container state cache. One background goroutine polls
	// podman ps every N seconds; all hot paths (buildStatus, siteinfo,
	// IsActive) read from the cache instead of spawning per-container
	// podman inspect subprocesses.
	podman.Cache.Start(context.Background())

	// Restart any LAN share proxies that were active before this process started.
	go cli.RestoreLANShareProxies()

	podman.AfterUnitChange = func(name string) {
		// Run the poll and snapshot publish in a goroutine so the HTTP
		// handler (and the unit start/stop call that triggered this) returns
		// immediately. PollNow blocks until podman ps completes, ensuring
		// the snapshot broadcast carries current container states rather than
		// a stale pre-mutation value — which would cause the UI toggle to
		// appear to revert.
		go func() {
			podman.Cache.PollNow()
			siteinfo.InvalidateUnitCache()
			eventbus.Default.Publish(eventbus.KindSites)
			eventbus.Default.Publish(eventbus.KindServices)
			eventbus.Default.Publish(eventbus.KindStatus)
		}()
	}

	// A single goroutine subscribes to the eventbus and invalidates the
	// relevant snapshot on every mutation. The /api/ws handler broadcasts
	// the freshly rebuilt bytes to every connected browser.
	go runSnapshotInvalidator()

	mux := http.NewServeMux()

	mux.HandleFunc("/api/status", withCORS(handleStatus))
	mux.HandleFunc("/api/sites", withCORS(handleSites))
	mux.HandleFunc("/api/services", withCORS(handleServices))
	mux.HandleFunc("/api/ws", handleWS)
	mux.HandleFunc("/api/lan-qr/", withCORS(handleLANQR))

	// Cross-process notifier for CLI/MCP. Loopback-only. PollNow in a
	// goroutine so the handler returns under the CLI's 500 ms POST
	// timeout while the cache refresh drives the next WS broadcast.
	mux.HandleFunc("/api/internal/notify", func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackRequest(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		go func() {
			podman.Cache.PollNow()
			siteinfo.InvalidateUnitCache()
			eventbus.Default.Publish(eventbus.KindSites)
			eventbus.Default.Publish(eventbus.KindServices)
			eventbus.Default.Publish(eventbus.KindStatus)
		}()
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("/api/services/presets", withCORS(handleServicePresets))
	mux.HandleFunc("/api/services/presets/", withCORS(publishAfter(handleServicePresetInstall, eventbus.KindServices, eventbus.KindStatus)))
	mux.HandleFunc("/api/services/", withCORS(publishAfter(handleServiceAction, eventbus.KindServices, eventbus.KindStatus, eventbus.KindSites)))
	mux.HandleFunc("/api/version", withCORS(func(w http.ResponseWriter, r *http.Request) {
		handleVersion(w, r, currentVersion)
	}))
	mux.HandleFunc("/api/php-versions", withCORS(handlePHPVersions))
	mux.HandleFunc("/api/php-versions/", withCORS(publishAfter(handlePHPVersionAction, eventbus.KindStatus, eventbus.KindSites)))
	mux.HandleFunc("/api/node-versions", withCORS(handleNodeVersions))
	mux.HandleFunc("/api/node-versions/install", withCORS(handleInstallNodeVersion))
	mux.HandleFunc("/api/node-versions/", withCORS(handleNodeVersionAction))
	mux.HandleFunc("/api/sites/link", withCORS(publishAfter(handleSiteLink, eventbus.KindSites)))
	mux.HandleFunc("/api/browse", withCORS(handleBrowse))
	mux.HandleFunc("/api/sites/", withCORS(publishAfter(handleSiteAction, eventbus.KindSites, eventbus.KindServices)))
	mux.HandleFunc("/api/logs/", withCORS(handleLogs))
	mux.HandleFunc("/api/queue/", withCORS(handleQueueLogs))
	mux.HandleFunc("/api/horizon/", withCORS(handleHorizonLogs))
	mux.HandleFunc("/api/stripe/", withCORS(handleStripeLogs))
	mux.HandleFunc("/api/schedule/", withCORS(handleScheduleLogs))
	mux.HandleFunc("/api/reverb/", withCORS(handleReverbLogs))
	mux.HandleFunc("/api/worker/", withCORS(handleWorkerLogs))
	mux.HandleFunc("/api/app-logs/", withCORS(handleAppLogs))
	mux.HandleFunc("/api/watcher/logs", withCORS(handleWatcherLogs))
	mux.HandleFunc("/api/watcher/start", withCORS(handleWatcherStart))
	mux.HandleFunc("/api/settings", withCORS(handleSettings))
	mux.HandleFunc("/api/settings/autostart", withCORS(handleSettingsAutostart))
	mux.HandleFunc("/api/xdebug/", withCORS(publishAfter(handleXdebugAction, eventbus.KindStatus)))
	mux.HandleFunc("/api/lerd/start", withCORS(handleLerdStart))
	mux.HandleFunc("/api/lerd/stop", withCORS(handleLerdStop))
	mux.HandleFunc("/api/lerd/quit", withCORS(handleLerdQuit))
	mux.HandleFunc("/api/lerd/update-terminal", withCORS(handleLerdUpdateTerminal))
	mux.HandleFunc("/api/remote-control", withCORS(handleRemoteControl))
	mux.HandleFunc("/api/access-mode", withCORS(handleAccessMode))
	mux.HandleFunc("/api/lan/status", withCORS(handleLANStatus))
	mux.HandleFunc("/api/remote-setup/generate", withCORS(handleRemoteSetupGenerate))
	mux.HandleFunc("/api/remote-setup", handleRemoteSetup) // intentional: no CORS, no withCORS, served as plain script
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
		// The shell embeds the entire JS/CSS inline and changes on
		// every binary update. Without this, browsers cache the HTML
		// indefinitely and keep running stale client code — which hid
		// the WebSocket URL-rewrite fix during local testing.
		w.Header().Set("Cache-Control", "no-store")
		w.Write(indexHTML) //nolint:errcheck
	})

	handler := withRemoteControlGate(mux)

	// Unix socket listener for the lerd.localhost nginx vhost. Linux only:
	// on macOS, lerd-nginx runs inside the podman-machine VM and unix
	// sockets don't traverse virtio-fs as functional sockets, so the
	// vhost falls back to TCP via host.containers.internal there.
	// Errors are non-fatal — direct http://localhost:7073 access still
	// works even if the socket can't be created.
	if runtime.GOOS != "darwin" {
		if err := os.MkdirAll(config.RunDir(), 0755); err != nil {
			fmt.Printf("[WARN] creating %s: %v — lerd.localhost vhost will not work\n", config.RunDir(), err)
		} else {
			sockPath := config.UISocketPath()
			_ = os.Remove(sockPath)
			unixLn, err := net.Listen("unix", sockPath)
			if err != nil {
				fmt.Printf("[WARN] binding %s: %v — lerd.localhost vhost will not work\n", sockPath, err)
			} else {
				if err := os.Chmod(sockPath, 0660); err != nil {
					fmt.Printf("[WARN] chmod %s: %v\n", sockPath, err)
				}
				unixSrv := &http.Server{
					Handler: handler,
					ConnContext: func(ctx context.Context, _ net.Conn) context.Context {
						return context.WithValue(ctx, ctxKeyUnixSocket{}, true)
					},
				}
				go func() {
					fmt.Printf("Lerd UI listening on unix:%s\n", sockPath)
					if err := unixSrv.Serve(unixLn); err != nil && err != http.ErrServerClosed {
						fmt.Printf("[WARN] unix socket server exited: %v\n", err)
					}
				}()
			}
		}
	}

	fmt.Printf("Lerd UI listening on http://%s\n", listenAddr)
	return http.ListenAndServe(listenAddr, handler)
}

var allowedCORSOrigins = map[string]bool{
	"http://lerd.localhost":  true,
	"https://lerd.localhost": true,
	"http://localhost:7073":  true,
	"http://127.0.0.1:7073":  true,
}

func withCORS(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if allowedCORSOrigins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h(w, r)
	}
}

// graphicalSessionKeys lists the env vars a GUI terminal needs to attach to
// the user's compositor. Missing any of these (notably WAYLAND_DISPLAY /
// DISPLAY) causes the spawned terminal to exit silently after fork.
var graphicalSessionKeys = []string{
	"WAYLAND_DISPLAY",
	"DISPLAY",
	"XAUTHORITY",
	"XDG_SESSION_TYPE",
	"XDG_CURRENT_DESKTOP",
	"XDG_RUNTIME_DIR",
	"XDG_DATA_DIRS",
	"DBUS_SESSION_BUS_ADDRESS",
}

// graphicalEnv returns os.Environ() enriched with graphical-session vars
// pulled from the systemd user manager and (as a last resort) probed from
// $XDG_RUNTIME_DIR. When lerd-ui runs as a lingering user service started at
// boot, its own env has no DISPLAY / WAYLAND_DISPLAY, so any GUI child it
// spawns dies on startup. This helper patches that up at spawn time.
//
// Darwin doesn't need this (Terminal/iTerm are launched via `open` which
// reattaches to the user's Aqua session), so the caller should skip it there.
func graphicalEnv() []string {
	env := os.Environ()
	have := map[string]string{}
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i > 0 {
			have[kv[:i]] = kv[i+1:]
		}
	}

	merged := map[string]string{}
	for _, k := range graphicalSessionKeys {
		if v := have[k]; v != "" {
			merged[k] = v
		}
	}

	if out, err := exec.Command("systemctl", "--user", "show-environment").Output(); err == nil {
		sc := bufio.NewScanner(strings.NewReader(string(out)))
		for sc.Scan() {
			line := sc.Text()
			i := strings.IndexByte(line, '=')
			if i <= 0 {
				continue
			}
			k, v := line[:i], line[i+1:]
			for _, want := range graphicalSessionKeys {
				if k == want && merged[k] == "" && v != "" {
					merged[k] = v
				}
			}
		}
	}

	if merged["WAYLAND_DISPLAY"] == "" {
		runtimeDir := merged["XDG_RUNTIME_DIR"]
		if runtimeDir == "" {
			runtimeDir = have["XDG_RUNTIME_DIR"]
		}
		if runtimeDir == "" {
			runtimeDir = fmt.Sprintf("/run/user/%d", os.Getuid())
		}
		if entries, err := os.ReadDir(runtimeDir); err == nil {
			for _, e := range entries {
				name := e.Name()
				if strings.HasPrefix(name, "wayland-") && !strings.HasSuffix(name, ".lock") {
					merged["WAYLAND_DISPLAY"] = name
					if merged["XDG_RUNTIME_DIR"] == "" {
						merged["XDG_RUNTIME_DIR"] = runtimeDir
					}
					break
				}
			}
		}
	}

	out := make([]string, 0, len(env)+len(merged))
	for _, kv := range env {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			out = append(out, kv)
			continue
		}
		if _, overridden := merged[kv[:i]]; overridden {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range merged {
		out = append(out, k+"="+v)
	}
	return out
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
		termCmd{"ptyxis", []string{"--working-directory", dir}},
		termCmd{"konsole", []string{"--separate", "--workdir", dir}},
		termCmd{"gnome-terminal", []string{"--working-directory", dir}},
		termCmd{"xfce4-terminal", []string{"--working-directory", dir}},
		termCmd{"tilix", []string{"--working-directory", dir}},
		termCmd{"terminator", []string{"--working-directory", dir}},
		termCmd{"xterm", []string{"-e", "sh", "-c", `cd "$0" && exec "$SHELL"`, dir}},
	)

	if runtime.GOOS == "darwin" {
		// `open -a Terminal dir` opens a new window at dir without echoing any
		// command — cleaner than `do script "cd ... && exec $SHELL"` which types
		// the command visibly into the shell. iTerm2 supports the same via open.
		if _, err := os.Stat("/Applications/iTerm.app"); err == nil {
			candidates = append(candidates, termCmd{"open", []string{"-a", "iTerm", dir}})
		}
		candidates = append(candidates, termCmd{"open", []string{"-a", "Terminal", dir}})
	}

	for _, t := range candidates {
		bin, err := exec.LookPath(t.bin)
		if err != nil {
			continue
		}
		args := t.args
		// For $TERMINAL with no preset args, just pass the dir via cd wrapper
		if t.bin == os.Getenv("TERMINAL") && len(args) == 0 {
			args = []string{"-e", "sh", "-c", `cd "$0" && exec "$SHELL"`, dir}
		}
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		if runtime.GOOS != "darwin" {
			cmd.Env = graphicalEnv()
		}
		if err := cmd.Start(); err != nil {
			return err
		}
		// Reap the child so we don't leave a zombie behind for every click.
		go func() { _ = cmd.Wait() }()
		return nil
	}
	return fmt.Errorf("no terminal emulator found; set $TERMINAL or install kitty, foot, alacritty, wezterm, ghostty, ptyxis, konsole, or gnome-terminal")
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v) //nolint:errcheck
	return string(b)
}

// StatusResponse is the response for GET /api/status.
type StatusResponse struct {
	DNS               DNSStatus    `json:"dns"`
	Nginx             ServiceCheck `json:"nginx"`
	PHPFPMs           []PHPStatus  `json:"php_fpms"`
	PHPDefault        string       `json:"php_default"`
	NodeDefault       string       `json:"node_default"`
	NodeManagedByLerd bool         `json:"node_managed_by_lerd"`
	WatcherRunning    bool         `json:"watcher_running"`
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
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(snapshots.Status())
}

func buildStatus() StatusResponse {
	cfg, _ := config.LoadGlobal()
	tld := "test"
	if cfg != nil {
		tld = cfg.DNS.TLD
	}

	dnsOK, _ := dns.Check(tld)
	nginxRunning := podman.Cache.Running("lerd-nginx")
	watcherRunning := services.Mgr.IsActive("lerd-watcher")

	versions, _ := phpPkg.ListInstalled()
	var phpStatuses []PHPStatus
	for _, v := range versions {
		short := strings.ReplaceAll(v, ".", "")
		running := podman.Cache.Running("lerd-php" + short + "-fpm")
		xdebugEnabled := cfg != nil && cfg.IsXdebugEnabled(v)
		phpStatuses = append(phpStatuses, PHPStatus{Version: v, Running: running, XdebugEnabled: xdebugEnabled})
	}

	phpDefault := ""
	nodeDefault := ""
	if cfg != nil {
		phpDefault = cfg.PHP.DefaultVersion
		nodeDefault = cfg.Node.DefaultVersion
	}
	nodeShim := filepath.Join(config.BinDir(), "node")
	_, nodeShimErr := os.Stat(nodeShim)
	nodeManagedByLerd := nodeShimErr == nil
	return StatusResponse{
		DNS:               DNSStatus{OK: dnsOK, TLD: tld},
		Nginx:             ServiceCheck{Running: nginxRunning},
		PHPFPMs:           phpStatuses,
		PHPDefault:        phpDefault,
		NodeDefault:       nodeDefault,
		NodeManagedByLerd: nodeManagedByLerd,
		WatcherRunning:    watcherRunning,
	}
}

func buildStatusJSON() []byte { return []byte(mustJSON(buildStatus())) }

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
	Failing bool   `json:"failing,omitempty"`
}

// ConflictingDomain describes a domain declared in .lerd.yaml that wasn't
// registered for the site because another site on this machine already owns
// it. Surfaced to the UI so the domain modal can render a warning icon next
// to the entry with the owning site name.
type ConflictingDomain struct {
	Domain  string `json:"domain"`
	OwnedBy string `json:"owned_by"`
}

// SiteResponse is the response for GET /api/sites.
type SiteResponse struct {
	Name               string              `json:"name"`
	Domain             string              `json:"domain"`
	Domains            []string            `json:"domains"`
	ConflictingDomains []ConflictingDomain `json:"conflicting_domains,omitempty"`
	Path               string              `json:"path"`
	PHPVersion         string              `json:"php_version"`
	NodeVersion        string              `json:"node_version"`
	TLS                bool                `json:"tls"`
	Framework          string              `json:"framework"`
	FPMRunning         bool                `json:"fpm_running"`
	IsLaravel          bool                `json:"is_laravel"`
	FrameworkLabel     string              `json:"framework_label"`
	QueueRunning       bool                `json:"queue_running"`
	QueueFailing       bool                `json:"queue_failing,omitempty"`
	StripeRunning      bool                `json:"stripe_running"`
	StripeSecretSet    bool                `json:"stripe_secret_set"`
	ScheduleRunning    bool                `json:"schedule_running"`
	ScheduleFailing    bool                `json:"schedule_failing,omitempty"`
	ReverbRunning      bool                `json:"reverb_running"`
	ReverbFailing      bool                `json:"reverb_failing,omitempty"`
	HasReverb          bool                `json:"has_reverb"`
	HasHorizon         bool                `json:"has_horizon"`
	HorizonRunning     bool                `json:"horizon_running"`
	HorizonFailing     bool                `json:"horizon_failing,omitempty"`
	HasQueueWorker     bool                `json:"has_queue_worker"`
	HasScheduleWorker  bool                `json:"has_schedule_worker"`
	FrameworkWorkers   []WorkerStatus      `json:"framework_workers,omitempty"`
	HasAppLogs         bool                `json:"has_app_logs"`
	LatestLogTime      string              `json:"latest_log_time,omitempty"`
	HasFavicon         bool                `json:"has_favicon"`
	Paused             bool                `json:"paused"`
	Branch             string              `json:"branch"`
	Worktrees          []WorktreeResponse  `json:"worktrees"`
	// Services lists the service names this site uses, sourced from the
	// project's .lerd.yaml. Used by the dashboard to render service badges
	// on the site detail panel.
	Services        []string `json:"services,omitempty"`
	LANPort         int      `json:"lan_port,omitempty"`
	LANShareURL     string   `json:"lan_share_url,omitempty"`
	CustomContainer bool     `json:"custom_container,omitempty"`
	ContainerPort   int      `json:"container_port,omitempty"`
	ContainerImage  string   `json:"container_image,omitempty"`
}

func handleSites(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(snapshots.Sites())
}

func buildSitesJSON() []byte { return []byte(mustJSON(buildSites())) }

func buildSites() []SiteResponse {
	enriched, err := siteinfo.LoadAll(siteinfo.EnrichUI)
	if err != nil {
		return []SiteResponse{}
	}
	_ = siteinfo.PersistVersionChanges(enriched)

	sites := make([]SiteResponse, 0, len(enriched))
	for _, e := range enriched {
		var fwWorkers []WorkerStatus
		for _, fw := range e.FrameworkWorkers {
			fwWorkers = append(fwWorkers, WorkerStatus{
				Name:    fw.Name,
				Label:   fw.Label,
				Running: fw.Running,
				Failing: fw.Failing,
			})
		}

		var conflicting []ConflictingDomain
		for _, cd := range e.ConflictingDomains {
			conflicting = append(conflicting, ConflictingDomain{
				Domain:  cd.Domain,
				OwnedBy: cd.OwnedBy,
			})
		}

		var worktreeResponses []WorktreeResponse
		for _, wt := range e.Worktrees {
			worktreeResponses = append(worktreeResponses, WorktreeResponse{
				Branch: wt.Branch,
				Domain: wt.Domain,
				Path:   wt.Path,
			})
		}
		if worktreeResponses == nil {
			worktreeResponses = []WorktreeResponse{}
		}

		sites = append(sites, SiteResponse{
			Name:               e.Name,
			Domain:             e.PrimaryDomain(),
			Domains:            e.Domains,
			ConflictingDomains: conflicting,
			Path:               e.Path,
			PHPVersion:         e.PHPVersion,
			NodeVersion:        e.NodeVersion,
			TLS:                e.Secured,
			Framework:          e.FrameworkName,
			IsLaravel:          e.FrameworkName == "laravel",
			FrameworkLabel:     e.FrameworkLabel,
			FPMRunning:         e.FPMRunning,
			QueueRunning:       e.QueueRunning,
			QueueFailing:       e.QueueFailing,
			StripeRunning:      e.StripeRunning,
			StripeSecretSet:    e.StripeSecretSet,
			ScheduleRunning:    e.ScheduleRunning,
			ScheduleFailing:    e.ScheduleFailing,
			ReverbRunning:      e.ReverbRunning,
			ReverbFailing:      e.ReverbFailing,
			HasReverb:          e.HasReverb,
			HasHorizon:         e.HasHorizon,
			HorizonRunning:     e.HorizonRunning,
			HorizonFailing:     e.HorizonFailing,
			HasQueueWorker:     e.HasQueueWorker,
			HasScheduleWorker:  e.HasScheduleWorker,
			FrameworkWorkers:   fwWorkers,
			HasAppLogs:         e.HasAppLogs,
			LatestLogTime:      e.LatestLogTime,
			HasFavicon:         e.HasFavicon,
			Paused:             e.Paused,
			Branch:             e.Branch,
			Worktrees:          worktreeResponses,
			Services:           e.Services,
			LANPort:            e.LANPort,
			LANShareURL:        cli.LANShareURL(e.LANPort),
			CustomContainer:    e.ContainerPort > 0,
			ContainerPort:      e.ContainerPort,
			ContainerImage:     e.ContainerImage,
		})
	}
	return sites
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
	Paused             bool              `json:"paused,omitempty"`
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
	"rustfs":      "http://localhost:9001/rustfs/console/",
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
		SiteCount:     countSitesUsingService(name),
		SiteDomains:   sitesUsingService(name),
		Pinned:        config.ServiceIsPinned(name),
		Paused:        config.ServiceIsPaused(name),
	}
}

// listActiveQueueWorkers returns the site names of active lerd-queue-* systemd units.
func listActiveQueueWorkers() []string {
	return listActiveUnitsBySuffix("lerd-queue-*.service", "lerd-queue-")
}

// listActiveScheduleWorkers returns site names of active lerd-schedule-* units.
// Includes timer-driven schedulers whose .service is static between firings.
func listActiveScheduleWorkers() []string {
	svc := listActiveUnitsBySuffix("lerd-schedule-*.service", "lerd-schedule-")
	timer := listActiveUnitsBySuffix("lerd-schedule-*.timer", "lerd-schedule-")
	seen := map[string]bool{}
	out := make([]string, 0, len(svc)+len(timer))
	for _, n := range append(svc, timer...) {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// listActiveReverbServers returns site names of active lerd-reverb-* units.
func listActiveReverbServers() []string {
	return listActiveUnitsBySuffix("lerd-reverb-*.service", "lerd-reverb-")
}

// listActiveHorizonWorkers returns site names of active lerd-horizon-* units.
func listActiveHorizonWorkers() []string {
	return listActiveUnitsBySuffix("lerd-horizon-*.service", "lerd-horizon-")
}

func handleServices(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(snapshots.Services())
}

func buildServicesJSON() []byte { return []byte(mustJSON(buildServicesList())) }

func buildServicesList() []ServiceResponse {
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
			Name:          svc.Name,
			Status:        status,
			EnvVars:       envMap,
			Dashboard:     svc.Dashboard,
			ConnectionURL: svc.ConnectionURL,
			Custom:        true,
			SiteCount:     countSitesUsingService(svc.Name),
			SiteDomains:   sitesUsingService(svc.Name),
			Pinned:        config.ServiceIsPinned(svc.Name),
			Paused:        config.ServiceIsPaused(svc.Name),
			DependsOn:     svc.DependsOn,
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
	return services
}

// PresetResponse describes a bundled service preset for the web UI.
type PresetResponse struct {
	Name           string                 `json:"name"`
	Description    string                 `json:"description"`
	Image          string                 `json:"image,omitempty"`
	Dashboard      string                 `json:"dashboard,omitempty"`
	DependsOn      []string               `json:"depends_on,omitempty"`
	MissingDeps    []string               `json:"missing_deps,omitempty"`
	Installed      bool                   `json:"installed"`
	Versions       []config.PresetVersion `json:"versions,omitempty"`
	DefaultVersion string                 `json:"default_version,omitempty"`
	InstalledTags  []string               `json:"installed_tags,omitempty"`
}

// handleServicePresets returns the list of bundled presets and whether each is
// already installed as a custom service.
func handleServicePresets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	presets, err := config.ListPresets()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]PresetResponse, 0, len(presets))
	for _, p := range presets {
		var missing []string
		var resolvedSvc *config.CustomService
		if loaded, err := config.LoadPreset(p.Name); err == nil {
			if svc, rerr := loaded.Resolve(""); rerr == nil {
				resolvedSvc = svc
				missing = cli.MissingPresetDependencies(svc)
			}
		}
		// For single-version presets installed reflects "is the canonical
		// service installed". For multi-version presets it reflects "are any
		// version-suffixed instances installed", and InstalledTags lists them.
		installed := false
		var installedTags []string
		if len(p.Versions) == 0 {
			if _, err := config.LoadCustomService(p.Name); err == nil {
				installed = true
			}
		} else {
			for _, v := range p.Versions {
				name := p.Name + "-" + config.SanitizeImageTag(v.Tag)
				if _, err := config.LoadCustomService(name); err == nil {
					installed = true
					installedTags = append(installedTags, v.Tag)
				}
			}
		}
		image := p.Image
		if resolvedSvc != nil && len(p.Versions) == 0 {
			image = resolvedSvc.Image
		}
		out = append(out, PresetResponse{
			Name:           p.Name,
			Description:    p.Description,
			Image:          image,
			Dashboard:      p.Dashboard,
			DependsOn:      p.DependsOn,
			MissingDeps:    missing,
			Installed:      installed,
			Versions:       p.Versions,
			DefaultVersion: p.DefaultVersion,
			InstalledTags:  installedTags,
		})
	}
	writeJSON(w, out)
}

// handleServicePresetInstall installs a bundled preset by name.
func handleServicePresetInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/services/presets/")
	name = strings.Trim(name, "/")
	if name == "" || strings.Contains(name, "/") {
		http.NotFound(w, r)
		return
	}
	version := r.URL.Query().Get("version")
	svc, err := cli.InstallPresetByName(name, version)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{
		"ok":         true,
		"name":       svc.Name,
		"dashboard":  svc.Dashboard,
		"depends_on": svc.DependsOn,
	})
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

	// If the name matches a registered custom service, skip the prefix-based
	// per-site routes below — otherwise a custom service named e.g. "stripe-mock"
	// would be routed as a per-site stripe listener and fail with
	// "unsupported action for stripe listener".
	_, customLoadErr := config.LoadCustomService(name)
	isCustom := customLoadErr == nil

	// Handle queue worker services (queue-{sitename})
	if !isCustom && strings.HasPrefix(name, "queue-") {
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
	if !isCustom && strings.HasPrefix(name, "stripe-") {
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
	if !isCustom && strings.HasPrefix(name, "schedule-") {
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
	if !isCustom && strings.HasPrefix(name, "horizon-") {
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
	if !isCustom && strings.HasPrefix(name, "reverb-") {
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
		// Bring every declared dependency up first. Without this, starting
		// mongo-express from the dashboard would leave mongo stopped and the
		// container would fail to connect.
		if !isBuiltin {
			if depErr := cli.StartServiceDependencies(customSvc); depErr != nil {
				resp := ServiceActionResponse{
					ServiceResponse: buildServiceResponse(name),
					OK:              false,
					Error:           depErr.Error(),
					Logs:            serviceRecentLogs(unit),
				}
				writeJSON(w, resp)
				return
			}
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
			cli.RegenerateFamilyConsumersForService(name)
		}
	case "stop":
		// Stop any custom services that depend on this one before stopping
		// it, mirroring the CLI's `lerd service stop` behaviour. Otherwise
		// stopping mysql leaves phpmyadmin running with a dead backend (and
		// the same for postgres+pgadmin, mongo+mongo-express).
		cli.StopServiceAndDependents(name)
		// Cover the parent itself in case the recursive helper short-circuited
		// (e.g. unit was reported inactive but the user explicitly clicked stop).
		opErr = podman.StopUnit(unit)
		if opErr == nil {
			_ = config.SetServicePaused(name, true)
			_ = config.SetServiceManuallyStarted(name, false)
			cli.RegenerateFamilyConsumersForService(name)
		}
	case "remove":
		if isBuiltin {
			http.Error(w, "cannot remove built-in service", http.StatusForbidden)
			return
		}
		_ = podman.StopUnit(unit)
		podman.RemoveContainer(unit)
		if err := podman.RemoveQuadlet(unit); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		_ = podman.DaemonReloadFn()
		if err := config.RemoveCustomService(name); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		cli.RegenerateFamilyConsumersForService(name)
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

// ensureServiceQuadlet writes the unit file for a built-in service and reloads the service manager.
func ensureServiceQuadlet(name string) error {
	quadletName := "lerd-" + name
	content, err := podman.GetQuadletTemplate(quadletName + ".container")
	if err != nil {
		return fmt.Errorf("unknown service %q", name)
	}
	if err := podman.WriteContainerUnitFn(quadletName, content); err != nil {
		return fmt.Errorf("writing unit for %s: %w", name, err)
	}
	return podman.DaemonReloadFn()
}

// ensureCustomServiceQuadlet writes the quadlet for a custom service and reloads systemd.
func ensureCustomServiceQuadlet(svc *config.CustomService) error {
	return serviceops.EnsureCustomServiceQuadlet(svc)
}

// countSitesUsingService counts how many active site .env files reference lerd-{name}.
func countSitesUsingService(name string) int {
	return config.CountSitesUsingService(name)
}

// sitesUsingService returns the domains of active sites that use the named service.
// Checks both .lerd.yaml services list and .env file references.
func sitesUsingService(name string) []string {
	reg, err := config.LoadSites()
	if err != nil {
		return nil
	}
	needle := "lerd-" + name
	var domains []string
	for _, s := range reg.Sites {
		if s.Ignored || s.Paused {
			continue
		}
		// Check .lerd.yaml services list first.
		if proj, pErr := config.LoadProjectConfig(s.Path); pErr == nil {
			found := false
			for _, svc := range proj.Services {
				if svc.Name == name {
					found = true
					break
				}
			}
			if found {
				domains = append(domains, s.PrimaryDomain())
				continue
			}
		}
		// Fall back to .env scanning.
		data, err := os.ReadFile(filepath.Join(s.Path, ".env"))
		if err != nil {
			continue
		}
		if strings.Contains(string(data), needle) {
			domains = append(domains, s.PrimaryDomain())
		}
	}
	return domains
}

// serviceRecentLogs is implemented per-platform in logs_linux.go / logs_darwin.go.

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

func handleSiteFavicon(w http.ResponseWriter, r *http.Request) {
	// path: /api/sites/{domain}/favicon
	domain := strings.TrimPrefix(r.URL.Path, "/api/sites/")
	domain = strings.TrimSuffix(domain, "/favicon")

	site, err := config.FindSiteByDomain(domain)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	path := siteinfo.DetectFavicon(site.Path, site.PublicDir, site.Framework, nil, false)
	if path == "" {
		http.NotFound(w, r)
		return
	}

	http.ServeFile(w, r, path)
}

// handleLANQR serves a QR code PNG for the LAN share URL of a site.
// Path: /api/lan-qr/{domain}
func handleLANQR(w http.ResponseWriter, r *http.Request) {
	domain := strings.TrimPrefix(r.URL.Path, "/api/lan-qr/")
	site, err := config.FindSiteByDomain(domain)
	if err != nil || site.LANPort == 0 {
		http.NotFound(w, r)
		return
	}
	shareURL := cli.LANShareURL(site.LANPort)
	if shareURL == "" {
		http.NotFound(w, r)
		return
	}
	png, err := qrcode.Encode(shareURL, qrcode.Medium, 160)
	if err != nil {
		http.Error(w, "qr encode: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "qr.png", time.Time{}, bytes.NewReader(png))
}

// SiteActionResponse is returned by POST /api/sites/{domain}/secure|unsecure.
type SiteActionResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func handleSiteAction(w http.ResponseWriter, r *http.Request) {
	// path: /api/sites/{domain}/secure or /api/sites/{domain}/unsecure
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/sites/"), "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	domain, action := parts[0], parts[1]

	// Favicon is a GET endpoint served separately.
	if action == "favicon" {
		handleSiteFavicon(w, r)
		return
	}

	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}

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
		envfile.UpdateAppURL(site.Path, "https", site.PrimaryDomain()) //nolint:errcheck
		_ = config.SetProjectSecured(site.Path, true)
		needsReload = true
	case "unsecure":
		if err := certs.UnsecureSite(*site); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		site.Secured = false
		envfile.UpdateAppURL(site.Path, "http", site.PrimaryDomain()) //nolint:errcheck
		_ = config.SetProjectSecured(site.Path, false)
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
		if site.IsCustomContainer() {
			writeJSON(w, SiteActionResponse{Error: "custom container sites do not use PHP versions"})
			return
		}
		_ = config.SetProjectPHPVersion(site.Path, version)
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
	case "restart":
		if err := cli.RestartSite(site.Name); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "rebuild":
		if err := cli.RebuildSite(site.Name); err != nil {
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
		go syncLerdYAMLWorkersDelayed(site)
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "horizon:stop":
		if err := cli.HorizonStopForSite(site.Name); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		if !site.Paused {
			_ = config.SetProjectWorkers(site.Path, cli.CollectRunningWorkerNames(site))
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "queue:start":
		phpVersion := site.PHPVersion
		if detected, err := phpPkg.DetectVersion(site.Path); err == nil && detected != "" {
			phpVersion = detected
		}
		go cli.QueueStartForSite(site.Name, site.Path, phpVersion) //nolint:errcheck
		go syncLerdYAMLWorkersDelayed(site)
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "queue:stop":
		if err := cli.QueueStopForSite(site.Name); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		if !site.Paused {
			_ = config.SetProjectWorkers(site.Path, cli.CollectRunningWorkerNames(site))
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "stripe:start":
		scheme := "http"
		if site.Secured {
			scheme = "https"
		}
		go cli.StripeStartForSite(site.Name, site.Path, scheme+"://"+site.PrimaryDomain()) //nolint:errcheck
		go syncLerdYAMLWorkersDelayed(site)
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "stripe:stop":
		if err := cli.StripeStopForSite(site.Name); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		if !site.Paused {
			_ = config.SetProjectWorkers(site.Path, cli.CollectRunningWorkerNames(site))
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "schedule:start":
		phpVersion := site.PHPVersion
		if detected, err := phpPkg.DetectVersion(site.Path); err == nil && detected != "" {
			phpVersion = detected
		}
		go cli.ScheduleStartForSite(site.Name, site.Path, phpVersion) //nolint:errcheck
		go syncLerdYAMLWorkersDelayed(site)
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "schedule:stop":
		if err := cli.ScheduleStopForSite(site.Name); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		if !site.Paused {
			_ = config.SetProjectWorkers(site.Path, cli.CollectRunningWorkerNames(site))
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "reverb:start":
		phpVersion := site.PHPVersion
		if detected, err := phpPkg.DetectVersion(site.Path); err == nil && detected != "" {
			phpVersion = detected
		}
		go cli.ReverbStartForSite(site.Name, site.Path, phpVersion) //nolint:errcheck
		go syncLerdYAMLWorkersDelayed(site)
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "reverb:stop":
		if err := cli.ReverbStopForSite(site.Name); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		if !site.Paused {
			_ = config.SetProjectWorkers(site.Path, cli.CollectRunningWorkerNames(site))
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "lan:share":
		if _, err := cli.LANShareStart(site.Name); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "lan:unshare":
		if err := cli.LANShareStop(site.Name); err != nil {
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
	case "domain:add":
		domainName := r.URL.Query().Get("name")
		if domainName == "" {
			writeJSON(w, SiteActionResponse{Error: "name parameter required"})
			return
		}
		cfg, cfgErr := config.LoadGlobal()
		if cfgErr != nil {
			writeJSON(w, SiteActionResponse{Error: "loading config: " + cfgErr.Error()})
			return
		}
		fullDomain := strings.ToLower(domainName) + "." + cfg.DNS.TLD
		if site.HasDomain(fullDomain) {
			writeJSON(w, SiteActionResponse{Error: "site already has domain " + fullDomain})
			return
		}
		if existing, eErr := config.IsDomainUsed(fullDomain); eErr == nil && existing != nil {
			writeJSON(w, SiteActionResponse{Error: "domain " + fullDomain + " is already used by site " + existing.Name})
			return
		}
		oldPrimary := site.PrimaryDomain()
		site.Domains = append(site.Domains, fullDomain)
		if err := config.AddSite(*site); err != nil {
			writeJSON(w, SiteActionResponse{Error: "updating registry: " + err.Error()})
			return
		}
		_ = config.SyncProjectDomains(site.Path, site.Domains, cfg.DNS.TLD)
		if err := siteops.RegenerateSiteVhost(site, oldPrimary); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		if site.Secured {
			certsDir := filepath.Join(config.CertsDir(), "sites")
			_ = certs.IssueCert(site.PrimaryDomain(), site.Domains, certsDir)
		}
		_ = podman.WriteContainerHosts()
		_ = nginx.Reload()
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "domain:edit":
		oldName := r.URL.Query().Get("old")
		newName := r.URL.Query().Get("new")
		if oldName == "" || newName == "" {
			writeJSON(w, SiteActionResponse{Error: "old and new parameters required"})
			return
		}
		cfg, cfgErr := config.LoadGlobal()
		if cfgErr != nil {
			writeJSON(w, SiteActionResponse{Error: "loading config: " + cfgErr.Error()})
			return
		}
		oldDomain := strings.ToLower(oldName) + "." + cfg.DNS.TLD
		newDomain := strings.ToLower(newName) + "." + cfg.DNS.TLD
		if !site.HasDomain(oldDomain) {
			writeJSON(w, SiteActionResponse{Error: "site does not have domain " + oldDomain})
			return
		}
		if oldDomain != newDomain {
			if existing, eErr := config.IsDomainUsed(newDomain); eErr == nil && existing != nil && existing.Path != site.Path {
				writeJSON(w, SiteActionResponse{Error: "domain " + newDomain + " is already used by site " + existing.Name})
				return
			}
		}
		oldPrimary := site.PrimaryDomain()
		for i, d := range site.Domains {
			if d == oldDomain {
				site.Domains[i] = newDomain
				break
			}
		}
		if err := config.AddSite(*site); err != nil {
			writeJSON(w, SiteActionResponse{Error: "updating registry: " + err.Error()})
			return
		}
		_ = config.SyncProjectDomains(site.Path, site.Domains, cfg.DNS.TLD)
		if err := siteops.RegenerateSiteVhost(site, oldPrimary); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		if site.Secured {
			certsDir := filepath.Join(config.CertsDir(), "sites")
			_ = certs.IssueCert(site.PrimaryDomain(), site.Domains, certsDir)
		}
		_ = podman.WriteContainerHosts()
		_ = nginx.Reload()
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "domain:remove":
		domainName := r.URL.Query().Get("name")
		if domainName == "" {
			writeJSON(w, SiteActionResponse{Error: "name parameter required"})
			return
		}
		cfg, cfgErr := config.LoadGlobal()
		if cfgErr != nil {
			writeJSON(w, SiteActionResponse{Error: "loading config: " + cfgErr.Error()})
			return
		}
		fullDomain := strings.ToLower(domainName) + "." + cfg.DNS.TLD

		// If the domain isn't in the registered list, it might still be in the
		// project's .lerd.yaml as a conflict-filtered entry. Remove it from
		// .lerd.yaml only — no registry, vhost, or cert work needed.
		if !site.HasDomain(fullDomain) {
			suffix := "." + cfg.DNS.TLD
			declared := strings.TrimSuffix(fullDomain, suffix)
			// Check if domain exists in .lerd.yaml before removing.
			proj, projErr := config.LoadProjectConfig(site.Path)
			if projErr != nil || proj == nil {
				writeJSON(w, SiteActionResponse{Error: "site does not have domain " + fullDomain})
				return
			}
			found := false
			for _, d := range proj.Domains {
				if strings.EqualFold(d, declared) {
					found = true
					break
				}
			}
			if !found {
				writeJSON(w, SiteActionResponse{Error: "site does not have domain " + fullDomain})
				return
			}
			if err := config.RemoveProjectDomain(site.Path, declared); err != nil {
				writeJSON(w, SiteActionResponse{Error: "updating .lerd.yaml: " + err.Error()})
				return
			}
			writeJSON(w, SiteActionResponse{OK: true})
			return
		}

		if len(site.Domains) <= 1 {
			writeJSON(w, SiteActionResponse{Error: "cannot remove the last domain"})
			return
		}
		oldPrimary := site.PrimaryDomain()
		var newDomains []string
		for _, d := range site.Domains {
			if d != fullDomain {
				newDomains = append(newDomains, d)
			}
		}
		site.Domains = newDomains
		if err := config.AddSite(*site); err != nil {
			writeJSON(w, SiteActionResponse{Error: "updating registry: " + err.Error()})
			return
		}
		_ = config.SyncProjectDomains(site.Path, site.Domains, cfg.DNS.TLD)
		if err := siteops.RegenerateSiteVhost(site, oldPrimary); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		if site.Secured {
			certsDir := filepath.Join(config.CertsDir(), "sites")
			_ = certs.IssueCert(site.PrimaryDomain(), site.Domains, certsDir)
		}
		_ = podman.WriteContainerHosts()
		_ = nginx.Reload()
		writeJSON(w, SiteActionResponse{OK: true})
		return
	default:
		// Handle framework worker actions: worker:{name}:start or worker:{name}:stop
		if strings.HasPrefix(action, "worker:") {
			parts := strings.SplitN(action, ":", 3)
			if len(parts) == 3 && (parts[2] == "start" || parts[2] == "stop") {
				workerName := parts[1]
				if parts[2] == "stop" {
					// Allow stopping orphaned workers that have no definition.
					if err := cli.WorkerStopForSite(site.Name, workerName); err != nil {
						writeJSON(w, SiteActionResponse{Error: err.Error()})
						return
					}
					if !site.Paused {
						_ = config.SetProjectWorkers(site.Path, cli.CollectRunningWorkerNames(site))
					}
				} else {
					fwN := site.Framework
					fw, ok := config.GetFrameworkForDir(fwN, site.Path)
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
					go cli.WorkerStartForSite(site.Name, site.Path, phpVersion, workerName, worker) //nolint:errcheck
					go syncLerdYAMLWorkersDelayed(site)
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
	case "start":
		short := strings.ReplaceAll(version, ".", "")
		unit := "lerd-php" + short + "-fpm"
		if err := podman.StartUnit(unit); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	case "stop":
		short := strings.ReplaceAll(version, ".", "")
		unit := "lerd-php" + short + "-fpm"
		if err := podman.StopUnit(unit); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	case "remove":
		short := strings.ReplaceAll(version, ".", "")
		unit := "lerd-php" + short + "-fpm"
		_ = podman.StopUnit(unit)
		if err := podman.RemoveQuadlet(unit); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		_ = podman.DaemonReloadFn()
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
	var req struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Version == "" || !validVersion.MatchString(req.Version) {
		writeJSON(w, map[string]any{"ok": false, "error": "invalid version"})
		return
	}
	version := req.Version
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

	// If no container exists for this unit, route to the platform log stream
	// (file tail for native services) or report not-running for container units.
	if exists, _ := podman.ContainerExists(container); !exists {
		if isContainerUnit(container) {
			fmt.Fprintf(w, "data: container %s is not running\n\n", container)
			flusher.Flush()
			return
		}
		// Native service (dns, watcher, ui) — stream from log file.
		streamUnitLogs(w, r, container)
		return
	}

	tail := "100"
	if r.Header.Get("Last-Event-ID") != "" {
		tail = "0"
	}

	pr, pw := io.Pipe()
	cmd := exec.CommandContext(r.Context(), podman.PodmanBin(), "logs", "-f", "--tail", tail, container)
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
		AutostartOnLogin: lerdSystemd.IsAutostartEnabled(),
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

	if err := cli.ApplyAutostart(!body.Enabled); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
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

// handleLerdUpdateTerminal opens the user's terminal emulator running
// `lerd update`, with a "press Enter to close" tail so the user can read
// the output. Loopback-only — listed in loopbackOnlyRoutes.
func handleLerdUpdateTerminal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	script := `lerd update; echo; read -rp "Press Enter to close..."`
	if err := openTerminalCommand(script); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// openTerminalCommand opens the user's terminal emulator and runs the given
// shell script in it. Mirrors openTerminalAt's candidate list — the two
// could merge later but the arg shapes diverge enough that keeping them
// separate is clearer for now.
func openTerminalCommand(script string) error {
	type termCmd struct {
		bin  string
		args []string
	}
	combined := "sh -c " + shQuote(script)
	candidates := []termCmd{
		{"kitty", []string{"sh", "-c", script}},
		{"foot", []string{"sh", "-c", script}},
		{"alacritty", []string{"-e", "sh", "-c", script}},
		{"wezterm", []string{"start", "--", "sh", "-c", script}},
		{"ghostty", []string{"-e", combined}},
		{"ptyxis", []string{"--", "sh", "-c", script}},
		{"konsole", []string{"--separate", "-e", "sh", "-c", script}},
		{"gnome-terminal", []string{"--", "sh", "-c", script}},
		{"xfce4-terminal", []string{"-e", combined}},
		{"tilix", []string{"-e", combined}},
		{"terminator", []string{"-e", combined}},
		{"xterm", []string{"-e", "sh", "-c", script}},
	}

	if runtime.GOOS == "darwin" {
		if _, err := os.Stat("/Applications/iTerm.app"); err == nil {
			as := "tell application \"iTerm2\"\n\tcreate window with default profile\n\ttell current session of current window\n\t\twrite text " + appleScriptStr(script) + "\n\tend tell\nend tell"
			candidates = append(candidates, termCmd{"osascript", []string{"-e", as}})
		}
		as := "tell application \"Terminal\"\n\tdo script " + appleScriptStr(script) + "\n\tactivate\nend tell"
		candidates = append(candidates, termCmd{"osascript", []string{"-e", as}})
	}
	for _, t := range candidates {
		bin, err := exec.LookPath(t.bin)
		if err != nil {
			continue
		}
		cmd := exec.Command(bin, t.args...)
		if runtime.GOOS != "darwin" {
			cmd.Env = graphicalEnv()
		}
		if err := cmd.Start(); err != nil {
			return err
		}
		go func() { _ = cmd.Wait() }()
		return nil
	}
	return fmt.Errorf("no terminal emulator found; set $TERMINAL or install kitty, foot, alacritty, wezterm, ghostty, ptyxis, konsole, or gnome-terminal")
}

// shQuote wraps s in single quotes, escaping any embedded single quotes
// using the standard '\" dance so the result is safe for /bin/sh -c.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// appleScriptStr returns an AppleScript string expression for s.
// AppleScript has no escape sequences; double quotes are spliced in via & quote &.
func appleScriptStr(s string) string {
	parts := strings.Split(s, `"`)
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = `"` + p + `"`
	}
	return strings.Join(quoted, " & quote & ")
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

// handleAppLogs serves application-level log files (e.g. Laravel's storage/logs/*.log).
//
//	GET /api/app-logs/{domain}            → list available log files
//	GET /api/app-logs/{domain}/{filename} → parsed log entries
func handleAppLogs(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/app-logs/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	domain := parts[0]
	site, err := config.FindSiteByDomain(domain)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	fwName := site.Framework
	if fwName == "" {
		fwName, _ = config.DetectFrameworkForDir(site.Path)
	}
	fw, hasFw := config.GetFramework(fwName)
	if !hasFw || len(fw.Logs) == 0 {
		writeJSON(w, map[string]any{"files": []any{}, "entries": []any{}})
		return
	}

	if len(parts) == 1 {
		// List available log files
		files, _ := applog.DiscoverLogFiles(site.Path, fw.Logs)
		if files == nil {
			files = []applog.LogFile{}
		}
		writeJSON(w, map[string]any{"files": files})
		return
	}

	// Parse entries for a specific file
	filename := parts[1]
	// Validate filename: only safe characters
	for _, c := range filename {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-') {
			http.NotFound(w, r)
			return
		}
	}

	fullPath := applog.ResolveLogFilePath(site.Path, fw.Logs, filename)
	if fullPath == "" {
		http.NotFound(w, r)
		return
	}

	maxEntries := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if n, err := fmt.Sscanf(limitStr, "%d", &maxEntries); err != nil || n != 1 {
			maxEntries = 100
		}
		if maxEntries <= 0 {
			maxEntries = 0 // 0 means unlimited
		}
	}

	format := applog.FormatForFile(fw.Logs, filename)
	entries, err := applog.ParseFile(fullPath, format, maxEntries)
	if err != nil {
		writeJSON(w, map[string]any{"entries": []any{}, "error": err.Error()})
		return
	}
	if entries == nil {
		entries = []applog.LogEntry{}
	}
	writeJSON(w, map[string]any{"entries": entries})
}

// handleBrowse returns a listing of directories for the file browser.
func handleBrowse(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = home
	}
	dir = filepath.Clean(dir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}

	type dirEntry struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	var dirs []dirEntry
	// Always include parent
	parent := filepath.Dir(dir)
	if parent != dir {
		dirs = append(dirs, dirEntry{Name: "..", Path: parent})
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dirs = append(dirs, dirEntry{Name: e.Name(), Path: filepath.Join(dir, e.Name())})
	}
	writeJSON(w, map[string]any{"current": dir, "dirs": dirs})
}

// handleSiteLink links a directory as a site via POST /api/sites/link.
// It streams command output as SSE events and sends a final "done" event.
func handleSiteLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		writeJSON(w, SiteActionResponse{Error: "path parameter required"})
		return
	}
	path = filepath.Clean(path)

	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		writeJSON(w, SiteActionResponse{Error: "not a valid directory: " + path})
		return
	}

	self, err := os.Executable()
	if err != nil {
		writeJSON(w, SiteActionResponse{Error: "resolving executable: " + err.Error()})
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
	w.Header().Set("X-Accel-Buffering", "no")

	// streamCmd runs a command and streams its output as SSE data events.
	// Returns the combined output and whether the command failed.
	streamCmd := func(name string, args ...string) (string, bool) {
		cmd := exec.CommandContext(r.Context(), name, args...)
		cmd.Dir = path

		pr, pw := io.Pipe()
		cmd.Stdout = pw
		cmd.Stderr = pw

		if startErr := cmd.Start(); startErr != nil {
			msg := startErr.Error()
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
			return msg, true
		}

		go func() {
			cmd.Wait() //nolint:errcheck
			pw.Close()
		}()

		var out strings.Builder
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			line := scanner.Text()
			out.WriteString(line)
			out.WriteByte('\n')
			escaped := strings.ReplaceAll(line, "\\", "\\\\")
			fmt.Fprintf(w, "data: %s\n\n", escaped)
			flusher.Flush()
		}
		return out.String(), cmd.ProcessState != nil && cmd.ProcessState.ExitCode() != 0
	}

	// Run lerd link.
	fmt.Fprintf(w, "data: → Linking site...\n\n")
	flusher.Flush()
	out, failed := streamCmd(self, "link")
	if failed {
		fmt.Fprintf(w, "event: done\ndata: %s\n\n", mustJSON(map[string]any{"ok": false, "error": "link failed: " + out}))
		flusher.Flush()
		return
	}

	// Run env setup (non-fatal).
	fmt.Fprintf(w, "data: → Setting up environment...\n\n")
	flusher.Flush()
	streamCmd(self, "env") //nolint:errcheck

	// Find the newly linked site to return its domain.
	site, err := config.FindSiteByPath(path)
	if err != nil {
		fmt.Fprintf(w, "event: done\ndata: %s\n\n", mustJSON(map[string]any{"ok": true}))
		flusher.Flush()
		return
	}
	fmt.Fprintf(w, "event: done\ndata: %s\n\n", mustJSON(map[string]any{"ok": true, "domain": site.PrimaryDomain()}))
	flusher.Flush()
}

// syncLerdYAMLWorkersDelayed waits briefly for the worker unit to start, then syncs.
func syncLerdYAMLWorkersDelayed(site *config.Site) {
	time.Sleep(2 * time.Second)
	if !site.Paused {
		_ = config.SetProjectWorkers(site.Path, cli.CollectRunningWorkerNames(site))
	}
}
