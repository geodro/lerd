package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
)

// TinkerSymbols is the set of project-defined names surfaced to the Tinker
// editor's autocomplete. Keep it minimal: classes the user is likely to type
// at the REPL.
type TinkerSymbols struct {
	Models    []string `json:"models"`
	Classes   []string `json:"classes"`
	Functions []string `json:"functions"`
}

var (
	classDeclRe = regexp.MustCompile(`(?m)^(?:final\s+|abstract\s+)?class\s+([A-Z][A-Za-z0-9_]*)\b`)
	// Heuristic for "this looks like a domain entity worth highlighting":
	// Laravel Eloquent, Doctrine ORM entities, plain Symfony entities.
	modelHintRe = regexp.MustCompile(
		`extends\s+(?:Model|Authenticatable|Pivot|MorphPivot|Eloquent\\Model)\b` +
			`|Illuminate\\Database\\Eloquent\\Model` +
			`|Doctrine\\ORM\\Mapping\\Entity` +
			`|#\[ORM\\Entity` +
			`|@ORM\\Entity` +
			`|@Entity\b`,
	)
	// Free-standing function declarations (composer files autoload, project
	// helpers files). PHP function names are case-insensitive at the parser
	// level but stylistically lowercase, so we look for that.
	funcDeclRe = regexp.MustCompile(`(?m)^\s*(?:#\[[^\]]+\]\s*)?function\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*\(`)
	// composer/autoload_files.php uses `$vendorDir . '/path'` for vendor
	// helper files and `$baseDir . '/path'` for project helper files.
	composerVendorPathRe = regexp.MustCompile(`\$vendorDir\s*\.\s*'([^']+)'`)
	composerBaseDirRe    = regexp.MustCompile(`\$baseDir\s*\.\s*'([^']+)'`)
)

// internalFnsCache memoizes the PHP-version-keyed list of `get_defined_functions()`
// internals so we don't pay the container exec cost on every request.
var internalFnsCache sync.Map // map[string][]string

// CollectTinkerSymbols scans the site for class declarations and flags
// model/entity-shaped ones. Source roots come from composer.json's
// `autoload.psr-4` section (works for Laravel `app/`, Symfony `src/`, and
// any framework that uses standard PSR-4 autoloading), with fallbacks for
// projects that don't declare PSR-4 paths.
func CollectTinkerSymbols(sitePath string) TinkerSymbols {
	models := map[string]struct{}{}
	classes := map[string]struct{}{}

	roots := resolveAutoloadRoots(sitePath)
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				name := d.Name()
				if name == "vendor" || name == "node_modules" || strings.HasPrefix(name, ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(d.Name(), ".php") {
				return nil
			}
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return nil
			}
			body := string(data)
			match := classDeclRe.FindStringSubmatch(body)
			if match == nil {
				return nil
			}
			name := match[1]
			classes[name] = struct{}{}
			if modelHintRe.MatchString(body) {
				models[name] = struct{}{}
			}
			return nil
		})
	}

	functions := map[string]struct{}{}
	for _, fn := range collectComposerAutoloadFunctions(sitePath) {
		functions[fn] = struct{}{}
	}
	for _, fn := range collectPHPInternalFunctions(sitePath) {
		functions[fn] = struct{}{}
	}

	return TinkerSymbols{
		Models:    sortedKeys(models),
		Classes:   sortedKeys(classes),
		Functions: sortedKeys(functions),
	}
}

