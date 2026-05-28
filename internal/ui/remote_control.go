package ui

import (
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"runtime"
	"strings"
	"time"

	lerdcli "github.com/geodro/lerd/internal/cli"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/nginx"
	"golang.org/x/crypto/bcrypt"
)

// loopbackOnlyRoutes are dashboard endpoints that perform actions too
// destructive or sensitive to allow from a remote (LAN) client even when
// remote-control is enabled with valid Basic auth credentials. Examples:
// shutting lerd down entirely, opening a terminal on the host, linking
// arbitrary host filesystem paths as new sites. The local user can still
// use them as normal because loopback bypasses everything.
var loopbackOnlyRoutes = []string{
	"/api/lerd/stop",            // shuts down all lerd containers
	"/api/lerd/quit",            // exits the dashboard process
	"/api/lerd/update-terminal", // spawns a terminal emulator on the host
	"/api/sites/link",           // links arbitrary host filesystem paths
	"/api/browse",               // browses host filesystem
	"/api/push/test",            // fires notifications onto subscribed devices
}

// loopbackOnlySiteSubactions are the per-site actions (under
// /api/sites/{domain}/) whose entire subtree is restricted to loopback.
// A subaction "/env" gates /api/sites/{d}/env and every nested route
// under it (e.g. /env/files, /env/backups, /env/backups/<name>,
// /env/restore), so adding a new subresource cannot accidentally escape
// the LAN gate by failing to be re-listed here.
var loopbackOnlySiteSubactions = []string{
	"/terminal", // opens an interactive shell on the host
	"/env",      // raw .env content + backups + restore (APP_KEY, DB creds, tokens)
}

// fromHost reports whether r's source IP belongs to one of the host's
// own interfaces. The mailpit container reaches the dashboard via
// host.containers.internal, which pasta (Linux) and gvproxy / vmnet
// (macOS) source-NAT to the host, so lerd-ui sees the request as coming
// from one of its own addresses. A LAN attacker arrives from a different
// IP and is rejected. Spoofing a host-owned address would break the TCP
// handshake because the SYN-ACK routes back into the host rather than
// reaching the attacker.
//
// Interfaces are re-read on every call so VPN attach, WiFi switch, or
// a late-arriving podman bridge are picked up without a daemon restart.
// Each call is a few syscalls, fine for webhook-rate traffic.
func fromHost(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	// IPv6 link-local sources may carry a zone suffix (fe80::1%eth0);
	// strip it before parsing so the value compare below works.
	if i := strings.Index(host, "%"); i != -1 {
		host = host[:i]
	}
	src := net.ParseIP(host)
	if src == nil {
		return false
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok || ipNet.IP == nil {
			continue
		}
		if src.Equal(ipNet.IP) {
			return true
		}
	}
	return false
}

// isLoopbackOnlyPath reports whether the given URL path is in either
// the exact-match list or matches a per-site action whose entire subtree
// is loopback-only. A subaction "/env" matches /api/sites/{d}/env exactly
// and any subroute under it (/env/files, /env/backups, /env/restore,
// /env/backups/<name>), so adding a new subresource never silently
// escapes the gate.
func isLoopbackOnlyPath(path string) bool {
	for _, p := range loopbackOnlyRoutes {
		if path == p {
			return true
		}
	}
	if !strings.HasPrefix(path, "/api/sites/") {
		return false
	}
	rest := strings.TrimPrefix(path, "/api/sites/")
	slash := strings.Index(rest, "/")
	if slash < 0 {
		return false
	}
	after := rest[slash:]
	for _, action := range loopbackOnlySiteSubactions {
		if after == action || strings.HasPrefix(after, action+"/") {
			return true
		}
	}
	return false
}

