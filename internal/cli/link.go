package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/certs"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/feedback"
	gitpkg "github.com/geodro/lerd/internal/git"
	"github.com/geodro/lerd/internal/nginx"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/siteops"
	"github.com/geodro/lerd/internal/store"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// linkSkipSetupPrompt suppresses the post-link "Run lerd setup?" suggestion
// when runLink is called from within lerd setup / lerd init (prevents infinite
// recursion and a redundant nag while setup is already running).
var linkSkipSetupPrompt bool

// linkSkipSummary defers runLink's success line and details block to the
// caller. Set by the init/setup flow so the ".env" step prints before the
// "linked" summary rather than after it.
var linkSkipSummary bool

// linkSkipDataImport suppresses the Sail data-import offer. It is kept separate
// from linkSkipSetupPrompt so routing `lerd link` through the init wizard
// (which suppresses the setup prompt) still offers to import an existing Sail
// database. Only the unattended (--all/CI) path sets it.
var linkSkipDataImport bool

// linkApplied and envApplied track whether the current process has already
// linked the site and applied its .env. When a link flows straight into setup
// (the "Run lerd setup?" prompt), this lets the setup pass skip re-printing the
// same provisioning steps and jump to the step selector.
var (
	linkApplied bool
	envApplied  bool
)

// linkAssumeYes approves a host-proxy dev command without the interactive
// confirmation prompt. Set by `lerd link --yes` and by the UI link flow, where
// the user's explicit action is the consent.
var linkAssumeYes bool

// presetVersionSuffix returns " (5.7)" for a non-empty version, otherwise "".
func presetVersionSuffix(version string) string {
	if version == "" {
		return ""
	}
	return " (" + version + ")"
}

// NewLinkCmd returns the link command.
func NewLinkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "link [domain]",
		Short: "Link the current directory as a site",
		Long:  "Register the current directory as a lerd site. The optional argument is the domain name without the TLD (e.g. 'myapp' becomes myapp.test). Defaults to the directory name.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLinkOrInit(args)
		},
	}
	cmd.Flags().BoolVar(&linkAssumeYes, "yes", false, "Approve a host-proxy dev command without the confirmation prompt")
	return cmd
}

// runLinkOrInit routes a user-typed `lerd link` into the init wizard when the
// project has no usable .lerd.yaml and we have an interactive terminal, so a
// fresh link guides the user through configuration (the wizard then links and
// offers setup) instead of leaving a bare, unconfigured registration. Every
// other case — a configured .lerd.yaml, a non-interactive shell (park, CI,
// scripts), or an explicit domain argument — falls through to a direct link.
// Internal runLink callers bypass this routing entirely.
func runLinkOrInit(args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	// IsEmpty (not file existence) so a present-but-empty .lerd.yaml still routes
	// to the wizard, matching what runLink's old empty-project branch did.
	proj, _ := config.LoadProjectConfig(cwd)
	hasConfig := proj != nil && !proj.IsEmpty()
	_, _, isWorktree := findOwningWorktree(cwd)
	if linkShouldRunWizard(hasConfig, isInteractive(), len(args) > 0, isWorktree) {
		return runInit(false)
	}
	return runLink(args)
}

// linkShouldRunWizard reports whether a user-invoked `lerd link` should run the
// init wizard rather than a bare link. Only true for a fresh, interactive,
// argument-free link on a real site directory: a missing .lerd.yaml means
// nothing is committed yet, the terminal can host the wizard, no explicit
// domain was requested, and the directory isn't a worktree (which inherits its
// parent's registration). Any false input keeps the fast, scriptable link.
func linkShouldRunWizard(hasConfig, interactive, hasDomainArg, isWorktree bool) bool {
	return !hasConfig && interactive && !hasDomainArg && !isWorktree
}

// linkShouldImportSail reports whether runLink should offer to import an
// existing Sail project's data. Gated on a live terminal and a dedicated
// suppression flag (NOT linkSkipSetupPrompt), so a fresh `lerd link` that
// routes through the init wizard still offers the import.
func linkShouldImportSail(interactive, skipImport, hasSail bool) bool {
	return interactive && !skipImport && hasSail
}

