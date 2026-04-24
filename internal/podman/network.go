package podman

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
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

	// The probe-failed marker doubles as the user opt-out: install --no-ipv6
	// (or LERD_DISABLE_IPV6=1) writes it before calling EnsureNetwork, so
	// dual-stack is suppressed on every code path below without a second flag.
	hostV6 := HostHasUsableIPv6() && !ipv6ProbeFailed(name)
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == name {
			netV6 := NetworkHasIPv6(name)
			if netV6 && !hostV6 {
				// Network has IPv6 but host no longer does, or user
				// opted out: strip it.
				return ErrNetworkNeedsMigration
			}
			if hostV6 && !netV6 {
				// Host gained IPv6 and no previous probe failure or
				// opt-out: try upgrading.
				return ErrNetworkNeedsMigration
			}
			if hostV6 && netV6 && AardvarkNetworkDrifted(name) {
				return ErrNetworkNeedsMigration
			}
			return nil
		}
	}

	_, err = createNetworkWithProbe(name, hostV6)
	return err
}

// ipv6ProbeFailedPath returns the marker file path that records a failed
// IPv6 probe so we don't retry the dual-stack migration on every install.
func ipv6ProbeFailedPath(networkName string) string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "lerd", "ipv6-probe-failed-"+networkName)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local/share/lerd", "ipv6-probe-failed-"+networkName)
}

// ipv6ProbeFailed reports whether a previous IPv6 probe failed for the
// named network.
func ipv6ProbeFailed(name string) bool {
	_, err := os.Stat(ipv6ProbeFailedPath(name))
	return err == nil
}

// markIPv6ProbeFailed writes a marker file to prevent retrying the
// dual-stack migration on subsequent installs.
func markIPv6ProbeFailed(name string) {
	path := ipv6ProbeFailedPath(name)
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	_ = os.WriteFile(path, []byte("probe failed\n"), 0644)
}

// clearIPv6ProbeFailed removes the marker so a future install retries.
func clearIPv6ProbeFailed(name string) {
	_ = os.Remove(ipv6ProbeFailedPath(name))
}

// MarkIPv6Disabled persists the user's opt-out (--no-ipv6 /
// LERD_DISABLE_IPV6=1) using the same marker file as a probe failure.
// EnsureNetwork honors it on every code path so dual-stack stays off
// across installs until the user removes the marker.
func MarkIPv6Disabled(name string) {
	markIPv6ProbeFailed(name)
}

// IPv6DisabledMarkerPath returns the on-disk path of the opt-out marker
// so callers (and the timeout-fallback warning) can show users exactly
// what to delete to retry dual-stack.
func IPv6DisabledMarkerPath(name string) string {
	return ipv6ProbeFailedPath(name)
}

// createNetworkWithProbe creates the podman network. When dualStack is true it
// first tries a dual-stack network and runs a throw-away container to verify
// aardvark-dns can bind the IPv6 gateway. If the probe fails, the network is
// torn down and recreated as v4-only. Returns the actual dual-stack state.
func createNetworkWithProbe(name string, dualStack bool) (bool, error) {
	args := []string{"network", "create", "--driver", "bridge"}
	if dualStack {
		args = append(args, "--ipv6", "--subnet", LerdULAv6Subnet)
	}
	args = append(args, "--opt", "mtu="+LerdNetworkMTU, name)
	if err := RunSilent(args...); err != nil {
		return dualStack, err
	}

	if dualStack {
		ok, timedOut := probeNetworkIPv6(name)
		if !ok {
			markIPv6ProbeFailed(name)
			if timedOut {
				fmt.Fprintf(os.Stderr,
					"    [WARN] IPv6 probe timed out after %s; falling back to v4-only.\n"+
						"    To retry dual-stack later, delete %s and re-run `lerd install`.\n",
					probeNetworkIPv6Timeout, IPv6DisabledMarkerPath(name))
			}
			// Systemd may have auto-restarted containers on this network
			// between RecreateNetwork and the probe. Stop and remove them
			// before tearing down the network.
			if out, err := Run("ps", "-a", "--filter", "network="+name, "--format", "{{.Names}}"); err == nil {
				for _, c := range strings.Split(out, "\n") {
					if c = strings.TrimSpace(c); c != "" {
						_ = StopUnit(c)
						_ = RunSilent("rm", "--force", c)
					}
				}
			}
			_ = RemoveNetwork(name)
			return createNetworkWithProbe(name, false)
		}
		clearIPv6ProbeFailed(name)
	}
	return dualStack, nil
}

// probeNetworkIPv6Timeout caps how long the throw-away container is allowed
// to run before we treat the probe as inconclusive. A hung podman or netns
// setup must not block install/recreate forever.
const probeNetworkIPv6Timeout = 30 * time.Second

// probeNetworkIPv6 starts a throw-away container on the named network to
// verify aardvark-dns can bind the IPv6 gateway. Returns ok=true when the
// container starts (or when the probe is inconclusive, e.g. missing image).
// Returns ok=false on aardvark-dns bind failures or when the probe times
// out, so the caller falls back to v4-only in both cases. timedOut is set
// when the deadline elapsed, so the caller can surface a recovery hint.
func probeNetworkIPv6(name string) (ok, timedOut bool) {
	ctx, cancel := context.WithTimeout(context.Background(), probeNetworkIPv6Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, PodmanBin(), "run", "--rm", "--network", name,
		"--pull", "never", "alpine:latest", "true")
	out, err := cmd.CombinedOutput()
	if err == nil {
		return true, false
	}
	if ctx.Err() == context.DeadlineExceeded {
		return false, true
	}
	s := string(out)
	bindFailure := strings.Contains(s, "aardvark-dns") ||
		strings.Contains(s, "Cannot assign requested address")
	return !bindFailure, false
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
		_ = StopUnit(c)
		_ = RunSilent("rm", "--force", c)
	}

	if err := RemoveNetwork(name); err != nil {
		return attached, false, fmt.Errorf("removing %s: %w", name, err)
	}

	hostV6 := HostHasUsableIPv6() && !ipv6ProbeFailed(name)
	actualV6, err := createNetworkWithProbe(name, hostV6)
	if err != nil {
		return attached, actualV6, fmt.Errorf("recreating %s: %w", name, err)
	}

	for _, dns := range prevDNS {
		_ = RunSilent("network", "update", "--dns-add", dns, name)
	}

	return attached, actualV6, nil
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
