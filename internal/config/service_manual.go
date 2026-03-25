package config

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func manuallyStartedServicesFile() string {
	return filepath.Join(DataDir(), "manually-started-services.yaml")
}

func loadManuallyStartedServices() (map[string]bool, error) {
	data, err := os.ReadFile(manuallyStartedServicesFile())
	if os.IsNotExist(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	if err := yaml.Unmarshal(data, &names); err != nil {
		return nil, err
	}
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m, nil
}

func saveManuallyStartedServices(m map[string]bool) error {
	var names []string
	for n := range m {
		names = append(names, n)
	}
	if err := os.MkdirAll(DataDir(), 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(names)
	if err != nil {
		return err
	}
	return os.WriteFile(manuallyStartedServicesFile(), data, 0644)
}

// ServiceIsManuallyStarted returns true if the service was explicitly started by
// the user (via `lerd service start` or the dashboard), making it exempt from
// auto-stop when no sites reference it.
func ServiceIsManuallyStarted(name string) bool {
	m, err := loadManuallyStartedServices()
	if err != nil {
		return false
	}
	return m[name]
}

// SetServiceManuallyStarted marks or clears the manually-started flag for the
// named service.
func SetServiceManuallyStarted(name string, v bool) error {
	m, err := loadManuallyStartedServices()
	if err != nil {
		m = map[string]bool{}
	}
	if v {
		m[name] = true
	} else {
		delete(m, name)
	}
	return saveManuallyStartedServices(m)
}

func pinnedServicesFile() string {
	return filepath.Join(DataDir(), "pinned-services.yaml")
}

func loadPinnedServices() (map[string]bool, error) {
	data, err := os.ReadFile(pinnedServicesFile())
	if os.IsNotExist(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	if err := yaml.Unmarshal(data, &names); err != nil {
		return nil, err
	}
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m, nil
}

func savePinnedServices(m map[string]bool) error {
	var names []string
	for n := range m {
		names = append(names, n)
	}
	if err := os.MkdirAll(DataDir(), 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(names)
	if err != nil {
		return err
	}
	return os.WriteFile(pinnedServicesFile(), data, 0644)
}

// ServiceIsPinned returns true if the service has been pinned by the user,
// meaning it will never be auto-stopped even when no sites reference it.
func ServiceIsPinned(name string) bool {
	m, err := loadPinnedServices()
	if err != nil {
		return false
	}
	return m[name]
}

// SetServicePinned marks or clears the pinned flag for the named service.
func SetServicePinned(name string, v bool) error {
	m, err := loadPinnedServices()
	if err != nil {
		m = map[string]bool{}
	}
	if v {
		m[name] = true
	} else {
		delete(m, name)
	}
	return savePinnedServices(m)
}

// CountSitesUsingService returns how many active (non-ignored, non-paused) site
// .env files reference lerd-{name}, i.e. are configured to use the service.
func CountSitesUsingService(name string) int {
	reg, err := LoadSites()
	if err != nil {
		return 0
	}
	needle := "lerd-" + name
	count := 0
	for _, s := range reg.Sites {
		if s.Ignored || s.Paused {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.Path, ".env"))
		if err != nil {
			continue
		}
		if strings.Contains(string(data), needle) {
			count++
		}
	}
	return count
}
