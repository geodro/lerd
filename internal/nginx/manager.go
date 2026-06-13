package nginx

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"text/template"
	"time"

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
	Resolver     string
	AccessSocket string
}

// VhostData is the data passed to vhost templates.
type VhostData struct {
	Domain          string // primary domain (used for config file naming)
	ServerNames     string // space-separated list of all domains for server_name directive
	Path            string
	PHPVersion      string
	PHPVersionShort string
	// FPMContainer is the container nginx fastcgi's to: the shared
	// lerd-php<ver>-fpm, or a per-site container for custom-FPM sites.
	FPMContainer    string
	CertDomain      string // domain whose cert files to use (defaults to Domain)
	PublicDir       string // document root subdirectory, e.g. "public", "web", "."
	Proxy           bool   // true when the site has a worker with WebSocket/HTTP proxy config
	ProxyPath       string // URL path for the proxy (e.g. "/app")
	ProxyPort       int    // port the worker listens on inside the PHP-FPM container
	CustomContainer string // container name for custom container sites (e.g. "lerd-custom-nestapp")
	CustomPort      int    // port the app listens on inside the custom container
	UpstreamHost    string // host-proxy upstream address (e.g. "host.containers.internal")
	UpstreamPort    int    // host-proxy upstream port (the dev server's host port)
	BackendSSL      bool   // proxy to the backend via HTTPS (app serves TLS on its own port)
	// LerdSite / LerdBranch surface the parent site name and (for worktrees)
	// the branch to PHP via fastcgi_param so the debug bridge can tag events
	// with stable identifiers instead of guessing from DOCUMENT_ROOT.
	LerdSite   string
	LerdBranch string
	// Profiling arms the SPX profiler for the site: when true the .php
	// location injects SPX_ENABLED=1 into HTTP_COOKIE so every request is
	// profiled. SPX_KEY is injected regardless (gated by the $spx_key map)
	// so the profiler UI is reachable.
	Profiling bool
	// RequestTimeout is the nginx request timeout in seconds rendered into the
	// fastcgi_*_timeout / proxy_*_timeout directives. Resolved per site by
	// resolveRequestTimeout (project .lerd.yaml, then global config, then 60s).
	RequestTimeout int
}

// resolveRequestTimeout returns the effective request timeout in seconds for
// the site at sitePath: project .lerd.yaml wins, then global config, then 60s.
// An empty sitePath skips the project lookup (site-less proxy vhosts).
func resolveRequestTimeout(sitePath string) int {
	if sitePath != "" {
		if pc, err := config.LoadProjectConfig(sitePath); err == nil && pc.RequestTimeout > 0 {
			return pc.RequestTimeout
		}
	}
	gc, err := config.LoadGlobal()
	if err != nil {
		return config.DefaultRequestTimeout
	}
	return gc.RequestTimeoutSeconds()
}

// phpShort converts "8.4" → "84".
func phpShort(version string) string {
	return strings.ReplaceAll(version, ".", "")
}

// profilerEnabled reports the global SPX profiler toggle. Vhosts inject
// SPX_ENABLED into FPM requests only when this is on.
func profilerEnabled() bool {
	cfg, err := config.LoadGlobal()
	return err == nil && cfg.IsProfilerEnabled()
}

