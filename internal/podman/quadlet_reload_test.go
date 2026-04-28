package podman

import (
	"errors"
	"testing"
)

func TestDaemonReloadIfNeeded_skipsWhenUnchangedAndNotPending(t *testing.T) {
	resetReloadState(t)
	count := installCountingReload(t, nil)

	if err := DaemonReloadIfNeeded(false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *count != 0 {
		t.Errorf("no reload expected, got %d", *count)
	}
}

func TestDaemonReloadIfNeeded_reloadsWhenChanged(t *testing.T) {
	resetReloadState(t)
	count := installCountingReload(t, nil)

	if err := DaemonReloadIfNeeded(true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *count != 1 {
		t.Errorf("expected 1 reload, got %d", *count)
	}
	if quadletReloadPending.Load() {
		t.Errorf("pending flag must be cleared after a successful reload")
	}
}

func TestDaemonReloadIfNeeded_setsPendingOnFailure(t *testing.T) {
	resetReloadState(t)
	wantErr := errors.New("dbus down")
	count := installCountingReload(t, wantErr)

	if err := DaemonReloadIfNeeded(true); !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
	if *count != 1 {
		t.Errorf("expected 1 reload attempt, got %d", *count)
	}
	if !quadletReloadPending.Load() {
		t.Errorf("pending flag must be set after a failed reload")
	}
}

func TestDaemonReloadIfNeeded_retriesPendingEvenWhenUnchanged(t *testing.T) {
	resetReloadState(t)
	wantErr := errors.New("dbus down")

	failCount := installCountingReload(t, wantErr)
	if err := DaemonReloadIfNeeded(true); !errors.Is(err, wantErr) {
		t.Fatalf("first call err: got %v want %v", err, wantErr)
	}
	if !quadletReloadPending.Load() {
		t.Fatalf("pending must be set after the failure")
	}
	if *failCount != 1 {
		t.Fatalf("first attempt count: got %d want 1", *failCount)
	}

	okCount := installCountingReload(t, nil)
	if err := DaemonReloadIfNeeded(false); err != nil {
		t.Fatalf("retry should succeed, got %v", err)
	}
	if *okCount != 1 {
		t.Errorf("retry must reload even with changed=false, got %d", *okCount)
	}
	if quadletReloadPending.Load() {
		t.Errorf("pending must clear after a successful retry")
	}

	if err := DaemonReloadIfNeeded(false); err != nil {
		t.Fatalf("third call err: %v", err)
	}
	if *okCount != 1 {
		t.Errorf("third call should be a no-op, got %d", *okCount)
	}
}

func resetReloadState(t *testing.T) {
	t.Helper()
	prev := quadletReloadPending.Swap(false)
	t.Cleanup(func() { quadletReloadPending.Store(prev) })
}

func installCountingReload(t *testing.T, returnErr error) *int {
	t.Helper()
	count := new(int)
	prev := DaemonReloadFn
	t.Cleanup(func() { DaemonReloadFn = prev })
	DaemonReloadFn = func() error {
		*count++
		return returnErr
	}
	return count
}
