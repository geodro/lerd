//go:build darwin

package dns

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/geodro/lerd/internal/config"
)

// readUpstreamDNS reads upstream DNS servers from /etc/resolv.conf.
// On macOS the OS keeps /etc/resolv.conf up-to-date with DHCP-assigned DNS servers,
// so parsing it gives the real upstreams without needing nmcli or resolvectl.
func readUpstreamDNS() []string {
	return parseNameservers("/etc/resolv.conf")
}

// ConfigureResolver writes /etc/resolver/<tld> so macOS routes .<tld> queries to
// the lerd-dns dnsmasq container on port 5300. macOS checks /etc/resolver/<tld>
// automatically for per-TLD DNS overrides — no daemon restart required.
func ConfigureResolver() error {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	tld := cfg.DNS.TLD
	if tld == "" {
		tld = "test"
	}

	resolverFile := filepath.Join("/etc/resolver", tld)
	content := []byte("nameserver 127.0.0.1\nport 5300\n")

	if isFileContent(resolverFile, content) {
		return nil
	}

	fmt.Println("  [sudo required] Configuring /etc/resolver for ." + tld + " DNS resolution")
	return sudoWriteFile(resolverFile, content, 0644)
}

// Teardown removes the /etc/resolver/<tld> file written by ConfigureResolver.
func Teardown() {
	cfg, _ := config.LoadGlobal()
	tld := "test"
	if cfg != nil && cfg.DNS.TLD != "" {
		tld = cfg.DNS.TLD
	}

	resolverFile := filepath.Join("/etc/resolver", tld)
	if _, err := os.Stat(resolverFile); err == nil {
		rmCmd := exec.Command("sudo", "rm", "-f", resolverFile)
		rmCmd.Stdin = os.Stdin
		rmCmd.Stdout = os.Stdout
		rmCmd.Stderr = os.Stderr
		rmCmd.Run() //nolint:errcheck
	}
}

// InstallSudoers is a no-op on macOS. /etc/resolver writes happen interactively
// during lerd install and do not require a passwordless sudoers drop-in.
func InstallSudoers() error {
	return nil
}

// ReadContainerDNS returns nil on macOS — the Podman network does not need
// container-side DNS servers because dnsmasq runs natively, not in a container.
func ReadContainerDNS() []string { return nil }
