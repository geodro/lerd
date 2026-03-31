package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	survey "github.com/AlecAivazis/survey/v2"
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

	var phpVersion string
	if err := survey.AskOne(&survey.Input{
		Message: "PHP version:",
		Default: phpDefault,
	}, &phpVersion); err != nil {
		return nil, err
	}

	var secured bool
	if err := survey.AskOne(&survey.Confirm{
		Message: "Enable HTTPS?",
		Default: defaults.Secured,
	}, &secured); err != nil {
		return nil, err
	}

	// Use saved services as defaults if re-running (--fresh), otherwise auto-detect.
	serviceDefaults := defaults.Services
	if len(serviceDefaults) == 0 {
		serviceDefaults = detectServicesFromDir(cwd)
	}

	var selectedServices []string
	if err := survey.AskOne(&survey.MultiSelect{
		Message: "Services needed:",
		Options: knownServices,
		Default: serviceDefaults,
	}, &selectedServices); err != nil {
		return nil, err
	}

	framework, _ := resolveFramework(cwd)

	return &config.ProjectConfig{
		PHPVersion: phpVersion,
		Framework:  framework,
		Secured:    secured,
		Services:   selectedServices,
	}, nil
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
			return detectServicesFromRules(envFilePath, envFormat, fw.Env.Services)
		}
	}

	return detectServicesHeuristic(envFilePath, envFormat)
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
		return runLink([]string{}, "")
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
	proj, err := config.LoadProjectConfig(cwd)
	if err != nil {
		return err
	}

	// Install PHP FPM with a progress loader if the version is not yet installed.
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

	if err := runLink([]string{}, ""); err != nil {
		return fmt.Errorf("linking site: %w", err)
	}

	if site, findErr := config.FindSiteByPath(cwd); findErr == nil {
		if proj.Secured && !site.Secured {
			if err := runSecure(nil, []string{}); err != nil {
				fmt.Printf("[WARN] enabling HTTPS: %v\n", err)
			}
		} else if !proj.Secured && site.Secured {
			if err := runUnsecure(nil, []string{}); err != nil {
				fmt.Printf("[WARN] disabling HTTPS: %v\n", err)
			}
		}
	}

	for _, svc := range proj.Services {
		if err := ensureServiceRunning(svc); err != nil {
			fmt.Printf("[WARN] service %s: %v\n", svc, err)
		}
	}

	return nil
}
