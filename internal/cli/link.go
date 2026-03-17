package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/nginx"
	nodeDet "github.com/geodro/lerd/internal/node"
	phpDet "github.com/geodro/lerd/internal/php"
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

	rawName := filepath.Base(cwd)
	if len(args) > 0 {
		rawName = args[0]
	}

	name, domain := siteNameAndDomain(rawName, cfg.DNS.TLD)
	if customDomain != "" {
		domain = customDomain
	}

	if isReservedDomain(domain) {
		return fmt.Errorf("domain %q is reserved for internal Lerd use", domain)
	}

	phpVersion, err := phpDet.DetectVersion(cwd)
	if err != nil {
		phpVersion = cfg.PHP.DefaultVersion
	}

	nodeVersion, err := nodeDet.DetectVersion(cwd)
	if err != nil {
		nodeVersion = cfg.Node.DefaultVersion
	}

	site := config.Site{
		Name:        name,
		Domain:      domain,
		Path:        cwd,
		PHPVersion:  phpVersion,
		NodeVersion: nodeVersion,
		Secured:     false,
	}

	if err := config.AddSite(site); err != nil {
		return fmt.Errorf("registering site: %w", err)
	}

	if err := nginx.GenerateVhost(site, phpVersion); err != nil {
		return fmt.Errorf("generating vhost: %w", err)
	}

	if err := ensureFPMQuadlet(phpVersion); err != nil {
		fmt.Printf("[WARN] FPM quadlet for PHP %s: %v\n", phpVersion, err)
	}

	fmt.Printf("Linked: %s -> %s (PHP %s, Node %s)\n", name, domain, phpVersion, nodeVersion)

	if err := nginx.Reload(); err != nil {
		fmt.Printf("[WARN] nginx reload: %v\n", err)
	}

	return nil
}
