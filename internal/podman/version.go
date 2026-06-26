package podman

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// parsePodmanVersion extracts major.minor from `podman --version` output.
// Accepts forms like "podman version 5.8.2", "podman version 4.9.3+ds1",
// "podman version 4.9". Returns an error if no version token is found.
func parsePodmanVersion(out string) (int, int, error) {
	fields := strings.Fields(out)
	for i, f := range fields {
		if f == "version" && i+1 < len(fields) {
			return splitMajorMinor(fields[i+1])
		}
	}
	return 0, 0, fmt.Errorf("podman version: no version token in %q", out)
}

func splitMajorMinor(v string) (int, int, error) {
	// Strip distro/build suffixes like "+ds1" or "-rc1".
	for _, sep := range []string{"+", "-", "~"} {
		if idx := strings.Index(v, sep); idx > 0 {
			v = v[:idx]
		}
	}
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return 0, 0, fmt.Errorf("podman version %q: not enough components", v)
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("podman version major %q: %w", parts[0], err)
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("podman version minor %q: %w", parts[1], err)
	}
	return major, minor, nil
}

// podmanVersionSupportsStopTimeout reports whether the StopTimeout= key in
// quadlet [Container] sections is recognised. Added in Podman 5.0; Ubuntu
// 24.04 ships 4.9.3 which rejects the unit and emits no service files.
func podmanVersionSupportsStopTimeout(major, minor int) bool {
	_ = minor
	return major >= 5
}

// supportsContainerStopTimeoutKey is the runtime test seam used by the
// quadlet generator. Tests override this to exercise both branches without
// shelling out. The default lazily probes the local podman once.
var supportsContainerStopTimeoutKey = defaultSupportsContainerStopTimeoutKey

var (
	stopTimeoutOnce   sync.Once
	stopTimeoutResult bool
)

func defaultSupportsContainerStopTimeoutKey() bool {
	stopTimeoutOnce.Do(func() {
		// Use PodmanBin() so the probe still resolves under launchd's
		// restricted PATH on macOS, where "podman" alone misses Homebrew.
		out, err := exec.Command(PodmanBin(), "--version").Output()
		if err != nil {
			// Conservative fallback: PodmanArgs= works on every quadlet
			// version, while StopTimeout= breaks <5.0. Better to use the
			// universal escape hatch than emit a unit systemd refuses.
			stopTimeoutResult = false
			return
		}
		major, minor, err := parsePodmanVersion(string(out))
		if err != nil {
			stopTimeoutResult = false
			return
		}
		stopTimeoutResult = podmanVersionSupportsStopTimeout(major, minor)
	})
	return stopTimeoutResult
}
