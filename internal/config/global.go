package config

import (
	"os"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// ServiceConfig holds configuration for an optional service.
type ServiceConfig struct {
	Enabled    bool     `yaml:"enabled"      mapstructure:"enabled"`
	Image      string   `yaml:"image"        mapstructure:"image"`
	Port       int      `yaml:"port"         mapstructure:"port"`
	ExtraPorts []string `yaml:"extra_ports"  mapstructure:"extra_ports"`
}

// GlobalConfig is the top-level lerd configuration.
type GlobalConfig struct {
	PHP struct {
		DefaultVersion string              `yaml:"default_version" mapstructure:"default_version"`
		XdebugEnabled  map[string]bool     `yaml:"xdebug_enabled"  mapstructure:"xdebug_enabled"`
		XdebugMode     map[string]string   `yaml:"xdebug_mode,omitempty" mapstructure:"xdebug_mode"`
		Extensions     map[string][]string `yaml:"extensions"      mapstructure:"extensions"`
	} `yaml:"php" mapstructure:"php"`
	Node struct {
		DefaultVersion string `yaml:"default_version" mapstructure:"default_version"`
	} `yaml:"node" mapstructure:"node"`
	Nginx struct {
		HTTPPort  int `yaml:"http_port"  mapstructure:"http_port"`
		HTTPSPort int `yaml:"https_port" mapstructure:"https_port"`
	} `yaml:"nginx" mapstructure:"nginx"`
	DNS struct {
		TLD string `yaml:"tld" mapstructure:"tld"`
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
	ParkedDirectories []string                 `yaml:"parked_directories" mapstructure:"parked_directories"`
	Services          map[string]ServiceConfig `yaml:"services"           mapstructure:"services"`
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

func defaultConfig() *GlobalConfig {
	cfg := &GlobalConfig{}
	cfg.PHP.DefaultVersion = "8.5"
	cfg.Node.DefaultVersion = "22"
	cfg.Nginx.HTTPPort = 80
	cfg.Nginx.HTTPSPort = 443
	cfg.DNS.TLD = "test"

	home, _ := os.UserHomeDir()
	cfg.ParkedDirectories = []string{home + "/Lerd"}

	cfg.Services = map[string]ServiceConfig{
		"mysql": {
			Enabled: true,
			Image:   "docker.io/library/mysql:8.0",
			Port:    3306,
		},
		"redis": {
			Enabled: true,
			Image:   "docker.io/library/redis:7-alpine",
			Port:    6379,
		},
		"postgres": {
			Enabled: false,
			Image:   "docker.io/postgis/postgis:16-3.5-alpine",
			Port:    5432,
		},
		"meilisearch": {
			Enabled: false,
			Image:   "docker.io/getmeili/meilisearch:v1.7",
			Port:    7700,
		},
		"rustfs": {
			Enabled: false,
			Image:   "docker.io/rustfs/rustfs:latest",
			Port:    9000,
		},
		"mailpit": {
			Enabled: false,
			Image:   "docker.io/axllent/mailpit:latest",
			Port:    1025,
		},
	}
	return cfg
}

// LoadGlobal reads config.yaml via viper, returning defaults if the file is absent.
func LoadGlobal() (*GlobalConfig, error) {
	cfgFile := GlobalConfigFile()

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
	return cfg, nil
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
	"meilisearch": {
		"getmeili/meilisearch:v1.7",
	},
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

// RemoveExtension removes ext from the custom extension list for version.
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
	return os.WriteFile(GlobalConfigFile(), data, 0644)
}
