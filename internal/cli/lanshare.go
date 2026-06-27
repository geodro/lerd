package cli

import (
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/geodro/lerd/internal/config"
)

var (
	lanShareMu      sync.Mutex
	lanShareServers = map[string]*http.Server{} // siteName → running proxy
)

// LANShareEnsurePort assigns a stable port to the site if not already set and
// saves it to sites.yaml. It does NOT start the proxy — that is the daemon's job.
// CLI commands use this to persist the port, then notify the daemon via its API.
func LANShareEnsurePort(siteName string) (int, error) {
	site, err := config.FindSite(siteName)
	if err != nil {
		return 0, err
	}
	if site.LANPort != 0 {
		return site.LANPort, nil
	}
	port := assignLANSharePort(siteName)
	site.LANPort = port
	if err := config.AddSite(*site); err != nil {
		return 0, fmt.Errorf("saving LAN port: %w", err)
	}
	return port, nil
}

// LANShareStart starts the in-process reverse proxy for the site. It is
// intended to be called from the daemon (UI server) only — the proxy goroutine
// lives in the daemon process. CLI commands should notify the daemon via its
// HTTP API instead of calling this directly.
func LANShareStart(siteName string) (int, error) {
	site, err := config.FindSite(siteName)
	if err != nil {
		return 0, err
	}
	if site.Paused {
		return site.LANPort, fmt.Errorf("site %q is paused", siteName)
	}

	port := site.LANPort
	if port == 0 {
		port = assignLANSharePort(siteName)
		site.LANPort = port
		if err := config.AddSite(*site); err != nil {
			return 0, fmt.Errorf("saving LAN port: %w", err)
		}
	}

	lanShareMu.Lock()
	if _, running := lanShareServers[siteName]; running {
		lanShareMu.Unlock()
		return port, nil
	}
	lanShareMu.Unlock()

	cfg, _ := config.LoadGlobal()
	httpPort := cfg.Nginx.HTTPPort
	if httpPort == 0 {
		httpPort = 80
	}
	httpsPort := cfg.Nginx.HTTPSPort
	if httpsPort == 0 {
		httpsPort = 443
	}

	srv, err := startLANShareProxy(site.PrimaryDomain(), port, httpPort, httpsPort, site.Secured)
	if err != nil {
		return 0, err
	}

	lanShareMu.Lock()
	lanShareServers[siteName] = srv
	lanShareMu.Unlock()

	return port, nil
}

// LANShareStop stops the LAN share proxy for the site and clears its port
// from the site registry.
func LANShareStop(siteName string) error {
	closeLANShareServer(siteName)

	site, err := config.FindSite(siteName)
	if err != nil {
		return err
	}
	site.LANPort = 0
	return config.AddSite(*site)
}

// LANShareStopServer closes the running proxy without clearing the site's
// stored LAN port. Used when pausing a site so the same port can be reused on
// unpause without invalidating any QR codes the user has shared.
func LANShareStopServer(siteName string) {
	closeLANShareServer(siteName)
}

func closeLANShareServer(siteName string) {
	lanShareMu.Lock()
	srv, running := lanShareServers[siteName]
	if running {
		delete(lanShareServers, siteName)
	}
	lanShareMu.Unlock()

	if running {
		srv.Close()
	}
}

// LANShareRunning reports whether a share proxy is active for the site.
func LANShareRunning(siteName string) bool {
	lanShareMu.Lock()
	defer lanShareMu.Unlock()
	_, ok := lanShareServers[siteName]
	return ok
}

