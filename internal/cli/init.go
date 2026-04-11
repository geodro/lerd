package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
	phpPkg "github.com/geodro/lerd/internal/php"
	"github.com/spf13/cobra"
)

// NewInitCmd returns the init command.
func NewInitCmd() *cobra.Command {
	var fresh bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a project: run the setup wizard and save .lerd.yaml",
		Long: `Run the setup wizard to configure PHP version, HTTPS, and required services,
then save the answers to .lerd.yaml in the current directory.

If .lerd.yaml already exists the wizard is skipped and the saved configuration
is applied directly. Use --fresh to re-run the wizard with existing values as
defaults.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runInit(fresh)
		},
	}
	cmd.Flags().BoolVar(&fresh, "fresh", false, "Re-run the wizard even if .lerd.yaml already exists")
	return cmd
}

func runInit(fresh bool) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	lerdYAMLPath := filepath.Join(cwd, ".lerd.yaml")
	_, statErr := os.Stat(lerdYAMLPath)
	hasExisting := statErr == nil

	if !hasExisting || fresh {
		existing, err := config.LoadProjectConfig(cwd)
		if err != nil {
			return err
		}
		cfg, err := runWizard(cwd, existing)
		if err != nil {
			return err
		}
		if err := config.SaveProjectConfig(cwd, cfg); err != nil {
			return fmt.Errorf("saving .lerd.yaml: %w", err)
		}
		fmt.Println("Saved .lerd.yaml")
	}

	return applyProjectConfig(cwd)
}

func runWizard(cwd string, defaults *config.ProjectConfig) (*config.ProjectConfig, error) {
	gcfg, err := config.LoadGlobal()
	if err != nil {
		return nil, err
	}

	// Seed defaults from the site registry when no saved config exists yet,
	// so already-set PHP version and HTTPS state are reflected on first run.
	if defaults.PHPVersion == "" && !defaults.Secured {
		if site, err := config.FindSiteByPath(cwd); err == nil {
			if defaults.PHPVersion == "" {
				defaults.PHPVersion = site.PHPVersion
			}
			if !defaults.Secured {
				defaults.Secured = site.Secured
			}
		}
	}

	phpDefault := defaults.PHPVersion
	if phpDefault == "" {
		if v, detErr := phpPkg.DetectVersion(cwd); detErr == nil {
			phpDefault = v
		} else {
			phpDefault = gcfg.PHP.DefaultVersion
		}
	}

	framework, _ := resolveFramework(cwd)
	phpMin, phpMax := "", ""
	if framework != "" {
		if fw, fwOk := config.GetFrameworkForDir(framework, cwd); fwOk {
			phpMin, phpMax = fw.PHP.Min, fw.PHP.Max
		}
	}
	phpDefault = phpPkg.ClampToRange(phpDefault, phpMin, phpMax)

	// Database is picked as a single choice (sqlite | mysql family member |
	// postgres family member), while other services are a multi-select. This
	// mirrors the runtime prompt in `lerd env` and prevents users from
	// accidentally selecting both mysql and postgres for the same project.
	// Multi-version mysql/postgres alternates installed via presets show up as
	// extra Database options instead of polluting the Services list.
	dbOptions, dbNameSet := buildDatabaseOptions()
	nonDBServiceOptions := make([]string, 0, len(knownServices))
	for _, svc := range knownServices {
		if !dbNameSet[svc] {
			nonDBServiceOptions = append(nonDBServiceOptions, svc)
		}
	}
	if customs, err := config.ListCustomServices(); err == nil {
		for _, svc := range customs {
			if dbNameSet[svc.Name] {
				continue
			}
			// Skip developer tools that the project's code never consumes
			// (phpMyAdmin, pgAdmin, mongo-express). They have no env_vars
			// and no env_detect because they don't integrate with .env.
			if len(svc.EnvVars) == 0 && svc.EnvDetect == nil {
				continue
			}
			nonDBServiceOptions = append(nonDBServiceOptions, svc.Name)
		}
	}

	// Use saved named services as defaults if re-running (--fresh), otherwise auto-detect.
	serviceDefaults := defaults.ServiceNames()
	if len(serviceDefaults) == 0 {
		serviceDefaults = detectServicesFromDir(cwd)
	}

	// Split detected/saved services into the DB choice and the rest.
	dbChoice := "sqlite"
	for _, name := range serviceDefaults {
		if dbNameSet[name] {
			dbChoice = name
			break
		}
	}
	// If nothing was saved/detected for DB, fall back to whatever .env says
	// (or sqlite, which is also Laravel's default).
	if dbChoice == "sqlite" {
		switch detectDBConnection(cwd) {
		case "mysql", "mariadb":
			dbChoice = "mysql"
		case "pgsql", "postgres":
			dbChoice = "postgres"
		}
	}
	nonDBSelected := make([]string, 0, len(serviceDefaults))
	for _, name := range serviceDefaults {
		if !dbNameSet[name] {
			nonDBSelected = append(nonDBSelected, name)
		}
	}

	phpVersion := phpDefault
	nodeVersion := defaults.NodeVersion
	secured := defaults.Secured

	selectedWorkers := defaults.Workers
	if len(selectedWorkers) == 0 {
		selectedWorkers = []string{}
	}

	// If there are custom workers from the existing config, let the user
	// choose which to keep before the workers step.
	var customWorkerNames []string
	var keepCustomWorkers []string
	if len(defaults.CustomWorkers) > 0 {
		for name := range defaults.CustomWorkers {
			customWorkerNames = append(customWorkerNames, name)
		}
		sort.Strings(customWorkerNames)
		keepCustomWorkers = make([]string, len(customWorkerNames))
		copy(keepCustomWorkers, customWorkerNames)
	}

	formGroups := []*huh.Group{
		huh.NewGroup(
			huh.NewInput().
				Title("PHP version").
				Value(&phpVersion).
				Validate(func(s string) error {
					if s == "" {
						return nil
					}
					return validatePHPVersion(s)
				}),
			huh.NewInput().
				Title("Node version").
				Description("Leave blank to skip").
				Value(&nodeVersion),
			huh.NewConfirm().
				Title("Enable HTTPS?").
				Value(&secured),
			huh.NewSelect[string]().
				Title("Database").
				Options(dbOptions...).
				Value(&dbChoice),
			huh.NewMultiSelect[string]().
				Title("Services").
				Options(huh.NewOptions(nonDBServiceOptions...)...).
				Value(&nonDBSelected),
		),
	}

	if len(customWorkerNames) > 0 {
		formGroups = append(formGroups, huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Custom workers").
				Description("Deselect to remove from .lerd.yaml").
				Options(huh.NewOptions(customWorkerNames...)...).
				Value(&keepCustomWorkers),
		))
	}

	if err := huh.NewForm(formGroups...).WithTheme(huh.ThemeCatppuccin()).Run(); err != nil {
		return nil, err
	}

	// Build the set of kept custom workers.
	keptSet := make(map[string]bool, len(keepCustomWorkers))
	for _, name := range keepCustomWorkers {
		keptSet[name] = true
	}

	// Detect available workers from the framework definition.
	// Workers with ConflictsWith suppress conflicted workers (e.g. horizon suppresses queue).
	// Custom workers that were removed are excluded, and their conflict rules
	// no longer apply — so previously suppressed workers become available again.
	var workerOptions []string
	if fw, ok := config.GetFrameworkForDir(framework, cwd); ok && fw.Workers != nil {
		// First pass: identify which workers are removed custom workers.
		removedCustom := map[string]bool{}
		for name := range fw.Workers {
			if defaults.CustomWorkers[name].Command != "" && !keptSet[name] {
				removedCustom[name] = true
			}
		}
		// Build suppression set only from workers that are NOT removed.
		suppressed := map[string]bool{}
		for name, wDef := range fw.Workers {
			if removedCustom[name] {
				continue
			}
			if wDef.Check != nil && !config.MatchesRule(cwd, *wDef.Check) {
				continue
			}
			for _, c := range wDef.ConflictsWith {
				suppressed[c] = true
			}
		}
		for name, wDef := range fw.Workers {
			if removedCustom[name] {
				continue
			}
			if wDef.Check != nil && !config.MatchesRule(cwd, *wDef.Check) {
				continue
			}
			if suppressed[name] {
				continue
			}
			workerOptions = append(workerOptions, name)
		}
		sort.Strings(workerOptions)
	}

	// Remove any selected workers that are no longer available.
	filtered := selectedWorkers[:0]
	availableSet := make(map[string]bool, len(workerOptions))
	for _, w := range workerOptions {
		availableSet[w] = true
	}
	for _, w := range selectedWorkers {
		if availableSet[w] {
			filtered = append(filtered, w)
		}
	}
	selectedWorkers = filtered

	if len(workerOptions) > 0 {
		workerGroups := []*huh.Group{
			huh.NewGroup(
				huh.NewMultiSelect[string]().
					Title("Workers").
					Description("Auto-start when linking").
					Options(huh.NewOptions(workerOptions...)...).
					Value(&selectedWorkers),
			),
		}
		if err := huh.NewForm(workerGroups...).WithTheme(huh.ThemeCatppuccin()).Run(); err != nil {
			return nil, err
		}
	}

	// Recombine the database pick and the non-DB multi-select into a single
	// services list for serialization. dbChoice is always one of sqlite/mysql/postgres.
	selectedServices := make([]string, 0, len(nonDBSelected)+1)
	selectedServices = append(selectedServices, dbChoice)
	selectedServices = append(selectedServices, nonDBSelected...)

	// Only embed the framework definition in .lerd.yaml for user-defined
	// frameworks that aren't available from the store. Built-in (laravel) and
	// store-installed frameworks can be fetched on any machine.
	var frameworkDef *config.Framework
	if framework != "" {
		info := config.GetFrameworkSource(framework)
		if info == config.SourceUser {
			if fw, ok := config.GetFramework(framework); ok {
				frameworkDef = fw
			}
		}
	}

	// Build an index of custom service definitions to embed in .lerd.yaml.
	// Priority: existing inline definition in defaults > definition file on disk.
	// Built-in services (knownServices) are never embedded — they don't need to be.
	// sqlite is treated as built-in here even though it's not a quadlet service.
	builtIn := make(map[string]bool, len(knownServices)+1)
	for _, s := range knownServices {
		builtIn[s] = true
	}
	builtIn["sqlite"] = true
	inlineByName := map[string]*config.CustomService{}
	for _, svc := range defaults.Services {
		if svc.Custom != nil {
			inlineByName[svc.Name] = svc.Custom
		}
	}

	services := make([]config.ProjectService, len(selectedServices))
	for i, name := range selectedServices {
		if builtIn[name] {
			services[i] = config.ProjectService{Name: name}
			continue
		}
		// Prefer the on-disk service definition (it's freshest) and fall back
		// to the inlined one in defaults for portability.
		var loaded *config.CustomService
		if svc, err := config.LoadCustomService(name); err == nil {
			loaded = svc
		} else if existing := inlineByName[name]; existing != nil {
			loaded = existing
		}
		if loaded != nil && loaded.Preset != "" {
			services[i] = config.ProjectService{
				Name:          name,
				Preset:        loaded.Preset,
				PresetVersion: loaded.PresetVersion,
			}
			continue
		}
		services[i] = config.ProjectService{Name: name, Custom: loaded}
	}

	// Resolve framework version from the definition that was used.
	frameworkVersion := ""
	if frameworkDef != nil && frameworkDef.Version != "" {
		frameworkVersion = frameworkDef.Version
	} else if fw, ok := config.GetFrameworkForDir(framework, cwd); ok && fw.Version != "" {
		frameworkVersion = fw.Version
	}

	// Filter custom workers to only those the user chose to keep.
	var filteredCustomWorkers map[string]config.FrameworkWorker
	if len(keepCustomWorkers) > 0 {
		filteredCustomWorkers = make(map[string]config.FrameworkWorker, len(keepCustomWorkers))
		for _, name := range keepCustomWorkers {
			if w, ok := defaults.CustomWorkers[name]; ok {
				filteredCustomWorkers[name] = w
			}
		}
	}

	return &config.ProjectConfig{
		PHPVersion:       phpVersion,
		NodeVersion:      nodeVersion,
		Framework:        framework,
		FrameworkVersion: frameworkVersion,
		FrameworkDef:     frameworkDef,
		Secured:          secured,
		Services:         services,
		Workers:          selectedWorkers,
		CustomWorkers:    filteredCustomWorkers,
		AppURL:           defaults.AppURL,
		Domains:          defaults.Domains,
	}, nil
}

// dbFamilies is the set of service families considered databases by the init
// wizard. Members of these families show up in the Database select instead of
// the Services multi-select.
var dbFamilies = map[string]bool{
	"mysql":    true,
	"mariadb":  true,
	"postgres": true,
	"mongo":    true,
}

// dbFamilyOf returns the database family of svc, or empty when svc is not a
// database. Honours the explicit Family field first, then falls back to
// pattern inference for legacy installs that pre-date the field.
func dbFamilyOf(svc *config.CustomService) string {
	if svc.Family != "" && dbFamilies[svc.Family] {
		return svc.Family
	}
	if inferred := config.InferFamily(svc.Name); dbFamilies[inferred] {
		return inferred
	}
	return ""
}

// dbFamilyLabels maps a family name to the human-friendly label prefix shown
// in the wizard's Database select.
var dbFamilyLabels = map[string]string{
	"mysql":    "MySQL",
	"mariadb":  "MariaDB",
	"postgres": "PostgreSQL",
	"mongo":    "MongoDB",
}

// formatDBOptionLabel returns "MySQL (lerd-mysql)" for the canonical family
// member or "MySQL 5.7 (lerd-mysql-5-7)" for a versioned alternate.
func formatDBOptionLabel(name string) string {
	family := name
	version := ""
	if config.InferFamily(name) != "" {
		family = config.InferFamily(name)
		if rest := strings.TrimPrefix(name, family); rest != "" && rest != name {
			version = strings.TrimPrefix(rest, "-")
			version = strings.ReplaceAll(version, "-", ".")
		}
	}
	label := dbFamilyLabels[family]
	if label == "" {
		label = strings.ToUpper(family[:1]) + family[1:]
	}
	if version != "" {
		label += " " + version
	}
	return fmt.Sprintf("%s (lerd-%s)", label, name)
}

// buildDatabaseOptions returns the Database select options and a set of every
// service name that lives in a database family (so the Services multi-select
// can filter them out). Always includes sqlite. Built-in mysql and postgres
// are always present; alternates and mongo show up only when installed.
func buildDatabaseOptions() ([]huh.Option[string], map[string]bool) {
	nameSet := map[string]bool{"sqlite": true}
	options := []huh.Option[string]{huh.NewOption("SQLite (no service)", "sqlite")}

	for _, name := range []string{"mysql", "postgres"} {
		nameSet[name] = true
		options = append(options, huh.NewOption(formatDBOptionLabel(name), name))
	}

	if customs, err := config.ListCustomServices(); err == nil {
		var dbCustoms []*config.CustomService
		for _, svc := range customs {
			if dbFamilyOf(svc) != "" {
				dbCustoms = append(dbCustoms, svc)
			}
		}
		sort.Slice(dbCustoms, func(i, j int) bool { return dbCustoms[i].Name < dbCustoms[j].Name })
		for _, svc := range dbCustoms {
			nameSet[svc.Name] = true
			options = append(options, huh.NewOption(formatDBOptionLabel(svc.Name), svc.Name))
		}
	}

	return options, nameSet
}

// detectServicesFromDir inspects the project's env file and returns the list
// of services that appear to be in use. For frameworks that have explicit
// detection rules (e.g. wordpress, symfony), those rules are applied.
// For Laravel and unknown frameworks a set of standard heuristics is used.
func detectServicesFromDir(cwd string) []string {
	frameworkName, _ := resolveFramework(cwd)

	envFilePath := filepath.Join(cwd, ".env")
	envFormat := "dotenv"

	if fw, ok := config.GetFramework(frameworkName); ok {
		f, fmt := fw.Env.Resolve(cwd)
		envFilePath = filepath.Join(cwd, f)
		envFormat = fmt

		if len(fw.Env.Services) > 0 {
			return detectServicesFromRules(envExampleFallback(envFilePath), envFormat, fw.Env.Services)
		}
	}

	return detectServicesHeuristic(envExampleFallback(envFilePath), envFormat)
}

// detectDBConnection returns the lowercased DB_CONNECTION value from the
// project's env file, preferring .env over .env.example. Empty string when
// no env file exists or the key is unset.
func detectDBConnection(cwd string) string {
	frameworkName, _ := resolveFramework(cwd)

	envFilePath := filepath.Join(cwd, ".env")
	envFormat := "dotenv"

	if fw, ok := config.GetFramework(frameworkName); ok {
		f, fmtName := fw.Env.Resolve(cwd)
		envFilePath = filepath.Join(cwd, f)
		envFormat = fmtName
	}

	readKey := makeEnvReader(envExampleFallback(envFilePath), envFormat)
	return strings.ToLower(strings.TrimSpace(readKey("DB_CONNECTION")))
}

// envExampleFallback returns path if it exists, or path+".example" if that
// exists, otherwise path (callers already handle missing files gracefully).
func envExampleFallback(path string) string {
	if _, err := os.Stat(path); err == nil {
		return path
	}
	if example := path + ".example"; fileExists(example) {
		return example
	}
	return path
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// validatePHPVersion checks that the input looks like a valid PHP version
// (e.g. "8.3", "8.4") and rejects inputs like "8,5" or plain strings.
func validatePHPVersion(s string) error {
	parts := strings.SplitN(s, ".", 2)
	if len(parts) != 2 {
		return fmt.Errorf("PHP version must be in MAJOR.MINOR format, e.g. 8.3")
	}
	for _, p := range parts {
		if p == "" {
			return fmt.Errorf("PHP version must be in MAJOR.MINOR format, e.g. 8.3")
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return fmt.Errorf("PHP version must be in MAJOR.MINOR format, e.g. 8.3")
			}
		}
	}
	return nil
}

// detectServicesFromRules uses the FrameworkServiceDef detection rules from a
// framework YAML to determine which services are active.
func detectServicesFromRules(envFilePath, envFormat string, rules map[string]config.FrameworkServiceDef) []string {
	readKey := makeEnvReader(envFilePath, envFormat)

	var detected []string
	for _, svc := range knownServices {
		def, ok := rules[svc]
		if !ok || len(def.Detect) == 0 {
			continue
		}
		for _, cond := range def.Detect {
			val := readKey(cond.Key)
			if val == "" {
				continue
			}
			if cond.ValuePrefix == "" || strings.HasPrefix(val, cond.ValuePrefix) {
				detected = append(detected, svc)
				break
			}
		}
	}
	return detected
}

// detectServicesHeuristic detects services for Laravel-style .env files where
// no explicit framework service detection rules are defined.
func detectServicesHeuristic(envFilePath, envFormat string) []string {
	readKey := makeEnvReader(envFilePath, envFormat)

	var detected []string

	dbConn := readKey("DB_CONNECTION")
	switch dbConn {
	case "mysql":
		detected = append(detected, "mysql")
	case "pgsql", "postgres":
		detected = append(detected, "postgres")
	}

	if v := readKey("REDIS_HOST"); v != "" && v != "null" && v != "127.0.0.1" && v != "localhost" {
		detected = append(detected, "redis")
	}

	if readKey("SCOUT_DRIVER") == "meilisearch" || readKey("MEILISEARCH_HOST") != "" {
		detected = append(detected, "meilisearch")
	}

	if readKey("FILESYSTEM_DISK") == "s3" && readKey("AWS_ENDPOINT") != "" {
		detected = append(detected, "rustfs")
	}

	if mailHost := readKey("MAIL_HOST"); mailHost == "lerd-mailpit" || readKey("MAIL_PORT") == "1025" {
		detected = append(detected, "mailpit")
	}

	return detected
}

// makeEnvReader returns a function that reads a single key from the env file,
// handling both dotenv and php-const formats.
func makeEnvReader(envFilePath, envFormat string) func(key string) string {
	if envFormat == "php-const" {
		values, err := envfile.ReadPhpConst(envFilePath)
		if err != nil {
			return func(string) string { return "" }
		}
		return func(key string) string { return values[key] }
	}
	return func(key string) string { return envfile.ReadKey(envFilePath, key) }
}

// runSetupInit is called by lerd setup as its first step. It runs the init
// wizard when .lerd.yaml does not exist and we are in interactive mode, or
// silently applies the saved config when .lerd.yaml is already present.
// In non-interactive (--all) mode with no .lerd.yaml it falls back to a plain
// lerd link so setup can still run unattended.
func runSetupInit(cwd string, skipWizard bool) error {
	lerdYAMLPath := filepath.Join(cwd, ".lerd.yaml")
	_, statErr := os.Stat(lerdYAMLPath)
	hasExisting := statErr == nil

	if !hasExisting && skipWizard {
		// Non-interactive and no saved config — just link with auto-detection.
		linkSkipSetupPrompt = true
		defer func() { linkSkipSetupPrompt = false }()
		return runLink([]string{})
	}

	if !hasExisting {
		existing, _ := config.LoadProjectConfig(cwd)
		cfg, err := runWizard(cwd, existing)
		if err != nil {
			return err
		}
		if err := config.SaveProjectConfig(cwd, cfg); err != nil {
			return fmt.Errorf("saving .lerd.yaml: %w", err)
		}
		fmt.Println("Saved .lerd.yaml")
	}

	return applyProjectConfig(cwd)
}

func applyProjectConfig(cwd string) error {
	// Suppress the "Run lerd setup?" prompt inside runLink — we're already
	// in init/setup and the caller handles worker steps separately.
	linkSkipSetupPrompt = true
	defer func() { linkSkipSetupPrompt = false }()
	proj, err := config.LoadProjectConfig(cwd)
	if err != nil {
		return err
	}

	// Install PHP FPM with a progress loader if the version is not yet installed.
	// runLink handles everything else (framework restore, node-version, secure, services).
	if proj.PHPVersion != "" && !phpPkg.IsInstalled(proj.PHPVersion) {
		phpVersion := proj.PHPVersion
		jobs := []BuildJob{{
			Label: "PHP " + phpVersion + " FPM",
			Run: func(w io.Writer) error {
				return ensureFPMQuadletTo(phpVersion, w)
			},
		}}
		if err := RunParallel(jobs); err != nil {
			fmt.Printf("[WARN] PHP %s FPM: %v\n", phpVersion, err)
		}
	}

	if err := runLink([]string{}); err != nil {
		return err
	}

	// Apply the wizard's service choices (database, etc.) to .env so the user
	// sees DB_CONNECTION/DB_HOST/etc. updated immediately after the wizard.
	// Best-effort — failures are warned, not fatal, since the link itself
	// succeeded and the user can re-run `lerd env` manually.
	if err := runEnv(nil, nil); err != nil {
		fmt.Printf("[WARN] lerd env: %v\n", err)
	}
	return nil
}
