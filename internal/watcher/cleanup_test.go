package watcher

import (
	"testing"
	"time"
)

func TestCleanupDue(t *testing.T) {
	now := time.Unix(1_000_000_000, 0)
	cases := []struct {
		name string
		last time.Time
		want bool
	}{
		{"never run (zero time)", time.Time{}, true},
		{"ran 25h ago", now.Add(-25 * time.Hour), true},
		{"ran exactly at interval", now.Add(-autoCleanupInterval), true},
		{"ran 1h ago", now.Add(-1 * time.Hour), false},
		{"ran just now", now, false},
	}
	for _, c := range cases {
		if got := cleanupDue(now, c.last); got != c.want {
			t.Errorf("%s: cleanupDue = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestAutoCleanupStampRoundTrip(t *testing.T) {
	dir := t.TempDir()
	stampPathFn = func() string { return dir + "/auto-cleanup.stamp" }
	t.Cleanup(func() { stampPathFn = defaultStampPath })

	// No stamp yet → zero time (so a first sweep is always due).
	if !lastAutoCleanup().IsZero() {
		t.Fatalf("expected zero time before any stamp, got %v", lastAutoCleanup())
	}

	now := time.Unix(1_700_000_000, 0)
	stampAutoCleanup(now)
	if got := lastAutoCleanup(); !got.Equal(now) {
		t.Errorf("round-trip = %v, want %v", got, now)
	}
}
