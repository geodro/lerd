package config

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// ServiceConfig holds configuration for an optional service.
type ServiceConfig struct {
	Enabled       bool     `yaml:"enabled"        mapstructure:"enabled"`
	Image         string   `yaml:"image"          mapstructure:"image"`
	Port          int      `yaml:"port"           mapstructure:"port"`
	ExtraPorts    []string `yaml:"extra_ports"    mapstructure:"extra_ports"`
	PreviousImage string   `yaml:"previous_image,omitempty" mapstructure:"previous_image"`
	// LastOp records the most recent mutation kind ("update" or "migrate") so
	// the rollback flow can refuse a swap that would race the new image
	// against the post-migrate (fresh) data dir. Empty means no recent op or a
	// state predating the field — treated as plain update for compatibility.
	LastOp string `yaml:"last_op,omitempty" mapstructure:"last_op"`
	// PreMigrateBackup is the absolute path to the data dir that was preserved
	// when the most recent operation was a migrate. Used by rollback to refuse
	// (or, in future, restore) when undoing the migrate would corrupt data.
	PreMigrateBackup string `yaml:"pre_migrate_backup,omitempty" mapstructure:"pre_migrate_backup"`
	// CanonicalVersion pins the preset version tag this service was first
	// installed on, so flipping the YAML's canonical (e.g. pg 16 → 18 in a
	// future release) never silently major-jumps existing installs.
	CanonicalVersion string `yaml:"canonical_version,omitempty" mapstructure:"canonical_version"`
}

