package podman

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// LerdULAv6Subnet is the deterministic IPv6 ULA prefix for the lerd network.
// The `1e7d` body is "lerd" in leetspeak, picked to avoid colliding with
// common defaults (fd00::, fd00:beef::, etc.).
const LerdULAv6Subnet = "fd00:1e7d::/64"

// LerdNetworkMTU pins the lerd bridge to the universal safe MTU. Fedora's
// rootless podman defaults eth0 to 65520 in the netns, which triggers
// EMSGSIZE on UDP DNS writes and stalls every lookup ~5 seconds.
const LerdNetworkMTU = "1500"

// ErrNetworkNeedsMigration signals the lerd network's dual-stack schema
// doesn't match host IPv6 support. Callers should run RecreateNetwork.
var ErrNetworkNeedsMigration = errors.New("lerd network needs recreate to match host IPv6 support")

// Swappable /proc paths so tests can stage a synthetic host profile.
var (
	ipv6DisablePath = "/proc/sys/net/ipv6/conf/all/disable_ipv6"
	ipv6IfInet6Path = "/proc/net/if_inet6"
)

// HostHasUsableIPv6 reports whether the host has a non-loopback,
// non-link-local IPv6 address. Without one, netavark can't reliably
// assign the ULA gateway on the rootless bridge and aardvark-dns bind fails.
func HostHasUsableIPv6() bool {
	if data, err := os.ReadFile(ipv6DisablePath); err == nil {
		if strings.TrimSpace(string(data)) == "1" {
			return false
		}
	}
	data, err := os.ReadFile(ipv6IfInet6Path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		scope, err := strconv.ParseUint(fields[3], 16, 32)
		if err != nil {
			continue
		}
		// 0x10 loopback, 0x20 link-local; anything else is usable.
		if scope == 0x10 || scope == 0x20 {
			continue
		}
		return true
	}
	return false
}

// NetworkGateway returns the gateway IP of the named Podman network.
// Falls back to "127.0.0.1" if it cannot be determined. When the network has
// both v4 and v6 subnets, returns the v4 gateway (which most callers expect
// for backwards compatibility).
func NetworkGateway(name string) string {
	out, err := exec.Command(PodmanBin(), "network", "inspect", name,
		"--format", "{{range .Subnets}}{{if (.Gateway).To4}}{{.Gateway}}{{end}}{{end}}").Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		// Fallback for older podman that doesn't expose .To4 in the template.
		out, err = exec.Command(PodmanBin(), "network", "inspect", name,
			"--format", "{{range .Subnets}}{{.Gateway}} {{end}}").Output()
		if err != nil {
			return "127.0.0.1"
		}
		for _, gw := range strings.Fields(string(out)) {
			if !strings.Contains(gw, ":") {
				return gw
			}
		}
		return "127.0.0.1"
	}
	return strings.TrimSpace(string(out))
}

// NetworkHasIPv6 reports whether the named podman network has at least one
// IPv6 subnet configured.
func NetworkHasIPv6(name string) bool {
	out, err := exec.Command(PodmanBin(), "network", "inspect", name,
		"--format", "{{range .Subnets}}{{.Subnet}} {{end}}").Output()
	if err != nil {
		return false
	}
	for _, subnet := range strings.Fields(string(out)) {
		if strings.Contains(subnet, ":") {
			return true
		}
	}
	return false
}

// EnsureNetwork creates the named podman network if it doesn't exist. The
// schema (v4-only vs dual-stack) follows HostHasUsableIPv6. Returns
// ErrNetworkNeedsMigration when an existing network's schema doesn't fit.
func EnsureNetwork(name string) error {
	out, err := Run("network", "ls", "--format={{.Name}}")
	if err != nil {
		return err
	}

	hostV6 := HostHasUsableIPv6()
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == name {
			netV6 := NetworkHasIPv6(name)
			if hostV6 != netV6 {
				return ErrNetworkNeedsMigration
			}
			if hostV6 && AardvarkNetworkDrifted(name) {
				return ErrNetworkNeedsMigration
			}
			return nil
		}
	}

	args := []string{"network", "create", "--driver", "bridge"}
	if hostV6 {
		args = append(args, "--ipv6", "--subnet", LerdULAv6Subnet)
	}
	args = append(args, "--opt", "mtu="+LerdNetworkMTU, name)
	return RunSilent(args...)
}

// aardvarkConfigPath returns the on-disk path to aardvark-dns's config file
// for the named network. Prefers XDG_RUNTIME_DIR; falls back to the rootless
// runtime dir convention /run/user/<uid>.
func aardvarkConfigPath(name string) string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "containers/networks/aardvark-dns", name)
	}
	return fmt.Sprintf("/run/user/%d/containers/networks/aardvark-dns/%s", os.Getuid(), name)
}

