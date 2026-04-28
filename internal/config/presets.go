package config

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"runtime"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// runtimeGOOS is a function variable so tests can override the host OS when
// exercising platform-specific preset behaviour. Production code never assigns it.
var runtimeGOOS = func() string { return runtime.GOOS }

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
	// Canonical marks this version as the default-preset's bare instance:
	// the service keeps the family name (no -<tag_safe> suffix), uses the
	// preset's top-level ports literally, and skips {{host_port}}/{{tag_safe}}
	// substitution. Exactly one version per multi-version preset may set it.
	Canonical bool `yaml:"canonical,omitempty" json:"canonical,omitempty"`
}

// PresetPlatformImage swaps the resolved image when the host OS matches and
// the current image (after version selection) matches the glob in ImageMatch.
// Lifted from the prior cli.platformImageOverride hardcoded list.
type PresetPlatformImage struct {
	OS         string `yaml:"os"`
	ImageMatch string `yaml:"image_match"`
	Image      string `yaml:"image"`
}

// Preset is the parsed YAML for a bundled service preset. It embeds
// CustomService for the shared fields and adds an optional Versions list +
// DefaultVersion for families that ship multiple selectable image tags. After
// the user picks a tag, Resolve() materialises a concrete CustomService whose
// Name and Image are version-specific while every other field stays shared.
type Preset struct {
	CustomService     `yaml:",inline"`
	Versions          []PresetVersion       `yaml:"versions,omitempty"`
	DefaultVersion    string                `yaml:"default_version,omitempty"`
	Default           bool                  `yaml:"default,omitempty"`
	UpdateStrategy    string                `yaml:"update_strategy,omitempty"`
	PlatformOverrides []PresetPlatformImage `yaml:"platform_overrides,omitempty"`
	// AllowMajorUpgrade lets the cross-strategy "Upgrade" button cross numeric
	// major boundaries. Default false: major upgrades typically require manual
	// data migration and should be installed as a separate alternate instead.
	AllowMajorUpgrade bool `yaml:"allow_major_upgrade,omitempty"`
	// TrackLatest makes fresh installs resolve the actual newest tag from the
	// registry at first start (within same-major + variant constraints). The
	// preset's `image:` field becomes a fallback for offline / registry-down
	// installs. Existing users with a saved image override are untouched —
	// they progress through the Update / Upgrade buttons instead.
	TrackLatest bool `yaml:"track_latest,omitempty"`
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
		// Multi-version presets surface a dropdown instead of a top-level image.
		image := p.Image
		if len(p.Versions) > 0 {
			image = ""
		}
		// Canonical version IS the default install; filter it out of the
		// alternates picker so users only see versions they can install
		// alongside the canonical.
		alternates := make([]PresetVersion, 0, len(p.Versions))
		altDefault := p.DefaultVersion
		for _, v := range p.Versions {
			if v.Canonical {
				continue
			}
			alternates = append(alternates, v)
		}
		if altDefault != "" {
			found := false
			for _, v := range alternates {
				if v.Tag == altDefault {
					found = true
					break
				}
			}
			if !found && len(alternates) > 0 {
				altDefault = alternates[0].Tag
			}
		}
		out = append(out, PresetMeta{
			Name:           p.Name,
			Description:    p.Description,
			Dashboard:      p.Dashboard,
			DependsOn:      p.DependsOn,
			Image:          image,
			Versions:       alternates,
			DefaultVersion: altDefault,
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
	if err := ValidatePresetYAML(data, name); err != nil {
		return nil, err
	}
	var p Preset
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parsing preset %s: %w", name, err)
	}
	if len(p.Versions) > 0 && p.DefaultVersion == "" {
		p.DefaultVersion = p.Versions[0].Tag
	}
	return &p, nil
}

// ValidatePresetYAML parses and validates raw preset YAML, returning a
// descriptive error on the first issue found. Exposed for tests that need to
// assert validation rules without round-tripping through the embed FS.
func ValidatePresetYAML(data []byte, name string) error {
	var p Preset
	if err := yaml.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("parsing preset %s: %w", name, err)
	}
	if p.Name == "" {
		return fmt.Errorf("preset %s: missing required field \"name\"", name)
	}
	if len(p.Versions) == 0 {
		if p.Image == "" {
			return fmt.Errorf("preset %s: missing required field \"image\"", name)
		}
		return nil
	}
	if p.Image != "" {
		return fmt.Errorf("preset %s: top-level \"image\" must be empty when \"versions\" is set", name)
	}
	canonicalCount := 0
	for i, v := range p.Versions {
		if v.Tag == "" || v.Image == "" {
			return fmt.Errorf("preset %s: versions[%d] missing tag or image", name, i)
		}
		if v.Canonical {
			canonicalCount++
		}
	}
	if canonicalCount > 1 {
		return fmt.Errorf("preset %s: at most one version may be canonical, found %d", name, canonicalCount)
	}
	return nil
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
	svc.Image = picked.Image
	svc.Preset = p.Name
	svc.PresetVersion = picked.Tag
	if !picked.Canonical {
		svc.Name = p.Name + "-" + safe
	}
	var hostPort string
	if picked.HostPort > 0 {
		hostPort = fmt.Sprintf("%d", picked.HostPort)
	}
	repl := strings.NewReplacer(
		"{{name}}", svc.Name,
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

// ApplyPlatformOverride swaps svc.Image with a platform-specific replacement
// when the runtime OS matches and the current image satisfies ImageMatch.
// The override image may use {{tag}} as a placeholder for the current image's
// tag — this lets `track_latest`-resolved tags (e.g. 16.4-3.5-alpine) survive
// the override swap. goos is usually runtime.GOOS, parameterised for tests.
func (p *Preset) ApplyPlatformOverride(svc *CustomService, goos string) {
	for _, po := range p.PlatformOverrides {
		if po.OS != goos {
			continue
		}
		if matched, _ := path.Match(po.ImageMatch, svc.Image); !matched {
			if _, rest, ok := splitImageRegistry(svc.Image); ok {
				if matched, _ = path.Match(po.ImageMatch, rest); !matched {
					continue
				}
			} else {
				continue
			}
		}
		out := po.Image
		if strings.Contains(out, "{{tag}}") {
			tag := ""
			if at := strings.LastIndex(svc.Image, ":"); at > 0 {
				tag = svc.Image[at+1:]
			}
			out = strings.ReplaceAll(out, "{{tag}}", tag)
		}
		svc.Image = out
		return
	}
}

// splitImageRegistry separates the registry hostname (or implicit docker.io)
// from the rest of an image reference. Returns ok=false if the image has no
// recognisable registry segment.
func splitImageRegistry(image string) (registry, rest string, ok bool) {
	slash := strings.Index(image, "/")
	if slash < 0 {
		return "", image, true
	}
	first := image[:slash]
	if strings.ContainsAny(first, ".:") || first == "localhost" {
		return first, image[slash+1:], true
	}
	return "", image, true
}

var (
	defaultPresetNamesOnce sync.Once
	defaultPresetNamesList []string
	defaultPresetNamesSet  map[string]bool
)

// DefaultPresetNames returns sorted names of every bundled preset whose YAML
// declares default: true. Cached on first call: the embed FS never changes
// during a process lifetime, so re-walking it on every lookup is wasteful.
func DefaultPresetNames() []string {
	defaultPresetNamesOnce.Do(loadDefaultPresetIndex)
	out := make([]string, len(defaultPresetNamesList))
	copy(out, defaultPresetNamesList)
	return out
}

// IsDefaultPreset reports whether name belongs to a default preset (the
// successor to the previous "built-in service" concept).
func IsDefaultPreset(name string) bool {
	defaultPresetNamesOnce.Do(loadDefaultPresetIndex)
	return defaultPresetNamesSet[name]
}

func loadDefaultPresetIndex() {
	defaultPresetNamesSet = map[string]bool{}
	entries, err := fs.ReadDir(presetFS, "presets")
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".yaml")
		p, err := LoadPreset(name)
		if err != nil || !p.Default {
			continue
		}
		defaultPresetNamesSet[name] = true
		defaultPresetNamesList = append(defaultPresetNamesList, name)
	}
	sort.Strings(defaultPresetNamesList)
}

