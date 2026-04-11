package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/siteops"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/store"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// linkSkipSetupPrompt suppresses the "Run lerd setup?" prompt when runLink
// is called from within lerd setup / lerd init (prevents infinite recursion).
var linkSkipSetupPrompt bool

// presetVersionSuffix returns " (5.7)" for a non-empty version, otherwise "".
func presetVersionSuffix(version string) string {
	if version == "" {
		return ""
	}
	return " (" + version + ")"
}

// NewLinkCmd returns the link command.
func NewLinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "link [domain]",
		Short: "Link the current directory as a site",
		Long:  "Register the current directory as a lerd site. The optional argument is the domain name without the TLD (e.g. 'myapp' becomes myapp.test). Defaults to the directory name.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLink(args)
		},
	}
}

func runLink(args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}

	// Load .lerd.yaml early so its values can influence the link.
	proj, _ := config.LoadProjectConfig(cwd)

	// Restore embedded custom framework definition before resolveFramework runs.
	// The embedded def in .lerd.yaml is the project's known-good configuration.
	// Compare against whichever definition is currently active (user-defined or store-installed).
	if proj != nil && proj.Framework != "" && proj.FrameworkDef != nil {
		proj.FrameworkDef.Name = proj.Framework
		existing, exists := config.GetFrameworkForDir(proj.Framework, cwd)
		if !exists {
			// No definition anywhere — save the embedded one to the store dir.
			_ = config.SaveStoreFramework(proj.FrameworkDef)
		} else {
			action, err := confirmReplace("framework", proj.Framework, existing, proj.FrameworkDef)
			if err != nil {
				return err
			}
			switch action {
			case replaceFromProject:
				// User chose the .lerd.yaml version — save to store dir.
				_ = config.SaveStoreFramework(proj.FrameworkDef)
			case replaceFromDisk:
				// User chose the local/store version — update .lerd.yaml.
				_ = config.SetProjectFrameworkDef(cwd, existing)
			}
		}
	}

	// Write .node-version from .lerd.yaml if the file is not already present.
	if proj != nil && proj.NodeVersion != "" {
		nodeVersionFile := filepath.Join(cwd, ".node-version")
		if _, statErr := os.Stat(nodeVersionFile); os.IsNotExist(statErr) {
			_ = os.WriteFile(nodeVersionFile, []byte(proj.NodeVersion+"\n"), 0644)
		}
	}

	rawName := filepath.Base(cwd)
	if len(args) > 0 {
		rawName = args[0]
	}

	baseName, _ := siteops.SiteNameAndDomain(rawName, cfg.DNS.TLD)
	name := freeSiteName(baseName, cwd)

	// Build the domains list.
	// 1. Start from .lerd.yaml domains if present, else auto-generate from name.
	// 2. If an explicit arg is given, ensure it is the primary (first) domain.
	var domains []string
	if proj != nil && len(proj.Domains) > 0 {
		for _, d := range proj.Domains {
			domains = append(domains, strings.ToLower(d)+"."+cfg.DNS.TLD)
		}
	} else {
		domains = []string{name + "." + cfg.DNS.TLD}
	}

	// If the user passed an explicit domain, make it the primary.
	if len(args) > 0 {
		explicit := strings.ToLower(args[0]) + "." + cfg.DNS.TLD
		// Remove it from the list if already present, then prepend.
		var filtered []string
		for _, d := range domains {
			if d != explicit {
				filtered = append(filtered, d)
			}
		}
		domains = append([]string{explicit}, filtered...)
	}

	// Filter out domains already owned by another site (and reserved domains).
	// The check is strict — a domain may only belong to one site regardless of
	// TLS scheme. We never touch .lerd.yaml on disk; the surviving in-memory
	// list is what gets registered. If everything was conflicted, fall back to
	// a freshly generated <baseName>.<tld>. Re-linking the same path is not a
	// conflict.
	kept, removed := resolveSiteDomains(domains, baseName, cwd, cfg.DNS.TLD)
	warnFilteredDomains(removed)
	domains = kept

	framework, ok := resolveFramework(cwd)
	detectedPublicDir := ""
	if !ok {
		detectedPublicDir = config.DetectPublicDir(cwd)
	}

	versions := siteops.DetectSiteVersions(cwd, framework, cfg.PHP.DefaultVersion, cfg.Node.DefaultVersion)
	phpVersion, nodeVersion := versions.PHP, versions.Node
	if proj != nil && proj.PHPVersion != "" {
		phpVersion = phpDet.ClampToRange(proj.PHPVersion, versions.PHPMin, versions.PHPMax)
	}
	if unclamped, _ := phpDet.DetectVersion(cwd); unclamped != phpVersion && (versions.PHPMin != "" || versions.PHPMax != "") {
		fmt.Printf("PHP %s is outside %s's supported range (%s–%s), using PHP %s.\n",
			unclamped, versions.FrameworkLabel, versions.PHPMin, versions.PHPMax, phpVersion)
	}
	if versions.SuggestedPHP != "" {
		fmt.Printf("PHP %s is recommended for %s. Install it? [Y/n] ", versions.SuggestedPHP, versions.FrameworkLabel)
		var answer string
		fmt.Scanln(&answer) //nolint:errcheck
		if answer == "" || answer[0] == 'Y' || answer[0] == 'y' {
			fmt.Printf("Installing PHP %s...\n", versions.SuggestedPHP)
			if err := ensureFPMQuadlet(versions.SuggestedPHP); err != nil {
				fmt.Printf("[WARN] installing PHP %s: %v\n", versions.SuggestedPHP, err)
			} else {
				phpVersion = versions.SuggestedPHP
				fmt.Printf("PHP %s installed, using it for this site.\n", versions.SuggestedPHP)
			}
		}
	}

	secured := siteops.CleanupRelink(cwd, name)

	site := config.Site{
		Name:        name,
		Domains:     domains,
		Path:        cwd,
		PHPVersion:  phpVersion,
		NodeVersion: nodeVersion,
		Secured:     secured,
		Framework:   framework,
		PublicDir:   detectedPublicDir,
	}

	if err := config.AddSite(site); err != nil {
		return fmt.Errorf("registering site: %w", err)
	}

	_ = config.SyncProjectDomains(cwd, site.Domains, cfg.DNS.TLD)

	if err := siteops.FinishLink(site, phpVersion); err != nil {
		return err
	}

	frameworkLabel := framework
	if frameworkLabel == "" {
		frameworkLabel = "unknown (public: " + detectedPublicDir + ")"
	}
	fmt.Printf("Linked: %s -> %s (PHP %s, Node %s, Framework: %s)\n", name, strings.Join(domains, ", "), phpVersion, nodeVersion, frameworkLabel)

	// Apply remaining .lerd.yaml settings: HTTPS and services.
	if proj != nil {
		if proj.Secured && !secured {
			if err := runSecure(nil, []string{}); err != nil {
				fmt.Printf("[WARN] securing site: %v\n", err)
			}
		} else if !proj.Secured && secured {
			if err := runUnsecure(nil, []string{}); err != nil {
				fmt.Printf("[WARN] disabling HTTPS: %v\n", err)
			}
		}

		for _, svc := range proj.Services {
			// Preset reference: install the bundled preset locally if the
			// teammate's machine does not have it yet, so the project is
			// portable across machines without inlining the definition.
			if svc.Preset != "" {
				if _, err := config.LoadCustomService(svc.Name); err != nil {
					fmt.Printf("  Installing preset %s%s\n", svc.Preset, presetVersionSuffix(svc.PresetVersion))
					if _, err := InstallPresetByName(svc.Preset, svc.PresetVersion); err != nil {
						fmt.Printf("[WARN] installing preset %s: %v\n", svc.Preset, err)
						continue
					}
				}
			} else if svc.Custom != nil {
				svc.Custom.Name = svc.Name
				existing, loadErr := config.LoadCustomService(svc.Name)
				shouldSave := true
				if loadErr == nil {
					action, err := confirmReplace("service", svc.Name, existing, svc.Custom)
					if err != nil {
						return err
					}
					switch action {
					case replaceFromProject:
						shouldSave = true
					case replaceFromDisk:
						svc.Custom = existing
						shouldSave = false
						// Update .lerd.yaml so the diff doesn't recur on next link.
						if p, _ := config.LoadProjectConfig(cwd); p != nil {
							for i, s := range p.Services {
								if s.Name == svc.Name {
									p.Services[i].Custom = existing
									_ = config.SaveProjectConfig(cwd, p)
									break
								}
							}
						}
					default:
						shouldSave = false
					}
				}
				if shouldSave {
					if err := config.SaveCustomService(svc.Custom); err != nil {
						fmt.Printf("[WARN] registering service %s: %v\n", svc.Name, err)
						continue
					}
				}
			}
			if err := ensureServiceRunning(svc.Name); err != nil {
				fmt.Printf("[WARN] service %s: %v\n", svc.Name, err)
			}
		}

		// Workers are configured — prompt for setup so the user can choose to
		// install dependencies, run migrations, and start workers in the right order.
		// Skip if workers are already running (re-link of an active site) or if
		// we're already inside a setup/init call (prevents infinite recursion).
		if len(proj.Workers) > 0 && !linkSkipSetupPrompt && !hasRunningWorkers(&site) {
			if isInteractive() {
				fmt.Printf("\n  Workers configured: %s\n", strings.Join(proj.Workers, ", "))
				fmt.Print("  Run lerd setup? [Y/n]: ")
				var answer string
				fmt.Scanln(&answer)
				answer = strings.TrimSpace(strings.ToLower(answer))
				if answer == "" || answer == "y" || answer == "yes" {
					if err := runSetup(false, false); err != nil {
						fmt.Printf("[WARN] setup: %v\n", err)
					}
				}
			} else {
				fmt.Printf("  Workers configured — run lerd setup to start them\n")
			}
		}
	}

	return nil
}

