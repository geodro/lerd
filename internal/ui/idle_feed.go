package ui

import (
	"net"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/idle"
)

// activityTracker records per-site last-active times fed by the nginx access
// feed. Created when the access feed starts; read by the dashboard snapshot and
// (later) the idle-suspend engine. nil until startAccessFeed runs.
var activityTracker *idle.Tracker

// startAccessFeed creates the activity tracker, seeds it so a fresh start does
// not look instantly idle, and (on Linux) binds the unix datagram socket nginx
// logs request hosts to. macOS is skipped because lerd-nginx runs inside the
// podman-machine VM there, where a host unix socket isn't reachable — same
// constraint as the UI socket. Best-effort throughout: a bind failure just means
// idle-suspend has no web signal, never a broken daemon.
func startAccessFeed() {
	activityTracker = idle.NewTracker(resolveHostToSite)
	seedActiveSites(activityTracker)
	idleEng = newIdleEngine(activityTracker)
	go idleEng.run()

	if runtime.GOOS == "darwin" {
		return
	}
	if err := os.MkdirAll(config.RunDir(), 0755); err != nil {
		return
	}
	sockPath := config.AccessSocketPath()
	_ = os.Remove(sockPath)
	conn, err := net.ListenPacket("unixgram", sockPath)
	if err != nil {
		return
	}
	// 0660: nginx (rootless podman maps container-root to this host uid) writes
	// the datagrams, matching the UI stream socket's permissions.
	_ = os.Chmod(sockPath, 0660)
	go readAccessFeed(conn)
}

// readAccessFeed records one site touch per access datagram until the socket
// closes (daemon shutdown), at which point ReadFrom errors and the loop exits.
func readAccessFeed(conn net.PacketConn) {
	buf := make([]byte, 4096)
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			return
		}
		if host := idle.ParseAccessHost(buf[:n]); host != "" {
			if site := activityTracker.TouchHost(host, time.Now()); site != "" {
				idleEng.OnActivity(site)
			}
		}
	}
}

// siteLastActiveUnix returns the site's last-active time as unix seconds, or 0
// when the feed hasn't run or no activity is recorded yet. Read by the sites
// snapshot to drive the dashboard's relative activity indicator.
func siteLastActiveUnix(site string) int64 {
	if activityTracker == nil {
		return 0
	}
	if ts, ok := activityTracker.LastActive(site); ok {
		return ts.Unix()
	}
	return 0
}

// siteIsIdle reports whether a site has gone past the idle timeout: the feature
// is enabled, the site isn't paused, and its last activity is older than the
// timeout. Unlike suspension this is true even for sites with no workers to
// stop, so the dashboard can mark every idle site, not only suspended ones.
func siteIsIdle(siteName string, paused, pinned, enabled bool, timeout time.Duration, now time.Time) bool {
	if !enabled || paused || pinned || activityTracker == nil {
		return false
	}
	idleFor, ok := activityTracker.IdleFor(siteName, now)
	return ok && idleFor >= timeout
}

// resolveHostToSite maps a request host to its idle key. A worktree domain
// resolves to the worktree's key (so its own traffic wakes the worktree, not the
// parent site); other hosts resolve to the owning site name. Hosts that belong to
// no registered site resolve to ok=false and are ignored by the tracker.
func resolveHostToSite(host string) (string, bool) {
	if key := idleEng.worktreeKeyForHost(host); key != "" {
		return key, true
	}
	site, err := config.FindSiteByDomain(host)
	if err != nil || site == nil {
		return "", false
	}
	return site.Name, true
}

// seedActiveSites restores each site's last-active time on startup: from the
// persisted file when present (so a restart/deploy keeps the countdown going),
// otherwise seeded to now (a new or never-seen site gets the grace window rather
// than looking instantly idle).
func seedActiveSites(t *idle.Tracker) {
	saved := idle.LoadActivity(config.IdleActivityFile())
	reg, err := config.LoadSites()
	if err != nil {
		return
	}
	now := time.Now()
	for _, s := range reg.Sites {
		if ts, ok := saved[s.Name]; ok && ts > 0 {
			t.TouchSite(s.Name, time.Unix(ts, 0))
		} else {
			t.TouchSite(s.Name, now)
		}
	}
	// Restore persisted worktree countdowns too (their keys carry a "/"), so a
	// restart doesn't hand every worktree a fresh grace window. A stale key for a
	// removed worktree is harmless: the engine only ever acts on worktrees it
	// re-detects from disk.
	for key, ts := range saved {
		if ts > 0 && strings.IndexByte(key, '/') >= 0 {
			t.TouchSite(key, time.Unix(ts, 0))
		}
	}
}
