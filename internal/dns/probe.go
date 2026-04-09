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
	host := "test-lerd-probe." + tld
	addrs, err := net.LookupHost(host)
	if err != nil {
		return false, nil //nolint:nilerr // DNS failure is a probe negative, not an error
	}

	exposed := false
	if cfg, err := config.LoadGlobal(); err == nil && cfg != nil {
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
