package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/nginx"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/siteops"
	"github.com/spf13/cobra"
)

// warnMissingExtensions checks composer.json for ext-* requirements and warns if any are
// not covered by the bundled image or the user's custom extension list.
func warnMissingExtensions(dir, name, phpVersion string, cfg *config.GlobalConfig) {
	detected := phpDet.DetectExtensions(dir)
	if len(detected) == 0 {
		return
	}
	bundled := podman.BundledExtensions()
	installed := cfg.GetExtensions(phpVersion)

	inSet := func(ext string, set []string) bool {
		for _, e := range set {
			if e == ext {
				return true
			}
		}
		return false
	}

	var missing []string
	for _, ext := range detected {
		if !inSet(ext, bundled) && !inSet(ext, installed) {
			missing = append(missing, ext)
		}
	}
	if len(missing) > 0 {
		fmt.Printf("  [!] %s requires PHP extensions not in the image: %s\n", name, strings.Join(missing, ", "))
		fmt.Printf("      Run: lerd php:ext add %s\n", strings.Join(missing, " "))
	}
}

// NewParkCmd returns the park command.
func NewParkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "park [directory]",
		Short: "Park a directory to serve all subdirectories as sites",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runPark,
	}
}

func runPark(_ *cobra.Command, args []string) error {
	dir := ""
	if len(args) > 0 {
		dir = args[0]
	} else {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return err
		}
	}

	// Resolve absolute path
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}

	// If the target directory is itself a framework project, refuse to park it.
	if _, ok := config.DetectFramework(absDir); ok {
		fmt.Printf("'%s' looks like a framework project, not a directory of projects.\n", absDir)
		fmt.Printf("Run 'lerd link' from that directory instead.\n")
		return nil
	}

	fmt.Printf("Parking directory: %s\n", absDir)

	// Add to parked directories in global config
	found := false
	for _, pd := range cfg.ParkedDirectories {
		if pd == absDir {
			found = true
			break
		}
	}
	if !found {
		cfg.ParkedDirectories = append(cfg.ParkedDirectories, absDir)
		if err := config.SaveGlobal(cfg); err != nil {
			return err
		}
	}

	// Scan subdirectories
	entries, err := os.ReadDir(absDir)
	if err != nil {
		return err
	}

	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		projectDir := filepath.Join(absDir, entry.Name())
		if registered, err := RegisterProject(projectDir, cfg); err != nil {
			fmt.Printf("  [WARN] could not register %s: %v\n", entry.Name(), err)
		} else if registered {
			count++
		}
	}

	if count > 0 {
		fmt.Printf("Reloading nginx (%d sites registered)...\n", count)
		if err := nginx.Reload(); err != nil {
			fmt.Printf("  [WARN] nginx reload: %v\n", err)
		}
	} else {
		fmt.Println("No PHP projects found in directory.")
	}

	// Rewrite FPM quadlets so volume mounts cover the new parked directory.
	_ = podman.RewriteFPMQuadlets()

	return nil
}

// reservedDomains are domains used by Lerd itself that cannot be assigned to user sites.
var reservedDomains = []string{}

// isReservedDomain returns true if the domain is reserved for internal Lerd use.
func isReservedDomain(domain string) bool {
	for _, r := range reservedDomains {
		if domain == r {
			return true
		}
	}
	return false
}

// freeSiteName returns the first available site name for the given path.
// If the desired name is unused, it is returned as-is.
// If it is already taken by the same path, it is returned as-is (re-link).
// If it is taken by a different path, "-2", "-3", … suffixes are tried until one is free.
func freeSiteName(desired, path string) string {
	for i := 0; ; i++ {
		candidate := desired
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d", desired, i+1)
		}
		existing, err := config.FindSite(candidate)
		if err != nil || existing == nil {
			return candidate // name is free
		}
		if existing.Path == path {
			return candidate // same site being re-registered
		}
	}
}

