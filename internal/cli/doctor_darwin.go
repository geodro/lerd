//go:build darwin

package cli

import (
	"os/exec"
	"strings"
)

// portListOutput returns a summary of listening TCP ports for batch checks.
func portListOutput() string {
	out, err := exec.Command("lsof", "-nP", "-iTCP", "-sTCP:LISTEN").Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// portInUse returns true if something is listening on the given TCP port.
func portInUse(port string) bool {
	out, err := exec.Command("lsof", "-nP", "-iTCP:"+port, "-sTCP:LISTEN").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), ":"+port)
}
