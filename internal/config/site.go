package config

import (
	"fmt"
	"os"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Site represents a single registered Lerd site.
type Site struct {
	Name          string   `yaml:"-"`
	Domains       []string `yaml:"-"`
	Path          string   `yaml:"path"`
	PHPVersion    string   `yaml:"php_version"`
	NodeVersion   string   `yaml:"node_version"`
	Secured       bool     `yaml:"secured"`
	Ignored       bool     `yaml:"ignored,omitempty"`
	Paused        bool     `yaml:"paused,omitempty"`
	PausedWorkers []string `yaml:"paused_workers,omitempty"`
	Framework     string   `yaml:"framework,omitempty"`
	PublicDir     string   `yaml:"public_dir,omitempty"`
	// AppURL, when set, is the per-machine override for APP_URL in the
	// project's env file. Lower priority than ProjectConfig.AppURL (which is
	// committed to the repo) and higher priority than the default generator
	// (`<scheme>://<primary-domain>`). Use this for personal customizations
	// you don't want to share via .lerd.yaml.
	AppURL string `yaml:"app_url,omitempty"`
	// LANPort, when non-zero, means a host-level reverse proxy is (or should
	// be) listening on 0.0.0.0:LANPort, forwarding to the site with the Host
	// header rewritten. LAN devices can reach the site at <lanIP>:LANPort
	// without any DNS configuration.
	LANPort int `yaml:"lan_port,omitempty"`
	// ContainerPort, when non-zero, means this site uses a per-project custom
	// container instead of the shared PHP-FPM image. The value is the port the
	// app listens on inside the container; nginx reverse-proxies to it.
	ContainerPort int `yaml:"container_port,omitempty"`
	// ContainerSSL, when true, means the app inside the custom container serves
	// TLS on its port; nginx will proxy_pass via HTTPS with ssl_verify off.
	ContainerSSL bool `yaml:"container_ssl,omitempty"`
	// Runtime is "fpm" (default) or "frankenphp". When "frankenphp" the site
	// runs a per-site dunglas/frankenphp:php<version> container and nginx
	// reverse-proxies to it on port 8000.
	Runtime string `yaml:"runtime,omitempty"`
	// RuntimeWorker toggles FrankenPHP worker mode when Runtime=="frankenphp".
	RuntimeWorker bool `yaml:"runtime_worker,omitempty"`
	// HostPort, when non-zero, means this site is a host-proxy site: it has no
	// container, and nginx reverse-proxies the domain to a process running on
	// the host (the dev server) listening on this port.
	HostPort int `yaml:"host_port,omitempty"`
	// HostSSL, when true, means the host process serves TLS on its port; nginx
	// proxies via HTTPS with ssl_verify off.
	HostSSL bool `yaml:"host_ssl,omitempty"`
	// HostCommand is the dev command lerd supervises for a host-proxy site
	// (e.g. "npm run start:dev"). Empty means proxy-only: the user runs the
	// server themselves and lerd only wires the proxy.
	HostCommand string `yaml:"host_command,omitempty"`
	// Group is the group key shared by a main site and its secondaries. It is
	// set to the main site's name. Empty when the site is not grouped.
	Group string `yaml:"group,omitempty"`
	// GroupSubdomain is the subdomain label a secondary occupies on the group
	// main's base domain (e.g. "admin" -> admin.<main-domain>). Empty on the
	// main site; non-empty identifies a secondary.
	GroupSubdomain string `yaml:"group_subdomain,omitempty"`
	// GroupSharedDB, when true on a secondary, means the site shares the group
	// main's database instead of its own: DB_DATABASE in its .env is kept in
	// sync with the main's database name.
	GroupSharedDB bool `yaml:"group_shared_db,omitempty"`
}

// IsGroupMain returns true when the site owns a group's base domain: it has a
// group key but no subdomain of its own.
func (s *Site) IsGroupMain() bool {
	return s.Group != "" && s.GroupSubdomain == ""
}

// IsGroupSecondary returns true when the site occupies a subdomain of its
// group main's base domain.
func (s *Site) IsGroupSecondary() bool {
	return s.Group != "" && s.GroupSubdomain != ""
}

// IsCustomContainer returns true when the site uses a per-project custom
// container instead of the shared PHP-FPM image.
func (s *Site) IsCustomContainer() bool {
	return s.ContainerPort > 0
}

// IsFrankenPHP returns true when the site is served by a per-site
// dunglas/frankenphp container instead of the shared PHP-FPM image.
func (s *Site) IsFrankenPHP() bool {
	return s.Runtime == "frankenphp"
}

// IsHostProxy returns true when the site is a host-proxy site: nginx
// reverse-proxies the domain to a host process instead of a container.
func (s *Site) IsHostProxy() bool {
	return s.HostPort > 0
}

// HostProxyWorkerName is the worker name of a host-proxy site's supervised
// dev server. There is exactly one per site.
const HostProxyWorkerName = "app"

// HostProxyWorkerUnit returns the worker unit name for a host-proxy site's dev
// server (lerd-app-<site>). Single source of truth for the cli (which starts
// and stops it) and siteinfo (which reports its health).
func HostProxyWorkerUnit(siteName string) string {
	return "lerd-" + HostProxyWorkerName + "-" + siteName
}

// PrimaryDomain returns the first (primary) domain for the site.
func (s *Site) PrimaryDomain() string {
	if len(s.Domains) > 0 {
		return s.Domains[0]
	}
	return ""
}

// HasDomain returns true if the site has the given domain.
func (s *Site) HasDomain(domain string) bool {
	for _, d := range s.Domains {
		if d == domain {
			return true
		}
	}
	return false
}

// siteYAML is the on-disk YAML representation of a Site, supporting both the
// legacy single "domain" field and the new "domains" array.
type siteYAML struct {
	Name           string   `yaml:"name"`
	Domain         string   `yaml:"domain,omitempty"`  // legacy single domain
	Domains        []string `yaml:"domains,omitempty"` // new multi-domain
	Path           string   `yaml:"path"`
	PHPVersion     string   `yaml:"php_version"`
	NodeVersion    string   `yaml:"node_version"`
	Secured        bool     `yaml:"secured"`
	Ignored        bool     `yaml:"ignored,omitempty"`
	Paused         bool     `yaml:"paused,omitempty"`
	PausedWorkers  []string `yaml:"paused_workers,omitempty"`
	Framework      string   `yaml:"framework,omitempty"`
	PublicDir      string   `yaml:"public_dir,omitempty"`
	AppURL         string   `yaml:"app_url,omitempty"`
	LANPort        int      `yaml:"lan_port,omitempty"`
	ContainerPort  int      `yaml:"container_port,omitempty"`
	ContainerSSL   bool     `yaml:"container_ssl,omitempty"`
	Runtime        string   `yaml:"runtime,omitempty"`
	RuntimeWorker  bool     `yaml:"runtime_worker,omitempty"`
	HostPort       int      `yaml:"host_port,omitempty"`
	HostSSL        bool     `yaml:"host_ssl,omitempty"`
	HostCommand    string   `yaml:"host_command,omitempty"`
	Group          string   `yaml:"group,omitempty"`
	GroupSubdomain string   `yaml:"group_subdomain,omitempty"`
	GroupSharedDB  bool     `yaml:"group_shared_db,omitempty"`
}

func (s Site) toYAML() siteYAML {
	return siteYAML{
		Name:           s.Name,
		Domains:        s.Domains,
		Path:           s.Path,
		PHPVersion:     s.PHPVersion,
		NodeVersion:    s.NodeVersion,
		Secured:        s.Secured,
		Ignored:        s.Ignored,
		Paused:         s.Paused,
		PausedWorkers:  s.PausedWorkers,
		Framework:      s.Framework,
		PublicDir:      s.PublicDir,
		AppURL:         s.AppURL,
		LANPort:        s.LANPort,
		ContainerPort:  s.ContainerPort,
		ContainerSSL:   s.ContainerSSL,
		Runtime:        s.Runtime,
		RuntimeWorker:  s.RuntimeWorker,
		HostPort:       s.HostPort,
		HostSSL:        s.HostSSL,
		HostCommand:    s.HostCommand,
		Group:          s.Group,
		GroupSubdomain: s.GroupSubdomain,
		GroupSharedDB:  s.GroupSharedDB,
	}
}

func (sy siteYAML) toSite() Site {
	domains := sy.Domains
	if len(domains) == 0 && sy.Domain != "" {
		domains = []string{sy.Domain}
	}
	return Site{
		Name:           sy.Name,
		Domains:        domains,
		Path:           sy.Path,
		PHPVersion:     sy.PHPVersion,
		NodeVersion:    sy.NodeVersion,
		Secured:        sy.Secured,
		Ignored:        sy.Ignored,
		Paused:         sy.Paused,
		PausedWorkers:  sy.PausedWorkers,
		Framework:      sy.Framework,
		PublicDir:      sy.PublicDir,
		AppURL:         sy.AppURL,
		LANPort:        sy.LANPort,
		ContainerPort:  sy.ContainerPort,
		ContainerSSL:   sy.ContainerSSL,
		Runtime:        sy.Runtime,
		RuntimeWorker:  sy.RuntimeWorker,
		HostPort:       sy.HostPort,
		HostSSL:        sy.HostSSL,
		HostCommand:    sy.HostCommand,
		Group:          sy.Group,
		GroupSubdomain: sy.GroupSubdomain,
		GroupSharedDB:  sy.GroupSharedDB,
	}
}

// SiteRegistry holds all registered sites.
type SiteRegistry struct {
	Sites []Site
}

type siteRegistryYAML struct {
	Sites []siteYAML `yaml:"sites"`
}

// sitesCache memoises the parsed registry keyed on sites.yaml's mtime+size.
// The daemon's snapshot path used to re-read and re-parse sites.yaml once per
// snapshot rebuild via LoadAll; with many sites this dominated the YAML parse
// cost. The cache returns a freshly-allocated registry so callers can mutate
// the slice without poisoning the cached value.
var (
	sitesCacheMu sync.Mutex
	sitesCache   *SiteRegistry
	sitesCacheAt time.Time
	sitesCacheSz int64
)

func invalidateSitesCache() {
	sitesCacheMu.Lock()
	sitesCache = nil
	sitesCacheAt = time.Time{}
	sitesCacheSz = 0
	sitesCacheMu.Unlock()
}

// LoadSites reads sites.yaml, returning an empty registry if the file does not exist.
func LoadSites() (*SiteRegistry, error) {
	path := SitesFile()
	info, statErr := os.Stat(path)

	sitesCacheMu.Lock()
	if sitesCache != nil && statErr == nil &&
		sitesCacheAt.Equal(info.ModTime()) && sitesCacheSz == info.Size() {
		out := cloneSiteRegistry(sitesCache)
		sitesCacheMu.Unlock()
		return out, nil
	}
	sitesCacheMu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &SiteRegistry{}, nil
		}
		return nil, err
	}

	var raw siteRegistryYAML
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	reg := &SiteRegistry{Sites: make([]Site, len(raw.Sites))}
	for i, sy := range raw.Sites {
		reg.Sites[i] = sy.toSite()
	}

	if statErr == nil {
		sitesCacheMu.Lock()
		sitesCache = cloneSiteRegistry(reg)
		sitesCacheAt = info.ModTime()
		sitesCacheSz = info.Size()
		sitesCacheMu.Unlock()
	}
	return reg, nil
}