// withRemoteControlGate wraps the dashboard mux with the LAN-access gate.
// Two independent flags control LAN access:
//
//   - cfg.LAN.Exposed   — "may LAN clients reach lerd at all?" (lerd lan:expose)
//   - cfg.UI.PasswordHash — "if they may, what credentials do they need?"
//     (lerd remote-control on)
//
// Behavior matrix for non-loopback requests:
//
//	cfg.LAN.Exposed | cfg.UI.PasswordHash | result
//	----------------|---------------------|--------------------------------
//	false           | empty               | 403 (LAN exposure off)
//	false           | set                 | 403 (LAN exposure off — credentials are inert)
//	true            | empty               | 403 (no credentials configured)
//	true            | set                 | require HTTP Basic auth
//
// Loopback (127.x, ::1) always bypasses both checks. OPTIONS preflight
// passes through (no Authorization header expected). /api/remote-setup
// has its own token + IP gate and is unaffected.
//
// Additionally, the loopbackOnlyRoutes list (lerd stop/quit, site link,
// terminal, filesystem browse) is rejected from non-loopback even with
// valid Basic auth. The local user keeps full access via loopback.
func withRemoteControlGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. CORS preflight: pass through. Browsers don't include the
		// Authorization header on preflight, so requiring auth here would
		// break every cross-origin request from a configured client.
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		// 2. The remote-setup bootstrap endpoint has its own gate (token,
		// RFC 1918 source IP, brute-force lockout). It must remain reachable
		// from a remote laptop *before* the user has set up dashboard auth.
		if r.URL.Path == "/api/remote-setup" {
			next.ServeHTTP(w, r)
			return
		}

		// 2b. Mailpit's webhook is POSTed from inside the mailpit container
		// to host.containers.internal:7073. pasta (Linux) and gvproxy /
		// vmnet (macOS) source-NAT that to one of the host's own interface
		// IPs, so we accept any caller whose source IP belongs to the
		// host. A LAN attacker arrives from a different IP and is rejected,
		// closing the "anyone on the WiFi can spam fake mail pushes" vector.
		if r.URL.Path == "/api/webhooks/mailpit" && fromHost(r) {
			next.ServeHTTP(w, r)
			return
		}

		// 3. Loopback (127.x, ::1) always bypasses. The local user owns the
		// machine and can never be locked out.
		if isLoopbackRequest(r) {
			next.ServeHTTP(w, r)
			return
		}

		// 3a. Loopback-only routes: even with valid Basic auth, certain
		// destructive actions (lerd stop, terminal, site link, filesystem
		// browse) are not allowed from non-loopback sources. The local
		// user can still trigger them via loopback.
		if isLoopbackOnlyPath(r.URL.Path) {
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "Forbidden — this action is only available from the lerd host (loopback).", http.StatusForbidden)
			return
		}

		// 4. Non-loopback path. Inspect the configured LAN/remote-control
		// state. All gate responses set Cache-Control: no-store so
		// browsers don't replay an old 403/401 after the user enables
		// remote control or LAN exposure.
		cfg, _ := config.LoadGlobal()

		// 4a. LAN exposure is the top-level gate. If lan:expose is off,
		// LAN clients are denied regardless of whether credentials are
		// set — this prevents stale credentials from a previous expose
		// session from surviving lan:unexpose, and matches the safe
		// default state of "lerd is invisible to the network".
		if cfg == nil || !cfg.LAN.Exposed {
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "Forbidden — lerd is not exposed to the LAN. Run `lerd lan:expose` on the server to enable LAN access.", http.StatusForbidden)
			return
		}

		// 4b. LAN exposure is on, but no remote-control credentials have
		// been configured. The dashboard is reachable but unauthenticated
		// access would be a free-for-all, so deny.
		if cfg.UI.PasswordHash == "" {
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "Forbidden — dashboard credentials are not configured. Run `lerd remote-control on` on the server to enable.", http.StatusForbidden)
			return
		}

		// 5. Validate HTTP Basic auth.
		user, pass, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="lerd dashboard"`)
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if subtle.ConstantTimeCompare([]byte(user), []byte(cfg.UI.Username)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="lerd dashboard"`)
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if bcrypt.CompareHashAndPassword([]byte(cfg.UI.PasswordHash), []byte(pass)) != nil {
			w.Header().Set("WWW-Authenticate", `Basic realm="lerd dashboard"`)
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// handleAccessMode serves /api/access-mode. Returns whether the request
// came from loopback so the frontend can hide UI elements that map to
// loopback-only endpoints (the terminal button, the link-site button, the
// stop-lerd button). Reachable from any source — there's no sensitive
// information here.
func handleAccessMode(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"loopback": isLoopbackRequest(r),
	})
}

