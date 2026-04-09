package dns

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
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
// then using sudo cp, so sudo can prompt for a password on the terminal.
// The mode parameter sets the file permissions (e.g. 0644, 0755).
func sudoWriteFile(path string, content []byte, mode os.FileMode) error {
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
	mkdirCmd := exec.Command("sudo", "mkdir", "-p", dir)
	mkdirCmd.Stdin = os.Stdin
	mkdirCmd.Stdout = os.Stdout
	mkdirCmd.Stderr = os.Stderr
	if err := mkdirCmd.Run(); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	cpCmd := exec.Command("sudo", "cp", tmp.Name(), path)
	cpCmd.Stdin = os.Stdin
	cpCmd.Stdout = os.Stdout
	cpCmd.Stderr = os.Stderr
	if err := cpCmd.Run(); err != nil {
		return fmt.Errorf("cp to %s: %w", path, err)
	}

	chmodCmd := exec.Command("sudo", "chmod", fmt.Sprintf("%o", mode), path)
	chmodCmd.Stdin = os.Stdin
	chmodCmd.Stdout = os.Stdout
	chmodCmd.Stderr = os.Stderr
	if err := chmodCmd.Run(); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}

// WriteDnsmasqConfig writes the lerd dnsmasq config to the given directory,
// auto-detecting the right target based on whether `lerd lan:expose` is on.
// When cfg.LAN.Exposed is true, .test queries resolve to the host's primary
// LAN IP; otherwise they resolve to 127.0.0.1.
func WriteDnsmasqConfig(dir string) error {
	target := "127.0.0.1"
	if cfg, err := config.LoadGlobal(); err == nil && cfg != nil && cfg.LAN.Exposed {
		if ip := primaryLANIP(); ip != "" {
			target = ip
		}
	}
	return WriteDnsmasqConfigFor(dir, target)
}

// primaryLANIP returns the local IPv4 address the kernel would use to reach a
// public destination. Duplicated from cli/dns.go to avoid an import cycle.
func primaryLANIP() string {
	conn, err := net.Dial("udp4", "1.1.1.1:80")
	if err == nil {
		defer conn.Close()
		return conn.LocalAddr().(*net.UDPAddr).IP.String()
	}
	ifaces, ifErr := net.Interfaces()
	if ifErr != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok {
				if v4 := ipnet.IP.To4(); v4 != nil && !v4.IsLoopback() {
					return v4.String()
				}
			}
		}
	}
	return ""
}

// WriteDnsmasqConfigFor writes the lerd dnsmasq config with target as the IP
// returned for *.test queries. Upstreams are detected from the running system.
func WriteDnsmasqConfigFor(dir, target string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if target == "" {
		target = "127.0.0.1"
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
	fmt.Fprintf(&sb, "address=/.test/%s\n", target)

	return os.WriteFile(filepath.Join(dir, "lerd.conf"), []byte(sb.String()), 0644)
}
