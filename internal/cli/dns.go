package cli

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dns"
	"github.com/geodro/lerd/internal/podman"
)

// lanExposureContainers is the canonical list of lerd containers whose
// PublishPort= bindings change between loopback and LAN modes.
//
// Only lerd-nginx is included on purpose: serving the sites is the whole
// point of lan:expose. The service containers (mysql, postgres, redis,
// meilisearch, rustfs, mailpit, etc.) intentionally stay bound to
// 127.0.0.1 in both modes — Laravel apps in lerd-php-fpm reach them via
// the podman bridge using container DNS names (DB_HOST=lerd-mysql, etc.),
// which is unaffected by the host bind. Exposing the database ports to
// the LAN by default would only matter for the rare "TablePlus from a
// second machine" use case, and would be a significant attack surface
// expansion on untrusted wifi. Power users who genuinely need that can
// SSH-tunnel or hand-edit a single quadlet.
//
// lerd-dns is also intentionally excluded: its publish is already pinned
// to 127.0.0.1:5300 in the embed (LAN access goes through the userspace
// lerd-dns-forwarder, not a publish flip), so regenerating its quadlet
// would be a no-op. EnableLANExposure restarts the lerd-dns unit
// separately to pick up the new dnsmasq target config.
var lanExposureContainers = []string{
	"lerd-nginx",
}

// LANProgressFunc is invoked by EnableLANExposure / DisableLANExposure
// after every meaningful step completes. The argument is a short
// human-readable label suitable for streaming to a frontend ("Rewriting
// container quadlets", "Restarting lerd-dns", "Done — LAN IP 192.168.x.y").
// May be nil; the no-progress path is the common case (CLI without
// streaming, internal idempotent re-application from `lerd remote-setup`).
type LANProgressFunc func(step string)

// EnableLANExposure flips lerd from the safe-on-coffee-shop-wifi default
// (everything bound to 127.0.0.1) to LAN-exposed mode. Concretely:
//
//   - persists cfg.LAN.Exposed=true so reinstalls and reboots restore the state
//   - regenerates every installed lerd-* container quadlet via WriteQuadlet,
//     which centrally rewrites PublishPort= lines to drop the loopback prefix
//   - daemon-reloads systemd and restarts each rewritten container
//   - rewrites the dnsmasq config to answer *.test queries with the host's
//     LAN IP and restarts lerd-dns
//   - installs and starts the userspace lerd-dns-forwarder.service that
//     bridges LAN-IP:5300 → 127.0.0.1:5300 (rootless pasta cannot accept
//     LAN-side traffic on its own, so a host-side forwarder is required)
//
// progress, if non-nil, is invoked after each step so the caller can
// stream feedback to a user (e.g. NDJSON over HTTP for the dashboard).
// Idempotent: safe to call repeatedly.
func EnableLANExposure(progress LANProgressFunc) (lanIP string, err error) {
	emit := func(step string) {
		if progress != nil {
			progress(step)
		}
	}

	emit("Saving LAN exposure flag")
	cfg, err := config.LoadGlobal()
	if err != nil {
		return "", fmt.Errorf("loading config: %w", err)
	}
	cfg.LAN.Exposed = true
	if err := config.SaveGlobal(cfg); err != nil {
		return "", fmt.Errorf("saving config: %w", err)
	}

	emit("Rewriting container quadlets")
	if err := regenerateLANContainerQuadlets(progress); err != nil {
		return "", err
	}

	emit("Detecting primary LAN IP")
	lanIP, err = detectPrimaryLANIP()
	if err != nil {
		return "", fmt.Errorf("could not auto-detect a LAN IP for the dnsmasq target: %w", err)
	}

	emit("Updating dnsmasq config (.test → " + lanIP + ")")
	if err := dns.WriteDnsmasqConfigFor(config.DnsmasqDir(), lanIP); err != nil {
		return "", fmt.Errorf("rewriting dnsmasq config: %w", err)
	}

	emit("Restarting lerd-dns")
	if err := reloadAndRestartUnit("lerd-dns"); err != nil {
		return "", err
	}

	emit("Installing lerd-dns-forwarder.service")
	if err := installDNSForwarderUnit(lanIP); err != nil {
		return "", fmt.Errorf("installing dns forwarder: %w", err)
	}

	emit("Starting lerd-dns-forwarder")
	if err := reloadAndRestartUnit("lerd-dns-forwarder"); err != nil {
		return "", fmt.Errorf("starting dns forwarder: %w", err)
	}

	emit("Done — lerd is reachable on " + lanIP)
	return lanIP, nil
}

