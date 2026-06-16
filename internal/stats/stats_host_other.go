//go:build !linux

package stats

// readHostProcesses is a no-op off Linux. systemd per-unit cgroup accounting
// isn't available on macOS (lerd's processes run under launchd), so the
// dashboard there shows containers only, as before.
func readHostProcesses() ([]ContainerStat, error) {
	return nil, nil
}
