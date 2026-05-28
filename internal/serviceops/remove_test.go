package serviceops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// stubPodmanRemove swaps the podman seams used by RemoveService for in-memory
// fakes and restores them on test cleanup. Returns a recorder so individual
// tests can introspect what RemoveService called.
type podmanRemoveRecorder struct {
	stopped         []string
	removed         []string
	removedQuadlets []string
	daemonReloads   int

	stopErr       error
	removeQuadErr error
	currentStatus string
}

func stubPodmanRemove(t *testing.T) *podmanRemoveRecorder {
	t.Helper()
	rec := &podmanRemoveRecorder{currentStatus: "active"}

	prevStop := removeStopUnit
	prevRm := removeContainerFn
	prevRmq := removeQuadletFn
	prevStatus := removeUnitStatusFn
	prevDaemon := podman.DaemonReloadFn

	removeStopUnit = func(name string) error {
		rec.stopped = append(rec.stopped, name)
		return rec.stopErr
	}
	removeContainerFn = func(name string) {
		rec.removed = append(rec.removed, name)
	}
	removeQuadletFn = func(name string) error {
		rec.removedQuadlets = append(rec.removedQuadlets, name)
		return rec.removeQuadErr
	}
	removeUnitStatusFn = func(name string) (string, error) {
		return rec.currentStatus, nil
	}
	podman.DaemonReloadFn = func() error { rec.daemonReloads++; return nil }

	t.Cleanup(func() {
		removeStopUnit = prevStop
		removeContainerFn = prevRm
		removeQuadletFn = prevRmq
		removeUnitStatusFn = prevStatus
		podman.DaemonReloadFn = prevDaemon
	})
	return rec
}

func mkTestDataDir(t *testing.T, name string, contents string) string {
	t.Helper()
	dir := config.DataSubDir(name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	marker := filepath.Join(dir, "marker.txt")
	if err := os.WriteFile(marker, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", marker, err)
	}
	return dir
}

func TestRemoveService_NoData_PreservesDataDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	stubPodmanRemove(t)

	dir := mkTestDataDir(t, "redis", "important")

	var events []PhaseEvent
	if err := RemoveService("redis", RemoveOptions{RemoveData: false}, func(e PhaseEvent) {
		events = append(events, e)
	}); err != nil {
		t.Fatalf("RemoveService: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "marker.txt")); err != nil {
		t.Errorf("data marker should still exist when RemoveData=false: %v", err)
	}

	gotPhases := map[string]bool{}
	for _, e := range events {
		gotPhases[e.Phase] = true
	}
	if gotPhases["removing_data"] {
		t.Errorf("removing_data phase emitted when RemoveData=false")
	}
	if !gotPhases["done"] {
		t.Errorf("expected done phase, got %v", events)
	}
}

func TestRemoveService_WithData_RenamesAside(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	stubPodmanRemove(t)

	dir := mkTestDataDir(t, "mariadb", "user-data")

	if err := RemoveService("mariadb", RemoveOptions{RemoveData: true}, func(PhaseEvent) {}); err != nil {
		t.Fatalf("RemoveService: %v", err)
	}

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("live data dir should be gone, stat err = %v", err)
	}

	parent := filepath.Dir(dir)
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var aside string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "mariadb.pre-remove-") {
			aside = filepath.Join(parent, e.Name())
			break
		}
	}
	if aside == "" {
		t.Fatalf("expected mariadb.pre-remove-<ts> sibling in %s, got %v", parent, entries)
	}
	body, err := os.ReadFile(filepath.Join(aside, "marker.txt"))
	if err != nil || string(body) != "user-data" {
		t.Errorf("rename-aside should preserve marker contents, got body=%q err=%v", body, err)
	}
}

func TestRemoveService_WithData_DeletesTuningOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	stubPodmanRemove(t)

	mkTestDataDir(t, "mariadb", "user-data")
	// Seed a tuning override the way MaterializeServiceTuning would.
	tuningPath := config.ServiceTuningFile("mariadb")
	if err := os.MkdirAll(filepath.Dir(tuningPath), 0o755); err != nil {
		t.Fatalf("mkdir tuning dir: %v", err)
	}
	if err := os.WriteFile(tuningPath, []byte("max_allowed_packet = 1G\n"), 0o644); err != nil {
		t.Fatalf("seed tuning file: %v", err)
	}

	if err := RemoveService("mariadb", RemoveOptions{RemoveData: true}, func(PhaseEvent) {}); err != nil {
		t.Fatalf("RemoveService: %v", err)
	}

	if _, err := os.Stat(tuningPath); !os.IsNotExist(err) {
		t.Errorf("tuning override must be removed when RemoveData=true, stat err = %v", err)
	}
}

