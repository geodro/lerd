package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// FrameworkFetchFunc is a callback that fetches a framework definition from the
// store and saves it locally. It is called when GetFrameworkForDir cannot find a
// local definition for the detected version. The store package registers this at
// startup to avoid a circular import.
type FrameworkFetchFunc func(name, version string) (*Framework, error)

// frameworkFetchHook is set by the store package via RegisterFrameworkFetchHook.
var frameworkFetchHook FrameworkFetchFunc

// RegisterFrameworkFetchHook sets the callback used to auto-fetch missing
// framework definitions from the store.
func RegisterFrameworkFetchHook(fn FrameworkFetchFunc) {
	frameworkFetchHook = fn
}

// Framework describes a PHP project framework type.
type Framework struct {
	Name  string `yaml:"name"`
	Label string `yaml:"label"`
	// Version is the framework major version this definition targets (e.g. "11", "7").
	Version string `yaml:"version,omitempty"`
	// PHP defines the supported PHP version range for this framework version.
	PHP       FrameworkPHP               `yaml:"php,omitempty"`
	Detect    []FrameworkRule            `yaml:"detect,omitempty"`
	PublicDir string                     `yaml:"public_dir"`
	Env       FrameworkEnvConf           `yaml:"env,omitempty"`
	Composer  string                     `yaml:"composer,omitempty"` // auto | true | false
	NPM       string                     `yaml:"npm,omitempty"`      // auto | true | false
	Workers   map[string]FrameworkWorker `yaml:"workers,omitempty"`
	Setup     []FrameworkSetupCmd        `yaml:"setup,omitempty"`
	// Console is the console command to run (without 'php' prefix).
	// Example: "artisan", "bin/console"
	Console string `yaml:"console,omitempty"`
	// Create is the scaffold command used by "lerd new". The target directory is appended automatically.
	// Example: "composer create-project --no-install --no-plugins --no-scripts laravel/laravel"
	Create string `yaml:"create,omitempty"`
	// Logs defines where application log files live for this framework.
	Logs []FrameworkLogSource `yaml:"logs,omitempty"`
}

// FrameworkWorker describes a long-running process managed as a systemd service.
// The Command is executed inside the PHP-FPM container for the site.
type FrameworkWorker struct {
	Label         string         `yaml:"label,omitempty"`
	Command       string         `yaml:"command"`
	Restart       string         `yaml:"restart,omitempty"`        // always | on-failure (default: always)
	Check         *FrameworkRule `yaml:"check,omitempty"`          // only show when check passes (file exists or composer package installed)
	ConflictsWith []string       `yaml:"conflicts_with,omitempty"` // workers to stop before starting this one (e.g. horizon conflicts_with queue)
	Proxy         *WorkerProxy   `yaml:"proxy,omitempty"`          // WebSocket/HTTP proxy config for nginx
}

// WorkerProxy describes an HTTP/WebSocket proxy that nginx should configure
// for this worker. When present, nginx adds a location block that proxies
// requests to the worker inside the PHP-FPM container.
type WorkerProxy struct {
	Path        string `yaml:"path"`                   // URL path to proxy (e.g. "/app")
	PortEnvKey  string `yaml:"port_env_key,omitempty"` // env key holding the port (e.g. "REVERB_SERVER_PORT")
	DefaultPort int    `yaml:"default_port,omitempty"` // fallback port if env key is missing (default: 8080)
}

// FrameworkLogSource describes where application log files live for a framework.
type FrameworkLogSource struct {
	Path   string `yaml:"path"`             // glob relative to project root, e.g. "storage/logs/*.log"
	Format string `yaml:"format,omitempty"` // "monolog" | "raw" (default: "raw")
}

// FrameworkSetupCmd describes a one-off bootstrap command run during project setup.
type FrameworkSetupCmd struct {
	Label   string         `yaml:"label"`
	Command string         `yaml:"command"`
	Default bool           `yaml:"default,omitempty"`
	Check   *FrameworkRule `yaml:"check,omitempty"` // only show when check passes (file exists or composer package installed)
}

