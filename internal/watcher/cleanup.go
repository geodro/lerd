package watcher

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/cleanup"
	"github.com/geodro/lerd/internal/config"
)

// autoCleanupInterval is the minimum gap between automatic safe-tier sweeps.
// Orphaned images aren't urgent, so a slow daily cadence reclaims rebuild
// leftovers on its own while keeping the watcher quiet.
const autoCleanupInterval = 24 * time.Hour

// WatchCleanup periodically reclaims orphaned lerd images (the safe tier only,
// never the --deep service-image tier). It ticks at interval but acts at most
// once per autoCleanupInterval, throttled by a persisted timestamp so a
// restarting watcher can't sweep more often. The auto_cleanup config gate turns
// it off.
func WatchCleanup(interval time.Duration) {
	// Check once at startup too: the daily stamp throttles it, but a watcher
	// that restarts more often than interval would otherwise never sweep, and a
	// fresh post-install start has rebuild orphans worth reaping promptly.
	runAutoCleanup(time.Now())

	t := time.NewTicker(interval)
	defer t.Stop()
	for range t.C {
		runAutoCleanup(time.Now())
	}
}

// runAutoCleanup performs one throttled, gated safe-tier sweep. Split from the
// ticker so its decision points are unit-testable with an injected clock. The
// actual reclaim is delegated to cleanup.SweepSafe so the watcher only adds the
// throttle and the log line on top of the shared pipeline.
func runAutoCleanup(now time.Time) {
	cfg, err := config.LoadGlobal()
	if err != nil || !cfg.AutoCleanupEnabled() {
		return
	}
	if !cleanupDue(now, lastAutoCleanup()) {
		return
	}

	images, freed, err := cleanup.SweepSafe()
	if err != nil {
		// Transient podman failure: don't stamp, so the next tick retries
		// instead of being throttled out for a full interval.
		return
	}
	stampAutoCleanup(now)
	if images > 0 {
		logger.Info("auto-cleanup reclaimed orphaned images", "images", images, "bytes", freed)
	}
}

// cleanupDue reports whether a full interval has elapsed since the last sweep.
func cleanupDue(now, last time.Time) bool {
	return now.Sub(last) >= autoCleanupInterval
}

// stampPathFn is the seam tests override to redirect the timestamp file.
var stampPathFn = defaultStampPath

func defaultStampPath() string {
	return filepath.Join(config.DataDir(), "auto-cleanup.stamp")
}

// lastAutoCleanup reads the last sweep time, or the zero time when the stamp is
// missing or unreadable (so the first sweep is always due).
func lastAutoCleanup() time.Time {
	b, err := os.ReadFile(stampPathFn())
	if err != nil {
		return time.Time{}
	}
	secs, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(secs, 0)
}

func stampAutoCleanup(now time.Time) {
	_ = os.WriteFile(stampPathFn(), []byte(strconv.FormatInt(now.Unix(), 10)), 0o644)
}