// LANShareRefreshIfRunning closes any running share proxy for the site and
// re-opens it using the current site config — needed when TLS gets toggled
// (the proxy was bound to the previous backend port and Secured flag).
// No-op when no proxy is running. Also refreshes any worktree shares so the
// branch listeners pick up the same change.
func LANShareRefreshIfRunning(siteName string) error {
	wasRunning := LANShareRunning(siteName)
	if wasRunning {
		closeLANShareServer(siteName)
		if _, err := LANShareStart(siteName); err != nil {
			return fmt.Errorf("restarting LAN share for %s: %w", siteName, err)
		}
	}
	entries, err := config.WorktreeLANsForSite(siteName)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		key := worktreeLANServerKey(e.Site, e.Branch)
		lanShareMu.Lock()
		_, running := lanShareServers[key]
		lanShareMu.Unlock()
		if !running {
			continue
		}
		closeLANShareServer(key)
		if _, err := LANShareStartWorktree(e.Site, e.Branch); err != nil {
			return fmt.Errorf("restarting worktree share %s/%s: %w", e.Site, e.Branch, err)
		}
	}
	return nil
}

// RestoreLANShareProxies restarts share proxies for every site that has a
// LANPort stored in the registry. Called once when the UI server starts.
func RestoreLANShareProxies() {
	reg, err := config.LoadSites()
	if err != nil {
		return
	}
	cfg, _ := config.LoadGlobal()
	httpPort := 80
	httpsPort := 443
	if cfg != nil {
		if cfg.Nginx.HTTPPort != 0 {
			httpPort = cfg.Nginx.HTTPPort
		}
		if cfg.Nginx.HTTPSPort != 0 {
			httpsPort = cfg.Nginx.HTTPSPort
		}
	}
	for _, s := range reg.Sites {
		if !shouldRunLANShareProxy(s) {
			continue
		}
		srv, err := startLANShareProxy(s.PrimaryDomain(), s.LANPort, httpPort, httpsPort, s.Secured)
		if err != nil {
			continue
		}
		lanShareMu.Lock()
		lanShareServers[s.Name] = srv
		lanShareMu.Unlock()
	}

	// Worktree-scoped shares persist alongside their parent site.
	wtEntries, err := config.LoadWorktreeLANRegistry()
	if err != nil {
		return
	}
	siteByName := map[string]config.Site{}
	for _, s := range reg.Sites {
		siteByName[s.Name] = s
	}
	liveByPath := map[string]map[string]bool{}
	for _, e := range wtEntries {
		s, ok := siteByName[e.Site]
		if !ok || s.Paused {
			continue
		}
		live, cached := liveByPath[s.Path]
		if !cached {
			live = liveWorktreeBranches(&s)
			liveByPath[s.Path] = live
		}
		// Skip orphans: the worktree was removed while lerd-ui was down, so
		// the listener should not come back. The watcher's startup pass will
		// drop the registry entry shortly.
		if !live[e.Branch] {
			continue
		}
		domain := e.Branch + "." + s.PrimaryDomain()
		srv, err := startLANShareProxy(domain, e.Port, httpPort, httpsPort, s.Secured)
		if err != nil {
			continue
		}
		lanShareMu.Lock()
		lanShareServers[worktreeLANServerKey(e.Site, e.Branch)] = srv
		lanShareMu.Unlock()
	}
}

// shouldRunLANShareProxy reports whether the daemon should keep a LAN share
// proxy bound for the site. Paused sites are excluded so the port is not held
// while the site is intentionally offline.
func shouldRunLANShareProxy(s config.Site) bool {
	return s.LANPort != 0 && !s.Paused
}

// assignLANSharePort finds the lowest unused port >= 9100 across all site +
// worktree LAN shares. excludeSiteName is the site whose port is being
// (re)assigned and should not block itself.
func assignLANSharePort(excludeSiteName string) int {
	const base = 9100
	used := map[int]bool{}
	if reg, err := config.LoadSites(); err == nil {
		for _, s := range reg.Sites {
			if s.Name == excludeSiteName || s.LANPort == 0 {
				continue
			}
			used[s.LANPort] = true
		}
	}
	if entries, err := config.LoadWorktreeLANRegistry(); err == nil {
		for _, e := range entries {
			used[e.Port] = true
		}
	}
	port := base
	for used[port] {
		port++
	}
	return port
}

