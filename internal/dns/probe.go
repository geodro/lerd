package dns

import (
	"net"

	"github.com/geodro/lerd/internal/config"
)

// Check resolves test-lerd-probe.{tld} and reports whether the answer is one
// the lerd dnsmasq could legitimately return. With lan:expose off, the
// expected answer is 127.0.0.1 (loopback). With lan:expose on, the dnsmasq
// answers with the host's primary LAN IP so remote clients can reach the
// actual nginx instance, but the local host still routes those packets
// through its own loopback interface so the site is reachable from the
// server itself too.
//
// Returns (true, nil) if DNS resolution is working correctly for the given
// TLD in either mode.
func Check(tld string) (bool, error) {
	cfg, _ := config.LoadGlobal()
	if cfg != nil && !cfg.DNS.Enabled {
		return true, nil
	}
	host := "test-lerd-probe." + tld
	addrs, err := net.LookupHost(host)
	if err != nil {
		return false, nil //nolint:nilerr // DNS failure is a probe negative, not an error
	}

	exposed := false
	if cfg != nil {
		exposed = cfg.LAN.Exposed
	}
	lanIP := ""
	if exposed {
		lanIP = primaryLANIP()
	}

	for _, addr := range addrs {
		if addr == "127.0.0.1" {
			return true, nil
		}
		if exposed && lanIP != "" && addr == lanIP {
			return true, nil
		}
	}
	return false, nil
}

// Status is the three-way DNS health the dashboard pill renders. It exists
// to separate a genuine lerd-dns outage from the common case where
// lerd-dns is perfectly healthy but the system resolver isn't routing the
// TLD to it, which a VPN client typically causes by rewriting
// systemd-resolved when it connects.
type Status string

const (
	StatusOK       Status = "ok"       // system resolver routes the TLD to lerd-dns
	StatusDegraded Status = "degraded" // lerd-dns answers directly, system resolver bypassed
	StatusDown     Status = "down"     // lerd-dns itself is not answering
)

// CheckStatus reports three-way DNS health. It runs Check first (the
// end-to-end system-resolver path); when that fails it queries lerd's
// dnsmasq directly on 127.0.0.1:5300. A direct answer means lerd-dns is up
// and only the resolver hookup is bypassed, so the result is Degraded
// rather than Down.
func CheckStatus(tld string) Status {
	// Check already short-circuits to true in disabled mode, so that case
	// resolves to StatusOK here without a separate branch.
	if ok, _ := Check(tld); ok {
		return StatusOK
	}
	if dnsmasqDirectOK(tld) {
		return StatusDegraded
	}
	return StatusDown
}

// DnsmasqAnswer returns the A-record answer lerd's dnsmasq gives for the
// configured TLD, queried directly on 127.0.0.1:5300 (bypassing the system
// resolver). Exported so the watcher can compare the published lan:expose
// mapping against the host's current primary LAN IP and detect drift after a
// sleep/wake DHCP renew or a network switch.
func DnsmasqAnswer(tld string) (string, error) {
	return defaultDnsmasqAnswer(tld)
}

// dnsmasqDirectOK queries lerd's dnsmasq straight on 127.0.0.1:5300,
// bypassing the system resolver entirely. It returns true when dnsmasq
// answers with an address lerd would legitimately hand out, which is the
// signal that lerd-dns is alive even though the system resolver isn't
// using it.
func dnsmasqDirectOK(tld string) bool {
	answer, err := defaultDnsmasqAnswer(tld)
	if err != nil {
		return false
	}
	if answer == "127.0.0.1" {
		return true
	}
	cfg, _ := config.LoadGlobal()
	if cfg != nil && cfg.LAN.Exposed && answer == primaryLANIP() {
		return true
	}
	return false
}
