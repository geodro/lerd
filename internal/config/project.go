package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultHostMySQLSocket is the unix socket a host-installed (system)
// MySQL/MariaDB server listens on by default on Debian/Ubuntu. Used when
// db.external is set but db.socket is not specified. See DefaultHostDBSocketPath
// for the OS-aware default (macOS Homebrew uses a different path).
const DefaultHostMySQLSocket = "/run/mysqld/mysqld.sock"

// defaultHostMySQLSocketDarwin is the conventional Homebrew MySQL/MariaDB socket
// on macOS. Used only when probing/displaying; macOS host-mode connects over TCP
// (see HostDBUsesTCP) because the socket can't cross the podman-machine boundary.
const defaultHostMySQLSocketDarwin = "/tmp/mysql.sock"

// Host-mode (db.external) DB transport on macOS. Unix sockets don't traverse the
// podman-machine virtio-fs boundary as functional sockets (the same constraint
// that puts lerd's debug-dump bridge on TCP — see paths.go DumpsBridgeTarget), so
// containerized PHP reaches the host server over TCP via gvproxy's host alias
// instead of a bind-mounted socket.
const (
	HostDBTCPHost = "host.containers.internal"
	HostDBTCPPort = "3306"
)

// hostDBGOOS is runtime.GOOS, indirected so tests can exercise both the Linux
// (socket) and macOS (TCP) host-DB transports regardless of the test host.
var hostDBGOOS = runtime.GOOS

// HostDBUsesTCP reports whether host-mode (db.external) connections use TCP
// (macOS) rather than a unix socket (Linux). On macOS the host socket is not
// reachable from inside the podman-machine VM, so the connection goes over TCP
// to HostDBTCPHost:HostDBTCPPort, which gvproxy forwards to the host's loopback.
func HostDBUsesTCP() bool { return hostDBGOOS == "darwin" }

// DefaultHostDBSocketPath returns the conventional host MySQL/MariaDB unix socket
// path for the current OS (Debian/Ubuntu on Linux, Homebrew on macOS).
func DefaultHostDBSocketPath() string {
	if hostDBGOOS == "darwin" {
		return defaultHostMySQLSocketDarwin
	}
	return DefaultHostMySQLSocket
}

// SetHostDBGOOSForTest overrides the OS used for host-DB transport/socket-path
// decisions and returns a restore func. Test-only.
func SetHostDBGOOSForTest(goos string) func() {
	prev := hostDBGOOS
	hostDBGOOS = goos
	return func() { hostDBGOOS = prev }
}

// DB backend identifiers, shared by the per-site switch and the global
// default. "container" is lerd's own MySQL/MariaDB container (db.external
// unset); "host" is the system MySQL reached over its unix socket
// (db.external set). The empty/zero value normalises to container.
const (
	DBBackendContainer = "container"
	DBBackendHost      = "host"
)

// ProjectDB holds optional database targeting info for the project.
// Setting these in .lerd.yaml lets db commands work without a .env file,
// which is useful for non-PHP projects (NestJS, Go, etc.).
type ProjectDB struct {
	Service  string `yaml:"service,omitempty"`
	Database string `yaml:"database,omitempty"`
	// External, when true, points the project at a host-installed (system)
	// database instead of lerd's containerized one. For MySQL/MariaDB the
	// containerized PHP reaches the host server over its unix socket (bind-
	// mounted into the FPM container), so `lerd env` emits DB_HOST=localhost +
	// DB_SOCKET and lerd does not start the lerd-mysql container. Opt-in,
	// per-project, committed to .lerd.yaml. See Socket.
	External bool `yaml:"external,omitempty"`
	// Socket overrides the host database unix socket path used when External
	// is set. Empty defaults to DefaultHostMySQLSocket.
	Socket string `yaml:"socket,omitempty"`
}

// HostSocketPath returns the host database unix socket to use in external
// mode: the configured Socket, or DefaultHostMySQLSocket when unset.
func (d ProjectDB) HostSocketPath() string {
	if d.Socket != "" {
		return d.Socket
	}
	return DefaultHostDBSocketPath()
}