// assignWorktreeLANPort finds the lowest unused port across all site and
// worktree LAN shares, excluding the (site, branch) that is being assigned.
func assignWorktreeLANPort(siteName, branch string) int {
	const base = 9100
	used := map[int]bool{}
	if reg, err := config.LoadSites(); err == nil {
		for _, s := range reg.Sites {
			if s.LANPort != 0 {
				used[s.LANPort] = true
			}
		}
	}
	if entries, err := config.LoadWorktreeLANRegistry(); err == nil {
		for _, e := range entries {
			if e.Site == siteName && e.Branch == branch {
				continue
			}
			used[e.Port] = true
		}
	}
	port := base
	for used[port] {
		port++
	}
	return port
}

// worktreeLANServerKey is the lanShareServers map key for a worktree share.
func worktreeLANServerKey(siteName, branch string) string {
	return siteName + "@" + branch
}

// LANShareStartWorktree starts the LAN share proxy for a worktree and
// persists its port. The proxy targets <branch>.<parent_domain> so nginx
// routes to the worktree's vhost. Idempotent.
func LANShareStartWorktree(siteName, branch string) (int, error) {
	site, err := config.FindSite(siteName)
	if err != nil {
		return 0, err
	}
	if site.Paused {
		return 0, fmt.Errorf("site %q is paused", siteName)
	}

	worktreeDomain := branch + "." + site.PrimaryDomain()

	port := 0
	if entry, found, err := config.FindWorktreeLAN(siteName, branch); err == nil && found {
		port = entry.Port
	}
	if port == 0 {
		port = assignWorktreeLANPort(siteName, branch)
	}

	if err := config.AddWorktreeLAN(config.WorktreeLANEntry{
		Site: siteName, Branch: branch, Port: port,
	}); err != nil {
		return 0, fmt.Errorf("saving worktree LAN port: %w", err)
	}

	key := worktreeLANServerKey(siteName, branch)
	lanShareMu.Lock()
	if _, running := lanShareServers[key]; running {
		lanShareMu.Unlock()
		return port, nil
	}
	lanShareMu.Unlock()

	cfg, _ := config.LoadGlobal()
	httpPort := cfg.Nginx.HTTPPort
	if httpPort == 0 {
		httpPort = 80
	}
	httpsPort := cfg.Nginx.HTTPSPort
	if httpsPort == 0 {
		httpsPort = 443
	}

	srv, err := startLANShareProxy(worktreeDomain, port, httpPort, httpsPort, site.Secured)
	if err != nil {
		return 0, err
	}
	lanShareMu.Lock()
	lanShareServers[key] = srv
	lanShareMu.Unlock()
	return port, nil
}

// LANShareStopWorktree stops the proxy and clears the registry entry.
func LANShareStopWorktree(siteName, branch string) error {
	closeLANShareServer(worktreeLANServerKey(siteName, branch))
	_, _, err := config.RemoveWorktreeLAN(siteName, branch)
	return err
}

// LANShareWorktreeRunning reports whether a worktree share proxy is bound.
func LANShareWorktreeRunning(siteName, branch string) bool {
	lanShareMu.Lock()
	defer lanShareMu.Unlock()
	_, ok := lanShareServers[worktreeLANServerKey(siteName, branch)]
	return ok
}

// DropOrphanedWorktreeLANShares removes registry entries and stops proxies
// for worktrees that no longer exist. Notifies the daemon over HTTP first so
// the close happens in lerd-ui's process where the listener actually lives;
// the watcher process's lanShareServers map is empty so a direct close would
// leave the listener bound. Falls back to in-process close + registry remove
// if the daemon is unreachable.
func DropOrphanedWorktreeLANShares(site *config.Site, liveBranches map[string]bool) {
	entries, err := config.WorktreeLANsForSite(site.Name)
	if err != nil || len(entries) == 0 {
		return
	}
	for _, e := range entries {
		if liveBranches[e.Branch] {
			continue
		}
		action := "lan:unshare?branch=" + url.QueryEscape(e.Branch)
		if err := notifyDaemon(site.PrimaryDomain(), action); err != nil {
			closeLANShareServer(worktreeLANServerKey(e.Site, e.Branch))
			_, _, _ = config.RemoveWorktreeLAN(e.Site, e.Branch)
		}
	}
}

