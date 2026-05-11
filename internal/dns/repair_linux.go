//go:build linux

package dns

// RepairPossible reports whether the DNS watcher can fix /etc/resolv.conf
// or NetworkManager configuration from the current process. Linux's repair
// path runs through systemd-resolved / nmcli / resolvectl and does not
// require root for the user-scoped commands lerd issues, so this is always
// true. Mirrors the macOS variant which gates on whether the lerd
// passwordless-sudo drop-in is in place.
func RepairPossible() bool { return true }