// handleLANStatus serves /api/lan/status.
//
//	GET                              → { exposed, lan_ip }
//	POST { action: "expose" }        → flips lerd to LAN-exposed mode
//	POST { action: "unexpose" }      → flips lerd back to loopback-only mode
//
// POST is gated to loopback inside the handler because it rewrites systemd
// user units, container quadlets, and dnsmasq config on the host.
func handleLANStatus(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg, _ := config.LoadGlobal()
		exposed := false
		if cfg != nil {
			exposed = cfg.LAN.Exposed
		}
		lanIP := ""
		if exposed {
			lanIP = uiPrimaryLANIP()
		}
		writeJSON(w, map[string]any{
			"exposed": exposed,
			"lan_ip":  lanIP,
			"macos":   runtime.GOOS == "darwin",
		})
		return

	case http.MethodPost:
		// POST touches systemd units and quadlets on the host — never allow
		// from LAN even if the caller has valid Basic auth.
		if !isLoopbackRequest(r) {
			http.Error(w, "Forbidden — LAN exposure can only be toggled from the lerd host (loopback).", http.StatusForbidden)
			return
		}
		var body struct {
			Action string `json:"action"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if body.Action != "expose" && body.Action != "unexpose" {
			http.Error(w, "unknown action — expected 'expose' or 'unexpose'", http.StatusBadRequest)
			return
		}

		// Stream NDJSON progress so the dashboard can render per-step
		// feedback instead of a single opaque spinner. Each line is a
		// JSON object: {step, status} for in-flight steps and a final
		// {result, exposed, lan_ip, error} envelope when the toggle
		// completes (or errors).
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		writeLine := func(payload map[string]any) {
			data, _ := json.Marshal(payload)
			_, _ = w.Write(append(data, '\n'))
			if flusher != nil {
				flusher.Flush()
			}
		}
		progress := func(step string) {
			writeLine(map[string]any{"step": step})
		}

		switch body.Action {
		case "expose":
			lanIP, err := lerdcli.EnableLANExposure(progress)
			if err != nil {
				writeLine(map[string]any{"result": "error", "error": err.Error()})
				return
			}
			writeLine(map[string]any{"result": "ok", "exposed": true, "lan_ip": lanIP})
			return
		case "unexpose":
			if err := lerdcli.DisableLANExposure(progress); err != nil {
				writeLine(map[string]any{"result": "error", "error": err.Error()})
				return
			}
			writeLine(map[string]any{"result": "ok", "exposed": false, "lan_ip": ""})
			return
		}

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

// uiPrimaryLANIP duplicates the dial-trick from cli/dns.go because importing
// cli from ui would risk a cycle. Cheap.
func uiPrimaryLANIP() string {
	conn, err := net.Dial("udp4", "1.1.1.1:80")
	if err == nil {
		defer conn.Close()
		return conn.LocalAddr().(*net.UDPAddr).IP.String()
	}
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok {
				if v4 := ipnet.IP.To4(); v4 != nil && !v4.IsLoopback() {
					return v4.String()
				}
			}
		}
	}
	return ""
}

// handleRemoteControl serves /api/remote-control. The middleware already
// gates this endpoint to loopback (because writing the password from a
// browser over HTTP would otherwise expose it to the network), so we don't
// need a second source-IP check here.
//
//	GET                                  → { enabled, username }
//	POST { action: "enable", username, password } → enables, persists hash
//	POST { action: "disable" }           → clears credentials
func handleRemoteControl(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg, err := config.LoadGlobal()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{
			"enabled":  cfg.UI.PasswordHash != "",
			"username": cfg.UI.Username,
		})
		return

	case http.MethodPost:
		var body struct {
			Action   string `json:"action"`
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		cfg, err := config.LoadGlobal()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		switch body.Action {
		case "enable":
			// In disabled-DNS mode the dashboard chains "set credentials"
			// with "flip lan:expose" into a single user action because the
			// dashboard is effectively the only thing LAN exposure unlocks
			// (sites can't resolve over .localhost on remote devices). So we
			// only require lan:expose to be on first when DNS is enabled.
			if !cfg.LAN.Exposed && cfg.DNS.Enabled {
				http.Error(w, "LAN exposure is off — run `lerd lan:expose` first. Dashboard credentials are only meaningful while the dashboard is reachable from other devices.", http.StatusBadRequest)
				return
			}
			if body.Username == "" || body.Password == "" {
				http.Error(w, "username and password are required", http.StatusBadRequest)
				return
			}
			hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
			if err != nil {
				http.Error(w, "hashing password: "+err.Error(), http.StatusInternalServerError)
				return
			}
			cfg.UI.Username = body.Username
			cfg.UI.PasswordHash = string(hash)
			if err := config.SaveGlobal(cfg); err != nil {
				http.Error(w, "saving config: "+err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, map[string]any{"ok": true, "enabled": true, "username": body.Username})
			return

		case "disable":
			cfg.UI.Username = ""
			cfg.UI.PasswordHash = ""
			if err := config.SaveGlobal(cfg); err != nil {
				http.Error(w, "saving config: "+err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, map[string]any{"ok": true, "enabled": false})
			return

		default:
			http.Error(w, "unknown action — expected 'enable' or 'disable'", http.StatusBadRequest)
			return
		}

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

// handleRemoteSetupGenerate serves /api/remote-setup/generate. POST creates a
// fresh one-time setup token and returns the curl one-liner the laptop should
// run. Loopback-only — generating a token from a remote browser would defeat
// the whole gate. The corresponding /api/remote-setup endpoint (consumed by
// the laptop) lives in remote_setup.go and has its own RFC 1918 + token gate.
func handleRemoteSetupGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !isLoopbackRequest(r) {
		http.Error(w, "Forbidden — setup codes can only be generated from the lerd host (loopback).", http.StatusForbidden)
		return
	}
	if cfg, _ := config.LoadGlobal(); cfg != nil && !cfg.DNS.Enabled {
		http.Error(w, "remote-setup requires lerd-managed DNS, the remote machine has no way to resolve *.localhost; set dns.enabled: true and re-run lerd install.", http.StatusBadRequest)
		return
	}

	code, err := lerdcli.GenerateRemoteSetupToken(15 * time.Minute)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	lanIP := uiPrimaryLANIP()
	target := lanIP
	if target == "" {
		target = "<server-ip>"
	}
	curl := "curl -sSL 'http://" + target + ":7073/api/remote-setup?code=" + code + "' | bash"
	writeJSON(w, map[string]any{
		"code":       code,
		"lan_ip":     lanIP,
		"curl":       curl,
		"expires_in": "15m",
	})
}

// isLoopbackRequest reports whether r should be treated as originating from
// the local host. Three paths qualify:
//
//  1. The connection arrived over the unix socket listener. Only host
//     processes with filesystem access to the socket can connect, so this
//     is at least as trusted as TCP loopback. The lerd.localhost nginx
//     vhost reaches lerd-ui via this path.
//  2. The TCP peer is a loopback IP (127.x, ::1). This catches direct visits
//     to http://localhost:7073 / http://127.0.0.1:7073.
//  3. The request carries an X-Lerd-Trust header whose value matches the
//     per-install token. Kept for backward compatibility with old vhosts
//     that may still inject the header; new installs use the unix socket.
func isLoopbackRequest(r *http.Request) bool {
	if v, _ := r.Context().Value(ctxKeyUnixSocket{}).(bool); v {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return true
	}
	if claimed := r.Header.Get("X-Lerd-Trust"); claimed != "" {
		token, err := nginx.LoadOrGenerateTrustToken()
		if err == nil && token != "" && subtle.ConstantTimeCompare([]byte(claimed), []byte(token)) == 1 {
			return true
		}
	}
	return false
}
