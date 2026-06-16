package watcher

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/geodro/lerd/internal/cli"
	"github.com/geodro/lerd/internal/config"
	gitpkg "github.com/geodro/lerd/internal/git"
	"github.com/geodro/lerd/internal/idle"
)

// wtKey is the idle key for a git worktree: its parent site name and the
// worktree's unit-slug base, joined by "/". A site name can't contain "/", so the
// key parses back unambiguously. The main site's idle key is just its name.
func wtKey(site, wtBase string) string { return site + "/" + wtBase }

// splitWtKey parses a key back into (site, wtBase). isWt is false for a main-site
// key (no "/"), in which case wtBase is empty.
func splitWtKey(key string) (site, wtBase string, isWt bool) {
	if i := strings.IndexByte(key, '/'); i >= 0 {
		return key[:i], key[i+1:], true
	}
	return key, "", false
}

// idleNotifyUI crosses the process boundary to refresh lerd-ui's sites snapshot
// after a suspend/resume (the watcher's in-process eventbus never reaches the
// UI). Wired by the watch command; a no-op in tests and when lerd-ui is down.
var idleNotifyUI = func() {}

// notifyDirty wakes the coalescing notifier. Buffered to 1 so a burst of
// suspend/resume/activity events collapses to one pending refresh rather than
// one synchronous loopback POST (and goroutine) per event.
var notifyDirty = make(chan struct{}, 1)

// notifyDebounce is the quiet period after a refresh POST, bounding the UI poke
// rate so an activity-ping burst can't flood lerd-ui.
const notifyDebounce = 250 * time.Millisecond

// publishSitesChanged requests a debounced dashboard refresh. Non-blocking: a
// pending request already covers this one, so it never stalls the datagram read
// loop or the engine tick.
func publishSitesChanged() {
	select {
	case notifyDirty <- struct{}{}:
	default:
	}
}

// runNotifier collapses refresh requests into at most one idleNotifyUI POST per
// notifyDebounce, so bursts can't fan out into many concurrent loopback POSTs.
func runNotifier() {
	for range notifyDirty {
		idleNotifyUI()
		time.Sleep(notifyDebounce)
	}
}

// idleTickInterval is how often the engine re-evaluates every site for
// suspension. Resume is also triggered immediately on activity (OnActivity), so
// this interval only bounds how long an idle site waits past its timeout before
// suspending — not how fast it wakes.
const idleTickInterval = 30 * time.Second

// idleEng is the suspend engine, created once by StartIdle. nil until then.
var idleEng *idleEngine

// detectWorktrees is the worktree detector the engine uses, a var so tests can
// stand in fake worktrees without a real git checkout.
var detectWorktrees = gitpkg.DetectWorktrees

// idleEngine suspends a site's suspendable workers once it has been idle past
// its timeout and resumes them on activity. It holds an in-memory mirror of
// which sites are currently suspended (also persisted in Site.IdleSuspendedWorkers)
// so OnActivity can decide whether to resume with a cheap map lookup per request.
type idleEngine struct {
	tracker   *idle.Tracker
	mu        sync.Mutex
	suspended map[string]bool
	// inFlight guards a site whose suspend or resume is running in a goroutine
	// (a suspend may run a slow one-time `npm run build`), so the tick and other
	// sites' wakes never block on it and the same site isn't worked twice at once.
	inFlight map[string]bool
	// worktreePath maps a worktree idle key to its checkout dir, and
	// worktreeKeyByDomain maps a worktree domain to its idle key. Both are rebuilt
	// each tick from git worktree detection so the access feed can attribute a
	// worktree-domain hit to the right key, and resume can find the checkout to
	// restart its workers in.
	worktreePath        map[string]string
	worktreeKeyByDomain map[string]string
}

