package dns

import (
	"net"
	"strings"
)

// vpnIfacePrefixes are the interface-name prefixes used by VPN tunnels:
// OpenVPN/WireGuard/AnyConnect tun*, macOS utun*, tap*, wg*, IPsec and ppp.
var vpnIfacePrefixes = []string{"tun", "utun", "tap", "wg", "ipsec", "ppp", "cscotun"}

// isVPNIface reports whether an interface name belongs to a VPN tunnel.
func isVPNIface(name string) bool {
	for _, prefix := range vpnIfacePrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// VPNActive reports whether a VPN tunnel interface is currently up. It is
// used to word the "DNS degraded" hint and the doctor diagnostic, since a
// VPN client rewriting the system resolver is by far the most common cause
// of the system-resolver path failing while lerd-dns itself stays healthy.
func VPNActive() bool {
	ifaces, err := net.Interfaces()
	if err != nil {
		return false
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp != 0 && isVPNIface(iface.Name) {
			return true
		}
	}
	return false
}