// vitePrefix is the URL prefix the LAN share proxy uses to forward requests
// to a per-site Vite dev server running on loopback. Body rewriting maps
// leaked loopback URLs like http://[::1]:5173/ to http://<lanHost><vitePrefix>5173/
// so LAN clients hit them through the share proxy (same-origin, WebSocket-capable).
const vitePrefix = "/__lerd_vite__/"

// startLANShareProxy starts an HTTP reverse proxy listening on 0.0.0.0:<port>.
// It rewrites the Host header to domain so nginx routes to the right vhost,
// sets X-Forwarded-Host to the incoming address, and rewrites response bodies
// and Location headers to replace domain URLs with the LAN address. Requests
// under vitePrefix are forwarded to a Vite dev server on loopback so dev-mode
// JS/CSS assets and the HMR websocket work from LAN devices.
func startLANShareProxy(domain string, port, httpPort, httpsPort int, secured bool) (*http.Server, error) {
	var target *url.URL
	if secured {
		target = &url.URL{Scheme: "https", Host: fmt.Sprintf("localhost:%d", httpsPort)}
	} else {
		target = &url.URL{Scheme: "http", Host: fmt.Sprintf("localhost:%d", httpPort)}
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	if secured {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true, ServerName: domain} //nolint:gosec
		proxy.Transport = t
	}

	const scheme = "http"

	orig := proxy.Director
	proxy.Director = func(req *http.Request) {
		lanHost := req.Host // e.g. "192.168.1.5:9100"
		orig(req)
		req.Header.Set("X-Forwarded-Host", lanHost)
		req.Header.Set("X-Forwarded-Proto", scheme)
		// Tell upstream the public port so frameworks don't fall back to
		// nginx's SERVER_PORT (e.g. 443 on the HTTPS vhost) when building
		// absolute URLs — that caused asset URLs like http://<ip>:443/foo.
		if _, p, err := net.SplitHostPort(lanHost); err == nil && p != "" {
			req.Header.Set("X-Forwarded-Port", p)
		}
		req.Host = domain
		// Tell upstream not to compress so we can rewrite bodies without
		// having to decompress/recompress every response.
		req.Header.Set("Accept-Encoding", "identity")
	}

	proxy.ModifyResponse = func(resp *http.Response) error {
		lanHost := resp.Request.Header.Get("X-Forwarded-Host")
		if lanHost == "" {
			return nil
		}

		// Rewrite Location headers: replace both http:// and https:// variants
		// of the origin domain with the plain-HTTP LAN address. The third pass
		// collapses https://<lanHost> redirects the app emitted itself (it
		// honored X-Forwarded-Host but forced an https scheme) so the browser
		// doesn't TLS-handshake the plain-HTTP proxy (ERR_SSL_PROTOCOL_ERROR).
		if loc := resp.Header.Get("Location"); loc != "" {
			loc = strings.ReplaceAll(loc, "https://"+domain, scheme+"://"+lanHost)
			loc = strings.ReplaceAll(loc, "http://"+domain, scheme+"://"+lanHost)
			loc = strings.ReplaceAll(loc, "https://"+lanHost, scheme+"://"+lanHost)
			resp.Header.Set("Location", loc)
		}

		ct := resp.Header.Get("Content-Type")
		enc := resp.Header.Get("Content-Encoding")

		if !isTextContent(ct) {
			return nil
		}

		// Read the body, decompressing gzip if needed.
		var body []byte
		var err error
		if enc == "gzip" {
			gr, gErr := gzip.NewReader(resp.Body)
			if gErr != nil {
				return nil // leave untouched if we can't decompress
			}
			body, err = io.ReadAll(gr)
			gr.Close()
			resp.Body.Close()
			if err != nil {
				return err
			}
			resp.Header.Del("Content-Encoding")
		} else if enc == "" || enc == "identity" {
			body, err = io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				return err
			}
		} else {
			return nil // unknown encoding, leave untouched
		}

		body = rewriteLANShareBody(body, domain, lanHost)

		resp.Body = io.NopCloser(bytes.NewReader(body))
		resp.ContentLength = int64(len(body))
		resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
		return nil
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return nil, fmt.Errorf("binding port %d: %w", port, err)
	}

	handler := newLANShareHandler(proxy)
	srv := &http.Server{Handler: handler}
	go srv.Serve(ln) //nolint:errcheck
	return srv, nil
}