// GlobalConfig is the top-level lerd configuration.
type GlobalConfig struct {
	// Editor is the command lerd runs to open a file at a line (the
	// "open in editor" links in the dashboard). Optional {file} and {line}
	// placeholders; if omitted, lerd appends the file. Empty = autodetect a
	// known GUI editor (code/cursor/phpstorm/subl/zed), then xdg-open/open.
	Editor string `yaml:"editor,omitempty" mapstructure:"editor"`
	PHP    struct {
		DefaultVersion string            `yaml:"default_version" mapstructure:"default_version"`
		XdebugEnabled  map[string]bool   `yaml:"xdebug_enabled"  mapstructure:"xdebug_enabled"`
		XdebugMode     map[string]string `yaml:"xdebug_mode,omitempty" mapstructure:"xdebug_mode"`
		// XdebugStart maps a PHP version to its xdebug.start_with_request value
		// (yes | trigger | no). Absent means the default "yes" (connect on every
		// request). "trigger"/"no" support on-demand debugging via the control
		// socket without flooding the IDE from every request and worker.
		XdebugStart map[string]string   `yaml:"xdebug_start,omitempty" mapstructure:"xdebug_start"`
		Extensions  map[string][]string `yaml:"extensions"      mapstructure:"extensions"`
		// ExtApkDeps maps a custom extension name to extra Alpine packages its
		// build needs. Keyed by extension (deps don't vary by PHP version).
		// lerd already knows the deps for some extensions; this is for the rest.
		ExtApkDeps map[string][]string `yaml:"ext_apk_deps,omitempty" mapstructure:"ext_apk_deps"`
		// Packages maps a PHP version to extra Alpine packages to install in the
		// FPM image's runtime stage (lerd php:pkg). For CLI tools and runtime
		// libraries users want available in the container; re-applied on rebuild.
		Packages map[string][]string `yaml:"packages,omitempty" mapstructure:"packages"`
	} `yaml:"php" mapstructure:"php"`
	Node struct {
		DefaultVersion string `yaml:"default_version" mapstructure:"default_version"`
	} `yaml:"node" mapstructure:"node"`
	Nginx struct {
		HTTPPort  int `yaml:"http_port"  mapstructure:"http_port"`
		HTTPSPort int `yaml:"https_port" mapstructure:"https_port"`
		// RequestTimeout is the default nginx request timeout in seconds,
		// overridable per project via .lerd.yaml request_timeout. Zero falls
		// back to nginx's own 60s default; read it via RequestTimeoutSeconds.
		RequestTimeout int `yaml:"request_timeout,omitempty" mapstructure:"request_timeout"`
	} `yaml:"nginx" mapstructure:"nginx"`
	DNS struct {
		// Enabled=false skips lerd-dns, mkcert CA, sudoers, and resolver
		// config; sites use *.localhost (RFC 6761). HTTPS is unavailable
		// in that mode. Default true preserves historical behaviour.
		Enabled bool   `yaml:"enabled" mapstructure:"enabled"`
		TLD     string `yaml:"tld"     mapstructure:"tld"`
		// Upstream pins the upstream DNS servers dnsmasq forwards
		// non-.test queries to. When empty, lerd auto-detects them from
		// the system resolver. Set this when auto-detection picks the
		// wrong servers (e.g. systemd-resolved fallbacks instead of your
		// LAN resolver). Accepts plain IPs; #port is allowed.
		Upstream []string `yaml:"upstream,omitempty" mapstructure:"upstream"`
	} `yaml:"dns" mapstructure:"dns"`
	LAN struct {
		// Exposed controls whether lerd's services are reachable from
		// other devices on the local network. When false (the default,
		// safe-on-coffee-shop-wifi state) every container PublishPort is
		// rewritten to bind 127.0.0.1, lerd-ui binds 127.0.0.1:7073, and
		// the lerd-dns-forwarder is stopped. When true, container ports
		// bind 0.0.0.0, lerd-ui binds 0.0.0.0:7073, dnsmasq is rewritten
		// to answer .test queries with the host's LAN IP, and the
		// userspace lerd-dns-forwarder runs to bridge LAN-IP:5300 to the
		// loopback-only DNS container.
		//
		// Toggled via `lerd lan:expose on/off`. The previous standalone
		// `dns:expose` flag was folded in here because there is no
		// meaningful state where the DNS resolver answers the LAN but
		// the actual services don't.
		Exposed bool `yaml:"exposed,omitempty" mapstructure:"exposed"`
	} `yaml:"lan,omitempty" mapstructure:"lan"`
	Autostart struct {
		// Disabled controls whether lerd boots itself at login. The
		// zero value (false) means lerd autostarts as it always has:
		// every lerd-* container quadlet ships with its [Install]
		// section, the podman generator wires it into
		// default.target.wants on every daemon-reload, and the
		// lerd-ui / lerd-watcher / per-site worker units are enabled.
		// Setting this to true makes WriteQuadletDiff strip the
		// [Install] section before write (so the generator stops
		// emitting wants symlinks), disables ui/watcher and every
		// per-site worker, and stops them. Toggled via
		// `lerd autostart enable / disable` and the dashboard / tray
		// switches.
		//
		// Inverted form (Disabled rather than Enabled) so the YAML zero
		// value preserves the historical autostart-on behaviour for
		// every existing install — users who never touch the toggle
		// see no change.
		Disabled bool `yaml:"disabled,omitempty" mapstructure:"disabled"`
	} `yaml:"autostart,omitempty" mapstructure:"autostart"`
	UI struct {
		// RemoteControl gates non-loopback access to the lerd dashboard.
		// Empty PasswordHash = disabled = LAN clients get 403. With a hash
		// set, LAN clients must present matching HTTP Basic auth. Loopback
		// (127.0.0.1, ::1) always bypasses both checks.
		Username     string `yaml:"username,omitempty" mapstructure:"username"`
		PasswordHash string `yaml:"password_hash,omitempty" mapstructure:"password_hash"`
	} `yaml:"ui,omitempty" mapstructure:"ui"`
	Workers struct {
		// ExecMode controls how framework workers (queue, schedule, horizon,
		// reverb, custom) are launched on macOS. "exec" (default) wraps a
		// single `podman exec` per worker in a dedup guard and lets launchd
		// supervise that process, matching Linux's lower-memory behaviour.
		// "container" runs each worker as its own detached container, which
		// costs more memory per worker but makes the podman supervisor
		// boundary 1:1 and sidesteps the SSH-bridge hiccups that can
		// otherwise produce phantom or duplicate workers.
		//
		// The field is ignored on Linux, which always runs workers as
		// `podman exec` into the shared FPM container (systemd is a
		// dependable supervisor there). Use WorkerExecMode() to read the
		// effective value.
		ExecMode string `yaml:"exec_mode,omitempty" mapstructure:"exec_mode"`
	} `yaml:"workers,omitempty" mapstructure:"workers"`
	Dumps struct {
		// Enabled is the single switch for the whole debug window: the dump
		// bridge AND the lerd_devtools collector (queries, mail, views, events,
		// jobs, http). Both the bridge and the extension read one runtime
		// sentinel (`enabled.flag`); their PHP/ini assets are always mounted
		// regardless of this flag, so what Enabled controls is just that
		// sentinel — touch = capture, missing = fast no-op, no FPM restart.
		// Toggled via `lerd dump on/off` or the dashboard Debug view.
		Enabled bool `yaml:"enabled,omitempty" mapstructure:"enabled"`
		// Passthrough controls whether dump()/dd() ALSO emit to the response
		// while the bridge is enabled. False (default) means captured-only:
		// the dashboard is the single destination and the response stays
		// clean (matching Herd's behaviour). True forwards each call through
		// Symfony's stock VarDumper handler after capture, useful as a
		// safety net when lerd-ui isn't running. No effect when Enabled is
		// false — without the bridge, dump() behaves exactly as Symfony
		// ships it.
		Passthrough bool `yaml:"passthrough,omitempty" mapstructure:"passthrough"`
	} `yaml:"dumps,omitempty" mapstructure:"dumps"`
	Devtools struct {
		// Workers includes long-running queue/scheduler worker queries in
		// capture. Off by default because their constant polling floods the
		// buffer; toggled from the dashboard "Show worker queries" checkbox.
		// The collector's enable state is shared with the debug bridge: one
		// sentinel (enabled.flag) and one config flag (Dumps.Enabled) arm both,
		// so there is no separate devtools enable toggle.
		Workers bool `yaml:"workers,omitempty" mapstructure:"workers"`
	} `yaml:"devtools,omitempty" mapstructure:"devtools"`
	Profiler struct {
		// Enabled toggles the SPX profiler globally. When on, nginx injects
		// SPX_ENABLED into every PHP-FPM site's requests so each is profiled.
		// Toggled via `lerd profile on/off` and the dashboard Profiler view.
		Enabled bool `yaml:"enabled,omitempty" mapstructure:"enabled"`
	} `yaml:"profiler,omitempty" mapstructure:"profiler"`
	Notifications struct {
		// Disabled globally mutes the notifier (WebSocket banners + Web
		// Push fanout). Inverted form so the zero value keeps existing
		// installs on. Toggled via `lerd notify on/off` and the tray.
		Disabled bool `yaml:"disabled,omitempty" mapstructure:"disabled"`
	} `yaml:"notifications,omitempty" mapstructure:"notifications"`
	HostProxy struct {
		// Disabled refuses to set up or start any host-proxy dev-server unit,
		// for users who never want lerd supervising a process on the host.
		// Inverted so the zero value keeps the feature available.
		Disabled bool `yaml:"disabled,omitempty" mapstructure:"disabled"`
		// SkipConfirmation runs a newly-linked host-proxy command without the
		// interactive "start this on your host?" prompt. Off by default so a
		// command from a cloned repo never runs unconfirmed.
		SkipConfirmation bool `yaml:"skip_confirmation,omitempty" mapstructure:"skip_confirmation"`
	} `yaml:"host_proxy,omitempty" mapstructure:"host_proxy"`
	IdleSuspend struct {
		// Enabled turns on activity-driven worker suspension: when a site sees
		// no activity for Timeout, its suspendable workers (queue, horizon, ...)
		// are gracefully stopped and resumed on the next request. Off by default
		// so a quiet dev box reclaims worker memory only once the user opts in.
		// Idle-suspend is a single global policy, not configured per site.
		Enabled bool `yaml:"enabled,omitempty" mapstructure:"enabled"`
		// Timeout is how long a site must be idle before its workers suspend, as
		// a Go duration string ("30m"). Empty or unparseable falls back to
		// DefaultIdleSuspendTimeout; read it via IdleSuspendTimeout.
		Timeout string `yaml:"timeout,omitempty" mapstructure:"timeout"`
	} `yaml:"idle_suspend,omitempty" mapstructure:"idle_suspend"`
	// AutoCleanup lets the watcher periodically reclaim orphaned lerd images
	// (safe tier only, never service images). On by default; set false to turn
	// off the daily sweep. Read it via AutoCleanupEnabled for nil-safety.
	AutoCleanup       bool                     `yaml:"auto_cleanup"       mapstructure:"auto_cleanup"`
	ParkedDirectories []string                 `yaml:"parked_directories" mapstructure:"parked_directories"`
	Services          map[string]ServiceConfig `yaml:"services"           mapstructure:"services"`
}