func cloneSiteRegistry(in *SiteRegistry) *SiteRegistry {
	if in == nil {
		return &SiteRegistry{}
	}
	out := &SiteRegistry{Sites: make([]Site, len(in.Sites))}
	for i, s := range in.Sites {
		cp := s
		if s.Domains != nil {
			cp.Domains = append([]string(nil), s.Domains...)
		}
		if s.PausedWorkers != nil {
			cp.PausedWorkers = append([]string(nil), s.PausedWorkers...)
		}
		out.Sites[i] = cp
	}
	return out
}

// SaveSites writes the registry to sites.yaml.
func SaveSites(reg *SiteRegistry) error {
	if err := os.MkdirAll(DataDir(), 0755); err != nil {
		return err
	}

	raw := siteRegistryYAML{Sites: make([]siteYAML, len(reg.Sites))}
	for i, s := range reg.Sites {
		raw.Sites[i] = s.toYAML()
	}

	data, err := yaml.Marshal(raw)
	if err != nil {
		return err
	}
	if err := os.WriteFile(SitesFile(), data, 0644); err != nil {
		return err
	}
	invalidateSitesCache()
	return nil
}

// AddSite appends or updates a site in the registry.
func AddSite(site Site) error {
	reg, err := LoadSites()
	if err != nil {
		return err
	}

	for i, s := range reg.Sites {
		if s.Name == site.Name {
			reg.Sites[i] = site
			return SaveSites(reg)
		}
	}

	reg.Sites = append(reg.Sites, site)
	return SaveSites(reg)
}