// lanShareHandler dispatches LAN share requests: requests with the Vite
// prefix go to a per-port loopback reverse proxy, everything else flows to
// the main (nginx) proxy. Per-port proxies are cached to keep connection
// pools warm across requests. activeVitePort remembers the most recently
// seen Vite port so Vite-internal paths without our prefix still route to
// the dev server — module imports lose the prefix after one hop and the
// Referer alone can't be relied on.
type lanShareHandler struct {
	main http.Handler

	mu             sync.Mutex
	viteProxies    map[int]*httputil.ReverseProxy
	activeVitePort int
}

func newLANShareHandler(main http.Handler) *lanShareHandler {
	return &lanShareHandler{main: main, viteProxies: map[int]*httputil.ReverseProxy{}}
}

func (h *lanShareHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if port, rest, ok := parseVitePrefixPath(r.URL.Path); ok {
		r.URL.Path = rest
		r.URL.RawPath = "" // force re-encode from Path
		// The loopback body rewriter also catches non-Vite services running
		// on loopback (RustFS/S3 on :9000, mailpit, etc.). Only mark this
		// port as the active Vite when the stripped path actually looks
		// like Vite-served content — otherwise a RustFS image fetch would
		// poison activeVitePort and break subsequent transitive Vite
		// imports.
		if isViteInternalPath(r.URL) {
			h.setActiveVitePort(port)
		}
		h.viteProxy(port).ServeHTTP(w, r)
		return
	}
	// Vite's HMR client opens a WebSocket to the page origin with no
	// distinguishing path (just "/?token=..."). When we know the active
	// Vite port, forward any WS upgrade there so HMR can connect.
	if isWebSocketUpgrade(r) {
		if port := h.getActiveVitePort(); port > 0 {
			h.viteProxy(port).ServeHTTP(w, r)
			return
		}
	}
	// Vite-transformed JS imports absolute paths like /node_modules/... that
	// don't carry our prefix. Route them to the most recently active Vite
	// port for this share. Falls through to the main proxy if no Vite has
	// been observed yet. We deliberately do NOT trust the Referer header —
	// the share binds to 0.0.0.0 so any LAN device could forge a Referer
	// pointing at an arbitrary loopback port (SSRF). activeVitePort, set
	// only by genuine /__lerd_vite__/<port>/ path requests, already covers
	// transitive imports past the first hop.
	if isViteInternalPath(r.URL) {
		if port := h.getActiveVitePort(); port > 0 {
			h.viteProxy(port).ServeHTTP(w, r)
			return
		}
	}
	h.main.ServeHTTP(w, r)
}

// isWebSocketUpgrade reports whether r is an HTTP upgrade to WebSocket.
// Per RFC 6455 the request carries Connection: upgrade + Upgrade: websocket
// (header names case-insensitive, values can include other tokens).
func isWebSocketUpgrade(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	for _, t := range strings.Split(r.Header.Get("Connection"), ",") {
		if strings.EqualFold(strings.TrimSpace(t), "upgrade") {
			return true
		}
	}
	return false
}

func (h *lanShareHandler) setActiveVitePort(port int) {
	h.mu.Lock()
	h.activeVitePort = port
	h.mu.Unlock()
}

func (h *lanShareHandler) getActiveVitePort() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.activeVitePort
}

// vitePathPrefixes are URL prefixes Vite owns by convention (its own runtime,
// the node_modules resolver, framework source roots). When a request for one
// of these arrives without our /__lerd_vite__/<port>/ prefix we route it to
// the active Vite dev server so module resolution keeps working past the
// first hop (transitive imports drop the prefix from their URL).
var vitePathPrefixes = []string{
	"/@vite/",
	"/@id/",
	"/@fs/",
	"/@react-refresh",
	"/node_modules/",
	"/__vite_ping",
	"/__vite_dev",
	"/resources/", // Laravel project source root
	"/src/",       // common Vite/Vue/React convention
	"/vendor/",    // Vite aliases like ziggy-js → /vendor/tightenco/ziggy
}

