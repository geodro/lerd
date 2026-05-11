package cli

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// stopTrackingMgr extends fakeServiceMgr with call tracking for the stop /
// disable / remove paths exercised by WorkerStopForSite + StopAllWorkersForWorktree.
type stopTrackingMgr struct {
	fakeServiceMgr
	disableCalls       []string
	removeServiceCalls []string
	removeTimerCalls   []string
	listResults        map[string][]string
}

func (s *stopTrackingMgr) Disable(name string) error {
	s.disableCalls = append(s.disableCalls, name)
	return nil
}
func (s *stopTrackingMgr) RemoveServiceUnit(name string) error {
	s.removeServiceCalls = append(s.removeServiceCalls, name)
	return nil
}
func (s *stopTrackingMgr) RemoveTimerUnit(name string) error {
	s.removeTimerCalls = append(s.removeTimerCalls, name)
	return nil
}
func (s *stopTrackingMgr) ListServiceUnits(pattern string) []string {
	if s.listResults == nil {
		return nil
	}
	if out, ok := s.listResults[pattern]; ok {
		return out
	}
	// Allow callers to register a single canonical glob. If lerd ever
	// passes a different pattern we'd fail to match, surfacing a real bug
	// rather than silently returning empty.
	for _, v := range s.listResults {
		return v
	}
	return nil
}

