package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/geodro/lerd/internal/config"
	phpPkg "github.com/geodro/lerd/internal/php"
	"github.com/spf13/cobra"
)

// NewCheckCmd returns the check command.
func NewCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Validate .lerd.yaml — PHP version, services, workers, container config, custom_workers, and db",
		RunE:  runCheck,
	}
}

func runCheck(_ *cobra.Command, _ []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	path := filepath.Join(cwd, ".lerd.yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("no .lerd.yaml found in %s — run lerd init to create one", cwd)
	}

	cfg, err := config.LoadProjectConfig(cwd)
	if err != nil {
		fmt.Printf("  FAIL  .lerd.yaml has invalid YAML syntax\n")
		fmt.Printf("        %v\n", err)
		return fmt.Errorf("validation failed")
	}

	warnings := 0
	errors := 0

	// PHP version
	if cfg.PHPVersion != "" {
		if err := validatePHPVersion(cfg.PHPVersion); err != nil {
			fmt.Printf("  FAIL  php_version: %s — %v\n", cfg.PHPVersion, err)
			errors++
		} else if !phpPkg.IsInstalled(cfg.PHPVersion) {
			fmt.Printf("  WARN  php_version: %s is not installed — run lerd php:install %s\n", cfg.PHPVersion, cfg.PHPVersion)
			warnings++
		} else {
			fmt.Printf("  OK    php_version: %s\n", cfg.PHPVersion)
		}
	}

	// Node version
	if cfg.NodeVersion != "" {
		fmt.Printf("  OK    node_version: %s\n", cfg.NodeVersion)
	}

	// Framework
	if cfg.Framework != "" {
		if cfg.FrameworkDef != nil {
			fmt.Printf("  OK    framework: %s (inline definition)\n", cfg.Framework)
		} else if _, ok := config.GetFramework(cfg.Framework); ok {
			fmt.Printf("  OK    framework: %s\n", cfg.Framework)
		} else {
			fmt.Printf("  WARN  framework: %q is not a known or user-defined framework\n", cfg.Framework)
			warnings++
		}
	}

	// Secured
	if cfg.Secured {
		fmt.Printf("  OK    secured: true\n")
	}

	// Domains
	if len(cfg.Domains) > 0 {
		fmt.Printf("  OK    domains: %v\n", cfg.Domains)
	}

	// Workers
	if len(cfg.Workers) > 0 {
		if cfg.Container != nil {
			// Custom container site: workers must be defined in custom_workers.
			for _, w := range cfg.Workers {
				if _, ok := cfg.CustomWorkers[w]; ok {
					fmt.Printf("  OK    worker: %s\n", w)
				} else {
					fmt.Printf("  FAIL  worker: %q is not defined in custom_workers\n", w)
					errors++
				}
			}
		} else {
			fwName := cfg.Framework
			fw, hasFw := config.GetFrameworkForDir(fwName, cwd)

			hasQueue := false
			hasHorizon := false
			for _, w := range cfg.Workers {
				if w == "queue" {
					hasQueue = true
				}
				if w == "horizon" {
					hasHorizon = true
				}

				if !hasFw || fw.Workers == nil {
					if fwName != "" {
						fmt.Printf("  WARN  worker: %q — framework %s has no worker definitions\n", w, fwName)
						warnings++
					} else {
						fmt.Printf("  WARN  worker: %q — no framework detected\n", w)
						warnings++
					}
					continue
				}
				wDef, ok := fw.Workers[w]
				if !ok {
					fmt.Printf("  FAIL  worker: %q is not defined for framework %s\n", w, fwName)
					errors++
					continue
				}
				if wDef.Check != nil && !config.MatchesRule(cwd, *wDef.Check) {
					fmt.Printf("  WARN  worker: %s — prerequisite not met (check rule failed)\n", w)
					warnings++
				} else {
					fmt.Printf("  OK    worker: %s\n", w)
				}
			}

			if hasQueue && hasHorizon {
				fmt.Printf("  WARN  workers: both queue and horizon are listed — horizon manages queues, queue worker will be skipped\n")
				warnings++
			}
			if hasQueue && SiteHasHorizon(cwd) {
				fmt.Printf("  WARN  workers: queue is listed but laravel/horizon is installed — horizon will be started instead\n")
				warnings++
			}
		}
	}

	// Services
	for _, svc := range cfg.Services {
		if svc.Custom != nil {
			// Inline definition — check required fields.
			if svc.Custom.Image == "" {
				fmt.Printf("  FAIL  service %q: inline definition is missing required \"image\" field\n", svc.Name)
				errors++
			} else {
				fmt.Printf("  OK    service: %s (inline, image: %s)\n", svc.Name, svc.Custom.Image)
			}
			continue
		}

		if svc.Preset != "" {
			// Preset reference — verify the preset exists in the catalog, then
			// check whether it has been installed on this machine.
			if _, err := config.LoadPreset(svc.Preset); err != nil {
				fmt.Printf("  FAIL  service %q: unknown preset %q\n", svc.Name, svc.Preset)
				errors++
			} else if _, err := config.LoadCustomService(svc.Name); err != nil {
				fmt.Printf("  WARN  service %s: preset %q not installed — run: lerd service preset install %s\n", svc.Name, svc.Preset, svc.Preset)
				warnings++
			} else {
				fmt.Printf("  OK    service: %s (preset: %s)\n", svc.Name, svc.Preset)
			}
			continue
		}

		if isKnownService(svc.Name) {
			fmt.Printf("  OK    service: %s\n", svc.Name)
			continue
		}

		// Check for custom service definition on disk.
		if _, err := config.LoadCustomService(svc.Name); err == nil {
			fmt.Printf("  OK    service: %s (custom)\n", svc.Name)
		} else {
			fmt.Printf("  FAIL  service %q: not a built-in service and no definition found at %s\n",
				svc.Name, filepath.Join(config.CustomServicesDir(), svc.Name+".yaml"))
			errors++
		}
	}

	// Container
	if cfg.Container != nil {
		if cfg.Container.Port <= 0 || cfg.Container.Port > 65535 {
			fmt.Printf("  FAIL  container.port: required and must be 1–65535\n")
			errors++
		} else {
			fmt.Printf("  OK    container.port: %d\n", cfg.Container.Port)
		}
		cfPath := cfg.Container.Containerfile
		if cfPath == "" {
			cfPath = "Containerfile.lerd"
		}
		if _, err := os.Stat(filepath.Join(cwd, cfPath)); os.IsNotExist(err) {
			fmt.Printf("  WARN  container.containerfile: %s not found — lerd link will fail\n", cfPath)
			warnings++
		} else {
			fmt.Printf("  OK    container.containerfile: %s\n", cfPath)
		}
		if cfg.Container.BuildContext != "" {
			if _, err := os.Stat(filepath.Join(cwd, cfg.Container.BuildContext)); os.IsNotExist(err) {
				fmt.Printf("  WARN  container.build_context: %s not found\n", cfg.Container.BuildContext)
				warnings++
			} else {
				fmt.Printf("  OK    container.build_context: %s\n", cfg.Container.BuildContext)
			}
		}
	}

	// custom_workers
	for name, w := range cfg.CustomWorkers {
		if w.Command == "" {
			fmt.Printf("  FAIL  custom_worker.%s: command is required\n", name)
			errors++
		} else {
			fmt.Printf("  OK    custom_worker.%s\n", name)
		}
	}

	// db
	if cfg.DB.Service != "" {
		if isKnownService(cfg.DB.Service) {
			fmt.Printf("  OK    db.service: %s\n", cfg.DB.Service)
		} else if _, err := config.LoadCustomService(cfg.DB.Service); err == nil {
			fmt.Printf("  OK    db.service: %s (custom)\n", cfg.DB.Service)
		} else {
			fmt.Printf("  FAIL  db.service: %q is not a known service\n", cfg.DB.Service)
			errors++
		}
	}

	// Summary
	fmt.Println()
	if errors > 0 {
		fmt.Printf("  %d error(s), %d warning(s)\n", errors, warnings)
		return fmt.Errorf("validation failed")
	}
	if warnings > 0 {
		fmt.Printf("  %d warning(s), no errors\n", warnings)
	} else {
		fmt.Printf("  .lerd.yaml is valid\n")
	}
	return nil
}