// viteSourceExtensions are file extensions Vite serves in dev mode that
// nginx + Laravel never serve directly. A request ending in one of these
// goes to Vite whenever we know a port (even without a matching path prefix
// or Referer), since the alternative is a guaranteed 404 from the main app.
var viteSourceExtensions = []string{
	".vue",
	".ts", ".tsx",
	".jsx",
	".mjs", ".cjs",
	".svelte",
	".scss", ".sass", ".less", ".styl", ".stylus",
	".astro",
	".mdx",
}

// viteQueryHints are query-string markers Vite stamps on transformed
// modules. Their presence is a strong signal the URL is dev-only.
var viteQueryHints = []string{
	"import",
	"worker",
	"sharedworker",
	"raw",
	"inline",
	"url",
}

func isViteInternalPath(u *url.URL) bool {
	for _, p := range vitePathPrefixes {
		if strings.HasPrefix(u.Path, p) {
			return true
		}
	}
	if ext := pathExt(u.Path); ext != "" {
		for _, e := range viteSourceExtensions {
			if ext == e {
				return true
			}
		}
	}
	if u.RawQuery != "" && queryHasViteHint(u.RawQuery) {
		return true
	}
	return false
}

func pathExt(path string) string {
	i := strings.LastIndexByte(path, '.')
	if i < 0 {
		return ""
	}
	// Don't pick up a dot from an earlier path segment.
	if strings.IndexByte(path[i:], '/') >= 0 {
		return ""
	}
	return strings.ToLower(path[i:])
}

func queryHasViteHint(query string) bool {
	// Note: we deliberately do NOT match ?v=<hash>. Vite uses it for cache
	// busting, but so does every Laravel/Symfony/CDN URL out there. The
	// path-prefix and extension rules already cover Vite's legitimate
	// cases (e.g. /node_modules/...?v= is caught by /node_modules/).
	for _, part := range strings.Split(query, "&") {
		key := part
		if i := strings.IndexByte(part, '='); i >= 0 {
			key = part[:i]
		}
		for _, h := range viteQueryHints {
			if key == h {
				return true
			}
		}
	}
	return false
}

func (h *lanShareHandler) viteProxy(port int) *httputil.ReverseProxy {
	h.mu.Lock()
	defer h.mu.Unlock()
	if p, ok := h.viteProxies[port]; ok {
		return p
	}
	// Use "localhost" so Go's dual-stack dialer reaches Vite whether it bound
	// to 127.0.0.1, [::1], or both — vite defaults vary by version and OS.
	target := &url.URL{Scheme: "http", Host: fmt.Sprintf("localhost:%d", port)}
	p := httputil.NewSingleHostReverseProxy(target)
	orig := p.Director
	p.Director = func(req *http.Request) {
		// Capture the lanHost (incoming Host header) before the default
		// director rewrites req.URL — ModifyResponse needs it to rewrite
		// loopback URLs leaking through Vite-served content.
		lanHost := req.Host
		orig(req)
		// Pretend the request came from the dev server's own origin so Vite
		// doesn't reject it on Origin/Host checks.
		req.Host = target.Host
		req.Header.Set("Origin", "http://"+target.Host)
		if lanHost != "" {
			req.Header.Set("X-Forwarded-Host", lanHost)
			req.Header.Set("X-Forwarded-Proto", "http")
		}
	}
	p.ModifyResponse = func(resp *http.Response) error {
		// Vite responses that include absolute references back to the dev
		// server (e.g. import paths in transformed JS) need the same
		// loopback→prefix rewrite as the main proxy applies.
		ct := resp.Header.Get("Content-Type")
		if !isTextContent(ct) && !strings.Contains(strings.ToLower(ct), "javascript") {
			return nil
		}
		lanHost := resp.Request.Header.Get("X-Forwarded-Host")
		if lanHost == "" {
			return nil
		}
		body, err := readMaybeGzipped(resp)
		if err != nil || body == nil {
			return err
		}
		body = rewriteLoopbackViteURLs(body, lanHost)
		resp.Body = io.NopCloser(bytes.NewReader(body))
		resp.ContentLength = int64(len(body))
		resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
		return nil
	}
	h.viteProxies[port] = p
	return p
}

