//go:build linux

package cli

import (
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/services"
)

// captureWriteMgr records every WriteServiceUnitIfChanged call so a test can
// assert which unit name and content the production code wrote.
type captureWriteMgr struct {
	stopTrackingMgr
	writes      []writeCall
	enabled     []string
	writeChange bool
}

type writeCall struct {
	name    string
	content string
}

func (c *captureWriteMgr) WriteServiceUnitIfChanged(name, content string) (bool, error) {
	c.writes = append(c.writes, writeCall{name: name, content: content})
	return c.writeChange, nil
}
func (c *captureWriteMgr) Enable(name string) error {
	c.enabled = append(c.enabled, name)
	return nil
}

func swapServiceMgr(t *testing.T, m services.ServiceManager) {
	t.Helper()
	prev := services.Mgr
	services.Mgr = m
	t.Cleanup(func() { services.Mgr = prev })
}

// TestRestoreWorker_parentPath_writesParentUnit pins the regression-free
// case: a parent-path restore should produce lerd-<worker>-<site> exactly
// like before the workerUnitName refactor.
func TestRestoreWorker_parentPath_writesParentUnit(t *testing.T) {
	registerSite(t, "ws", "/p/ws")
	mgr := &captureWriteMgr{writeChange: true}
	swapServiceMgr(t, mgr)

	w := config.FrameworkWorker{Command: "npm run dev", Host: true, Label: "Vite"}
	restoreWorker("ws", "/p/ws", "8.4", "vite", w)

	if len(mgr.writes) == 0 {
		t.Fatal("expected at least one WriteServiceUnitIfChanged call")
	}
	if got := mgr.writes[0].name; got != "lerd-vite-ws" {
		t.Errorf("got unit %q, want %q", got, "lerd-vite-ws")
	}
	if !strings.Contains(mgr.writes[0].content, "WorkingDirectory=/p/ws") {
		t.Errorf("expected WorkingDirectory=/p/ws in unit content, got %q", mgr.writes[0].content)
	}
}

// TestRestoreWorker_worktreePath_writesSuffixedUnit pins the fix: when
// restoreWorker is invoked for a worktree path under the parent site, the
// produced unit must be lerd-<worker>-<site>-<wtBase> with WorkingDirectory
// set to the worktree path. Pre-fix, restoreWorker built the parent-shaped
// unit name from siteName alone, so per-worktree workers couldn't survive
// a daemon restart cleanly.
func TestRestoreWorker_worktreePath_writesSuffixedUnit(t *testing.T) {
	registerSite(t, "ws", "/p/ws")
	mgr := &captureWriteMgr{writeChange: true}
	swapServiceMgr(t, mgr)

	w := config.FrameworkWorker{Command: "npm run dev", Host: true, Label: "Vite"}
	restoreWorker("ws", "/p/ws/main", "8.4", "vite", w)

	if len(mgr.writes) == 0 {
		t.Fatal("expected at least one WriteServiceUnitIfChanged call")
	}
	if got := mgr.writes[0].name; got != "lerd-vite-ws-main" {
		t.Errorf("got unit %q, want %q", got, "lerd-vite-ws-main")
	}
	if !strings.Contains(mgr.writes[0].content, "WorkingDirectory=/p/ws/main") {
		t.Errorf("expected WorkingDirectory=/p/ws/main in unit content, got %q", mgr.writes[0].content)
	}
	// The systemd Description should reflect the worktree label so users
	// can tell parent and worktree units apart in journalctl / systemctl.
	if !strings.Contains(mgr.writes[0].content, "ws/main") {
		t.Errorf("expected ws/main label in unit content for worktree, got %q", mgr.writes[0].content)
	}
}
