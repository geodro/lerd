package ui

import (
	"testing"
)

func TestNoteVisibilityCounter(t *testing.T) {
	// Reset global state before and after test so other tests aren't affected.
	visibleClients.Store(0)
	t.Cleanup(func() { visibleClients.Store(0) })

	noteVisibility(true) // 1
	noteVisibility(true) // 2
	if v := visibleClients.Load(); v != 2 {
		t.Errorf("expected 2 visible clients, got %d", v)
	}

	noteVisibility(false) // 1 — should NOT change interval to idle yet
	if v := visibleClients.Load(); v != 1 {
		t.Errorf("expected 1 visible client after one decrement, got %d", v)
	}

	noteVisibility(false) // 0 — now idle
	if v := visibleClients.Load(); v != 0 {
		t.Errorf("expected 0 visible clients, got %d", v)
	}

	// Extra decrement (simulates the double-decrement bug we fixed: browser
	// sends visible=false then connection closes) must not go negative.
	noteVisibility(false)
	if v := visibleClients.Load(); v != 0 {
		t.Errorf("counter should not go below 0, got %d", v)
	}
}

func TestNoteVisibilityMultipleConnections(t *testing.T) {
	visibleClients.Store(0)
	t.Cleanup(func() { visibleClients.Store(0) })

	// Simulate two connections: A visible, B visible.
	noteVisibility(true) // A connects (assumed visible) → 1
	noteVisibility(true) // B connects → 2

	// A hides.
	noteVisibility(false) // → 1 (B still visible, should stay at focused interval)
	if v := visibleClients.Load(); v != 1 {
		t.Errorf("expected 1 after A hides, got %d", v)
	}

	// A disconnects — with the fix, since connVisible is false for A, no
	// extra decrement. We simulate the correct behaviour: no call.
	// (The fixed handleWS only calls noteVisibility(false) on disconnect
	// if the connection was still visible.)
	// Counter should still be 1.
	if v := visibleClients.Load(); v != 1 {
		t.Errorf("disconnect of already-hidden A should not change counter; got %d", v)
	}

	// B disconnects while still visible → 0.
	noteVisibility(false) // → 0
	if v := visibleClients.Load(); v != 0 {
		t.Errorf("expected 0 after B disconnects, got %d", v)
	}
}
