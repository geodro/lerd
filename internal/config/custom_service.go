package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

var validServiceName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// EnvDetect defines auto-detection rules for `lerd env`.
type EnvDetect struct {
	Key         string `yaml:"key"`
	ValuePrefix string `yaml:"value_prefix,omitempty"`
}

// SiteInit defines an optional command to run inside the service container
// once per project when `lerd env` detects this service.
// Use it for any per-site setup: creating a database, a user, indexes, etc.
// The exec string may contain {{site}} and {{site_testing}} placeholders,
// which are replaced with the project site handle at runtime.
type SiteInit struct {
	// Container to exec into. Defaults to lerd-<service name>.
	Container string `yaml:"container,omitempty"`
	// Exec is passed to sh -c inside the container.
	Exec string `yaml:"exec"`
}

// CustomService represents a user-defined OCI-based service.
type CustomService struct {
	Name        string            `yaml:"name"`
	Image       string            `yaml:"image"`
	Ports       []string          `yaml:"ports,omitempty"`
	Environment map[string]string `yaml:"environment,omitempty"`
	DataDir     string            `yaml:"data_dir,omitempty"`
	Exec        string            `yaml:"exec,omitempty"`
	EnvVars     []string          `yaml:"env_vars,omitempty"`
	EnvDetect   *EnvDetect        `yaml:"env_detect,omitempty"`
	SiteInit    *SiteInit         `yaml:"site_init,omitempty"`
	Dashboard   string            `yaml:"dashboard,omitempty"`
	Description string            `yaml:"description,omitempty"`
}

// LoadCustomService loads a custom service by name from the services directory.
func LoadCustomService(name string) (*CustomService, error) {
	return LoadCustomServiceFromFile(filepath.Join(CustomServicesDir(), name+".yaml"))
}

// LoadCustomServiceFromFile parses a CustomService from any YAML file path.
func LoadCustomServiceFromFile(path string) (*CustomService, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var svc CustomService
	if err := yaml.Unmarshal(data, &svc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if svc.Name == "" {
		return nil, fmt.Errorf("%s: missing required field \"name\"", path)
	}
	if svc.Image == "" {
		return nil, fmt.Errorf("%s: missing required field \"image\"", path)
	}
	return &svc, nil
}

// SaveCustomService validates and writes a custom service config to disk.
func SaveCustomService(svc *CustomService) error {
	if !validServiceName.MatchString(svc.Name) {
		return fmt.Errorf("invalid service name %q: must match [a-z0-9][a-z0-9-]*", svc.Name)
	}
	dir := CustomServicesDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(svc)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, svc.Name+".yaml")
	return os.WriteFile(path, data, 0644)
}

// RemoveCustomService deletes a custom service config file.
func RemoveCustomService(name string) error {
	path := filepath.Join(CustomServicesDir(), name+".yaml")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ListCustomServices returns all custom services defined in the services directory.
func ListCustomServices() ([]*CustomService, error) {
	dir := CustomServicesDir()
	entries, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	var services []*CustomService
	for _, path := range entries {
		name := filepath.Base(path)
		name = name[:len(name)-5] // strip .yaml
		svc, err := LoadCustomService(name)
		if err != nil {
			continue // skip malformed files
		}
		services = append(services, svc)
	}
	return services, nil
}