func runLink(args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	if parent, branch, ok := findOwningWorktree(cwd); ok {
		fmt.Printf("This directory is the %q worktree of site %q.\n", branch, parent.Name)
		fmt.Printf("Worktrees inherit the parent's registration; not linking %s as a separate site.\n", cwd)
		fmt.Printf("Manage it from the parent (%s) or via `lerd worktree`.\n", parent.Path)
		return nil
	}

	start := time.Now()
	// A standalone link opens the feedback block with a blank line for breathing
	// room; when embedded in init/setup the caller already printed one.
	if !linkSkipSetupPrompt {
		feedback.Begin()
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

	// Custom container path: the project defines its own Containerfile and
	// nginx reverse-proxies to it. Skip PHP/framework detection entirely.
	if proj != nil && proj.Container != nil && proj.Container.Port > 0 {
		secured := siteops.ResolveSecured(siteops.CleanupRelink(cwd, name), proj, cfg)
		site := config.Site{
			Name:          name,
			Domains:       domains,
			Path:          cwd,
			Secured:       secured,
			ContainerPort: proj.Container.Port,
			ContainerSSL:  proj.Container.SSL,
		}
		if err := config.AddSite(site); err != nil {
			return fmt.Errorf("registering site: %w", err)
		}
		// A site that used to be FrankenPHP or custom-FPM and now reverse-proxies a
		// container leaves its old per-site PHP quadlet auto-starting; clear it.
		reconcileStaleRuntimeQuadlets(site)
		_ = config.SyncProjectDomains(cwd, site.Domains, cfg.DNS.TLD)
		if err := siteops.FinishCustomLink(site, proj.Container); err != nil {
			return err
		}
		printLinkSummary(site, start)
		return linkApplyServices(cwd, proj)
	}

	// Host-proxy path: the project runs a dev server on the host and nginx
	// reverse-proxies to it. No container, no PHP/framework detection.
	if proj != nil && proj.Proxy != nil && proj.Proxy.Port > 0 {
		// Gate supervising a dev command on the host behind explicit consent: a
		// re-link with the same approved command, --yes, or the wizard's own
		// choice passes silently; a fresh repo-authored command prompts.
		approved := linkAssumeYes
		if existing, err := config.FindSite(name); err == nil && existing.HostCommand == proj.Proxy.Command {
			approved = true
		}
		if err := approveHostProxyCommand(name, proj.Proxy.Command, approved); err != nil {
			return err
		}
		secured := siteops.ResolveSecured(siteops.CleanupRelink(cwd, name), proj, cfg)
		site := config.Site{
			Name:        name,
			Domains:     domains,
			Path:        cwd,
			Secured:     secured,
			HostPort:    proj.Proxy.Port,
			HostSSL:     proj.Proxy.SSL,
			HostCommand: proj.Proxy.Command,
		}
		if err := config.AddSite(site); err != nil {
			return fmt.Errorf("registering site: %w", err)
		}
		// A site that used to be FrankenPHP or custom-FPM and now proxies a host
		// dev server leaves its old per-site PHP quadlet auto-starting; clear it.
		reconcileStaleRuntimeQuadlets(site)
		_ = config.SyncProjectDomains(cwd, site.Domains, cfg.DNS.TLD)
		if err := siteops.FinishHostProxyLink(site); err != nil {
			return err
		}
		startHostProxyWorker(site, proj.Proxy)
		printLinkSummary(site, start)
		return linkApplyServices(cwd, proj)
	}

	framework, ok := resolveFramework(cwd)
	detectedPublicDir := ""
	if proj != nil && proj.PublicDir != "" {
		detectedPublicDir = proj.PublicDir
	} else if !ok {
		detectedPublicDir = config.DetectPublicDir(cwd)
	}

	frameworkLabel := framework
	if frameworkLabel == "" {
		frameworkLabel = "unknown (public: " + detectedPublicDir + ")"
	}
	fwStep := feedback.Start("detecting framework")
	if framework != "" {
		fwStep.OK(frameworkLabel)
	} else {
		fwStep.Info(frameworkLabel)
	}

	versions := siteops.DetectSiteVersions(cwd, framework, cfg.PHP.DefaultVersion, cfg.Node.DefaultVersion)
	phpVersion, nodeVersion := versions.PHP, versions.Node
	if proj != nil && proj.PHPVersion != "" {
		phpVersion = phpDet.ClampToRange(proj.PHPVersion, versions.PHPMin, versions.PHPMax)
	}
	if versions.PHPMin != "" || versions.PHPMax != "" {
		unclamped, _ := phpDet.DetectVersion(cwd)
		if unclamped != phpVersion {
			if versions.SuggestedPHP != "" {
				q := fmt.Sprintf("Using PHP %s (best installed in range %s–%s). Install PHP %s?",
					phpVersion, versions.PHPMin, versions.PHPMax, versions.SuggestedPHP)
				if feedback.Confirm(q, true) {
					inst := feedback.Start("installing PHP " + versions.SuggestedPHP)
					if err := ensureFPMQuadlet(versions.SuggestedPHP); err != nil {
						inst.Fail(err)
					} else {
						inst.OK("")
						phpVersion = versions.SuggestedPHP
					}
				}
			} else {
				fmt.Printf("Using PHP %s (%s supports %s–%s).\n",
					phpVersion, versions.FrameworkLabel, versions.PHPMin, versions.PHPMax)
			}
		}
	}

	secured := siteops.ResolveSecured(siteops.CleanupRelink(cwd, name), proj, cfg)

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

	// Honour .lerd.yaml runtime selection (e.g. "frankenphp") so a re-link
	// rehydrates the site with the committed runtime rather than resetting
	// to FPM.
	if proj != nil && proj.Runtime != "" {
		site.Runtime = proj.Runtime
		site.RuntimeWorker = proj.RuntimeWorker
		// FrankenPHP only publishes images for PHP >= 8.2; without this guard the
		// build normalizes the version up (e.g. 8.1 -> 8.5) and silently runs a
		// different PHP than the site reports. Mirror the `lerd runtime` guard and
		// fall back to FPM rather than upgrading PHP behind the user's back.
		if site.IsFrankenPHP() && !config.IsFrankenPHPVersion(site.PHPVersion) {
			fmt.Printf("  FrankenPHP has no PHP %s image; linking as FPM instead\n", site.PHPVersion)
			site.Runtime = ""
			site.RuntimeWorker = false
		}
	}
	// A container: config with no port on a PHP project means the site is served
	// by fastcgi from its own image, built from the project's Containerfile.
	if proj != nil && proj.Container != nil && proj.Container.Port == 0 {
		site.Runtime = "fpm-custom"
		// The PHP version is fixed by the Containerfile's FROM lerd-php<ver>-fpm line,
		// not project detection; honour it so the reported version and the per-version
		// ini mounts match the image the container actually runs.
		if v := podman.CustomFPMBaseVersion(cwd, proj.Container); v != "" {
			site.PHPVersion = v
			phpVersion = v
		}
	}

	if err := config.AddSite(site); err != nil {
		return fmt.Errorf("registering site: %w", err)
	}

	// A re-link of a site that dropped its frankenphp or custom-FPM runtime
	// leaves the old per-site quadlet behind; reconcile it to the site's real type.
	reconcileStaleRuntimeQuadlets(site)

	_ = config.SyncProjectDomains(cwd, site.Domains, cfg.DNS.TLD)

	if site.IsCustomFPM() {
		result := "php " + feedback.Val(phpVersion) + " · nginx vhost written"
		if err := provisionAndSecure("building custom FPM image", result, site,
			func(s config.Site) error { return siteops.FinishCustomFPMLink(s, proj.Container) }); err != nil {
			return err
		}
		printLinkSummary(site, start)
		return linkApplyServices(cwd, proj)
	}

	if site.IsFrankenPHP() {
		result := "php " + feedback.Val(phpVersion) + " · node " + feedback.Val(nodeVersion) + " · nginx vhost written"
		if err := provisionAndSecure("provisioning FrankenPHP runtime", result, site,
			func(s config.Site) error { return siteops.FinishFrankenPHPLink(s) }); err != nil {
			return err
		}
		printLinkSummary(site, start)
		return linkApplyServices(cwd, proj)
	}

	result := "php " + feedback.Val(phpVersion) + " · node " + feedback.Val(nodeVersion) + " · nginx vhost written"
	if err := provisionAndSecure("provisioning PHP-FPM runtime", result, site,
		func(s config.Site) error { return siteops.FinishLink(s, phpVersion) }); err != nil {
		return err
	}
	printLinkSummary(site, start)

	// Sail detection — offer to import data before setup so lerd's DB is
	// populated from the existing Sail environment.
	if linkShouldImportSail(isInteractive(), linkSkipDataImport, config.ComposerHasPackage(cwd, "laravel/sail")) {
		sailDBName := sailLinkDetectDBName(cwd)
		if feedback.Confirm("This project uses Laravel Sail. Import database (and S3 files) from Sail into lerd?", false) {
			if err := runImportSail(false, false, "sail", "password", sailDBName, sailDBName != "", false, false); err != nil {
				feedback.Warn("sail import: %v", err)
			}
		}
	}

	if hint, suggest := linkNextStep(linkSkipSetupPrompt); suggest {
		if isInteractive() {
			if feedback.Confirm("Run lerd setup?", true) {
				if err := runSetup(false, false); err != nil {
					feedback.Warn("setup: %v", err)
				}
			}
		} else {
			fmt.Println(hint)
		}
	}

	// Apply remaining .lerd.yaml settings: HTTPS and services. secured already
	// folds in the DNS-managed gate, so a secured: true project on a localhost
	// install lands here with secured=false and is left on http rather than
	// triggering a runSecure that the cert layer would only reject.
	if proj != nil {
		if proj.Secured && !secured && cfg.DNSManaged() {
			if err := runSecure(nil, []string{}); err != nil {
				feedback.Warn("securing site: %v", err)
			}
		} else if !proj.Secured && secured {
			if err := runUnsecure(nil, []string{}); err != nil {
				feedback.Warn("disabling HTTPS: %v", err)
			}
		}

		if err := linkApplyServices(cwd, proj); err != nil {
			return err
		}
	}

	return nil
}

// provisionAndSecure runs a PHP runtime's link finisher as plain HTTP under a
// "provisioning" step, then, when the site is secured, issues its certificate
// under a separate "generating certificate" step. Splitting the secure work out
// of the finisher keeps mkcert's work on its own clean line instead of buried
// inside the runtime step.
func provisionAndSecure(label, result string, site config.Site, finish func(config.Site) error) error {
	unsecured := site
	unsecured.Secured = false
	prov := feedback.Start(label)
	if err := finish(unsecured); err != nil {
		prov.Fail(err)
		return err
	}
	prov.OK(result)

	if site.Secured {
		cert := feedback.Start("generating certificate")
		if err := certs.SecureSite(site); err != nil {
			cert.Fail(err)
			return err
		}
		// SecureSite swaps the vhost to HTTPS on disk but doesn't reload nginx;
		// the finisher above only reloaded it for the plain-HTTP config, so
		// without this the site keeps serving the now-deleted HTTP vhost from
		// nginx's memory and HTTPS never comes up until an unrelated reload.
		if err := nginx.Reload(); err != nil {
			cert.Fail(err)
			return err
		}
		cert.OK("trusted")
	}
	return nil
}

// printLinkSummary prints the green success line and an aligned details block,
// deriving every field from the registered site so callers don't repeat them.
func printLinkSummary(site config.Site, start time.Time) {
	// Reached on every successful link path, so this is where we record that the
	// site is linked for this process (even when the summary itself is deferred).
	linkApplied = true
	if linkSkipSummary {
		return
	}
	feedback.Success("linked", time.Since(start))

	scheme := "http"
	if site.Secured {
		scheme = "https"
	}
	sum := feedback.NewSummary().Row("Site", feedback.Val(scheme+"://"+site.PrimaryDomain()))
	switch {
	case site.HostPort > 0:
		sum.Row("Serving", fmt.Sprintf("host proxy · port %d", site.HostPort))
	case site.ContainerPort > 0:
		sum.Row("Serving", fmt.Sprintf("custom container · port %d", site.ContainerPort))
	case site.PHPVersion != "":
		sum.Row("PHP", feedback.Val(site.PHPVersion)+" · "+linkRuntimeLabel(site))
	}
	if site.NodeVersion != "" {
		sum.Row("Node", feedback.Val(site.NodeVersion))
	}
	if site.Framework != "" {
		sum.Row("Framework", site.Framework)
	}
	if env := sailReadRawEnv(site.Path); env["DB_CONNECTION"] != "" {
		db := env["DB_CONNECTION"]
		if cache := env["CACHE_STORE"]; cache != "" {
			db += " · cache " + cache
		}
		sum.Row("DB", db)
	}
	sum.Print()
}

// linkRuntimeLabel names the serving runtime for the link summary's PHP row.
func linkRuntimeLabel(site config.Site) string {
	switch {
	case site.IsFrankenPHP():
		return "FrankenPHP"
	case site.IsCustomFPM():
		return "custom FPM"
	default:
		return "FPM"
	}
}

// linkApplyServices installs and starts services declared in .lerd.yaml.
// Shared by both the standard PHP link path and the custom container path.
// approveInlineService surfaces a brand-new inline service defined in a
// project's .lerd.yaml and confirms it before lerd installs and runs it as a
// container, since the image and command come from the (possibly cloned) repo.
// A scripted or UI link (--yes) and a non-interactive run proceed; an
// interactive run prompts.
func approveInlineService(svc *config.CustomService) bool {
	if linkAssumeYes || !isInteractive() {
		return true
	}
	fmt.Printf("\nThis project defines a service lerd will run as a container:\n")
	fmt.Printf("  name:  %s\n", svc.Name)
	fmt.Printf("  image: %s\n", svc.Image)
	if svc.Exec != "" {
		fmt.Printf("  exec:  %s\n", svc.Exec)
	}
	if len(svc.Ports) > 0 {
		fmt.Printf("  ports: %s\n", strings.Join(svc.Ports, ", "))
	}
	return promptConfirm("Install and start it?")
}

func linkApplyServices(cwd string, proj *config.ProjectConfig) error {
	if proj == nil {
		return nil
	}
	for _, svc := range proj.Services {
		if svc.Preset != "" {
			if _, err := config.LoadCustomService(svc.Name); err != nil {
				fmt.Printf("  Installing preset %s%s\n", svc.Preset, presetVersionSuffix(svc.PresetVersion))
				if _, err := InstallPresetByName(svc.Preset, svc.PresetVersion); err != nil {
					feedback.Warn("installing preset %s: %v", svc.Preset, err)
					continue
				}
			}
		} else if svc.Custom != nil {
			svc.Custom.Name = svc.Name
			existing, loadErr := config.LoadCustomService(svc.Name)
			shouldSave := true
			if loadErr != nil {
				// Brand-new inline service from the project's .lerd.yaml: its
				// image and command come from the (possibly cloned) repo, so
				// show what it will run and confirm before installing it.
				if !approveInlineService(svc.Custom) {
					fmt.Printf("  Skipped service %s\n", svc.Name)
					continue
				}
			}
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
					feedback.Warn("registering service %s: %v", svc.Name, err)
					continue
				}
			}
		}
		if err := ensureServiceRunning(svc.Name); err != nil {
			feedback.Warn("service %s: %v", svc.Name, err)
		}
	}
	return nil
}

