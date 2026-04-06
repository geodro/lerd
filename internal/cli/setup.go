package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/spf13/cobra"
)

// setupStep describes one bootstrap action.
type setupStep struct {
	label   string
	enabled bool // default selection
	run     func() error
}

// NewSetupCmd returns the setup command.
func NewSetupCmd() *cobra.Command {
	var allSteps bool
	var skipOpen bool

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Bootstrap a PHP project (composer, npm, env, migrate, assets, open)",
		Long: `Configures the site and runs a series of standard project setup steps with
an interactive step-selector so you can toggle which steps to execute.

Before the step selector, lerd setup runs the lerd init wizard so you can
choose the PHP version, HTTPS, and required services. The answers are saved
to .lerd.yaml (commit it for portability). On subsequent runs, or when
.lerd.yaml already exists, the config is applied silently with no prompts.

Steps for all frameworks:
  1. composer install        — skipped if vendor/ already exists
  2. npm install/ci          — skipped if node_modules/ already exists (uses ci if lockfile exists)
  3. lerd env                — configure env file with lerd service settings
  4. lerd mcp:inject         — inject MCP config (off by default)
  5. npm run <build|production|prod> — build front-end assets (detected from package.json scripts)
  6. lerd secure             — enable HTTPS via mkcert (off by default)

Additional steps for Laravel projects:
  7. php artisan storage:link — create storage symlink
  8. php artisan migrate     — run database migrations
  9. php artisan db:seed     — seed the database (off by default)
  10. queue:start            — start queue worker
  11. stripe:listen          — start Stripe webhook listener (off by default)
  12. schedule:start         — start task scheduler
  13. reverb:start           — start Reverb WebSocket server (if configured)

Use --all to skip all selectors and run everything (useful in CI). In --all
mode with no .lerd.yaml, site registration falls back to auto-detection.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runSetup(allSteps, skipOpen)
		},
	}

	cmd.Flags().BoolVarP(&allSteps, "all", "a", false, "Select all steps without prompting (for CI/automation)")
	cmd.Flags().BoolVar(&skipOpen, "skip-open", false, "Do not open the site in the browser at the end")
	return cmd
}

func runSetup(allSteps, skipOpen bool) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	// Run init wizard (or apply saved .lerd.yaml) before any other step so
	// PHP version, HTTPS, and services are configured first.
	fmt.Println("→ Configuring site...")
	if err := runSetupInit(cwd, allSteps); err != nil {
		fmt.Printf("  [WARN] %v\n", err)
	}

	site, _ := config.FindSiteByPath(cwd)
	isLaravel := site != nil && site.IsLaravel()

	// Load saved workers from .lerd.yaml to pre-select them in the step selector.
	projCfg, _ := config.LoadProjectConfig(cwd)
	savedWorkers := make(map[string]bool)
	if projCfg != nil {
		for _, w := range projCfg.Workers {
			savedWorkers[w] = true
		}
	}

	_, vendorMissing := os.Stat(cwd + "/vendor")
	_, nodeModulesMissing := os.Stat(cwd + "/node_modules")
	_, pkgJSONErr := os.Stat(cwd + "/package.json")
	hasPackageJSON := pkgJSONErr == nil
	_, lockMissing := os.Stat(cwd + "/package-lock.json")
	_, shrinkMissing := os.Stat(cwd + "/npm-shrinkwrap.json")
	hasLockFile := lockMissing == nil || shrinkMissing == nil
	buildScript := detectBuildScript(cwd + "/package.json")

	// Always run lerd env first — it configures .env, starts services, and
	// creates databases before any other step that may depend on them.
	fmt.Println("\n→ Running: lerd env")
	if err := runEnv(nil, nil); err != nil {
		fmt.Printf("  [WARN] lerd env: %v\n", err)
	}

	steps := []setupStep{
		{
			label:   "composer install",
			enabled: os.IsNotExist(vendorMissing),
			run: func() error {
				cmd := exec.Command("composer", "install")
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				return cmd.Run()
			},
		},
		{
			label:   "npm install/ci",
			enabled: os.IsNotExist(nodeModulesMissing) && hasPackageJSON,
			run: func() error {
				if hasLockFile {
					return runWithFnm("npm", []string{"ci"})
				}
				return runWithFnm("npm", []string{"install"})
			},
		},
		{
			label:   "lerd mcp:inject",
			enabled: false,
			run: func() error {
				return runMCPInject("")
			},
		},
		{
			label:   "npm run " + buildScript,
			enabled: hasPackageJSON && buildScript != "",
			run: func() error {
				return runWithFnm("npm", []string{"run", buildScript})
			},
		},
	}

	// Framework setup commands (one-off bootstrap steps like migrations, storage:link, etc.)
	if site != nil {
		fwName := site.Framework
		if fwName == "" {
			fwName, _ = config.DetectFramework(cwd)
		}
		if fw, ok := config.GetFramework(fwName); ok {
			for _, sc := range fw.Setup {
				// Skip commands whose check doesn't pass.
				if sc.Check != nil && !config.MatchesRule(cwd, *sc.Check) {
					continue
				}
				setupCmd := sc
				enabled := setupCmd.Default
				// Laravel dynamic default: only enable storage:link when actually needed.
				if fwName == "laravel" && strings.Contains(setupCmd.Command, "storage:link") {
					enabled = siteNeedsStorageLink(cwd)
				}
				steps = append(steps, setupStep{
					label:   setupCmd.Label,
					enabled: enabled,
					run: func() error {
						return execInContainer(cwd, setupCmd.Command)
					},
				})
			}
		}
	}

	// Only offer the secure step when the site isn't already secured by lerd init.
	if site == nil || !site.Secured {
		steps = append(steps, setupStep{
			label:   "lerd secure",
			enabled: false,
			run: func() error {
				return runSecure(nil, nil)
			},
		})
	}

	if isLaravel {
		// Show horizon instead of queue when laravel/horizon is installed.
		if SiteHasHorizon(cwd) {
			steps = append(steps, setupStep{
				label:   "horizon:start",
				enabled: savedWorkers["horizon"],
				run: func() error {
					s, err := config.FindSiteByPath(cwd)
					if err != nil {
						return fmt.Errorf("site not registered: %w", err)
					}
					phpVersion := s.PHPVersion
					if phpVersion == "" {
						if detected, detErr := phpDet.DetectVersion(cwd); detErr == nil {
							phpVersion = detected
						} else {
							cfg, _ := config.LoadGlobal()
							phpVersion = cfg.PHP.DefaultVersion
						}
					}
					return HorizonStartForSite(s.Name, cwd, phpVersion)
				},
			})
		} else {
			steps = append(steps, setupStep{
				label:   "queue:start",
				enabled: savedWorkers["queue"] || siteUsesRedisQueue(cwd),
				run: func() error {
					s, err := config.FindSiteByPath(cwd)
					if err != nil {
						return fmt.Errorf("site not registered: %w", err)
					}
					phpVersion := s.PHPVersion
					if phpVersion == "" {
						if detected, detErr := phpDet.DetectVersion(cwd); detErr == nil {
							phpVersion = detected
						} else {
							cfg, _ := config.LoadGlobal()
							phpVersion = cfg.PHP.DefaultVersion
						}
					}
					return QueueStartForSite(s.Name, cwd, phpVersion)
				},
			})
		}

		steps = append(steps, setupStep{
			label:   "stripe:listen",
			enabled: siteHasStripeSecret(cwd),
			run: func() error {
				s, err := config.FindSiteByPath(cwd)
				if err != nil {
					return fmt.Errorf("site not registered: %w", err)
				}
				base := siteURL(cwd)
				if base == "" {
					return fmt.Errorf("could not resolve site URL — run 'lerd link' first")
				}
				return StripeStartForSite(s.Name, cwd, base)
			},
		})

		steps = append(steps, setupStep{
			label:   "schedule:start",
			enabled: savedWorkers["schedule"],
			run: func() error {
				s, err := config.FindSiteByPath(cwd)
				if err != nil {
					return fmt.Errorf("site not registered: %w", err)
				}
				phpVersion := s.PHPVersion
				if phpVersion == "" {
					if detected, detErr := phpDet.DetectVersion(cwd); detErr == nil {
						phpVersion = detected
					} else {
						cfg, _ := config.LoadGlobal()
						phpVersion = cfg.PHP.DefaultVersion
					}
				}
				return ScheduleStartForSite(s.Name, cwd, phpVersion)
			},
		})

		steps = append(steps, setupStep{
			label:   "reverb:start",
			enabled: savedWorkers["reverb"] || SiteUsesReverb(cwd),
			run: func() error {
				s, err := config.FindSiteByPath(cwd)
				if err != nil {
					return fmt.Errorf("site not registered: %w", err)
				}
				phpVersion := s.PHPVersion
				if phpVersion == "" {
					if detected, detErr := phpDet.DetectVersion(cwd); detErr == nil {
						phpVersion = detected
					} else {
						cfg, _ := config.LoadGlobal()
						phpVersion = cfg.PHP.DefaultVersion
					}
				}
				return ReverbStartForSite(s.Name, cwd, phpVersion)
			},
		})
	}

	// Custom framework workers (e.g. messenger for Symfony).
	// For Laravel, skip built-in worker names already handled above.
	if site != nil {
		fwName := site.Framework
		if fwName == "" {
			fwName, _ = config.DetectFramework(cwd)
		}
		if fw, ok := config.GetFramework(fwName); ok && fw.Workers != nil {
			for wName, wDef := range fw.Workers {
				if wDef.Check != nil && !config.MatchesRule(cwd, *wDef.Check) {
					continue
				}
				if isLaravel {
					switch wName {
					case "queue", "schedule", "reverb":
						continue
					}
				}
				wn := wName
				wd := wDef
				steps = append(steps, setupStep{
					label:   wn + ":start",
					enabled: savedWorkers[wn],
					run: func() error {
						s, err := config.FindSiteByPath(cwd)
						if err != nil {
							return fmt.Errorf("site not registered: %w", err)
						}
						phpVersion := s.PHPVersion
						if phpVersion == "" {
							if detected, detErr := phpDet.DetectVersion(cwd); detErr == nil {
								phpVersion = detected
							} else {
								cfg, _ := config.LoadGlobal()
								phpVersion = cfg.PHP.DefaultVersion
							}
						}
						return WorkerStartForSite(s.Name, cwd, phpVersion, wn, wd)
					},
				})
			}
		}
	}

	if !skipOpen {
		steps = append(steps, setupStep{
			label:   "lerd open",
			enabled: true,
			run: func() error {
				return runOpen(nil, nil)
			},
		})
	}

	// Determine which steps to run.
	var selected []string
	if allSteps {
		for _, s := range steps {
			selected = append(selected, s.label)
		}
	} else {
		options := make([]string, len(steps))
		defaults := []string{}
		for i, s := range steps {
			options[i] = s.label
			if s.enabled {
				defaults = append(defaults, s.label)
			}
		}

		selected = defaults // pre-select enabled steps
		if err := huh.NewForm(
			huh.NewGroup(
				huh.NewMultiSelect[string]().
					Title("Setup steps").
					Options(huh.NewOptions(options...)...).
					Value(&selected),
			),
		).WithTheme(huh.ThemeCatppuccin()).Run(); err != nil {
			return err
		}
	}

	if len(selected) == 0 {
		fmt.Println("No steps selected. Nothing to do.")
		return nil
	}

	// Build a set for O(1) lookup.
	selectedSet := make(map[string]bool, len(selected))
	for _, s := range selected {
		selectedSet[s] = true
	}

	// Execute steps in order.
	for _, s := range steps {
		if !selectedSet[s.label] {
			continue
		}
		fmt.Printf("\n→ Running: %s\n", s.label)
		if err := s.run(); err != nil {
			fmt.Printf("✗ %s failed: %v\n", s.label, err)
			if !promptContinue() {
				return fmt.Errorf("setup aborted after %q failed", s.label)
			}
		}
	}

	fmt.Println("\nSetup complete.")
	return nil
}

// detectBuildScript reads package.json and returns the best build script name.
// Priority: build (vite/default) → production (laravel-mix) → prod → "".
func detectBuildScript(pkgJSONPath string) string {
	data, err := os.ReadFile(pkgJSONPath)
	if err != nil {
		return "build"
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return "build"
	}
	for _, candidate := range []string{"build", "production", "prod"} {
		if _, ok := pkg.Scripts[candidate]; ok {
			return candidate
		}
	}
	return ""
}

// siteUsesRedisQueue returns true if the site at cwd has QUEUE_CONNECTION=redis.
// Checks .env first, falls back to .env.example (for projects not yet configured).
func siteUsesRedisQueue(cwd string) bool {
	for _, name := range []string{".env", ".env.example"} {
		v := envfile.ReadKey(filepath.Join(cwd, name), "QUEUE_CONNECTION")
		if v != "" {
			return v == "redis"
		}
	}
	return false
}

// siteNeedsStorageLink returns true when storage:link has not been run yet and
// the site uses the local filesystem disk (the default).
func siteNeedsStorageLink(cwd string) bool {
	if _, err := os.Lstat(filepath.Join(cwd, "public", "storage")); err == nil {
		return false // symlink already exists
	}
	for _, name := range []string{".env", ".env.example"} {
		v := envfile.ReadKey(filepath.Join(cwd, name), "FILESYSTEM_DISK")
		if v != "" {
			return v == "local"
		}
	}
	return true // FILESYSTEM_DISK unset → defaults to local
}

// siteHasStripeSecret returns true if STRIPE_SECRET is present in .env or .env.example.
func siteHasStripeSecret(cwd string) bool {
	for _, name := range []string{".env", ".env.example"} {
		if envfile.ReadKey(filepath.Join(cwd, name), "STRIPE_SECRET") != "" {
			return true
		}
	}
	return false
}

// execInContainer runs an arbitrary command string inside the site's PHP-FPM container.
func execInContainer(dir, command string) error {
	version, err := phpDet.DetectVersion(dir)
	if err != nil {
		cfg, _ := config.LoadGlobal()
		version = cfg.PHP.DefaultVersion
	}
	short := strings.ReplaceAll(version, ".", "")
	container := "lerd-php" + short + "-fpm"
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return fmt.Errorf("empty setup command")
	}
	cmdArgs := append([]string{"exec", "-i", "-w", dir, container}, parts...)
	cmd := exec.Command("podman", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// promptContinue asks the user whether to continue after a step failure.
// Returns true if the user wants to continue.
func promptContinue() bool {
	fmt.Print("  Continue with remaining steps? [y/N]: ")
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
		return answer == "y" || answer == "yes"
	}
	return false
}