// ContainerConfig holds per-project custom container settings. When present
// in .lerd.yaml the site gets its own dedicated container built from the
// user's Containerfile, and nginx reverse-proxies to it instead of using
// the shared PHP-FPM image.
type ContainerConfig struct {
	Port          int    `yaml:"port"`                    // port the app listens on inside the container (required)
	Containerfile string `yaml:"containerfile,omitempty"` // path to Containerfile, default "Containerfile.lerd"
	BuildContext  string `yaml:"build_context,omitempty"` // build context directory, default "."
	Target        string `yaml:"target,omitempty"`        // multi-stage build target passed as --target to podman build
	SSL           bool   `yaml:"ssl,omitempty"`           // proxy to the container via HTTPS (app serves TLS on its port)
}

// ProxyConfig holds per-project host-proxy settings. When present in
// .lerd.yaml the site runs no container: lerd supervises the dev command on
// the host and nginx reverse-proxies the domain to it on the given port.
type ProxyConfig struct {
	Command    string `yaml:"command,omitempty"`      // dev command, e.g. "npm run start:dev"; empty = proxy-only
	Port       int    `yaml:"port"`                   // host port the app binds (required)
	SSL        bool   `yaml:"ssl,omitempty"`          // app serves TLS on its port; proxy via https
	PortEnvKey string `yaml:"port_env_key,omitempty"` // env var the port is injected as (default "PORT")
	HostEnvKey string `yaml:"host_env_key,omitempty"` // env var the bind address is injected as (default "HOST")
	InjectHost *bool  `yaml:"inject_host,omitempty"`  // inject the bind-address env (HOST=0.0.0.0) so the server listens on all interfaces; nil/unset = true, set false to opt out
}

// ProjectConfig holds per-project configuration stored in .lerd.yaml.
type ProjectConfig struct {
	Domains     []string `yaml:"domains,omitempty"`
	PHPVersion  string   `yaml:"php_version,omitempty"`
	NodeVersion string   `yaml:"node_version,omitempty"`
	// JSRuntime pins the JavaScript runtime for this project's host workers and
	// install/build steps: "bun" forces bun, "node" (or "npm") forces Node/npm
	// and opts out of the no-Node bun fallback. Empty auto-detects bun from a
	// bun.lockb / bunfig.toml / packageManager field. Use "node" for apps bun
	// can't run (e.g. NestJS with native addons). See node.UsesBun.
	JSRuntime        string     `yaml:"js_runtime,omitempty"`
	Framework        string     `yaml:"framework,omitempty"`
	FrameworkVersion string     `yaml:"framework_version,omitempty"`
	FrameworkDef     *Framework `yaml:"framework_def,omitempty"`
	// PublicDir overrides the framework's default document-root subdirectory
	// for this project, e.g. "public_html" for a Laravel skeleton that doesn't
	// use the conventional "public/" folder. Empty means use the framework
	// default. Wins over Framework.PublicDir at nginx-config time.
	PublicDir     string                     `yaml:"public_dir,omitempty"`
	Secured       bool                       `yaml:"secured,omitempty"`
	Services      []ProjectService           `yaml:"services,omitempty"`
	Workers       []string                   `yaml:"workers,omitempty"`
	CustomWorkers map[string]FrameworkWorker `yaml:"custom_workers,omitempty"`
	// ReloadWorkers lists worker names this project runs in auto-reload mode
	// (restart on file changes) for development, for workers that declare a
	// reload_command variant. Per project so one project can develop with
	// reload while another stays in standard mode. Laravel's horizon worker is
	// currently the only worker with a reload variant.
	ReloadWorkers []string `yaml:"reload_workers,omitempty"`
	// Commands extends or overrides the framework's command set. Entries with
	// a Name matching a framework command replace it (set Disabled: true to
	// suppress instead). Entries with a new Name are appended. See
	// ResolveCommands for the merge logic.
	Commands []FrameworkCommand `yaml:"commands,omitempty"`
	// AppURL, when set, is the value lerd writes to the project's APP_URL (or
	// the framework-configured URL key) on every `lerd env` run. Committed to
	// the repo so the choice is shared across machines. Takes precedence over
	// the per-machine override in sites.yaml.
	AppURL    string           `yaml:"app_url,omitempty"`
	DB        ProjectDB        `yaml:"db,omitempty"`
	Container *ContainerConfig `yaml:"container,omitempty"`
	// Proxy, when set, makes this a host-proxy site (see ProxyConfig). Mutually
	// exclusive with Container; Validate rejects setting both.
	Proxy *ProxyConfig `yaml:"proxy,omitempty"`
	// Runtime selects how the site's PHP is served. "fpm" (default) uses the
	// shared lerd-php{version}-fpm container; "frankenphp" spins up a
	// per-site dunglas/frankenphp container that keeps PHP resident.
	Runtime string `yaml:"runtime,omitempty"`
	// RuntimeWorker, when true and Runtime is "frankenphp", starts the
	// FrankenPHP container in worker mode. Framework-specific entrypoints
	// decide whether this flag is honoured.
	RuntimeWorker bool `yaml:"runtime_worker,omitempty"`
	// DBIsolated, when true on a worktree's .lerd.yaml, opts the worktree
	// into its own database (named <parent_db>_<sanitized_branch>) so
	// migrations don't bleed into the parent. Off by default.
	DBIsolated bool `yaml:"db_isolated,omitempty"`
	// EnvOverrides maps env keys to template values that are resolved and
	// written into the worktree's .env when a worktree is created. Supported
	// placeholders: {{domain}} (worktree domain), {{scheme}} (http/https),
	// {{site}} (database-safe name). When APP_URL is present here it takes
	// precedence over the default scheme://domain rewrite.
	EnvOverrides map[string]string `yaml:"env_overrides,omitempty"`
	// RequestTimeout overrides the nginx request timeout for this project, in
	// seconds. Zero inherits the global nginx.request_timeout (default 60s).
	// Raise it for apps with deliberately long-running requests.
	RequestTimeout int `yaml:"request_timeout,omitempty"`
	// Stripe holds optional per-project Stripe webhook listener settings: the
	// route events forward to and which .env key holds the secret. Absent for
	// projects on the Laravel defaults, which are auto-detected.
	Stripe *StripeConfig `yaml:"stripe,omitempty"`
}

