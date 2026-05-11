package config

import "testing"

// These tests pin down the round-trip behaviour of the three service-name-set
// pairs (paused / manually-started / pinned) so the helper extraction below
// cannot silently change semantics.

func TestServicePaused_RoundTrip(t *testing.T) {
	setDataDir(t)

	if ServiceIsPaused("redis") {
		t.Fatal("expected redis not paused on empty store")
	}

	if err := SetServicePaused("redis", true); err != nil {
		t.Fatalf("SetServicePaused true: %v", err)
	}
	if !ServiceIsPaused("redis") {
		t.Fatal("expected redis paused after Set(true)")
	}

	if err := SetServicePaused("redis", false); err != nil {
		t.Fatalf("SetServicePaused false: %v", err)
	}
	if ServiceIsPaused("redis") {
		t.Fatal("expected redis not paused after Set(false)")
	}
}

func TestServiceManuallyStarted_RoundTrip(t *testing.T) {
	setDataDir(t)

	if ServiceIsManuallyStarted("postgres") {
		t.Fatal("expected postgres not manually-started on empty store")
	}

	if err := SetServiceManuallyStarted("postgres", true); err != nil {
		t.Fatalf("SetServiceManuallyStarted true: %v", err)
	}
	if !ServiceIsManuallyStarted("postgres") {
		t.Fatal("expected postgres manually-started after Set(true)")
	}

	if err := SetServiceManuallyStarted("postgres", false); err != nil {
		t.Fatalf("SetServiceManuallyStarted false: %v", err)
	}
	if ServiceIsManuallyStarted("postgres") {
		t.Fatal("expected postgres not manually-started after Set(false)")
	}
}

func TestServicePinned_RoundTrip(t *testing.T) {
	setDataDir(t)

	if ServiceIsPinned("mysql") {
		t.Fatal("expected mysql not pinned on empty store")
	}

	if err := SetServicePinned("mysql", true); err != nil {
		t.Fatalf("SetServicePinned true: %v", err)
	}
	if !ServiceIsPinned("mysql") {
		t.Fatal("expected mysql pinned after Set(true)")
	}

	if err := SetServicePinned("mysql", false); err != nil {
		t.Fatalf("SetServicePinned false: %v", err)
	}
	if ServiceIsPinned("mysql") {
		t.Fatal("expected mysql not pinned after Set(false)")
	}
}

func TestServiceSets_AreIndependent(t *testing.T) {
	setDataDir(t)

	if err := SetServicePaused("redis", true); err != nil {
		t.Fatalf("SetServicePaused: %v", err)
	}
	if err := SetServiceManuallyStarted("redis", true); err != nil {
		t.Fatalf("SetServiceManuallyStarted: %v", err)
	}
	if err := SetServicePinned("redis", true); err != nil {
		t.Fatalf("SetServicePinned: %v", err)
	}

	if err := SetServicePaused("redis", false); err != nil {
		t.Fatalf("SetServicePaused false: %v", err)
	}

	if ServiceIsPaused("redis") {
		t.Fatal("paused should be cleared")
	}
	if !ServiceIsManuallyStarted("redis") {
		t.Fatal("manually-started should still be set after clearing paused")
	}
	if !ServiceIsPinned("redis") {
		t.Fatal("pinned should still be set after clearing paused")
	}
}

func TestServicePaused_PersistsAcrossLoads(t *testing.T) {
	setDataDir(t)

	if err := SetServicePaused("a", true); err != nil {
		t.Fatalf("SetServicePaused a: %v", err)
	}
	if err := SetServicePaused("b", true); err != nil {
		t.Fatalf("SetServicePaused b: %v", err)
	}

	if !ServiceIsPaused("a") || !ServiceIsPaused("b") {
		t.Fatal("expected a and b paused")
	}
	if ServiceIsPaused("c") {
		t.Fatal("did not expect c paused")
	}
}

func TestServiceIs_MissingFile(t *testing.T) {
	setDataDir(t)

	if ServiceIsPaused("anything") {
		t.Error("ServiceIsPaused on missing file should be false")
	}
	if ServiceIsManuallyStarted("anything") {
		t.Error("ServiceIsManuallyStarted on missing file should be false")
	}
	if ServiceIsPinned("anything") {
		t.Error("ServiceIsPinned on missing file should be false")
	}
}