// DefaultPresetMeta returns the resolved canonical CustomService for a default
// preset by name. The resolved service carries the preset's env_vars,
// dashboard, connection_url, ports and family, plus any platform image
// override. Returns an error for unknown or non-default preset names.
//
// The result is the canonical instance only — it does not consult the user's
// global config Services map for image overrides. Callers that need user
// overrides applied should layer that on themselves; serviceops.EnsureDefaultPresetQuadlet
// (introduced in Phase C) is the integration point.
func DefaultPresetMeta(name string) (*CustomService, error) {
	if !IsDefaultPreset(name) {
		return nil, fmt.Errorf("not a default preset: %q", name)
	}
	cached, err := cachedDefaultPresetMeta(name)
	if err != nil {
		return nil, err
	}
	out := *cached
	if len(cached.EnvVars) > 0 {
		out.EnvVars = append([]string(nil), cached.EnvVars...)
	}
	if len(cached.Ports) > 0 {
		out.Ports = append([]string(nil), cached.Ports...)
	}
	return &out, nil
}

// DefaultPresetEnvVars returns the resolved env_vars slice for a default
// preset, or nil for any other name. Used by the env writer (lerd env) and
// the web UI to surface the same .env hints regardless of caller.
func DefaultPresetEnvVars(name string) []string {
	svc, err := DefaultPresetMeta(name)
	if err != nil {
		return nil
	}
	return svc.EnvVars
}

// DefaultPresetDashboard returns the dashboard URL for a default preset, or
// empty for non-defaults / presets that don't expose a dashboard.
func DefaultPresetDashboard(name string) string {
	svc, err := DefaultPresetMeta(name)
	if err != nil {
		return ""
	}
	return svc.Dashboard
}

// DefaultPresetConnectionURL returns the developer-facing connection URL for
// a default preset, or empty for non-defaults / presets without one.
func DefaultPresetConnectionURL(name string) string {
	svc, err := DefaultPresetMeta(name)
	if err != nil {
		return ""
	}
	return svc.ConnectionURL
}

var (
	defaultPresetCacheMu sync.Mutex
	defaultPresetCache   = map[string]*CustomService{}
)

func cachedDefaultPresetMeta(name string) (*CustomService, error) {
	defaultPresetCacheMu.Lock()
	defer defaultPresetCacheMu.Unlock()
	if c, ok := defaultPresetCache[name]; ok {
		return c, nil
	}
	p, err := LoadPreset(name)
	if err != nil {
		return nil, err
	}
	svc, err := p.Resolve("")
	if err != nil {
		return nil, err
	}
	p.ApplyPlatformOverride(svc, runtimeGOOS())
	defaultPresetCache[name] = svc
	return svc, nil
}
