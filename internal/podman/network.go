package podman

import (
	"os/exec"
	"strings"
)

// NetworkGateway returns the gateway IP of the named Podman network.
// Falls back to "127.0.0.1" if it cannot be determined.
func NetworkGateway(name string) string {
	out, err := exec.Command(PodmanBin(), "network", "inspect", name,
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

// EnsureNetworkDNS syncs the DNS servers on the named network to the provided list.
// It drops servers no longer present and adds new ones. This sets the upstream
// forwarders that aardvark-dns uses, which is necessary on systems where
// /etc/resolv.conf points to a stub resolver (e.g. 127.0.0.53) that is not
// reachable from inside the container network namespace.
func EnsureNetworkDNS(name string, servers []string) error {
	if len(servers) == 0 {
		return nil
	}

	// Get current DNS servers on the network.
	out, err := Run("network", "inspect", name, "--format", "{{range .NetworkDNSServers}}{{.}} {{end}}")
	if err != nil {
		return err
	}

	current := map[string]bool{}
	for _, s := range strings.Fields(out) {
		current[s] = true
	}

	desired := map[string]bool{}
	for _, s := range servers {
		desired[s] = true
	}

	// Drop servers that are no longer desired.
	for s := range current {
		if !desired[s] {
			if err := RunSilent("network", "update", "--dns-drop", s, name); err != nil {
				return err
			}
		}
	}

	// Add servers that are not yet present.
	for s := range desired {
		if !current[s] {
			if err := RunSilent("network", "update", "--dns-add", s, name); err != nil {
				return err
			}
		}
	}

	return nil
}