// resolvePublicDir returns the document root subdirectory for a site.
// site.PublicDir wins (set from .lerd.yaml's public_dir, or from autodetect
// when no framework matched), then the framework definition's PublicDir, then
// "public" as the final fallback. Each candidate runs through ValidatePublicDir
// so a hostile .lerd.yaml can't pivot the nginx root out of the project.
func resolvePublicDir(site config.Site) string {
	if site.PublicDir != "" {
		if err := config.ValidatePublicDir(site.PublicDir); err == nil {
			return site.PublicDir
		}
	}
	if fw, ok := config.GetFrameworkForDir(site.Framework, site.Path); ok && fw.PublicDir != "" {
		if err := config.ValidatePublicDir(fw.PublicDir); err == nil {
			return fw.PublicDir
		}
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
		FPMContainer:    podman.FPMContainerName(site, phpVersion),
		PublicDir:       publicDir,
		Proxy:           hasProxy,
		ProxyPath:       proxyPath,
		ProxyPort:       proxyPort,
		LerdSite:        site.Name,
		Profiling:       profilerEnabled(),
		RequestTimeout:  resolveRequestTimeout(site.Path),
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
		FPMContainer:    podman.FPMContainerName(site, phpVersion),
		CertDomain:      site.PrimaryDomain(),
		PublicDir:       publicDir,
		Proxy:           hasProxy,
		ProxyPath:       proxyPath,
		ProxyPort:       proxyPort,
		LerdSite:        site.Name,
		Profiling:       profilerEnabled(),
		RequestTimeout:  resolveRequestTimeout(site.Path),
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

// GenerateFrankenPHPVhost renders the HTTP vhost template for a FrankenPHP
// site. Nginx reverse-proxies to the per-site lerd-fp-<name>:8000 container
// using the shared custom-container template.
func GenerateFrankenPHPVhost(site config.Site) error {
	tmplData, err := GetTemplate("vhost-custom.conf.tmpl")
	if err != nil {
		return err
	}
	tmpl, err := template.New("vhost-custom").Parse(string(tmplData))
	if err != nil {
		return err
	}

	data := VhostData{
		Domain:          site.PrimaryDomain(),
		ServerNames:     serverNamesWithWildcards(site.Domains),
		CustomContainer: podman.FrankenPHPContainerName(site.Name),
		CustomPort:      podman.FrankenPHPPort,
		RequestTimeout:  resolveRequestTimeout(site.Path),
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

// GenerateFrankenPHPSSLVhost renders the HTTPS vhost template for a FrankenPHP site.
func GenerateFrankenPHPSSLVhost(site config.Site) error {
	tmplData, err := GetTemplate("vhost-custom-ssl.conf.tmpl")
	if err != nil {
		return err
	}
	tmpl, err := template.New("vhost-custom-ssl").Parse(string(tmplData))
	if err != nil {
		return err
	}

	data := VhostData{
		Domain:          site.PrimaryDomain(),
		ServerNames:     serverNamesWithWildcards(site.Domains),
		CertDomain:      site.PrimaryDomain(),
		CustomContainer: podman.FrankenPHPContainerName(site.Name),
		CustomPort:      podman.FrankenPHPPort,
		RequestTimeout:  resolveRequestTimeout(site.Path),
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

// GenerateCustomVhost renders the HTTP vhost template for a custom container
// site and writes it to conf.d. Nginx reverse-proxies to the container instead
// of using fastcgi_pass.
func GenerateCustomVhost(site config.Site) error {
	tmplData, err := GetTemplate("vhost-custom.conf.tmpl")
	if err != nil {
		return err
	}

	tmpl, err := template.New("vhost-custom").Parse(string(tmplData))
	if err != nil {
		return err
	}

	data := VhostData{
		Domain:          site.PrimaryDomain(),
		ServerNames:     serverNamesWithWildcards(site.Domains),
		CustomContainer: podman.CustomContainerName(site.Name),
		CustomPort:      site.ContainerPort,
		BackendSSL:      site.ContainerSSL,
		RequestTimeout:  resolveRequestTimeout(site.Path),
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

// GenerateCustomSSLVhost renders the SSL vhost template for a custom container
// site and writes it to conf.d.
func GenerateCustomSSLVhost(site config.Site) error {
	tmplData, err := GetTemplate("vhost-custom-ssl.conf.tmpl")
	if err != nil {
		return err
	}

	tmpl, err := template.New("vhost-custom-ssl").Parse(string(tmplData))
	if err != nil {
		return err
	}

	data := VhostData{
		Domain:          site.PrimaryDomain(),
		ServerNames:     serverNamesWithWildcards(site.Domains),
		CertDomain:      site.PrimaryDomain(),
		CustomContainer: podman.CustomContainerName(site.Name),
		CustomPort:      site.ContainerPort,
		BackendSSL:      site.ContainerSSL,
		RequestTimeout:  resolveRequestTimeout(site.Path),
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

// hostProxyUpstream returns the host address nginx proxies a host-proxy site to.
// macOS resolves host.containers.internal via gvproxy; on Linux we reuse the
// routable gateway IP the probe cached in the hosts file (pure read, no podman).
func hostProxyUpstream() string {
	if runtime.GOOS == "darwin" {
		return "host.containers.internal"
	}
	if ip := podman.ReadHostGatewayFromFile(); ip != "" {
		return ip
	}
	return "host.containers.internal"
}

// GenerateHostProxyVhost renders the HTTP vhost for a host-proxy site and writes
// it to conf.d. Nginx reverse-proxies to a process on the host instead of a
// container.
func GenerateHostProxyVhost(site config.Site) error {
	return generateHostProxyVhost(site, "vhost-hostproxy.conf.tmpl", site.PrimaryDomain()+".conf", false)
}

// GenerateHostProxySSLVhost renders the SSL vhost for a host-proxy site and
// writes it to conf.d/<domain>-ssl.conf.
func GenerateHostProxySSLVhost(site config.Site) error {
	return generateHostProxyVhost(site, "vhost-hostproxy-ssl.conf.tmpl", site.PrimaryDomain()+"-ssl.conf", true)
}

func generateHostProxyVhost(site config.Site, tmplName, confName string, ssl bool) error {
	tmplData, err := GetTemplate(tmplName)
	if err != nil {
		return err
	}
	tmpl, err := template.New(tmplName).Parse(string(tmplData))
	if err != nil {
		return err
	}

	data := VhostData{
		Domain:         site.PrimaryDomain(),
		ServerNames:    serverNamesWithWildcards(site.Domains),
		UpstreamHost:   hostProxyUpstream(),
		UpstreamPort:   site.HostPort,
		BackendSSL:     site.HostSSL,
		RequestTimeout: resolveRequestTimeout(site.Path),
	}
	if ssl {
		data.CertDomain = site.PrimaryDomain()
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return err
	}

	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}
	confPath := filepath.Join(config.NginxConfD(), confName)
	return os.WriteFile(confPath, buf.Bytes(), 0644)
}

// GenerateWorktreeVhostFor picks GenerateWorktreeSSLVhost or GenerateWorktreeVhost
// based on the secured flag, so callers (scanWorktrees, syncWorktree,
// migrateWorktreeVhosts) don't repeat the if/else around the two
// underlying generators. parentDomain is consulted only on the SSL path.
// siteName + branch are forwarded so the worktree's PHP requests get tagged
// with LERD_SITE / LERD_BRANCH for dump grouping.
func GenerateWorktreeVhostFor(domain, path, phpVersion, parentDomain, siteName, branch string, secured bool) error {
	if secured {
		return GenerateWorktreeSSLVhost(domain, path, phpVersion, parentDomain, siteName, branch)
	}
	return GenerateWorktreeVhost(domain, path, phpVersion, siteName, branch)
}

// worktreeFPMContainer resolves the FPM container a worktree vhost points at:
// the parent site's per-site container for custom-FPM sites, otherwise the
// shared lerd-php<ver>-fpm.
func worktreeFPMContainer(siteName, phpVersion string) string {
	if site, _ := config.FindSite(siteName); site != nil {
		return podman.FPMContainerName(*site, phpVersion)
	}
	return "lerd-php" + phpShort(phpVersion) + "-fpm"
}

// GenerateWorktreeVhost renders the HTTP vhost template for a worktree checkout
// and writes it to conf.d/<domain>.conf.
func GenerateWorktreeVhost(domain, path, phpVersion, siteName, branch string) error {
	tmplData, err := GetTemplate("vhost.conf.tmpl")
	if err != nil {
		return err
	}

	tmpl, err := template.New("vhost").Parse(string(tmplData))
	if err != nil {
		return err
	}

	data := VhostData{
		Domain:          domain,
		ServerNames:     domain + " *." + domain,
		Path:            path,
		PHPVersion:      phpVersion,
		PHPVersionShort: phpShort(phpVersion),
		FPMContainer:    worktreeFPMContainer(siteName, phpVersion),
		PublicDir:       "public",
		LerdSite:        siteName,
		LerdBranch:      branch,
		Profiling:       profilerEnabled(),
		RequestTimeout:  resolveRequestTimeout(path),
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

// GenerateWorktreeSSLVhost renders the SSL vhost template for a worktree checkout,
// reusing the parent site's wildcard certificate (*.parentDomain).
func GenerateWorktreeSSLVhost(domain, path, phpVersion, parentDomain, siteName, branch string) error {
	tmplData, err := GetTemplate("vhost-ssl.conf.tmpl")
	if err != nil {
		return err
	}

	tmpl, err := template.New("vhost-ssl").Parse(string(tmplData))
	if err != nil {
		return err
	}

	data := VhostData{
		Domain:          domain,
		ServerNames:     domain + " *." + domain,
		Path:            path,
		PHPVersion:      phpVersion,
		PHPVersionShort: phpShort(phpVersion),
		FPMContainer:    worktreeFPMContainer(siteName, phpVersion),
		CertDomain:      parentDomain,
		PublicDir:       "public",
		LerdSite:        siteName,
		LerdBranch:      branch,
		Profiling:       profilerEnabled(),
		RequestTimeout:  resolveRequestTimeout(path),
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

// GenerateWorktreeHostProxyVhostFor renders a worktree's host-proxy vhost: nginx
// reverse-proxies the worktree domain to the dev server running on the host at
// upstreamPort (the worktree's own port). The HTTP/SSL choice mirrors
// GenerateWorktreeVhostFor, and the SSL variant reuses the parent's wildcard
// cert (*.parentDomain) just like the PHP worktree path.
func GenerateWorktreeHostProxyVhostFor(domain, path, parentDomain string, upstreamPort int, backendSSL, secured bool) error {
	data := VhostData{
		Domain:         domain,
		ServerNames:    domain + " *." + domain,
		UpstreamHost:   hostProxyUpstream(),
		UpstreamPort:   upstreamPort,
		BackendSSL:     backendSSL,
		RequestTimeout: resolveRequestTimeout(path),
	}
	tmplName := "vhost-hostproxy.conf.tmpl"
	if secured {
		tmplName = "vhost-hostproxy-ssl.conf.tmpl"
		data.CertDomain = parentDomain
	}

	tmplData, err := GetTemplate(tmplName)
	if err != nil {
		return err
	}
	tmpl, err := template.New(tmplName).Parse(string(tmplData))
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return err
	}
	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(config.NginxConfD(), domain+".conf"), buf.Bytes(), 0644)
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
    listen [::]:80;
    server_name %s;
    return 302 https://$host$request_uri;
}

server {
    listen 443 ssl;
    listen [::]:443 ssl;
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
    listen [::]:80;
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
    listen [::]:80;
    server_name %s;
    return 302 https://$host$request_uri;
}

server {
    listen 443 ssl;
    listen [::]:443 ssl;
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
    listen [::]:80;
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
	Domain         string
	UpstreamHost   string
	UpstreamPort   int
	RequestTimeout int
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
		Domain:         domain,
		UpstreamHost:   upstreamHost,
		UpstreamPort:   upstreamPort,
		RequestTimeout: resolveRequestTimeout(""),
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

// Test runs `nginx -t` inside the lerd-nginx container and returns the
// combined stdout+stderr output along with the exit error. nginx writes its
// per-directive validation diagnostics to stderr, so the output is the
// useful payload regardless of success or failure, and callers should
// surface it as-is.
//
// The exec is bounded by a 10 second context so a paused/stuck container
// cannot wedge the calling HTTP handler indefinitely; nginx -t against a
// healthy container completes in well under a second. On timeout the
// returned error wraps context.DeadlineExceeded and the buffer carries
// whatever podman managed to emit before the deadline fired.
func Test() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, podman.PodmanBin(), "exec", "lerd-nginx", "nginx", "-t")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := strings.TrimSpace(buf.String())
	if ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("nginx -t timed out after 10s: %w", ctx.Err())
	}
	return out, err
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
			var regenErr error
			switch {
			case site.IsHostProxy():
				regenErr = GenerateHostProxyVhost(site)
			case site.IsCustomContainer():
				regenErr = GenerateCustomVhost(site)
			case site.IsFrankenPHP():
				regenErr = GenerateFrankenPHPVhost(site)
			default:
				regenErr = GenerateVhost(site, site.PHPVersion)
			}
			if regenErr != nil {
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

// defaultVhostManagedHashSuffix is appended to the conf filename to form
// a sentinel that records the sha256 of what lerd last wrote. nginx
// ignores files that don't match its `*.conf` include glob, so the
// sentinel stays out of the way.
const defaultVhostManagedHashSuffix = ".lerd-managed-hash"

// EnsureDefaultVhost writes a catch-all default server that shows a branded
// error page for any HTTP request that doesn't match a registered site. For
// HTTPS we cannot serve a real catch-all because browsers (Chrome especially)
// reject TLD-level wildcard certificates like `*.test` with
// ERR_CERT_COMMON_NAME_INVALID, and we can't issue per-domain certs ahead of
// time. ssl_reject_handshake produces a clean connection error
// (ERR_SSL_UNRECOGNIZED_NAME_ALERT) which is the best UX available.
//
// The file is left alone when the user has manually edited it: lerd
// stores a sentinel hash of what it last wrote, and skips rewriting when
// the on-disk content no longer matches that hash. Removing the file (or
// the sentinel) restores lerd's automatic management.
func EnsureDefaultVhost() error {
	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}
	if err := writeErrorPages(); err != nil {
		return fmt.Errorf("writing error pages: %w", err)
	}

	canonical := renderDefaultVhost()
	path := filepath.Join(config.NginxConfD(), "_default.conf")
	sentinelPath := path + defaultVhostManagedHashSuffix

	canonicalHash := contentHashHex(canonical)

	onDisk, readErr := os.ReadFile(path)
	if errors.Is(readErr, os.ErrNotExist) {
		if err := WriteFileAtomic(path, canonical, 0644); err != nil {
			return err
		}
		return WriteFileAtomic(sentinelPath, []byte(canonicalHash), 0644)
	}
	if readErr != nil {
		return readErr
	}

	onDiskHash := contentHashHex(onDisk)
	lastWritten := strings.TrimSpace(readFileOrEmpty(sentinelPath))
	if lastWritten == "" {
		// Sentinel missing: sentinel-write crash (reclaim if hashes match),
		// pre-sentinel binary upgrade, or hand edit. The last two are
		// indistinguishable, so preserve and tell the user how to opt back in.
		if onDiskHash == canonicalHash {
			return WriteFileAtomic(sentinelPath, []byte(canonicalHash), 0644)
		}
		fmt.Printf("  [INFO] %s has no lerd sentinel; preserving on-disk content. If this is from a lerd upgrade and you haven't edited it, run: rm %s\n", path, path)
		return nil
	}
	if lastWritten != onDiskHash {
		fmt.Printf("  [INFO] %s differs from lerd's recorded last-write; preserving your edits. Remove the file to restore lerd's catch-all.\n", path)
		return nil
	}
	if onDiskHash == canonicalHash {
		return nil
	}
	// On-disk matches what lerd last wrote, but the template moved on.
	if err := WriteFileAtomic(path, canonical, 0644); err != nil {
		return err
	}
	return WriteFileAtomic(sentinelPath, []byte(canonicalHash), 0644)
}

// readFileOrEmpty returns the file's contents as a string, or "" on any
// error. Used for the sentinel read so a missing file and an unreadable
// file collapse to the same "no recorded last-write" state.
func readFileOrEmpty(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// WriteFileAtomic writes data to path via a sibling .tmp file followed by
// a rename, so a crash mid-write can never leave nginx pointing at a
// half-written conf. Preserves the destination file's mode if it already
// existed so an out-of-band chmod survives the rewrite; uses the caller's
// mode only when creating from scratch. Temp file is removed on any error.
func WriteFileAtomic(path string, data []byte, mode os.FileMode) error {
	effective := mode
	if info, err := os.Stat(path); err == nil {
		effective = info.Mode().Perm()
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, effective); err != nil {
		return err
	}
	if err := os.Chmod(tmp, effective); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// renderDefaultVhost returns the canonical _default.conf content.
// Separate from the writer so callers (and tests) can compute the same
// bytes lerd would write without touching disk.
func renderDefaultVhost() []byte {
	errorDir := config.ErrorPagesDir()
	return []byte(fmt.Sprintf(`server {
    listen 80 default_server;
    listen [::]:80 default_server;
    root %s;
    location / {
        try_files /404.html =404;
        default_type text/html;
    }
}
server {
    listen 443 default_server ssl;
    listen [::]:443 default_server ssl;
    ssl_reject_handshake on;
}
`, errorDir))
}

// contentHashHex is sha256 → hex, used as the managed-file sentinel value.
func contentHashHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
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
// The upstream differs by platform because container → host connectivity
// works differently on each:
//
//   - Linux: lerd-nginx runs in a rootless podman bridge. Reaching the
//     host over TCP via host.containers.internal depends on netavark /
//     pasta wiring up the 169.254.1.2 alias, which silently breaks
//     across podman versions and host network changes. We bind-mount
//     lerd-ui's unix socket into the container instead — filesystem
//     access only, no networking, no detection. lerd-ui marks
//     socket-arriving requests as loopback in isLoopbackRequest.
//
//   - macOS: lerd-ui runs as a native macOS process and lerd-nginx runs
//     inside the podman-machine VM. Unix sockets don't traverse the
//     virtio-fs / 9p hypervisor boundary as functional sockets, so
//     binding one on the macOS host doesn't help the VM. We fall back
//     to TCP via host.containers.internal:7073 — gvproxy reliably
//     forwards this on podman-machine, and the request carries an
//     X-Lerd-Trust header that the gate matches against the per-install
//     token (proxy_set_header overwrites any client-supplied value, so
//     a LAN attacker can't inject it).
//
// .localhost is RFC 6761 reserved and always resolves to the visiting
// device's loopback, so this vhost is unreachable from a LAN browser doing
// the obvious thing (http://lerd.localhost from a remote machine hits the
// remote machine's own 127.0.0.1, not the lerd server).
func EnsureLerdVhost() error {
	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}

	var content string
	if runtime.GOOS == "darwin" {
		token, err := LoadOrGenerateTrustToken()
		if err != nil {
			return fmt.Errorf("loading trust token: %w", err)
		}
		content = fmt.Sprintf(`server {
    listen 80;
    listen [::]:80;
    server_name lerd.localhost;

    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Lerd-Trust %s;

    location = / {
        proxy_pass http://host.containers.internal:7073;
    }

    location ^~ /icons/ {
        proxy_pass http://host.containers.internal:7073;
    }

    location ^~ /assets/ {
        proxy_pass http://host.containers.internal:7073;
    }

    location ^~ /_spx/ {
        proxy_pass http://host.containers.internal:7073;
    }

    location ^~ /_svc/ {
        proxy_pass http://host.containers.internal:7073;
    }

    location = /manifest.webmanifest {
        proxy_pass http://host.containers.internal:7073;
    }

    location = /sw.js {
        proxy_pass http://host.containers.internal:7073;
    }

    location = /offline.html {
        proxy_pass http://host.containers.internal:7073;
    }

    location / {
        return 444;
    }
}
`, token)
	} else {
		content = fmt.Sprintf(`server {
    listen 80;
    listen [::]:80;
    server_name lerd.localhost;

    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;

    location = / {
        proxy_pass http://unix:%[1]s:;
    }

    location ^~ /icons/ {
        proxy_pass http://unix:%[1]s:$request_uri;
    }

    location ^~ /assets/ {
        proxy_pass http://unix:%[1]s:$request_uri;
    }

    location ^~ /_spx/ {
        proxy_pass http://unix:%[1]s:$request_uri;
    }

    location ^~ /_svc/ {
        proxy_pass http://unix:%[1]s:$request_uri;
    }

    location = /manifest.webmanifest {
        proxy_pass http://unix:%[1]s:$request_uri;
    }

    location = /sw.js {
        proxy_pass http://unix:%[1]s:$request_uri;
    }

    location = /offline.html {
        proxy_pass http://unix:%[1]s:$request_uri;
    }

    location / {
        return 444;
    }
}
`, config.UISocketPath())
	}
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
	if err := EnsureCustomD(); err != nil {
		return err
	}
	if err := EnsureHttpD(); err != nil {
		return err
	}
	if err := EnsureForwardedConf(); err != nil {
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
		Resolver:     podman.NetworkGateway("lerd"),
		AccessSocket: config.AccessSocketPath(),
	}); err != nil {
		return fmt.Errorf("rendering nginx.conf: %w", err)
	}
	return os.WriteFile(destPath, buf.Bytes(), 0644)
}

// forwardedConf declares $real_forwarded_host / $real_forwarded_proto /
// $real_forwarded_port at http{} level. Each falls back to the local
// connection value when no X-Forwarded-* header is present, so direct
// browser access still works. The port map fixes URLs that frameworks
// (Ziggy, Symfony Request::getSchemeAndHttpHost) compute from
// SERVER_PORT — without it a tunneled/LAN-shared site emits absolute
// URLs with the nginx listen port (e.g. http://<ip>:443/foo).
const forwardedConf = `# Generated by lerd. Declares variables used by per-site vhosts.
# Edit user overrides in ~/.local/share/lerd/nginx/custom.d/ instead.
map $http_x_forwarded_host $real_forwarded_host {
    default $http_x_forwarded_host;
    ""      $host;
}

map $http_x_forwarded_proto $real_forwarded_proto {
    default $http_x_forwarded_proto;
    ""      $scheme;
}

map $http_x_forwarded_port $real_forwarded_port {
    default $http_x_forwarded_port;
    ""      $server_port;
}
`

// EnsureForwardedConf writes the shared _forwarded.conf snippet into
// conf.d. The "_" prefix makes it load before site vhosts.
func EnsureForwardedConf() error {
	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}
	key, err := config.LoadOrGenerateProfilerKey()
	if err != nil {
		return err
	}
	content := forwardedConf + spxKeyMap(key)
	return os.WriteFile(
		filepath.Join(config.NginxConfD(), "_forwarded.conf"),
		[]byte(content),
		0644,
	)
}

// spxKeyMap resolves $spx_key to the SPX http key for direct local requests
// and to an empty string when an X-Forwarded-Host header is present, so the
// SPX profiler stays unreachable through tunnels and LAN shares.
func spxKeyMap(key string) string {
	return fmt.Sprintf(`
map $http_x_forwarded_host $spx_key {
    default "";
    ""      %q;
}
`, key)
}

// EnsureProfilerVhost writes the profiler.localhost vhost: a dedicated
// hostname routed to a PHP-FPM container so SPX serves its report UI for the
// dashboard's global Profiler entry, independent of any site.
func EnsureProfilerVhost() error {
	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}
	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	// SCRIPT_FILENAME just needs a real file to exist; SPX intercepts the
	// SPX_UI_URI request and serves its UI before dump-bridge.php runs.
	content := fmt.Sprintf(`server {
    listen 80;
    listen [::]:80;
    server_name profiler.localhost;

    location / {
        set $fpm "lerd-php%s-fpm";
        fastcgi_pass $fpm:9000;
        include fastcgi_params;
        fastcgi_param SCRIPT_FILENAME /usr/local/etc/lerd/dump-bridge.php;
        fastcgi_param HTTP_COOKIE "SPX_KEY=$spx_key";
    }
}
`, phpShort(cfg.PHP.DefaultVersion))
	return os.WriteFile(filepath.Join(config.NginxConfD(), "_profiler.conf"), []byte(content), 0644)
}

// EnsureCustomD creates the user-override directory. Lerd never writes here
// after creation, so user snippets survive `lerd update`.
func EnsureCustomD() error {
	return os.MkdirAll(config.NginxCustomD(), 0755)
}

// EnsureHttpD creates the http.d directory for user http-level overrides so the
// nginx.conf `include /etc/nginx/http.d/*.conf;` always resolves, even before
// the user has added any override.
func EnsureHttpD() error {
	return os.MkdirAll(config.NginxHttpD(), 0755)
}

// RewriteNginxQuadlet rewrites lerd-nginx.container from the bundled template
// and reports whether the on-disk quadlet actually changed. Callers use this
// to bring installs that pre-date a template change (e.g. the new http.d
// mount) up to current shape on demand, instead of waiting for the next
// `lerd start` / `lerd update`. The http config editor calls it before
// writing the user override so the freshly written file is actually mounted
// into the running nginx container — without this heal, the file would be
// orphaned on disk and silently ignored.
func RewriteNginxQuadlet() (changed bool, err error) {
	content, err := podman.GetQuadletTemplate("lerd-nginx.container")
	if err != nil {
		return false, fmt.Errorf("reading bundled nginx quadlet template: %w", err)
	}
	return podman.WriteQuadletDiff("lerd-nginx", content)
}
