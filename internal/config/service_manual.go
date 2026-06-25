package config

import (
	"os"
	"path/filepath"
	"strings"
)

func manuallyStartedServicesFile() string {
	return filepath.Join(DataDir(), "manually-started-services.yaml")
}

// ServiceIsManuallyStarted returns true if the service was explicitly started by
// the user (via `lerd service start` or the dashboard), making it exempt from
// auto-stop when no sites reference it.
func ServiceIsManuallyStarted(name string) bool {
	return serviceSetContains(manuallyStartedServicesFile(), name)
}

// SetServiceManuallyStarted marks or clears the manually-started flag for the
// named service.
func SetServiceManuallyStarted(name string, v bool) error {
	return serviceSetUpdate(manuallyStartedServicesFile(), name, v)
}

func pinnedServicesFile() string {
	return filepath.Join(DataDir(), "pinned-services.yaml")
}

// ServiceIsPinned returns true if the service has been pinned by the user,
// meaning it will never be auto-stopped even when no sites reference it.
func ServiceIsPinned(name string) bool {
	return serviceSetContains(pinnedServicesFile(), name)
}

// SetServicePinned marks or clears the pinned flag for the named service.
func SetServicePinned(name string, v bool) error {
	return serviceSetUpdate(pinnedServicesFile(), name, v)
}

// ServicePublishedPort returns the published host port a service was pinned to
// (0 = preset/version default). It is non-zero when the user ran
// `lerd service port`, or when the port-ownership guard auto-shifted lerd's DB
// off the engine default because a host server owns it. Readers that surface a
// host-facing endpoint (e.g. the dashboard's connection URL) use it so the port
// reflects where lerd's container actually listens, not the default a coexisting
// host server may be sitting on.
func ServicePublishedPort(name string) int {
	cfg, err := LoadGlobal()
	if err != nil {
		return 0
	}
	if sc, ok := cfg.Services[name]; ok {
		return sc.PublishedPort
	}
	return 0
}

// CountSitesUsingPHP returns how many non-ignored, non-paused sites are
// registered with the given PHP version.
func CountSitesUsingPHP(version string) int {
	reg, err := LoadSites()
	if err != nil {
		return 0
	}
	count := 0
	for _, s := range reg.Sites {
		if s.Ignored || s.Paused {
			continue
		}
		if s.PHPVersion == version {
			count++
		}
	}
	return count
}

// SitesUsingService returns the active (non-ignored, non-paused) sites whose
// .lerd.yaml lists the service or whose .env references lerd-{name}.
func SitesUsingService(name string) []Site {
	reg, err := LoadSites()
	if err != nil {
		return nil
	}
	needle := "lerd-" + name
	// name's database family (empty for non-DB services). A host-capable DB
	// service is "not used" by a site that has switched to the host (system)
	// database over its socket, even though the site's .lerd.yaml may still list
	// it and its .env may still mention lerd-<name>.
	nameFam := FamilyOfName(name)
	var out []Site
	for _, s := range reg.Sites {
		if s.Ignored || s.Paused {
			continue
		}
		proj, pErr := LoadProjectConfig(s.Path)
		// Don't count a host-backend site against the container DB service of its
		// own family, so that service can auto-stop once every site of that family
		// has moved to the host.
		if pErr == nil && proj != nil && proj.DB.External && dbFamiliesShareContainer(nameFam, proj.DBFamily()) {
			continue
		}
		if pErr == nil && proj != nil {
			matched := false
			for _, svc := range proj.Services {
				if svc.Name == name {
					out = append(out, s)
					matched = true
					break
				}
			}
			if matched {
				continue
			}
		}
		if data, err := os.ReadFile(filepath.Join(s.Path, ".env")); err == nil {
			if strings.Contains(string(data), needle) {
				out = append(out, s)
			}
		}
	}
	return out
}

// CountSitesUsingService returns how many active (non-ignored, non-paused) site
// .env files reference lerd-{name}, i.e. are configured to use the service.
func CountSitesUsingService(name string) int {
	return len(SitesUsingService(name))
}
