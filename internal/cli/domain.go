package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/certs"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
	"github.com/geodro/lerd/internal/nginx"
	"github.com/geodro/lerd/internal/podman"
	"github.com/spf13/cobra"
)

// NewDomainCmd returns the domain command with add/remove/list subcommands.
func NewDomainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "domain",
		Short: "Manage domains for the current site",
	}
	cmd.AddCommand(newDomainAddCmd())
	cmd.AddCommand(newDomainRemoveCmd())
	cmd.AddCommand(newDomainListCmd())
	return cmd
}

func newDomainAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <name>",
		Short: "Add a domain to the current site (name without .test)",
		Args:  cobra.ExactArgs(1),
		RunE:  runDomainAdd,
	}
}

func newDomainRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a domain from the current site (name without .test)",
		Args:  cobra.ExactArgs(1),
		RunE:  runDomainRemove,
	}
}

func newDomainListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List domains for the current site",
		RunE:  runDomainList,
	}
}

// resolveSiteForCwd finds the site registered for the current working directory.
func resolveSiteForCwd() (*config.Site, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	site, err := config.FindSiteByPath(cwd)
	if err != nil {
		return nil, fmt.Errorf("no site registered for %s — link it first with lerd link", cwd)
	}
	return site, nil
}

func runDomainAdd(_ *cobra.Command, args []string) error {
	site, err := resolveSiteForCwd()
	if err != nil {
		return err
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}

	domainName := strings.ToLower(args[0])
	fullDomain := domainName + "." + cfg.DNS.TLD

	if isReservedDomain(fullDomain) {
		return fmt.Errorf("domain %q is reserved for internal Lerd use", fullDomain)
	}

	// Check if already present on this site.
	if site.HasDomain(fullDomain) {
		return fmt.Errorf("site %q already has domain %q", site.Name, fullDomain)
	}

	// Check if used by another site (strict — regardless of TLS scheme).
	if existing, err := config.IsDomainUsed(fullDomain); err == nil && existing != nil {
		return fmt.Errorf("domain %q is already used by site %q", fullDomain, existing.Name)
	}

	// Remove old vhost before updating domains (file is named after primary domain).
	oldPrimary := site.PrimaryDomain()

	site.Domains = append(site.Domains, fullDomain)

	if err := config.AddSite(*site); err != nil {
		return fmt.Errorf("updating site registry: %w", err)
	}

	// Sync to .lerd.yaml.
	_ = config.SyncProjectDomains(site.Path, site.Domains, cfg.DNS.TLD)

	// Regenerate vhost (file stays named after primary domain).
	if err := RegenerateSiteVhost(site, oldPrimary); err != nil {
		return err
	}

	// If secured, reissue cert to cover the new domain.
	if site.Secured {
		certsDir := filepath.Join(config.CertsDir(), "sites")
		if err := certs.IssueCert(site.PrimaryDomain(), site.Domains, certsDir); err != nil {
			fmt.Printf("[WARN] reissuing certificate: %v\n", err)
		}
	}

	if err := podman.WriteContainerHosts(); err != nil {
		fmt.Printf("[WARN] updating container hosts file: %v\n", err)
	}

	if err := nginx.Reload(); err != nil {
		fmt.Printf("[WARN] nginx reload: %v\n", err)
	}

	if site.PrimaryDomain() != oldPrimary {
		if err := envfile.SyncPrimaryDomain(site.Path, site.PrimaryDomain(), site.Secured); err != nil {
			fmt.Printf("[WARN] syncing .env to new primary domain: %v\n", err)
		}
	}

	fmt.Printf("Added domain %s to site %s\n", fullDomain, site.Name)
	return nil
}

func runDomainRemove(_ *cobra.Command, args []string) error {
	site, err := resolveSiteForCwd()
	if err != nil {
		return err
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}

	domainName := strings.ToLower(args[0])
	fullDomain := domainName + "." + cfg.DNS.TLD

	if !site.HasDomain(fullDomain) {
		return fmt.Errorf("site %q does not have domain %q", site.Name, fullDomain)
	}

	if len(site.Domains) <= 1 {
		return fmt.Errorf("cannot remove the last domain from site %q", site.Name)
	}

	oldPrimary := site.PrimaryDomain()

	// Remove the domain.
	var newDomains []string
	for _, d := range site.Domains {
		if d != fullDomain {
			newDomains = append(newDomains, d)
		}
	}
	site.Domains = newDomains

	if err := config.AddSite(*site); err != nil {
		return fmt.Errorf("updating site registry: %w", err)
	}

	// Sync to .lerd.yaml.
	_ = config.SyncProjectDomains(site.Path, site.Domains, cfg.DNS.TLD)

	// If the primary domain changed (we removed the old primary), rename the vhost file.
	if err := RegenerateSiteVhost(site, oldPrimary); err != nil {
		return err
	}

	// If secured, reissue cert without the removed domain.
	if site.Secured {
		certsDir := filepath.Join(config.CertsDir(), "sites")
		if err := certs.IssueCert(site.PrimaryDomain(), site.Domains, certsDir); err != nil {
			fmt.Printf("[WARN] reissuing certificate: %v\n", err)
		}
	}

	if err := podman.WriteContainerHosts(); err != nil {
		fmt.Printf("[WARN] updating container hosts file: %v\n", err)
	}

	if err := nginx.Reload(); err != nil {
		fmt.Printf("[WARN] nginx reload: %v\n", err)
	}

	if site.PrimaryDomain() != oldPrimary {
		if err := envfile.SyncPrimaryDomain(site.Path, site.PrimaryDomain(), site.Secured); err != nil {
			fmt.Printf("[WARN] syncing .env to new primary domain: %v\n", err)
		}
	}

	fmt.Printf("Removed domain %s from site %s\n", fullDomain, site.Name)
	return nil
}

func runDomainList(_ *cobra.Command, _ []string) error {
	site, err := resolveSiteForCwd()
	if err != nil {
		return err
	}

	for i, d := range site.Domains {
		if i == 0 {
			fmt.Printf("  %s (primary)\n", d)
		} else {
			fmt.Printf("  %s\n", d)
		}
	}
	return nil
}

// RegenerateSiteVhost regenerates the nginx vhost for a site. If the primary
// domain changed (oldPrimary != current primary), it removes the old vhost file first.
func RegenerateSiteVhost(site *config.Site, oldPrimary string) error {
	newPrimary := site.PrimaryDomain()

	// If primary changed, remove the old conf file.
	if oldPrimary != newPrimary {
		_ = nginx.RemoveVhost(oldPrimary)
	}

	if site.Secured {
		if err := nginx.GenerateSSLVhost(*site, site.PHPVersion); err != nil {
			return fmt.Errorf("generating SSL vhost: %w", err)
		}
		sslConf := filepath.Join(config.NginxConfD(), newPrimary+"-ssl.conf")
		mainConf := filepath.Join(config.NginxConfD(), newPrimary+".conf")
		_ = os.Remove(mainConf)
		if err := os.Rename(sslConf, mainConf); err != nil {
			return fmt.Errorf("installing SSL vhost: %w", err)
		}
	} else {
		if err := nginx.GenerateVhost(*site, site.PHPVersion); err != nil {
			return fmt.Errorf("generating vhost: %w", err)
		}
	}
	return nil
}
