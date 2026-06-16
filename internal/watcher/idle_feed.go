package watcher

import (
	"context"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/idle"
)

// activityTracker records per-site last-active times fed by the access feed and
// control socket, read by the engine and persisted to config.IdleActivityFile()
// for lerd-ui and the CLI to render. Allocated once by StartIdle.
var activityTracker *idle.Tracker

// Idle-suspend lifecycle. The control socket is always bound (the toggle/wake
// point); everything else lives in an enabled session started by enableIdle and
// torn down by disableIdle, so a disabled feature does no work at all.
var (
	idleMu       sync.Mutex
	idleCancel   context.CancelFunc // non-nil exactly while a session runs
	idleActive   atomic.Bool        // gate for control-socket activity pings
	idleStartSrc func(stop <-chan struct{}) error
)

// StartIdle wires the idle subsystem: the control socket is always bound, while
// the session (access feed, tick, source watcher) only runs when enabled.
// sourceWatcher runs until its stop channel closes.
func StartIdle(notify func(), sourceWatcher func(stop <-chan struct{}) error) {
	if notify != nil {
		idleNotifyUI = notify
	}
	idleStartSrc = sourceWatcher
	activityTracker = idle.NewTracker(resolveHostToSite)
	idleEng = newIdleEngine(activityTracker)
	go runNotifier()
	startControlSocket()

	// Boot memory is the persisted config flag, not the ephemeral socket. When
	// off, still resume any workers a prior session left suspended (e.g. toggled
	// off while the watcher was down) so they're never stranded stopped.
	if cfg, err := config.LoadGlobal(); err == nil && cfg.IdleSuspend.Enabled {
		enableIdle()
	} else {
		go idleEng.resumeUntilClear()
	}
}

// enableIdle starts the idle session: seed activity, bind the nginx access feed,
// run the engine tick, and start the source-file watcher, all tied to one cancel
// context. Idempotent and safe to call from boot or a control "enable".
func enableIdle() {
	idleMu.Lock()
	defer idleMu.Unlock()
	if idleCancel != nil {
		return // already running
	}
	ctx, cancel := context.WithCancel(context.Background())
	idleCancel = cancel
	idleActive.Store(true)

	seedActiveSites(activityTracker)
	_ = os.MkdirAll(config.RunDir(), 0755)
	_ = activityTracker.Save(config.IdleActivityFile())

	go idleEng.run(ctx)

	// macOS skips the access feed: lerd-nginx runs in the podman-machine VM where
	// a host unix socket isn't reachable. Source-file saves are the macOS signal.
	if runtime.GOOS != "darwin" {
		if conn, ok := listenDatagram(config.AccessSocketPath()); ok {
			go func() { <-ctx.Done(); conn.Close() }()
			go readDatagrams(conn, handleAccessDatagram)
		}
	}
	if idleStartSrc != nil {
		go func() { _ = idleStartSrc(ctx.Done()) }()
	}
}

// disableIdle stops the session (tick, access feed, source watcher), then resumes
// every suspended worker in the background via resumeUntilClear, which retries so
// a suspend mid-flight isn't skipped now that no later tick will catch it.
func disableIdle() {
	idleMu.Lock()
	if idleCancel != nil {
		idleActive.Store(false)
		idleCancel()
		idleCancel = nil
	}
	idleMu.Unlock()
	go idleEng.resumeUntilClear()
}

// handleAccessDatagram records one site touch per nginx access datagram, waking a
// suspended site when the request host resolves to one.
func handleAccessDatagram(b []byte) {
	if host := idle.ParseAccessHost(b); host != "" {
		if site := activityTracker.TouchHost(host, time.Now()); site != "" {
			idleEng.OnActivity(site)
		}
	}
}

// listenDatagram binds a unix datagram socket at path under RunDir, replacing any
// stale one. ok=false on failure (idle-suspend is best-effort, so callers skip).
// 0660 matches nginx's writer uid on the access socket and the UI stream socket.
func listenDatagram(path string) (net.PacketConn, bool) {
	if err := os.MkdirAll(config.RunDir(), 0755); err != nil {
		return nil, false
	}
	_ = os.Remove(path)
	conn, err := net.ListenPacket("unixgram", path)
	if err != nil {
		return nil, false
	}
	_ = os.Chmod(path, 0660)
	return conn, true
}

// readDatagrams delivers each datagram to handle until the socket closes (daemon
// shutdown), at which point ReadFrom errors and the loop exits.
func readDatagrams(conn net.PacketConn, handle func([]byte)) {
	buf := make([]byte, 4096)
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			return
		}
		handle(buf[:n])
	}
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