// IsEmpty returns true when the config has no meaningful content, which
// typically means .lerd.yaml did not exist.
func (c *ProjectConfig) IsEmpty() bool {
	return len(c.Domains) == 0 && c.PHPVersion == "" && c.NodeVersion == "" &&
		c.JSRuntime == "" &&
		c.Framework == "" && c.FrameworkVersion == "" && c.FrameworkDef == nil &&
		c.PublicDir == "" && len(c.Services) == 0 &&
		len(c.Workers) == 0 && len(c.CustomWorkers) == 0 && len(c.ReloadWorkers) == 0 && len(c.Commands) == 0 && !c.Secured &&
		c.AppURL == "" && c.DB.Service == "" && c.DB.Database == "" &&
		!c.DB.External && c.DB.Socket == "" &&
		c.Container == nil && c.Proxy == nil && c.Runtime == "" && !c.RuntimeWorker &&
		!c.DBIsolated && len(c.EnvOverrides) == 0 && c.RequestTimeout == 0 && c.Stripe == nil
}

// Validate reports configuration that can't be honoured. A site is either a
// custom container or a host proxy, never both.
func (c *ProjectConfig) Validate() error {
	if c.Container != nil && c.Proxy != nil {
		return fmt.Errorf(".lerd.yaml sets both container: and proxy:; a site can only be one")
	}
	return nil
}

// ReloadsWorker reports whether the named worker is opted into auto-reload
// (restart on file changes) for this project.
func (c *ProjectConfig) ReloadsWorker(name string) bool {
	for _, w := range c.ReloadWorkers {
		if w == name {
			return true
		}
	}
	return false
}

// ProjectReloadsWorker reports whether the project at dir opts the named worker
// into auto-reload mode. Returns false when .lerd.yaml is absent or unreadable.
func ProjectReloadsWorker(dir, name string) bool {
	cfg, err := LoadProjectConfig(dir)
	if err != nil || cfg == nil {
		return false
	}
	return cfg.ReloadsWorker(name)
}

// ServiceNames returns the name of every service in the config, for callers
// that only need the list of names (e.g. the init wizard multi-select).
func (p *ProjectConfig) ServiceNames() []string {
	names := make([]string, len(p.Services))
	for i, s := range p.Services {
		names[i] = s.Name
	}
	return names
}

