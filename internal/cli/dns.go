package cli

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"

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

// detectPrimaryLANIP returns the local IPv4 address that the kernel would use
// to reach a public destination, without actually sending a packet. Falls back
// to scanning interfaces when the dial trick fails (e.g. no default route).
func detectPrimaryLANIP() (string, error) {
	conn, err := net.Dial("udp4", "1.1.1.1:80")
	if err == nil {
		defer conn.Close()
		return conn.LocalAddr().(*net.UDPAddr).IP.String(), nil
	}

	ifaces, ifErr := net.Interfaces()
	if ifErr != nil {
		return "", fmt.Errorf("listing interfaces: %w", ifErr)
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok {
				if v4 := ipnet.IP.To4(); v4 != nil && !v4.IsLoopback() {
					return v4.String(), nil
				}
			}
		}
	}
	return "", fmt.Errorf("no usable IPv4 address found")
}
