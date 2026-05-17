package serviceops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// TestEnsureDefaultPresetQuadletPinned_preservesImage covers the regression
// the v1.19.0-beta.6 fix targets — a `lerd update` install rewrite must not
// silently jump default-preset users from their installed minor to whatever
// preset.Image declares. Reinstall now preserves the on-disk image even
// after RemoveService deletes the quadlet by passing the captured image to
// EnsureDefaultPresetQuadletPinned.
func TestEnsureDefaultPresetQuadletPinned_preservesImage(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))

	orig := podman.DaemonReloadFn
	t.Cleanup(func() { podman.DaemonReloadFn = orig })
	podman.DaemonReloadFn = func() error { return nil }

	pinned := "docker.io/getmeili/meilisearch:v1.7.0"
	if err := EnsureDefaultPresetQuadletPinned("meilisearch", pinned); err != nil {
		t.Fatalf("EnsureDefaultPresetQuadletPinned: %v", err)
	}

	quadletPath := filepath.Join(config.QuadletDir(), "lerd-meilisearch.container")
	data, err := os.ReadFile(quadletPath)
	if err != nil {
		t.Fatalf("read quadlet: %v", err)
	}
	if !strings.Contains(string(data), "Image="+pinned) {
		t.Errorf("expected Image=%s in quadlet, got:\n%s", pinned, string(data))
	}
}

// TestEnsureDefaultPresetQuadletPinned_emptyPinFallsThroughToPreset verifies
// that passing pinnedImage="" preserves existing behaviour: the function
// still resolves the image via the preset/strategy/track_latest path.
func TestEnsureDefaultPresetQuadletPinned_emptyPinFallsThroughToPreset(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))

	orig := podman.DaemonReloadFn
	t.Cleanup(func() { podman.DaemonReloadFn = orig })
	podman.DaemonReloadFn = func() error { return nil }

	if err := EnsureDefaultPresetQuadletPinned("meilisearch", ""); err != nil {
		t.Fatalf("EnsureDefaultPresetQuadletPinned with empty pin: %v", err)
	}
	quadletPath := filepath.Join(config.QuadletDir(), "lerd-meilisearch.container")
	data, err := os.ReadFile(quadletPath)
	if err != nil {
		t.Fatalf("read quadlet: %v", err)
	}
	if !strings.Contains(string(data), "Image=docker.io/getmeili/meilisearch:") {
		t.Errorf("expected meilisearch Image= line in quadlet, got:\n%s", string(data))
	}
}

func TestEnsureDefaultPresetQuadlet_writesCanonicalPinOnFirstInstall(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))

	orig := podman.DaemonReloadFn
	t.Cleanup(func() { podman.DaemonReloadFn = orig })
	podman.DaemonReloadFn = func() error { return nil }

	if err := EnsureDefaultPresetQuadlet("postgres"); err != nil {
		t.Fatalf("EnsureDefaultPresetQuadlet: %v", err)
	}
	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if got := cfg.Services["postgres"].CanonicalVersion; got != "16" {
		t.Errorf("first install must persist CanonicalVersion=16, got %q", got)
	}
}

func TestEnsureDefaultPresetQuadlet_singleVersionPresetSkipsPin(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))

	orig := podman.DaemonReloadFn
	t.Cleanup(func() { podman.DaemonReloadFn = orig })
	podman.DaemonReloadFn = func() error { return nil }

	if err := EnsureDefaultPresetQuadlet("mailpit"); err != nil {
		t.Fatalf("EnsureDefaultPresetQuadlet(mailpit): %v", err)
	}
	cfg, _ := config.LoadGlobal()
	if got := cfg.Services["mailpit"].CanonicalVersion; got != "" {
		t.Errorf("single-version preset must not write a CanonicalVersion pin, got %q", got)
	}
}

func TestMatchVersionByImageTag(t *testing.T) {
	versions := []config.PresetVersion{
		{Tag: "18", Image: "docker.io/postgis/postgis:18-3.6-alpine"},
		{Tag: "17", Image: "docker.io/postgis/postgis:17-3.6-alpine"},
		{Tag: "16", Image: "docker.io/postgis/postgis:16-3.5-alpine"},
	}
	cases := map[string]string{
		"docker.io/postgis/postgis:16-3.5-alpine":   "16",
		"docker.io/postgis/postgis:16.5-3.5-alpine": "16",
		"docker.io/postgis/postgis:18-3.6-alpine":   "18",
		"docker.io/postgis/postgis:17.2-3.6-alpine": "17",
		"docker.io/library/mysql:8.4.9":             "",
		"no-tag":                                    "",
	}
	for image, want := range cases {
		if got := matchVersionByImageTag(image, versions); got != want {
			t.Errorf("matchVersionByImageTag(%q) = %q, want %q", image, got, want)
		}
	}
	mysqlVersions := []config.PresetVersion{
		{Tag: "9.7"}, {Tag: "8.4"}, {Tag: "5.7"},
	}
	if got := matchVersionByImageTag("docker.io/library/mysql:8.4.9", mysqlVersions); got != "8.4" {
		t.Errorf("mysql 8.4.9 should match 8.4, got %q", got)
	}
}