// ProjectService entries take three YAML shapes:
//
//   - redis                       # named reference (built-in)
//   - mysql:                      # preset reference, optional version
//     preset: mysql
//     version: "5.6"
//   - mongodb:                    # inline custom definition (legacy / hand-rolled)
//     image: mongo:7
//     ...
//
// Preset references are the preferred form for services installed via
// `lerd service preset` because each machine resolves the embedded preset
// locally — picking up bug fixes, default tweaks, and per-machine port
// allocations without churn in .lerd.yaml.
type ProjectService struct {
	Name          string
	Preset        string         // empty unless this is a preset reference
	PresetVersion string         // empty for single-version presets
	Custom        *CustomService // nil unless this is an inline definition
}

// UnmarshalYAML accepts the three shapes documented on ProjectService.
func (s *ProjectService) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		s.Name = value.Value
		return nil

	case yaml.MappingNode:
		if len(value.Content) != 2 {
			return fmt.Errorf("service entry must have exactly one key, got %d", len(value.Content)/2)
		}
		s.Name = value.Content[0].Value
		body := value.Content[1]
		// Peek at the body to decide preset-ref vs inline custom def. A
		// preset reference has a top-level "preset:" key.
		if body.Kind == yaml.MappingNode && hasMappingKey(body, "preset") {
			var ref struct {
				Preset  string `yaml:"preset"`
				Version string `yaml:"version,omitempty"`
			}
			if err := body.Decode(&ref); err != nil {
				return fmt.Errorf("decoding preset reference %q: %w", s.Name, err)
			}
			s.Preset = ref.Preset
			s.PresetVersion = ref.Version
			return nil
		}
		var svc CustomService
		if err := body.Decode(&svc); err != nil {
			return fmt.Errorf("decoding inline service %q: %w", s.Name, err)
		}
		svc.Name = s.Name
		s.Custom = &svc
		return nil

	default:
		return fmt.Errorf("unexpected YAML node kind %v for service entry", value.Kind)
	}
}

func hasMappingKey(node *yaml.Node, key string) bool {
	if node.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return true
		}
	}
	return false
}

// MarshalYAML serialises back to the compact form: plain string for named
// references, single-key preset map for preset references, single-key custom
// map for inline definitions.
func (s ProjectService) MarshalYAML() (interface{}, error) {
	if s.Preset == "" && s.Custom == nil {
		return s.Name, nil
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: s.Name}
	valNode := &yaml.Node{}
	if s.Preset != "" {
		ref := struct {
			Preset  string `yaml:"preset"`
			Version string `yaml:"version,omitempty"`
		}{Preset: s.Preset, Version: s.PresetVersion}
		if err := valNode.Encode(ref); err != nil {
			return nil, err
		}
	} else {
		if err := valNode.Encode(s.Custom); err != nil {
			return nil, err
		}
	}
	if valNode.Kind == yaml.DocumentNode && len(valNode.Content) == 1 {
		valNode = valNode.Content[0]
	}
	return &yaml.Node{
		Kind:    yaml.MappingNode,
		Content: []*yaml.Node{keyNode, valNode},
	}, nil
}

// Resolve returns the concrete CustomService for this entry. Preset references
// are resolved against the embedded preset library; inline definitions are
// returned as-is. Named built-in references return (nil, nil) — callers handle
// built-ins separately.
func (s ProjectService) Resolve() (*CustomService, error) {
	if s.Preset != "" {
		preset, err := LoadPreset(s.Preset)
		if err != nil {
			return nil, fmt.Errorf("preset %q referenced by project service %q: %w", s.Preset, s.Name, err)
		}
		return preset.Resolve(s.PresetVersion)
	}
	if s.Custom != nil {
		copy := *s.Custom
		return &copy, nil
	}
	return nil, nil
}

// projectConfigCache memoises parsed .lerd.yaml entries keyed by file path,
// invalidated by mtime+size. Used by the daemon's per-site enrichment so each
// snapshot rebuild doesn't re-read every project's .lerd.yaml. Negative
// entries cache the absence so missing files don't cost a fresh stat+open
// each time.
type projectConfigCacheEntry struct {
	cfg   *ProjectConfig // nil = file missing
	mtime time.Time
	size  int64
}

var (
	projectConfigCacheMu sync.Mutex
	projectConfigCache   = map[string]projectConfigCacheEntry{}
)

func invalidateProjectConfigCache(dir string) {
	path := filepath.Join(dir, ".lerd.yaml")
	projectConfigCacheMu.Lock()
	delete(projectConfigCache, path)
	projectConfigCacheMu.Unlock()
}

