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
const snapshotTTL = 2 * time.Second

// snapshotCache holds cached JSON bytes of the last /api/sites, /api/services,
// and /api/status responses. Handlers read from here instead of rebuilding
// from scratch on every poll; /api/ws broadcasts the same bytes to every
// connected browser.
type snapshotCache struct {
	mu sync.Mutex

	sites, services, status       []byte
	sitesAt, servicesAt, statusAt time.Time
}

var snapshots = &snapshotCache{}

// Sites returns cached /api/sites JSON, rebuilding if stale.
func (c *snapshotCache) Sites() []byte {
	c.mu.Lock()
	fresh := c.sites != nil && time.Since(c.sitesAt) < snapshotTTL
	c.mu.Unlock()
	if fresh {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.sites
	}
	b := buildSitesJSON()
	c.mu.Lock()
	c.sites = b
	c.sitesAt = time.Now()
	c.mu.Unlock()
	return b
}

// Services returns cached /api/services JSON, rebuilding if stale.
func (c *snapshotCache) Services() []byte {
	c.mu.Lock()
	fresh := c.services != nil && time.Since(c.servicesAt) < snapshotTTL
	c.mu.Unlock()
	if fresh {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.services
	}
	b := buildServicesJSON()
	c.mu.Lock()
	c.services = b
	c.servicesAt = time.Now()
	c.mu.Unlock()
	return b
}

// Status returns cached /api/status JSON, rebuilding if stale.
func (c *snapshotCache) Status() []byte {
	c.mu.Lock()
	fresh := c.status != nil && time.Since(c.statusAt) < snapshotTTL
	c.mu.Unlock()
	if fresh {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.status
	}
	b := buildStatusJSON()
	c.mu.Lock()
	c.status = b
	c.statusAt = time.Now()
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
	c.mu.Unlock()
}
