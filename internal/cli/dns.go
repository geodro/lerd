package cli

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dns"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
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

	if cfg.DNS.Enabled {
		emit("Rewriting container quadlets")
		if err := regenerateLANContainerQuadlets(progress); err != nil {
			return "", err
		}
	}

	emit("Detecting primary LAN IP")
	lanIP, err = detectPrimaryLANIP()
	if err != nil {
		return "", fmt.Errorf("could not auto-detect a LAN IP for the dnsmasq target: %w", err)
	}

	if cfg.DNS.Enabled {
		emit("Updating dnsmasq config (.test → " + lanIP + ")")
		if err := dns.WriteDnsmasqConfigFor(config.DnsmasqDir(), lanIP); err != nil {
			return "", fmt.Errorf("rewriting dnsmasq config: %w", err)
		}

		emit("Restarting lerd-dns")
		if err := reloadAndRestartUnit("lerd-dns"); err != nil {
			return "", err
		}

		if err := ensureLANForwarder(lanIP, emit); err != nil {
			return "", err
		}
	}

	emit("Done — lerd is reachable on " + lanIP)
	return lanIP, nil
}

// ensureLANForwarder installs the host-side LAN DNS forwarder when the platform
// needs it. The forwarder is the Linux rootless-pasta workaround: there the
// lerd-dns container cannot bind the host LAN address, so a forwarder bridges
// lanIP:5300 → 127.0.0.1:5300. On macOS lerd-dns is a host dnsmasq that already
// binds all interfaces (including lanIP), so installing a forwarder would
// double-bind lanIP:5300 and crash lerd-dns; there this is a no-op.
func ensureLANForwarder(lanIP string, emit func(string)) error {
	if lerdDNSBindsLANPort {
		if emit != nil {
			emit("lerd-dns binds the LAN address directly; no forwarder needed")
		}
		return nil
	}
	return installLANForwarderFn(lanIP, emit)
}

// installAndStartForwarder runs the preflight, installs the lerd-dns-forwarder
// unit, and starts it. Default implementation behind installLANForwarderFn.
func installAndStartForwarder(lanIP string, emit func(string)) error {
	if err := preflightForwarderPort(lanIP, emit); err != nil {
		return err
	}
	if emit != nil {
		emit("Installing lerd-dns-forwarder.service")
	}
	if err := installDNSForwarderUnit(lanIP); err != nil {
		return fmt.Errorf("installing dns forwarder: %w", err)
	}
	if emit != nil {
		emit("Starting lerd-dns-forwarder")
	}
	if err := reloadAndRestartUnit("lerd-dns-forwarder"); err != nil {
		return fmt.Errorf("starting dns forwarder: %w", err)
	}
	return nil
}

// ensureLANForwarderRemoved tears down the LAN DNS forwarder, the mirror of
// ensureLANForwarder. Unlike the install side it intentionally runs on every
// platform: macOS no longer installs a forwarder, but a Mac upgrading from an
// older build (which did) still needs unexpose to clear that stale unit, or it
// keeps holding lanIP:5300 and breaks .test resolution. Removing a missing unit
// is ignored, so this is a safe no-op when no forwarder is present.
func ensureLANForwarderRemoved(emit func(string)) error {
	if emit != nil {
		emit("Stopping lerd-dns-forwarder")
	}
	_ = services.Mgr.Stop("lerd-dns-forwarder")
	_ = services.Mgr.Disable("lerd-dns-forwarder")
	if err := services.Mgr.RemoveServiceUnit("lerd-dns-forwarder"); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing forwarder unit: %w", err)
	}
	_ = services.Mgr.DaemonReload()

	return nil
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

	if cfg.DNS.Enabled {
		emit("Rewriting container quadlets")
		if err := regenerateLANContainerQuadlets(progress); err != nil {
			return err
		}
	}

	if cfg.DNS.Enabled {
		if err := ensureLANForwarderRemoved(emit); err != nil {
			return err
		}

		emit("Reverting dnsmasq to 127.0.0.1")
		if err := dns.WriteDnsmasqConfigFor(config.DnsmasqDir(), "127.0.0.1"); err != nil {
			return fmt.Errorf("rewriting dnsmasq config: %w", err)
		}

		emit("Restarting lerd-dns")
		if err := reloadAndRestartUnit("lerd-dns"); err != nil {
			return err
		}
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

	if err := services.Mgr.DaemonReload(); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}
	for _, name := range restarted {
		if progress != nil {
			progress("Restarting " + name)
		}
		// Ignore individual container restart errors so a single dead
		// service doesn't block the rest of the toggle. The user will
		// see the bad state via `lerd doctor` / podman ps.
		_ = services.Mgr.Restart(name)
	}
	return nil
}