// startWorkersForSite starts the named workers for a site.
// Workers with a Check rule that doesn't pass are skipped. Workers that conflict
// with another requested worker are resolved via ConflictsWith (e.g. horizon replaces queue).
func startWorkersForSite(site *config.Site, workers []string, phpVersion string) {
	fw, hasFw := config.GetFrameworkForDir(site.Framework, site.Path)
	if !hasFw || fw.Workers == nil {
		return
	}

	// Build a set of requested workers, applying conflict resolution.
	// If a worker with ConflictsWith is requested AND its conflicts are also
	// requested, the conflicting workers are removed (e.g. horizon removes queue).
	requested := make(map[string]bool, len(workers))
	for _, w := range workers {
		requested[w] = true
	}

	// Check if any worker with conflicts should replace others.
	for _, w := range workers {
		wDef, ok := fw.Workers[w]
		if !ok {
			continue
		}
		// Skip workers whose check doesn't pass.
		if wDef.Check != nil && !config.MatchesRule(site.Path, *wDef.Check) {
			delete(requested, w)
			continue
		}
		for _, conflict := range wDef.ConflictsWith {
			delete(requested, conflict)
		}
	}

	for _, w := range workers {
		if !requested[w] {
			continue
		}
		worker, ok := fw.Workers[w]
		if !ok {
			continue
		}
		// Skip workers whose check doesn't pass.
		if worker.Check != nil && !config.MatchesRule(site.Path, *worker.Check) {
			continue
		}
		// Stop conflicting workers before starting.
		for _, conflict := range worker.ConflictsWith {
			WorkerStopForSite(site.Name, conflict) //nolint:errcheck
		}
		if err := WorkerStartForSite(site.Name, site.Path, phpVersion, w, worker); err != nil {
			fmt.Printf("[WARN] starting worker %s: %v\n", w, err)
		}
	}
}