// FrameworkPHP defines the supported PHP version range for a framework version.
type FrameworkPHP struct {
	Min string `yaml:"min,omitempty"` // minimum PHP version (e.g. "8.2")
	Max string `yaml:"max,omitempty"` // maximum PHP version (e.g. "8.4")
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

	// KeyGeneration describes how to generate an application key if missing.
	KeyGeneration *EnvKeyGeneration `yaml:"key_generation,omitempty"`
}

// EnvKeyGeneration describes how to generate an application encryption key.
type EnvKeyGeneration struct {
	EnvKey         string `yaml:"env_key"`                   // env var to check/set (e.g. "APP_KEY")
	Command        string `yaml:"command,omitempty"`         // artisan command to run if vendor/ exists (e.g. "key:generate")
	FallbackPrefix string `yaml:"fallback_prefix,omitempty"` // prefix for random key fallback (e.g. "base64:")
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
	Create:    "composer create-project --no-install --no-plugins --no-scripts laravel/laravel",
	Detect: []FrameworkRule{
		{File: "artisan"},
		{Composer: "laravel/framework"},
	},
	Env: FrameworkEnvConf{
		File:        ".env",
		ExampleFile: ".env.example",
		Format:      "dotenv",
		KeyGeneration: &EnvKeyGeneration{
			EnvKey:         "APP_KEY",
			Command:        "key:generate",
			FallbackPrefix: "base64:",
		},
		Services: map[string]FrameworkServiceDef{
			"mysql": {
				Detect: []FrameworkServiceDetect{
					{Key: "DB_CONNECTION", ValuePrefix: "mysql"},
					{Key: "DB_CONNECTION", ValuePrefix: "mariadb"},
				},
				Vars: []string{
					"DB_CONNECTION=mysql",
					"DB_HOST=lerd-mysql",
					"DB_PORT=3306",
					"DB_DATABASE={{site}}",
					"DB_USERNAME=root",
					"DB_PASSWORD=lerd",
				},
			},
			"postgres": {
				Detect: []FrameworkServiceDetect{
					{Key: "DB_CONNECTION", ValuePrefix: "pgsql"},
				},
				Vars: []string{
					"DB_CONNECTION=pgsql",
					"DB_HOST=lerd-postgres",
					"DB_PORT=5432",
					"DB_DATABASE={{site}}",
					"DB_USERNAME=postgres",
					"DB_PASSWORD=lerd",
				},
			},
			"redis": {
				Detect: []FrameworkServiceDetect{
					{Key: "REDIS_HOST"},
					{Key: "CACHE_STORE", ValuePrefix: "redis"},
					{Key: "SESSION_DRIVER", ValuePrefix: "redis"},
					{Key: "QUEUE_CONNECTION", ValuePrefix: "redis"},
				},
				Vars: []string{
					"REDIS_HOST=lerd-redis",
					"REDIS_PORT=6379",
					"REDIS_PASSWORD=",
				},
			},
			"meilisearch": {
				Detect: []FrameworkServiceDetect{
					{Key: "SCOUT_DRIVER", ValuePrefix: "meilisearch"},
				},
				Vars: []string{
					"MEILISEARCH_HOST=http://lerd-meilisearch:7700",
					"MEILISEARCH_NO_ANALYTICS=true",
				},
			},
			"rustfs": {
				Detect: []FrameworkServiceDetect{
					{Key: "FILESYSTEM_DISK", ValuePrefix: "s3"},
					{Key: "AWS_ENDPOINT"},
				},
				Vars: []string{
					"AWS_ACCESS_KEY_ID=lerd",
					"AWS_SECRET_ACCESS_KEY=lerdpassword",
					"AWS_BUCKET={{site}}",
					"AWS_ENDPOINT=http://lerd-rustfs:9000",
					"AWS_URL=http://localhost:9000/{{site}}",
					"AWS_USE_PATH_STYLE_ENDPOINT=true",
				},
			},
			"mailpit": {
				Detect: []FrameworkServiceDetect{
					{Key: "MAIL_HOST"},
				},
				Vars: []string{
					"MAIL_MAILER=smtp",
					"MAIL_HOST=lerd-mailpit",
					"MAIL_PORT=1025",
					"MAIL_USERNAME=null",
					"MAIL_PASSWORD=null",
					"MAIL_ENCRYPTION=null",
				},
			},
		},
	},
	Composer: "auto",
	NPM:      "auto",
	Console:  "artisan",
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
			Check:   &FrameworkRule{Composer: "laravel/reverb"},
			Proxy: &WorkerProxy{
				Path:        "/app",
				PortEnvKey:  "REVERB_SERVER_PORT",
				DefaultPort: 8080,
			},
		},
		"horizon": {
			Label:         "Horizon",
			Command:       "php artisan horizon",
			Restart:       "always",
			Check:         &FrameworkRule{Composer: "laravel/horizon"},
			ConflictsWith: []string{"queue"},
		},
	},
	Setup: []FrameworkSetupCmd{
		{Label: "php artisan storage:link", Command: "php artisan storage:link", Default: true},
		{Label: "php artisan migrate", Command: "php artisan migrate", Default: true},
		{Label: "php artisan db:seed", Command: "php artisan db:seed", Default: false},
	},
	Logs: []FrameworkLogSource{
		{Path: "storage/logs/*.log", Format: "monolog"},
	},
}

