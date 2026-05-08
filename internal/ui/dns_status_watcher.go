package ui

import (
	"sync/atomic"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dns"
	"github.com/geodro/lerd/internal/eventbus"
)

// dnsStatusWatchInterval is how often the watcher re-probes DNS while a UI
// tab is visible. One net.LookupHost every 30s is well below the cost of
// the existing per-15s container cache poll.
const dnsStatusWatchInterval = 30 * time.Second

// lastDNSOK encodes the previously observed dns.Check result as an atomic
// int32: 0 = never observed, 1 = up, 2 = down. Atomic so the watcher and
// any future probe path can share state without a mutex.
var lastDNSOK atomic.Int32

const (
	dnsObsUnknown int32 = 0
	dnsObsUp      int32 = 1
	dnsObsDown    int32 = 2
)

// dnsStatusDeps is the injection surface for tickDNSStatus so the
// transition-and-publish logic can be unit-tested without touching the
// real config, resolver, or event bus.
type dnsStatusDeps struct {
	tld     func() string
	check   func(tld string) (bool, error)
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
		check:   dns.Check,
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
// Each tick: if no tab is visible, skip. Otherwise probe once, compare to
// the last observation, and publish KindStatus on transition.
func runDNSStatusWatcher() {
	deps := defaultDNSStatusDeps()
	ticker := time.NewTicker(dnsStatusWatchInterval)
	defer ticker.Stop()
	for range ticker.C {
		tickDNSStatus(deps)
	}
}

// tickDNSStatus runs one observation and publishes on transition.
func tickDNSStatus(d dnsStatusDeps) {
	if !d.visible() {
		return
	}
	ok, _ := d.check(d.tld())
	cur := dnsObsDown
	if ok {
		cur = dnsObsUp
	}
	prev := lastDNSOK.Swap(cur)
	if prev == cur {
		return
	}
	d.publish()
}
