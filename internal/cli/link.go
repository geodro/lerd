package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/certs"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/nginx"
	nodeDet "github.com/geodro/lerd/internal/node"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// linkSkipSetupPrompt suppresses the "Run lerd setup?" prompt when runLink
// is called from within lerd setup / lerd init (prevents infinite recursion).
var linkSkipSetupPrompt bool

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
	if proj != nil && proj.Framework != "" && proj.FrameworkDef != nil {
		proj.FrameworkDef.Name = proj.Framework
		existing, exists := config.GetFramework(proj.Framework)
		if !exists {
			// Does not exist locally — save without prompting.
			_ = config.SaveFramework(proj.FrameworkDef)
		} else {
			action, err := confirmReplace("framework", proj.Framework, existing, proj.FrameworkDef)
			if err != nil {
				return err
			}
			switch action {
			case replaceFromProject:
				_ = config.SaveFramework(proj.FrameworkDef)
			case replaceFromDisk:
				proj.FrameworkDef = existing
				_ = config.SaveProjectConfig(cwd, proj)
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

	baseName, _ := siteNameAndDomain(rawName, cfg.DNS.TLD)
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

	// Validate that none of the domains are already used by another site.
	for _, d := range domains {
		if isReservedDomain(d) {
			return fmt.Errorf("domain %q is reserved for internal Lerd use", d)
		}
		if existing, err := config.IsDomainUsed(d); err == nil && existing != nil {
			// Allow re-linking the same site (same path).
			if existing.Path != cwd {
				return fmt.Errorf("domain %q is already used by site %q", d, existing.Name)
			}
		}
	}

	phpVersion, err := phpDet.DetectVersion(cwd)
	if err != nil {
		phpVersion = cfg.PHP.DefaultVersion
	}
	if proj != nil && proj.PHPVersion != "" {
		phpVersion = proj.PHPVersion
	}

	nodeVersion, err := nodeDet.DetectVersion(cwd)
	if err != nil {
		nodeVersion = cfg.Node.DefaultVersion
	}

	framework, ok := resolveFramework(cwd)
	detectedPublicDir := ""
	if !ok {
		detectedPublicDir = config.DetectPublicDir(cwd)
	}

	// Check if this path already has registered sites (re-link scenario).
	// Carry over secured state and clean up any old registrations at this path.
	secured := false
	if reg, err := config.LoadSites(); err == nil {
		for _, existing := range reg.Sites {
			if existing.Path != cwd {
				continue
			}
			secured = secured || existing.Secured
			if existing.Name != name {
				_ = nginx.RemoveVhost(existing.PrimaryDomain())
				_ = config.RemoveSite(existing.Name)
			}
		}
	}

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

	// Write domains to .lerd.yaml (creates or updates).
	{
		proj, _ := config.LoadProjectConfig(cwd)
		suffix := "." + cfg.DNS.TLD
		var names []string
		for _, d := range site.Domains {
			names = append(names, strings.TrimSuffix(d, suffix))
		}
		proj.Domains = names
		if err := config.SaveProjectConfig(cwd, proj); err != nil {
			fmt.Printf("[WARN] writing .lerd.yaml: %v\n", err)
		}
	}

	if secured {
		// Reissue cert for the (possibly new) domain and regenerate SSL vhost.
		if err := certs.SecureSite(site); err != nil {
			return fmt.Errorf("securing site: %w", err)
		}
	} else {
		if err := nginx.GenerateVhost(site, phpVersion); err != nil {
			return fmt.Errorf("generating vhost: %w", err)
		}
	}

	if err := ensureFPMQuadlet(phpVersion); err != nil {
		fmt.Printf("[WARN] FPM quadlet for PHP %s: %v\n", phpVersion, err)
	}

	if err := podman.WriteContainerHosts(); err != nil {
		fmt.Printf("[WARN] updating container hosts file: %v\n", err)
	}

	frameworkLabel := framework
	if frameworkLabel == "" {
		frameworkLabel = "unknown (public: " + detectedPublicDir + ")"
	}
	fmt.Printf("Linked: %s -> %s (PHP %s, Node %s, Framework: %s)\n", name, strings.Join(domains, ", "), phpVersion, nodeVersion, frameworkLabel)

	if err := nginx.Reload(); err != nil {
		fmt.Printf("[WARN] nginx reload: %v\n", err)
	}

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
			if svc.Custom != nil {
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
// It applies the same detection as the init wizard: if horizon is installed,
// "queue" is upgraded to "horizon"; if reverb is not configured, it is skipped.
func startWorkersForSite(site *config.Site, workers []string, phpVersion string) {
	hasHorizon := SiteHasHorizon(site.Path)
	hasReverb := SiteUsesReverb(site.Path)
	fw, hasFw := config.GetFramework(site.Framework)

	for _, w := range workers {
		// If horizon is installed, start horizon instead of queue.
		if w == "queue" && hasHorizon {
			w = "horizon"
		}
		// Skip reverb if not configured in the project.
		if w == "reverb" && !hasReverb {
			continue
		}

		var err error
		switch w {
		case "queue":
			err = QueueStartForSite(site.Name, site.Path, phpVersion)
		case "schedule":
			err = ScheduleStartForSite(site.Name, site.Path, phpVersion)
		case "reverb":
			err = ReverbStartForSite(site.Name, site.Path, phpVersion)
		case "horizon":
			err = HorizonStartForSite(site.Name, site.Path, phpVersion)
		default:
			if hasFw && fw.Workers != nil {
				if worker, ok := fw.Workers[w]; ok {
					err = WorkerStartForSite(site.Name, site.Path, phpVersion, w, worker)
				}
			}
		}
		if err != nil {
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
	if proj, err := config.LoadProjectConfig(dir); err == nil && proj.Framework != "" {
		if _, ok := config.GetFramework(proj.Framework); ok {
			return proj.Framework, true
		}
		// Framework definition not found locally — restore from the embedded def
		// in .lerd.yaml if present (enables portability across machines).
		if proj.FrameworkDef != nil {
			proj.FrameworkDef.Name = proj.Framework
			if saveErr := config.SaveFramework(proj.FrameworkDef); saveErr == nil {
				return proj.Framework, true
			}
		}
		return "", false
	}
	return config.DetectFramework(dir)
}
