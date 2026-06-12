package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
	nodeDet "github.com/geodro/lerd/internal/node"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	lerdSystemd "github.com/geodro/lerd/internal/systemd"
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
	// Worktrees aren't registered as sites; fall back to the parent so
	// setup steps (workers, framework cmds) still apply against cwd.
	if site == nil {
		if parent, _, ok := findOwningWorktree(cwd); ok {
			site = parent
		}
	}

	// Load saved workers from .lerd.yaml to pre-select them in the step selector.
	projCfg, _ := config.LoadProjectConfig(cwd)
	savedWorkers := make(map[string]bool)
	if projCfg != nil {
		for _, w := range projCfg.Workers {
			savedWorkers[w] = true
		}
	}

	_, vendorMissing := os.Stat(cwd + "/vendor")
	_, composerJSONErr := os.Stat(cwd + "/composer.json")
	hasComposerJSON := composerJSONErr == nil
	_, nodeModulesMissing := os.Stat(cwd + "/node_modules")
	_, pkgJSONErr := os.Stat(cwd + "/package.json")
	hasPackageJSON := pkgJSONErr == nil
	_, lockMissing := os.Stat(cwd + "/package-lock.json")
	_, shrinkMissing := os.Stat(cwd + "/npm-shrinkwrap.json")
	hasLockFile := lockMissing == nil || shrinkMissing == nil
	buildScript := detectBuildScript(cwd + "/package.json")
	// If the user opted into a worker that replaces the build (vite et al),
	// pre-disable the build step. Still toggleable in the chooser.
	buildReplaced := false
	if site != nil {
		buildReplaced = len(OptedInBuildReplacers(site, cwd)) > 0
	}

	// runSetupInit -> applyProjectConfig already ran `lerd env`; do not
	// duplicate it here.

	// Resolve the PHP version backing this site so a bun project can have bun
	// auto-installed into its container without the user running a second
	// command.
	bunPHPVersion := ""
	if site != nil {
		bunPHPVersion = site.PHPVersion
	}
	if bunPHPVersion == "" {
		if v, err := phpDet.DetectVersion(cwd); err == nil {
			bunPHPVersion = v
		}
	}

	steps := []setupStep{}
	// composer install only makes sense for a PHP project; skip it entirely for
	// Node-only / host-proxy sites that have no composer.json.
	if hasComposerJSON {
		steps = append(steps, setupStep{
			label:   "composer install",
			enabled: os.IsNotExist(vendorMissing),
			run: func() error {
				return composerInContainer(cwd, "install")
			},
		})
	}
	// Labels reflect the JS runtime lerd will actually drive (bun for bun
	// projects when bun is on the host, npm otherwise); the run funcs dispatch
	// the same way via runJSInstall/runJSScript.
	jsRuntime := "npm"
	if nodeDet.UsesBun(cwd) && nodeDet.BunPath() != "" {
		jsRuntime = "bun"
	}
	installLabel := "npm install/ci"
	buildLabel := "npm run " + buildScript
	if jsRuntime == "bun" {
		installLabel = "bun install"
		buildLabel = "bun run " + buildScript
	}
	steps = append(steps, []setupStep{
		{
			label:   installLabel,
			enabled: os.IsNotExist(nodeModulesMissing) && hasPackageJSON,
			run: func() error {
				return runJSInstall(cwd, hasLockFile)
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
			label:   buildLabel,
			enabled: hasPackageJSON && buildScript != "" && !buildReplaced,
			run: func() error {
				if _, err := os.Stat(cwd + "/node_modules"); os.IsNotExist(err) {
					fmt.Println("  node_modules not found.")
					fmt.Printf("  Run %s install first? [Y/n]: ", jsRuntime)
					scanner := bufio.NewScanner(os.Stdin)
					if scanner.Scan() {
						answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
						if answer == "" || answer == "y" || answer == "yes" {
							fmt.Printf("\n→ Running: %s install\n", jsRuntime)
							if err := runJSInstall(cwd, hasLockFile); err != nil {
								return fmt.Errorf("%s install failed: %w", jsRuntime, err)
							}
						} else {
							return fmt.Errorf("node_modules not found — skipping build")
						}
					}
				}
				return runJSScript(cwd, buildScript)
			},
		},
		{
			// Mirror the host's bun into the PHP-FPM container so `lerd shell`
			// has a working (musl) bun, with no extra command. lerd never
			// installs bun on the host; this only fires when the user already
			// has bun installed there. Idempotent: skips when the container bun
			// is already present, and installContainerBun only restarts the
			// container if the volume isn't mounted yet. Non-fatal.
			label:   "bun (container)",
			enabled: nodeDet.BunPath() != "" && bunPHPVersion != "",
			run: func() error {
				// Cheap exec check deferred to run time so setup planning never
				// blocks on podman; installContainerBun is the no-op fast path.
				if bunInstalledInContainer(bunPHPVersion) {
					return nil
				}
				if err := installContainerBun(bunPHPVersion, "", os.Stdout); err != nil {
					fmt.Printf("  [WARN] could not install bun in the container: %v\n", err)
				}
				return nil
			},
		},
	}...)

	// Offer in-container Pest browser testing when the project depends on the
	// Playwright-based browser plugin and isn't set up yet. Left unchecked by
	// default: selecting it triggers a multi-minute image rebuild + browser
	// download, too heavy to run on a blind Enter. Runs after the JS install step
	// so playwright is in node_modules. Non-fatal: installPestBrowser fails fast
	// (before any rebuild) when playwright is missing, surfaced here as a warning.
	if hasComposerJSON && bunPHPVersion != "" &&
		pestBrowserSupportedVersion(bunPHPVersion) == nil &&
		config.ComposerHasPackage(cwd, "pestphp/pest-plugin-browser") {
		alreadyBaked := false
		if gcfg, err := config.LoadGlobal(); err == nil {
			alreadyBaked = slices.Contains(gcfg.GetPackages(bunPHPVersion), pestBrowserPkg)
		}
		if !alreadyBaked {
			steps = append(steps, setupStep{
				label:   "pest:browser (container)",
				enabled: false,
				run: func() error {
					if err := installPestBrowser(bunPHPVersion, os.Stdout); err != nil {
						fmt.Printf("  [WARN] could not set up Pest browser testing: %v\n", err)
					}
					return nil
				},
			})
		}
	}

	// Framework setup commands (one-off bootstrap steps like migrations, storage:link, etc.)
	if site != nil {
		fwName := site.Framework
		if fwName == "" {
			fwName, _ = config.DetectFrameworkForDir(cwd)
		}
		if fw, ok := config.GetFrameworkForDir(fwName, cwd); ok {
			for _, sc := range fw.Setup {
				// Skip commands whose check doesn't pass.
				if sc.Check != nil && !config.MatchesRule(cwd, *sc.Check) {
					continue
				}
				setupCmd := sc
				enabled := setupCmd.Default
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

	// Orphaned workers — running units with no framework definition.
	// Shown before framework workers so they are stopped first.
	if site != nil {
		known := make(map[string]bool)
		if fw, ok := config.GetFrameworkForDir(site.Framework, cwd); ok {
			for wn := range fw.Workers {
				known[wn] = true
			}
		}
		orphans := lerdSystemd.FindOrphanedWorkers(site.Name, known)
		for _, oName := range orphans {
			on := oName
			steps = append(steps, setupStep{
				label:   on + ":stop (orphaned)",
				enabled: true,
				run: func() error {
					return WorkerStopForSite(site.Name, site.Path, on)
				},
			})
		}
	}

	// Framework workers — driven entirely by the framework definition.
	// Workers with ConflictsWith suppress the conflicted worker from the list
	// (e.g. horizon replaces queue when horizon's check passes).
	if site != nil {
		fwName := site.Framework
		if fwName == "" {
			fwName, _ = config.DetectFrameworkForDir(cwd)
		}
		if fw, ok := config.GetFrameworkForDir(fwName, cwd); ok && fw.Workers != nil {
			suppressed := map[string]bool{}
			for _, wDef := range fw.Workers {
				if wDef.Check != nil && !config.MatchesRule(cwd, *wDef.Check) {
					continue
				}
				for _, c := range wDef.ConflictsWith {
					suppressed[c] = true
				}
			}

			for wName, wDef := range fw.Workers {
				if wDef.Check != nil && !config.MatchesRule(cwd, *wDef.Check) {
					continue
				}
				if suppressed[wName] {
					continue
				}
				wn := wName
				wd := wDef
				ownerSite := site
				steps = append(steps, setupStep{
					label:   wn + ":start",
					enabled: savedWorkers[wn],
					run: func() error {
						phpVersion := ownerSite.PHPVersion
						if phpVersion == "" {
							if detected, detErr := phpDet.DetectVersion(cwd); detErr == nil {
								phpVersion = detected
							} else {
								cfg, _ := config.LoadGlobal()
								phpVersion = cfg.PHP.DefaultVersion
							}
						}
						for _, conflict := range wd.ConflictsWith {
							WorkerStopForSite(ownerSite.Name, cwd, conflict) //nolint:errcheck
						}
						return WorkerStartForSite(ownerSite.Name, cwd, phpVersion, wn, wd, true)
					},
				})
			}
		}
	}

	// Stripe listener (not a framework worker, still special-cased).
	if site != nil && siteHasStripeSecret(cwd) {
		ownerSite := site
		steps = append(steps, setupStep{
			label:   "stripe:listen",
			enabled: true,
			run: func() error {
				base := siteURL(cwd)
				if base == "" {
					return fmt.Errorf("could not resolve site URL, run 'lerd link' first")
				}
				return StripeStartForSite(ownerSite.Name, cwd, base)
			},
		})
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

// siteHasStripeSecret returns true if a Stripe secret is present for the
// project. The live .env is resolved through config.StripeSecretSet so a pinned
// secret_env_key is honoured (matching what StripeStartForSite will read);
// .env.example is probed against the candidate keys as a scaffold fallback.
func siteHasStripeSecret(cwd string) bool {
	if config.StripeSecretSet(cwd) {
		return true
	}
	for _, key := range config.StripeSecretEnvCandidates {
		if envfile.ReadKey(filepath.Join(cwd, ".env.example"), key) != "" {
			return true
		}
	}
	return false
}

// execInContainer runs an arbitrary command string inside the site's PHP-FPM container.
// composerInContainer runs composer inside the project's PHP-FPM container,
// ensuring the correct PHP version is used for dependency resolution.
func composerInContainer(dir string, args ...string) error {
	version, err := phpDet.DetectVersion(dir)
	if err != nil {
		cfg, _ := config.LoadGlobal()
		version = cfg.PHP.DefaultVersion
	}
	container := fpmContainerForDir(dir, version)

	podman.EnsurePathMounted(dir, version)

	home := os.Getenv("HOME")
	composerPhar := filepath.Join(config.BinDir(), "composer.phar")

	cmdArgs := []string{"exec", "-i", "-w", dir,
		"--env", "HOME=" + home,
		"--env", "COMPOSER_HOME=" + filepath.Join(home, ".config", "composer"),
		container, "php", composerPhar,
	}
	cmdArgs = append(cmdArgs, args...)

	cmd := podman.Cmd(cmdArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func execInContainer(dir, command string) error {
	version, err := phpDet.DetectVersion(dir)
	if err != nil {
		cfg, _ := config.LoadGlobal()
		version = cfg.PHP.DefaultVersion
	}
	container := fpmContainerForDir(dir, version)
	podman.EnsurePathMounted(dir, version)
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return fmt.Errorf("empty setup command")
	}
	cmdArgs := append([]string{"exec", "-i", "-w", dir, container}, parts...)
	cmd := podman.Cmd(cmdArgs...)
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
