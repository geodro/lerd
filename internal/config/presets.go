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

// PresetVersion is a single selectable image tag for a multi-version preset
// family (e.g. mysql 5.7, mysql 5.6, mariadb 11). Single-version presets like
// phpmyadmin or pgadmin omit Versions entirely and use the embedded
// CustomService image directly.
type PresetVersion struct {
	Tag   string `yaml:"tag" json:"tag"`
	Label string `yaml:"label,omitempty" json:"label,omitempty"`
	Image string `yaml:"image" json:"image"`
	// HostPort is the host-side port published for this specific version.
	// Each version gets its own fixed port so multiple alternates can run
	// side by side without colliding. Substituted into the family's
	// templated ports, env_vars and connection_url via {{host_port}}.
	HostPort int `yaml:"host_port,omitempty" json:"host_port,omitempty"`
}

// Preset is the parsed YAML for a bundled service preset. It embeds
// CustomService for the shared fields and adds an optional Versions list +
// DefaultVersion for families that ship multiple selectable image tags. After
// the user picks a tag, Resolve() materialises a concrete CustomService whose
// Name and Image are version-specific while every other field stays shared.
type Preset struct {
	CustomService  `yaml:",inline"`
	Versions       []PresetVersion `yaml:"versions,omitempty"`
	DefaultVersion string          `yaml:"default_version,omitempty"`
}

// PresetMeta is the lightweight description of a bundled preset, suitable for
// listing in CLI tables and the web UI without parsing every field.
type PresetMeta struct {
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	Dashboard      string          `json:"dashboard,omitempty"`
	DependsOn      []string        `json:"depends_on,omitempty"`
	Image          string          `json:"image"`
	Versions       []PresetVersion `json:"versions,omitempty"`
	DefaultVersion string          `json:"default_version,omitempty"`
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
		p, err := LoadPreset(name)
		if err != nil {
			continue
		}
		// For multi-version presets there is no canonical Image — leave the
		// top-level Image blank and surface the version list instead so the
		// frontend can render a dropdown.
		image := p.Image
		if len(p.Versions) > 0 {
			image = ""
		}
		out = append(out, PresetMeta{
			Name:           p.Name,
			Description:    p.Description,
			Dashboard:      p.Dashboard,
			DependsOn:      p.DependsOn,
			Image:          image,
			Versions:       p.Versions,
			DefaultVersion: p.DefaultVersion,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// LoadPreset returns the parsed Preset for a bundled file by name.
func LoadPreset(name string) (*Preset, error) {
	data, err := presetFS.ReadFile("presets/" + name + ".yaml")
	if err != nil {
		return nil, fmt.Errorf("unknown preset %q", name)
	}
	var p Preset
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parsing preset %s: %w", name, err)
	}
	if p.Name == "" {
		return nil, fmt.Errorf("preset %s: missing required field \"name\"", name)
	}
	// Single-version presets must declare an image. Multi-version presets must
	// not — the picked PresetVersion supplies it instead.
	if len(p.Versions) == 0 {
		if p.Image == "" {
			return nil, fmt.Errorf("preset %s: missing required field \"image\"", name)
		}
	} else {
		if p.Image != "" {
			return nil, fmt.Errorf("preset %s: top-level \"image\" must be empty when \"versions\" is set", name)
		}
		for i, v := range p.Versions {
			if v.Tag == "" || v.Image == "" {
				return nil, fmt.Errorf("preset %s: versions[%d] missing tag or image", name, i)
			}
		}
		if p.DefaultVersion == "" {
			p.DefaultVersion = p.Versions[0].Tag
		}
	}
	return &p, nil
}

// PresetExists reports whether a bundled preset with the given name exists.
func PresetExists(name string) bool {
	_, err := presetFS.Open("presets/" + name + ".yaml")
	return err == nil
}

// SanitizeImageTag returns a container-name-safe form of an image tag by
// replacing every character that systemd/podman do not accept in unit names
// with a hyphen. "5.7" -> "5-7", "8.0.34" -> "8-0-34", "11.4+focal" -> "11-4-focal".
func SanitizeImageTag(tag string) string {
	var b strings.Builder
	for _, r := range tag {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// Resolve materialises the preset into a concrete CustomService for the picked
// version. For single-version presets the embedded CustomService is returned
// as-is and version is ignored. For multi-version presets, version names a tag
// in Versions; an empty version selects DefaultVersion. The resolved service's
// Name is "<family>-<sanitized-tag>" and its Image is taken from the version
// entry. EnvVars and ConnectionURL are scanned for {{tag}} and {{tag_safe}}
// placeholders so the family-shared template can reference the picked tag.
func (p *Preset) Resolve(version string) (*CustomService, error) {
	if len(p.Versions) == 0 {
		svc := p.CustomService
		svc.Preset = p.Name
		return &svc, nil
	}
	if version == "" {
		version = p.DefaultVersion
	}
	var picked *PresetVersion
	for i := range p.Versions {
		if p.Versions[i].Tag == version {
			picked = &p.Versions[i]
			break
		}
	}
	if picked == nil {
		return nil, fmt.Errorf("preset %q has no version %q", p.Name, version)
	}
	safe := SanitizeImageTag(picked.Tag)
	svc := p.CustomService
	svc.Name = p.Name + "-" + safe
	svc.Image = picked.Image
	svc.Preset = p.Name
	svc.PresetVersion = picked.Tag
	hostPort := ""
	if picked.HostPort > 0 {
		hostPort = fmt.Sprintf("%d", picked.HostPort)
	}
	repl := strings.NewReplacer(
		"{{tag}}", picked.Tag,
		"{{tag_safe}}", safe,
		"{{host_port}}", hostPort,
	)
	if len(svc.Ports) > 0 {
		out := make([]string, len(svc.Ports))
		for i, port := range svc.Ports {
			out[i] = repl.Replace(port)
		}
		svc.Ports = out
	}
	if len(svc.EnvVars) > 0 {
		out := make([]string, len(svc.EnvVars))
		for i, kv := range svc.EnvVars {
			out[i] = repl.Replace(kv)
		}
		svc.EnvVars = out
	}
	if svc.ConnectionURL != "" {
		svc.ConnectionURL = repl.Replace(svc.ConnectionURL)
	}
	if svc.Dashboard != "" {
		svc.Dashboard = repl.Replace(svc.Dashboard)
	}
	return &svc, nil
}