// AutoCleanupEnabled reports whether the watcher's periodic image cleanup is on.
// Nil-safe and defaults on, matching defaultConfig, so an unconfigured install
// reclaims orphaned images on its own.
func (c *GlobalConfig) AutoCleanupEnabled() bool {
	return c == nil || c.AutoCleanup
}

// DefaultIdleSuspendTimeout is how long a site stays idle before its
// suspendable workers are stopped, when no explicit timeout is configured.
const DefaultIdleSuspendTimeout = 30 * time.Minute

// IdleSuspendTimeout returns the effective global idle timeout, falling back to
// DefaultIdleSuspendTimeout when unset, unparseable, or non-positive.
func (c *GlobalConfig) IdleSuspendTimeout() time.Duration {
	if c.IdleSuspend.Timeout != "" {
		if d, err := time.ParseDuration(c.IdleSuspend.Timeout); err == nil && d > 0 {
			return d
		}
	}
	return DefaultIdleSuspendTimeout
}

// DefaultRequestTimeout is nginx's built-in fastcgi/proxy read-timeout default
// (seconds), used when neither the project nor the global config sets one.
const DefaultRequestTimeout = 60

// RequestTimeoutSeconds returns the effective global nginx request timeout in
// seconds, falling back to nginx's 60s default when unset or non-positive.
func (c *GlobalConfig) RequestTimeoutSeconds() int {
	if c.Nginx.RequestTimeout > 0 {
		return c.Nginx.RequestTimeout
	}
	return DefaultRequestTimeout
}

