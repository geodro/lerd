package podman

import (
	"strings"
	"testing"
)

func TestGenerateCustomContainerQuadlet(t *testing.T) {
	content := GenerateCustomContainerQuadlet("nestapp", "/home/user/projects/nestapp", 3000)

	checks := []struct {
		label    string
		contains string
	}{
		{"image", "Image=lerd-custom-nestapp:local"},
		{"container name", "ContainerName=lerd-custom-nestapp"},
		{"network", "Network=lerd"},
		{"project volume", "Volume=/home/user/projects/nestapp:/home/user/projects/nestapp:rw"},
		{"hosts volume", "/etc/hosts:ro,z"},
		{"security opt", "--security-opt=label=disable"},
		{"workdir", "--workdir=/home/user/projects/nestapp"},
		{"restart", "Restart=always"},
		{"description", "Description=Lerd custom container (nestapp)"},
		{"install", "WantedBy=default.target"},
	}

	for _, check := range checks {
		if !strings.Contains(content, check.contains) {
			t.Errorf("%s: quadlet missing %q\n\nFull content:\n%s", check.label, check.contains, content)
		}
	}
}

func TestGenerateCustomContainerQuadlet_DifferentSite(t *testing.T) {
	content := GenerateCustomContainerQuadlet("goapp", "/var/www/goapp", 8080)

	if !strings.Contains(content, "Image=lerd-custom-goapp:local") {
		t.Error("wrong image name")
	}
	if !strings.Contains(content, "ContainerName=lerd-custom-goapp") {
		t.Error("wrong container name")
	}
	if !strings.Contains(content, "Volume=/var/www/goapp:/var/www/goapp:rw") {
		t.Error("wrong project volume")
	}
	if !strings.Contains(content, "--workdir=/var/www/goapp") {
		t.Error("wrong workdir")
	}
}