// registerSite writes a sites.yaml so config.FindSite resolves the parent
// path used by workerUnitName / workerDisplaySite.
func registerSite(t *testing.T, name, path string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	dir := filepath.Join(tmp, "lerd")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	yaml := "sites:\n" +
		"    - name: " + name + "\n" +
		"      domains:\n" +
		"        - " + name + ".test\n" +
		"      path: " + path + "\n" +
		"      php_version: \"8.4\"\n" +
		"      node_version: \"22\"\n"
	if err := os.WriteFile(filepath.Join(dir, "sites.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	// Sanity check.
	if _, err := config.FindSite(name); err != nil {
		t.Fatalf("FindSite(%q): %v", name, err)
	}
}

// TestWorkerUnitName_parentPath pins that the parent unit name has no
// worktree suffix when sitePath equals the registered site path.
func TestWorkerUnitName_parentPath(t *testing.T) {
	registerSite(t, "ws", "/p/ws")
	got := workerUnitName("ws", "/p/ws", "vite")
	if got != "lerd-vite-ws" {
		t.Errorf("got %q, want %q", got, "lerd-vite-ws")
	}
}

// TestWorkerUnitName_worktreePath pins that worktree paths produce the
// suffixed unit name format used by per-worktree worker units.
func TestWorkerUnitName_worktreePath(t *testing.T) {
	registerSite(t, "ws", "/p/ws")
	got := workerUnitName("ws", "/p/ws/feat-x", "vite")
	if got != "lerd-vite-ws-feat-x" {
		t.Errorf("got %q, want %q", got, "lerd-vite-ws-feat-x")
	}
}

// TestWorkerUnitName_emptyPath is the safe fallback: callers that don't
// know the path get the parent unit name, matching pre-refactor behaviour.
func TestWorkerUnitName_emptyPath(t *testing.T) {
	registerSite(t, "ws", "/p/ws")
	got := workerUnitName("ws", "", "vite")
	if got != "lerd-vite-ws" {
		t.Errorf("got %q, want %q", got, "lerd-vite-ws")
	}
}

// TestStopAllWorkersForWorktree pins that every per-worktree unit attached
// to the given (site, worktree) pair is disabled and its unit file removed,
// while parent-site units and other-worktree units are left alone. This is
// the regression that #319 left behind: lerd worktree remove never stopped
// these units, so they restart-looped against a deleted WorkingDirectory.
func TestStopAllWorkersForWorktree(t *testing.T) {
	registerSite(t, "ws", "/p/ws")
	fake := &stopTrackingMgr{
		listResults: map[string][]string{
			"lerd-*-ws-feat-x": {
				"lerd-vite-ws-feat-x",
				"lerd-queue-ws-feat-x",
			},
		},
	}
	swapMgr(t, fake)

	if err := StopAllWorkersForWorktree("ws", "feat-x"); err != nil {
		t.Fatalf("StopAllWorkersForWorktree: %v", err)
	}

	gotRemoved := append([]string(nil), fake.removeServiceCalls...)
	sort.Strings(gotRemoved)
	wantRemoved := []string{"lerd-queue-ws-feat-x", "lerd-vite-ws-feat-x"}
	if !equalStrings(gotRemoved, wantRemoved) {
		t.Errorf("removeServiceCalls = %v, want %v", gotRemoved, wantRemoved)
	}
	for _, want := range wantRemoved {
		found := false
		for _, got := range fake.disableCalls {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected Disable(%q) but did not see it in %v", want, fake.disableCalls)
		}
	}
}

// TestStopAllWorkersForWorktree_noUnits is a no-op when nothing matches.
// Important so the worktree-remove path doesn't fail loudly when the user
// never opted into any host workers.
func TestStopAllWorkersForWorktree_noUnits(t *testing.T) {
	registerSite(t, "ws", "/p/ws")
	fake := &stopTrackingMgr{listResults: map[string][]string{}}
	swapMgr(t, fake)

	if err := StopAllWorkersForWorktree("ws", "feat-x"); err != nil {
		t.Fatalf("expected nil for no matching units, got %v", err)
	}
	if len(fake.removeServiceCalls) != 0 {
		t.Errorf("unexpected RemoveServiceUnit calls: %v", fake.removeServiceCalls)
	}
}

// TestStopAllWorkersForWorktree_emptyArgs guards against a glob that would
// match every parent-site unit (`lerd-*--`) or every unit whatsoever.
func TestStopAllWorkersForWorktree_emptyArgs(t *testing.T) {
	fake := &stopTrackingMgr{listResults: map[string][]string{
		"shouldNotBeQueried": {"lerd-vite-ws"},
	}}
	swapMgr(t, fake)

	if err := StopAllWorkersForWorktree("", ""); err != nil {
		t.Fatalf("empty args should be no-op, got %v", err)
	}
	if err := StopAllWorkersForWorktree("ws", ""); err != nil {
		t.Fatalf("empty wtBase should be no-op, got %v", err)
	}
	if err := StopAllWorkersForWorktree("", "feat"); err != nil {
		t.Fatalf("empty siteName should be no-op, got %v", err)
	}
	if len(fake.removeServiceCalls) != 0 {
		t.Errorf("expected zero removals, got %v", fake.removeServiceCalls)
	}
}

// TestStopAllWorkersForWorktree_filtersSpuriousMatches guards the suffix
// match: a glob like lerd-*-ws-feat-x can theoretically match a unit that
// happens to *contain* the suffix mid-string. Defensive trimming is what
// keeps the operation safe if Mgr returns unexpected results.
func TestStopAllWorkersForWorktree_filtersSpuriousMatches(t *testing.T) {
	registerSite(t, "ws", "/p/ws")
	fake := &stopTrackingMgr{
		listResults: map[string][]string{
			"lerd-*-ws-feat-x": {
				"lerd-vite-ws-feat-x",
				"lerd-vite-ws", // parent unit, should not be touched
			},
		},
	}
	swapMgr(t, fake)

	if err := StopAllWorkersForWorktree("ws", "feat-x"); err != nil {
		t.Fatalf("StopAllWorkersForWorktree: %v", err)
	}

	for _, removed := range fake.removeServiceCalls {
		if !strings.HasSuffix(removed, "-ws-feat-x") {
			t.Errorf("removed parent or unrelated unit: %q", removed)
		}
	}
}

// TestStopAllWorkersForWorktree_propagatesError returns the first underlying
// error so the caller can log it but keeps tearing down siblings — a single
// broken unit shouldn't block the rest of the cleanup.
func TestStopAllWorkersForWorktree_propagatesError(t *testing.T) {
	registerSite(t, "ws", "/p/ws")
	fake := &errOnRemoveMgr{
		stopTrackingMgr: stopTrackingMgr{
			listResults: map[string][]string{
				"lerd-*-ws-feat-x": {
					"lerd-vite-ws-feat-x",
					"lerd-queue-ws-feat-x",
				},
			},
		},
	}
	swapMgr(t, fake)

	err := StopAllWorkersForWorktree("ws", "feat-x")
	if err == nil {
		t.Fatal("expected a non-nil error from RemoveServiceUnit failure")
	}
	if !errors.Is(err, errFakeRemove) && !strings.Contains(err.Error(), "fake remove") {
		t.Errorf("unexpected error: %v", err)
	}
	// Both units should still have been attempted.
	if len(fake.removeServiceCalls) != 2 {
		t.Errorf("expected both removals to be attempted, got %v", fake.removeServiceCalls)
	}
}

var errFakeRemove = errors.New("fake remove")

type errOnRemoveMgr struct {
	stopTrackingMgr
}

func (e *errOnRemoveMgr) RemoveServiceUnit(name string) error {
	e.removeServiceCalls = append(e.removeServiceCalls, name)
	return errFakeRemove
}
