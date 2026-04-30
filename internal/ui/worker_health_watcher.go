package ui

import (
	"encoding/json"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/geodro/lerd/internal/eventbus"
	"github.com/geodro/lerd/internal/workerheal"
)

// healthWatchInterval is how often the watcher re-runs the cached detector
// when at least one UI tab is open. Each tick is one map walk over the
// existing 3-second-TTL unit-state cache plus a string compare; no
// subprocess, no file read. Idle tabs drop to no work at all.
const healthWatchInterval = 5 * time.Second

// lastHealthSig is the last unhealthy-set signature seen by the watcher.
// Stored as an unsafe pointer through atomic so the watcher and broker
// don't race when the watcher updates it after publishing.
var lastHealthSig atomic.Value // string

// runWorkerHealthWatcher closes the gap between systemd's internal state
// transitions (start-limit-hit, external `systemctl stop`, anything that
// happens without lerd-ui's involvement) and the dashboard banner. Each
// tick:
//
//  1. If no tab is visible, skip — the next tab open does an initial fetch
//     anyway, and idle tabs shouldn't burn CPU.
//  2. Read the unhealthy set from the existing cached detector. Cheap.
//  3. Compare to the last seen signature. If unchanged, do nothing.
//  4. If changed, publish KindSites — the snapshot path then rebuilds the
//     unhealthy_workers JSON and the broker pushes it to every tab.
//
// The watcher does NOT run the heal itself; it only surfaces drift.
func runWorkerHealthWatcher() {
	ticker := time.NewTicker(healthWatchInterval)
	defer ticker.Stop()
	for range ticker.C {
		if visibleClients.Load() == 0 {
			continue
		}
		unhealthy, err := workerheal.Detect()
		if err != nil {
			continue
		}
		sig := healthSignature(unhealthy)
		prev, _ := lastHealthSig.Load().(string)
		if sig == prev {
			continue
		}
		lastHealthSig.Store(sig)
		// Riding KindSites means the existing snapshot/broker pipeline
		// rebuilds and pushes without needing a separate event kind.
		eventbus.Default.Publish(eventbus.KindSites)
	}
}

// healthSignature renders a stable string for set-equality checks. Sorting
// keeps the comparison robust against map-iteration order.
func healthSignature(ws []workerheal.UnhealthyWorker) string {
	if len(ws) == 0 {
		return ""
	}
	parts := make([]string, len(ws))
	for i, w := range ws {
		parts[i] = w.Unit + ":" + w.State
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// buildUnhealthyWorkersJSON serialises the current detector output. Errors
// degrade to an empty array so the dashboard never sees a malformed frame.
func buildUnhealthyWorkersJSON() []byte {
	out, err := workerheal.Detect()
	if err != nil || len(out) == 0 {
		return []byte("[]")
	}
	b, err := json.Marshal(out)
	if err != nil {
		return []byte("[]")
	}
	return b
}
