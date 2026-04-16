package nginx

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
	"github.com/geodro/lerd/internal/podman"
)

// detectSiteProxy checks the site's framework definition for a worker with a
// proxy configuration. Returns the proxy path and port if found.
func detectSiteProxy(site config.Site) (path string, port int, ok bool) {
	fw, fwOK := config.GetFrameworkForDir(site.Framework, site.Path)
	if !fwOK {
		return "", 0, false
	}
	proxy, _ := fw.DetectProxy(site.Path)
	if proxy == nil {
		return "", 0, false
	}
	proxyPort := proxy.DefaultPort
	if proxyPort == 0 {
		proxyPort = 8080
	}
	if proxy.PortEnvKey != "" {
		if v := envfile.ReadKey(filepath.Join(site.Path, ".env"), proxy.PortEnvKey); v != "" {
			if p, err := strconv.Atoi(v); err == nil && p > 0 {
				proxyPort = p
			}
		}
	}
	return proxy.Path, proxyPort, true
}

type nginxConfData struct {
	Resolver string
}

// VhostData is the data passed to vhost templates.
type VhostData struct {
	Domain          string // primary domain (used for config file naming)
	ServerNames     string // space-separated list of all domains for server_name directive
	Path            string
	PHPVersion      string
	PHPVersionShort string
	CertDomain      string // domain whose cert files to use (defaults to Domain)
	PublicDir       string // document root subdirectory, e.g. "public", "web", "."
	Proxy           bool   // true when the site has a worker with WebSocket/HTTP proxy config
	ProxyPath       string // URL path for the proxy (e.g. "/app")
	ProxyPort       int    // port the worker listens on inside the PHP-FPM container
}

// phpShort converts "8.4" → "84".
func phpShort(version string) string {
	return strings.ReplaceAll(version, ".", "")
}

// resolvePublicDir returns the document root subdirectory for a site.
// site.PublicDir takes precedence (set when no framework matched at link time),
// then the framework definition's PublicDir, then "public" as a default.
func resolvePublicDir(site config.Site) string {
	if site.PublicDir != "" {
		return site.PublicDir
	}
	if fw, ok := config.GetFrameworkForDir(site.Framework, site.Path); ok && fw.PublicDir != "" {
		return fw.PublicDir
	}
	return "public"
}

// serverNamesWithWildcards returns a space-separated list of all domains plus
// a *.domain wildcard for each, so subdomains are routed to the site too.
// Worktree subdomains take priority because they have their own vhost with an
// exact server_name (nginx prefers exact over wildcard).
func serverNamesWithWildcards(domains []string) string {
	var parts []string
	for _, d := range domains {
		parts = append(parts, d, "*."+d)
	}
	return strings.Join(parts, " ")
}

// GenerateVhost renders the HTTP vhost template and writes it to conf.d.
func GenerateVhost(site config.Site, phpVersion string) error {
	tmplData, err := GetTemplate("vhost.conf.tmpl")
	if err != nil {
		return err
	}

	tmpl, err := template.New("vhost").Parse(string(tmplData))
	if err != nil {
		return err
	}

	publicDir := resolvePublicDir(site)
	serverNames := serverNamesWithWildcards(site.Domains)

	proxyPath, proxyPort, hasProxy := detectSiteProxy(site)
	data := VhostData{
		Domain:          site.PrimaryDomain(),
		ServerNames:     serverNames,
		Path:            site.Path,
		PHPVersion:      phpVersion,
		PHPVersionShort: phpShort(phpVersion),
		PublicDir:       publicDir,
		Proxy:           hasProxy,
		ProxyPath:       proxyPath,
		ProxyPort:       proxyPort,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return err
	}

	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}
	confPath := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+".conf")
	return os.WriteFile(confPath, buf.Bytes(), 0644)
}

