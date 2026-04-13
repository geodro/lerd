package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

var validServiceName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// EnvDetect defines auto-detection rules for `lerd env`.
type EnvDetect struct {
	Key         string `yaml:"key,omitempty"`
	ValuePrefix string `yaml:"value_prefix,omitempty"`
	// Composer triggers detection when the named package is present in the
	// project's composer.json (require or require-dev). Used instead of Key
	// when the service should be auto-detected by a dependency rather than
	// an existing .env variable (e.g. selenium detected via laravel/dusk).
	Composer string `yaml:"composer,omitempty"`
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

// FileMount is a single file rendered to disk on the host and bind-mounted
// into a custom service container. It exists so presets can ship config files
// (e.g. pgAdmin's servers.json, a pgpass) without requiring the user to manage
// any host paths themselves.
type FileMount struct {
	// Target is the absolute path inside the container where the file appears.
	Target string `yaml:"target"`
	// Content is the literal file body, written verbatim.
	Content string `yaml:"content"`
	// Mode is the octal permission bits, e.g. "0600". Defaults to "0644".
	Mode string `yaml:"mode,omitempty"`
	// Chown adds the :U flag to the volume mount so podman re-chowns the file
	// to match the container's expected UID. Required when the in-container
	// process runs as a non-root user (e.g. pgAdmin runs as uid 5050) and the
	// file mode would otherwise hide it from that user (e.g. 0600).
	Chown bool `yaml:"chown,omitempty"`
}

// CustomService represents a user-defined OCI-based service.
type CustomService struct {
	Name          string            `yaml:"name"`
	Image         string            `yaml:"image"`
	Ports         []string          `yaml:"ports,omitempty"`
	Environment   map[string]string `yaml:"environment,omitempty"`
	DataDir       string            `yaml:"data_dir,omitempty"`
	Exec          string            `yaml:"exec,omitempty"`
	EnvVars       []string          `yaml:"env_vars,omitempty"`
	EnvDetect     *EnvDetect        `yaml:"env_detect,omitempty"`
	SiteInit      *SiteInit         `yaml:"site_init,omitempty"`
	Dashboard     string            `yaml:"dashboard,omitempty"`
	ConnectionURL string            `yaml:"connection_url,omitempty"`
	Description   string            `yaml:"description,omitempty"`
	DependsOn     []string          `yaml:"depends_on,omitempty"`
	Files         []FileMount       `yaml:"files,omitempty"`
	// Family groups related services so admin UIs can auto-discover every
	// member. e.g. the mysql preset declares family: mysql, and phpMyAdmin
	// uses dynamic_env to read all family members at quadlet generation time.
	Family string `yaml:"family,omitempty"`
	// Preset is the bundled preset name this service was installed from.
	// Set by InstallPresetByName. Used so the init wizard can store a
	// preset reference in .lerd.yaml instead of an inlined definition.
	Preset string `yaml:"preset,omitempty"`
	// PresetVersion is the picked version tag for multi-version presets.
	// Empty for single-version presets.
	PresetVersion string `yaml:"preset_version,omitempty"`
	// ShareHosts mounts the browser-testing hosts file
	// (~/.local/share/lerd/browser-hosts) into the container at /etc/hosts,
	// so the container can resolve .test domains to the nginx container's IP
	// on the Podman network. Used by browser testing services like Selenium
	// that need to reach lerd sites by domain name.
	ShareHosts bool `yaml:"share_hosts,omitempty" json:"share_hosts,omitempty"`
	// DynamicEnv declares container env vars whose value is computed at
	// quadlet generation time. Currently supported directive:
	//   discover_family:<name>  -> comma-joined hostnames of every installed
	//   service in the named family (built-in or custom).
	DynamicEnv map[string]string `yaml:"dynamic_env,omitempty"`
}

// ServiceFilePath returns the deterministic host path for a single FileMount
// belonging to the named service. Both the materialiser and the quadlet
// generator use this so they agree on layout without explicit plumbing.
func ServiceFilePath(svcName string, target string) string {
	safe := strings.ReplaceAll(strings.TrimPrefix(target, "/"), "/", "_")
	return filepath.Join(ServiceFilesDir(svcName), safe)
}

// builtinFamilies maps each built-in service name to the family it belongs
// to so dynamic_env discovery can include built-ins alongside custom services.
var builtinFamilies = map[string]string{
	"mysql":       "mysql",
	"postgres":    "postgres",
	"redis":       "redis",
	"meilisearch": "meilisearch",
	"rustfs":      "rustfs",
	"mailpit":     "mailpit",
}

// knownFamilies is the set of recognised family names. It is a superset of
// the families with built-in implementations — mongo and mariadb for example
// only exist as presets, but are still "known families" for the purposes of
// name inference and wizard categorisation.
var knownFamilies = map[string]bool{
	"mysql":       true,
	"mariadb":     true,
	"postgres":    true,
	"redis":       true,
	"meilisearch": true,
	"rustfs":      true,
	"mailpit":     true,
	"mongo":       true,
}

// IsKnownFamily reports whether name is a recognised service family.
func IsKnownFamily(name string) bool { return knownFamilies[name] }

// ServicesInFamily returns the container hostnames (lerd-<name>) of every
// installed service that belongs to the named family. Built-ins match against
// builtinFamilies; custom services match by their Family field, with a
// fallback that infers family from the name prefix (e.g. mysql-5-7 -> mysql)
// so services installed before the explicit field existed still discover.
// Names are returned in deterministic order so the resulting env var stays
// stable across regenerations.
func ServicesInFamily(family string) []string {
	if family == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for name, fam := range builtinFamilies {
		if fam == family {
			host := "lerd-" + name
			if !seen[host] {
				seen[host] = true
				out = append(out, host)
			}
		}
	}
	if customs, err := ListCustomServices(); err == nil {
		for _, svc := range customs {
			if svc.Family != family && InferFamily(svc.Name) != family {
				continue
			}
			host := "lerd-" + svc.Name
			if !seen[host] {
				seen[host] = true
				out = append(out, host)
			}
		}
	}
	sort.Strings(out)
	return out
}

// InferFamily returns the family for a custom service whose name follows the
// versioned-alternate template <family>-<digit...>, or the bare family name
// when the service is the canonical preset (e.g. "mongo"). Returns empty when
// neither pattern matches a known family.
func InferFamily(name string) string {
	if knownFamilies[name] {
		return name
	}
	for i := 1; i < len(name)-1; i++ {
		if name[i] != '-' {
			continue
		}
		if name[i+1] < '0' || name[i+1] > '9' {
			continue
		}
		prefix := name[:i]
		if knownFamilies[prefix] {
			return prefix
		}
	}
	return ""
}

// ResolveDynamicEnv applies any dynamic_env directives on svc, writing the
// computed values into svc.Environment. Called immediately before quadlet
// generation so the resolved values land in the rendered .container file.
func ResolveDynamicEnv(svc *CustomService) error {
	if len(svc.DynamicEnv) == 0 {
		return nil
	}
	if svc.Environment == nil {
		svc.Environment = make(map[string]string, len(svc.DynamicEnv))
	}
	for k, directive := range svc.DynamicEnv {
		parts := strings.SplitN(directive, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("service %s: invalid dynamic_env directive %q for %s", svc.Name, directive, k)
		}
		switch parts[0] {
		case "discover_family":
			seen := map[string]bool{}
			var all []string
			for _, fam := range strings.Split(parts[1], ",") {
				for _, host := range ServicesInFamily(strings.TrimSpace(fam)) {
					if !seen[host] {
						seen[host] = true
						all = append(all, host)
					}
				}
			}
			sort.Strings(all)
			svc.Environment[k] = strings.Join(all, ",")
		default:
			return fmt.Errorf("service %s: unknown dynamic_env directive %q", svc.Name, parts[0])
		}
	}
	return nil
}

// MaterializeServiceFiles writes each FileMount in svc to its host path,
// creating the parent directory and applying the requested mode.
func MaterializeServiceFiles(svc *CustomService) error {
	if len(svc.Files) == 0 {
		return nil
	}
	dir := ServiceFilesDir(svc.Name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating files dir for %s: %w", svc.Name, err)
	}
	for _, f := range svc.Files {
		if f.Target == "" {
			return fmt.Errorf("service %s: file mount missing target", svc.Name)
		}
		mode := os.FileMode(0644)
		if f.Mode != "" {
			parsed, err := strconv.ParseUint(f.Mode, 8, 32)
			if err != nil {
				return fmt.Errorf("service %s: invalid mode %q for %s: %w", svc.Name, f.Mode, f.Target, err)
			}
			mode = os.FileMode(parsed)
		}
		path := ServiceFilePath(svc.Name, f.Target)
		// Unlink first: with chown:true podman's :U flag re-owns the file to a
		// userns-mapped uid, so a plain rewrite would EACCES. Removing the dir
		// entry succeeds because the parent dir is owned by us.
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing stale %s for service %s: %w", path, svc.Name, err)
		}
		if err := os.WriteFile(path, []byte(f.Content), mode); err != nil {
			return fmt.Errorf("writing %s for service %s: %w", path, svc.Name, err)
		}
		// WriteFile honours umask; chmod explicitly so 0600 sticks.
		if err := os.Chmod(path, mode); err != nil {
			return fmt.Errorf("chmod %s: %w", path, err)
		}
	}
	return nil
}

// CustomServicesDependingOn returns the names of all custom services that
// declare name in their depends_on list.
func CustomServicesDependingOn(name string) []string {
	customs, err := ListCustomServices()
	if err != nil {
		return nil
	}
	var out []string
	for _, svc := range customs {
		for _, dep := range svc.DependsOn {
			if dep == name {
				out = append(out, svc.Name)
				break
			}
		}
	}
	return out
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