// hasRunningWorkers returns true if any workers are currently active for the site.
func hasRunningWorkers(site *config.Site) bool {
	return len(collectRunningWorkers(site)) > 0
}

// isInteractive returns true if stdin is a terminal.
func isInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// resolveFramework returns the framework name for the project at dir.
// It reads the .lerd.yaml framework field first (explicit override), then
// auto-detects via config.DetectFramework. Returns ("", false) if no
// framework definition is found.
func resolveFramework(dir string) (string, bool) {
	if name, ok := config.DetectFrameworkForDir(dir); ok {
		return name, true
	}
	// Interactive store fallback — only for terminal commands.
	return store.DetectFrameworkWithStore(dir)
}

// fetchFrameworkFromStore attempts to install a framework definition from the
// store. Returns true if successful.
func fetchFrameworkFromStore(name, dir string) bool {
	client := store.NewClient()
	version := ""
	if idx, err := client.FetchIndex(); err == nil {
		for _, entry := range idx.Frameworks {
			if entry.Name == name {
				version = store.ResolveVersion(dir, entry.Detect, entry.Versions, "")
				break
			}
		}
	}
	fw, err := client.FetchFramework(name, version)
	if err != nil {
		return false
	}
	if err := config.SaveStoreFramework(fw); err != nil {
		return false
	}
	v := fw.Version
	if v == "" {
		v = "latest"
	}
	fmt.Printf("  Installed %s@%s from store\n", name, v)
	return true
}
