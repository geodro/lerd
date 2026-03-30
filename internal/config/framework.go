package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Framework describes a PHP project framework type.
type Framework struct {
	Name      string                     `yaml:"name"`
	Label     string                     `yaml:"label"`
	Detect    []FrameworkRule            `yaml:"detect,omitempty"`
	PublicDir string                     `yaml:"public_dir"`
	Env       FrameworkEnvConf           `yaml:"env,omitempty"`
	Composer  string                     `yaml:"composer,omitempty"` // auto | true | false
	NPM       string                     `yaml:"npm,omitempty"`      // auto | true | false
	Workers   map[string]FrameworkWorker `yaml:"workers,omitempty"`
	// Create is the scaffold command used by "lerd new". The target directory is appended automatically.
	// Example: "composer create-project laravel/laravel"
	Create string `yaml:"create,omitempty"`
}

// FrameworkWorker describes a long-running process managed as a systemd service.
// The Command is executed inside the PHP-FPM container for the site.
type FrameworkWorker struct {
	Label   string `yaml:"label,omitempty"`
	Command string `yaml:"command"`
	Restart string `yaml:"restart,omitempty"` // always | on-failure (default: always)
}

// FrameworkRule is a single detection rule for a framework.
// Any matching rule is sufficient to identify the framework.
type FrameworkRule struct {
	File     string `yaml:"file,omitempty"`     // file must exist in project root
	Composer string `yaml:"composer,omitempty"` // package must be in composer.json require/require-dev
}

// FrameworkEnvConf describes how the framework manages its env file.
type FrameworkEnvConf struct {
	File           string `yaml:"file,omitempty"`            // primary env file (relative to project)
	ExampleFile    string `yaml:"example_file,omitempty"`    // example to copy from if File missing
	Format         string `yaml:"format,omitempty"`          // dotenv | php-const (default: dotenv)
	FallbackFile   string `yaml:"fallback_file,omitempty"`   // used when File doesn't exist
	FallbackFormat string `yaml:"fallback_format,omitempty"` // format for FallbackFile

	// URLKey is the env key that holds the application URL (default: APP_URL).
	URLKey string `yaml:"url_key,omitempty"`

	// Services defines per-service detection rules and env vars to apply.
	// Keys match the built-in service names: mysql, postgres, redis, meilisearch, rustfs, mailpit.
	Services map[string]FrameworkServiceDef `yaml:"services,omitempty"`
}

// FrameworkServiceDef describes how a service is detected and configured for a framework.
type FrameworkServiceDef struct {
	// Detect lists env key conditions; any match signals the service is in use.
	Detect []FrameworkServiceDetect `yaml:"detect,omitempty"`
	// Vars is the list of KEY=VALUE pairs to apply when the service is detected.
	// Use {{site}} for the per-project database name.
	Vars []string `yaml:"vars,omitempty"`
}

// FrameworkServiceDetect is a single detection condition.
// The service is considered active when Key exists in the env file and,
// if ValuePrefix is set, its value starts with that prefix.
type FrameworkServiceDetect struct {
	Key         string `yaml:"key"`
	ValuePrefix string `yaml:"value_prefix,omitempty"`
}

// Resolve returns the env file path and format to use for the given project directory.
// It returns the primary file if it exists, otherwise the fallback.
// Defaults to ".env" with "dotenv" format if nothing is configured.
func (e FrameworkEnvConf) Resolve(projectDir string) (file, format string) {
	primary := e.File
	if primary == "" {
		primary = ".env"
	}
	primaryFmt := e.Format
	if primaryFmt == "" {
		primaryFmt = "dotenv"
	}

	primaryPath := filepath.Join(projectDir, primary)
	if _, err := os.Stat(primaryPath); err == nil {
		return primary, primaryFmt
	}

	// Primary file doesn't exist — try fallback
	if e.FallbackFile != "" {
		fallbackPath := filepath.Join(projectDir, e.FallbackFile)
		if _, err := os.Stat(fallbackPath); err == nil {
			fallbackFmt := e.FallbackFormat
			if fallbackFmt == "" {
				fallbackFmt = "dotenv"
			}
			return e.FallbackFile, fallbackFmt
		}
	}

	// Return primary regardless (env.go will handle the missing file)
	return primary, primaryFmt
}

// laravelFramework is the only built-in framework definition.
var laravelFramework = &Framework{
	Name:      "laravel",
	Label:     "Laravel",
	PublicDir: "public",
	Create:    "composer create-project laravel/laravel",
	Detect: []FrameworkRule{
		{File: "artisan"},
		{Composer: "laravel/framework"},
	},
	Env: FrameworkEnvConf{
		File:        ".env",
		ExampleFile: ".env.example",
		Format:      "dotenv",
	},
	Composer: "auto",
	NPM:      "auto",
	Workers: map[string]FrameworkWorker{
		"queue": {
			Label:   "Queue Worker",
			Command: "php artisan queue:work --queue=default --tries=3 --timeout=60",
			Restart: "always",
		},
		"schedule": {
			Label:   "Task Scheduler",
			Command: "php artisan schedule:work",
			Restart: "always",
		},
		"reverb": {
			Label:   "Reverb WebSocket",
			Command: "php artisan reverb:start",
			Restart: "on-failure",
		},
	},
}

