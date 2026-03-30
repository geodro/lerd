//go:build darwin

package cli

func serviceStartHint(unit string) string {
	return "lerd start"
}

func serviceStatusHint(unit string) string {
	return "lerd start  |  check: launchctl print gui/$(id -u)/com.lerd." + unit
}

func dnsRestartHint() string {
	return "run 'lerd install' to reconfigure DNS"
}

func podmanDaemonHint() string {
	return "podman machine start"
}
