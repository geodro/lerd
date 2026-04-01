package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ProjectConfig holds per-project configuration stored in .lerd.yaml.
type ProjectConfig struct {
	PHPVersion   string           `yaml:"php_version,omitempty"`
	NodeVersion  string           `yaml:"node_version,omitempty"`
	Framework    string           `yaml:"framework,omitempty"`
	FrameworkDef *Framework       `yaml:"framework_def,omitempty"`
	Secured      bool             `yaml:"secured,omitempty"`
	Services     []ProjectService `yaml:"services,omitempty"`
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

// ProjectService is either a named reference to a built-in or custom service
// (e.g. "redis") or an inline custom service definition.
//
// YAML forms:
//
//   - redis                      # named reference
//   - mongodb:                   # inline definition
//     image: mongo:7
//     ...
type ProjectService struct {
	Name   string
	Custom *CustomService // nil for named references
}

// UnmarshalYAML handles both scalar ("redis") and mapping ({mongodb: {...}})
// forms of a service entry.
func (s *ProjectService) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		s.Name = value.Value
		return nil

	case yaml.MappingNode:
		// Expect exactly one key whose value is the custom service body.
		if len(value.Content) != 2 {
			return fmt.Errorf("inline service definition must have exactly one key, got %d", len(value.Content)/2)
		}
		s.Name = value.Content[0].Value
		var svc CustomService
		if err := value.Content[1].Decode(&svc); err != nil {
			return fmt.Errorf("decoding inline service %q: %w", s.Name, err)
		}
		svc.Name = s.Name
		s.Custom = &svc
		return nil

	default:
		return fmt.Errorf("unexpected YAML node kind %v for service entry", value.Kind)
	}
}

// MarshalYAML serialises back to the compact form: plain string for named
// references, single-key map for inline definitions.
func (s ProjectService) MarshalYAML() (interface{}, error) {
	if s.Custom == nil {
		return s.Name, nil
	}
	// Build a single-key mapping node so the YAML looks like:
	//   - mongodb:
	//       image: ...
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: s.Name}
	valNode := &yaml.Node{}
	if err := valNode.Encode(s.Custom); err != nil {
		return nil, err
	}
	// Encode wraps in a document node; unwrap it.
	if valNode.Kind == yaml.DocumentNode && len(valNode.Content) == 1 {
		valNode = valNode.Content[0]
	}
	mapNode := &yaml.Node{
		Kind:    yaml.MappingNode,
		Content: []*yaml.Node{keyNode, valNode},
	}
	return mapNode, nil
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
