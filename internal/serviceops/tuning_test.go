package serviceops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// fakeQuadletOnDisk lays down a lerd-<name>.container file so
// ServiceInstalled returns true without going through podman.
func fakeQuadletOnDisk(t *testing.T, name string) {
	t.Helper()
	dir := config.QuadletDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir quadlet dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lerd-"+name+".container"), []byte("[Container]\n"), 0o644); err != nil {
		t.Fatalf("write fake quadlet: %v", err)
	}
}

// TestSaveTuningOverride_NotInstalled covers the regression #438 dev flagged:
// without the ServiceInstalled guard, a POST against a removed default
// preset would silently reinstall the service via materialise+regen+restart
// as a side effect of an edit. The guard must produce a typed sentinel so
// the HTTP handler can map it to 404 instead of leaking a generic 500.
func TestSaveTuningOverride_NotInstalled(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	_, err := SaveTuningOverride("mysql", "max_allowed_packet = 1G\n", false)
	if err == nil {
		t.Fatal("expected error for uninstalled service")
	}
	if !errors.Is(err, ErrTuningServiceNotInstalled) {
		t.Errorf("expected ErrTuningServiceNotInstalled, got: %v", err)
	}
	// The hint must be runnable as-is so the user can recover without
	// guessing the command shape.
	if got := err.Error(); !contains(got, "lerd service preset install mysql") {
		t.Errorf("expected install hint in error, got: %v", got)
	}
}

// TestSaveTuningOverride_FamilyUnsupported covers the path where the
// service is installed but its family has no tuningMounts entry. The
// helper must short-circuit before touching disk so the HTTP handler
// can map ErrTuningFamilyUnsupported to 400.
func TestSaveTuningOverride_FamilyUnsupported(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if err := config.SaveCustomService(&config.CustomService{
		Name:   "meilisearch",
		Image:  "docker.io/getmeili/meilisearch:v1",
		Family: "meilisearch",
	}); err != nil {
		t.Fatalf("SaveCustomService: %v", err)
	}
	fakeQuadletOnDisk(t, "meilisearch")

	_, err := SaveTuningOverride("meilisearch", "anything", false)
	if err == nil {
		t.Fatal("expected family-unsupported error")
	}
	if !errors.Is(err, ErrTuningFamilyUnsupported) {
		t.Errorf("expected ErrTuningFamilyUnsupported, got: %v", err)
	}
	// No tuning file should have been written.
	if _, err := os.Stat(config.ServiceTuningFile("meilisearch")); !os.IsNotExist(err) {
		t.Errorf("tuning file must not be written when family is unsupported, stat err = %v", err)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