func newIdleEngine(t *idle.Tracker) *idleEngine {
	e := &idleEngine{
		tracker:             t,
		suspended:           map[string]bool{},
		inFlight:            map[string]bool{},
		worktreePath:        map[string]string{},
		worktreeKeyByDomain: map[string]string{},
	}
	// Seed from persisted state so a lerd-ui restart remembers which sites and
	// worktrees are suspended and resumes them on the next request rather than
	// leaving their workers stopped with no memory.
	if reg, err := config.LoadSites(); err == nil {
		for i := range reg.Sites {
			s := reg.Sites[i]
			if len(s.IdleSuspendedWorkers) > 0 {
				// Verify the claim against reality. If a supposedly-suspended worker is
				// actually running (an install/relink restarted it without clearing the
				// list), the list is stale: drop it so the engine re-evaluates from
				// scratch instead of believing the site is asleep and never re-suspending.
				if cli.IdleSuspendStateIsStale(&s) {
					_ = config.SetSiteIdleSuspendedWorkers(s.Name, nil)
				} else {
					e.suspended[s.Name] = true
				}
			}
			for wtBase, workers := range s.WorktreeIdleSuspended {
				if len(workers) == 0 {
					continue
				}
				// Same reality check as the main site above: drop a worktree slot whose
				// workers are actually running so the engine doesn't believe a live
				// worktree is asleep and never re-suspend it.
				if cli.WorktreeIdleSuspendStateIsStale(&s, wtBase, workers) {
					_ = config.SetWorktreeIdleSuspendedWorkers(s.Name, wtBase, nil)
					continue
				}
				e.suspended[wtKey(s.Name, wtBase)] = true
			}
		}
	}
	return e
}

// run drives the periodic suspend evaluation until the process exits.
func (e *idleEngine) run(ctx context.Context) {
	defer recoverEngine("ticker")
	t := time.NewTicker(idleTickInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done(): // idle-suspend disabled: stop ticking
			return
		case <-t.C:
			e.tick()
			// Persist last-active so a restart/deploy restores the countdowns
			// instead of re-seeding to now.
			_ = e.tracker.Save(config.IdleActivityFile())
		}
	}
}

func (e *idleEngine) tick() {
	defer recoverEngine("tick")
	cfg, err := config.LoadGlobal()
	if err != nil {
		return
	}
	reg, err := config.LoadSites()
	if err != nil {
		return
	}
	// Idle-suspend is a single global policy; it applies identically to every
	// site, so resolve it once per tick rather than per site.
	enabled := cfg.IdleSuspend.Enabled
	timeout := cfg.IdleSuspendTimeout()
	now := time.Now()
	// Rebuilt every tick from worktree detection, then swapped in atomically.
	newWtPath := map[string]string{}
	newWtDomain := map[string]string{}
	for i := range reg.Sites {
		s := reg.Sites[i]
		if s.Ignored || s.Paused {
			continue
		}
		e.mu.Lock()
		if !e.inFlight[s.Name] {
			// Reconcile our cached belief against reality, not just the persisted
			// list. A restore/install/boot path can re-create, re-enable, and
			// re-start the workers we suspended without clearing the list (they're
			// left enabled), so trusting the list alone would wedge the site as
			// "believed asleep" while its workers actually run, never re-suspending.
			// When the list claims suspended but a listed worker is in fact running,
			// drop the stale list so this same tick re-suspends the idle site.
			// Skipped mid-flight so a slow suspend/resume goroutine isn't
			// second-guessed before it persists.
			believed := len(s.IdleSuspendedWorkers) > 0
			if believed && cli.IdleSuspendStateIsStale(&s) {
				_ = config.SetSiteIdleSuspendedWorkers(s.Name, nil)
				believed = false
			}
			e.suspended[s.Name] = believed
		}
		suspended := e.suspended[s.Name]
		e.mu.Unlock()
		if s.Pinned {
			// Pinned sites never go idle. If one was pinned while already
			// suspended, wake it so the pin takes effect immediately. Still tick
			// its worktrees: the pin covers them too (tickWorktrees resumes a
			// suspended worktree and skips suspending), and the pass keeps their
			// domains resolvable for the access feed.
			if suspended {
				e.resume(s.Name)
			}
			e.tickWorktrees(&s, enabled, timeout, now, newWtPath, newWtDomain)
			continue
		}
		idleFor, hasRecord := e.tracker.IdleFor(s.Name, now)
		switch idle.Decide(enabled, timeout, idleFor, hasRecord, suspended) {
		case idle.ActionSuspend:
			e.suspend(s.Name)
		case idle.ActionResume:
			e.resume(s.Name)
		}

		// Each git worktree idles on its own timer, independent of the main site.
		e.tickWorktrees(&s, enabled, timeout, now, newWtPath, newWtDomain)
	}

	// Swap in the freshly-detected worktree lookups for the access feed and resume.
	e.mu.Lock()
	e.worktreePath = newWtPath
	e.worktreeKeyByDomain = newWtDomain
	e.mu.Unlock()
}