// LoadProjectConfig reads .lerd.yaml from dir, returning an empty config if
// the file does not exist.
func LoadProjectConfig(dir string) (*ProjectConfig, error) {
	path := filepath.Join(dir, ".lerd.yaml")
	info, statErr := os.Stat(path)

	projectConfigCacheMu.Lock()
	entry, hit := projectConfigCache[path]
	cacheValid := hit && (statErr != nil && entry.cfg == nil ||
		statErr == nil && entry.mtime.Equal(info.ModTime()) && entry.size == info.Size())
	if cacheValid {
		out := cloneProjectConfig(entry.cfg)
		projectConfigCacheMu.Unlock()
		if out == nil {
			return &ProjectConfig{}, nil
		}
		return out, nil
	}
	projectConfigCacheMu.Unlock()

	if statErr != nil {
		if os.IsNotExist(statErr) {
			projectConfigCacheMu.Lock()
			projectConfigCache[path] = projectConfigCacheEntry{}
			projectConfigCacheMu.Unlock()
			return &ProjectConfig{}, nil
		}
		return nil, statErr
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ProjectConfig{}, nil
		}
		return nil, err
	}
	var cfg ProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	if err := ValidatePublicDir(cfg.PublicDir); err != nil {
		fmt.Printf("[WARN] %s: %v, ignoring public_dir override\n", path, err)
		cfg.PublicDir = ""
	}

	if err := cfg.Validate(); err != nil {
		fmt.Printf("[WARN] %s: %v, ignoring proxy:\n", path, err)
		cfg.Proxy = nil
	}

	// Neutralise a hostile webhook path at the source so it can never reach the
	// Stripe listener unit's ExecStart line; readers fall back to the default.
	if cfg.Stripe != nil && cfg.Stripe.Path != "" {
		if _, err := ValidateStripeWebhookPath(cfg.Stripe.Path); err != nil {
			fmt.Printf("[WARN] %s: %v, ignoring stripe path\n", path, err)
			cfg.Stripe.Path = ""
		}
	}

	projectConfigCacheMu.Lock()
	projectConfigCache[path] = projectConfigCacheEntry{
		cfg: &cfg, mtime: info.ModTime(), size: info.Size(),
	}
	projectConfigCacheMu.Unlock()

	return cloneProjectConfig(&cfg), nil
}

// cloneProjectConfig returns a copy with mutable maps and slices freshly
// allocated. Inner FrameworkWorker entries are copied by value; their nested
// pointers (Check, Proxy) aren't deep-copied because callers don't mutate
// them in place.
func cloneProjectConfig(in *ProjectConfig) *ProjectConfig {
	if in == nil {
		return nil
	}
	out := *in
	if in.Domains != nil {
		out.Domains = append([]string(nil), in.Domains...)
	}
	if in.Services != nil {
		out.Services = append([]ProjectService(nil), in.Services...)
	}
	if in.Workers != nil {
		out.Workers = append([]string(nil), in.Workers...)
	}
	if in.ReloadWorkers != nil {
		out.ReloadWorkers = append([]string(nil), in.ReloadWorkers...)
	}
	if in.CustomWorkers != nil {
		out.CustomWorkers = make(map[string]FrameworkWorker, len(in.CustomWorkers))
		for k, v := range in.CustomWorkers {
			out.CustomWorkers[k] = v
		}
	}
	if in.EnvOverrides != nil {
		out.EnvOverrides = make(map[string]string, len(in.EnvOverrides))
		for k, v := range in.EnvOverrides {
			out.EnvOverrides[k] = v
		}
	}
	if in.Container != nil {
		cp := *in.Container
		out.Container = &cp
	}
	if in.Proxy != nil {
		cp := *in.Proxy
		out.Proxy = &cp
	}
	if in.Stripe != nil {
		cp := *in.Stripe
		out.Stripe = &cp
	}
	if in.FrameworkDef != nil {
		out.FrameworkDef = cloneFrameworkMutable(in.FrameworkDef)
	}
	return &out
}

// SaveProjectConfig writes cfg to .lerd.yaml in dir.
func SaveProjectConfig(dir string, cfg *ProjectConfig) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, ".lerd.yaml"), data, 0644); err != nil {
		return err
	}
	invalidateProjectConfigCache(dir)
	return nil
}
