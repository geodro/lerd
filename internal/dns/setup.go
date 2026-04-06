//go:build linux

package dns

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
)

const nmDnsConf = `[main]
dns=dnsmasq
`

const nmDnsmasqConf = `server=/test/127.0.0.1#5300
`

const resolvedDropin = `[Resolve]
DNS=127.0.0.1:5300
Domains=~test
`

// nmDispatcherScript is installed at /etc/NetworkManager/dispatcher.d/99-lerd-dns.
// On systems with NetworkManager + systemd-resolved, NM manages resolved via DBus and
// overrides global resolved.conf drop-ins. Per-interface DNS set via resolvectl is
// respected. We set two routing domains: ~test routes .test queries to lerd's dnsmasq,
// and ~. keeps the interface as the default route so all other DNS (internet) still works.
// The DHCP-assigned DNS servers are preserved alongside lerd's so internet continues
// to work even when lerd-dns is not yet running.
// When the network changes (LAN↔WiFi, switching networks), the script also rewrites
// the lerd dnsmasq config and restarts lerd-dns so the new upstream DNS is picked up
// immediately without requiring a manual lerd restart.
const nmDispatcherScript = `#!/bin/sh
# Lerd DNS: route .test queries through local dnsmasq on port 5300
IFACE="$1"
ACTION="$2"
LERD_DNS=""

if [ "$ACTION" = "up" ] || [ "$ACTION" = "dhcp4-change" ] || [ "$ACTION" = "dhcp6-change" ]; then
    LERD_DNS=$(nmcli -g IP4.DNS device show "$IFACE" 2>/dev/null | tr '|' '\n' | grep -v '^$' | tr '\n' ' ')
    resolvectl dns "$IFACE" 127.0.0.1:5300 $LERD_DNS 2>/dev/null || true
    resolvectl domain "$IFACE" ~test ~. 2>/dev/null || true
elif [ "$ACTION" = "down" ]; then
    # Interface went down: switch lerd-dns to the remaining default interface's DNS
    # so upstream resolution keeps working (e.g. closing wired while on WiFi).
    DEFAULT_IFACE=$(ip route show default 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="dev"){print $(i+1);exit}}')
    [ -n "$DEFAULT_IFACE" ] && [ "$DEFAULT_IFACE" != "$IFACE" ] || exit 0
    LERD_DNS=$(nmcli -g IP4.DNS device show "$DEFAULT_IFACE" 2>/dev/null | tr '|' '\n' | grep -v '^$' | tr '\n' ' ')
else
    exit 0
fi

[ -n "$LERD_DNS" ] || exit 0

# Sync lerd-dns dnsmasq config and restart for any user running it.
for uid_dir in /run/user/[0-9]*/; do
    [ -d "$uid_dir" ] || continue
    bus="${uid_dir}bus"
    [ -S "$bus" ] || continue
    XDG_RUNTIME_DIR="$uid_dir" DBUS_SESSION_BUS_ADDRESS="unix:path=$bus" \
        systemctl --user is-active lerd-dns >/dev/null 2>&1 || continue
    uid=$(basename "$uid_dir")
    home=$(getent passwd "$uid" | cut -d: -f6)
    config_file="$home/.local/share/lerd/dnsmasq/lerd.conf"
    [ -f "$config_file" ] || continue
    tld=$(grep 'tld:' "$home/.config/lerd/config.yaml" 2>/dev/null | sed 's/.*tld:[[:space:]]*//' | sed 's/[^a-zA-Z0-9._-]//g' | head -1)
    tld=${tld:-test}
    printf '# Lerd DNS configuration\nport=5300\nno-resolv\n' > "$config_file"
    for dns_ip in $LERD_DNS; do
        printf 'server=%s\n' "$dns_ip" >> "$config_file"
    done
    printf 'address=/.%s/127.0.0.1\n' "$tld" >> "$config_file"
    XDG_RUNTIME_DIR="$uid_dir" DBUS_SESSION_BUS_ADDRESS="unix:path=$bus" \
        systemctl --user restart lerd-dns 2>/dev/null || true
done
`

// isSystemdResolvedActive returns true if systemd-resolved is the active DNS resolver.
var isSystemdResolvedActive = func() bool {
	cmd := exec.Command("systemctl", "is-active", "--quiet", "systemd-resolved")
	if err := cmd.Run(); err != nil {
		return false
	}
	// Also check that /etc/resolv.conf points to the stub resolver
	data, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "127.0.0.53") || strings.Contains(string(data), "systemd-resolved")
}