// GetFramework returns the framework definition for the given name.
// It loads the base definition from the built-in (laravel), store, or user dir,
// then merges any user-defined overlay on top. The overlay can add/override
// workers and setup commands without replacing the entire definition.
// Returns (nil, false) if the framework is not found.
func GetFramework(name string) (*Framework, bool) {
	if name == "" {
		return nil, false
	}

	// Find the base definition.
	base := loadBaseFramework(name)
	if base == nil {
		// No base — check if a user-only definition exists (custom framework).
		if fw := loadFrameworkYAML(filepath.Join(FrameworksDir(), name+".yaml")); fw != nil {
			return fw, true
		}
		return nil, false
	}

	// Merge user overlay (if any) on top of the base.
	return mergeUserOverlay(base), true
}

// loadBaseFramework returns the base definition for a framework:
// built-in for "laravel", then store-installed (versioned > unversioned).
func loadBaseFramework(name string) *Framework {
	if name == "laravel" {
		// Copy built-in so callers don't mutate the global.
		fw := *laravelFramework
		workers := make(map[string]FrameworkWorker, len(laravelFramework.Workers))
		for k, v := range laravelFramework.Workers {
			workers[k] = v
		}
		fw.Workers = workers
		return &fw
	}

	// Store-installed: unversioned first (backwards compat), then versioned.
	if fw := loadFrameworkYAML(filepath.Join(StoreFrameworksDir(), name+".yaml")); fw != nil {
		return fw
	}
	return loadBestVersionedFramework(name, "")
}

// mergeUserOverlay checks for a user-defined overlay file in FrameworksDir()
// and merges its workers and setup commands on top of base.
// User additions/overrides win. If no overlay exists, base is returned as-is.
func mergeUserOverlay(base *Framework) *Framework {
	path := filepath.Join(FrameworksDir(), base.Name+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return base
	}
	var overlay Framework
	if yaml.Unmarshal(data, &overlay) != nil {
		return base
	}

	// Merge workers.
	if base.Workers == nil {
		base.Workers = make(map[string]FrameworkWorker)
	}
	for k, v := range overlay.Workers {
		base.Workers[k] = v
	}

	// Merge setup commands (overlay replaces the full list if provided).
	if overlay.Setup != nil {
		base.Setup = append(base.Setup, overlay.Setup...)
	}

	// Merge logs (overlay replaces the full list if provided).
	if overlay.Logs != nil {
		base.Logs = overlay.Logs
	}

	return base
}

