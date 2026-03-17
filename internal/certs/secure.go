package certs

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/nginx"
)

// SecureSite issues a TLS certificate for the site and switches its nginx vhost to HTTPS.
func SecureSite(domain, phpVersion string) error {
	certsDir := filepath.Join(config.CertsDir(), "sites")
	if err := IssueCert(domain, certsDir); err != nil {
		return fmt.Errorf("issuing certificate: %w", err)
	}

	site := config.Site{Domain: domain, PHPVersion: phpVersion}
	if err := nginx.GenerateSSLVhost(site, phpVersion); err != nil {
		return fmt.Errorf("generating SSL vhost: %w", err)
	}

	sslConf := filepath.Join(config.NginxConfD(), domain+"-ssl.conf")
	mainConf := filepath.Join(config.NginxConfD(), domain+".conf")
	if err := os.Remove(mainConf); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing HTTP vhost: %w", err)
	}
	if err := os.Rename(sslConf, mainConf); err != nil {
		return fmt.Errorf("renaming SSL config: %w", err)
	}

	return nginx.Reload()
}

// UnsecureSite regenerates a plain HTTP vhost for the site, removing TLS.
func UnsecureSite(domain, phpVersion string) error {
	mainConf := filepath.Join(config.NginxConfD(), domain+".conf")
	if err := os.Remove(mainConf); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing SSL vhost: %w", err)
	}

	site := config.Site{Domain: domain, PHPVersion: phpVersion}
	if err := nginx.GenerateVhost(site, phpVersion); err != nil {
		return fmt.Errorf("generating HTTP vhost: %w", err)
	}

	// Remove cert files
	certsDir := filepath.Join(config.CertsDir(), "sites")
	os.Remove(filepath.Join(certsDir, domain+".crt")) //nolint:errcheck
	os.Remove(filepath.Join(certsDir, domain+".key")) //nolint:errcheck

	return nginx.Reload()
}