// tickWorktrees evaluates each of the site's worktrees for suspend/resume and
// records its path + domain in the rebuilding lookup maps. A worktree gets the
// same startup grace as a site: never-before-seen keys are seeded to now so they
// aren't suspended inside their first window.
func (e *idleEngine) tickWorktrees(s *config.Site, enabled bool, timeout time.Duration, now time.Time, outPath, outDomain map[string]string) {
	wts, err := detectWorktrees(s.Path, s.PrimaryDomain())
	if err != nil {
		// Detection failed (transient git error): leave state alone rather than
		// risk pruning a worktree that still exists.
		return
	}
	// Track which worktree keys still exist so stale suspended state for a deleted
	// worktree can be cleared below; a deleted worktree is never revisited
	// otherwise and would show as suspended forever.
	detected := make(map[string]bool, len(wts))
	for _, wt := range wts {
		wtBase := config.WorktreeUnitSlug(filepath.Base(wt.Path))
		key := wtKey(s.Name, wtBase)
		detected[key] = true
		outPath[key] = wt.Path
		if wt.Domain != "" {
			outDomain[strings.ToLower(wt.Domain)] = key
		}

		e.mu.Lock()
		if !e.inFlight[key] {
			// Same reality-based reconcile as the main site: if the slot claims
			// suspended but the worktree's worker is actually running (a restore
			// path restarted it without clearing the slot), drop the stale slot so
			// this tick re-suspends it instead of believing it asleep forever.
			believed := len(s.WorktreeIdleSuspended[wtBase]) > 0
			if believed && cli.WorktreeIdleSuspendStateIsStale(s, wtBase, s.WorktreeIdleSuspended[wtBase]) {
				_ = config.SetWorktreeIdleSuspendedWorkers(s.Name, wtBase, nil)
				believed = false
			}
			e.suspended[key] = believed
		}
		suspended := e.suspended[key]
		e.mu.Unlock()

		if s.Pinned {
			if suspended {
				e.resumeWorktree(s.Name, wtBase, wt.Path)
			}
			continue
		}

		idleFor, hasRecord := e.tracker.IdleFor(key, now)
		if !hasRecord {
			e.tracker.TouchSite(key, now) // first sighting: start its grace window
			continue
		}
		switch idle.Decide(enabled, timeout, idleFor, hasRecord, suspended) {
		case idle.ActionSuspend:
			e.suspendWorktree(s.Name, wtBase, wt.Path)
		case idle.ActionResume:
			e.resumeWorktree(s.Name, wtBase, wt.Path)
		}
	}
	e.pruneStaleWorktrees(s.Name, detected)
}