// GetFrameworkForDir is like GetFramework but auto-detects the framework version
// from composer.lock in projectDir. If a version-specific store definition exists
// it is preferred over an unversioned one. User overlay workers are always merged.
// When a version is detected but no local definition exists, it attempts to fetch
// the definition from the store automatically.
func GetFrameworkForDir(name, projectDir string) (*Framework, bool) {
	if name == "" {
		return nil, false
	}

	// 1. Resolve version from composer.lock (source of truth) or .lerd.yaml (fallback).
	version := ComposerLockMajorVersion(projectDir, name)
	if proj, err := LoadProjectConfig(projectDir); err == nil {
		if version == "" && proj.FrameworkVersion != "" {
			version = proj.FrameworkVersion
		} else if version != "" && proj.FrameworkVersion != "" && version != proj.FrameworkVersion {
			proj.FrameworkVersion = version
			_ = SaveProjectConfig(projectDir, proj)
		}
	}

	// 2. Find the base definition from the store directory.
	var base *Framework
	versionedPath := ""
	if version != "" {
		versionedPath = filepath.Join(StoreFrameworksDir(), name+"@"+version+".yaml")
		base = loadFrameworkYAML(versionedPath)
	}

	// 3. Auto-fetch from the store: either the file is missing, or it's older
	//    than 24 hours and may have been updated upstream.
	if version != "" && frameworkFetchHook != nil {
		shouldFetch := base == nil
		if !shouldFetch && versionedPath != "" {
			if info, err := os.Stat(versionedPath); err == nil {
				shouldFetch = time.Since(info.ModTime()) > 24*time.Hour
			}
		}
		if shouldFetch {
			if fetched, err := frameworkFetchHook(name, version); err == nil && fetched != nil {
				base = fetched
			}
		}
	}

	// 4. Fall back to any available local definition.
	if base == nil {
		base = loadFrameworkYAML(filepath.Join(StoreFrameworksDir(), name+".yaml"))
	}
	if base == nil {
		base = loadBestVersionedFramework(name, "")
	}

	if base != nil {
		base = mergeUserOverlay(base)
		return mergeProjectWorkers(base, projectDir), true
	}

	// 4. For Laravel, fall back to the built-in definition.
	if name == "laravel" {
		fw, ok := GetFramework(name)
		if ok {
			return mergeProjectWorkers(fw, projectDir), true
		}
	}

	// 5. No store definition — check user-only definition (custom framework).
	if fw := loadFrameworkYAML(filepath.Join(FrameworksDir(), name+".yaml")); fw != nil {
		return mergeProjectWorkers(fw, projectDir), true
	}

	return nil, false
}

// mergeProjectWorkers merges custom_workers from .lerd.yaml on top of the
// framework definition. These are project-specific workers that live in git.
func mergeProjectWorkers(fw *Framework, projectDir string) *Framework {
	if projectDir == "" {
		return fw
	}
	proj, err := LoadProjectConfig(projectDir)
	if err != nil || len(proj.CustomWorkers) == 0 {
		return fw
	}
	if fw.Workers == nil {
		fw.Workers = make(map[string]FrameworkWorker)
	}
	for k, v := range proj.CustomWorkers {
		fw.Workers[k] = v
	}
	return fw
}

// GetFrameworkSource returns the source of the active framework definition.
// Returns SourceBuiltIn for "laravel", SourceUser if a user-defined file exists,
// SourceStore if a store-installed file exists, or "" if not found.
func GetFrameworkSource(name string) FrameworkSource {
	if name == "laravel" {
		return SourceBuiltIn
	}
	if loadFrameworkYAML(filepath.Join(FrameworksDir(), name+".yaml")) != nil {
		return SourceUser
	}
	// Check store (unversioned and versioned).
	if loadFrameworkYAML(filepath.Join(StoreFrameworksDir(), name+".yaml")) != nil {
		return SourceStore
	}
	matches, _ := filepath.Glob(filepath.Join(StoreFrameworksDir(), name+"@*.yaml"))
	if len(matches) > 0 {
		return SourceStore
	}
	return ""
}

// LoadUserFramework loads a user-defined framework from FrameworksDir().
// Returns nil if not found.
func LoadUserFramework(name string) *Framework {
	return loadFrameworkYAML(filepath.Join(FrameworksDir(), name+".yaml"))
}