// isNetworkManagerActive returns true if NetworkManager is running.
var isNetworkManagerActive = func() bool {
	cmd := exec.Command("systemctl", "is-active", "--quiet", "NetworkManager")
	return cmd.Run() == nil
}

// ResolverHint returns a user-facing hint for restarting the active DNS resolver.
func ResolverHint() string {
	if isNetworkManagerActive() {
		return "sudo systemctl restart NetworkManager"
	}
	if isSystemdResolvedActive() {
		return "sudo systemctl restart systemd-resolved"
	}
	return "restart your DNS resolver"
}

// lerdDNSInterfaces returns all network interfaces that currently have
// 127.0.0.1:5300 configured as a DNS server (set by the lerd dispatcher).
func lerdDNSInterfaces() []string {
	out, err := exec.Command("resolvectl", "status").Output()
	if err != nil {
		// Fallback to just the default interface.
		if iface := defaultInterface(); iface != "" {
			return []string{iface}
		}
		return nil
	}
	return parseLerdDNSInterfaces(string(out))
}

// parseLerdDNSInterfaces extracts interface names from resolvectl status output
// that have 127.0.0.1:5300 configured as a DNS server.
func parseLerdDNSInterfaces(output string) []string {
	var ifaces []string
	var currentIface string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Link ") {
			if start := strings.Index(line, "("); start >= 0 {
				if end := strings.Index(line, ")"); end > start {
					currentIface = line[start+1 : end]
				}
			}
		}
		if currentIface != "" && strings.Contains(line, "127.0.0.1:5300") {
			ifaces = append(ifaces, currentIface)
			currentIface = ""
		}
	}
	return ifaces
}

// defaultInterface returns the name of the default network interface (e.g. "enp1s0").
func defaultInterface() string {
	out, err := exec.Command("ip", "route", "show", "default").Output()
	if err != nil {
		return ""
	}
	return parseDefaultIface(string(out))
}

