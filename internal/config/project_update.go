package config

import (
	"os"
	"path/filepath"
	"slices"
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

// AddProjectWorker appends name to the workers list if not already present.
// No-op if .lerd.yaml does not exist.
func AddProjectWorker(dir, name string) error {
	return updateProjectConfig(dir, func(cfg *ProjectConfig) {
		for _, w := range cfg.Workers {
			if w == name {
				return
			}
		}
		cfg.Workers = append(cfg.Workers, name)
	})
}

// SetProjectWorkerReload opts the named worker into or out of auto-reload mode
// (restart on file changes) for the project, persisting to .lerd.yaml. Enabling
// creates .lerd.yaml when it does not exist yet, so the preference survives
// rather than silently no-op'ing. Disabling on a project with no .lerd.yaml is
// a no-op: the worker is already in standard mode, so no file is created.
func SetProjectWorkerReload(dir, name string, enabled bool) error {
	if !enabled {
		return updateProjectConfig(dir, func(cfg *ProjectConfig) {
			cfg.ReloadWorkers = removeWorkerName(cfg.ReloadWorkers, name)
		})
	}

	cfg, err := LoadProjectConfig(dir)
	if err != nil {
		return err
	}
	if cfg.ReloadsWorker(name) {
		return nil
	}
	cfg.ReloadWorkers = append(cfg.ReloadWorkers, name)
	return SaveProjectConfig(dir, cfg)
}

// removeWorkerName returns names with every occurrence of name removed, reusing
// the backing array (the caller's slice is a fresh clone from LoadProjectConfig).
func removeWorkerName(names []string, name string) []string {
	return slices.DeleteFunc(names, func(w string) bool { return w == name })
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
			// Strip only the default TLD so ordinary sites stay shareable as
			// bare labels. A domain on a different TLD (e.g. alice.local while
			// the default is test) is kept whole, so a relink restores it as-is
			// instead of re-suffixing it into alice.local.test.
			name := d
			if strings.HasSuffix(d, suffix) {
				name = strings.TrimSuffix(d, suffix)
			}
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

// SetProjectRuntime updates runtime and runtime_worker. No-op if .lerd.yaml
// does not exist. Passing an empty runtime clears both fields.
func SetProjectRuntime(dir, runtime string, worker bool) error {
	return updateProjectConfig(dir, func(cfg *ProjectConfig) {
		cfg.Runtime = runtime
		cfg.RuntimeWorker = worker && runtime != ""
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

// SetProjectCommand adds or replaces a command in .lerd.yaml's commands: block,
// matched by Name. Creates the file if it doesn't exist (commands can land on
// fresh projects, unlike custom workers which presume a registered site).
func SetProjectCommand(dir string, cmd FrameworkCommand) error {
	if cmd.Name == "" {
		return &CommandValidationError{Reason: "name is required"}
	}
	cfg, err := LoadProjectConfig(dir)
	if err != nil {
		return err
	}
	replaced := false
	for i := range cfg.Commands {
		if cfg.Commands[i].Name == cmd.Name {
			cfg.Commands[i] = cmd
			replaced = true
			break
		}
	}
	if !replaced {
		cfg.Commands = append(cfg.Commands, cmd)
	}
	return SaveProjectConfig(dir, cfg)
}

// RemoveProjectCommand deletes a project-level command entry by Name.
// Returns CommandNotFoundError if .lerd.yaml has no entry with that name.
func RemoveProjectCommand(dir string, name string) error {
	path := filepath.Join(dir, ".lerd.yaml")
	if _, err := os.Stat(path); err != nil {
		return &CommandNotFoundError{Name: name}
	}
	cfg, err := LoadProjectConfig(dir)
	if err != nil {
		return err
	}
	for i := range cfg.Commands {
		if cfg.Commands[i].Name == name {
			cfg.Commands = append(cfg.Commands[:i], cfg.Commands[i+1:]...)
			if len(cfg.Commands) == 0 {
				cfg.Commands = nil
			}
			return SaveProjectConfig(dir, cfg)
		}
	}
	return &CommandNotFoundError{Name: name}
}

// CommandNotFoundError is returned when a project command name is not in .lerd.yaml.
type CommandNotFoundError struct{ Name string }

func (e *CommandNotFoundError) Error() string {
	return "command " + e.Name + " not found in .lerd.yaml"
}

// CommandValidationError flags structural problems before we touch the yaml.
type CommandValidationError struct{ Reason string }

func (e *CommandValidationError) Error() string { return "invalid command: " + e.Reason }

// ReplaceProjectDBService removes any existing DB service entry from the
// project's .lerd.yaml and adds the given choice. A "DB service" is sqlite or
// any service in the mysql / mariadb / postgres / mongo families, so alternates
// like postgres-pgvector or mariadb-10-11 replace the previous DB pick
// cleanly. Creates .lerd.yaml entries even if the file doesn't exist yet.
func ReplaceProjectDBService(dir string, choice string) error {
	cfg, err := LoadProjectConfig(dir)
	if err != nil {
		return err
	}
	filtered := cfg.Services[:0]
	for _, svc := range cfg.Services {
		if !IsDBServiceName(svc.Name) {
			filtered = append(filtered, svc)
		}
	}
	cfg.Services = append(filtered, ProjectService{Name: choice})
	return SaveProjectConfig(dir, cfg)
}

// IsDBServiceName reports whether name refers to a database service: sqlite,
// or any service whose family is mysql, mariadb, postgres, or mongo. Used by
// the DB-picker logic in `lerd env` and `db_set` to decide what counts as
// "the database for this project".
func IsDBServiceName(name string) bool {
	if name == "sqlite" {
		return true
	}
	switch FamilyOfName(name) {
	case "mysql", "mariadb", "postgres", "mongo":
		return true
	}
	return false
}
