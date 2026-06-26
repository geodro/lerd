package podman

import (
	"fmt"
	"strings"
)

// Shared ssh-agent sidecar. It holds the user's unlocked SSH keys in memory and
// exposes its socket on the lerd-ssh-agent named volume, which is also mounted
// into the FPM containers (see lerd-php-fpm.container.tmpl) so composer's
// git-over-SSH can reach the agent for private packages with passphrase keys.
// Keys without a passphrase already work through the bind-mounted ~/.ssh, so the
// agent is only needed for passphrase-protected keys.
const (
	// SSHAgentContainer is the podman container name of the sidecar.
	SSHAgentContainer = "lerd-ssh-agent"
	// SSHAgentUnit is the quadlet/systemd unit name (matches the container).
	SSHAgentUnit = "lerd-ssh-agent"
	// SSHAgentVolume is the named volume that carries the agent socket. Podman
	// auto-creates it on first container start; it lives inside the podman
	// machine on macOS, so the socket never crosses the host-VM boundary.
	SSHAgentVolume = "lerd-ssh-agent"
	// SSHAgentMountDir is where the named volume is mounted in every container.
	SSHAgentMountDir = "/ssh-agent"
	// SSHAgentSocket is the agent socket path, shared via the named volume.
	SSHAgentSocket = "/ssh-agent/agent.sock"
)

// GenerateSSHAgentQuadlet renders the quadlet for the shared ssh-agent sidecar.
// image is the FPM image to reuse: it ships openssh-client (so ssh-agent and
// ssh-add are present), which avoids pulling or building a dedicated image. The
// agent reads key files from the read-only ~/.ssh bind mount; the unlocked key
// material stays in the agent's memory only.
func GenerateSSHAgentQuadlet(image string) string {
	var b strings.Builder
	fmt.Fprintln(&b, "[Unit]")
	fmt.Fprintln(&b, "Description=Lerd shared ssh-agent")
	fmt.Fprintln(&b, "After=network.target")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "[Container]")
	fmt.Fprintf(&b, "Image=%s\n", image)
	fmt.Fprintf(&b, "ContainerName=%s\n", SSHAgentContainer)
	fmt.Fprintln(&b, "Network=lerd")
	fmt.Fprintf(&b, "Volume=%s:%s\n", SSHAgentVolume, SSHAgentMountDir)
	fmt.Fprintln(&b, "Volume=%h/.ssh:%h/.ssh:ro")
	fmt.Fprintf(&b, "Environment=SSH_AUTH_SOCK=%s\n", SSHAgentSocket)
	fmt.Fprintln(&b, "PodmanArgs=--security-opt=label=disable")
	fmt.Fprintf(&b, "Exec=ssh-agent -D -a %s\n", SSHAgentSocket)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "[Service]")
	fmt.Fprintln(&b, "Restart=always")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "[Install]")
	fmt.Fprintln(&b, "WantedBy=default.target")
	return b.String()
}

// SSHAuthSockEnv returns the `podman exec` --env arguments that point
// composer/php at the shared ssh-agent, but only when the agent container is
// running. When it is not, it returns nil so ssh falls back to the on-disk keys
// instead of failing against a dead socket.
func SSHAuthSockEnv() []string {
	return sshAuthSockEnv(ContainerRunningQuiet(SSHAgentContainer))
}

// sshAuthSockEnv is the pure decision behind SSHAuthSockEnv, split out so it can
// be unit-tested without a running container.
func sshAuthSockEnv(agentRunning bool) []string {
	if !agentRunning {
		return nil
	}
	return []string{"--env", "SSH_AUTH_SOCK=" + SSHAgentSocket}
}
