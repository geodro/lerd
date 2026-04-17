package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// fakeUnitLifecycle records which unit was restarted.
type fakeUnitLifecycle struct {
	restartedUnit string
}

func (f *fakeUnitLifecycle) Start(name string) error                { return nil }
func (f *fakeUnitLifecycle) Stop(name string) error                 { return nil }
func (f *fakeUnitLifecycle) Restart(name string) error              { f.restartedUnit = name; return nil }
func (f *fakeUnitLifecycle) UnitStatus(name string) (string, error) { return "active", nil }

func TestRestartSite_CustomContainer(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	siteDir := t.TempDir()
	config.AddSite(config.Site{
		Name:          "nestapp",
		Domains:       []string{"nestapp.test"},
		Path:          siteDir,
		ContainerPort: 3000,
	})

	fake := &fakeUnitLifecycle{}
	podman.UnitLifecycle = fake
	defer func() { podman.UnitLifecycle = nil }()

	if err := RestartSite("nestapp"); err != nil {
		t.Fatalf("RestartSite: %v", err)
	}
	if fake.restartedUnit != "lerd-custom-nestapp" {
		t.Errorf("restarted unit = %q, want lerd-custom-nestapp", fake.restartedUnit)
	}
}

func TestRestartSite_PHPSite(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	siteDir := t.TempDir()
	config.AddSite(config.Site{
		Name:       "phpapp",
		Domains:    []string{"phpapp.test"},
		Path:       siteDir,
		PHPVersion: "8.4",
	})

	fake := &fakeUnitLifecycle{}
	podman.UnitLifecycle = fake
	defer func() { podman.UnitLifecycle = nil }()

	if err := RestartSite("phpapp"); err != nil {
		t.Fatalf("RestartSite: %v", err)
	}
	if fake.restartedUnit != "lerd-php84-fpm" {
		t.Errorf("restarted unit = %q, want lerd-php84-fpm", fake.restartedUnit)
	}
}

func TestRestartSite_NotFound(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	// Write an empty sites.yaml so FindSite returns not found.
	dir := filepath.Join(tmp, "lerd")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "sites.yaml"), []byte("sites: []\n"), 0644)

	err := RestartSite("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent site")
	}
}