// GenerateSSLVhost renders the SSL vhost template and writes it to conf.d.
func GenerateSSLVhost(site config.Site, phpVersion string) error {
	tmplData, err := GetTemplate("vhost-ssl.conf.tmpl")
	if err != nil {
		return err
	}

	tmpl, err := template.New("vhost-ssl").Parse(string(tmplData))
	if err != nil {
		return err
	}

	publicDir := resolvePublicDir(site)
	serverNames := serverNamesWithWildcards(site.Domains)

	proxyPath, proxyPort, hasProxy := detectSiteProxy(site)
	data := VhostData{
		Domain:          site.PrimaryDomain(),
		ServerNames:     serverNames,
		Path:            site.Path,
		PHPVersion:      phpVersion,
		PHPVersionShort: phpShort(phpVersion),
		CertDomain:      site.PrimaryDomain(),
		PublicDir:       publicDir,
		Proxy:           hasProxy,
		ProxyPath:       proxyPath,
		ProxyPort:       proxyPort,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return err
	}

	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}
	confPath := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+"-ssl.conf")
	return os.WriteFile(confPath, buf.Bytes(), 0644)
}

// GenerateWorktreeVhost renders the HTTP vhost template for a worktree checkout
// and writes it to conf.d/<domain>.conf.
func GenerateWorktreeVhost(site config.Site, domain, path, phpVersion string) error {
	tmplData, err := GetTemplate("vhost.conf.tmpl")
	if err != nil {
		return err
	}

	tmpl, err := template.New("vhost").Parse(string(tmplData))
	if err != nil {
		return err
	}

	data := worktreeVhostData(site, domain, path, phpVersion)

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return err
	}

	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}
	confPath := filepath.Join(config.NginxConfD(), domain+".conf")
	return os.WriteFile(confPath, buf.Bytes(), 0644)
}

// GenerateWorktreeSSLVhost renders the SSL vhost template for a worktree checkout,
// reusing the parent site's wildcard certificate (*.parentDomain).
func GenerateWorktreeSSLVhost(site config.Site, domain, path, phpVersion string) error {
	tmplData, err := GetTemplate("vhost-ssl.conf.tmpl")
	if err != nil {
		return err
	}

	tmpl, err := template.New("vhost-ssl").Parse(string(tmplData))
	if err != nil {
		return err
	}

	data := worktreeVhostData(site, domain, path, phpVersion)
	data.CertDomain = site.PrimaryDomain()

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return err
	}

	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}
	confPath := filepath.Join(config.NginxConfD(), domain+".conf")
	return os.WriteFile(confPath, buf.Bytes(), 0644)
}

func worktreeVhostData(site config.Site, domain, path, phpVersion string) VhostData {
	worktreeSite := site
	worktreeSite.Path = path
	worktreeSite.Domains = []string{domain}

	proxyPath, proxyPort, hasProxy := detectSiteProxy(worktreeSite)
	return VhostData{
		Domain:          domain,
		ServerNames:     domain,
		Path:            path,
		PHPVersion:      phpVersion,
		PHPVersionShort: phpShort(phpVersion),
		PublicDir:       resolvePublicDir(worktreeSite),
		Proxy:           hasProxy,
		ProxyPath:       proxyPath,
		ProxyPort:       proxyPort,
	}
}

// GeneratePausedVhost writes a minimal nginx vhost that serves the static paused
// landing page for the given site. For secured sites it also adds the HTTPS block
// so the redirect and TLS still work while the site is paused.
func GeneratePausedVhost(site config.Site) error {
	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}

	pausedDir := config.PausedDir()
	serverNames := serverNamesWithWildcards(site.Domains)

	var conf string
	if site.Secured {
		conf = fmt.Sprintf(`server {
    listen 80;
    server_name %s;
    return 302 https://$host$request_uri;
}

server {
    listen 443 ssl;
    server_name %s;
    ssl_certificate /etc/nginx/certs/%s.crt;
    ssl_certificate_key /etc/nginx/certs/%s.key;
    root %s;
    location / {
        try_files /paused.html =503;
        default_type text/html;
    }
}
`, serverNames, serverNames, site.PrimaryDomain(), site.PrimaryDomain(), pausedDir)
	} else {
		conf = fmt.Sprintf(`server {
    listen 80;
    server_name %s;
    root %s;
    location / {
        try_files /paused.html =503;
        default_type text/html;
    }
}
`, serverNames, pausedDir)
	}

	confPath := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+".conf")
	if err := os.WriteFile(confPath, []byte(conf), 0644); err != nil {
		return err
	}
	// For secured sites the SSL vhost lives in a separate file; remove it so
	// nginx doesn't still route HTTPS requests to PHP-FPM while the site is paused.
	if site.Secured {
		_ = os.Remove(filepath.Join(config.NginxConfD(), site.PrimaryDomain()+"-ssl.conf"))
	}
	return nil
}

