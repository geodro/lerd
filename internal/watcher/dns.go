package watcher

import (
	"time"

	"github.com/geodro/lerd/internal/dns"
	"github.com/geodro/lerd/internal/systemd"
)

// idleSkipEveryN controls how aggressively to back off polling when the
// session is idle or locked. We still probe once every N ticks so a
// returning user hits a healed DNS immediately instead of a 0.5-1s
// resolver timeout. At interval=30s, N=10 means one probe per 5 min.
const idleSkipEveryN = 10

// WatchDNS polls DNS health for the given TLD every interval. When resolution
// is broken it waits for lerd-dns to be ready and re-applies the resolver
// configuration, replicating the DNS repair done by lerd start. When the
// user session is idle or locked it backs off to one probe every 10 ticks
// so laptops don't pay the per-30s DNS lookup battery cost while away.
func WatchDNS(interval time.Duration, tld string) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	tickCount := 0
	for range ticker.C {
		tickCount++
		if systemd.SessionIsIdleOrLocked() && tickCount%idleSkipEveryN != 0 {
			continue
		}

		ok, _ := dns.Check(tld)
		if ok {
			continue
		}

		logger.Warn("DNS resolution broken, repairing", "tld", tld)

		if err := dns.WaitReady(10 * time.Second); err != nil {
			logger.Error("lerd-dns not ready", "err", err)
			continue
		}

		if err := dns.ConfigureResolver(); err != nil {
			logger.Error("DNS repair failed", "err", err)
		} else {
			logger.Info("DNS resolution restored", "tld", tld)
		}
	}
}