// DisableLANExposure flips lerd back to the safe loopback default. Inverts
// EnableLANExposure: rewrites every container PublishPort to bind 127.0.0.1,
// stops the dns-forwarder, reverts dnsmasq to answer with 127.0.0.1, and
// revokes any outstanding remote-setup token (a code is only useful while
// the LAN forwarder is running). progress receives one event per step;
// pass nil for the silent path. Idempotent.
func DisableLANExposure(progress LANProgressFunc) error {
	emit := func(step string) {
		if progress != nil {
			progress(step)
		}
	}

	emit("Saving LAN exposure flag")
	cfg, err := config.LoadGlobal()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	cfg.LAN.Exposed = false
	if err := config.SaveGlobal(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	emit("Revoking outstanding remote-setup tokens")
	if err := ClearRemoteSetupToken(); err != nil {
		return fmt.Errorf("revoking remote-setup token: %w", err)
	}

	emit("Rewriting container quadlets")
	if err := regenerateLANContainerQuadlets(progress); err != nil {
		return err
	}

	emit("Stopping lerd-dns-forwarder")
	_ = exec.Command("systemctl", "--user", "stop", "lerd-dns-forwarder").Run()
	_ = exec.Command("systemctl", "--user", "disable", "lerd-dns-forwarder").Run()
	if err := os.Remove(dnsForwarderUnitPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing forwarder unit: %w", err)
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()

	emit("Reverting dnsmasq to 127.0.0.1")
	if err := dns.WriteDnsmasqConfigFor(config.DnsmasqDir(), "127.0.0.1"); err != nil {
		return fmt.Errorf("rewriting dnsmasq config: %w", err)
	}

	emit("Restarting lerd-dns")
	if err := reloadAndRestartUnit("lerd-dns"); err != nil {
		return err
	}

	emit("Done — lerd is loopback only")
	return nil
}

// regenerateLANContainerQuadlets re-reads each installed lerd-* container
// quadlet from the embed FS, runs it back through WriteQuadlet (which now
// applies BindForLAN based on cfg.LAN.Exposed), then daemon-reloads and
// restarts the running containers so the new PublishPort bindings take
// effect. Containers that aren't installed are skipped. progress, if
// non-nil, receives a per-container "Restarting <name>" event so callers
// streaming feedback can show finer-grained progress.
func regenerateLANContainerQuadlets(progress LANProgressFunc) error {
	restarted := []string{}
	for _, name := range lanExposureContainers {
		if !podman.QuadletInstalled(name) {
			continue
		}
		content, err := podman.GetQuadletTemplate(name + ".container")
		if err != nil {
			return fmt.Errorf("reading %s quadlet template: %w", name, err)
		}
		if err := podman.WriteContainerUnitFn(name, content); err != nil {
			return fmt.Errorf("rewriting %s quadlet: %w", name, err)
		}
		restarted = append(restarted, name)
	}

	if len(restarted) == 0 {
		return nil
	}

	if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("systemctl --user daemon-reload: %w", err)
	}
	for _, name := range restarted {
		if progress != nil {
			progress("Restarting " + name)
		}
		// Ignore individual container restart errors so a single dead
		// service doesn't block the rest of the toggle. The user will
		// see the bad state via `lerd doctor` / podman ps.
		_ = exec.Command("systemctl", "--user", "restart", name).Run()
	}
	// Nginx may have been renumbered by the restart, so refresh the
	// container-hosts file mapping .test domains to the live bridge IP.
	if err := podman.WriteContainerHosts(); err != nil && progress != nil {
		progress("[WARN] refreshing container hosts file: " + err.Error())
	}
	return nil
}

// dnsForwarderUnitPath returns the path to the systemd user unit file.
func dnsForwarderUnitPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "systemd", "user", "lerd-dns-forwarder.service")
}