// Worker exec-mode constants. `exec` is the default on every platform;
// `container` is available as an opt-in on macOS for users who prefer the
// reliability of per-worker containers over the memory savings of
// podman-exec into the shared FPM container.
const (
	WorkerExecModeExec      = "exec"
	WorkerExecModeContainer = "container"
)

// WorkerExecMode returns the effective worker exec mode for the current
// platform. Invalid or empty configured values normalise to "exec".
func (c *GlobalConfig) WorkerExecMode() string {
	switch c.Workers.ExecMode {
	case WorkerExecModeContainer:
		return WorkerExecModeContainer
	}
	return WorkerExecModeExec
}

// DNSManaged reports whether lerd is managing local DNS, which is the
// prerequisite for HTTPS (mkcert CA, *.test resolution). A nil receiver counts
// as managed, matching how the rest of the codebase treats an absent config
// (an unconfigured install behaves as if DNS is enabled). It is the single
// predicate the install wizard, `lerd secure`, and the cert layer all share.
func (c *GlobalConfig) DNSManaged() bool {
	return c == nil || c.DNS.Enabled
}

func defaultConfig() *GlobalConfig {
	cfg := &GlobalConfig{}
	cfg.PHP.DefaultVersion = "8.5"
	cfg.Node.DefaultVersion = "22"
	cfg.Nginx.HTTPPort = 80
	cfg.Nginx.HTTPSPort = 443
	cfg.DNS.Enabled = true
	cfg.DNS.TLD = "test"
	cfg.AutoCleanup = true

	home, _ := os.UserHomeDir()
	cfg.ParkedDirectories = []string{home + "/Lerd"}

	// Hydrate the per-service defaults from each default-preset YAML so the
	// preset is the single source of truth for image, host port and identity.
	// Image overrides users have written into ~/.config/lerd/config.yaml are
	// merged on top by viper after this point in LoadGlobal.
	cfg.Services = map[string]ServiceConfig{}
	for _, name := range DefaultPresetNames() {
		svc, err := DefaultPresetMeta(name)
		if err != nil {
			continue
		}
		entry := ServiceConfig{Enabled: true, Port: firstHostPort(svc.Ports)}
		// Skip the Image seed for track_latest presets so EnsureDefaultPresetQuadlet
		// can detect "fresh install, no user pin" and resolve the actual current
		// upstream tag at install time. Existing users' saved Image overrides
		// continue to win via viper merge.
		if p, _ := LoadPreset(name); p == nil || !p.TrackLatest {
			entry.Image = svc.Image
		}
		cfg.Services[name] = entry
	}
	return cfg
}

