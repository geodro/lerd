package podman

import (
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func TestCustomFPMContainerName(t *testing.T) {
	if got := CustomFPMContainerName("myapp"); got != "lerd-cfpm-myapp" {
		t.Fatalf("CustomFPMContainerName: want lerd-cfpm-myapp, got %s", got)
	}
}

func TestFPMContainerName(t *testing.T) {
	// Custom-FPM site resolves to its per-site container regardless of version.
	cfpm := config.Site{Name: "myapp", Runtime: "fpm-custom"}
	if got := FPMContainerName(cfpm, "8.4"); got != "lerd-cfpm-myapp" {
		t.Errorf("FPMContainerName(custom-fpm): want lerd-cfpm-myapp, got %s", got)
	}
	// Plain PHP site resolves to the shared per-version container.
	plain := config.Site{Name: "myapp"}
	if got := FPMContainerName(plain, "8.4"); got != "lerd-php84-fpm" {
		t.Errorf("FPMContainerName(plain): want lerd-php84-fpm, got %s", got)
	}
	// A custom-container (port-based proxy) site is not custom-FPM; it still
	// resolves to the shared FPM name here (it isn't served by fastcgi).
	proxy := config.Site{Name: "node", ContainerPort: 3000}
	if got := FPMContainerName(proxy, "8.3"); got != "lerd-php83-fpm" {
		t.Errorf("FPMContainerName(proxy): want lerd-php83-fpm, got %s", got)
	}
}

func TestGenerateCustomFPMQuadlet_OverridesImageAndName(t *testing.T) {
	content, err := generateCustomFPMQuadlet("myapp", "8.4")
	if err != nil {
		t.Fatalf("generateCustomFPMQuadlet: %v", err)
	}
	// Image and ContainerName must point at the per-site custom build, not the
	// shared lerd-php84-fpm.
	mustContain := []string{
		"Image=" + CustomImageName("myapp"),
		"ContainerName=lerd-cfpm-myapp",
	}
	for _, s := range mustContain {
		if !strings.Contains(content, s) {
			t.Errorf("quadlet missing %q\n%s", s, content)
		}
	}
	// The shared image/name must be gone (the override replaced them).
	for _, s := range []string{"Image=lerd-php84-fpm:local", "ContainerName=lerd-php84-fpm"} {
		if strings.Contains(content, s) {
			t.Errorf("quadlet still has shared %q after override", s)
		}
	}
	// It reuses the shared FPM template, so it inherits the lerd mounts; the
	// dump bridge mount target is a stable proof of that.
	if !strings.Contains(content, "/usr/local/etc/lerd") {
		t.Errorf("custom FPM quadlet did not inherit shared FPM mounts:\n%s", content)
	}
}
