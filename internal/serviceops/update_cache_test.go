package serviceops

import (
	"testing"
	"time"
)

func TestUpdateAvailabilityCache_storeAndRetrieve(t *testing.T) {
	t.Cleanup(func() { invalidateUpdateAvailability("svc") })

	if got := cachedUpdateAvailability("svc"); got != nil {
		t.Fatalf("expected nil before store, got %+v", got)
	}

	want := &UpdateAvailability{Service: "svc", LatestTag: "1.2.3"}
	storeUpdateAvailability("svc", want)

	got := cachedUpdateAvailability("svc")
	if got == nil || got.LatestTag != "1.2.3" {
		t.Fatalf("expected cached entry with tag 1.2.3, got %+v", got)
	}
}

func TestUpdateAvailabilityCache_invalidate(t *testing.T) {
	storeUpdateAvailability("svc", &UpdateAvailability{Service: "svc", LatestTag: "9.9"})
	invalidateUpdateAvailability("svc")

	if got := cachedUpdateAvailability("svc"); got != nil {
		t.Fatalf("expected nil after invalidate, got %+v", got)
	}
}

func TestUpdateAvailabilityCache_expiresAfterTTL(t *testing.T) {
	t.Cleanup(func() { invalidateUpdateAvailability("svc") })

	updateAvailMu.Lock()
	updateAvailCache["svc"] = updateAvailabilityEntry{
		value: &UpdateAvailability{Service: "svc"},
		at:    time.Now().Add(-2 * updateAvailabilityTTL),
	}
	updateAvailMu.Unlock()

	if got := cachedUpdateAvailability("svc"); got != nil {
		t.Fatalf("expected nil after TTL expiry, got %+v", got)
	}
}

func TestUpdateAvailabilityCache_storeNilIsNoop(t *testing.T) {
	t.Cleanup(func() { invalidateUpdateAvailability("svc") })

	storeUpdateAvailability("svc", nil)
	if got := cachedUpdateAvailability("svc"); got != nil {
		t.Fatalf("storing nil should be no-op, got %+v", got)
	}
}