// installDNSForwarderUnit writes the systemd user service that runs the
// `lerd dns-forwarder` daemon, listening on lanIP:5300 and forwarding to
// 127.0.0.1:5300. Idempotent — overwrites the existing file.
func installDNSForwarderUnit(lanIP string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		return err
	}
	binPath := filepath.Join(home, ".local", "bin", "lerd")
	content := fmt.Sprintf(`[Unit]
Description=Lerd DNS LAN Forwarder (rootless pasta workaround)
After=lerd-dns.service
Requires=lerd-dns.service

[Service]
ExecStart=%s dns-forwarder --listen %s:5300 --forward 127.0.0.1:5300
Restart=on-failure
RestartSec=2

[Install]
WantedBy=default.target
`, binPath, lanIP)
	if err := os.WriteFile(dnsForwarderUnitPath(), []byte(content), 0o644); err != nil {
		return err
	}
	_ = exec.Command("systemctl", "--user", "enable", "lerd-dns-forwarder").Run()
	return nil
}

// reloadAndRestartUnit runs `systemctl --user daemon-reload` followed by a
// restart of the given unit. Used by `lan:expose` / `lan:unexpose` after
// rewriting a quadlet or unit file so the new content takes effect.
func reloadAndRestartUnit(unit string) error {
	if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("systemctl --user daemon-reload: %w", err)
	}
	cmd := exec.Command("systemctl", "--user", "restart", unit)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemctl --user restart %s: %w", unit, err)
	}
	return nil
}

// detectPrimaryLANIP returns the host's primary LAN IPv4 address.
// The UDP-dial trick is tried first; if the result comes from a VPN tunnel
// (utun/tun/tap) we fall back to scanning physical interfaces.
func detectPrimaryLANIP() (string, error) {
	conn, err := net.Dial("udp4", "1.1.1.1:80")
	if err == nil {
		ip := conn.LocalAddr().(*net.UDPAddr).IP
		conn.Close()
		if name, ok := interfaceNameForIP(ip); ok && !isTunnelInterface(name) {
			return ip.String(), nil
		}
		// Fell through: the route goes through a VPN tunnel — keep scanning below.
	}

	ifaces, ifErr := net.Interfaces()
	if ifErr != nil {
		return "", fmt.Errorf("listing interfaces: %w", ifErr)
	}
	// First pass: physical interfaces only (en*, eth*, wlan*).
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if isTunnelInterface(iface.Name) || isContainerInterface(iface.Name) {
			continue
		}
		if ip := firstPrivateV4(iface); ip != "" {
			return ip, nil
		}
	}
	// Second pass: any non-tunnel interface as a last resort.
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if isTunnelInterface(iface.Name) {
			continue
		}
		if ip := firstPrivateV4(iface); ip != "" {
			return ip, nil
		}
	}
	return "", fmt.Errorf("no usable IPv4 address found")
}

// interfaceNameForIP returns the interface name that owns ip.
func interfaceNameForIP(ip net.IP) (string, bool) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", false
	}
	for _, iface := range ifaces {
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.Equal(ip) {
				return iface.Name, true
			}
		}
	}
	return "", false
}

// isTunnelInterface reports whether the interface looks like a VPN tunnel
// (macOS utun*, Linux tun*/tap*, WireGuard wg*, etc.).
func isTunnelInterface(name string) bool {
	for _, prefix := range []string{"utun", "tun", "tap", "wg", "ipsec", "ppp"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// isContainerInterface reports whether the interface belongs to a container network.
func isContainerInterface(name string) bool {
	for _, prefix := range []string{"docker", "podman", "veth", "bridge", "br-", "vf-", "vz-"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// firstPrivateV4 returns the first RFC-1918 IPv4 address on iface, or "".
func firstPrivateV4(iface net.Interface) string {
	addrs, _ := iface.Addrs()
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok {
			v4 := ipnet.IP.To4()
			if v4 != nil && !v4.IsLoopback() && isPrivateV4(v4) {
				return v4.String()
			}
		}
	}
	return ""
}

// isPrivateV4 reports whether ip is in an RFC-1918 private range.
func isPrivateV4(ip net.IP) bool {
	private := []struct{ net, mask [4]byte }{
		{[4]byte{10, 0, 0, 0}, [4]byte{255, 0, 0, 0}},
		{[4]byte{172, 16, 0, 0}, [4]byte{255, 240, 0, 0}},
		{[4]byte{192, 168, 0, 0}, [4]byte{255, 255, 0, 0}},
	}
	for _, p := range private {
		match := true
		for i := range 4 {
			if ip[i]&p.mask[i] != p.net[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
