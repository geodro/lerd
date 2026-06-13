// Package idle implements activity-driven worker suspension: it tracks when each
// site was last active and (later) suspends a quiet site's suspendable workers,
// resuming them on the next activity. This file is the activity tracker — the
// pure, in-memory record of "site X was last active at T" fed by the nginx
// access feed, file-edit events, and CLI/MCP actions.
package idle

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"
)

// SiteResolver maps a request host (e.g. "admin.myapp.test") to the owning
// site name, collapsing a site's many domains onto one activity record.
// It returns ok=false when the host belongs to no registered site.
type SiteResolver func(host string) (site string, ok bool)

// Tracker records the last-active time per site. It is safe for concurrent use:
// the access-feed listener, the watcher bridge, and the engine ticker all touch
// it from different goroutines.
type Tracker struct {
	mu     sync.Mutex
	last   map[string]time.Time
	resolc SiteResolver
}

// NewTracker returns a tracker that resolves request hosts to sites via resolve.
// resolve may be nil, in which case TouchHost is a no-op (only TouchSite works).
func NewTracker(resolve SiteResolver) *Tracker {
	return &Tracker{last: map[string]time.Time{}, resolc: resolve}
}

// TouchSite records activity for a known site name at time t. Older timestamps
// never move the record backwards, so out-of-order datagrams are harmless.
func (t *Tracker) TouchSite(site string, at time.Time) {
	if site == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if cur, ok := t.last[site]; !ok || at.After(cur) {
		t.last[site] = at
	}
}

// TouchHost resolves a request host to its site and records activity. Hosts that
// belong to no site are ignored. Returns the resolved site (or "" when unmatched).
func (t *Tracker) TouchHost(host string, at time.Time) string {
	if t.resolc == nil {
		return ""
	}
	site, ok := t.resolc(normalizeHost(host))
	if !ok {
		return ""
	}
	t.TouchSite(site, at)
	return site
}

// LastActive returns the last-active time for a site and whether one is recorded.
func (t *Tracker) LastActive(site string) (time.Time, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	ts, ok := t.last[site]
	return ts, ok
}

// IdleFor reports how long the site has been idle as of now. A site with no
// recorded activity reports ok=false rather than a misleading "infinitely idle",
// so callers can apply a startup grace window instead of suspending immediately.
func (t *Tracker) IdleFor(site string, now time.Time) (time.Duration, bool) {
	ts, ok := t.LastActive(site)
	if !ok {
		return 0, false
	}
	return now.Sub(ts), true
}

// Snapshot returns a copy of the last-active map for read-only consumers (the
// dashboard, diagnostics) without exposing the internal map.
func (t *Tracker) Snapshot() map[string]time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(map[string]time.Time, len(t.last))
	for k, v := range t.last {
		out[k] = v
	}
	return out
}

// Seed marks the given sites active as of t, used on lerd-ui startup so a
// restart's empty map doesn't make every site look instantly idle and get
// suspended inside its grace window. Seeding never overwrites a newer record.
func (t *Tracker) Seed(sites []string, at time.Time) {
	for _, s := range sites {
		t.TouchSite(s, at)
	}
}

// Forget drops a site's record, e.g. when a site is removed. No-op if absent.
func (t *Tracker) Forget(site string) {
	t.mu.Lock()
	delete(t.last, site)
	t.mu.Unlock()
}

// Save persists the last-active map to path as JSON ({site: unixSeconds}) so a
// restart can restore the idle countdowns instead of re-seeding to now. Written
// atomically via a temp file + rename.
func (t *Tracker) Save(path string) error {
	snap := t.Snapshot()
	m := make(map[string]int64, len(snap))
	for k, v := range snap {
		m[k] = v.Unix()
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LoadActivity reads a last-active map previously written by Save, returning the
// site->unix-seconds map (nil when the file is missing or unreadable).
func LoadActivity(path string) map[string]int64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var m map[string]int64
	if json.Unmarshal(data, &m) != nil {
		return nil
	}
	return m
}

// normalizeHost strips a :port suffix and lowercases, so "Myapp.test:443" and
// "myapp.test" resolve to the same site.
func normalizeHost(host string) string {
	host = strings.TrimSpace(host)
	if i := strings.LastIndex(host, ":"); i >= 0 && !strings.Contains(host[i:], "]") {
		host = host[:i]
	}
	return strings.ToLower(host)
}

// ParseAccessHost extracts the request host from one nginx access datagram. The
// access feed logs a minimal "$host" message through syslog framing, so the host
// is the final whitespace-delimited token of the line. Returns "" for nginx's
// "-" placeholder (a request with no Host header) or an unparseable line.
func ParseAccessHost(datagram []byte) string {
	line := strings.TrimRight(string(datagram), "\n\x00 ")
	if line == "" {
		return ""
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	host := fields[len(fields)-1]
	if host == "-" || strings.HasSuffix(host, ":") {
		return ""
	}
	return host
}
