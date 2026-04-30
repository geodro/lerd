package siteinfo

import (
	"os/exec"
	"strings"
	"sync"
	"time"
)

// unitCacheTTL bounds how long a batched systemctl snapshot is reused before
// a refresh is triggered on the next lookup.
const unitCacheTTL = 3 * time.Second

type unitCache struct {
	mu     sync.Mutex
	states map[string]string
	at     time.Time
}

var (
	globalUnitCache unitCache

	// unitCacheListFn is swappable for tests. It returns the raw output of
	// `systemctl --user list-units --all --no-legend --plain 'lerd-*'`.
	unitCacheListFn = defaultUnitCacheList
)

func defaultUnitCacheList() (string, error) {
	out, err := exec.Command("systemctl", "--user", "list-units", "--all", "--no-legend", "--plain", "lerd-*").Output()
	return string(out), err
}

// InvalidateUnitCache forces the next UnitStatus lookup to re-run systemctl.
// Call this after any mutation that changes lerd-* unit state (start, stop,
// enable, disable, etc.) so cached "active" values do not go stale.
func InvalidateUnitCache() {
	globalUnitCache.mu.Lock()
	globalUnitCache.at = time.Time{}
	globalUnitCache.mu.Unlock()
}

// AllUnitStates returns a snapshot of every cached lerd-* unit state
// (unit name → "active" | "inactive" | "failed" | …). The map is a copy
// safe for callers to walk without holding the cache mutex. Triggers a
// refresh if the cache is stale, but otherwise reuses the same batched
// systemctl snapshot the dashboard's enrichment path is already populating
// — zero extra subprocess cost for callers like the worker-health detector.
func AllUnitStates() map[string]string {
	globalUnitCache.mu.Lock()
	defer globalUnitCache.mu.Unlock()
	if globalUnitCache.states == nil || time.Since(globalUnitCache.at) > unitCacheTTL {
		_ = globalUnitCache.refreshLocked()
	}
	out := make(map[string]string, len(globalUnitCache.states))
	for k, v := range globalUnitCache.states {
		out[k] = v
	}
	return out
}

// unitStatusCached returns the active state of a lerd-* unit, consulting a
// short-lived batched snapshot. One systemctl call populates ~all lerd units
// instead of one subprocess per worker.
func unitStatusCached(name string) (string, error) {
	globalUnitCache.mu.Lock()
	defer globalUnitCache.mu.Unlock()

	if globalUnitCache.states == nil || time.Since(globalUnitCache.at) > unitCacheTTL {
		if err := globalUnitCache.refreshLocked(); err != nil {
			return "unknown", nil
		}
	}

	if st, ok := globalUnitCache.states[name]; ok {
		return st, nil
	}
	return "unknown", nil
}

func (c *unitCache) refreshLocked() error {
	out, err := unitCacheListFn()
	if err != nil {
		return err
	}
	states := make(map[string]string, 64)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Columns: UNIT LOAD ACTIVE SUB DESCRIPTION
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		unit, active := fields[0], fields[2]
		// Strip the .service suffix so callers can pass either form.
		// Timer and other suffixes are preserved since enrichWorkers
		// explicitly looks up "lerd-schedule-<site>.timer".
		states[unit] = active
		if strings.HasSuffix(unit, ".service") {
			states[strings.TrimSuffix(unit, ".service")] = active
		}
	}
	c.states = states
	c.at = time.Now()
	return nil
}