// pruneStaleWorktrees clears in-memory and persisted suspended state for the
// site's worktrees that no longer exist (detected is the set of worktree keys
// present this tick), so a removed worktree stops showing as suspended forever.
func (e *idleEngine) pruneStaleWorktrees(siteName string, detected map[string]bool) {
	e.mu.Lock()
	var stale []string
	for key, susp := range e.suspended {
		site, _, isWt := splitWtKey(key)
		if !isWt || site != siteName || !susp {
			continue
		}
		if !detected[key] {
			stale = append(stale, key)
		}
	}
	for _, key := range stale {
		delete(e.suspended, key)
	}
	e.mu.Unlock()
	for _, key := range stale {
		if _, wtBase, ok := splitWtKey(key); ok {
			_ = config.SetWorktreeIdleSuspendedWorkers(siteName, wtBase, nil)
		}
	}
}

// OnActivity is called for every request that resolves to an idle key (a site
// name, or a worktree key). It resumes a currently-suspended target immediately
// (off the feed's hot path) and is a cheap no-op for the common case of one that
// isn't suspended.
func (e *idleEngine) OnActivity(key string) {
	if e == nil {
		return
	}
	e.mu.Lock()
	suspended := e.suspended[key]
	wtPath := e.worktreePath[key]
	e.mu.Unlock()
	if !suspended {
		return
	}
	// If a suspend is mid-flight (slow build), resume() below no-ops on the
	// inFlight guard and this wake is dropped. That is fine: the activity already
	// updated last-active, so once the suspend settles the next tick sees the site
	// active again and resumes it (idle < timeout && suspended -> ActionResume).
	if site, wtBase, isWt := splitWtKey(key); isWt {
		if wtPath != "" {
			e.resumeWorktree(site, wtBase, wtPath) // resumeWorktree runs its own goroutine
		}
		return
	}
	e.resume(key) // resume runs its own goroutine
}

