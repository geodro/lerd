package serviceops

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// reconcileEnv sets up an isolated HOME/XDG tree and a no-op daemon reload so
// ReconcileServices can write/read quadlets without touching the real host.
func reconcileEnv(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	orig := podman.DaemonReloadFn
	t.Cleanup(func() { podman.DaemonReloadFn = orig })
	podman.DaemonReloadFn = func() error { return nil }
}

// writeQuadlet writes a quadlet for name; marked=true tags it as a managed
// service (what GenerateCustomQuadlet does), marked=false mimics a site/worker
// quadlet that reconcile must never touch.
func writeQuadlet(t *testing.T, name string, marked bool) {
	t.Helper()
	dir := config.QuadletDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir quadlet dir: %v", err)
	}
	body := "[Container]\nImage=docker.io/example/" + name + ":latest\n"
	if marked {
		body = podman.CustomServiceQuadletMarker + "\n" + body
	}
	if err := os.WriteFile(filepath.Join(dir, "lerd-"+name+".container"), []byte(body), 0o644); err != nil {
		t.Fatalf("write quadlet %s: %v", name, err)
	}
}

// TestReconcileServices_forwardHealsMissingQuadlet: a service whose YAML exists
// but whose quadlet is missing gets its quadlet regenerated (issue #678).
func TestReconcileServices_forwardHealsMissingQuadlet(t *testing.T) {
	reconcileEnv(t)

	if err := config.SaveCustomService(&config.CustomService{Name: "gotenberg", Image: "docker.io/gotenberg/gotenberg:8"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if podman.QuadletInstalled("lerd-gotenberg") {
		t.Fatalf("precondition: quadlet should not exist yet")
	}

	res, err := ReconcileServices(nil)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !slices.Contains(res.QuadletsRegenerated, "gotenberg") {
		t.Fatalf("expected gotenberg in QuadletsRegenerated, got %+v", res)
	}
	if !podman.QuadletInstalled("lerd-gotenberg") {
		t.Fatalf("expected quadlet regenerated from the YAML")
	}
}

// TestReconcileServices_removesMarkedOrphans: a marked service quadlet with no
// backing YAML is an orphan and is removed, whether or not its name is a preset
// (covers the user-named orphan gap, issue #678 #4).
func TestReconcileServices_removesMarkedOrphans(t *testing.T) {
	reconcileEnv(t)
	rec := stubPodmanRemove(t)
	rec.currentStatus = "inactive"

	writeQuadlet(t, "gotenberg", true)   // preset-named orphan
	writeQuadlet(t, "acme-widget", true) // user-named orphan

	res, err := ReconcileServices(nil)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	for _, want := range []string{"gotenberg", "acme-widget"} {
		if !slices.Contains(res.OrphansRemoved, want) {
			t.Fatalf("expected %s in OrphansRemoved, got %+v", want, res)
		}
		if !slices.Contains(rec.removedQuadlets, "lerd-"+want) {
			t.Fatalf("expected RemoveService to drop lerd-%s, recorder: %+v", want, rec.removedQuadlets)
		}
	}
}

// TestReconcileServices_removesLegacyUnmarkedPresetOrphan: an orphan written by
// an older binary has no marker, but a preset-named quadlet with no YAML is
// still reaped via the preset-name fallback (the existing-drift migration case).
func TestReconcileServices_removesLegacyUnmarkedPresetOrphan(t *testing.T) {
	reconcileEnv(t)
	rec := stubPodmanRemove(t)
	rec.currentStatus = "inactive"

	writeQuadlet(t, "gotenberg", false) // legacy orphan, no marker

	res, err := ReconcileServices(nil)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !slices.Contains(res.OrphansRemoved, "gotenberg") {
		t.Fatalf("expected legacy unmarked preset orphan reaped, got %+v", res)
	}
}

// TestReconcileServices_sparesDefaultPreset: built-in default presets carry the
// marker but have no YAML by design, so they must not be treated as orphans.
func TestReconcileServices_sparesDefaultPreset(t *testing.T) {
	reconcileEnv(t)
	rec := stubPodmanRemove(t)

	if !config.IsDefaultPreset("mysql") {
		t.Fatalf("test premise broken: mysql must be a default preset")
	}
	writeQuadlet(t, "mysql", true)

	res, err := ReconcileServices(nil)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if slices.Contains(res.OrphansRemoved, "mysql") || slices.Contains(rec.removedQuadlets, "lerd-mysql") {
		t.Fatalf("default preset must never be reaped, res=%+v rec=%+v", res, rec.removedQuadlets)
	}
}

// TestReconcileServices_sparesUnmarkedQuadlet: a quadlet without the managed
// marker (e.g. a site container lerd-custom-<site>) must be left untouched.
func TestReconcileServices_sparesUnmarkedQuadlet(t *testing.T) {
	reconcileEnv(t)
	rec := stubPodmanRemove(t)

	writeQuadlet(t, "custom-gonitro", false) // site custom container, unmarked

	res, err := ReconcileServices(nil)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(res.OrphansRemoved) != 0 || len(rec.removedQuadlets) != 0 {
		t.Fatalf("unmarked quadlet must not be removed, res=%+v rec=%+v", res, rec.removedQuadlets)
	}
}

// TestReconcileServices_skipsRunningOrphan: an orphan whose container is still
// up must be left in place, not force-stopped and destroyed (issue #678 #2).
func TestReconcileServices_skipsRunningOrphan(t *testing.T) {
	reconcileEnv(t)
	rec := stubPodmanRemove(t)

	prev := orphanContainerRunningFn
	t.Cleanup(func() { orphanContainerRunningFn = prev })
	orphanContainerRunningFn = func(string) bool { return true }

	writeQuadlet(t, "gotenberg", true)

	res, err := ReconcileServices(nil)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !slices.Contains(res.RunningOrphansSkipped, "gotenberg") {
		t.Fatalf("expected gotenberg in RunningOrphansSkipped, got %+v", res)
	}
	if len(res.OrphansRemoved) != 0 || len(rec.removedQuadlets) != 0 {
		t.Fatalf("running orphan must not be removed, res=%+v rec=%+v", res, rec.removedQuadlets)
	}
}

// TestReconcileServices_continuesPastForwardError: one service that fails to
// regenerate must not block the other heals or the orphan-cleanup pass (#1).
func TestReconcileServices_continuesPastForwardError(t *testing.T) {
	reconcileEnv(t)
	rec := stubPodmanRemove(t)
	rec.currentStatus = "inactive"

	for _, n := range []string{"bad", "good"} {
		if err := config.SaveCustomService(&config.CustomService{Name: n, Image: "docker.io/example/" + n}); err != nil {
			t.Fatalf("save %s: %v", n, err)
		}
	}
	writeQuadlet(t, "gotenberg", true) // orphan that must still be reaped despite the error

	prev := ensureQuadletFn
	t.Cleanup(func() { ensureQuadletFn = prev })
	ensureQuadletFn = func(svc *config.CustomService) error {
		if svc.Name == "bad" {
			return errors.New("boom")
		}
		return prev(svc)
	}

	res, err := ReconcileServices(nil)
	if err == nil || !contains(err.Error(), "bad") {
		t.Fatalf("expected aggregated error mentioning bad, got: %v", err)
	}
	if !slices.Contains(res.QuadletsRegenerated, "good") {
		t.Fatalf("good service must still be regenerated despite bad failing, got %+v", res)
	}
	if !slices.Contains(res.OrphansRemoved, "gotenberg") {
		t.Fatalf("orphan cleanup must still run despite a forward error, got %+v", res)
	}
}

// TestReconcileServices_steadyStateNoop: a fully-installed service (YAML +
// marked quadlet) triggers neither a regeneration nor a removal.
func TestReconcileServices_steadyStateNoop(t *testing.T) {
	reconcileEnv(t)

	if err := config.SaveCustomService(&config.CustomService{Name: "gotenberg", Image: "docker.io/gotenberg/gotenberg:8"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	writeQuadlet(t, "gotenberg", true)

	res, err := ReconcileServices(nil)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(res.QuadletsRegenerated) != 0 || len(res.OrphansRemoved) != 0 {
		t.Fatalf("steady state must be a no-op, got %+v", res)
	}
}
