package serviceops

import (
	"sync"
	"time"
)

// updateAvailabilityTTL bounds how long a CheckUpdateAvailable result is
// reused. Snapshot rebuilds in lerd-ui can fire many times per second during
// systemd burst transitions; without a cache, each rebuild forks one
// `podman image inspect` per service. 30 seconds balances freshness against
// fork pressure. Mutating ops (update, rollback, migrate) call
// invalidateUpdateAvailability(name) to drop their entry on completion.
const updateAvailabilityTTL = 30 * time.Second

type updateAvailabilityEntry struct {
	value *UpdateAvailability
	at    time.Time
}

var (
	updateAvailMu    sync.Mutex
	updateAvailCache = map[string]updateAvailabilityEntry{}
)

// cachedUpdateAvailability returns a recent CheckUpdateAvailable result for
// name, or nil if no fresh entry exists.
func cachedUpdateAvailability(name string) *UpdateAvailability {
	updateAvailMu.Lock()
	defer updateAvailMu.Unlock()
	entry, ok := updateAvailCache[name]
	if !ok || time.Since(entry.at) > updateAvailabilityTTL {
		return nil
	}
	return entry.value
}

func storeUpdateAvailability(name string, v *UpdateAvailability) {
	if v == nil {
		return
	}
	updateAvailMu.Lock()
	updateAvailCache[name] = updateAvailabilityEntry{value: v, at: time.Now()}
	updateAvailMu.Unlock()
}

// invalidateUpdateAvailability drops the cached entry for name. Called from
// the apply paths so the next read reflects the new image immediately.
func invalidateUpdateAvailability(name string) {
	updateAvailMu.Lock()
	delete(updateAvailCache, name)
	updateAvailMu.Unlock()
}