func TestRemoveService_WithoutData_KeepsTuningOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	stubPodmanRemove(t)

	mkTestDataDir(t, "mariadb", "user-data")
	tuningPath := config.ServiceTuningFile("mariadb")
	if err := os.MkdirAll(filepath.Dir(tuningPath), 0o755); err != nil {
		t.Fatalf("mkdir tuning dir: %v", err)
	}
	if err := os.WriteFile(tuningPath, []byte("max_allowed_packet = 1G\n"), 0o644); err != nil {
		t.Fatalf("seed tuning file: %v", err)
	}

	if err := RemoveService("mariadb", RemoveOptions{RemoveData: false}, func(PhaseEvent) {}); err != nil {
		t.Fatalf("RemoveService: %v", err)
	}

	// `service remove` without --purge keeps user state so a subsequent
	// install picks up where you left off; the tuning override is part of
	// that state.
	if _, err := os.Stat(tuningPath); err != nil {
		t.Errorf("tuning override must survive RemoveData=false, stat err = %v", err)
	}
}

func TestRemoveService_StopFailureAborts_DataUntouched(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	rec := stubPodmanRemove(t)
	rec.stopErr = errors.New("systemctl stop boom")

	dir := mkTestDataDir(t, "postgres", "do not touch")

	err := RemoveService("postgres", RemoveOptions{RemoveData: true}, func(PhaseEvent) {})
	if err == nil {
		t.Fatal("expected error when StopUnit fails")
	}

	body, readErr := os.ReadFile(filepath.Join(dir, "marker.txt"))
	if readErr != nil || string(body) != "do not touch" {
		t.Errorf("data must be untouched on stop failure, body=%q err=%v", body, readErr)
	}
	if len(rec.removedQuadlets) != 0 {
		t.Errorf("quadlet must not be removed when stop failed: %v", rec.removedQuadlets)
	}
}

func TestRemoveService_DefaultPresetSucceeds(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	rec := stubPodmanRemove(t)

	mkTestDataDir(t, "postgres", "x")

	if !config.IsDefaultPreset("postgres") {
		t.Skip("postgres is not a default preset on this build")
	}

	if err := RemoveService("postgres", RemoveOptions{RemoveData: false}, func(PhaseEvent) {}); err != nil {
		t.Fatalf("RemoveService(postgres) for default preset: %v", err)
	}
	if got := rec.removed; len(got) != 1 || got[0] != "lerd-postgres" {
		t.Errorf("expected RemoveContainer(\"lerd-postgres\"), got %v", got)
	}
	if got := rec.removedQuadlets; len(got) != 1 || got[0] != "lerd-postgres" {
		t.Errorf("expected RemoveQuadlet(\"lerd-postgres\"), got %v", got)
	}
}

// TestRemoveService_OrphanQuadlet_NoYAML_Succeeds covers the recovery path for
// the orphan-quadlet bug: a service whose YAML config is missing but whose
// .container quadlet still exists must be removable so users can fully clean
// up the drift that motivated unifying installed-detection on the quadlet.
func TestRemoveService_OrphanQuadlet_NoYAML_Succeeds(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	rec := stubPodmanRemove(t)

	qdir := config.QuadletDir()
	if err := os.MkdirAll(qdir, 0o755); err != nil {
		t.Fatalf("mkdir quadlet dir: %v", err)
	}
	quadletPath := filepath.Join(qdir, "lerd-mysql.container")
	if err := os.WriteFile(quadletPath, []byte("[Container]\nImage=docker.io/library/mysql:8.4\n"), 0o644); err != nil {
		t.Fatalf("write quadlet: %v", err)
	}
	if !ServiceInstalled("mysql") {
		t.Fatalf("precondition: ServiceInstalled should report true with quadlet on disk")
	}
	if _, err := config.LoadCustomService("mysql"); err == nil {
		t.Fatalf("precondition: no YAML expected for mysql in this temp tree")
	}

	if err := RemoveService("mysql", RemoveOptions{RemoveData: false}, func(PhaseEvent) {}); err != nil {
		t.Fatalf("RemoveService for orphan quadlet should not error: %v", err)
	}
	if got := rec.removedQuadlets; len(got) != 1 || got[0] != "lerd-mysql" {
		t.Errorf("expected RemoveQuadlet(\"lerd-mysql\"), got %v", got)
	}
}

