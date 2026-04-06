//go:build linux

package cli

import "github.com/geodro/lerd/internal/dns"

func serviceStartHint(unit string) string {
	return "systemctl --user start " + unit
}

func serviceStatusHint(unit string) string {
	return "systemctl --user status " + unit
}

func dnsRestartHint() string {
	return "run 'lerd install' or: " + dns.ResolverHint()
}

func podmanDaemonHint() string {
	return "podman system service --time=0 &  or  systemctl --user start podman.socket"
}
