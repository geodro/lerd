package ui

import (
	"sync/atomic"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dns"
	"github.com/geodro/lerd/internal/eventbus"
)

// dnsStatusWatchInterval is how often the watcher re-probes DNS while a UI
// tab is visible. One probe every 30s is well below the cost of the
// existing per-15s container cache poll.
const dnsStatusWatchInterval = 30 * time.Second

// DNS observations the watcher tracks. They mirror dns.Status plus an
// "unknown" zero value for "never observed".
const (
	dnsObsUnknown  int32 = 0
	dnsObsOK       int32 = 1
	dnsObsDegraded int32 = 2
	dnsObsDown     int32 = 3
)

// lastDNSObs is the last *confirmed* observation, the one the published
// snapshot reflects. pendingDNSObs is a candidate awaiting a second
// consecutive tick before it latches, so a single transient probe failure
// (common the moment a VPN connects) can't flip the dashboard pill on its
// own. Both atomic so the watcher needs no mutex.
var (
	lastDNSObs    atomic.Int32
	pendingDNSObs atomic.Int32
)

// dnsStatusDeps is the injection surface for tickDNSStatus so the
// transition-and-publish logic can be unit-tested without touching the
// real config, resolver, or event bus.
type dnsStatusDeps struct {
	tld     func() string
	check   func(tld string) dns.Status
	visible func() bool
	publish func()
}

// defaultDNSStatusDeps wires the production resolver and bus.
func defaultDNSStatusDeps() dnsStatusDeps {
	return dnsStatusDeps{
		tld: func() string {
			cfg, _ := config.LoadGlobal()
			if cfg == nil {
				return "test"
			}
			return cfg.DNS.TLD
		},
		check:   dns.CheckStatus,
		visible: func() bool { return visibleClients.Load() > 0 },
		publish: func() { eventbus.Default.Publish(eventbus.KindStatus) },
	}
}

// runDNSStatusWatcher closes the cross-process gap between WatchDNS (which
// runs in the lerd-watcher process and only does repair) and the WebSocket
// broker (which lives here in the lerd-ui process). The eventbus is
// per-process, so a publish from the watcher never reaches subscribers
// here, and the dashboard would otherwise stay red after boot until the
// user manually refreshed even after lerd-dns came online.
//
// Probes immediately on startup so a UI tab opened during boot doesn't
// sit on stale state for up to 30s while DNS comes online. Each
// subsequent tick: if no tab is visible, skip. Otherwise probe once,
// compare to the last observation, and publish KindStatus on transition.
func runDNSStatusWatcher() {
	deps := defaultDNSStatusDeps()
	tickDNSStatus(deps)
	ticker := time.NewTicker(dnsStatusWatchInterval)
	defer ticker.Stop()
	for range ticker.C {
		tickDNSStatus(deps)
	}
}

// obsFromStatus maps a dns.Status onto the watcher's observation enum.
func obsFromStatus(s dns.Status) int32 {
	switch s {
	case dns.StatusOK:
		return dnsObsOK
	case dns.StatusDegraded:
		return dnsObsDegraded
	default:
		return dnsObsDown
	}
}

// tickDNSStatus runs one observation and publishes on a confirmed
// transition. A change must survive two consecutive ticks before it
// latches, so a single transient blip never flips the pill on its own.
func tickDNSStatus(d dnsStatusDeps) {
	if !d.visible() {
		return
	}
	cur := obsFromStatus(d.check(d.tld()))
	confirmed := lastDNSObs.Load()

	// First observation latches immediately so a tab opened during boot
	// isn't stuck on unknown for a whole debounce cycle.
	if confirmed == dnsObsUnknown {
		lastDNSObs.Store(cur)
		pendingDNSObs.Store(cur)
		d.publish()
		return
	}
	if cur == confirmed {
		pendingDNSObs.Store(cur) // matches steady state, drop any stale candidate
		return
	}
	// The observation differs from the confirmed state. Publish only once
	// the same new value has been seen on two consecutive ticks.
	if pendingDNSObs.Swap(cur) != cur {
		return
	}
	lastDNSObs.Store(cur)
	d.publish()
}