// loadFrameworkYAML reads and parses a single framework YAML file.
func loadFrameworkYAML(path string) *Framework {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var fw Framework
	if yaml.Unmarshal(data, &fw) != nil || fw.Name == "" {
		return nil
	}
	return &fw
}

// loadBestVersionedFramework scans StoreFrameworksDir for <name>@<version>.yaml files.
// If preferVersion is set, it tries that first. Otherwise picks the first match
// alphabetically (which for numeric versions gives the latest).
func loadBestVersionedFramework(name, preferVersion string) *Framework {
	if preferVersion != "" {
		if fw := loadFrameworkYAML(filepath.Join(StoreFrameworksDir(), name+"@"+preferVersion+".yaml")); fw != nil {
			return fw
		}
	}
	pattern := filepath.Join(StoreFrameworksDir(), name+"@*.yaml")
	matches, _ := filepath.Glob(pattern)
	if len(matches) == 0 {
		return nil
	}
	// Reverse sort so highest version comes first (e.g. @7 before @6).
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))
	for _, path := range matches {
		if fw := loadFrameworkYAML(path); fw != nil {
			return fw
		}
	}
	return nil
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

	// User-defined frameworks, then store-installed (including versioned files).
	seen := map[string]bool{}
	for _, fwDir := range []string{FrameworksDir(), StoreFrameworksDir()} {
		entries, _ := filepath.Glob(filepath.Join(fwDir, "*.yaml"))
		for _, yamlPath := range entries {
			fw := loadFrameworkYAML(yamlPath)
			if fw == nil || seen[fw.Name] {
				continue
			}
			seen[fw.Name] = true
			if matchesFramework(dir, fw) {
				return fw.Name, true
			}
		}
	}

	return "", false
}

// ListFrameworks returns all available framework definitions:
// the laravel built-in plus any user-defined YAMLs in FrameworksDir().
func ListFrameworks() []*Framework {
	result := []*Framework{laravelFramework}
	seen := map[string]bool{"laravel": true}

	// User-defined first (unversioned), then store-installed.
	// For store, include both <name>.yaml and <name>@<version>.yaml.
	// Deduplicate by name — user-defined wins, then first store version seen.
	for _, fwDir := range []string{FrameworksDir(), StoreFrameworksDir()} {
		entries, _ := filepath.Glob(filepath.Join(fwDir, "*.yaml"))
		// Sort reverse so higher versions appear first.
		sort.Sort(sort.Reverse(sort.StringSlice(entries)))
		for _, yamlPath := range entries {
			fw := loadFrameworkYAML(yamlPath)
			if fw == nil {
				continue
			}
			if seen[fw.Name] {
				continue
			}
			seen[fw.Name] = true
			result = append(result, fw)
		}
	}

	return result
}

// FrameworkSource describes where a framework definition came from.
type FrameworkSource string

const (
	SourceBuiltIn FrameworkSource = "built-in"
	SourceUser    FrameworkSource = "user"
	SourceStore   FrameworkSource = "store"
)

// FrameworkInfo holds a framework definition together with its source metadata.
type FrameworkInfo struct {
	*Framework
	Source FrameworkSource
}

