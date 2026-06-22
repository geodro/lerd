package dns

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
)

// pastaDefaultForwarder is pasta's rootless-netns DNS forwarder IP, which
// bridges into the host resolver and preserves .test routing. Last-resort
// fallback when no other upstream is usable.
const pastaDefaultForwarder = "169.254.1.1"

// isFileContent returns true if the file at path already contains exactly content.
func isFileContent(path string, content []byte) bool {
	existing, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return string(existing) == string(content)
}

// parseNameservers parses nameserver entries from a resolv.conf-style file.
// Skips loopback, stub resolver, and zoned link-local addresses.
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
		if ip := sanitizeDNSIP(ip); ip != "" {
			servers = append(servers, ip)
		}
	}
	return servers
}

// sanitizeDNSIP returns ip if it is usable as an upstream DNS target inside the
// lerd container netns, or "" if it should be filtered. Loopback, unspecified
// and zoned addresses (e.g. fe80::...%18) are rejected — podman/netavark cannot
// consume scoped addresses, and link-local zones are interface-bound anyway.
func sanitizeDNSIP(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" || ip == "--" {
		return ""
	}
	if strings.ContainsRune(ip, '%') {
		return ""
	}
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.IsLoopback() || parsed.IsUnspecified() {
		return ""
	}
	return ip
}

// NormalizeUpstreamEntry validates a single dns.upstream entry, an IP with an
// optional dnsmasq-style "#port" suffix, and returns its cleaned form. ok is
// false when the IP is missing, malformed, loopback/unspecified, or the port
// is not a number, so callers can report the bad entry back to the user.
func NormalizeUpstreamEntry(entry string) (string, bool) {
	ip, port, hasPort := strings.Cut(strings.TrimSpace(entry), "#")
	clean := sanitizeDNSIP(ip)
	if clean == "" {
		return "", false
	}
	if hasPort {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", false
		}
		clean += "#" + port
	}
	return clean, true
}

// configuredUpstreamDNS returns the upstream DNS servers pinned in the global
// config (dns.upstream), sanitized. Returns nil when none are configured or
// none are usable, in which case callers fall back to auto-detection.
func configuredUpstreamDNS() []string {
	cfg, err := config.LoadGlobal()
	if err != nil || cfg == nil {
		return nil
	}
	var out []string
	for _, entry := range cfg.DNS.Upstream {
		if clean, ok := NormalizeUpstreamEntry(entry); ok {
			out = append(out, clean)
		}
	}
	return out
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

// sudoWriteFile writes content to a system path by piping it through
// `sudo tee <path>`. Earlier versions wrote a /tmp/lerd-sudo-XXXXXX
// staging file then ran `sudo cp tmp dst`, which required a sudoers rule
// with a wildcard in the source argument. Modern strict sudo parsers —
// sudo-rs (the Rust rewrite that Ubuntu 26.04 LTS made the default) and
// C sudo from 1.9.16 onward (Fedora 41+, Arch / CachyOS, openSUSE
// Tumbleweed, NixOS unstable) — hard-reject wildcards in command
// arguments and fall back to the password-prompt path on every call,
// which broke install on those distros (issue #269). Piping to tee with
// a fully qualified destination has no wildcard, so the matching sudoers
// rule grants `tee /exact/path` cleanly.
func sudoWriteFile(path string, content []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	mkdirCmd := exec.Command("sudo", "mkdir", "-p", dir)
	mkdirCmd.Stdin = os.Stdin
	mkdirCmd.Stdout = os.Stdout
	mkdirCmd.Stderr = os.Stderr
	if err := mkdirCmd.Run(); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	teeCmd := exec.Command("sudo", "tee", path)
	teeCmd.Stdin = bytes.NewReader(content)
	// tee echoes its input on stdout; discard that or it spams the
	// terminal during install.
	teeCmd.Stdout = io.Discard
	teeCmd.Stderr = os.Stderr
	if err := teeCmd.Run(); err != nil {
		return fmt.Errorf("tee to %s: %w", path, err)
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
//
// When cfg.LAN.Exposed is false the config answers .test queries with
// 127.0.0.1 / ::1, suitable for local-only use. When it's true the config
// answers with the host's primary LAN IP (v4 + v6 when available) so remote
// clients reach the actual nginx instance through the lerd-dns-forwarder
// service.
func WriteDnsmasqConfig(dir string) error {
	target := "127.0.0.1"
	if cfg, err := config.LoadGlobal(); err == nil && cfg != nil && cfg.LAN.Exposed {
		if ip := primaryLANIP(); ip != "" {
			target = ip
		}
	}
	return WriteDnsmasqConfigFor(dir, target)
}

// primaryLANIP returns the local IPv4 address that the kernel would use
// to reach a public destination.
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

// primaryLANIPv6 returns the host's primary global-unicast IPv6, or "" if
// none. Link-local and ULA are skipped: LAN-exposed mode publishes
// reachable endpoints, and those scopes don't qualify.
func primaryLANIPv6() string {
	conn, err := net.Dial("udp6", "[2001:4860:4860::8888]:80")
	if err == nil {
		defer conn.Close()
		ip := conn.LocalAddr().(*net.UDPAddr).IP
		if ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() {
			return ip.String()
		}
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
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP
			if ip.To4() != nil {
				continue
			}
			if ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() {
				return ip.String()
			}
		}
	}
	return ""
}

// deriveV6Target picks the AAAA target for .test mirroring v4's reach:
// loopback or empty → ::1; LAN-exposed → host's primary global v6, else ::1.
func deriveV6Target(v4 string) string {
	if v4 == "" || v4 == "127.0.0.1" {
		return "::1"
	}
	if v6 := primaryLANIPv6(); v6 != "" {
		return v6
	}
	return "::1"
}

// WriteDnsmasqConfigFor writes the lerd dnsmasq config with `target` as the
// IPv4 answer for `*.test`. An AAAA pair is derived (::1 locally, host's
// global v6 when LAN-exposed). Upstreams come from the system; if none are
// usable, the pasta default forwarder is used so .test routing keeps working.
func WriteDnsmasqConfigFor(dir, target string) error {
	return WriteDnsmasqConfigDual(dir, target, deriveV6Target(target))
}

// WriteDnsmasqConfigDual is the v4+v6 form of WriteDnsmasqConfigFor. Pass
// v6Target = "" to skip the AAAA record entirely.
func WriteDnsmasqConfigDual(dir, v4Target, v6Target string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if v4Target == "" {
		v4Target = "127.0.0.1"
	}

	upstreams := readUpstreamDNS()
	if len(upstreams) == 0 {
		upstreams = defaultUpstreamFallback()
	}

	var sb strings.Builder
	sb.WriteString("# Lerd DNS configuration\n")
	sb.WriteString("port=5300\n")
	if len(upstreams) > 0 {
		sb.WriteString("no-resolv\n")
		for _, ip := range upstreams {
			fmt.Fprintf(&sb, "server=%s\n", ip)
		}
	}
	fmt.Fprintf(&sb, "address=/.test/%s\n", v4Target)
	if v6Target != "" {
		fmt.Fprintf(&sb, "address=/.test/%s\n", v6Target)
	}

	return os.WriteFile(filepath.Join(dir, "lerd.conf"), []byte(sb.String()), 0644)
}
