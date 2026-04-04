//go:build linux

package cli

import (
	"os/exec"
	"strings"
)

// portInUse returns true if something is listening on the given TCP port.
func portInUse(port string) bool {
	out, err := exec.Command("ss", "-tlnp").Output()
	if err != nil {
		return false
	}
	needle := ":" + port + " "
	return strings.Contains(string(out), needle)
}