// RemoveSite removes a site by name from the registry.
func RemoveSite(name string) error {
	reg, err := LoadSites()
	if err != nil {
		return err
	}

	filtered := reg.Sites[:0]
	for _, s := range reg.Sites {
		if s.Name != name {
			filtered = append(filtered, s)
		}
	}
	reg.Sites = filtered
	return SaveSites(reg)
}

// IgnoreSite marks a site as ignored (used for parked sites that have been unlinked).
func IgnoreSite(name string) error {
	reg, err := LoadSites()
	if err != nil {
		return err
	}

	for i, s := range reg.Sites {
		if s.Name == name {
			reg.Sites[i].Ignored = true
			return SaveSites(reg)
		}
	}
	return fmt.Errorf("site %q not found", name)
}

// FindSite returns the site with the given name, or an error if not found.
func FindSite(name string) (*Site, error) {
	reg, err := LoadSites()
	if err != nil {
		return nil, err
	}

	for _, s := range reg.Sites {
		if s.Name == name {
			s := s
			return &s, nil
		}
	}
	return nil, fmt.Errorf("site %q not found", name)
}

// FindSiteByPath returns the site whose path matches, or an error if not found.
func FindSiteByPath(path string) (*Site, error) {
	reg, err := LoadSites()
	if err != nil {
		return nil, err
	}

	for _, s := range reg.Sites {
		if s.Path == path {
			s := s
			return &s, nil
		}
	}
	return nil, fmt.Errorf("site with path %q not found", path)
}

// FindSiteByDomain returns the site that has the given domain (checks all domains),
// or an error if not found.
func FindSiteByDomain(domain string) (*Site, error) {
	reg, err := LoadSites()
	if err != nil {
		return nil, err
	}

	for _, s := range reg.Sites {
		if s.HasDomain(domain) {
			s := s
			return &s, nil
		}
	}
	return nil, fmt.Errorf("site with domain %q not found", domain)
}

// IsDomainUsed checks if any site already uses this domain.
// Returns the site that uses it, or nil if the domain is free.
//
// The check is strict: a domain may only belong to one site, regardless of
// TLS scheme. Two sites cannot share the same domain even if one runs on
// HTTPS and the other on HTTP — DNS and browser caches don't reliably
// disambiguate by scheme, and the resulting setup is fragile.
func IsDomainUsed(domain string) (*Site, error) {
	reg, err := LoadSites()
	if err != nil {
		return nil, err
	}

	for _, s := range reg.Sites {
		if s.HasDomain(domain) {
			s := s
			return &s, nil
		}
	}
	return nil, nil
}
