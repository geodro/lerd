package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ProjectConfig holds per-project configuration stored in .lerd.yaml.
type ProjectConfig struct {
	PHPVersion string   `yaml:"php_version,omitempty"`
	Framework  string   `yaml:"framework,omitempty"`
	Secured    bool     `yaml:"secured,omitempty"`
	Services   []string `yaml:"services,omitempty"`
}

// LoadProjectConfig reads .lerd.yaml from dir, returning an empty config if
// the file does not exist.
func LoadProjectConfig(dir string) (*ProjectConfig, error) {
	data, err := os.ReadFile(filepath.Join(dir, ".lerd.yaml"))
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
	return &cfg, nil
}

// SaveProjectConfig writes cfg to .lerd.yaml in dir.
func SaveProjectConfig(dir string, cfg *ProjectConfig) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, ".lerd.yaml"), data, 0644)
}
