package podman

import (
	"strings"
	"testing"
)

func TestSSHAuthSockEnv_RunningInjectsSocket(t *testing.T) {
	got := sshAuthSockEnv(true)
	want := []string{"--env", "SSH_AUTH_SOCK=" + SSHAgentSocket}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("sshAuthSockEnv(true) = %v, want %v", got, want)
	}
}

func TestSSHAuthSockEnv_StoppedReturnsNil(t *testing.T) {
	if got := sshAuthSockEnv(false); got != nil {
		t.Errorf("sshAuthSockEnv(false) = %v, want nil (must fall back to on-disk keys)", got)
	}
}

func TestGenerateSSHAgentQuadlet(t *testing.T) {
	content := GenerateSSHAgentQuadlet("lerd-php85-fpm:local")
	for _, want := range []string{
		"Image=lerd-php85-fpm:local",
		"ContainerName=" + SSHAgentContainer,
		"Volume=" + SSHAgentVolume + ":" + SSHAgentMountDir,
		"Volume=%h/.ssh:%h/.ssh:ro",
		"Exec=ssh-agent -D -a " + SSHAgentSocket,
		"Restart=always",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("quadlet missing %q in:\n%s", want, content)
		}
	}
}

// The FPM container template must mount the agent volume, otherwise composer in
// the FPM container can't reach the shared agent socket.
func TestFPMTemplateMountsAgentVolume(t *testing.T) {
	tmpl, err := GetQuadletTemplate("lerd-php-fpm.container.tmpl")
	if err != nil {
		t.Fatalf("reading FPM template: %v", err)
	}
	if !strings.Contains(string(tmpl), "Volume="+SSHAgentVolume+":"+SSHAgentMountDir) {
		t.Errorf("FPM template must mount %s:%s", SSHAgentVolume, SSHAgentMountDir)
	}
}
