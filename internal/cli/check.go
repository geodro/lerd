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
		Short: "Validate .lerd.yaml syntax, services, and PHP version",
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
		fwName := cfg.Framework
		if fwName == "" {
			fwName, _ = config.DetectFramework(cwd)
		}
		fw, hasFw := config.GetFramework(fwName)

		hasQueue := false
		hasHorizon := false
		for _, w := range cfg.Workers {
			if w == "queue" {
				hasQueue = true
			}
			if w == "horizon" {
				hasHorizon = true
			}

			switch w {
			case "horizon":
				if !SiteHasHorizon(cwd) {
					fmt.Printf("  WARN  worker: %s — laravel/horizon is not installed\n", w)
					warnings++
				} else {
					fmt.Printf("  OK    worker: %s\n", w)
				}
			case "reverb":
				if !SiteUsesReverb(cwd) {
					fmt.Printf("  WARN  worker: %s — reverb is not configured (no laravel/reverb in composer.json and no BROADCAST_CONNECTION=reverb in .env)\n", w)
					warnings++
				} else {
					fmt.Printf("  OK    worker: %s\n", w)
				}
			case "queue", "schedule":
				if hasFw && fw.Workers != nil {
					if _, ok := fw.Workers[w]; ok {
						fmt.Printf("  OK    worker: %s\n", w)
					} else {
						fmt.Printf("  WARN  worker: %q is not defined for framework %s\n", w, fwName)
						warnings++
					}
				} else if fwName != "" {
					fmt.Printf("  WARN  worker: %q — framework %s has no worker definitions\n", w, fwName)
					warnings++
				} else {
					fmt.Printf("  WARN  worker: %q — no framework detected\n", w)
					warnings++
				}
			default:
				if hasFw && fw.Workers != nil {
					if _, ok := fw.Workers[w]; ok {
						fmt.Printf("  OK    worker: %s (custom)\n", w)
					} else {
						fmt.Printf("  FAIL  worker: %q is not defined for framework %s\n", w, fwName)
						errors++
					}
				} else {
					fmt.Printf("  FAIL  worker: %q — no framework worker definition found\n", w)
					errors++
				}
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