// parseVitePrefixPath returns the Vite target port and the remaining path
// (with a leading "/") when path starts with vitePrefix and the next segment
// parses as a port number. Returns ok=false otherwise.
func parseVitePrefixPath(path string) (port int, rest string, ok bool) {
	if !strings.HasPrefix(path, vitePrefix) {
		return 0, "", false
	}
	tail := path[len(vitePrefix):]
	slash := strings.IndexByte(tail, '/')
	var portStr string
	if slash == -1 {
		portStr = tail
		rest = "/"
	} else {
		portStr = tail[:slash]
		rest = tail[slash:]
	}
	p, err := strconv.Atoi(portStr)
	if err != nil || p <= 0 || p > 65535 {
		return 0, "", false
	}
	return p, rest, true
}

// readMaybeGzipped reads resp.Body, transparently decompressing gzip and
// stripping the Content-Encoding header. Returns nil body for unknown
// encodings so the caller can leave the response untouched.
func readMaybeGzipped(resp *http.Response) ([]byte, error) {
	enc := resp.Header.Get("Content-Encoding")
	switch enc {
	case "", "identity":
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		return body, err
	case "gzip":
		gr, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, nil
		}
		body, rerr := io.ReadAll(gr)
		gr.Close()
		resp.Body.Close()
		if rerr != nil {
			return nil, rerr
		}
		resp.Header.Del("Content-Encoding")
		return body, nil
	default:
		return nil, nil
	}
}

// PrintLANShareQR prints a compact QR code for the given URL to stdout using
// half-block Unicode characters (two rows per terminal line).
func PrintLANShareQR(rawURL string) {
	qr, err := qrcode.New(rawURL, qrcode.Medium)
	if err != nil {
		return
	}
	qr.DisableBorder = false
	bitmap := qr.Bitmap() // [][]bool, true = dark module

	// Render two rows of modules per terminal line using half-block chars.
	// Quiet zone is already included in Bitmap().
	for y := 0; y < len(bitmap); y += 2 {
		row1 := bitmap[y]
		var row2 []bool
		if y+1 < len(bitmap) {
			row2 = bitmap[y+1]
		} else {
			row2 = make([]bool, len(row1))
		}
		for x := range row1 {
			top := row1[x]
			bot := row2[x]
			switch {
			case top && bot:
				fmt.Fprint(os.Stdout, "█")
			case top:
				fmt.Fprint(os.Stdout, "▀")
			case bot:
				fmt.Fprint(os.Stdout, "▄")
			default:
				fmt.Fprint(os.Stdout, " ")
			}
		}
		fmt.Fprintln(os.Stdout)
	}
}

// LANShareURL returns the URL a LAN device would use to reach the site on the
// given port, or an empty string if the port is 0 or the LAN IP cannot be detected.
// rewriteLANShareBody collapses absolute URLs to http://<lanHost>. The third
// pass catches https://<lanHost> URLs Laravel emitted itself (X-Forwarded-Host
// honored, APP_URL forced https) so browsers don't TLS-handshake the proxy.
// The fourth pass fixes URLs that point at the LAN IP with the wrong port
// (e.g. http://<ip>:443/foo when the framework used SERVER_PORT from nginx).
// The final pass redirects loopback dev-server URLs (Vite on [::1]:5173 etc.)
// through the share proxy's Vite prefix so LAN devices can reach them.
func rewriteLANShareBody(body []byte, domain, lanHost string) []byte {
	body = bytes.ReplaceAll(body, []byte("https://"+domain), []byte("http://"+lanHost))
	body = bytes.ReplaceAll(body, []byte("http://"+domain), []byte("http://"+lanHost))
	body = bytes.ReplaceAll(body, []byte("https://"+lanHost), []byte("http://"+lanHost))
	if lanIP, _, err := net.SplitHostPort(lanHost); err == nil && lanIP != "" {
		// Terminator class covers HTML/JS quotes, JSON terminators, plus
		// `)` for CSS url(...) and `;` for CSS rules.
		re := regexp.MustCompile(`https?://` + regexp.QuoteMeta(lanIP) + `(?::\d+)?([/"'<>?#;)\s])`)
		body = re.ReplaceAll(body, []byte("http://"+lanHost+"$1"))
	}
	body = rewriteLoopbackViteURLs(body, lanHost)
	return body
}

