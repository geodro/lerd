package config

import (
	"fmt"
	"os"
	"strings"
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
	// Pinned excludes the site from idle-suspend: its workers stay running even
	// when the global idle policy is on, so a site you want always-warm never
	// sleeps.
	Pinned    bool   `yaml:"pinned,omitempty"`
	Framework string `yaml:"framework,omitempty"`
	PublicDir string `yaml:"public_dir,omitempty"`
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
	// ApprovedCommands holds exact command strings the user has consented to run
	// on the host for this site (project-origin custom host workers and commands).
	// Keyed by the exact string so a changed command re-prompts.
	ApprovedCommands []string `yaml:"approved_commands,omitempty"`
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
	// IdleSuspendedWorkers records the workers the idle engine gracefully
	// stopped while the site was quiet, so activity can resume them. Kept
	// distinct from PausedWorkers (manual `lerd pause`) so an automatic suspend
	// and a manual pause never clobber each other's restore list. Idle-suspend
	// itself is configured globally (config.yaml idle_suspend), not per site.
	IdleSuspendedWorkers []string `yaml:"idle_suspended_workers,omitempty"`

	// WorktreeIdleSuspended records, per git worktree (keyed by the worktree's
	// unit-slug base), the workers the idle engine stopped while that worktree was
	// quiet. Each worktree idles on its own timer, independent of the main site
	// and of the other worktrees, so its suspended set is tracked separately.
	WorktreeIdleSuspended map[string][]string `yaml:"worktree_idle_suspended,omitempty"`
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

// IsCustomFPM returns true when the site is a PHP project served by fastcgi
// from its own per-site image, built from a Containerfile (a container: config
// with no port). It is a normal PHP-FPM site whose container is per-site.
func (s *Site) IsCustomFPM() bool {
	return s.Runtime == "fpm-custom"
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
	Name                  string              `yaml:"name"`
	Domain                string              `yaml:"domain,omitempty"`  // legacy single domain
	Domains               []string            `yaml:"domains,omitempty"` // new multi-domain
	Path                  string              `yaml:"path"`
	PHPVersion            string              `yaml:"php_version"`
	NodeVersion           string              `yaml:"node_version"`
	Secured               bool                `yaml:"secured"`
	Ignored               bool                `yaml:"ignored,omitempty"`
	Paused                bool                `yaml:"paused,omitempty"`
	PausedWorkers         []string            `yaml:"paused_workers,omitempty"`
	Pinned                bool                `yaml:"pinned,omitempty"`
	Framework             string              `yaml:"framework,omitempty"`
	PublicDir             string              `yaml:"public_dir,omitempty"`
	AppURL                string              `yaml:"app_url,omitempty"`
	LANPort               int                 `yaml:"lan_port,omitempty"`
	ContainerPort         int                 `yaml:"container_port,omitempty"`
	ContainerSSL          bool                `yaml:"container_ssl,omitempty"`
	Runtime               string              `yaml:"runtime,omitempty"`
	RuntimeWorker         bool                `yaml:"runtime_worker,omitempty"`
	HostPort              int                 `yaml:"host_port,omitempty"`
	HostSSL               bool                `yaml:"host_ssl,omitempty"`
	HostCommand           string              `yaml:"host_command,omitempty"`
	ApprovedCommands      []string            `yaml:"approved_commands,omitempty"`
	Group                 string              `yaml:"group,omitempty"`
	GroupSubdomain        string              `yaml:"group_subdomain,omitempty"`
	GroupSharedDB         bool                `yaml:"group_shared_db,omitempty"`
	IdleSuspendedWorkers  []string            `yaml:"idle_suspended_workers,omitempty"`
	WorktreeIdleSuspended map[string][]string `yaml:"worktree_idle_suspended,omitempty"`
}

func (s Site) toYAML() siteYAML {
	return siteYAML{
		Name:                  s.Name,
		Domains:               s.Domains,
		Path:                  s.Path,
		PHPVersion:            s.PHPVersion,
		NodeVersion:           s.NodeVersion,
		Secured:               s.Secured,
		Ignored:               s.Ignored,
		Paused:                s.Paused,
		PausedWorkers:         s.PausedWorkers,
		Pinned:                s.Pinned,
		Framework:             s.Framework,
		PublicDir:             s.PublicDir,
		AppURL:                s.AppURL,
		LANPort:               s.LANPort,
		ContainerPort:         s.ContainerPort,
		ContainerSSL:          s.ContainerSSL,
		Runtime:               s.Runtime,
		RuntimeWorker:         s.RuntimeWorker,
		HostPort:              s.HostPort,
		HostSSL:               s.HostSSL,
		HostCommand:           s.HostCommand,
		ApprovedCommands:      s.ApprovedCommands,
		Group:                 s.Group,
		GroupSubdomain:        s.GroupSubdomain,
		GroupSharedDB:         s.GroupSharedDB,
		IdleSuspendedWorkers:  s.IdleSuspendedWorkers,
		WorktreeIdleSuspended: s.WorktreeIdleSuspended,
	}
}

func (sy siteYAML) toSite() Site {
	domains := sy.Domains
	if len(domains) == 0 && sy.Domain != "" {
		domains = []string{sy.Domain}
	}
	return Site{
		Name:                  sy.Name,
		Domains:               domains,
		Path:                  sy.Path,
		PHPVersion:            sy.PHPVersion,
		NodeVersion:           sy.NodeVersion,
		Secured:               sy.Secured,
		Ignored:               sy.Ignored,
		Paused:                sy.Paused,
		PausedWorkers:         sy.PausedWorkers,
		Pinned:                sy.Pinned,
		Framework:             sy.Framework,
		PublicDir:             sy.PublicDir,
		AppURL:                sy.AppURL,
		LANPort:               sy.LANPort,
		ContainerPort:         sy.ContainerPort,
		ContainerSSL:          sy.ContainerSSL,
		Runtime:               sy.Runtime,
		RuntimeWorker:         sy.RuntimeWorker,
		HostPort:              sy.HostPort,
		HostSSL:               sy.HostSSL,
		HostCommand:           sy.HostCommand,
		ApprovedCommands:      sy.ApprovedCommands,
		Group:                 sy.Group,
		GroupSubdomain:        sy.GroupSubdomain,
		GroupSharedDB:         sy.GroupSharedDB,
		IdleSuspendedWorkers:  sy.IdleSuspendedWorkers,
		WorktreeIdleSuspended: sy.WorktreeIdleSuspended,
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

// siteWriteMu serializes every read-modify-write of the registry (AddSite,
// RemoveSite, ReorderSites, IgnoreSite). Each does LoadSites -> mutate ->
// SaveSites; without one lock spanning the whole sequence, concurrent writers
// (the idle engine's goroutines, a CLI pin/pause, a worker toggle) interleave
// and clobber each other, which let one site's worker list bleed onto another.
var siteWriteMu sync.Mutex

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
		if s.IdleSuspendedWorkers != nil {
			cp.IdleSuspendedWorkers = append([]string(nil), s.IdleSuspendedWorkers...)
		}
		if s.WorktreeIdleSuspended != nil {
			cp.WorktreeIdleSuspended = make(map[string][]string, len(s.WorktreeIdleSuspended))
			for k, v := range s.WorktreeIdleSuspended {
				cp.WorktreeIdleSuspended[k] = append([]string(nil), v...)
			}
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
	// A site name flows into systemd unit file names and bodies (Description=,
	// --env=LERD_SITE=, lerd-stripe-<name>, ...). Refuse newline/NUL (which
	// would inject a unit directive) and slash (which would escape the unit
	// path), closing the injection even for callers that bypass SiteNameAndDomain.
	if ContainsUnitInjectionChars(site.Name) || strings.ContainsRune(site.Name, '/') {
		return fmt.Errorf("invalid site name %q: must not contain newline, NUL, or slash", site.Name)
	}
	siteWriteMu.Lock()
	defer siteWriteMu.Unlock()
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

// CommandApproved reports whether the user has consented to run command on the
// host for this site (see ApprovedCommands).
func (s Site) CommandApproved(command string) bool {
	for _, c := range s.ApprovedCommands {
		if c == command {
			return true
		}
	}
	return false
}

// HostCommandAllowed reports whether a project-supplied host command may run for
// a site without prompting. disabled is true when the global switch refuses all
// project host commands; otherwise allowed is true when the global skip is set or
// the user already approved this exact command for the site.
func HostCommandAllowed(siteName, command string) (allowed, disabled bool) {
	gcfg, _ := LoadGlobal()
	if gcfg.HostCommands.Disabled {
		return false, true
	}
	if gcfg.HostCommands.SkipConfirmation {
		return true, false
	}
	site, _ := FindSite(siteName)
	return site != nil && site.CommandApproved(command), false
}

// ApproveSiteCommand records that the user consented to run command on the host
// for the named site, so future runs and boot restore don't re-prompt. No-op if
// already recorded or the site is unknown.
func ApproveSiteCommand(siteName, command string) error {
	if command == "" {
		return nil
	}
	siteWriteMu.Lock()
	defer siteWriteMu.Unlock()
	reg, err := LoadSites()
	if err != nil {
		return err
	}
	for i := range reg.Sites {
		if reg.Sites[i].Name != siteName {
			continue
		}
		if reg.Sites[i].CommandApproved(command) {
			return nil
		}
		reg.Sites[i].ApprovedCommands = append(reg.Sites[i].ApprovedCommands, command)
		return SaveSites(reg)
	}
	return nil
}

// RemoveSite removes a site by name from the registry.
func RemoveSite(name string) error {
	siteWriteMu.Lock()
	defer siteWriteMu.Unlock()
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

// ReorderSites permutes the registry so its sites follow the given order of
// names. Names that don't match a site are ignored, and any site whose name is
// absent from order is kept and appended after the ordered ones in its original
// relative position, so paused sites and grouped secondaries are never dropped.
func ReorderSites(order []string) error {
	siteWriteMu.Lock()
	defer siteWriteMu.Unlock()
	reg, err := LoadSites()
	if err != nil {
		return err
	}

	byName := make(map[string]int, len(reg.Sites))
	for i, s := range reg.Sites {
		byName[s.Name] = i
	}

	out := make([]Site, 0, len(reg.Sites))
	placed := make([]bool, len(reg.Sites))
	for _, name := range order {
		if i, ok := byName[name]; ok && !placed[i] {
			out = append(out, reg.Sites[i])
			placed[i] = true
		}
	}
	for i, s := range reg.Sites {
		if !placed[i] {
			out = append(out, s)
		}
	}

	reg.Sites = out
	return SaveSites(reg)
}

// IgnoreSite marks a site as ignored (used for parked sites that have been unlinked).
func IgnoreSite(name string) error {
	siteWriteMu.Lock()
	defer siteWriteMu.Unlock()
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

// SetSiteIdleSuspendedWorkers atomically updates just a site's idle-suspended
// worker list. It reloads the record under the write lock and rewrites only that
// field, so a stale full-record write (FindSite -> mutate -> AddSite) can't clobber
// a concurrent change to another field like Paused or Pinned.
func SetSiteIdleSuspendedWorkers(name string, workers []string) error {
	siteWriteMu.Lock()
	defer siteWriteMu.Unlock()
	reg, err := LoadSites()
	if err != nil {
		return err
	}
	for i := range reg.Sites {
		if reg.Sites[i].Name == name {
			reg.Sites[i].IdleSuspendedWorkers = workers
			return SaveSites(reg)
		}
	}
	return fmt.Errorf("site %q not found", name)
}

// SetSitePinned atomically updates just a site's idle-suspend pin flag. Like
// SetSiteIdleSuspendedWorkers it rewrites only that field under the write lock, so
// `lerd idle pin/unpin` can't clobber a concurrent SetSiteIdleSuspendedWorkers
// write the idle engine makes for the same site.
func SetSitePinned(name string, pinned bool) error {
	siteWriteMu.Lock()
	defer siteWriteMu.Unlock()
	reg, err := LoadSites()
	if err != nil {
		return err
	}
	for i := range reg.Sites {
		if reg.Sites[i].Name == name {
			reg.Sites[i].Pinned = pinned
			return SaveSites(reg)
		}
	}
	return fmt.Errorf("site %q not found", name)
}

// SetWorktreeIdleSuspendedWorkers atomically updates a single worktree's
// idle-suspended worker list (keyed by the worktree's unit-slug base). Passing an
// empty list clears that worktree's entry, and clearing the last entry drops the
// map, so a resumed worktree leaves no residue in sites.yaml. Uses the same write
// lock as the other registry mutators.
func SetWorktreeIdleSuspendedWorkers(name, wtBase string, workers []string) error {
	siteWriteMu.Lock()
	defer siteWriteMu.Unlock()
	reg, err := LoadSites()
	if err != nil {
		return err
	}
	for i := range reg.Sites {
		if reg.Sites[i].Name != name {
			continue
		}
		m := reg.Sites[i].WorktreeIdleSuspended
		if len(workers) == 0 {
			delete(m, wtBase)
			if len(m) == 0 {
				reg.Sites[i].WorktreeIdleSuspended = nil
			}
		} else {
			if m == nil {
				m = map[string][]string{}
				reg.Sites[i].WorktreeIdleSuspended = m
			}
			m[wtBase] = workers
		}
		return SaveSites(reg)
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
