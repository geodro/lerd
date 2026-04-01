package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/nginx"
	nodeDet "github.com/geodro/lerd/internal/node"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/spf13/cobra"
)

// NewLinkCmd returns the link command.
func NewLinkCmd() *cobra.Command {
	var domain string

	cmd := &cobra.Command{
		Use:   "link [name]",
		Short: "Link the current directory as a site",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLink(args, domain)
		},
	}
	cmd.Flags().StringVar(&domain, "domain", "", "Custom domain (defaults to <name>.test)")
	return cmd
}

func runLink(args []string, customDomain string) error {
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
			if ok, err := confirmReplace("framework", proj.Framework, existing, proj.FrameworkDef); err != nil {
				return err
			} else if ok {
				_ = config.SaveFramework(proj.FrameworkDef)
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
	domain := name + "." + cfg.DNS.TLD
	if customDomain != "" {
		domain = strings.ToLower(customDomain)
	}

	if isReservedDomain(domain) {
		return fmt.Errorf("domain %q is reserved for internal Lerd use", domain)
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

	// Preserve Secured state if the same site is being re-linked.
	secured := false
	if existing, err := config.FindSite(name); err == nil && existing != nil && existing.Path == cwd {
		secured = existing.Secured
	}

	site := config.Site{
		Name:        name,
		Domain:      domain,
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

	if secured {
		// Regenerate SSL vhost in place, reusing the existing cert.
		if err := nginx.GenerateSSLVhost(site, phpVersion); err != nil {
			return fmt.Errorf("generating SSL vhost: %w", err)
		}
		sslConf := filepath.Join(config.NginxConfD(), site.Domain+"-ssl.conf")
		mainConf := filepath.Join(config.NginxConfD(), site.Domain+".conf")
		_ = os.Remove(mainConf)
		if err := os.Rename(sslConf, mainConf); err != nil {
			return fmt.Errorf("installing SSL vhost: %w", err)
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
	fmt.Printf("Linked: %s -> %s (PHP %s, Node %s, Framework: %s)\n", name, domain, phpVersion, nodeVersion, frameworkLabel)

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
				save := true
				if loadErr == nil {
					var err error
					save, err = confirmReplace("service", svc.Name, existing, svc.Custom)
					if err != nil {
						return err
					}
				}
				if save {
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
	}

	return nil
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
