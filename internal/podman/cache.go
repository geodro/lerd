package podman

import (
	"context"
	"strings"
	"sync"
	"time"
)

// ContainerCache polls podman for container states on a configurable interval
// and serves reads from an in-memory snapshot. One background goroutine does
// all the work; every other caller reads from the map without spawning any
// subprocesses.
type ContainerCache struct {
	mu      sync.RWMutex
	running map[string]bool
	started bool

	intervalMu sync.Mutex
	interval   time.Duration

	// refresh is signalled to trigger an immediate out-of-cycle refresh
	// (e.g. after a container start/stop mutation).
	refresh chan struct{}

	// pollFn fetches container states; defaults to the real podman ps call.
	// Swappable in tests.
	pollFn func() (string, error)

	// onChangeMu guards onChange; readers load it under the read side of mu.
	onChangeMu sync.Mutex
	onChange   func()
}

// SetOnChange installs a callback that fires after a poll detects a change
// in the running map. Used by the daemon to trigger a snapshot publish on
// external state changes (container crash, systemctl outside of lerd-ui)
// without paying for a DBus PropertiesChanged subscription. Pass nil to
// remove the callback.
func (c *ContainerCache) SetOnChange(fn func()) {
	c.onChangeMu.Lock()
	c.onChange = fn
	c.onChangeMu.Unlock()
}

func defaultPollFn() (string, error) {
	return Run("ps", "-a",
		"--filter", "name=lerd-",
		"--format", "{{.Names}}\t{{.State}}")
}

// Cache is the process-wide container state store. Start it once from serve-ui;
// CLI commands that don't call Start fall back to direct podman inspect.
var Cache = &ContainerCache{
	running:  make(map[string]bool),
	interval: 15 * time.Second,
	refresh:  make(chan struct{}, 1),
	pollFn:   defaultPollFn,
}

// Start launches the background refresh loop. Safe to call only once.
func (c *ContainerCache) Start(ctx context.Context) {
	c.mu.Lock()
	c.started = true
	c.mu.Unlock()

	c.poll() // initial population before returning
	go c.loop(ctx)
}

// Running returns true if the named container is currently running.
// If the cache has not been started (CLI context), it falls back to a direct
// podman inspect so one-off commands still work correctly.
func (c *ContainerCache) Running(name string) bool {
	c.mu.RLock()
	started := c.started
	v := c.running[name]
	c.mu.RUnlock()

	if !started {
		running, _ := ContainerRunning(name)
		return running
	}
	return v
}

// Snapshot returns a copy of the cached container map. When the cache has not
// been started (CLI context), it falls back to a synchronous podman ps so
// one-off commands still see current state. Used by callers that need to
// enumerate containers by name pattern without spawning their own subprocess
// on the daemon's hot path.
func (c *ContainerCache) Snapshot() map[string]bool {
	c.mu.RLock()
	if c.started {
		out := make(map[string]bool, len(c.running))
		for k, v := range c.running {
			out[k] = v
		}
		c.mu.RUnlock()
		return out
	}
	c.mu.RUnlock()

	out, err := c.pollFn()
	fresh := make(map[string]bool)
	if err != nil {
		return fresh
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		state := strings.ToLower(strings.TrimSpace(parts[1]))
		fresh[name] = strings.HasPrefix(state, "running")
	}
	return fresh
}

// Refresh schedules an immediate re-poll on the background goroutine.
// Returns without blocking; at most one pending refresh is queued.
func (c *ContainerCache) Refresh() {
	select {
	case c.refresh <- struct{}{}:
	default:
	}
}

// PollNow runs a synchronous poll and updates the in-memory state before
// returning. Use this when the caller needs current data immediately (e.g.
// the initial WebSocket snapshot). Unlike Refresh, it blocks until done.
func (c *ContainerCache) PollNow() {
	c.poll()
}

// SetInterval changes the background polling interval. Safe to call from any goroutine.
func (c *ContainerCache) SetInterval(d time.Duration) {
	c.intervalMu.Lock()
	c.interval = d
	c.intervalMu.Unlock()
}

func (c *ContainerCache) loop(ctx context.Context) {
	for {
		c.intervalMu.Lock()
		d := c.interval
		c.intervalMu.Unlock()

		select {
		case <-ctx.Done():
			return
		case <-time.After(d):
			c.poll()
		case <-c.refresh:
			c.poll()
		}
	}
}

// poll runs a single podman ps and updates the running map.
// One subprocess per cycle instead of one per container.
func (c *ContainerCache) poll() {
	out, err := c.pollFn()

	fresh := make(map[string]bool)
	if err == nil {
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, "\t", 2)
			if len(parts) != 2 {
				continue
			}
			name := strings.TrimSpace(parts[0])
			state := strings.ToLower(strings.TrimSpace(parts[1]))
			fresh[name] = strings.HasPrefix(state, "running")
		}
	}
	// On error (e.g. machine stopped) fresh is empty — all containers appear
	// as not running, which is the correct observed state.

	c.mu.Lock()
	changed := !runningMapsEqual(c.running, fresh)
	c.running = fresh
	c.mu.Unlock()

	if !changed {
		return
	}
	c.onChangeMu.Lock()
	cb := c.onChange
	c.onChangeMu.Unlock()
	if cb != nil {
		cb()
	}
}

// runningMapsEqual compares two name→running maps without allocating.
func runningMapsEqual(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}
