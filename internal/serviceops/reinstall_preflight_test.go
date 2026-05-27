package serviceops

import (
	"errors"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// stubPreflightSeams installs configurable pre-flight seams plus permissive
// install/reprov fakes, so each test can verify that pre-flight failure
// short-circuits before RemoveService deletes anything.
func stubPreflightSeams(t *testing.T, validateErr, prefetchErr error) *reinstallRecorder {
	t.Helper()
	rec := stubReinstallSeams(t)
	prevValidate := reinstallValidateFn
	prevPrefetch := reinstallPrefetchImageFn
	reinstallValidateFn = func(string, reinstallSpec) error { return validateErr }
	reinstallPrefetchImageFn = func(string, func(PhaseEvent)) error { return prefetchErr }
	t.Cleanup(func() {
		reinstallValidateFn = prevValidate
		reinstallPrefetchImageFn = prevPrefetch
	})
	return rec
}

func TestReinstallService_PreflightValidateFailure_LeavesYAMLIntact(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	stubPodmanRemove(t)
	rec := stubPreflightSeams(t, errors.New("unknown preset"), nil)

	saveCustomServiceForReinstall(t, "myservice", "1.2.3")

	err := ReinstallService("myservice", false, func(PhaseEvent) {})
	if err == nil || !strings.Contains(err.Error(), "unknown preset") {
		t.Fatalf("expected pre-flight validate error, got %v", err)
	}
	if _, err := config.LoadCustomService("myservice"); err != nil {
		t.Errorf("YAML must survive a pre-flight validate failure, got %v", err)
	}
	if len(rec.installCalls) != 0 {
		t.Errorf("install must not run after pre-flight failure, got %v", rec.installCalls)
	}
}

func TestReinstallService_PreflightPullFailure_LeavesYAMLIntact(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	stubPodmanRemove(t)
	rec := stubPreflightSeams(t, nil, errors.New("registry unreachable"))

	saveCustomServiceForReinstall(t, "myservice", "1.2.3")

	err := ReinstallService("myservice", false, func(PhaseEvent) {})
	if err == nil || !strings.Contains(err.Error(), "registry unreachable") {
		t.Fatalf("expected pre-flight pull error, got %v", err)
	}
	if _, err := config.LoadCustomService("myservice"); err != nil {
		t.Errorf("YAML must survive a pre-flight pull failure, got %v", err)
	}
	if len(rec.installCalls) != 0 {
		t.Errorf("install must not run after pre-flight failure, got %v", rec.installCalls)
	}
}

func TestCaptureReinstallSpec_UsesPresetFieldNotServiceName(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	// Non-canonical-version service: name "mariadb-10-11", preset "mariadb".
	// captureReinstallSpec must pick up Preset from YAML so the install path
	// loads the real preset, not the synthesised service name.
	svc := &config.CustomService{
		Name:          "mariadb-10-11",
		Image:         "docker.io/library/mariadb:10.11",
		Preset:        "mariadb",
		PresetVersion: "10.11",
	}
	if err := config.SaveCustomService(svc); err != nil {
		t.Fatalf("SaveCustomService: %v", err)
	}

	spec, err := captureReinstallSpec("mariadb-10-11")
	if err != nil {
		t.Fatalf("captureReinstallSpec: %v", err)
	}
	if spec.presetName != "mariadb" {
		t.Errorf("presetName = %q, want mariadb (from Preset field, not service name)", spec.presetName)
	}
	if spec.version != "10.11" {
		t.Errorf("version = %q, want 10.11", spec.version)
	}
}

func TestCaptureReinstallSpec_BackfillsPresetFromName_WhenYAMLLacksField(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	// Legacy YAML written before the Preset field existed: fall back to
	// the service name itself, which is the canonical preset name for
	// single-version presets.
	svc := &config.CustomService{
		Name:  "selenium",
		Image: "docker.io/selenium/standalone-chrome:latest",
	}
	if err := config.SaveCustomService(svc); err != nil {
		t.Fatalf("SaveCustomService: %v", err)
	}

	spec, err := captureReinstallSpec("selenium")
	if err != nil {
		t.Fatalf("captureReinstallSpec: %v", err)
	}
	if spec.presetName != "selenium" {
		t.Errorf("presetName = %q, want selenium (backfilled from name)", spec.presetName)
	}
}

func TestRealReinstallInstall_CustomServicePath_RoutesViaPresetName(t *testing.T) {
	// Verifies the bug fix: when reinstalling a non-canonical-version
	// service, the install seam must receive spec.presetName="mariadb"
	// (the source preset) so realReinstallInstall can call
	// InstallPresetStreaming with the correct preset name, instead of
	// "mariadb-10-11" which is not a preset and fails after RemoveService.
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	rec := stubReinstallSeams(t)
	svc := &config.CustomService{
		Name:          "mariadb-10-11",
		Image:         "docker.io/library/mariadb:10.11",
		Preset:        "mariadb",
		PresetVersion: "10.11",
	}
	if err := config.SaveCustomService(svc); err != nil {
		t.Fatalf("SaveCustomService: %v", err)
	}
	stubPodmanRemove(t)

	if err := ReinstallService("mariadb-10-11", false, func(PhaseEvent) {}); err != nil {
		t.Fatalf("ReinstallService: %v", err)
	}
	if len(rec.installCalls) != 1 {
		t.Fatalf("expected 1 install call, got %d", len(rec.installCalls))
	}
	call := rec.installCalls[0]
	if call.Name != "mariadb-10-11" {
		t.Errorf("install Name = %q, want mariadb-10-11 (the service being reinstalled)", call.Name)
	}
	if call.PresetName != "mariadb" {
		t.Errorf("install PresetName = %q, want mariadb (the SOURCE preset, not the service name)", call.PresetName)
	}
	if call.Version != "10.11" {
		t.Errorf("install Version = %q, want 10.11", call.Version)
	}
}
