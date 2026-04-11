package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ProjectConfig holds per-project configuration stored in .lerd.yaml.
type ProjectConfig struct {
	Domains          []string                   `yaml:"domains,omitempty"`
	PHPVersion       string                     `yaml:"php_version,omitempty"`
	NodeVersion      string                     `yaml:"node_version,omitempty"`
	Framework        string                     `yaml:"framework,omitempty"`
	FrameworkVersion string                     `yaml:"framework_version,omitempty"`
	FrameworkDef     *Framework                 `yaml:"framework_def,omitempty"`
	Secured          bool                       `yaml:"secured,omitempty"`
	Services         []ProjectService           `yaml:"services,omitempty"`
	Workers          []string                   `yaml:"workers,omitempty"`
	CustomWorkers    map[string]FrameworkWorker `yaml:"custom_workers,omitempty"`
	// AppURL, when set, is the value lerd writes to the project's APP_URL (or
	// the framework-configured URL key) on every `lerd env` run. Committed to
	// the repo so the choice is shared across machines. Takes precedence over
	// the per-machine override in sites.yaml.
	AppURL string `yaml:"app_url,omitempty"`
}

// IsEmpty returns true when the config has no meaningful content, which
// typically means .lerd.yaml did not exist.
func (c *ProjectConfig) IsEmpty() bool {
	return len(c.Domains) == 0 && c.PHPVersion == "" && c.NodeVersion == "" &&
		c.Framework == "" && len(c.Services) == 0 && len(c.Workers) == 0 &&
		len(c.CustomWorkers) == 0 && !c.Secured && c.AppURL == ""
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