// GetFramework returns the framework definition for the given name.
// For non-laravel frameworks it checks user-defined YAMLs in FrameworksDir().
// For laravel it always starts from the built-in and merges any user-defined
// workers on top, so queue/schedule/reverb are always available.
// Returns (nil, false) if the framework is not found.
func GetFramework(name string) (*Framework, bool) {
	if name == "" {
		return nil, false
	}

	if name == "laravel" {
		// Start from a copy of the built-in
		merged := *laravelFramework
		mergedWorkers := make(map[string]FrameworkWorker, len(laravelFramework.Workers))
		for k, v := range laravelFramework.Workers {
			mergedWorkers[k] = v
		}
		// Merge user-defined workers (user additions/overrides win)
		path := filepath.Join(FrameworksDir(), "laravel.yaml")
		if data, err := os.ReadFile(path); err == nil {
			var userFw Framework
			if yaml.Unmarshal(data, &userFw) == nil {
				for k, v := range userFw.Workers {
					mergedWorkers[k] = v
				}
			}
		}
		merged.Workers = mergedWorkers
		return &merged, true
	}

	// User-defined YAML for other frameworks
	path := filepath.Join(FrameworksDir(), name+".yaml")
	if data, err := os.ReadFile(path); err == nil {
		var fw Framework
		if yaml.Unmarshal(data, &fw) == nil && fw.Name != "" {
			return &fw, true
		}
	}

	return nil, false
}

// DetectPublicDir inspects dir for a well-known PHP public directory and returns it.
// It checks directories used by common PHP frameworks in priority order.
// A candidate is accepted only if it contains an index.php file, ensuring the
// directory is actually the document root and not an empty placeholder.
// Returns "." if no valid candidate is found (serve from project root).
func DetectPublicDir(dir string) string {
	candidates := []string{"public", "web", "webroot", "pub", "www", "htdocs"}
	for _, c := range candidates {
		info, err := os.Stat(filepath.Join(dir, c))
		if err != nil || !info.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, c, "index.php")); err == nil {
			return c
		}
	}
	return "."
}

// DetectFramework inspects dir and returns the detected framework name.
// It checks laravel first (built-in), then user-defined frameworks in FrameworksDir().
// Returns ("", false) if no framework matches.
func DetectFramework(dir string) (string, bool) {
	// Laravel built-in first
	if matchesFramework(dir, laravelFramework) {
		return "laravel", true
	}

	// User-defined frameworks
	entries, _ := filepath.Glob(filepath.Join(FrameworksDir(), "*.yaml"))
	for _, yamlPath := range entries {
		data, err := os.ReadFile(yamlPath)
		if err != nil {
			continue
		}
		var fw Framework
		if yaml.Unmarshal(data, &fw) != nil || fw.Name == "" {
			continue
		}
		if matchesFramework(dir, &fw) {
			return fw.Name, true
		}
	}

	return "", false
}

// ListFrameworks returns all available framework definitions:
// the laravel built-in plus any user-defined YAMLs in FrameworksDir().
func ListFrameworks() []*Framework {
	result := []*Framework{laravelFramework}

	entries, _ := filepath.Glob(filepath.Join(FrameworksDir(), "*.yaml"))
	for _, yamlPath := range entries {
		data, err := os.ReadFile(yamlPath)
		if err != nil {
			continue
		}
		var fw Framework
		if yaml.Unmarshal(data, &fw) != nil || fw.Name == "" {
			continue
		}
		result = append(result, &fw)
	}

	return result
}

// SaveFramework writes a framework definition to FrameworksDir()/{name}.yaml.
// For the laravel built-in, only the Workers field is persisted (other fields
// come from the built-in definition and are always merged in by GetFramework).
func SaveFramework(fw *Framework) error {
	if err := os.MkdirAll(FrameworksDir(), 0755); err != nil {
		return err
	}
	toSave := fw
	if fw.Name == "laravel" {
		// Only persist workers — built-in handles everything else
		toSave = &Framework{Name: fw.Name, Workers: fw.Workers}
	}
	data, err := yaml.Marshal(toSave)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(FrameworksDir(), fw.Name+".yaml"), data, 0644)
}

// RemoveFramework deletes a user-defined framework YAML. For "laravel" it only
// removes the user workers overlay (the built-in definition remains).
func RemoveFramework(name string) error {
	return os.Remove(filepath.Join(FrameworksDir(), name+".yaml"))
}

func matchesFramework(dir string, fw *Framework) bool {
	if len(fw.Detect) == 0 {
		return false
	}
	for _, rule := range fw.Detect {
		if rule.File != "" {
			if _, err := os.Stat(filepath.Join(dir, rule.File)); err == nil {
				return true
			}
		}
		if rule.Composer != "" {
			if composerHasPackage(dir, rule.Composer) {
				return true
			}
		}
	}
	return false
}

func composerHasPackage(dir, pkg string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "composer.json"))
	if err != nil {
		return false
	}
	var c struct {
		Require    map[string]string `json:"require"`
		RequireDev map[string]string `json:"require-dev"`
	}
	if json.Unmarshal(data, &c) != nil {
		return false
	}
	_, inRequire := c.Require[pkg]
	_, inDev := c.RequireDev[pkg]
	return inRequire || inDev
}