// ListFrameworksDetailed returns all available framework definitions with source info.
// Frameworks with a store base + user overlay show as "store" with merged workers.
func ListFrameworksDetailed() []FrameworkInfo {
	var result []FrameworkInfo
	seenNameVersion := map[string]bool{}
	hasStoreLaravel := false

	key := func(name, version string) string { return name + "@" + version }

	// Store-installed: each versioned file is a separate entry.
	storeEntries, _ := filepath.Glob(filepath.Join(StoreFrameworksDir(), "*.yaml"))
	sort.Sort(sort.Reverse(sort.StringSlice(storeEntries)))
	for _, yamlPath := range storeEntries {
		fw := loadFrameworkYAML(yamlPath)
		if fw == nil {
			continue
		}
		k := key(fw.Name, fw.Version)
		if seenNameVersion[k] {
			continue
		}
		seenNameVersion[k] = true
		merged := mergeUserOverlay(fw)
		result = append(result, FrameworkInfo{Framework: merged, Source: SourceStore})
		if fw.Name == "laravel" {
			hasStoreLaravel = true
		}
	}

	// Built-in Laravel (only if no store-installed version exists).
	if !hasStoreLaravel {
		if fw, ok := GetFramework("laravel"); ok {
			result = append(result, FrameworkInfo{Framework: fw, Source: SourceBuiltIn})
		}
	}

	// User-only (skip if a store version for the same name already listed).
	seenName := map[string]bool{"laravel": true}
	for _, info := range result {
		seenName[info.Name] = true
	}
	entries, _ := filepath.Glob(filepath.Join(FrameworksDir(), "*.yaml"))
	for _, yamlPath := range entries {
		fw := loadFrameworkYAML(yamlPath)
		if fw == nil || seenName[fw.Name] {
			continue
		}
		seenName[fw.Name] = true
		result = append(result, FrameworkInfo{Framework: fw, Source: SourceUser})
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
		// Only persist workers, setup, and logs — built-in handles everything else
		toSave = &Framework{Name: fw.Name, Workers: fw.Workers, Setup: fw.Setup, Logs: fw.Logs}
	}
	data, err := yaml.Marshal(toSave)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(FrameworksDir(), fw.Name+".yaml"), data, 0644)
}

// GetConsoleCommand returns the console binary (without the "php" prefix) for
// the framework detected in projectDir. It checks the site registry first, then
// falls back to auto-detection. For Laravel the default is "artisan".
func GetConsoleCommand(projectDir string) (string, error) {
	site, err := FindSiteByPath(projectDir)
	if err != nil || site.Framework == "" {
		return "", fmt.Errorf("no framework assigned — run 'lerd link' first")
	}

	fw, ok := GetFrameworkForDir(site.Framework, projectDir)
	if !ok {
		return "", fmt.Errorf("framework %q not found", site.Framework)
	}

	if fw.Console == "" {
		return "", fmt.Errorf(
			"no console command defined for framework %q — add 'console' field to %s/%s.yaml",
			fw.Name,
			FrameworksDir(),
			fw.Name,
		)
	}

	return fw.Console, nil
}

// SaveStoreFramework writes a store-installed framework definition to StoreFrameworksDir().
// If the framework has a Version field, the file is named <name>@<version>.yaml.
// Otherwise it is named <name>.yaml (backwards compatible).
func SaveStoreFramework(fw *Framework) error {
	dir := StoreFrameworksDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(fw)
	if err != nil {
		return err
	}
	filename := fw.Name + ".yaml"
	if fw.Version != "" {
		filename = fw.Name + "@" + fw.Version + ".yaml"
	}
	return os.WriteFile(filepath.Join(dir, filename), data, 0644)
}

// RemoveUserFramework silently removes a user-defined framework YAML if it exists.
// Used when migrating from user-defined to store-installed.
func RemoveUserFramework(name string) {
	os.Remove(filepath.Join(FrameworksDir(), name+".yaml")) //nolint:errcheck
}

// FrameworkFile describes a framework definition file on disk.
type FrameworkFile struct {
	Path    string
	Version string // "" for unversioned
	Source  FrameworkSource
}

// ListFrameworkFiles returns all definition files for a framework across user
// and store directories.
func ListFrameworkFiles(name string) []FrameworkFile {
	var files []FrameworkFile
	seen := make(map[string]bool)

	add := func(path string, source FrameworkSource) {
		if seen[path] {
			return
		}
		if _, err := os.Stat(path); err != nil {
			return
		}
		seen[path] = true
		version := ""
		base := filepath.Base(path)
		if i := strings.IndexByte(base, '@'); i != -1 {
			version = strings.TrimSuffix(base[i+1:], ".yaml")
		}
		files = append(files, FrameworkFile{Path: path, Version: version, Source: source})
	}

	add(filepath.Join(FrameworksDir(), name+".yaml"), SourceUser)

	storeDir := StoreFrameworksDir()
	add(filepath.Join(storeDir, name+".yaml"), SourceStore)
	matches, _ := filepath.Glob(filepath.Join(storeDir, name+"@*.yaml"))
	for _, m := range matches {
		add(m, SourceStore)
	}

	return files
}

