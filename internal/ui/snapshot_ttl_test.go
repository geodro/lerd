package ui

import (
	"testing"
	"time"
)

func TestCurrentSnapshotTTL_TracksVisibility(t *testing.T) {
	visibleClients.Store(0)
	t.Cleanup(func() { visibleClients.Store(0) })

	if got, want := currentSnapshotTTL(), snapshotTTLIdle; got != want {
		t.Errorf("with no visible clients, TTL = %v, want %v", got, want)
	}

	visibleClients.Store(1)
	if got, want := currentSnapshotTTL(), snapshotTTLActive; got != want {
		t.Errorf("with 1 visible client, TTL = %v, want %v", got, want)
	}

	visibleClients.Store(0)
	if got, want := currentSnapshotTTL(), snapshotTTLIdle; got != want {
		t.Errorf("after returning to no visible, TTL = %v, want %v", got, want)
	}
}

func TestSnapshotIdleTTLIsAtLeastFiveMinutes(t *testing.T) {
	if snapshotTTLIdle < 5*time.Minute {
		t.Errorf("idle TTL should keep tray polls off the rebuild path; got %v, want >= 5m", snapshotTTLIdle)
	}
	if snapshotTTLActive >= snapshotTTLIdle {
		t.Errorf("active TTL must be shorter than idle TTL; active=%v idle=%v", snapshotTTLActive, snapshotTTLIdle)
	}
}

func TestBrokerHasPeers(t *testing.T) {
	b := &wsBroker{peers: map[chan wsMessage]struct{}{}}
	if b.hasPeers() {
		t.Error("empty broker should report no peers")
	}

	ch := b.add()
	if !b.hasPeers() {
		t.Error("broker with one peer should report hasPeers")
	}

	b.remove(ch)
	if b.hasPeers() {
		t.Error("broker after remove should report no peers")
	}
}
