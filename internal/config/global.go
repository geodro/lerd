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
	UI struct {
		// RemoteControl gates non-loopback access to the lerd dashboard.
		// Empty PasswordHash = disabled = LAN clients get 403. With a hash
		// set, LAN clients must present matching HTTP Basic auth. Loopback
		// (127.0.0.1, ::1) always bypasses both checks.
		Username     string `yaml:"username,omitempty" mapstructure:"username"`
		PasswordHash string `yaml:"password_hash,omitempty" mapstructure:"password_hash"`
	} `yaml:"ui,omitempty" mapstructure:"ui"`
	ParkedDirectories []string                 `yaml:"parked_directories" mapstructure:"parked_directories"`
	Services          map[string]ServiceConfig `yaml:"services"           mapstructure:"services"`
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
			Image:   "mysql:8.0",
			Port:    3306,
		},
		"redis": {
			Enabled: true,
			Image:   "redis:7-alpine",
			Port:    6379,
		},
		"postgres": {
			Enabled: false,
			Image:   "postgis/postgis:16-3.5-alpine",
			Port:    5432,
		},
		"meilisearch": {
			Enabled: false,
			Image:   "getmeili/meilisearch:v1.7",
			Port:    7700,
		},
		"rustfs": {
			Enabled: false,
			Image:   "rustfs/rustfs:latest",
			Port:    9000,
		},
		"mailpit": {
			Enabled: false,
			Image:   "axllent/mailpit:latest",
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
	return cfg, nil
}

// IsXdebugEnabled returns true if Xdebug is enabled for the given PHP version.
func (c *GlobalConfig) IsXdebugEnabled(version string) bool {
	return c.PHP.XdebugEnabled[version]
}

// SetXdebug enables or disables Xdebug for the given PHP version.
func (c *GlobalConfig) SetXdebug(version string, enabled bool) {
	if c.PHP.XdebugEnabled == nil {
		c.PHP.XdebugEnabled = map[string]bool{}
	}
	if !enabled {
		delete(c.PHP.XdebugEnabled, version)
		return
	}
	c.PHP.XdebugEnabled[version] = true
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
