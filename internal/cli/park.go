package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/laravel"
	"github.com/geodro/lerd/internal/nginx"
	nodeDet "github.com/geodro/lerd/internal/node"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
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
		fmt.Println("No Laravel projects found in directory.")
	}

	return nil
}

// reservedDomains are domains used by Lerd itself that cannot be assigned to user sites.
var reservedDomains = []string{"lerd.test"}

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

// siteNameAndDomain converts a directory name into a clean site name and .test domain.
// Strips well-known TLDs (e.g. .com, .ltd) and replaces remaining dots with dashes.
// Examples:
//   - "myapp"            → "myapp",         "myapp.test"
//   - "admin.astrolov.com" → "admin-astrolov", "admin-astrolov.test"
//   - "my.project.io"   → "my-project",     "my-project.test"
func siteNameAndDomain(dirName, tld string) (string, string) {
	knownTLDs := []string{
		".com", ".net", ".org", ".io", ".co", ".ltd", ".dev", ".app", ".me",
		".info", ".biz", ".uk", ".us", ".eu", ".de", ".fr", ".ca", ".au",
	}
	name := strings.ToLower(dirName)
	for _, ext := range knownTLDs {
		if strings.HasSuffix(name, ext) {
			name = name[:len(name)-len(ext)]
			break
		}
	}
	name = strings.ReplaceAll(name, ".", "-")
	return name, name + "." + tld
}

// RegisterProject registers a single project directory as a lerd site if it is a Laravel
// project and not already registered. Returns true if newly registered.
func RegisterProject(projectDir string, cfg *config.GlobalConfig) (bool, error) {
	if !laravel.IsLaravel(projectDir) {
		return false, nil
	}

	baseName, domain := siteNameAndDomain(filepath.Base(projectDir), cfg.DNS.TLD)
	if isReservedDomain(domain) {
		return false, nil
	}

	name := freeSiteName(baseName, projectDir)
	domain = name + "." + cfg.DNS.TLD

	phpVersion, err := phpDet.DetectVersion(projectDir)
	if err != nil {
		phpVersion = cfg.PHP.DefaultVersion
	}

	nodeVersion, err := nodeDet.DetectVersion(projectDir)
	if err != nil {
		nodeVersion = cfg.Node.DefaultVersion
	}

	warnMissingExtensions(projectDir, name, phpVersion, cfg)

	// Skip if already registered at this path.
	if existing, err := config.FindSite(name); err == nil && existing != nil && existing.Path == projectDir {
		return false, nil
	}

	site := config.Site{
		Name:        name,
		Domain:      domain,
		Path:        projectDir,
		PHPVersion:  phpVersion,
		NodeVersion: nodeVersion,
		Secured:     false,
	}

	if err := config.AddSite(site); err != nil {
		return false, err
	}

	if err := nginx.GenerateVhost(site, phpVersion); err != nil {
		return false, fmt.Errorf("generating vhost: %w", err)
	}

	if err := ensureFPMQuadlet(phpVersion); err != nil {
		fmt.Printf("  [WARN] could not ensure FPM for PHP %s: %v\n", phpVersion, err)
	}

	fmt.Printf("  + %s -> %s (PHP %s, Node %s)\n", name, domain, phpVersion, nodeVersion)
	return true, nil
}

// ensureFPMQuadlet builds the PHP image if needed, then writes (or overwrites) the quadlet.
func ensureFPMQuadlet(phpVersion string) error {
	versionShort := strings.ReplaceAll(phpVersion, ".", "")
	unitName := "lerd-php" + versionShort + "-fpm"

	if err := podman.BuildFPMImage(phpVersion); err != nil {
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