// sailLinkDetectDBName reads DB_DATABASE from the project's .env so the link
// prompt can pass the correct Sail database name directly to runImportSail.
// Returns "" when .env is absent or DB_DATABASE is not set.
func sailLinkDetectDBName(cwd string) string {
	env := sailReadRawEnv(cwd)
	return env["DB_DATABASE"]
}

// startWorkersForSite starts the named workers for a site.
// Workers with a Check rule that doesn't pass are skipped. Workers that conflict
// with another requested worker are resolved via ConflictsWith (e.g. horizon replaces queue).
func startWorkersForSite(site *config.Site, workers []string, phpVersion string) {
	// Stripe is not a framework worker, so the framework loop below skips it.
	// Start its listener directly when it was among the workers being restored
	// (e.g. recreated after a runtime switch), before the no-framework early
	// return so it survives even on a site lerd has no framework definition for.
	for _, w := range workers {
		if w == "stripe" {
			scheme := "http"
			if site.Secured {
				scheme = "https"
			}
			baseURL := scheme + "://" + site.PrimaryDomain()
			if err := StripeStartForSite(site.Name, site.Path, baseURL); err != nil {
				feedback.Warn("starting stripe listener: %v", err)
			}
			break
		}
	}

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
			WorkerStopForSite(site.Name, site.Path, conflict) //nolint:errcheck
		}
		if err := WorkerStartForSite(site.Name, site.Path, phpVersion, w, worker, true); err != nil {
			feedback.Warn("starting worker %s: %v", w, err)
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

// linkNextStep returns the guidance shown after a standalone lerd link, and
// whether to show it at all. It always points at lerd setup: setup takes a
// freshly linked project the rest of the way (deps, migrations, workers, HTTPS)
// and runs the init wizard itself when there is no .lerd.yaml, so suggesting
// init separately is redundant friction. Returns suggest=false when link runs
// inside setup/init (skipPrompt), so the guidance never nags mid-setup.
func linkNextStep(skipPrompt bool) (hint string, suggest bool) {
	if skipPrompt {
		return "", false
	}
	return "\nRun 'lerd setup' to install dependencies, run migrations, and start workers.", true
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

// findOwningWorktree returns the parent site if cwd is one of its worktree
// checkouts. Used to short-circuit runLink so worktrees don't get registered
// as standalone sites.
func findOwningWorktree(cwd string) (*config.Site, string, bool) {
	reg, err := config.LoadSites()
	if err != nil {
		return nil, "", false
	}
	for i := range reg.Sites {
		s := &reg.Sites[i]
		if s.Ignored || s.Path == cwd {
			continue
		}
		wts, _ := gitpkg.DetectWorktrees(s.Path, s.PrimaryDomain())
		for _, wt := range wts {
			if wt.Path == cwd {
				return s, wt.Branch, true
			}
		}
	}
	return nil, "", false
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