// RemoveFrameworkFile removes a single framework definition file.
func RemoveFrameworkFile(path string) error {
	return os.Remove(path)
}

// RemoveFramework deletes all framework definition files (user and store) for
// the given name.
func RemoveFramework(name string) error {
	files := ListFrameworkFiles(name)
	if len(files) == 0 {
		return &os.PathError{Op: "remove", Path: name, Err: os.ErrNotExist}
	}
	for _, f := range files {
		os.Remove(f.Path) //nolint:errcheck
	}
	return nil
}

// HasWorker returns true if the framework defines a worker with the given name
// and (if the worker has a Check rule) the check passes for the project at dir.
func (fw *Framework) HasWorker(name, dir string) bool {
	w, ok := fw.Workers[name]
	if !ok {
		return false
	}
	if w.Check != nil && dir != "" {
		return MatchesRule(dir, *w.Check)
	}
	return true
}

// WorkerProxy returns the proxy configuration for the first worker that has one
// and whose check rule passes for the project at dir. Returns nil if no proxy is configured.
func (fw *Framework) DetectProxy(dir string) (*WorkerProxy, string) {
	for name, w := range fw.Workers {
		if w.Proxy == nil {
			continue
		}
		if w.Check != nil && !MatchesRule(dir, *w.Check) {
			continue
		}
		return w.Proxy, name
	}
	return nil, ""
}

// MatchesRule returns true if the given rule matches the project directory.
func MatchesRule(dir string, rule FrameworkRule) bool {
	if rule.File != "" {
		if _, err := os.Stat(filepath.Join(dir, rule.File)); err == nil {
			return true
		}
	}
	if rule.Composer != "" {
		if ComposerHasPackage(dir, rule.Composer) {
			return true
		}
	}
	return false
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
			if ComposerHasPackage(dir, rule.Composer) {
				return true
			}
		}
	}
	return false
}

// ComposerLockMajorVersion detects the major version of a framework from composer.lock.
// It scans store-installed definitions to find the composer package name for the framework,
// then looks up the version in composer.lock. Returns "" if not determinable.
func ComposerLockMajorVersion(projectDir, frameworkName string) string {
	if projectDir == "" {
		return ""
	}

	var packages []string
	if frameworkName == "laravel" {
		packages = []string{"laravel/framework"}
	} else {
		pattern := filepath.Join(StoreFrameworksDir(), frameworkName+"@*.yaml")
		matches, _ := filepath.Glob(pattern)
		matches = append(matches, filepath.Join(StoreFrameworksDir(), frameworkName+".yaml"))
		for _, path := range matches {
			if fw := loadFrameworkYAML(path); fw != nil {
				for _, rule := range fw.Detect {
					if rule.Composer != "" {
						packages = append(packages, rule.Composer)
					}
				}
				break
			}
		}
	}

	if len(packages) == 0 {
		return ""
	}

	data, err := os.ReadFile(filepath.Join(projectDir, "composer.lock"))
	if err != nil {
		return ""
	}
	var lock struct {
		Packages    []struct{ Name, Version string } `json:"packages"`
		PackagesDev []struct{ Name, Version string } `json:"packages-dev"`
	}
	if json.Unmarshal(data, &lock) != nil {
		return ""
	}

	all := append(lock.Packages, lock.PackagesDev...)
	for _, pkg := range packages {
		for _, p := range all {
			if p.Name == pkg {
				return extractMajorVersion(p.Version)
			}
		}
	}
	return ""
}

func extractMajorVersion(version string) string {
	v := strings.TrimPrefix(version, "v")
	if i := strings.IndexByte(v, '-'); i != -1 {
		v = v[:i]
	}
	parts := strings.SplitN(v, ".", 2)
	if len(parts) == 0 || parts[0] == "" {
		return ""
	}
	return parts[0]
}

// ComposerHasPackage reports whether the composer.json in dir lists pkg
// in require or require-dev.
func ComposerHasPackage(dir, pkg string) bool {
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
