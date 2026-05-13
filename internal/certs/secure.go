package certs

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
	gitpkg "github.com/geodro/lerd/internal/git"
	"github.com/geodro/lerd/internal/nginx"
)

// ErrDNSDisabled signals that the operation requires the lerd-managed DNS /
// mkcert CA stack, which the user has opted out of. Surfaces through the CLI
// `lerd secure` command and the dashboard HTTPS toggle.
var ErrDNSDisabled = fmt.Errorf("HTTPS requires lerd-managed DNS, set dns.enabled: true and re-run lerd install")

// SecureSite issues a TLS certificate for the site and switches its nginx vhost to HTTPS.
func SecureSite(site config.Site) error {
	if cfg, _ := config.LoadGlobal(); cfg != nil && !cfg.DNS.Enabled {
		return ErrDNSDisabled
	}
	if err := issueCertWithWorktrees(site); err != nil {
		return fmt.Errorf("issuing certificate: %w", err)
	}

	if site.IsCustomContainer() {
		if err := nginx.GenerateCustomSSLVhost(site); err != nil {
			return fmt.Errorf("generating custom SSL vhost: %w", err)
		}
	} else if site.IsFrankenPHP() {
		if err := nginx.GenerateFrankenPHPSSLVhost(site); err != nil {
			return fmt.Errorf("generating FrankenPHP SSL vhost: %w", err)
		}
	} else if err := nginx.GenerateSSLVhost(site, site.PHPVersion); err != nil {
		return fmt.Errorf("generating SSL vhost: %w", err)
	}

	sslConf := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+"-ssl.conf")
	mainConf := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+".conf")
	if err := os.Remove(mainConf); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing HTTP vhost: %w", err)
	}
	if err := os.Rename(sslConf, mainConf); err != nil {
		return fmt.Errorf("renaming SSL config: %w", err)
	}

	// Regenerate SSL vhosts and sync APP_URL + VITE_REVERB_* for worktrees.
	if worktrees, err := gitpkg.DetectWorktrees(site.Path, site.PrimaryDomain()); err == nil {
		for _, wt := range worktrees {
			effectivePHP := config.WorktreePHPVersion(wt.Path, site.PHPVersion)
			_ = nginx.GenerateWorktreeSSLVhost(wt.Domain, wt.Path, effectivePHP, site.PrimaryDomain(), site.Name, wt.Branch)
			envfile.SyncPrimaryDomain(wt.Path, wt.Domain, true) //nolint:errcheck
		}
	}

	return nil
}

// ReissueCertForWorktree reissues the site's TLS certificate to include
// wildcard SANs for all current worktree domains (*.branch.domain.test).
// Call this after a new worktree is created on a secured site so that
// subdomains like app.branch.domain.test are covered by the certificate.
func ReissueCertForWorktree(site config.Site) error {
	return issueCertWithWorktrees(site)
}

// issueCertWithWorktrees detects all worktrees for the site and issues a
// certificate covering the site's own domains plus *.worktreeDomain for each
// worktree, so that deep subdomains (e.g. app.branch.domain.test) work. The
// reissue is atomic: a transient mkcert failure leaves the existing cert
// intact rather than tripping RepairVhosts into flipping the site to HTTP.
func issueCertWithWorktrees(site config.Site) error {
	certsDir := filepath.Join(config.CertsDir(), "sites")

	var wtDomains []string
	if worktrees, err := gitpkg.DetectWorktrees(site.Path, site.PrimaryDomain()); err == nil {
		for _, wt := range worktrees {
			wtDomains = append(wtDomains, wt.Domain)
		}
	}
	domains := WorktreeCertDomains(site.Domains, wtDomains)

	return IssueCertForce(site.PrimaryDomain(), domains, certsDir)
}

// WorktreeCertDomains builds the full domain list for a certificate that covers
// the site's own domains plus all worktree domains. Each domain gets a wildcard
// entry via IssueCert, so worktree domains like branch.myapp.test produce
// *.branch.myapp.test SANs for deep subdomain coverage.
func WorktreeCertDomains(siteDomains []string, worktreeDomains []string) []string {
	domains := make([]string, len(siteDomains))
	copy(domains, siteDomains)
	domains = append(domains, worktreeDomains...)
	return domains
}

// UnsecureSite regenerates a plain HTTP vhost for the site, removing TLS.
func UnsecureSite(site config.Site) error {
	mainConf := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+".conf")
	if err := os.Remove(mainConf); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing SSL vhost: %w", err)
	}

	if site.IsCustomContainer() {
		if err := nginx.GenerateCustomVhost(site); err != nil {
			return fmt.Errorf("generating custom HTTP vhost: %w", err)
		}
	} else if site.IsFrankenPHP() {
		if err := nginx.GenerateFrankenPHPVhost(site); err != nil {
			return fmt.Errorf("generating FrankenPHP HTTP vhost: %w", err)
		}
	} else if err := nginx.GenerateVhost(site, site.PHPVersion); err != nil {
		return fmt.Errorf("generating HTTP vhost: %w", err)
	}

	// Switch any worktree SSL vhosts back to plain HTTP and sync env.
	if worktrees, err := gitpkg.DetectWorktrees(site.Path, site.PrimaryDomain()); err == nil {
		for _, wt := range worktrees {
			effectivePHP := config.WorktreePHPVersion(wt.Path, site.PHPVersion)
			_ = nginx.GenerateWorktreeVhost(wt.Domain, wt.Path, effectivePHP, site.Name, wt.Branch)
			envfile.SyncPrimaryDomain(wt.Path, wt.Domain, false) //nolint:errcheck
		}
	}

	// Remove cert files
	certsDir := filepath.Join(config.CertsDir(), "sites")
	os.Remove(filepath.Join(certsDir, site.PrimaryDomain()+".crt")) //nolint:errcheck
	os.Remove(filepath.Join(certsDir, site.PrimaryDomain()+".key")) //nolint:errcheck

	return nil
}
