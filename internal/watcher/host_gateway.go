package watcher

import (
	"net"
	"time"

	"github.com/geodro/lerd/internal/podman"
)

// hostGatewayDeps is the injection surface for tickHostGateway so the
// orchestration can be unit-tested without spinning up lerd-nginx.
type hostGatewayDeps struct {
	primaryLANIP func() string
	readCurrent  func() string
	reachable    func(ip string) bool
	detectFresh  func() string
	writeHosts   func() error
	log          func(level, msg string, kv ...any)
}

// hostGatewayState is the cross-tick memory for WatchHostGateway. We only
// keep the last-seen LAN IP so the fast path (compare and skip) is one
// cheap UDP dial with no container exec. Promoted out of the tick
// function so tests can seed it.
type hostGatewayState struct {
	lastLAN string
}

// WatchHostGateway keeps the host.containers.internal entry in the shared
// PHP-FPM /etc/hosts file pointing at an IP that actually routes back to the
// host. Without this, a laptop that changes networks (coffee shop to home
// wifi to mobile hotspot) ends up with a stale LAN IP in /etc/hosts and
// Xdebug silently times out until the next `lerd start`.
//
// Steady-state cost is deliberately near-zero: we track the host's primary
// LAN IP across ticks and only run the expensive podman exec reachability
// probe when it changes. The LAN-IP lookup is a Go net.Dial("udp4",
// "1.1.1.1:80") which never sends a packet — the kernel just returns the
// route source address — so it's microseconds per tick. This matters on
// macOS in particular, where podman exec goes through the podman-machine
// VM's gvproxy / sshd / runtime and costs 300 ms – 1 s per call.
//
// A LAN change on macOS doesn't necessarily invalidate gvproxy's
// host.containers.internal address, so the reprobe after a LAN rotation
// may turn up the same IP on disk and correctly skip the write. One
// spurious podman exec per real network change is cheap enough not to
// justify a platform-specific fast path.
func WatchHostGateway(interval time.Duration) {
	deps := hostGatewayDeps{
		primaryLANIP: primaryLANIP,
		readCurrent:  podman.ReadHostGatewayFromFile,
		reachable:    podman.HostReachable,
		detectFresh:  podman.DetectHostGatewayIPProbeOnly,
		writeHosts:   podman.WriteContainerHosts,
		log: func(level, msg string, kv ...any) {
			switch level {
			case "info":
				logger.Info(msg, kv...)
			case "warn":
				logger.Warn(msg, kv...)
			}
		},
	}
	state := &hostGatewayState{lastLAN: primaryLANIP()}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		tickHostGateway(deps, state)
	}
}

// tickHostGateway runs one iteration of the watch loop. The fast path
// (LAN IP unchanged since last tick) returns without touching podman,
// keeping the steady-state cost to a UDP-dial's worth of CPU. The slow
// path only fires when the host's primary LAN IP actually changes, which
// is the signal a laptop moved networks.
func tickHostGateway(d hostGatewayDeps, s *hostGatewayState) {
	lan := d.primaryLANIP()
	if lan == s.lastLAN {
		return
	}
	s.lastLAN = lan

	current := d.readCurrent()
	if current != "" && d.reachable(current) {
		return
	}
	fresh := d.detectFresh()
	if fresh == "" || fresh == current {
		return
	}
	if err := d.writeHosts(); err != nil {
		d.log("warn", "rewriting container hosts file", "err", err)
		return
	}
	d.log("info", "host gateway IP updated", "old", current, "new", fresh)
}

// primaryLANIP returns the local IPv4 address the kernel would use to reach
// a public destination. Duplicates internal/podman/hosts.go's helper rather
// than importing it, because we want this watcher cost to stay micro-level
// and not pay for loading the podman package's init costs on every tick.
func primaryLANIP() string {
	conn, err := net.Dial("udp4", "1.1.1.1:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}
