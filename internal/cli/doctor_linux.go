//go:build linux

package cli

import (
	"os/exec"
	"strings"
)

// portListOutput returns the raw output of ss -tlnp for batch port checks.
func portListOutput() string {
	out, err := exec.Command("ss", "-tlnp").Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// portInUse returns true if something is listening on the given TCP port.
func portInUse(port string) bool {
	return strings.Contains(portListOutput(), ":"+port+" ")
}
