package watcher

import (
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
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
	check               func(tld string) (bool, error)
	waitReady           func(time.Duration) error
	configureResolver   func() error
	repairPossible      func() bool
	idleOrLocked        func() bool
	publishStatus       func()
	dnsEnvFingerprint   func() string
	resyncContainerDNS  func() error
	repairExposeMapping func() (changed bool, err error)
	log                 func(level, msg string, kv ...any)
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

// defaultRepairExposeMapping re-renders the host dnsmasq .tld answer to the
// current primary LAN IP and reloads lerd-dns when lan:expose is on and the
// published mapping has drifted. lerd only regenerates that mapping on
// `lerd start`, so a sleep/wake DHCP renew or a network switch leaves dnsmasq
// answering the old IP; CheckStatus compares that answer against the live
// primaryLANIP and reports the dashboard pill down even though lerd-dns is
// serving fine, and in lan:expose mode the published address eventually stops
// routing once the old lease is gone. The config dir (DnsmasqDir) is
// user-owned and mounted read-only into the lerd-dns container, so the
// rewrite needs no privilege escalation and a unit reload picks it up on both
// macOS (launchd) and Linux (systemd).
//
// Returns (false, nil), a safe no-op, when expose is off, the host has no
// LAN IP yet, or the mapping already matches, so it can run on every failed
// health tick without thrashing the daemon.
func defaultRepairExposeMapping(tld string) (bool, error) {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return false, err
	}
	if cfg == nil || !cfg.LAN.Exposed {
		return false, nil
	}
	target := primaryLANIP()
	if target == "" {
		return false, nil
	}
	if answer, err := dns.DnsmasqAnswer(tld); err == nil && answer == target {
		return false, nil
	}
	if err := dns.WriteDnsmasqConfig(config.DnsmasqDir()); err != nil {
		return false, err
	}
	return true, podman.RestartUnit("lerd-dns")
}

// linkChangeDebounce caps how long the netlink burst from a single VPN
// connect or disconnect is allowed to settle before we re-tick. The kernel
// emits a flurry of RTM_NEWLINK / RTM_NEWADDR over the first few hundred
// milliseconds; re-syncing mid-burst would aim aardvark-dns at an
// intermediate resolver set that the network doesn't actually settle on.
const linkChangeDebounce = 750 * time.Millisecond

// WatchDNS polls DNS health for the given TLD every interval. When resolution
// is broken it waits for lerd-dns to be ready and re-applies the resolver
// configuration, replicating the DNS repair done by lerd start. When the
// user session is idle or locked it backs off to one probe every 10 ticks
// so laptops don't pay the per-30s DNS lookup battery cost while away.
//
// On Linux it also subscribes to rtnetlink link and address changes via
// linkChanges, so a VPN connect or disconnect kicks an immediate tick
// instead of waiting up to interval for the next poll. The poll stays as
// a safety net so a missed netlink event can't strand the system.
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

	// Cross-platform: heal a stale lan:expose .tld mapping after the host LAN
	// IP changes. Bind the configured TLD so the tick body stays no-arg.
	deps.repairExposeMapping = func() (bool, error) { return defaultRepairExposeMapping(tld) }

	state := &dnsWatchState{}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	done := make(chan struct{})
	defer close(done)

	linkRaw := make(chan struct{}, 32)
	linkSettled := make(chan struct{}, 4)
	go func() {
		if err := dns.LinkChanges(linkRaw, done); err != nil {
			logger.Warn("rtnetlink unavailable, DNS reacts on the safety-net poll only", "err", err)
		}
	}()
	go dns.DebounceEvents(linkRaw, linkSettled, linkChangeDebounce, done)

	runDNSLoop(deps, state, tld, ticker.C, linkSettled, done)
}

// runDNSLoop runs the tick state machine: an immediate first probe, then
// a tick on every ticker fire and every settled link change. done unblocks
// the loop so tests can shut down deterministically.
func runDNSLoop(d dnsWatchDeps, state *dnsWatchState, tld string,
	tickerC <-chan time.Time, linkC <-chan struct{}, done <-chan struct{}) {

	tickDNS(d, state, tld)
	for {
		select {
		case <-done:
			return
		case <-tickerC:
			tickDNS(d, state, tld)
		case <-linkC:
			tickDNS(d, state, tld)
		}
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

	// A stale lan:expose mapping (the dnsmasq .tld answer drifting from the
	// host's current primary LAN IP after a sleep/wake or DHCP renew) makes
	// CheckStatus report down even though lerd-dns is healthy. Re-render the
	// mapping and reload lerd-dns first: the config dir is user-owned, so this
	// needs no privilege escalation and heals even on a host where the
	// sudo-gated resolver repair below is unavailable. A no-op (expose off or
	// the mapping already current) returns false and falls through.
	if d.repairExposeMapping != nil {
		switch changed, err := d.repairExposeMapping(); {
		case err != nil:
			d.log("warn", "lan:expose DNS mapping repair failed", "err", err)
		case changed:
			d.log("info", "lan:expose DNS mapping re-rendered to the current LAN IP")
			if ok2, _ := d.check(tld); ok2 {
				up := true
				s.lastOK = &up
				d.publishStatus()
				return
			}
		}
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