// worktreeKeyForHost returns the idle key for a worktree request host, or "" if
// the host isn't a known worktree domain. Used by the access feed's resolver so a
// worktree's own traffic wakes the worktree, not its parent site.
func (e *idleEngine) worktreeKeyForHost(host string) string {
	if e == nil {
		return ""
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.worktreeKeyByDomain[strings.ToLower(host)]
}

// resumeUntilClear is the disable path's safety net (replacing the tick that used
// to re-resume disabled-but-suspended sites): it retries until nothing is left
// suspended, catching a site whose slow mid-flight suspend resume() had skipped.
func (e *idleEngine) resumeUntilClear() {
	if e == nil {
		return
	}
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		if idleActive.Load() {
			return // re-enabled: the live session owns suspend/resume again
		}
		e.ResumeAllSuspended()
		// Keep going while a suspend is still in-flight: it may not have written
		// its list yet, so an empty config alone doesn't mean nothing's suspended.
		if !persistedIdleSuspendExists() && !e.anyInFlight() {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// anyInFlight reports whether any site's suspend or resume goroutine is running.
func (e *idleEngine) anyInFlight() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.inFlight) > 0
}

// persistedIdleSuspendExists reports whether any site or worktree still records a
// non-empty idle-suspended worker list on disk. A read error returns true so the
// drain keeps retrying rather than giving up and stranding a worker.
func persistedIdleSuspendExists() bool {
	reg, err := config.LoadSites()
	if err != nil {
		return true
	}
	for i := range reg.Sites {
		if len(reg.Sites[i].IdleSuspendedWorkers) > 0 {
			return true
		}
		for _, w := range reg.Sites[i].WorktreeIdleSuspended {
			if len(w) > 0 {
				return true
			}
		}
	}
	return false
}

// ResumeAllSuspended resumes every currently-suspended site at once and clears
// any stale persisted suspended-worker lists. Called when idle-suspend is turned
// off so workers come straight back immediately, and so a site whose on-disk
// state drifted from the engine's in-memory view can't keep looking asleep.
func (e *idleEngine) ResumeAllSuspended() {
	if e == nil {
		return
	}
	// Resume the main sites the engine genuinely has suspended (workers stopped).
	e.mu.Lock()
	var mainNames []string
	for name, on := range e.suspended {
		if on {
			if _, _, isWt := splitWtKey(name); !isWt {
				mainNames = append(mainNames, name)
			}
		}
	}
	e.mu.Unlock()
	for _, name := range mainNames {
		e.resume(name)
	}

	reg, err := config.LoadSites()
	if err != nil {
		return
	}
	changed := false
	for i := range reg.Sites {
		s := reg.Sites[i]
		// Reconcile a stale main-site list (workers already running) by clearing it.
		// Skip while a suspend is in-flight: it has written the list but not yet set
		// e.suspended, so clearing now would strand it stopped with nothing to resume.
		e.mu.Lock()
		inSet := e.suspended[s.Name]
		inFlight := e.inFlight[s.Name]
		e.mu.Unlock()
		if len(s.IdleSuspendedWorkers) > 0 && !inSet && !inFlight {
			_ = config.SetSiteIdleSuspendedWorkers(s.Name, nil)
			changed = true
		}

		// Resume every suspended worktree; if its checkout is gone, just clear the
		// stale slot.
		for wtBase, workers := range s.WorktreeIdleSuspended {
			if len(workers) == 0 {
				continue
			}
			key := wtKey(s.Name, wtBase)
			e.mu.Lock()
			e.suspended[key] = true // ensure resumeWorktree proceeds
			e.mu.Unlock()
			wtPath, err := e.worktreePathForBase(&s, wtBase)
			if err != nil {
				// Transient git error: leave the worker suspended rather than clear
				// the slot and flip state, which would strand it down forever. The
				// next tick resumes it (Decide returns Resume while disabled).
				continue
			}
			if wtPath != "" {
				e.resumeWorktree(s.Name, wtBase, wtPath)
			} else {
				_ = config.SetWorktreeIdleSuspendedWorkers(s.Name, wtBase, nil)
				e.mu.Lock()
				e.suspended[key] = false
				e.mu.Unlock()
			}
			changed = true
		}
	}
	if changed {
		publishSitesChanged()
	}
}

// worktreePathForBase resolves a worktree's checkout dir from its unit-slug base
// by detecting the site's current worktrees. Returns ("", nil) when detection
// succeeds but the worktree is genuinely gone, and ("", err) on a transient git
// error so callers can tell the two apart instead of treating a hiccup as a
// removal and stranding a suspended worker.
func (e *idleEngine) worktreePathForBase(s *config.Site, wtBase string) (string, error) {
	wts, err := detectWorktrees(s.Path, s.PrimaryDomain())
	if err != nil {
		return "", err
	}
	for _, wt := range wts {
		if config.WorktreeUnitSlug(filepath.Base(wt.Path)) == wtBase {
			return wt.Path, nil
		}
	}
	return "", nil
}

// suspend stops a site's workers in the background (a vite site may run a
// one-time build first). The quick state checks happen under the mutex; the slow
// work runs in a goroutine guarded by inFlight so the tick and other sites' wakes
// are never blocked.
func (e *idleEngine) suspend(siteName string) {
	e.mu.Lock()
	if e.suspended[siteName] || e.inFlight[siteName] {
		e.mu.Unlock()
		return
	}
	e.inFlight[siteName] = true
	e.mu.Unlock()

	go func() {
		defer recoverEngine("suspend")
		defer e.clearInFlight(siteName)
		site, err := config.FindSite(siteName)
		if err != nil {
			return
		}
		workers := cli.SuspendWorkersForIdle(site)
		if len(workers) == 0 {
			return // nothing stoppable (e.g. only vite, no build yet)
		}
		if err := config.SetSiteIdleSuspendedWorkers(siteName, workers); err != nil {
			fmt.Printf("[WARN] idle-suspend persist %s: %v\n", siteName, err)
		}
		e.mu.Lock()
		e.suspended[siteName] = true
		e.mu.Unlock()
		fmt.Printf("[idle] suspended %s: %v\n", siteName, workers)
		publishSitesChanged()
	}()
}

// resume restarts a suspended site's workers in the background.
func (e *idleEngine) resume(siteName string) {
	e.mu.Lock()
	if !e.suspended[siteName] || e.inFlight[siteName] {
		e.mu.Unlock()
		return
	}
	e.inFlight[siteName] = true
	e.mu.Unlock()

	go func() {
		defer recoverEngine("resume")
		defer e.clearInFlight(siteName)
		site, err := config.FindSite(siteName)
		if err != nil {
			return
		}
		workers := site.IdleSuspendedWorkers
		cli.ResumeWorkersForIdle(site, workers)
		if err := config.SetSiteIdleSuspendedWorkers(siteName, nil); err != nil {
			fmt.Printf("[WARN] idle-resume persist %s: %v\n", siteName, err)
		}
		e.mu.Lock()
		e.suspended[siteName] = false
		e.mu.Unlock()
		fmt.Printf("[idle] resumed %s: %v\n", siteName, workers)
		publishSitesChanged()
	}()
}

// suspendWorktree stops a worktree's own workers in the background, mirroring
// suspend() but targeting the worktree's units (lerd-<w>-<site>-<wtBase>) and
// persisting under the worktree's slot.
func (e *idleEngine) suspendWorktree(siteName, wtBase, wtPath string) {
	key := wtKey(siteName, wtBase)
	e.mu.Lock()
	if e.suspended[key] || e.inFlight[key] {
		e.mu.Unlock()
		return
	}
	e.inFlight[key] = true
	e.mu.Unlock()

	go func() {
		defer recoverEngine("suspend-wt")
		defer e.clearInFlight(key)
		site, err := config.FindSite(siteName)
		if err != nil {
			return
		}
		workers := cli.SuspendWorktreeWorkersForIdle(site, wtPath)
		if len(workers) == 0 {
			return
		}
		if err := config.SetWorktreeIdleSuspendedWorkers(siteName, wtBase, workers); err != nil {
			fmt.Printf("[WARN] idle-suspend persist %s worktree %s: %v\n", siteName, wtBase, err)
		}
		e.mu.Lock()
		e.suspended[key] = true
		e.mu.Unlock()
		fmt.Printf("[idle] suspended %s (worktree %s): %v\n", siteName, wtBase, workers)
		publishSitesChanged()
	}()
}

// resumeWorktree restarts a worktree's previously suspended workers.
func (e *idleEngine) resumeWorktree(siteName, wtBase, wtPath string) {
	key := wtKey(siteName, wtBase)
	e.mu.Lock()
	if !e.suspended[key] || e.inFlight[key] {
		e.mu.Unlock()
		return
	}
	e.inFlight[key] = true
	e.mu.Unlock()

	go func() {
		defer recoverEngine("resume-wt")
		defer e.clearInFlight(key)
		site, err := config.FindSite(siteName)
		if err != nil {
			return
		}
		var workers []string
		if site.WorktreeIdleSuspended != nil {
			workers = site.WorktreeIdleSuspended[wtBase]
		}
		cli.ResumeWorktreeWorkersForIdle(site, wtPath, workers)
		if err := config.SetWorktreeIdleSuspendedWorkers(siteName, wtBase, nil); err != nil {
			fmt.Printf("[WARN] idle-resume persist %s worktree %s: %v\n", siteName, wtBase, err)
		}
		e.mu.Lock()
		e.suspended[key] = false
		e.mu.Unlock()
		fmt.Printf("[idle] resumed %s (worktree %s): %v\n", siteName, wtBase, workers)
		publishSitesChanged()
	}()
}

func (e *idleEngine) clearInFlight(siteName string) {
	e.mu.Lock()
	delete(e.inFlight, siteName)
	e.mu.Unlock()
}

// recoverEngine keeps an engine panic from taking down lerd-ui. The feature is
// best-effort; a bad tick logs and the next one tries again.
func recoverEngine(what string) {
	if r := recover(); r != nil {
		fmt.Printf("[WARN] idle engine (%s) recovered: %v\n", what, r)
	}
}
