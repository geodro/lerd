package tui

import (
	"net"
	"sync"
	"time"
)

// The LAN IP lookup fires on every render when a site has LAN share on, so
// we cache it for a few seconds. The underlying net.Dial trick is cheap but
// re-running it every 100 ms of key input is wasteful when the routing
// table almost never changes between frames.
var lanIPCache struct {
	sync.Mutex
	value string
	at    time.Time
}

const lanIPTTL = 5 * time.Second

// primaryLANIP returns the host's primary outbound IPv4 address, or "" when
// detection fails. Mirrors cli.detectPrimaryLANIP so the TUI and the web UI
// agree on what URL the user should share — but kept local to avoid an
// import cycle between internal/tui and internal/cli.
func primaryLANIP() string {
	lanIPCache.Lock()
	defer lanIPCache.Unlock()
	if lanIPCache.value != "" && time.Since(lanIPCache.at) < lanIPTTL {
		return lanIPCache.value
	}

	if ip := dialProbeIP(); ip != "" {
		lanIPCache.value = ip
		lanIPCache.at = time.Now()
		return ip
	}
	if ip := ifaceProbeIP(); ip != "" {
		lanIPCache.value = ip
		lanIPCache.at = time.Now()
		return ip
	}
	return ""
}

func dialProbeIP() string {
	conn, err := net.Dial("udp4", "1.1.1.1:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || addr == nil {
		return ""
	}
	return addr.IP.String()
}

func ifaceProbeIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
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
			if v4 := ipnet.IP.To4(); v4 != nil && !v4.IsLoopback() {
				return v4.String()
			}
		}
	}
	return ""
}
