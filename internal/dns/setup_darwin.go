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

// InstallSudoers writes a sudoers drop-in granting the current user passwordless
// access to write /etc/resolver/<tld>. This is required so the DNS watcher can
// automatically repair the resolver config after sleep/wake without prompting.
func InstallSudoers() error {
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("LOGNAME")
	}
	if user == "" {
		return fmt.Errorf("cannot determine current user")
	}

	// sudoWriteFile calls: sudo mkdir -p <dir>, sudo cp <tmp> <dst>, sudo chmod <mode> <dst>
	// $TMPDIR on macOS is /var/folders/<2-char>/<hash>/T/ — two wildcard levels needed.
	content := fmt.Sprintf(
		"# Lerd: allow writing /etc/resolver/<tld> without password\n"+
			"%s ALL=(root) NOPASSWD: /bin/mkdir -p /etc/resolver\n"+
			"%s ALL=(root) NOPASSWD: /bin/cp /var/folders/*/*/T/lerd-sudo-* /etc/resolver/*\n"+
			"%s ALL=(root) NOPASSWD: /bin/chmod 644 /etc/resolver/*\n",
		user, user, user,
	)

	const sudoersPath = "/etc/sudoers.d/lerd"
	if isFileContent(sudoersPath, []byte(content)) {
		return nil
	}

	fmt.Println("  [sudo required] Installing DNS sudoers rule")
	if err := sudoWriteFile(sudoersPath, []byte(content), 0440); err != nil {
		return fmt.Errorf("writing sudoers drop-in: %w", err)
	}
	return nil
}

// ReadContainerDNS returns nil on macOS — the Podman network does not need
// container-side DNS servers because dnsmasq runs natively, not in a container.
func ReadContainerDNS() []string { return nil }

// ReadUpstreamDNS returns upstream DNS server IPs from /etc/resolv.conf.
func ReadUpstreamDNS() []string {
	return readUpstreamDNS()
}

// ResolverHint returns a user-facing hint for restarting DNS on macOS.
func ResolverHint() string {
	return "run 'lerd install' to reconfigure DNS"
}
