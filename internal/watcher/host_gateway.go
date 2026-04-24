package watcher

import (
	"net"
	"runtime"
	"time"

	"github.com/geodro/lerd/internal/podman"
)

// hostGatewayDeps is the injection surface for tickHostGateway so the
// orchestration can be unit-tested without spinning up lerd-nginx.
type hostGatewayDeps struct {
	primaryLANIP    func() string
	readCurrent     func() string
	reachable       func(ip string) bool
	detectFresh     func() string
	writeHosts      func() error
	readNginxOnDisk func() string
	liveNginxIP     func() string
	// driftEveryN gates the nginx drift check to one run every N ticks.
	// Values below 1 are treated as 1 (every tick).
	driftEveryN int
	log         func(level, msg string, kv ...any)
}

// hostGatewayState is the cross-tick memory for WatchHostGateway.
type hostGatewayState struct {
	lastLAN   string
	tickCount int
}

// nginxDriftDefault returns the default number of ticks between nginx
// drift checks for the current platform. macOS pays VM-hop cost per
// podman inspect so it polls much less often; Linux is nearly free.
func nginxDriftDefault() int {
	if runtime.GOOS == "darwin" {
		return 30
	}
	return 10
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
		primaryLANIP:    primaryLANIP,
		readCurrent:     podman.ReadHostGatewayFromFile,
		reachable:       podman.HostReachable,
		detectFresh:     podman.DetectHostGatewayIPProbeOnly,
		writeHosts:      podman.WriteContainerHosts,
		readNginxOnDisk: podman.ReadNginxIPFromContainerHosts,
		liveNginxIP:     podman.NginxContainerIPOrEmpty,
		driftEveryN:     nginxDriftDefault(),
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

// tickHostGateway runs one iteration: LAN-change path every tick, nginx
// drift path on a slower cadence so laptops stay deep-sleep friendly.
func tickHostGateway(d hostGatewayDeps, s *hostGatewayState) {
	s.tickCount++
	tickLANChange(d, s)
	n := d.driftEveryN
	if n < 1 {
		n = 1
	}
	if s.tickCount%n == 0 {
		tickNginxDrift(d)
	}
}

// tickLANChange rewrites the hosts file when the primary LAN IP changed
// since the last tick and the old gateway entry no longer routes.
func tickLANChange(d hostGatewayDeps, s *hostGatewayState) {
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

// tickNginxDrift rewrites the hosts file when the nginx bridge IP on disk
// differs from the live container's IP. Catches container renumbering that
// the LAN change path can't detect. One file read + one inspect per tick.
func tickNginxDrift(d hostGatewayDeps) {
	if d.readNginxOnDisk == nil || d.liveNginxIP == nil {
		return
	}
	onDisk := d.readNginxOnDisk()
	if onDisk == "" {
		return
	}
	live := d.liveNginxIP()
	if live == "" || live == onDisk {
		return
	}
	if err := d.writeHosts(); err != nil {
		d.log("warn", "rewriting container hosts file for nginx IP drift", "err", err)
		return
	}
	d.log("info", "nginx container IP drift corrected", "old", onDisk, "new", live)
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
