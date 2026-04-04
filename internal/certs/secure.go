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

// SecureSite issues a TLS certificate for the site and switches its nginx vhost to HTTPS.
func SecureSite(site config.Site) error {
	certsDir := filepath.Join(config.CertsDir(), "sites")
	if err := IssueCert(site.PrimaryDomain(), site.Domains, certsDir); err != nil {
		return fmt.Errorf("issuing certificate: %w", err)
	}

	if err := nginx.GenerateSSLVhost(site, site.PHPVersion); err != nil {
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

	// Regenerate SSL vhosts and update APP_URL for any worktrees.
	if worktrees, err := gitpkg.DetectWorktrees(site.Path, site.PrimaryDomain()); err == nil {
		for _, wt := range worktrees {
			_ = nginx.GenerateWorktreeSSLVhost(wt.Domain, wt.Path, site.PHPVersion, site.PrimaryDomain())
			envfile.UpdateAppURL(wt.Path, "https", wt.Domain) //nolint:errcheck
		}
	}

	return nil
}

// UnsecureSite regenerates a plain HTTP vhost for the site, removing TLS.
func UnsecureSite(site config.Site) error {
	mainConf := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+".conf")
	if err := os.Remove(mainConf); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing SSL vhost: %w", err)
	}

	if err := nginx.GenerateVhost(site, site.PHPVersion); err != nil {
		return fmt.Errorf("generating HTTP vhost: %w", err)
	}

	// Switch any worktree SSL vhosts back to plain HTTP and update APP_URL.
	if worktrees, err := gitpkg.DetectWorktrees(site.Path, site.PrimaryDomain()); err == nil {
		for _, wt := range worktrees {
			_ = nginx.GenerateWorktreeVhost(wt.Domain, wt.Path, site.PHPVersion)
			envfile.UpdateAppURL(wt.Path, "http", wt.Domain) //nolint:errcheck
		}
	}

	// Remove cert files
	certsDir := filepath.Join(config.CertsDir(), "sites")
	os.Remove(filepath.Join(certsDir, site.PrimaryDomain()+".crt")) //nolint:errcheck
	os.Remove(filepath.Join(certsDir, site.PrimaryDomain()+".key")) //nolint:errcheck

	return nil
}