// firstHostPort returns the host-side port number from the first ports entry,
// e.g. "3306:3306" → 3306. Used by defaultConfig to populate ServiceConfig.Port
// without mirroring the YAML port literals in code.
func firstHostPort(ports []string) int {
	if len(ports) == 0 {
		return 0
	}
	first := ports[0]
	if i := strings.Index(first, ":"); i >= 0 {
		first = first[:i]
	}
	n, _ := strconv.Atoi(first)
	return n
}

// globalCache memoises the last LoadGlobal result keyed on config.yaml's
// mtime+size. The daemon's snapshot path used to call LoadGlobal hundreds of
// times per rebuild (one per site, transitively), and each call re-parsed
// every preset YAML via defaultConfig — pprof showed yaml.Unmarshal as the
// dominant CPU cost. The cache returns a deep copy so callers can mutate the
// returned struct without poisoning the cache.
var (
	globalCacheMu sync.Mutex
	globalCache   *GlobalConfig
	globalCacheAt time.Time
	globalCacheSz int64
)

// invalidateGlobalCache drops the cached config so the next LoadGlobal re-reads
// from disk. Called from SaveGlobal so writes are visible immediately.
func invalidateGlobalCache() {
	globalCacheMu.Lock()
	globalCache = nil
	globalCacheAt = time.Time{}
	globalCacheSz = 0
	globalCacheMu.Unlock()
}

// EffectiveTLD returns the configured DNS TLD, falling back to "test" when the
// global config can't be loaded or leaves it empty. Single source of truth for
// the many callers that need the active TLD with that default.
func EffectiveTLD() string {
	if cfg, err := LoadGlobal(); err == nil && cfg.DNS.TLD != "" {
		return cfg.DNS.TLD
	}
	return "test"
}

// LoadGlobal reads config.yaml via viper, returning defaults if the file is absent.
func LoadGlobal() (*GlobalConfig, error) {
	cfgFile := GlobalConfigFile()

	var (
		statMtime time.Time
		statSize  int64
		statErr   error
	)
	if info, err := os.Stat(cfgFile); err == nil {
		statMtime = info.ModTime()
		statSize = info.Size()
	} else {
		statErr = err
	}

	globalCacheMu.Lock()
	if globalCache != nil && statErr == nil &&
		globalCacheAt.Equal(statMtime) && globalCacheSz == statSize {
		out := cloneGlobalConfig(globalCache)
		globalCacheMu.Unlock()
		return out, nil
	}
	globalCacheMu.Unlock()

	v := viper.NewWithOptions(viper.KeyDelimiter("::"))
	v.SetConfigFile(cfgFile)
	v.SetConfigType("yaml")

	if err := v.ReadInConfig(); err != nil {
		if os.IsNotExist(err) {
			return defaultConfig(), nil
		}
		return nil, err
	}

	cfg := defaultConfig()
	if err := v.Unmarshal(cfg); err != nil {
		return nil, err
	}
	migrateStaleServiceImages(cfg)

	if statErr == nil {
		globalCacheMu.Lock()
		globalCache = cloneGlobalConfig(cfg)
		globalCacheAt = statMtime
		globalCacheSz = statSize
		globalCacheMu.Unlock()
	}
	return cfg, nil
}

