package nginx

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// detectSiteReverb returns true when the project at sitePath uses Laravel Reverb —
// either as a composer dependency or with BROADCAST_CONNECTION=reverb in .env/.env.example.
func detectSiteReverb(sitePath string) bool {
	if data, err := os.ReadFile(filepath.Join(sitePath, "composer.json")); err == nil {
		if strings.Contains(string(data), `"laravel/reverb"`) {
			return true
		}
	}
	for _, name := range []string{".env", ".env.example"} {
		if data, err := os.ReadFile(filepath.Join(sitePath, name)); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "#") {
					continue
				}
				if strings.EqualFold(line, "BROADCAST_CONNECTION=reverb") ||
					strings.EqualFold(line, `BROADCAST_CONNECTION="reverb"`) ||
					strings.EqualFold(line, `BROADCAST_CONNECTION='reverb'`) {
					return true
				}
			}
		}
	}
	return false
}

type nginxConfData struct {
	Resolver string
}

// VhostData is the data passed to vhost templates.
type VhostData struct {
	Domain          string
	Path            string
	PHPVersion      string
	PHPVersionShort string
	CertDomain      string // domain whose cert files to use (defaults to Domain)
	PublicDir       string // document root subdirectory, e.g. "public", "web", "."
	Reverb          bool   // true when the site uses Laravel Reverb (adds /app WebSocket proxy)
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
	if fw, ok := config.GetFramework(site.Framework); ok && fw.PublicDir != "" {
		return fw.PublicDir
	}
	return "public"
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

	data := VhostData{
		Domain:          site.Domain,
		Path:            site.Path,
		PHPVersion:      phpVersion,
		PHPVersionShort: phpShort(phpVersion),
		PublicDir:       publicDir,
		Reverb:          detectSiteReverb(site.Path),
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return err
	}

	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}
	confPath := filepath.Join(config.NginxConfD(), site.Domain+".conf")
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

	data := VhostData{
		Domain:          site.Domain,
		Path:            site.Path,
		PHPVersion:      phpVersion,
		PHPVersionShort: phpShort(phpVersion),
		CertDomain:      site.Domain,
		PublicDir:       publicDir,
		Reverb:          detectSiteReverb(site.Path),
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return err
	}

	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}
	confPath := filepath.Join(config.NginxConfD(), site.Domain+"-ssl.conf")
	return os.WriteFile(confPath, buf.Bytes(), 0644)
}

// GenerateWorktreeVhost renders the HTTP vhost template for a worktree checkout
// and writes it to conf.d/<domain>.conf.
func GenerateWorktreeVhost(domain, path, phpVersion string) error {
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
		Path:            path,
		PHPVersion:      phpVersion,
		PHPVersionShort: phpShort(phpVersion),
		PublicDir:       "public",
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
func GenerateWorktreeSSLVhost(domain, path, phpVersion, parentDomain string) error {
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
		Path:            path,
		PHPVersion:      phpVersion,
		PHPVersionShort: phpShort(phpVersion),
		CertDomain:      parentDomain,
		PublicDir:       "public",
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

// GeneratePausedVhost writes a minimal nginx vhost that serves the static paused
// landing page for the given site. For secured sites it also adds the HTTPS block
// so the redirect and TLS still work while the site is paused.
func GeneratePausedVhost(site config.Site) error {
	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}

	pausedDir := config.PausedDir()
	htmlFile := "/" + site.Domain + ".html"

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
        try_files %s =503;
        default_type text/html;
    }
}
`, site.Domain, site.Domain, site.Domain, site.Domain, pausedDir, htmlFile)
	} else {
		conf = fmt.Sprintf(`server {
    listen 80;
    server_name %s;
    root %s;
    location / {
        try_files %s =503;
        default_type text/html;
    }
}
`, site.Domain, pausedDir, htmlFile)
	}

	confPath := filepath.Join(config.NginxConfD(), site.Domain+".conf")
	if err := os.WriteFile(confPath, []byte(conf), 0644); err != nil {
		return err
	}
	// For secured sites the SSL vhost lives in a separate file; remove it so
	// nginx doesn't still route HTTPS requests to PHP-FPM while the site is paused.
	if site.Secured {
		_ = os.Remove(filepath.Join(config.NginxConfD(), site.Domain+"-ssl.conf"))
	}
	return nil
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

// EnsureDefaultVhost writes a catch-all default server that returns 444 for any
// request that doesn't match a registered site. This prevents nginx from falling
// back to the first alphabetical vhost for unknown hostnames.
func EnsureDefaultVhost() error {
	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}
	content := "server {\n    listen 80 default_server;\n    return 444;\n}\nserver {\n    listen 443 default_server ssl;\n    ssl_reject_handshake on;\n}\n"
	return os.WriteFile(filepath.Join(config.NginxConfD(), "_default.conf"), []byte(content), 0644)
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