func TestRemoveService_InactiveUnit_SkipsStop(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	rec := stubPodmanRemove(t)
	rec.currentStatus = "inactive"

	if err := RemoveService("redis", RemoveOptions{}, func(PhaseEvent) {}); err != nil {
		t.Fatalf("RemoveService: %v", err)
	}
	if len(rec.stopped) != 0 {
		t.Errorf("StopUnit should not be invoked when status=inactive, got %v", rec.stopped)
	}
}

func TestRemoveService_PhaseOrder(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	stubPodmanRemove(t)
	mkTestDataDir(t, "mariadb", "x")

	var events []PhaseEvent
	if err := RemoveService("mariadb", RemoveOptions{RemoveData: true}, func(e PhaseEvent) {
		events = append(events, e)
	}); err != nil {
		t.Fatalf("RemoveService: %v", err)
	}

	want := []string{"stopping_unit", "removing_container", "removing_data", "removing_quadlet", "removing_config", "regenerating_consumers", "done"}
	if len(events) != len(want) {
		t.Fatalf("phase count mismatch: got %d want %d (events=%v)", len(events), len(want), events)
	}
	for i, p := range want {
		if events[i].Phase != p {
			t.Errorf("phase[%d] = %q, want %q (events=%v)", i, events[i].Phase, p, events)
		}
	}
}

// Ensure the package documentation example for nil-safe emit holds: callers
// that pass nil for emit must not crash.
func TestRemoveService_NilEmit(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	stubPodmanRemove(t)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("RemoveService panicked with nil emit: %v", r)
		}
	}()
	if err := RemoveService("redis", RemoveOptions{}, nil); err != nil {
		t.Fatalf("RemoveService nil-emit: %v", err)
	}
}

// TestRenameDataAside_CrossDeviceFallbackPreservesData simulates the EXDEV
// branch by faking the Rename so it always returns a *os.LinkError wrapping
// syscall.EXDEV, even though src and dst live on the same tmpfs. The
// fallback must copy the directory aside before deleting the source, not
// silently os.RemoveAll the data.
func TestRenameDataAside_CrossDeviceFallbackPreservesData(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	dir := mkTestDataDir(t, "redis", "preserve-me")

	// Force the rename to return EXDEV so we exercise the cross-device path.
	prev := osRenameFn
	osRenameFn = func(_, _ string) error {
		return &os.LinkError{Op: "rename", Old: "x", New: "y", Err: syscall.EXDEV}
	}
	t.Cleanup(func() { osRenameFn = prev })

	if err := renameDataAside(dir); err != nil {
		t.Fatalf("renameDataAside: %v", err)
	}

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("source dir should be removed after copy, stat err=%v", err)
	}
	parent := filepath.Dir(dir)
	entries, _ := os.ReadDir(parent)
	var aside string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "redis.pre-remove-") {
			aside = filepath.Join(parent, e.Name())
		}
	}
	if aside == "" {
		t.Fatalf("expected rename-aside dir under %s, got %v", parent, entries)
	}
	body, err := os.ReadFile(filepath.Join(aside, "marker.txt"))
	if err != nil || string(body) != "preserve-me" {
		t.Errorf("EXDEV fallback must preserve content, got body=%q err=%v", body, err)
	}
}

// Sanity check: rename-aside timestamp suffix is unique enough that two
// successive removes don't collide on the same tick.
func TestRemoveService_RenameAside_DistinctTimestamps(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	stubPodmanRemove(t)

	for i := 0; i < 2; i++ {
		mkTestDataDir(t, "redis", fmt.Sprintf("run-%d", i))
		if err := RemoveService("redis", RemoveOptions{RemoveData: true}, func(PhaseEvent) {}); err != nil {
			t.Fatalf("RemoveService run %d: %v", i, err)
		}
	}

	parent := filepath.Dir(config.DataSubDir("redis"))
	entries, _ := os.ReadDir(parent)
	count := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "redis.pre-remove-") {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected 2 distinct rename-aside dirs, got %d (%v)", count, entries)
	}
}