// cloneGlobalConfig returns a deep copy. Maps and slices are duplicated so
// callers cannot mutate the cached value.
func cloneGlobalConfig(in *GlobalConfig) *GlobalConfig {
	out := *in
	if in.PHP.XdebugEnabled != nil {
		out.PHP.XdebugEnabled = make(map[string]bool, len(in.PHP.XdebugEnabled))
		for k, v := range in.PHP.XdebugEnabled {
			out.PHP.XdebugEnabled[k] = v
		}
	}
	if in.PHP.XdebugMode != nil {
		out.PHP.XdebugMode = make(map[string]string, len(in.PHP.XdebugMode))
		for k, v := range in.PHP.XdebugMode {
			out.PHP.XdebugMode[k] = v
		}
	}
	if in.PHP.XdebugStart != nil {
		out.PHP.XdebugStart = make(map[string]string, len(in.PHP.XdebugStart))
		for k, v := range in.PHP.XdebugStart {
			out.PHP.XdebugStart[k] = v
		}
	}
	if in.PHP.Extensions != nil {
		out.PHP.Extensions = make(map[string][]string, len(in.PHP.Extensions))
		for k, v := range in.PHP.Extensions {
			cp := make([]string, len(v))
			copy(cp, v)
			out.PHP.Extensions[k] = cp
		}
	}
	if in.PHP.ExtApkDeps != nil {
		out.PHP.ExtApkDeps = make(map[string][]string, len(in.PHP.ExtApkDeps))
		for k, v := range in.PHP.ExtApkDeps {
			cp := make([]string, len(v))
			copy(cp, v)
			out.PHP.ExtApkDeps[k] = cp
		}
	}
	if in.PHP.Packages != nil {
		out.PHP.Packages = make(map[string][]string, len(in.PHP.Packages))
		for k, v := range in.PHP.Packages {
			cp := make([]string, len(v))
			copy(cp, v)
			out.PHP.Packages[k] = cp
		}
	}
	if in.ParkedDirectories != nil {
		out.ParkedDirectories = append([]string(nil), in.ParkedDirectories...)
	}
	if in.DNS.Upstream != nil {
		out.DNS.Upstream = append([]string(nil), in.DNS.Upstream...)
	}
	if in.Services != nil {
		out.Services = make(map[string]ServiceConfig, len(in.Services))
		for k, v := range in.Services {
			cp := v
			if v.ExtraPorts != nil {
				cp.ExtraPorts = append([]string(nil), v.ExtraPorts...)
			}
			out.Services[k] = cp
		}
	}
	return &out
}

// staleServiceImages maps service name → list of historical default images
// that earlier lerd releases persisted into user configs. When LoadGlobal
// finds one of these on disk it transparently replaces it with the current
// default from defaultConfig() so users picking up the upgrade automatically
// move onto the new image (e.g. postgres → postgis/postgis for PostGIS
// support) without having to hand-edit ~/.config/lerd/config.yaml.
var staleServiceImages = map[string][]string{
	"mysql": {
		"mysql:8.0",
	},
	"redis": {
		"redis:7-alpine",
	},
	"postgres": {
		"postgres:16-alpine",
		"docker.io/library/postgres:16-alpine",
		"docker.io/postgres:16-alpine",
		"postgis/postgis:16-3.5-alpine",
	},
	// meilisearch deliberately omitted: every minor bump breaks data-dir
	// compatibility, so silently upgrading existing v1.7 users to v1.42
	// would crash their running container. New installs pick up the latest
	// minor through defaultConfig; existing users keep their pinned image.
	"rustfs": {
		"rustfs/rustfs:latest",
	},
	"mailpit": {
		"axllent/mailpit:latest",
	},
}

func migrateStaleServiceImages(cfg *GlobalConfig) {
	if cfg == nil || cfg.Services == nil {
		return
	}
	defaults := defaultConfig().Services
	changed := false
	for name, stale := range staleServiceImages {
		svc, ok := cfg.Services[name]
		if !ok {
			continue
		}
		def, hasDefault := defaults[name]
		if !hasDefault {
			continue
		}
		// Skip migration for track_latest presets where defaultConfig has no
		// concrete image: rewriting to "" would land the user in the
		// fresh-install path on next start, silently bumping their data dir
		// across major-line boundaries (e.g. mysql:8.0 → 8.4 forward upgrade).
		if def.Image == "" {
			continue
		}
		for _, s := range stale {
			if svc.Image == s {
				svc.Image = def.Image
				cfg.Services[name] = svc
				changed = true
				break
			}
		}
	}
	if changed {
		_ = SaveGlobal(cfg)
	}
}

// IsXdebugEnabled returns true if Xdebug is enabled for the given PHP version.
func (c *GlobalConfig) IsXdebugEnabled(version string) bool {
	return c.GetXdebugMode(version) != ""
}

// GetXdebugMode returns the configured Xdebug mode for version, or "" when
// disabled. Entries in the legacy xdebug_enabled map (no explicit mode) are
// treated as mode "debug" so configs written by older lerd builds keep the
// same behaviour they had before per-mode support existed.
func (c *GlobalConfig) GetXdebugMode(version string) string {
	if m, ok := c.PHP.XdebugMode[version]; ok && m != "" {
		return m
	}
	if c.PHP.XdebugEnabled[version] {
		return "debug"
	}
	return ""
}

