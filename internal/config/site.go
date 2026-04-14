package config

import (
	"fmt"
	"os"

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
	Name          string   `yaml:"name"`
	Domain        string   `yaml:"domain,omitempty"`  // legacy single domain
	Domains       []string `yaml:"domains,omitempty"` // new multi-domain
	Path          string   `yaml:"path"`
	PHPVersion    string   `yaml:"php_version"`
	NodeVersion   string   `yaml:"node_version"`
	Secured       bool     `yaml:"secured"`
	Ignored       bool     `yaml:"ignored,omitempty"`
	Paused        bool     `yaml:"paused,omitempty"`
	PausedWorkers []string `yaml:"paused_workers,omitempty"`
	Framework     string   `yaml:"framework,omitempty"`
	PublicDir     string   `yaml:"public_dir,omitempty"`
	AppURL        string   `yaml:"app_url,omitempty"`
	LANPort       int      `yaml:"lan_port,omitempty"`
}

func (s Site) toYAML() siteYAML {
	return siteYAML{
		Name:          s.Name,
		Domains:       s.Domains,
		Path:          s.Path,
		PHPVersion:    s.PHPVersion,
		NodeVersion:   s.NodeVersion,
		Secured:       s.Secured,
		Ignored:       s.Ignored,
		Paused:        s.Paused,
		PausedWorkers: s.PausedWorkers,
		Framework:     s.Framework,
		PublicDir:     s.PublicDir,
		AppURL:        s.AppURL,
		LANPort:       s.LANPort,
	}
}

func (sy siteYAML) toSite() Site {
	domains := sy.Domains
	if len(domains) == 0 && sy.Domain != "" {
		domains = []string{sy.Domain}
	}
	return Site{
		Name:          sy.Name,
		Domains:       domains,
		Path:          sy.Path,
		PHPVersion:    sy.PHPVersion,
		NodeVersion:   sy.NodeVersion,
		Secured:       sy.Secured,
		Ignored:       sy.Ignored,
		Paused:        sy.Paused,
		PausedWorkers: sy.PausedWorkers,
		Framework:     sy.Framework,
		PublicDir:     sy.PublicDir,
		AppURL:        sy.AppURL,
		LANPort:       sy.LANPort,
	}
}

// SiteRegistry holds all registered sites.
type SiteRegistry struct {
	Sites []Site
}

type siteRegistryYAML struct {
	Sites []siteYAML `yaml:"sites"`
}

// LoadSites reads sites.yaml, returning an empty registry if the file does not exist.
func LoadSites() (*SiteRegistry, error) {
	data, err := os.ReadFile(SitesFile())
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
	return reg, nil
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
	return os.WriteFile(SitesFile(), data, 0644)
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