// GeneratePausedWorktreeVhost writes a paused nginx vhost for a worktree domain.
// certDomain is the parent site's domain whose cert files back the wildcard.
func GeneratePausedWorktreeVhost(domain, certDomain, pausedDir string, secured bool) error {
	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}

	var conf string
	if secured {
		conf = fmt.Sprintf(`server {
    listen 80;
    server_name %s;
    return 302 https://$host$request_uri;
}

server {
    listen 443 ssl;
    server_name %s;
    ssl_certificate /etc/nginx/certs/%s.crt;
    ssl_certificate_key /etc/nginx/certs/%s.key;
    root %s;
    location / {
        try_files /paused.html =503;
        default_type text/html;
    }
}
`, domain, domain, certDomain, certDomain, pausedDir)
	} else {
		conf = fmt.Sprintf(`server {
    listen 80;
    server_name %s;
    root %s;
    location / {
        try_files /paused.html =503;
        default_type text/html;
    }
}
`, domain, pausedDir)
	}

	confPath := filepath.Join(config.NginxConfD(), domain+".conf")
	return os.WriteFile(confPath, []byte(conf), 0644)
}

// RemoveVhost deletes the vhost config files for the given domain.
func RemoveVhost(domain string) error {
	confD := config.NginxConfD()
	for _, suffix := range []string{".conf", "-ssl.conf"} {
		path := filepath.Join(confD, domain+suffix)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// proxyVhostData is the template data for vhost-proxy.conf.tmpl.
type proxyVhostData struct {
	Domain       string
	UpstreamHost string
	UpstreamPort int
}

// GenerateProxyVhost renders vhost-proxy.conf.tmpl and writes conf.d/{domain}.conf.
func GenerateProxyVhost(domain, upstreamHost string, upstreamPort int) error {
	tmplData, err := GetTemplate("vhost-proxy.conf.tmpl")
	if err != nil {
		return err
	}

	tmpl, err := template.New("vhost-proxy").Parse(string(tmplData))
	if err != nil {
		return err
	}

	data := proxyVhostData{
		Domain:       domain,
		UpstreamHost: upstreamHost,
		UpstreamPort: upstreamPort,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return err
	}

	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}
	confPath := filepath.Join(config.NginxConfD(), domain+".conf")
	return os.WriteFile(confPath, buf.Bytes(), 0644)
}

// Reload signals nginx to reload its configuration.
func Reload() error {
	_, err := podman.Run("exec", "lerd-nginx", "nginx", "-s", "reload")
	return err
}

// VhostRepair describes a single vhost that was repaired during pre-flight.
type VhostRepair struct {
	Domain string
	Reason string // "missing-cert" or "orphan-ssl"
}

// RepairVhosts performs pre-flight validation of nginx vhost configs before start.
// It fixes SSL vhosts that reference cert files that don't exist on the host:
//
//   - If the domain belongs to a registered site, the vhost is regenerated as
//     plain HTTP and the site registry is updated (Secured = false).
//   - If no matching site exists (orphan SSL vhost), the config is removed.
//
// Plain HTTP vhosts are left untouched even if they don't match any site — they
// are harmless and may belong to worktrees, parked sites, or ignored sites.
func RepairVhosts() []VhostRepair {
	certsDir := filepath.Join(config.CertsDir(), "sites")
	confDir := config.NginxConfD()
	entries, err := os.ReadDir(confDir)
	if err != nil {
		return nil
	}

	reg, err := config.LoadSites()
	if err != nil {
		return nil
	}

	var repairs []VhostRepair
	dirty := false

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".conf") {
			continue
		}
		// Skip internal configs (default catch-all and lerd dashboard proxy).
		if entry.Name() == "_default.conf" || entry.Name() == "lerd.localhost.conf" {
			continue
		}

		confPath := filepath.Join(confDir, entry.Name())
		domain := strings.TrimSuffix(entry.Name(), ".conf")

		data, err := os.ReadFile(confPath)
		if err != nil {
			continue
		}

		// Only act on vhosts with missing TLS certificates — those crash nginx.
		if !hasMissingCert(string(data), certsDir) {
			continue
		}

		repaired := false
		for i, site := range reg.Sites {
			if site.PrimaryDomain() != domain || !site.Secured {
				continue
			}
			// Regenerate as plain HTTP vhost.
			if err := GenerateVhost(site, site.PHPVersion); err != nil {
				continue
			}
			reg.Sites[i].Secured = false
			dirty = true
			repaired = true
			repairs = append(repairs, VhostRepair{Domain: domain, Reason: "missing-cert"})
			os.Remove(filepath.Join(certsDir, domain+".crt")) //nolint:errcheck
			os.Remove(filepath.Join(certsDir, domain+".key")) //nolint:errcheck
			break
		}
		if !repaired {
			// No matching site — orphan SSL vhost with missing cert, remove it.
			os.Remove(confPath) //nolint:errcheck
			repairs = append(repairs, VhostRepair{Domain: domain, Reason: "orphan-ssl"})
		}
	}

	if dirty {
		config.SaveSites(reg) //nolint:errcheck
	}

	return repairs
}

