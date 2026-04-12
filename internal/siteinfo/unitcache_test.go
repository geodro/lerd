package siteinfo

import (
	"testing"
	"time"
)

func withStubList(t *testing.T, out string, err error) {
	t.Helper()
	prev := unitCacheListFn
	unitCacheListFn = func() (string, error) { return out, err }
	InvalidateUnitCache()
	t.Cleanup(func() {
		unitCacheListFn = prev
		InvalidateUnitCache()
	})
}

func TestUnitStatusCachedParsesListUnits(t *testing.T) {
	withStubList(t, `lerd-queue-astrolov.service     loaded active   running Lerd Queue Worker
lerd-schedule-silvia.timer      loaded active   waiting Lerd Schedule Timer
lerd-reverb-boom.service        loaded failed   failed  Lerd Reverb
`, nil)

	cases := map[string]string{
		"lerd-queue-astrolov":         "active",
		"lerd-queue-astrolov.service": "active",
		"lerd-schedule-silvia.timer":  "active",
		"lerd-reverb-boom":            "failed",
		"lerd-ghost":                  "unknown",
	}
	for unit, want := range cases {
		got, _ := unitStatusCached(unit)
		if got != want {
			t.Errorf("%s: got %q, want %q", unit, got, want)
		}
	}
}

func TestUnitStatusCachedBatchesCalls(t *testing.T) {
	calls := 0
	prev := unitCacheListFn
	unitCacheListFn = func() (string, error) {
		calls++
		return "lerd-queue-a.service loaded active running x\n", nil
	}
	InvalidateUnitCache()
	t.Cleanup(func() {
		unitCacheListFn = prev
		InvalidateUnitCache()
	})

	for i := 0; i < 50; i++ {
		unitStatusCached("lerd-queue-a")
	}
	if calls != 1 {
		t.Fatalf("expected 1 systemctl call for 50 lookups, got %d", calls)
	}
}

func TestUnitStatusCachedRefreshesAfterInvalidate(t *testing.T) {
	calls := 0
	prev := unitCacheListFn
	unitCacheListFn = func() (string, error) {
		calls++
		return "lerd-queue-a.service loaded active running x\n", nil
	}
	InvalidateUnitCache()
	t.Cleanup(func() {
		unitCacheListFn = prev
		InvalidateUnitCache()
	})

	unitStatusCached("lerd-queue-a")
	InvalidateUnitCache()
	unitStatusCached("lerd-queue-a")
	if calls != 2 {
		t.Fatalf("expected 2 calls after invalidate, got %d", calls)
	}
}

func TestUnitStatusCachedRefreshesAfterTTL(t *testing.T) {
	calls := 0
	prev := unitCacheListFn
	unitCacheListFn = func() (string, error) {
		calls++
		return "lerd-queue-a.service loaded active running x\n", nil
	}
	InvalidateUnitCache()
	t.Cleanup(func() {
		unitCacheListFn = prev
		InvalidateUnitCache()
	})

	unitStatusCached("lerd-queue-a")

	// Backdate the cache so the next call triggers a refresh without
	// sleeping for the full TTL.
	globalUnitCache.mu.Lock()
	globalUnitCache.at = time.Now().Add(-unitCacheTTL - time.Second)
	globalUnitCache.mu.Unlock()

	unitStatusCached("lerd-queue-a")
	if calls != 2 {
		t.Fatalf("expected 2 calls after TTL expiry, got %d", calls)
	}
}

func TestUnitStatusCachedSystemctlFailure(t *testing.T) {
	withStubList(t, "", errFakeSystemctl{})
	got, _ := unitStatusCached("lerd-anything")
	if got != "unknown" {
		t.Fatalf("want unknown on systemctl failure, got %q", got)
	}
}

type errFakeSystemctl struct{}

func (errFakeSystemctl) Error() string { return "boom" }
