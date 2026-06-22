package serviceops

import (
	"net"
	"os"
	"strconv"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// probeTimeout bounds each socket/TCP dial so the host-MySQL probe stays snappy
// for the dashboard even when nothing is listening.
const probeTimeout = 750 * time.Millisecond

// legacyMySQLSocket is the older /var/run path some distros still symlink the
// MySQL socket to; probed as a fallback when the default /run path is absent.
const legacyMySQLSocket = "/var/run/mysqld/mysqld.sock"

// HostMySQLStatus reports whether a host-installed (system) MySQL/MariaDB is
// present and reachable, for the dashboard's "system MySQL" backend option.
// It needs no MySQL client — only a socket stat, a unix dial, and a TCP probe.
type HostMySQLStatus struct {
	SocketPresent    bool   `json:"socket_present"`
	SocketPath       string `json:"socket_path"`
	Live             bool   `json:"live"`              // a server is accepting on the socket
	TCP3306Listening bool   `json:"tcp3306_listening"` // something answers on 127.0.0.1:3306
	TCP3306Owner     string `json:"tcp3306_owner"`     // host | lerd | none | unknown
	// LerdPort is the host port lerd-mysql publishes (3306 by default, or the
	// configured PublishedPort override). When it differs from 3306 the host
	// system MySQL can own 3306 and the two coexist. LerdPortListening reports
	// whether something answers on 127.0.0.1:<LerdPort>.
	LerdPort          int  `json:"lerd_port"`
	LerdPortListening bool `json:"lerd_port_listening"`
}

// ProbeHostMySQL inspects the host MySQL unix socket at socketPath (empty =>
// config.DefaultHostMySQLSocket, with the /var/run fallback) and 127.0.0.1:3306,
// then attributes the TCP listener. lerd's rootless MySQL container never
// exposes a host unix socket, so socket presence is the strong "this is the
// host server" discriminator.
func ProbeHostMySQL(socketPath string) HostMySQLStatus {
	path := socketPath
	if path == "" {
		path = config.DefaultHostDBSocketPath()
	}
	present, live := socketLive(path, probeTimeout)
	if !present && path != legacyMySQLSocket {
		if p2, l2 := socketLive(legacyMySQLSocket, probeTimeout); p2 {
			path, present, live = legacyMySQLSocket, p2, l2
		}
	}
	listening := tcpListening("127.0.0.1:3306", probeTimeout)
	lerdPort := lerdMySQLPort()
	lerdListening := listening // when lerd is on 3306, its listener is the 3306 one
	if lerdPort != 3306 {
		lerdListening = tcpListening(net.JoinHostPort("127.0.0.1", strconv.Itoa(lerdPort)), probeTimeout)
	}
	lerdRunning := podman.ContainerRunningQuiet("lerd-mysql")
	return HostMySQLStatus{
		SocketPresent:     present,
		SocketPath:        path,
		Live:              live,
		TCP3306Listening:  listening,
		TCP3306Owner:      attribute3306Owner(listening, present, lerdRunning, lerdPort == 3306),
		LerdPort:          lerdPort,
		LerdPortListening: lerdListening,
	}
}

// lerdMySQLPort returns the host port lerd-mysql publishes: the configured
// PublishedPort override when set, otherwise the canonical default 3306.
func lerdMySQLPort() int {
	if cfg, err := config.LoadGlobal(); err == nil {
		if sc, ok := cfg.Services["mysql"]; ok && sc.PublishedPort > 0 {
			return sc.PublishedPort
		}
	}
	return 3306
}

// socketLive reports whether path is a unix socket (present) and whether a
// server is currently accepting connections on it (live). A stale socket file
// left by a crashed server is present but not live.
func socketLive(path string, timeout time.Duration) (present, live bool) {
	fi, err := os.Stat(path)
	if err != nil || fi.Mode()&os.ModeSocket == 0 {
		return false, false
	}
	conn, err := net.DialTimeout("unix", path, timeout)
	if err != nil {
		return true, false
	}
	_ = conn.Close()
	return true, true
}

// tcpListening reports whether something is accepting TCP connections at addr.
func tcpListening(addr string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// attribute3306Owner decides who owns 127.0.0.1:3306. lerdOn3306 says whether
// lerd-mysql publishes 3306 (true unless it was moved off via PublishedPort).
// A running lerd-mysql that still publishes 3306 wins attribution (they would
// conflict with a host server there); once lerd is moved off 3306, a 3306
// listener is the host server, so a present socket — or just the listener
// itself — attributes to the host. With lerdOn3306=true the result is identical
// to the previous three-signal behaviour, so existing callers are unaffected.
func attribute3306Owner(listening, socketPresent, lerdRunning, lerdOn3306 bool) string {
	switch {
	case !listening:
		return "none"
	case lerdRunning && lerdOn3306:
		return "lerd"
	case socketPresent:
		return "host"
	case lerdRunning && !lerdOn3306:
		// lerd is running elsewhere, yet 3306 has a listener: it must be the host.
		return "host"
	default:
		return "unknown"
	}
}