// parseDefaultIface extracts the interface name from `ip route show default` output.
func parseDefaultIface(output string) string {
	// "default via 192.168.1.1 dev enp1s0 ..."
	parts := strings.Fields(output)
	for i, p := range parts {
		if p == "dev" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// resolvPaths is the ordered list of resolv.conf files to try for upstream DNS detection.
// Overridable in tests.
var resolvPaths = []string{"/run/systemd/resolve/resolv.conf", "/etc/resolv.conf"}

// nmcliDNSFunc is the function used to get DHCP DNS via nmcli. Overridable in tests.
var nmcliDNSFunc = func() []string {
	out, err := exec.Command("nmcli", "-g", "IP4.DNS", "device", "show").Output()
	if err != nil {
		return nil
	}
	return parseNmcliLines(string(out))
}

// ReadContainerDNS returns the DNS servers to configure as aardvark-dns upstreams
// for the lerd Podman bridge network. It reads DnsForwardIps from the pasta rootless-netns
// info.json (typically 169.254.1.1), which chains through systemd-resolved and therefore
// resolves both .test domains (via lerd-dns) and internet domains. Falls back to
// ReadUpstreamDNS if the file is unavailable (e.g. before Podman initialises the netns).
func ReadContainerDNS() []string {
	path := fmt.Sprintf("/run/user/%d/containers/networks/rootless-netns/info.json", os.Getuid())
	data, err := os.ReadFile(path)
	if err != nil {
		return readUpstreamDNS()
	}
	var info struct {
		DnsForwardIps []string `json:"DnsForwardIps"`
	}
	if err := json.Unmarshal(data, &info); err != nil || len(info.DnsForwardIps) == 0 {
		return readUpstreamDNS()
	}
	return info.DnsForwardIps
}

// ReadUpstreamDNS returns upstream DNS server IPs from the running system.
// Sources tried in order:
//  1. /run/systemd/resolve/resolv.conf — real upstreams on systemd-resolved systems
//  2. /etc/resolv.conf — fallback
//  3. nmcli — DHCP-provided DNS from NetworkManager
//
// Returns nil if nothing is found; callers should omit no-resolv in that case.
func ReadUpstreamDNS() []string {
	return readUpstreamDNS()
}

// readUpstreamDNS is the internal implementation.
func readUpstreamDNS() []string {
	for _, path := range resolvPaths {
		if servers := parseNameservers(path); len(servers) > 0 {
			return servers
		}
	}
	return nmcliDNSFunc()
}

// nmcliDNS reads DHCP-assigned DNS servers from NetworkManager via nmcli.
func nmcliDNS() []string {
	return nmcliDNSFunc()
}

// parseNmcliLines parses the output of `nmcli -g IP4.DNS device show`.
func parseNmcliLines(output string) []string {
	var servers []string
	seen := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		// nmcli may separate multiple values with |
		for _, ip := range strings.Split(line, "|") {
			ip = strings.TrimSpace(ip)
			if ip == "" || ip == "--" || ip == "127.0.0.1" || ip == "127.0.0.53" || ip == "::1" {
				continue
			}
			if !seen[ip] {
				seen[ip] = true
				servers = append(servers, ip)
			}
		}
	}
	return servers
}

// Setup writes DNS configuration for .test resolution and restarts the resolver.
// On systemd-resolved + NetworkManager systems (Ubuntu etc.) it uses an NM dispatcher script.
// On pure systemd-resolved systems it uses a resolved drop-in.
// On NetworkManager-only systems it uses NM's embedded dnsmasq.
//
// Deprecated: prefer calling WriteDnsmasqConfig then ConfigureResolver separately so
// that the dnsmasq container can be started between the two steps.
func Setup() error {
	if err := WriteDnsmasqConfig(config.DnsmasqDir()); err != nil {
		return fmt.Errorf("writing lerd dnsmasq config: %w", err)
	}
	return ConfigureResolver()
}

// ConfigureResolver configures the system DNS resolver to forward .test to the
// lerd-dns dnsmasq container on port 5300. Call this after lerd-dns is running so
// that any immediate resolvectl changes don't break DNS before dnsmasq is up.
func ConfigureResolver() error {
	if isSystemdResolvedActive() {
		if isNetworkManagerActive() {
			return setupNMWithResolved()
		}
		return setupSystemdResolved()
	}
	return setupNetworkManager()
}

// setupNMWithResolved handles Ubuntu-style: NM manages systemd-resolved via DBUS.
// NM overrides global DNS set in resolved.conf drop-ins, so we use an NM dispatcher
// script that applies per-interface DNS via resolvectl on each "up" event, then
// applies it immediately to the current default interface.
func setupNMWithResolved() error {
	dispatcherScript := "/etc/NetworkManager/dispatcher.d/99-lerd-dns"

	if !isFileContent(dispatcherScript, []byte(nmDispatcherScript)) {
		fmt.Println("  [sudo required] Configuring NetworkManager dispatcher for .test DNS resolution")

		if err := sudoWriteFile(dispatcherScript, []byte(nmDispatcherScript), 0755); err != nil {
			return fmt.Errorf("writing NM dispatcher script: %w", err)
		}
	}

	// Remove stale resolved drop-in if present (it doesn't work with NM)
	dropin := "/etc/systemd/resolved.conf.d/lerd.conf"
	if _, err := os.Stat(dropin); err == nil {
		rmCmd := exec.Command("sudo", "rm", "-f", dropin)
		rmCmd.Stdin = os.Stdin
		rmCmd.Stdout = os.Stdout
		rmCmd.Stderr = os.Stderr
		rmCmd.Run() //nolint:errcheck
	}

	// Apply immediately to the current default interface.
	// Include DHCP-assigned upstream DNS servers alongside lerd's so internet
	// continues to work even when lerd-dns is not running.
	iface := defaultInterface()
	if iface == "" {
		return nil
	}

	// Revert the interface to clear any stale DNS server failure state from boot.
	// At boot, the NM dispatcher sets 127.0.0.1:5300 before lerd-dns starts; resolved
	// marks it failed and promotes the fallback to "current". Calling resolvectl with
	// the same list later does not reset the current server. Reverting first forces a
	// clean slate so our subsequent dns call starts with 127.0.0.1:5300 as current.
	revertCmd := exec.Command("sudo", "resolvectl", "revert", iface)
	revertCmd.Stdin = os.Stdin
	revertCmd.Stdout = os.Stdout
	revertCmd.Stderr = os.Stderr
	revertCmd.Run() //nolint:errcheck

	dnsArgs := []string{"sudo", "resolvectl", "dns", iface, "127.0.0.1:5300"}
	dnsArgs = append(dnsArgs, readUpstreamDNS()...)
	dnsCmd := exec.Command(dnsArgs[0], dnsArgs[1:]...)
	dnsCmd.Stdin = os.Stdin
	dnsCmd.Stdout = os.Stdout
	dnsCmd.Stderr = os.Stderr
	if err := dnsCmd.Run(); err != nil {
		return fmt.Errorf("applying DNS to %s: %w", iface, err)
	}

	domainCmd := exec.Command("sudo", "resolvectl", "domain", iface, "~test", "~.")
	domainCmd.Stdin = os.Stdin
	domainCmd.Stdout = os.Stdout
	domainCmd.Stderr = os.Stderr
	if err := domainCmd.Run(); err != nil {
		return fmt.Errorf("applying domain routing to %s: %w", iface, err)
	}

	// Keep dnsmasq config in sync with the upstream DNS servers now active on
	// the interface. resolvectl has just updated systemd-resolved, so
	// readUpstreamDNS() will return the current (post-change) upstreams.
	// Restart lerd-dns only when the config actually changes to avoid
	// unnecessary downtime on normal starts where nothing has changed.
	existing, _ := os.ReadFile(filepath.Join(config.DnsmasqDir(), "lerd.conf"))
	if err := WriteDnsmasqConfig(config.DnsmasqDir()); err == nil {
		updated, _ := os.ReadFile(filepath.Join(config.DnsmasqDir(), "lerd.conf"))
		if string(existing) != string(updated) {
			exec.Command("systemctl", "--user", "restart", "lerd-dns").Run() //nolint:errcheck
		}
	}

	return nil
}

// setupSystemdResolved configures systemd-resolved to forward .test to port 5300.
// Used only when systemd-resolved is active without NetworkManager managing it.
func setupSystemdResolved() error {
	dropin := "/etc/systemd/resolved.conf.d/lerd.conf"

	if isFileContent(dropin, []byte(resolvedDropin)) {
		if info, err := os.Stat(dropin); err == nil && info.Mode().Perm() == 0644 {
			return nil
		}
	}

	fmt.Println("  [sudo required] Configuring systemd-resolved for .test DNS resolution")

	if err := sudoWriteFile(dropin, []byte(resolvedDropin), 0644); err != nil {
		return fmt.Errorf("writing resolved drop-in: %w", err)
	}

	cmd := exec.Command("sudo", "systemctl", "restart", "systemd-resolved")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("restarting systemd-resolved: %w", err)
	}
	return nil
}

