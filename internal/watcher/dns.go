package watcher

import (
	"time"

	"github.com/geodro/lerd/internal/dns"
	"github.com/geodro/lerd/internal/eventbus"
	"github.com/geodro/lerd/internal/systemd"
)

// idleSkipEveryN controls how aggressively to back off polling when the
// session is idle or locked. We still probe once every N ticks so a
// returning user hits a healed DNS immediately.
const idleSkipEveryN = 10

// dnsWatchDeps is the injection surface for tickDNS so the orchestration
// can be unit-tested without an actual resolver or eventbus subscriber.
type dnsWatchDeps struct {
	check             func(tld string) (bool, error)
	waitReady         func(time.Duration) error
	configureResolver func() error
	repairPossible    func() bool
	idleOrLocked      func() bool
	publishStatus     func()
	log               func(level, msg string, kv ...any)
}

// dnsWatchState is the cross-tick memory for WatchDNS. lastOK starts nil
// so the first observation always publishes, in case the snapshot built
// during boot baked in a stale dns.ok=false.
type dnsWatchState struct {
	lastOK            *bool
	tickCount         int
	repairUnavailable bool
}

// WatchDNS polls DNS health for the given TLD every interval. When resolution
// is broken it waits for lerd-dns to be ready and re-applies the resolver
// configuration, replicating the DNS repair done by lerd start. When the
// user session is idle or locked it backs off to one probe every 10 ticks
// so laptops don't pay the per-30s DNS lookup battery cost while away.
//
// Every observed transition and every successful repair publishes
// eventbus.KindStatus so the dashboard reflects the live state via the
// WebSocket without a manual refresh.
func WatchDNS(interval time.Duration, tld string) {
	deps := dnsWatchDeps{
		check:             dns.Check,
		waitReady:         dns.WaitReady,
		configureResolver: dns.ConfigureResolver,
		repairPossible:    dns.RepairPossible,
		idleOrLocked:      systemd.SessionIsIdleOrLocked,
		publishStatus:     func() { eventbus.Default.Publish(eventbus.KindStatus) },
		log: func(level, msg string, kv ...any) {
			switch level {
			case "info":
				logger.Info(msg, kv...)
			case "warn":
				logger.Warn(msg, kv...)
			case "error":
				logger.Error(msg, kv...)
			}
		},
	}
	state := &dnsWatchState{}

	// Probe immediately so a UI opened during boot doesn't sit on stale
	// dns.ok=false for up to 30s while DNS comes online naturally.
	tickDNS(deps, state, tld)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		tickDNS(deps, state, tld)
	}
}

// tickDNS runs one iteration of the DNS health loop. It returns early
// during idle backoff. On every tick that probes, the previous
// observation is compared and a transition publishes KindStatus.
func tickDNS(d dnsWatchDeps, s *dnsWatchState, tld string) {
	s.tickCount++
	if d.idleOrLocked() && s.tickCount%idleSkipEveryN != 0 {
		return
	}

	ok, _ := d.check(tld)
	transitioned := s.lastOK == nil || *s.lastOK != ok
	prev := ok
	s.lastOK = &prev

	if transitioned {
		d.publishStatus()
	}

	if ok {
		return
	}

	// Skip repair when the platform can't write the resolver config from
	// this process (macOS without /etc/sudoers.d/lerd in place). Logging
	// this every tick would spam — emit once and remember the gate.
	if d.repairPossible != nil && !d.repairPossible() {
		if !s.repairUnavailable {
			d.log("warn", "DNS resolution broken; automatic repair unavailable on this host (run lerd install to grant the watcher resolver write access)", "tld", tld)
			s.repairUnavailable = true
		}
		return
	}
	s.repairUnavailable = false

	d.log("warn", "DNS resolution broken, repairing", "tld", tld)

	if err := d.waitReady(10 * time.Second); err != nil {
		d.log("error", "lerd-dns not ready", "err", err)
		return
	}

	if err := d.configureResolver(); err != nil {
		d.log("error", "DNS repair failed", "err", err)
		return
	}

	d.log("info", "DNS resolution restored", "tld", tld)
	// Repair flipped DNS from down to up; publish now so the dashboard
	// doesn't wait up to 30s for the next tick to notice.
	up := true
	s.lastOK = &up
	d.publishStatus()
}
