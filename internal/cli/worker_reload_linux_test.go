//go:build linux

package cli

import (
	"testing"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// fakeLifecycle satisfies podman.UnitLifecycle so StartUnit does not touch the
// real systemd manager during the test.
type fakeLifecycle struct{}

func (fakeLifecycle) Start(string) error                { return nil }
func (fakeLifecycle) Stop(string) error                 { return nil }
func (fakeLifecycle) Restart(string) error              { return nil }
func (fakeLifecycle) UnitStatus(string) (string, error) { return "active", nil }
func (fakeLifecycle) AllUnitStates() map[string]string  { return nil }

func stubStartAndReload(t *testing.T) *int {
	t.Helper()
	prevLC := podman.UnitLifecycle
	podman.UnitLifecycle = fakeLifecycle{}
	t.Cleanup(func() { podman.UnitLifecycle = prevLC })

	reloads := 0
	prevReload := podman.DaemonReloadFn
	podman.DaemonReloadFn = func() error { reloads++; return nil }
	t.Cleanup(func() { podman.DaemonReloadFn = prevReload })
	return &reloads
}

// A worker unit whose content changed (e.g. a runtime switch re-pointed it at a
// different container) must trigger a daemon-reload before enable/start, or
// systemd keeps acting on the stale cached unit.
func TestWorkerStartForSite_reloadsWhenUnitChanged(t *testing.T) {
	registerSite(t, "ws", "/p/ws")
	mgr := &captureWriteMgr{writeChange: true}
	swapServiceMgr(t, mgr)
	reloads := stubStartAndReload(t)

	w := config.FrameworkWorker{Command: "npm run dev", Host: true, Label: "Vite"}
	if err := WorkerStartForSite("ws", "/p/ws", "8.4", "vite", w, false); err != nil {
		t.Fatalf("WorkerStartForSite: %v", err)
	}

	if *reloads == 0 {
		t.Error("expected a daemon-reload after the changed unit, got none")
	}
	if len(mgr.enabled) == 0 {
		t.Error("expected the unit to be enabled after reload")
	}
}

// An unchanged unit must not reload: nothing on disk moved, so the cached unit
// is already correct and a reload would be wasted churn.
func TestWorkerStartForSite_noReloadWhenUnchanged(t *testing.T) {
	registerSite(t, "ws", "/p/ws")
	mgr := &captureWriteMgr{writeChange: false}
	swapServiceMgr(t, mgr)
	reloads := stubStartAndReload(t)

	w := config.FrameworkWorker{Command: "npm run dev", Host: true, Label: "Vite"}
	if err := WorkerStartForSite("ws", "/p/ws", "8.4", "vite", w, false); err != nil {
		t.Fatalf("WorkerStartForSite: %v", err)
	}

	if *reloads != 0 {
		t.Errorf("expected no daemon-reload for an unchanged unit, got %d", *reloads)
	}
}
