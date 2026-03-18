package config

import (
	"os"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// ServiceConfig holds configuration for an optional service.
type ServiceConfig struct {
	Enabled bool   `yaml:"enabled" mapstructure:"enabled"`
	Image   string `yaml:"image"   mapstructure:"image"`
	Port    int    `yaml:"port"    mapstructure:"port"`
}

// GlobalConfig is the top-level lerd configuration.
type GlobalConfig struct {
	PHP struct {
		DefaultVersion string `yaml:"default_version" mapstructure:"default_version"`
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
		"minio": {
			Enabled: false,
			Image:   "minio/minio:latest",
			Port:    9000,
		},
		"mailpit": {
			Enabled: false,
			Image:   "axllent/mailpit:latest",
			Port:    1025,
		},
		"soketi": {
			Enabled: false,
			Image:   "quay.io/soketi/soketi:latest-16-alpine",
			Port:    6001,
		},
	}
	return cfg
}

// LoadGlobal reads config.yaml via viper, returning defaults if the file is absent.
func LoadGlobal() (*GlobalConfig, error) {
	cfgFile := GlobalConfigFile()

	v := viper.New()
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