// hasMissingCert returns true if the vhost content contains an ssl_certificate
// directive pointing to a cert file that doesn't exist on the host.
func hasMissingCert(content, certsDir string) bool {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ssl_certificate ") {
			continue
		}
		certPath := strings.TrimSuffix(strings.TrimPrefix(line, "ssl_certificate "), ";")
		certPath = strings.TrimSpace(certPath)
		hostPath := filepath.Join(certsDir, filepath.Base(certPath))
		if _, err := os.Stat(hostPath); os.IsNotExist(err) {
			return true
		}
	}
	return false
}

// EnsureDefaultVhost writes a catch-all default server that shows a branded
// error page for any HTTP request that doesn't match a registered site. For
// HTTPS we cannot serve a real catch-all because browsers (Chrome especially)
// reject TLD-level wildcard certificates like `*.test` with
// ERR_CERT_COMMON_NAME_INVALID, and we can't issue per-domain certs ahead of
// time. ssl_reject_handshake produces a clean connection error
// (ERR_SSL_UNRECOGNIZED_NAME_ALERT) which is the best UX available.
func EnsureDefaultVhost() error {
	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}

	// Write the error page HTML.
	if err := writeErrorPages(); err != nil {
		return fmt.Errorf("writing error pages: %w", err)
	}

	errorDir := config.ErrorPagesDir()
	content := fmt.Sprintf(`server {
    listen 80 default_server;
    root %s;
    location / {
        try_files /404.html =404;
        default_type text/html;
    }
}
server {
    listen 443 default_server ssl;
    ssl_reject_handshake on;
}
`, errorDir)
	return os.WriteFile(filepath.Join(config.NginxConfD(), "_default.conf"), []byte(content), 0644)
}

const errorPageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Site Not Found — Lerd</title>
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
    code {
      background: #262a36;
      padding: 0.15rem 0.4rem;
      border-radius: 4px;
      font-size: 0.8rem;
      font-family: ui-monospace, 'Cascadia Code', monospace;
      color: #e5e7eb;
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
    .btn-primary:hover { background: #e02419; }
    .btn-secondary { background: #262a36; color: #e5e7eb; border: 1px solid #2d3142; }
    .btn-secondary:hover { background: #2d3142; }
  </style>
</head>
<body>
  <div class="card">
    <div class="logo">L</div>
    <h1>Site Not Found</h1>
    <p class="host" id="host"></p>
    <p>This domain is not linked to any site. Run <code>lerd link</code> in your project directory to register it.</p>
    <div class="actions">
      <a id="dashboard-link" href="#" class="btn-primary">Open Dashboard</a>
      <button class="btn-secondary" onclick="location.reload()">Retry</button>
    </div>
  </div>
  <script>
    document.getElementById('host').textContent = location.hostname;
    // The dashboard runs on lerd-ui at port 7073 on the same host the visitor
    // already reached. Using location.hostname (rather than a hardcoded
    // lerd.localhost) means LAN clients get a working link to the server's
    // address, not their own loopback.
    document.getElementById('dashboard-link').href = location.protocol + '//' + location.hostname + ':7073/';
  </script>
</body>
</html>
`

// writeErrorPages ensures the error page HTML files exist in the error pages directory.
func writeErrorPages() error {
	dir := config.ErrorPagesDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "404.html"), []byte(errorPageHTML), 0644)
}

// EnsureLerdVhost generates the nginx vhost for http://lerd.localhost,
// which reverse-proxies to the lerd-ui process running on the host so the
// browser's URL bar stays on lerd.localhost (no redirect to localhost:7073).
//
// Background: lerd-nginx runs in a rootless podman bridge, so any outbound
// connection it makes to a host service arrives with a non-loopback source
// IP (the bridge gateway, e.g. 10.89.7.1). Without further context, lerd-ui
// cannot tell a legitimate proxy hop from this vhost apart from a LAN
// attacker hitting http://server-ip:7073 directly.
//
// We bridge that gap with a per-install random trust token (see
// trust_token.go) injected via `proxy_set_header X-Lerd-Trust <token>;`.
// Two properties make this safe against header injection:
//
//  1. `proxy_set_header` REPLACES any client-supplied X-Lerd-Trust value,
//     so a LAN attacker who sets the header in their own request has it
//     overwritten by nginx before it reaches lerd-ui. The only header value
//     that ever reaches lerd-ui is the legitimate one nginx put there.
//  2. The token lives in ~/.local/share/lerd/nginx-trust-token with mode
//  0600. An off-host attacker cannot read it.
//
// .localhost is RFC 6761 reserved and always resolves to the visiting
// device's loopback, so this vhost is unreachable from a LAN browser doing
// the obvious thing (http://lerd.localhost from a remote machine hits the
// remote machine's own 127.0.0.1, not the lerd server).
func EnsureLerdVhost() error {
	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}

	token, err := LoadOrGenerateTrustToken()
	if err != nil {
		return fmt.Errorf("loading trust token: %w", err)
	}

	// This vhost serves ONLY static dashboard assets (HTML, icons, manifest).
	// /api/* is intentionally NOT proxied — clients must hit lerd-ui on
	// :7073 directly, where loopback enforcement actually works. The
	// dashboard JavaScript detects when it was loaded from lerd.localhost
	// and rewrites all API/EventSource calls to absolute http://localhost:7073
	// URLs, which the browser sends directly over loopback (bypassing nginx
	// and the auth gate via the loopback peer check).
	//
	// host.containers.internal is podman's standard alias for the host
	// gateway from inside a rootless container. lerd-ui binds 0.0.0.0:7073
	// so it's reachable via that path. The X-Lerd-Trust header injected
	// below identifies the static-asset proxy hop as a legitimate nginx
	// request so lerd-ui's gate serves the HTML — it does NOT grant the
	// proxied request access to /api/* because nginx returns 444 for any
	// path outside the explicit static allowlist before it ever reaches
	// the proxy.
	content := fmt.Sprintf(`server {
    listen 80;
    server_name lerd.localhost;

    # Shared proxy settings for the static asset locations below.
    # Trust token (X-Lerd-Trust) identifies this hop as nginx-on-the-host
    # so lerd-ui's gate serves the asset; proxy_set_header OVERWRITES any
    # client-supplied value, so a LAN attacker cannot inject it.
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Lerd-Trust %s;

    # The HTML shell.
    location = / {
        proxy_pass http://host.containers.internal:7073;
    }

    # Embedded SVG/PNG icons used by the dashboard and the PWA manifest.
    location ^~ /icons/ {
        proxy_pass http://host.containers.internal:7073;
    }

    # The PWA manifest.
    location = /manifest.webmanifest {
        proxy_pass http://host.containers.internal:7073;
    }

    # Everything else (notably /api/*) is closed. Clients must reach
    # the API directly on http://localhost:7073 where the loopback gate
    # is enforced at the TCP layer.
    location / {
        return 444;
    }
}
`, token)
	return os.WriteFile(filepath.Join(config.NginxConfD(), "lerd.localhost.conf"), []byte(content), 0644)
}

// EnsureNginxConfig copies the base nginx.conf to the data dir if it is missing.
func EnsureNginxConfig() error {
	nginxDir := config.NginxDir()
	if err := os.MkdirAll(nginxDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}

	destPath := filepath.Join(nginxDir, "nginx.conf")
	tmplData, err := GetTemplate("nginx.conf")
	if err != nil {
		return fmt.Errorf("failed to read embedded nginx.conf: %w", err)
	}
	tmpl, err := template.New("nginx.conf").Parse(string(tmplData))
	if err != nil {
		return fmt.Errorf("parsing nginx.conf template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, nginxConfData{
		Resolver: podman.NetworkGateway("lerd"),
	}); err != nil {
		return fmt.Errorf("rendering nginx.conf: %w", err)
	}
	return os.WriteFile(destPath, buf.Bytes(), 0644)
}