// SetXdebug enables (mode "debug") or disables Xdebug for version. Use
// SetXdebugMode directly when a non-default mode is wanted.
func (c *GlobalConfig) SetXdebug(version string, enabled bool) {
	if !enabled {
		c.SetXdebugMode(version, "")
		return
	}
	c.SetXdebugMode(version, "debug")
}

// SetXdebugMode sets the Xdebug mode for version. Empty mode disables Xdebug.
// Both the modern xdebug_mode map and the legacy xdebug_enabled map are kept
// in sync so downgrades don't silently flip state.
func (c *GlobalConfig) SetXdebugMode(version, mode string) {
	if c.PHP.XdebugEnabled == nil {
		c.PHP.XdebugEnabled = map[string]bool{}
	}
	if c.PHP.XdebugMode == nil {
		c.PHP.XdebugMode = map[string]string{}
	}
	if mode == "" {
		delete(c.PHP.XdebugEnabled, version)
		delete(c.PHP.XdebugMode, version)
		return
	}
	c.PHP.XdebugEnabled[version] = true
	c.PHP.XdebugMode[version] = mode
}

// GetXdebugStart returns the xdebug.start_with_request value for version,
// defaulting to "yes" (connect on every request) when unset.
func (c *GlobalConfig) GetXdebugStart(version string) string {
	if v, ok := c.PHP.XdebugStart[version]; ok && v != "" {
		return v
	}
	return "yes"
}

// SetXdebugStart records the xdebug.start_with_request value for version. An
// empty value (or "yes", the default) clears the entry so the config stays lean.
func (c *GlobalConfig) SetXdebugStart(version, value string) {
	if value == "" || value == "yes" {
		delete(c.PHP.XdebugStart, version)
		return
	}
	if c.PHP.XdebugStart == nil {
		c.PHP.XdebugStart = map[string]string{}
	}
	c.PHP.XdebugStart[version] = value
}

// GetExtensions returns the custom extensions configured for the given PHP version.
func (c *GlobalConfig) GetExtensions(version string) []string {
	if c.PHP.Extensions == nil {
		return nil
	}
	return c.PHP.Extensions[version]
}

// AddExtension adds ext to the custom extension list for version (no-op if already present).
func (c *GlobalConfig) AddExtension(version, ext string) {
	if c.PHP.Extensions == nil {
		c.PHP.Extensions = map[string][]string{}
	}
	for _, e := range c.PHP.Extensions[version] {
		if e == ext {
			return
		}
	}
	c.PHP.Extensions[version] = append(c.PHP.Extensions[version], ext)
}

// RemoveExtension removes ext from the custom extension list for version, and
// drops any extra apk deps recorded for it once no version still uses it.
func (c *GlobalConfig) RemoveExtension(version, ext string) {
	if c.PHP.Extensions == nil {
		return
	}
	exts := c.PHP.Extensions[version]
	filtered := exts[:0]
	for _, e := range exts {
		if e != ext {
			filtered = append(filtered, e)
		}
	}
	if len(filtered) == 0 {
		delete(c.PHP.Extensions, version)
	} else {
		c.PHP.Extensions[version] = filtered
	}
	stillUsed := false
	for _, list := range c.PHP.Extensions {
		for _, e := range list {
			if e == ext {
				stillUsed = true
			}
		}
	}
	if !stillUsed {
		delete(c.PHP.ExtApkDeps, ext)
		if len(c.PHP.ExtApkDeps) == 0 {
			c.PHP.ExtApkDeps = nil
		}
	}
}

// GetPackages returns the extra Alpine packages configured for the given PHP
// version's FPM image.
func (c *GlobalConfig) GetPackages(version string) []string {
	if c.PHP.Packages == nil {
		return nil
	}
	return c.PHP.Packages[version]
}

// AddPackage adds pkg to the extra-packages list for version (no-op if present).
func (c *GlobalConfig) AddPackage(version, pkg string) {
	if c.PHP.Packages == nil {
		c.PHP.Packages = map[string][]string{}
	}
	for _, p := range c.PHP.Packages[version] {
		if p == pkg {
			return
		}
	}
	c.PHP.Packages[version] = append(c.PHP.Packages[version], pkg)
}