// RegisterProject registers a single project directory as a lerd site if it
// looks like a PHP project. It detects the framework first; if none matches it
// falls back to auto-detecting the public directory. Returns true if newly registered.
func RegisterProject(projectDir string, cfg *config.GlobalConfig) (bool, error) {
	// Don't register a directory that lives inside an existing framework project.
	// This prevents Laravel subdirs (app/, vendor/, public/, etc.) from being
	// registered as sites when a project root is accidentally used as a park dir.
	if _, ok := config.DetectFramework(filepath.Dir(projectDir)); ok {
		fmt.Printf("  [WARN] skipping %s — looks like a subdirectory of a framework project.\n         Run 'lerd link' from %s instead.\n", projectDir, filepath.Dir(projectDir))
		return false, nil
	}

	framework, ok := config.DetectFrameworkForDir(projectDir)
	detectedPublicDir := ""
	if !ok {
		detectedPublicDir = config.DetectPublicDir(projectDir)
		// Only register if we're confident it's a PHP project:
		// either a known public dir was found (has public/index.php) or
		// the root itself has composer.json / a PHP file.
		if detectedPublicDir == "." && !looksLikePHPProject(projectDir) {
			return false, nil
		}
	}

	baseName, domain := siteops.SiteNameAndDomain(filepath.Base(projectDir), cfg.DNS.TLD)
	if isReservedDomain(domain) {
		return false, nil
	}

	name := freeSiteName(baseName, projectDir)
	domain = name + "." + cfg.DNS.TLD

	// Build domains list from .lerd.yaml if present, else from auto-generated name.
	var domains []string
	if proj, pErr := config.LoadProjectConfig(projectDir); pErr == nil && len(proj.Domains) > 0 {
		for _, d := range proj.Domains {
			domains = append(domains, strings.ToLower(d)+"."+cfg.DNS.TLD)
		}
	} else {
		domains = []string{domain}
	}

	// Filter out conflicting / reserved domains. Strict — a domain may only
	// belong to one site regardless of TLS scheme. .lerd.yaml is never
	// modified on disk; the surviving list is what we register. If everything
	// was conflicted, fall back to a freshly generated <baseName>.<tld>.
	kept, removed := resolveSiteDomains(domains, baseName, projectDir, cfg.DNS.TLD)
	warnFilteredDomains(removed)
	domains = kept

	versions := siteops.DetectSiteVersions(projectDir, framework, cfg.PHP.DefaultVersion, cfg.Node.DefaultVersion)
	phpVersion, nodeVersion := versions.PHP, versions.Node

	warnMissingExtensions(projectDir, name, phpVersion, cfg)

	// Skip if already registered at this path.
	if existing, err := config.FindSite(name); err == nil && existing != nil && existing.Path == projectDir {
		return false, nil
	}

	site := config.Site{
		Name:        name,
		Domains:     domains,
		Path:        projectDir,
		PHPVersion:  phpVersion,
		NodeVersion: nodeVersion,
		Secured:     false,
		Framework:   framework,
		PublicDir:   detectedPublicDir,
	}

	if err := config.AddSite(site); err != nil {
		return false, err
	}

	if err := siteops.FinishLink(site, phpVersion); err != nil {
		return false, err
	}

	frameworkLabel := framework
	if frameworkLabel == "" {
		frameworkLabel = "unknown (public: " + detectedPublicDir + ")"
	}
	fmt.Printf("  + %s -> %s (PHP %s, Node %s, Framework: %s)\n", name, strings.Join(domains, ", "), phpVersion, nodeVersion, frameworkLabel)
	return true, nil
}

// looksLikePHPProject returns true if dir contains composer.json or any .php file
// at the top level, indicating it is likely a PHP project worth registering.
func looksLikePHPProject(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "composer.json")); err == nil {
		return true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".php") {
			return true
		}
	}
	return false
}

// ensureFPMQuadlet builds the PHP image if needed, then writes (or overwrites) the quadlet.
func ensureFPMQuadlet(phpVersion string) error {
	return ensureFPMQuadletTo(phpVersion, os.Stdout)
}

// ensureFPMQuadletTo is like ensureFPMQuadlet but writes build output to w.
func ensureFPMQuadletTo(phpVersion string, w io.Writer) error {
	versionShort := strings.ReplaceAll(phpVersion, ".", "")
	unitName := "lerd-php" + versionShort + "-fpm"

	if err := podman.BuildFPMImageTo(phpVersion, false, w); err != nil {
		return fmt.Errorf("building FPM image for PHP %s: %w", phpVersion, err)
	}
	_ = podman.StoreFPMHash()

	// Ensure the xdebug ini exists (mode=off by default).
	if _, err := os.Stat(config.PHPConfFile(phpVersion)); os.IsNotExist(err) {
		_ = podman.WriteXdebugIni(phpVersion, false)
	}

	if err := podman.WriteFPMQuadlet(phpVersion); err != nil {
		return err
	}

	return podman.StartUnit(unitName)
}