// setupNetworkManager configures NetworkManager's embedded dnsmasq.
func setupNetworkManager() error {
	nmConfFile := "/etc/NetworkManager/conf.d/lerd.conf"
	nmDnsmasqFile := "/etc/NetworkManager/dnsmasq.d/lerd.conf"

	if isFileContent(nmConfFile, []byte(nmDnsConf)) && isFileContent(nmDnsmasqFile, []byte(nmDnsmasqConf)) {
		return nil
	}

	fmt.Println("  [sudo required] Configuring NetworkManager for .test DNS resolution")

	if err := sudoWriteFile(nmConfFile, []byte(nmDnsConf), 0644); err != nil {
		return fmt.Errorf("writing NetworkManager conf: %w", err)
	}

	if err := sudoWriteFile(nmDnsmasqFile, []byte(nmDnsmasqConf), 0644); err != nil {
		return fmt.Errorf("writing NetworkManager dnsmasq conf: %w", err)
	}

	cmd := exec.Command("sudo", "systemctl", "restart", "NetworkManager")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("restarting NetworkManager: %w", err)
	}
	return nil
}

// Teardown removes all lerd DNS configuration from the system and restores normal resolution.
func Teardown() {
	// NM dispatcher script
	dispatcherScript := "/etc/NetworkManager/dispatcher.d/99-lerd-dns"
	if _, err := os.Stat(dispatcherScript); err == nil {
		rmCmd := exec.Command("sudo", "rm", "-f", dispatcherScript)
		rmCmd.Stdin = os.Stdin
		rmCmd.Stdout = os.Stdout
		rmCmd.Stderr = os.Stderr
		rmCmd.Run() //nolint:errcheck
	}

	// systemd-resolved drop-in
	dropin := "/etc/systemd/resolved.conf.d/lerd.conf"
	if _, err := os.Stat(dropin); err == nil {
		rmCmd := exec.Command("sudo", "rm", "-f", dropin)
		rmCmd.Stdin = os.Stdin
		rmCmd.Stdout = os.Stdout
		rmCmd.Stderr = os.Stderr
		rmCmd.Run() //nolint:errcheck
	}

	// NetworkManager conf and dnsmasq conf
	for _, f := range []string{
		"/etc/NetworkManager/conf.d/lerd.conf",
		"/etc/NetworkManager/dnsmasq.d/lerd.conf",
	} {
		if _, err := os.Stat(f); err == nil {
			rmCmd := exec.Command("sudo", "rm", "-f", f)
			rmCmd.Stdin = os.Stdin
			rmCmd.Stdout = os.Stdout
			rmCmd.Stderr = os.Stderr
			rmCmd.Run() //nolint:errcheck
		}
	}

	// Revert ALL interfaces that have lerd DNS (127.0.0.1:5300) configured.
	// The dispatcher script applies DNS to every interface on "up", not just
	// the default one, so reverting only the default leaves virtual bridges
	// (virbr0, vnet*) pointing at the dead dnsmasq port.
	for _, iface := range lerdDNSInterfaces() {
		revertCmd := exec.Command("sudo", "resolvectl", "revert", iface)
		revertCmd.Stdin = os.Stdin
		revertCmd.Stdout = os.Stdout
		revertCmd.Stderr = os.Stderr
		revertCmd.Run() //nolint:errcheck
	}

	// Restart the resolver to apply the removal and re-establish upstream DNS.
	if isNetworkManagerActive() {
		fmt.Println("  Restarting NetworkManager (may take a moment)...")
		cmd := exec.Command("sudo", "systemctl", "restart", "NetworkManager")
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Run() //nolint:errcheck

		// NM restart doesn't always re-push DHCP DNS to resolved after a
		// resolvectl revert. Explicitly apply the DHCP-assigned servers so
		// internet DNS works immediately after uninstall.
		if iface := defaultInterface(); iface != "" {
			upstreams := nmcliDNSFunc()
			if len(upstreams) > 0 {
				args := append([]string{"sudo", "resolvectl", "dns", iface}, upstreams...)
				pushCmd := exec.Command(args[0], args[1:]...)
				pushCmd.Stdin = os.Stdin
				pushCmd.Stdout = os.Stdout
				pushCmd.Stderr = os.Stderr
				pushCmd.Run() //nolint:errcheck
			}
		}
	} else if isSystemdResolvedActive() {
		exec.Command("sudo", "systemctl", "restart", "systemd-resolved").Run() //nolint:errcheck
	}
}