// loopbackViteURLRe matches http(s)://<loopback>:<port> URLs that leaked into
// a response body. The first capture is the port, the second is the URL
// terminator (path slash, quote, etc.). Terminator class covers HTML/JS
// quotes, JSON terminators, plus `)` for CSS url(...) and `;` for CSS rules.
var loopbackViteURLRe = regexp.MustCompile(`https?://(?:localhost|127\.0\.0\.1|\[::1\]):(\d+)([/"'<>?#;)\s])`)

// loopbackViteURLEscRe matches the JSON-escaped form `http:\/\/host:port\/...`
// that ends up in Inertia.js page payloads (Laravel's json_encode default
// escapes forward slashes). Without this pass, framework URLs like
// "avatar_url":"http:\/\/localhost:9000\/..." in the data-page attribute
// never get rewritten and LAN devices try to load them from their own
// localhost. The terminator class covers JSON's `\/` separator plus the
// HTML-encoded `&quot;` (the `&` is the first byte) and bare quotes/angles.
var loopbackViteURLEscRe = regexp.MustCompile(`https?:\\/\\/(?:localhost|127\.0\.0\.1|\[::1\]):(\d+)(\\/|["'<>&])`)

// rewriteLoopbackViteURLs maps absolute loopback dev-server URLs onto
// http://<lanHost><vitePrefix><port>/... so the LAN device fetches them
// through the share proxy (which forwards them back to the dev server).
// URLs targeting the share port itself are left alone so we don't loop.
// Handles both the plain `http://` form and the JSON-escaped `http:\/\/`
// form that appears in Inertia.js data-page payloads.
func rewriteLoopbackViteURLs(body []byte, lanHost string) []byte {
	_, lanPort, err := net.SplitHostPort(lanHost)
	if err != nil {
		return body
	}
	body = loopbackViteURLRe.ReplaceAllFunc(body, func(match []byte) []byte {
		sub := loopbackViteURLRe.FindSubmatch(match)
		port, term := string(sub[1]), sub[2]
		if port == lanPort {
			return match
		}
		out := make([]byte, 0, len(lanHost)+len(vitePrefix)+len(port)+1+len(term)+len("http://"))
		out = append(out, "http://"...)
		out = append(out, lanHost...)
		out = append(out, vitePrefix...)
		out = append(out, port...)
		out = append(out, term...)
		return out
	})
	body = loopbackViteURLEscRe.ReplaceAllFunc(body, func(match []byte) []byte {
		sub := loopbackViteURLEscRe.FindSubmatch(match)
		port, term := string(sub[1]), sub[2]
		if port == lanPort {
			return match
		}
		// Build http:\/\/<lanHost>\/__lerd_vite__\/<port> keeping JSON
		// slash escaping consistent with the surrounding payload.
		out := []byte(`http:\/\/`)
		out = append(out, lanHost...)
		out = append(out, `\/__lerd_vite__\/`...)
		out = append(out, port...)
		// Re-emit the terminator. \/ stays as-is; quotes/angles/& stay too.
		out = append(out, term...)
		return out
	})
	return body
}

func LANShareURL(lanPort int) string {
	if lanPort == 0 {
		return ""
	}
	ip, err := detectPrimaryLANIP()
	if err != nil {
		return ""
	}
	return fmt.Sprintf("http://%s:%d", ip, lanPort)
}