func TestEnsureDefaultPresetQuadlet_honorsCanonicalPinAcrossFlip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))

	orig := podman.DaemonReloadFn
	t.Cleanup(func() { podman.DaemonReloadFn = orig })
	podman.DaemonReloadFn = func() error { return nil }

	// Simulate a user who installed postgres before a future YAML flip:
	// CanonicalVersion="17" pretends the user's install pre-dated whatever
	// the current YAML calls canonical. Reconcile must resolve against 17,
	// not the YAML's canonical, but keep the bare "postgres" name.
	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if cfg.Services == nil {
		cfg.Services = map[string]config.ServiceConfig{}
	}
	cfg.Services["postgres"] = config.ServiceConfig{CanonicalVersion: "17"}
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}

	if err := EnsureDefaultPresetQuadlet("postgres"); err != nil {
		t.Fatalf("EnsureDefaultPresetQuadlet: %v", err)
	}
	quadlet, err := os.ReadFile(filepath.Join(config.QuadletDir(), "lerd-postgres.container"))
	if err != nil {
		t.Fatalf("read quadlet: %v", err)
	}
	if !strings.Contains(string(quadlet), ":17-") {
		t.Errorf("pinned postgres must resolve to a :17- image, got:\n%s", string(quadlet))
	}
	if !strings.Contains(string(quadlet), "ContainerName=lerd-postgres\n") {
		t.Errorf("pinned postgres must keep bare ContainerName=lerd-postgres, got:\n%s", string(quadlet))
	}
	// Pin must not be overwritten by reconcile.
	cfg2, _ := config.LoadGlobal()
	if got := cfg2.Services["postgres"].CanonicalVersion; got != "17" {
		t.Errorf("reconcile must preserve existing CanonicalVersion=17, got %q", got)
	}
}

func TestCaptureReinstallSpec_emptyPresetVersionRejected(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	// mysql is multi-version, so a custom-service YAML with empty
	// preset_version is a corruption that would silently bump the user
	// off whatever tag they were running. captureReinstallSpec must
	// refuse rather than fall through to DefaultVersion.
	svc := &config.CustomService{
		Name:          "mysql",
		Image:         "docker.io/library/mysql:8.4.9",
		Family:        "mysql",
		PresetVersion: "",
	}
	if err := config.SaveCustomService(svc); err != nil {
		t.Fatalf("SaveCustomService: %v", err)
	}

	_, err := captureReinstallSpec("mysql")
	if err == nil {
		t.Fatal("expected error refusing reinstall with empty preset_version")
	}
	if !strings.Contains(err.Error(), "preset_version") {
		t.Errorf("error should mention preset_version: %v", err)
	}
}

func TestCaptureReinstallSpec_capturesImageForDefaultPreset(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))

	orig := podman.DaemonReloadFn
	t.Cleanup(func() { podman.DaemonReloadFn = orig })
	podman.DaemonReloadFn = func() error { return nil }

	// Plant a default-preset quadlet whose Image= differs from the preset's
	// canonical tag. captureReinstallSpec must read this off disk so the
	// reinstall preserves it across the RemoveService → install hop.
	if err := os.MkdirAll(config.QuadletDir(), 0o755); err != nil {
		t.Fatalf("mkdir QuadletDir: %v", err)
	}
	planted := "[Container]\nImage=docker.io/getmeili/meilisearch:v1.7.0\nContainerName=lerd-meilisearch\n"
	if err := os.WriteFile(filepath.Join(config.QuadletDir(), "lerd-meilisearch.container"), []byte(planted), 0o644); err != nil {
		t.Fatalf("write planted quadlet: %v", err)
	}

	spec, err := captureReinstallSpec("meilisearch")
	if err != nil {
		t.Fatalf("captureReinstallSpec: %v", err)
	}
	if spec.image != "docker.io/getmeili/meilisearch:v1.7.0" {
		t.Errorf("expected captured image v1.7.0, got %q", spec.image)
	}
}
