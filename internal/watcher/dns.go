package watcher

import (
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/dns"
	"github.com/geodro/lerd/internal/eventbus"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/systemd"
)

// idleSkipEveryN controls how aggressively to back off polling when the
// session is idle or locked. We still probe once every N ticks so a
// returning user hits a healed DNS immediately.
const idleSkipEveryN = 10

// dnsWatchDeps is the injection surface for tickDNS so the orchestration
// can be unit-tested without an actual resolver or eventbus subscriber.
type dnsWatchDeps struct {
	check              func(tld string) (bool, error)
	waitReady          func(time.Duration) error
	configureResolver  func() error
	repairPossible     func() bool
	idleOrLocked       func() bool
	publishStatus      func()
	dnsEnvFingerprint  func() string
	resyncContainerDNS func() error
	log                func(level, msg string, kv ...any)
}

// dnsWatchState is the cross-tick memory for WatchDNS. lastOK starts nil
// so the first observation always publishes, in case the snapshot built
// during boot baked in a stale dns.ok=false.
type dnsWatchState struct {
	lastOK            *bool
	tickCount         int
	repairUnavailable bool
	dnsEnv            string
	dnsEnvSeen        bool
}

// defaultDNSEnvFingerprint summarises the host DNS environment: the sorted
// upstream resolver set plus whether a VPN tunnel is up. Either changing
// (VPN connect/disconnect, network switch) means aardvark-dns is serving
// stale forwarders and a stale cache, so container DNS needs a re-sync.
func defaultDNSEnvFingerprint() string {
	up := dns.ReadUpstreamDNS()
	sort.Strings(up)
	vpn := "0"
	if dns.VPNActive() {
		vpn = "1"
	}
	return strings.Join(up, ",") + "|" + vpn
}

// defaultResyncContainerDNS re-points the lerd network's aardvark-dns at
// the current host resolvers and reloads the network so containers pick
// them up. This is the automatic equivalent of a manual `lerd restart`
// after a VPN connects.
func defaultResyncContainerDNS() error {
	if err := podman.EnsureNetworkDNS("lerd", dns.ReadContainerDNS()); err != nil {
		return err
	}
	return podman.ReloadNetworks()
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

	// Container DNS re-sync recovers from aardvark-dns forwarder staleness,
	// which is specific to Linux rootless podman. macOS containers get DNS
	// from the podman machine VM (ReadContainerDNS is nil there), so there
	// is nothing to re-sync and the detection stays off.
	if runtime.GOOS == "linux" {
		deps.dnsEnvFingerprint = defaultDNSEnvFingerprint
		deps.resyncContainerDNS = defaultResyncContainerDNS
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

	// Re-sync container DNS when the host resolver environment changes
	// (VPN connect/disconnect, network switch). The lerd network's
	// aardvark-dns is otherwise left on the pre-change forwarders and a
	// stale cache, so containers can't resolve newly-routable hostnames
	// until a manual `lerd restart`. The first tick only records the
	// baseline so a fresh watcher start never triggers a re-sync.
	if d.dnsEnvFingerprint != nil {
		fp := d.dnsEnvFingerprint()
		if s.dnsEnvSeen && fp != s.dnsEnv {
			d.log("info", "host DNS changed, re-syncing container DNS")
			if d.resyncContainerDNS != nil {
				if err := d.resyncContainerDNS(); err != nil {
					d.log("warn", "container DNS re-sync failed", "err", err)
				}
			}
		}
		s.dnsEnv = fp
		s.dnsEnvSeen = true
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
