package podman

import (
	"os/exec"
	"strings"
)

// NetworkGateway returns the gateway IP of the named Podman network.
// Falls back to "127.0.0.1" if it cannot be determined.
func NetworkGateway(name string) string {
	out, err := exec.Command(podmanBin(), "network", "inspect", name,
		"--format", "{{range .Subnets}}{{.Gateway}}{{end}}").Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return "127.0.0.1"
	}
	return strings.TrimSpace(string(out))
}

// EnsureNetwork creates the named Podman network if it does not already exist.
func EnsureNetwork(name string) error {
	out, err := Run("network", "ls", "--format={{.Name}}")
	if err != nil {
		return err
	}

	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == name {
			return nil // already exists
		}
	}

	return RunSilent("network", "create", "--driver", "bridge", name)
}