// Seams for preflightForwarderPort so the logic can be unit-tested
// without binding real ports, spawning lsof, or depending on services.Mgr.
var (
	forwarderUnitStatusFn = func() string {
		s, _ := services.Mgr.UnitStatus("lerd-dns-forwarder")
		return s
	}
	// lerdDNSBindsLANPort is true on platforms where the main lerd-dns daemon
	// binds lanIP:5300 itself (macOS host dnsmasq) instead of via the separate
	// lerd-dns-forwarder (the Linux rootless-pasta workaround). On such
	// platforms LAN exposure must NOT install the forwarder: it would
	// double-bind lanIP:5300 and crash lerd-dns. Seam so both models are
	// testable from any build host.
	lerdDNSBindsLANPort = runtime.GOOS == "darwin"
	// installLANForwarderFn installs and starts the host-side LAN DNS
	// forwarder. Seam so the macOS skip can be asserted without touching real
	// units.
	installLANForwarderFn = installAndStartForwarder
	forwarderPortFreeFn   = forwarderPortFree
	forwarderPortHolderFn = forwarderPortHolderLsof
)

// forwarderPort is the host port lerd-dns-forwarder listens on.
const forwarderPort = 5300

// preflightForwarderPort refuses to install the LAN DNS forwarder when
// something else (typically a legacy host-side dnsmasq) already owns
// lanIP:5300. Without this check the launchd plist would write fine,
// then the daemon would race against the existing holder on every boot.
// Skipped when our own forwarder is already active or activating: that's
// the re-run-of-lan-expose case where the existing unit will be replaced.
// emit may be nil; when set it receives one user-visible progress line
// describing the path taken.
func preflightForwarderPort(lanIP string, emit func(string)) error {
	if s := forwarderUnitStatusFn(); s == "active" || s == "activating" || s == "deactivating" {
		if emit != nil {
			emit("Pre-flight: lerd-dns-forwarder is " + s + "; skipping port check")
		}
		return nil
	}
	if emit != nil {
		emit(fmt.Sprintf("Pre-flight: checking %s:%d is free", lanIP, forwarderPort))
	}
	if forwarderPortFreeFn(lanIP, forwarderPort) {
		return nil
	}
	holder := forwarderPortHolderFn(lanIP, forwarderPort)
	return fmt.Errorf("%s:%d is already in use; lerd cannot install the LAN DNS forwarder.\n%s\nStop the conflicting service (or rebind it off port %d) and re-run `lerd lan expose`", lanIP, forwarderPort, holder, forwarderPort)
}

// forwarderPortFree returns true when both UDP and TCP on host:port are
// free to bind. Best-effort: a process using SO_REUSEPORT can share a
// bound port with our probe and slip through; covers the common case of
// a long-running dnsmasq holding the address.
func forwarderPortFree(host string, port int) bool {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	if conn, err := net.ListenPacket("udp", addr); err == nil {
		conn.Close()
	} else {
		return false
	}
	if l, err := net.Listen("tcp", addr); err == nil {
		l.Close()
	} else {
		return false
	}
	return true
}

// forwarderPortHolderLsof shells out to lsof to identify the conflicting
// process. Uses stdout only so transient lsof stderr warnings don't
// leak into the user-facing error. Falls back to a per-platform hint
// when lsof is missing or returns nothing.
func forwarderPortHolderLsof(host string, port int) string {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	cmd := exec.Command("lsof", "-nP", "-iUDP@"+addr, "-iTCP@"+addr)
	out, err := cmd.Output()
	trimmed := strings.TrimSpace(string(out))
	if err == nil && trimmed != "" {
		var lines []string
		for _, line := range strings.Split(trimmed, "\n") {
			lines = append(lines, "  "+line)
		}
		return strings.Join(lines, "\n")
	}
	return forwarderHolderFallbackHint(runtime.GOOS, port)
}

// forwarderHolderFallbackHint returns the per-OS suggestion shown when
// lsof can't identify the conflicting process. Factored out so both
// branches can be unit-tested from any build host.
func forwarderHolderFallbackHint(goos string, port int) string {
	if goos == "linux" {
		return fmt.Sprintf("  (could not identify the holder; try: sudo ss -tulpn | grep ':%d')", port)
	}
	return fmt.Sprintf("  (could not identify the holder; try: sudo lsof -nP -i :%d)", port)
}

// installDNSForwarderUnit writes the user service that runs the
// `lerd dns-forwarder` daemon, listening on lanIP:5300 and forwarding to
// 127.0.0.1:5300. Routes through services.Mgr so the unit content is
// rendered as a systemd .service on Linux and a launchd plist on macOS
// (see services/launchd_darwin.go::parseServiceUnit). Idempotent.
func installDNSForwarderUnit(lanIP string) error {
	home, err := os.UserHomeDir()
	if err != nil {
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
	if err := services.Mgr.WriteServiceUnit("lerd-dns-forwarder", content); err != nil {
		return err
	}
	_ = services.Mgr.Enable("lerd-dns-forwarder")
	return nil
}

// reloadAndRestartUnit reloads the service manager and restarts the given
// unit. Used by `lan:expose` / `lan:unexpose` after rewriting a quadlet or
// unit file so the new content takes effect.
func reloadAndRestartUnit(unit string) error {
	if err := services.Mgr.DaemonReload(); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}
	if err := services.Mgr.Restart(unit); err != nil {
		return fmt.Errorf("restart %s: %w", unit, err)
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
