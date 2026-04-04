package dns

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// isFileContent returns true if the file at path already contains exactly content.
func isFileContent(path string, content []byte) bool {
	existing, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return string(existing) == string(content)
}

// parseNameservers parses nameserver entries from a resolv.conf-style file.
// Skips loopback and stub resolver addresses.
func parseNameservers(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var servers []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "nameserver ") {
			continue
		}
		ip := strings.TrimSpace(strings.TrimPrefix(line, "nameserver "))
		// Skip loopback / stub resolver addresses
		if ip == "" || ip == "127.0.0.1" || ip == "127.0.0.53" || ip == "::1" {
			continue
		}
		servers = append(servers, ip)
	}
	return servers
}

// WaitReady blocks until lerd-dns is accepting TCP connections on port 5300
// (dnsmasq supports DNS over TCP), or until the timeout elapses.
// Returns nil when ready, error on timeout.
func WaitReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:5300", 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("lerd-dns not ready after %s", timeout)
}

// sudoWriteFile writes content to a system path by writing to a temp file
// then using a single sudo sh invocation to mkdir + cp, so the user is only
// prompted for their password once regardless of sudo credential cache state.
func sudoWriteFile(path string, content []byte) error {
	tmp, err := os.CreateTemp("", "lerd-sudo-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	dir := filepath.Dir(path)
	cmd := exec.Command("sudo", "sh", "-c",
		fmt.Sprintf("mkdir -p %q && cp %q %q", dir, tmp.Name(), path))
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// WriteDnsmasqConfig writes the lerd dnsmasq config to the given directory.
// Upstream DNS servers are detected from the running system via readUpstreamDNS,
// which is implemented per-platform. If no upstreams are detected, no-resolv is
// omitted so dnsmasq falls back to the container's /etc/resolv.conf.
func WriteDnsmasqConfig(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	upstreams := readUpstreamDNS()

	var sb strings.Builder
	sb.WriteString("# Lerd DNS configuration\n")
	sb.WriteString("port=5300\n")
	if len(upstreams) > 0 {
		sb.WriteString("no-resolv\n")
		for _, ip := range upstreams {
			fmt.Fprintf(&sb, "server=%s\n", ip)
		}
	}
	sb.WriteString("address=/.test/127.0.0.1\n")

	return os.WriteFile(filepath.Join(dir, "lerd.conf"), []byte(sb.String()), 0644)
}
