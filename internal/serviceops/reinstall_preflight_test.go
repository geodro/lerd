package serviceops

import (
	"errors"
	"os"
	"path/filepath"
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

func TestReinstallService_SuppressesFamilyRegenDuringRemove(t *testing.T) {
	// The regen-during-remove was racing with the post-install regen
	// (rendering a plist without the service being reinstalled, then
	// restarting consumers against the partial plist). Reinstall must
	// pass SkipFamilyRegen so the install's own regen does the work
	// once, after the new YAML is on disk.
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	stubPodmanRemove(t)
	stubReinstallSeams(t)

	regenCalls := 0
	prev := removeRegenerateFamilyFn
	removeRegenerateFamilyFn = func(string) { regenCalls++ }
	t.Cleanup(func() { removeRegenerateFamilyFn = prev })

	svc := &config.CustomService{
		Name:          "mariadb-10-11",
		Image:         "docker.io/library/mariadb:10.11",
		Preset:        "mariadb",
		PresetVersion: "10.11",
		Family:        "mariadb",
	}
	if err := config.SaveCustomService(svc); err != nil {
		t.Fatalf("SaveCustomService: %v", err)
	}

	if err := ReinstallService("mariadb-10-11", false, func(PhaseEvent) {}); err != nil {
		t.Fatalf("ReinstallService: %v", err)
	}
	if regenCalls != 0 {
		t.Errorf("RegenerateFamilyConsumers must not fire during RemoveService on the reinstall path, got %d calls", regenCalls)
	}
}

func TestReinstallService_DefaultPreset_TriggersExplicitFamilyRegen(t *testing.T) {
	// EnsureDefaultPresetQuadletPinned does not regen family consumers,
	// so ReinstallService must do it explicitly after the install step.
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	stubPodmanRemove(t)
	rec := stubReinstallSeams(t)

	// mysql is a default preset in the embedded bundle. Save a YAML so
	// captureReinstallSpec's custom-service branch fires; routing falls
	// through to the default-preset path because IsDefaultPreset("mysql")
	// returns true.
	svc := &config.CustomService{
		Name:          "mysql",
		Image:         "docker.io/library/mysql:8.4",
		Preset:        "mysql",
		PresetVersion: "8.4",
		Family:        "mysql",
	}
	if err := config.SaveCustomService(svc); err != nil {
		t.Fatalf("SaveCustomService: %v", err)
	}

	if err := ReinstallService("mysql", false, func(PhaseEvent) {}); err != nil {
		t.Fatalf("ReinstallService: %v", err)
	}
	if len(rec.familyRegenCalls) != 1 || rec.familyRegenCalls[0] != "mysql" {
		t.Errorf("default-preset reinstall should call family regen once with %q, got %v", "mysql", rec.familyRegenCalls)
	}
}

func TestReinstallService_InstallFailure_TriggersFamilyRegen(t *testing.T) {
	// When the install step fails, consumers' plists still reference
	// the now-deleted target. ReinstallService must sync them to the
	// post-Remove "target gone" state.
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	stubPodmanRemove(t)
	rec := stubReinstallSeams(t)
	rec.installErr = errors.New("simulated install failure")

	svc := &config.CustomService{
		Name:          "mariadb-10-11",
		Image:         "docker.io/library/mariadb:10.11",
		Preset:        "mariadb",
		PresetVersion: "10.11",
		Family:        "mariadb",
	}
	if err := config.SaveCustomService(svc); err != nil {
		t.Fatalf("SaveCustomService: %v", err)
	}

	err := ReinstallService("mariadb-10-11", false, func(PhaseEvent) {})
	if err == nil {
		t.Fatal("expected install failure to propagate")
	}
	if len(rec.familyRegenCalls) != 1 || rec.familyRegenCalls[0] != "mariadb-10-11" {
		t.Errorf("install failure should call family regen once with %q, got %v", "mariadb-10-11", rec.familyRegenCalls)
	}
}

func TestReinstallService_CustomServiceSuccess_NoExtraFamilyRegen(t *testing.T) {
	// For a custom-service reinstall that succeeds, InstallPresetByName's
	// internal regen handles family consumers. ReinstallService must NOT
	// fire its explicit regen (which would double-regen).
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	stubPodmanRemove(t)
	rec := stubReinstallSeams(t)

	svc := &config.CustomService{
		Name:          "mariadb-10-11",
		Image:         "docker.io/library/mariadb:10.11",
		Preset:        "mariadb",
		PresetVersion: "10.11",
		Family:        "mariadb",
	}
	if err := config.SaveCustomService(svc); err != nil {
		t.Fatalf("SaveCustomService: %v", err)
	}

	if err := ReinstallService("mariadb-10-11", false, func(PhaseEvent) {}); err != nil {
		t.Fatalf("ReinstallService: %v", err)
	}
	if len(rec.familyRegenCalls) != 0 {
		t.Errorf("custom-service success should not call the explicit family regen, got %v", rec.familyRegenCalls)
	}
}

func TestRemoveService_SkipFamilyRegen_HonoursFlag(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	stubPodmanRemove(t)

	regenCalls := 0
	prev := removeRegenerateFamilyFn
	removeRegenerateFamilyFn = func(string) { regenCalls++ }
	t.Cleanup(func() { removeRegenerateFamilyFn = prev })

	mkSvc := func(name string) {
		svc := &config.CustomService{
			Name:   name,
			Image:  "docker.io/test/" + name + ":latest",
			Family: "regen-family",
		}
		if err := config.SaveCustomService(svc); err != nil {
			t.Fatalf("SaveCustomService %s: %v", name, err)
		}
	}

	mkSvc("regen-target")
	if err := RemoveService("regen-target", RemoveOptions{SkipFamilyRegen: true}, func(PhaseEvent) {}); err != nil {
		t.Fatalf("RemoveService: %v", err)
	}
	if regenCalls != 0 {
		t.Errorf("SkipFamilyRegen=true should suppress regen, got %d calls", regenCalls)
	}

	mkSvc("regen-target-2")
	if err := RemoveService("regen-target-2", RemoveOptions{}, func(PhaseEvent) {}); err != nil {
		t.Fatalf("RemoveService: %v", err)
	}
	if regenCalls != 1 {
		t.Errorf("default RemoveOptions should regen, got %d calls", regenCalls)
	}
}

func TestMissingPresetDependencies_BuiltinNotInstalled_Reports(t *testing.T) {
	// Pre-fix: any IsBuiltin(dep) dep was treated as always-satisfied,
	// so a phpmyadmin reinstall would pass validate even with mysql
	// uninstalled, then crash in EnsureServiceRunning after RemoveService
	// had already wiped phpmyadmin. The fix: built-in deps must have a
	// quadlet on disk to count as installed.
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	svc := &config.CustomService{
		Name:      "phpmyadmin",
		DependsOn: []string{"mysql"}, // mysql is a default preset (IsBuiltin true)
	}
	// No mysql quadlet on disk -> mysql is genuinely missing.
	if missing := MissingPresetDependencies(svc); len(missing) != 1 || missing[0] != "mysql" {
		t.Errorf("with no mysql quadlet, expected [mysql] missing, got %v", missing)
	}
}

func TestMissingPresetDependencies_BuiltinInstalled_OK(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	// Materialise a lerd-mysql.container so podman.QuadletInstalled returns true.
	quadletDir := config.QuadletDir()
	if err := os.MkdirAll(quadletDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(quadletDir, "lerd-mysql.container"), []byte("[Container]\nImage=docker.io/library/mysql:8\n"), 0644); err != nil {
		t.Fatal(err)
	}

	svc := &config.CustomService{
		Name:      "phpmyadmin",
		DependsOn: []string{"mysql"},
	}
	if missing := MissingPresetDependencies(svc); len(missing) != 0 {
		t.Errorf("with lerd-mysql quadlet present, expected no missing deps, got %v", missing)
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

func TestReinstallService_ForwardsPresetNameToInstallSeam(t *testing.T) {
	// Composition-level check: ReinstallService passes spec.presetName
	// (NOT the service name) to the install seam.
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

func TestRealReinstallInstall_CustomServicePath_CallsStreamingWithPresetName(t *testing.T) {
	// Bug-fix coverage at the seam closest to the actual one-line fix:
	// realReinstallInstall's custom-service branch must invoke the
	// streaming fn with the PRESET name, not the service name. Pre-fix
	// the call passed `name` ("mariadb-10-11") and InstallPresetStreaming
	// errored with "unknown preset" AFTER RemoveService had run.
	var captured struct {
		name    string
		version string
	}
	prev := reinstallStreamingFn
	reinstallStreamingFn = func(name, version string, emit func(PhaseEvent)) (*config.CustomService, error) {
		captured.name = name
		captured.version = version
		return &config.CustomService{Name: name}, nil
	}
	t.Cleanup(func() { reinstallStreamingFn = prev })

	spec := reinstallSpec{
		presetName: "mariadb",
		version:    "10.11",
		image:      "docker.io/library/mariadb:10.11",
	}
	if _, err := realReinstallInstall("mariadb-10-11", spec, func(PhaseEvent) {}); err != nil {
		t.Fatalf("realReinstallInstall: %v", err)
	}
	if captured.name != "mariadb" {
		t.Errorf("streaming fn name = %q, want %q (preset name, NOT service name)", captured.name, "mariadb")
	}
	if captured.version != "10.11" {
		t.Errorf("streaming fn version = %q, want %q", captured.version, "10.11")
	}
}
