package config

import (
	"os"
	"path/filepath"
	"strings"
)

// updateProjectConfig loads .lerd.yaml, applies fn, and saves.
// No-op if .lerd.yaml does not exist.
func updateProjectConfig(dir string, fn func(*ProjectConfig)) error {
	path := filepath.Join(dir, ".lerd.yaml")
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	cfg, err := LoadProjectConfig(dir)
	if err != nil {
		return err
	}
	fn(cfg)
	return SaveProjectConfig(dir, cfg)
}

// SetProjectSecured updates the secured field. No-op if .lerd.yaml does not exist.
func SetProjectSecured(dir string, secured bool) error {
	return updateProjectConfig(dir, func(cfg *ProjectConfig) {
		cfg.Secured = secured
	})
}

// SetProjectPHPVersion updates php_version. No-op if .lerd.yaml does not exist.
func SetProjectPHPVersion(dir string, version string) error {
	return updateProjectConfig(dir, func(cfg *ProjectConfig) {
		cfg.PHPVersion = version
	})
}

// SetProjectWorkers replaces the workers list. No-op if .lerd.yaml does not exist.
func SetProjectWorkers(dir string, workers []string) error {
	return updateProjectConfig(dir, func(cfg *ProjectConfig) {
		cfg.Workers = workers
	})
}

// SetProjectDomains replaces the domains list. No-op if .lerd.yaml does not exist.
func SetProjectDomains(dir string, domains []string) error {
	return updateProjectConfig(dir, func(cfg *ProjectConfig) {
		cfg.Domains = domains
	})
}

// SyncProjectDomains merges fullDomains (stripping the TLD suffix) with any
// existing .lerd.yaml domains, deduplicating case-insensitively. Registered
// domains come first; pre-existing entries not in the registered list are
// appended (conflict-filtered domains preserved for self-healing).
// No-op if .lerd.yaml does not exist.
func SyncProjectDomains(dir string, fullDomains []string, tld string) error {
	return updateProjectConfig(dir, func(cfg *ProjectConfig) {
		suffix := "." + tld
		seen := make(map[string]bool)
		var names []string
		for _, d := range fullDomains {
			name := strings.TrimSuffix(d, suffix)
			low := strings.ToLower(name)
			if !seen[low] {
				names = append(names, name)
				seen[low] = true
			}
		}
		for _, d := range cfg.Domains {
			low := strings.ToLower(d)
			if !seen[low] {
				names = append(names, d)
				seen[low] = true
			}
		}
		cfg.Domains = names
	})
}

// RemoveProjectDomain removes a single domain (case-insensitive match).
// No-op if .lerd.yaml does not exist.
func RemoveProjectDomain(dir string, domain string) error {
	return updateProjectConfig(dir, func(cfg *ProjectConfig) {
		var kept []string
		for _, d := range cfg.Domains {
			if !strings.EqualFold(d, domain) {
				kept = append(kept, d)
			}
		}
		cfg.Domains = kept
	})
}

// SetProjectFrameworkVersion updates framework_version. No-op if .lerd.yaml
// does not exist or the version hasn't changed.
func SetProjectFrameworkVersion(dir string, version string) error {
	return updateProjectConfig(dir, func(cfg *ProjectConfig) {
		cfg.FrameworkVersion = version
	})
}

// SetProjectFrameworkDef replaces the embedded framework definition.
// No-op if .lerd.yaml does not exist.
func SetProjectFrameworkDef(dir string, def *Framework) error {
	return updateProjectConfig(dir, func(cfg *ProjectConfig) {
		cfg.FrameworkDef = def
	})
}

// SetProjectCustomWorker adds or replaces a custom worker entry.
// No-op if .lerd.yaml does not exist.
func SetProjectCustomWorker(dir string, name string, w FrameworkWorker) error {
	return updateProjectConfig(dir, func(cfg *ProjectConfig) {
		if cfg.CustomWorkers == nil {
			cfg.CustomWorkers = make(map[string]FrameworkWorker)
		}
		cfg.CustomWorkers[name] = w
	})
}

// RemoveProjectCustomWorker deletes a custom worker by name.
// No-op if .lerd.yaml does not exist. Returns an error if the worker is not found.
func RemoveProjectCustomWorker(dir string, name string) error {
	path := filepath.Join(dir, ".lerd.yaml")
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	cfg, err := LoadProjectConfig(dir)
	if err != nil {
		return err
	}
	if _, exists := cfg.CustomWorkers[name]; !exists {
		return &WorkerNotFoundError{Name: name}
	}
	delete(cfg.CustomWorkers, name)
	if len(cfg.CustomWorkers) == 0 {
		cfg.CustomWorkers = nil
	}
	return SaveProjectConfig(dir, cfg)
}

// WorkerNotFoundError is returned when a custom worker name is not in .lerd.yaml.
type WorkerNotFoundError struct {
	Name string
}

func (e *WorkerNotFoundError) Error() string {
	return "custom worker " + e.Name + " not found in .lerd.yaml"
}

// ReplaceProjectDBService removes any existing sqlite/mysql/postgres service
// and adds the given choice. Creates .lerd.yaml entries even if the file
// doesn't exist yet (database choice is typically part of initial setup).
func ReplaceProjectDBService(dir string, choice string) error {
	cfg, err := LoadProjectConfig(dir)
	if err != nil {
		return err
	}
	dbNames := map[string]bool{"sqlite": true, "mysql": true, "postgres": true}
	filtered := cfg.Services[:0]
	for _, svc := range cfg.Services {
		if !dbNames[svc.Name] {
			filtered = append(filtered, svc)
		}
	}
	cfg.Services = append(filtered, ProjectService{Name: choice})
	return SaveProjectConfig(dir, cfg)
}
