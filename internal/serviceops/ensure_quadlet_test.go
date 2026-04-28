package serviceops

import (
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

func TestEnsureCustomServiceQuadlet_reloadsOnlyWhenContentChanges(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))

	count := 0
	orig := podman.DaemonReloadFn
	t.Cleanup(func() { podman.DaemonReloadFn = orig })
	podman.DaemonReloadFn = func() error {
		count++
		return nil
	}

	svc := &config.CustomService{
		Name:  "mongo-express",
		Image: "docker.io/library/mongo-express:latest",
		Ports: []string{"127.0.0.1:8082:8081"},
	}

	if err := EnsureCustomServiceQuadlet(svc); err != nil {
		t.Fatalf("first EnsureCustomServiceQuadlet: %v", err)
	}
	if count != 1 {
		t.Errorf("first call should reload once, got %d", count)
	}

	if err := EnsureCustomServiceQuadlet(svc); err != nil {
		t.Fatalf("second EnsureCustomServiceQuadlet: %v", err)
	}
	if count != 1 {
		t.Errorf("second call with unchanged content must not reload, got %d total", count)
	}

	svc.Image = "docker.io/library/mongo-express:1.0.2"
	if err := EnsureCustomServiceQuadlet(svc); err != nil {
		t.Fatalf("third EnsureCustomServiceQuadlet: %v", err)
	}
	if count != 2 {
		t.Errorf("changed image should reload again, got %d total", count)
	}
}
