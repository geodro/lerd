package ui

import (
	"sync"
	"time"

	"github.com/geodro/lerd/internal/eventbus"
)

// snapshotTTL bounds how long a cached snapshot is reused before the next
// request triggers a rebuild. Mutations that change state call
// eventbus.Default.Publish, which invalidates the relevant kind so the next
// read recomputes immediately.
//
// Two cadences: the short one when a real UI tab is open (the dashboard
// expects near-realtime data), the long one when only the tray is polling.
// The tray shows slow-changing state (nginx running, dns ok, php list); five
// minutes of staleness there is fine because explicit mutations invalidate
// via AfterUnitChange and are reflected on the next tray poll regardless.
const (
	snapshotTTLActive = 15 * time.Second
	snapshotTTLIdle   = 5 * time.Minute
)

func currentSnapshotTTL() time.Duration {
	if visibleClients.Load() > 0 {
		return snapshotTTLActive
	}
	return snapshotTTLIdle
}

// snapshotCache holds cached JSON bytes of the last /api/sites, /api/services,
// and /api/status responses. Handlers read from here instead of rebuilding
// from scratch on every poll; /api/ws broadcasts the same bytes to every
// connected browser.
type snapshotCache struct {
	mu sync.Mutex

	sites, services, status       []byte
	sitesAt, servicesAt, statusAt time.Time

	// Unhealthy-workers JSON shadows the sites cycle: it is derived from
	// the same batched unit-state cache and invalidated alongside KindSites,
	// so callers don't pay any extra subprocess cost.
	unhealthy   []byte
	unhealthyAt time.Time

	// One build-mutex per kind serialises concurrent rebuilds so that when
	// podman inspect is slow, goroutines queue behind one in-flight rebuild
	// rather than each spawning their own batch of subprocesses.
	sitesBuild, servicesBuild, statusBuild, unhealthyBuild sync.Mutex
}

var snapshots = &snapshotCache{}

// Sites returns cached /api/sites JSON, rebuilding if stale.
// If a rebuild is already in progress, returns the stale value immediately
// rather than queuing behind the in-flight build.
func (c *snapshotCache) Sites() []byte {
	c.mu.Lock()
	if c.sites != nil && time.Since(c.sitesAt) < currentSnapshotTTL() {
		b := c.sites
		c.mu.Unlock()
		return b
	}
	stale := c.sites
	c.mu.Unlock()

	if !c.sitesBuild.TryLock() {
		return stale
	}
	defer c.sitesBuild.Unlock()

	c.mu.Lock()
	if c.sites != nil && time.Since(c.sitesAt) < currentSnapshotTTL() {
		b := c.sites
		c.mu.Unlock()
		return b
	}
	c.mu.Unlock()

	b := buildSitesJSON()
	c.mu.Lock()
	c.sites = b
	c.sitesAt = time.Now()
	c.mu.Unlock()
	return b
}

// Services returns cached /api/services JSON, rebuilding if stale.
// If a rebuild is already in progress, returns the stale value immediately
// rather than queuing behind the in-flight build.
func (c *snapshotCache) Services() []byte {
	c.mu.Lock()
	if c.services != nil && time.Since(c.servicesAt) < currentSnapshotTTL() {
		b := c.services
		c.mu.Unlock()
		return b
	}
	stale := c.services
	c.mu.Unlock()

	if !c.servicesBuild.TryLock() {
		return stale
	}
	defer c.servicesBuild.Unlock()

	c.mu.Lock()
	if c.services != nil && time.Since(c.servicesAt) < currentSnapshotTTL() {
		b := c.services
		c.mu.Unlock()
		return b
	}
	c.mu.Unlock()

	b := buildServicesJSON()
	c.mu.Lock()
	c.services = b
	c.servicesAt = time.Now()
	c.mu.Unlock()
	return b
}

// Status returns cached /api/status JSON, rebuilding if stale.
// If a rebuild is already in progress, returns the stale value immediately
// rather than queuing behind the in-flight build.
func (c *snapshotCache) Status() []byte {
	c.mu.Lock()
	if c.status != nil && time.Since(c.statusAt) < currentSnapshotTTL() {
		b := c.status
		c.mu.Unlock()
		return b
	}
	stale := c.status
	c.mu.Unlock()

	if !c.statusBuild.TryLock() {
		return stale
	}
	defer c.statusBuild.Unlock()

	c.mu.Lock()
	if c.status != nil && time.Since(c.statusAt) < currentSnapshotTTL() {
		b := c.status
		c.mu.Unlock()
		return b
	}
	c.mu.Unlock()

	b := buildStatusJSON()
	c.mu.Lock()
	c.status = b
	c.statusAt = time.Now()
	c.mu.Unlock()
	return b
}

// UnhealthyWorkers returns cached worker-health JSON, rebuilding if stale.
// Pinned to the KindSites lifecycle: same source cache, same invalidation
// signal, no extra polling.
func (c *snapshotCache) UnhealthyWorkers() []byte {
	c.mu.Lock()
	if c.unhealthy != nil && time.Since(c.unhealthyAt) < currentSnapshotTTL() {
		b := c.unhealthy
		c.mu.Unlock()
		return b
	}
	stale := c.unhealthy
	c.mu.Unlock()

	if !c.unhealthyBuild.TryLock() {
		return stale
	}
	defer c.unhealthyBuild.Unlock()

	c.mu.Lock()
	if c.unhealthy != nil && time.Since(c.unhealthyAt) < currentSnapshotTTL() {
		b := c.unhealthy
		c.mu.Unlock()
		return b
	}
	c.mu.Unlock()

	b := buildUnhealthyWorkersJSON()
	c.mu.Lock()
	c.unhealthy = b
	c.unhealthyAt = time.Now()
	c.mu.Unlock()
	return b
}

// Invalidate drops the cached bytes for one kind so the next read rebuilds.
func (c *snapshotCache) Invalidate(kind string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch kind {
	case eventbus.KindSites:
		c.sitesAt = time.Time{}
		// Worker health shares the sites lifecycle.
		c.unhealthyAt = time.Time{}
	case eventbus.KindServices:
		c.servicesAt = time.Time{}
	case eventbus.KindStatus:
		c.statusAt = time.Time{}
	}
}

// InvalidateAll drops all three cached snapshots.
func (c *snapshotCache) InvalidateAll() {
	c.mu.Lock()
	c.sitesAt = time.Time{}
	c.servicesAt = time.Time{}
	c.statusAt = time.Time{}
	c.unhealthyAt = time.Time{}
	c.mu.Unlock()
}
