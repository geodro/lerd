package config

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed presets/*.yaml
var presetFS embed.FS

// PresetMeta is the lightweight description of a bundled preset, suitable for
// listing in CLI tables and the web UI without parsing every field.
type PresetMeta struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Dashboard   string   `json:"dashboard,omitempty"`
	DependsOn   []string `json:"depends_on,omitempty"`
	Image       string   `json:"image"`
}

// ListPresets returns the metadata for all bundled service presets, sorted by
// name.
func ListPresets() ([]PresetMeta, error) {
	entries, err := fs.ReadDir(presetFS, "presets")
	if err != nil {
		return nil, err
	}
	var out []PresetMeta
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".yaml")
		svc, err := LoadPreset(name)
		if err != nil {
			continue
		}
		out = append(out, PresetMeta{
			Name:        svc.Name,
			Description: svc.Description,
			Dashboard:   svc.Dashboard,
			DependsOn:   svc.DependsOn,
			Image:       svc.Image,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// LoadPreset returns the CustomService for a bundled preset by name.
func LoadPreset(name string) (*CustomService, error) {
	data, err := presetFS.ReadFile("presets/" + name + ".yaml")
	if err != nil {
		return nil, fmt.Errorf("unknown preset %q", name)
	}
	var svc CustomService
	if err := yaml.Unmarshal(data, &svc); err != nil {
		return nil, fmt.Errorf("parsing preset %s: %w", name, err)
	}
	if svc.Name == "" {
		return nil, fmt.Errorf("preset %s: missing required field \"name\"", name)
	}
	if svc.Image == "" {
		return nil, fmt.Errorf("preset %s: missing required field \"image\"", name)
	}
	return &svc, nil
}

// PresetExists reports whether a bundled preset with the given name exists.
func PresetExists(name string) bool {
	_, err := presetFS.Open("presets/" + name + ".yaml")
	return err == nil
}
