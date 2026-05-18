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

// lastUnhealthySet is the previous tick's unhealthy slice, kept so the
// watcher can compute the *new* failures (set difference) and fire a
// notification only for units that just transitioned into failure — not
// every tick that re-broadcasts the same long-standing failure.
var lastUnhealthySet atomic.Value // []workerheal.UnhealthyWorker

// healthWatcherInitialized gates first-tick notifications. The first run
// seeds lastUnhealthySet from whatever workers were already failed when
// lerd-ui came up, without firing — otherwise a launchd restart with N
// pre-existing failures would dispatch N notifications instantly.
var healthWatcherInitialized atomic.Bool

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
	seedHealthState()
	ticker := time.NewTicker(healthWatchInterval)
	defer ticker.Stop()
	for range ticker.C {
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
		queueWorkerFailureNotifications(diffNewFailuresAndCommit(unhealthy))
		// Skip the eventbus publish when no tab is open; the snapshot
		// rebuild would just rebuild bytes nobody reads. Notifications
		// above still fire so closed-PWA users get the push.
		if visibleClients.Load() == 0 {
			continue
		}
		eventbus.Default.Publish(eventbus.KindSites)
	}
}

// seedHealthState records the baseline at process start so a launchd
// restart with pre-existing failures doesn't fire N notifications on the
// first tick, AND a clean start still notifies when a failure later
// appears (the loop's sig-changed guard would otherwise leave the helper's
// initialized flag false until the first state change, swallowing it).
func seedHealthState() {
	unhealthy, err := workerheal.Detect()
	if err != nil {
		return
	}
	lastHealthSig.Store(healthSignature(unhealthy))
	lastUnhealthySet.Store(append([]workerheal.UnhealthyWorker(nil), unhealthy...))
	healthWatcherInitialized.Store(true)
}

// diffNewFailuresAndCommit returns workers in unhealthy that weren't
// present last tick, then commits the current set. First call returns
// nothing — it only seeds state — defensive belt-and-suspenders for
// callers that didn't invoke seedHealthState first.
func diffNewFailuresAndCommit(unhealthy []workerheal.UnhealthyWorker) []workerheal.UnhealthyWorker {
	prev, _ := lastUnhealthySet.Load().([]workerheal.UnhealthyWorker)
	var out []workerheal.UnhealthyWorker
	if healthWatcherInitialized.Load() {
		out = newWorkerFailures(prev, unhealthy)
	}
	lastUnhealthySet.Store(append([]workerheal.UnhealthyWorker(nil), unhealthy...))
	healthWatcherInitialized.Store(true)
	return out
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
// Each entry is enriched with the last journal line so the dashboard can
// surface "why did this fail?" without a drill-down.
func buildUnhealthyWorkersJSON() []byte {
	out, err := workerheal.Detect()
	if err != nil || len(out) == 0 {
		return []byte("[]")
	}
	out = workerheal.Enrich(out)
	b, err := json.Marshal(out)
	if err != nil {
		return []byte("[]")
	}
	return b
}