// collectComposerAutoloadFunctions reads composer's generated
// `vendor/composer/autoload_files.php` (the index of every file Composer
// loads at boot to register helper functions) and harvests free-standing
// function declarations from each. This catches things like Laravel's
// `collect()`, `dd()`, Symfony polyfills, etc.
func collectComposerAutoloadFunctions(sitePath string) []string {
	autoload := filepath.Join(sitePath, "vendor", "composer", "autoload_files.php")
	data, err := os.ReadFile(autoload)
	if err != nil {
		return nil
	}
	body := string(data)

	fns := map[string]struct{}{}
	scan := func(absPath string) {
		// Cap each helper file at 256 KB so a runaway file can't stall us.
		f, err := os.Open(absPath)
		if err != nil {
			return
		}
		defer f.Close()
		buf := make([]byte, 256*1024)
		n, _ := f.Read(buf)
		for _, m := range funcDeclRe.FindAllSubmatch(buf[:n], -1) {
			fns[string(m[1])] = struct{}{}
		}
	}
	for _, m := range composerVendorPathRe.FindAllStringSubmatch(body, -1) {
		scan(filepath.Join(sitePath, "vendor", m[1]))
	}
	for _, m := range composerBaseDirRe.FindAllStringSubmatch(body, -1) {
		scan(filepath.Join(sitePath, m[1]))
	}

	out := make([]string, 0, len(fns))
	for fn := range fns {
		out = append(out, fn)
	}
	sort.Strings(out)
	return out
}

// collectPHPInternalFunctions returns the `get_defined_functions(true)['internal']`
// list for the site's PHP version, cached per version so the container
// exec only happens once. Returns nil if PHP can't be reached.
func collectPHPInternalFunctions(sitePath string) []string {
	version, err := phpDet.DetectVersion(sitePath)
	if err != nil || version == "" {
		return nil
	}
	if v, ok := internalFnsCache.Load(version); ok {
		return v.([]string)
	}

	container := fpmContainerForDir(sitePath, version)
	if running, _ := podman.ContainerRunning(container); !running {
		return nil
	}
	cmd := podman.Cmd(
		"exec", "-i", container,
		"php", "-r", `echo json_encode(get_defined_functions(true)["internal"] ?? []);`,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil
	}
	var fns []string
	if err := json.Unmarshal(stdout.Bytes(), &fns); err != nil {
		return nil
	}
	sort.Strings(fns)
	internalFnsCache.Store(version, fns)
	return fns
}

// resolveAutoloadRoots returns the directories to scan for project classes.
// Reads composer.json's `autoload.psr-4` and `autoload-dev.psr-4` mappings
// when present, falling back to common framework conventions otherwise.
func resolveAutoloadRoots(sitePath string) []string {
	seen := map[string]struct{}{}
	add := func(rel string) {
		p := filepath.Clean(filepath.Join(sitePath, rel))
		// Only include paths that exist and are inside the site dir.
		if !strings.HasPrefix(p, sitePath) {
			return
		}
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			seen[p] = struct{}{}
		}
	}

	// Try composer.json first.
	composerPath := filepath.Join(sitePath, "composer.json")
	if data, err := os.ReadFile(composerPath); err == nil {
		var cfg struct {
			Autoload struct {
				PSR4 map[string]any `json:"psr-4"`
			} `json:"autoload"`
			AutoloadDev struct {
				PSR4 map[string]any `json:"psr-4"`
			} `json:"autoload-dev"`
		}
		if err := json.Unmarshal(data, &cfg); err == nil {
			for _, v := range cfg.Autoload.PSR4 {
				addPSR4Value(v, add)
			}
			for _, v := range cfg.AutoloadDev.PSR4 {
				addPSR4Value(v, add)
			}
		}
	}

	// Convention fallbacks for projects without composer.json or with a
	// non-standard autoload section. Cheap to scan an extra dir if it
	// happens to also be in psr-4.
	add("app/Models")
	add("app")
	add("src")

	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// addPSR4Value handles the two shapes a psr-4 entry can take:
//
//	"App\\": "app/"            (string)
//	"App\\Tests\\": ["tests"]  (array of strings)
func addPSR4Value(v any, add func(string)) {
	switch x := v.(type) {
	case string:
		if x != "" {
			add(x)
		}
	case []any:
		for _, item := range x {
			if s, ok := item.(string); ok && s != "" {
				add(s)
			}
		}
	}
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