// RemovePackage removes pkg from the extra-packages list for version.
func (c *GlobalConfig) RemovePackage(version, pkg string) {
	if c.PHP.Packages == nil {
		return
	}
	pkgs := c.PHP.Packages[version]
	filtered := pkgs[:0]
	for _, p := range pkgs {
		if p != pkg {
			filtered = append(filtered, p)
		}
	}
	if len(filtered) == 0 {
		delete(c.PHP.Packages, version)
	} else {
		c.PHP.Packages[version] = filtered
	}
}

// GetExtApkDeps returns the user-configured extra Alpine packages for ext.
func (c *GlobalConfig) GetExtApkDeps(ext string) []string {
	if c.PHP.ExtApkDeps == nil {
		return nil
	}
	return c.PHP.ExtApkDeps[ext]
}

// AllExtApkDeps returns the full user-configured extension → apk deps map.
func (c *GlobalConfig) AllExtApkDeps() map[string][]string {
	return c.PHP.ExtApkDeps
}

// SetExtApkDeps records (or clears, when deps is empty) the extra Alpine
// packages needed to build ext.
func (c *GlobalConfig) SetExtApkDeps(ext string, deps []string) {
	if len(deps) == 0 {
		delete(c.PHP.ExtApkDeps, ext)
		if len(c.PHP.ExtApkDeps) == 0 {
			c.PHP.ExtApkDeps = nil
		}
		return
	}
	if c.PHP.ExtApkDeps == nil {
		c.PHP.ExtApkDeps = map[string][]string{}
	}
	cp := make([]string, len(deps))
	copy(cp, deps)
	c.PHP.ExtApkDeps[ext] = cp
}

// IsDumpsEnabled reports whether the lerd debug bridge is on for all PHP
// versions. The toggle is global because the bridge file is a single,
// version-agnostic asset bind-mounted into every FPM container.
func (c *GlobalConfig) IsDumpsEnabled() bool {
	return c.Dumps.Enabled
}

// SetDumpsEnabled flips the debug bridge toggle. Persist via SaveGlobal and
// run dumpsops.Apply to actually rewrite the FPM quadlets.
func (c *GlobalConfig) SetDumpsEnabled(enabled bool) {
	c.Dumps.Enabled = enabled
}

// IsDevtoolsWorkers reports whether queue/scheduler worker queries are captured.
func (c *GlobalConfig) IsDevtoolsWorkers() bool {
	return c.Devtools.Workers
}

// SetDevtoolsWorkers flips worker-query capture. Persist via SaveGlobal and run
// devtoolsops.SetWorkers to touch the runtime sentinel.
func (c *GlobalConfig) SetDevtoolsWorkers(enabled bool) {
	c.Devtools.Workers = enabled
}

// IsProfilerEnabled reports whether the SPX profiler is globally armed.
func (c *GlobalConfig) IsProfilerEnabled() bool {
	return c.Profiler.Enabled
}

// IsDumpsPassthrough reports whether the bridge should also forward each
// captured dump to Symfony's stock VarDumper handler (response output).
// Always false in effect when the bridge itself is disabled.
func (c *GlobalConfig) IsDumpsPassthrough() bool {
	return c.Dumps.Passthrough
}

// SetDumpsPassthrough flips the passthrough flag. Persist via SaveGlobal
// and follow up with a `lerd-php*-fpm` restart so the new ini value takes
// effect (PHP reads ini directives at FPM startup, not per request).
func (c *GlobalConfig) SetDumpsPassthrough(enabled bool) {
	c.Dumps.Passthrough = enabled
}

// IsNotificationsEnabled reports whether the global notifier is allowed
// to fan out (WebSocket banners + Web Push). Inverted storage so existing
// installs default to enabled.
func (c *GlobalConfig) IsNotificationsEnabled() bool {
	return !c.Notifications.Disabled
}

// SetNotificationsEnabled flips the global notifier toggle. Persist via
// SaveGlobal; dispatchNotification re-reads the flag on every event.
func (c *GlobalConfig) SetNotificationsEnabled(enabled bool) {
	c.Notifications.Disabled = !enabled
}

// SaveGlobal writes the configuration to config.yaml.
func SaveGlobal(cfg *GlobalConfig) error {
	if err := os.MkdirAll(ConfigDir(), 0755); err != nil {
		return err
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(GlobalConfigFile(), data, 0644); err != nil {
		return err
	}
	invalidateGlobalCache()
	return nil
}