// InstallSudoers writes a sudoers drop-in granting the current user passwordless
// access to resolvectl commands. This is required for the autostart service which
// runs non-interactively and cannot prompt for a sudo password.
func InstallSudoers() error {
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("LOGNAME")
	}
	if user == "" {
		return fmt.Errorf("cannot determine current user")
	}

	content := fmt.Sprintf(
		"# Lerd: allow resolvectl and NM dispatcher script install without password\n"+
			"%s ALL=(root) NOPASSWD: /usr/bin/resolvectl dns *, /usr/bin/resolvectl domain *, /usr/bin/resolvectl revert *\n"+
			"%s ALL=(root) NOPASSWD: /usr/bin/mkdir -p /etc/NetworkManager/dispatcher.d\n"+
			"%s ALL=(root) NOPASSWD: /usr/bin/cp /tmp/lerd-sudo-* /etc/NetworkManager/dispatcher.d/99-lerd-dns\n"+
			"%s ALL=(root) NOPASSWD: /usr/bin/chmod 755 /etc/NetworkManager/dispatcher.d/99-lerd-dns\n"+
			"%s ALL=(root) NOPASSWD: /usr/bin/mkdir -p /etc/systemd/resolved.conf.d\n"+
			"%s ALL=(root) NOPASSWD: /usr/bin/cp /tmp/lerd-sudo-* /etc/systemd/resolved.conf.d/lerd.conf\n"+
			"%s ALL=(root) NOPASSWD: /usr/bin/chmod 644 /etc/systemd/resolved.conf.d/lerd.conf\n"+
			"%s ALL=(root) NOPASSWD: /usr/bin/systemctl restart systemd-resolved\n",
		user, user, user, user, user, user, user, user,
	)

	const sudoersPath = "/etc/sudoers.d/lerd"
	if isFileContent(sudoersPath, []byte(content)) {
		return nil
	}

	if err := sudoWriteFile(sudoersPath, []byte(content), 0440); err != nil {
		return fmt.Errorf("writing sudoers drop-in: %w", err)
	}
	return nil
}

