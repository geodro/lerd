package certs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/geodro/lerd/internal/config"
)

// MkcertPath returns the path to the mkcert binary.
func MkcertPath() string {
	return filepath.Join(config.BinDir(), "mkcert")
}

// InstallCA installs the mkcert root CA into the system trust store.
func InstallCA() error {
	cmd := exec.Command(MkcertPath(), "-install")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mkcert -install: %w", err)
	}
	return nil
}

// IssueCert issues a TLS certificate covering all the given domains using mkcert.
// The cert files are named after primaryDomain. Each domain also gets a wildcard entry.
func IssueCert(primaryDomain string, allDomains []string, certsDir string) error {
	if err := os.MkdirAll(certsDir, 0755); err != nil {
		return err
	}

	certFile := filepath.Join(certsDir, primaryDomain+".crt")
	keyFile := filepath.Join(certsDir, primaryDomain+".key")

	// Build the list of SANs: each domain + its wildcard.
	var sans []string
	for _, d := range allDomains {
		sans = append(sans, d, "*."+d)
	}

	args := []string{"-cert-file", certFile, "-key-file", keyFile}
	args = append(args, sans...)

	cmd := exec.Command(MkcertPath(), args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mkcert for %s: %w", primaryDomain, err)
	}
	return nil
}

// CertExists returns true if the certificate for the domain already exists.
func CertExists(domain string) bool {
	certFile := filepath.Join(config.CertsDir(), "sites", domain+".crt")
	_, err := os.Stat(certFile)
	return err == nil
}