// aardvarkListenHasV6 reports whether the first line of an aardvark-dns
// config file contains a v6 address in its listen-ips field. First line
// format: "<listen-ip>[,<listen-ip>...] <forwarder-ip>...".
func aardvarkListenHasV6(firstLine string) bool {
	fields := strings.Fields(firstLine)
	if len(fields) == 0 {
		return false
	}
	for _, ip := range strings.Split(fields[0], ",") {
		if strings.Contains(ip, ":") {
			return true
		}
	}
	return false
}

// AardvarkNetworkDrifted returns true when the named network is dual-stack
// but aardvark-dns's on-disk listen line is v4-only, which stalls every
// lookup ~5s. Returns false when the config file is absent (fresh / macOS).
func AardvarkNetworkDrifted(name string) bool {
	if !NetworkHasIPv6(name) {
		return false
	}
	data, err := os.ReadFile(aardvarkConfigPath(name))
	if err != nil {
		return false
	}
	firstLine := data
	if i := strings.IndexByte(string(data), '\n'); i >= 0 {
		firstLine = data[:i]
	}
	return !aardvarkListenHasV6(string(firstLine))
}

// RemoveNetwork force-removes the podman network, wipes the aardvark-dns
// runtime file, and kills aardvark-dns so it respawns fresh against the
// new config when containers next join (fixes Fedora netavark's stale-inode).
func RemoveNetwork(name string) error {
	err := RunSilent("network", "rm", "--force", name)
	_ = os.Remove(aardvarkConfigPath(name))
	_ = exec.Command("pkill", "-f", "aardvark-dns").Run()
	return err
}

// RecreateNetwork destroys and recreates the named network with the schema
// that matches HostHasUsableIPv6. Returns the attached container names so
// the caller can StartUnit them, plus whether the new network is dual-stack.
func RecreateNetwork(name string) ([]string, bool, error) {
	dnsOut, err := Run("network", "inspect", name,
		"--format", "{{range .NetworkDNSServers}}{{.}} {{end}}")
	if err != nil {
		return nil, false, fmt.Errorf("inspect %s: %w", name, err)
	}
	prevDNS := strings.Fields(strings.TrimSpace(dnsOut))

	containersOut, err := Run("ps", "-a",
		"--filter", "network="+name,
		"--format", "{{.Names}}")
	if err != nil {
		return nil, false, fmt.Errorf("listing containers on %s: %w", name, err)
	}
	var attached []string
	for _, c := range strings.Split(containersOut, "\n") {
		if c = strings.TrimSpace(c); c != "" {
			attached = append(attached, c)
		}
	}

	for _, c := range attached {
		_ = RunSilent("stop", "--time", "10", c)
		_ = RunSilent("rm", "--force", c)
	}

	if err := RemoveNetwork(name); err != nil {
		return attached, false, fmt.Errorf("removing %s: %w", name, err)
	}

	hostV6 := HostHasUsableIPv6()
	args := []string{"network", "create", "--driver", "bridge"}
	if hostV6 {
		args = append(args, "--ipv6", "--subnet", LerdULAv6Subnet)
	}
	args = append(args, "--opt", "mtu="+LerdNetworkMTU, name)
	if err := RunSilent(args...); err != nil {
		return attached, hostV6, fmt.Errorf("recreating %s: %w", name, err)
	}

	for _, dns := range prevDNS {
		_ = RunSilent("network", "update", "--dns-add", dns, name)
	}

	return attached, hostV6, nil
}

// EnsureNetworkDNS syncs the DNS servers on the named network to the provided list.
// It drops servers no longer present and adds new ones. This sets the upstream
// forwarders that aardvark-dns uses, which is necessary on systems where
// /etc/resolv.conf points to a stub resolver (e.g. 127.0.0.53) that is not
// reachable from inside the container network namespace.
func EnsureNetworkDNS(name string, servers []string) error {
	if len(servers) == 0 {
		return nil
	}

	// Get current DNS servers on the network.
	out, err := Run("network", "inspect", name, "--format", "{{range .NetworkDNSServers}}{{.}} {{end}}")
	if err != nil {
		return err
	}

	current := map[string]bool{}
	for _, s := range strings.Fields(out) {
		current[s] = true
	}

	desired := map[string]bool{}
	for _, s := range servers {
		desired[s] = true
	}

	// Drop servers that are no longer desired.
	for s := range current {
		if !desired[s] {
			if err := RunSilent("network", "update", "--dns-drop", s, name); err != nil {
				return err
			}
		}
	}

	// Add servers that are not yet present.
	for s := range desired {
		if !current[s] {
			if err := RunSilent("network", "update", "--dns-add", s, name); err != nil {
				return err
			}
		}
	}

	return nil
}
