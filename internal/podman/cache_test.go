package podman

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// newTestCache returns a fresh ContainerCache with a custom poll function.
func newTestCache(pollFn func() (string, error)) *ContainerCache {
	return &ContainerCache{
		running:  make(map[string]bool),
		interval: 10 * time.Second,
		refresh:  make(chan struct{}, 1),
		pollFn:   pollFn,
	}
}

func TestPollParsesRunningContainers(t *testing.T) {
	c := newTestCache(func() (string, error) {
		return "lerd-nginx\trunning\nlerd-mysql\texited\nlerd-redis\tRunning Up 2h", nil
	})
	c.started = true
	c.poll()

	if !c.Running("lerd-nginx") {
		t.Error("lerd-nginx should be running")
	}
	if c.Running("lerd-mysql") {
		t.Error("lerd-mysql should not be running (exited)")
	}
	if !c.Running("lerd-redis") {
		t.Error("lerd-redis should be running (Running prefix, mixed case)")
	}
	if c.Running("lerd-postgres") {
		t.Error("lerd-postgres should not be running (not in output)")
	}
}

func TestPollEmptyOutputClearsState(t *testing.T) {
	c := newTestCache(func() (string, error) {
		return "lerd-nginx\trunning", nil
	})
	c.started = true
	c.poll()
	if !c.Running("lerd-nginx") {
		t.Fatal("expected running after first poll")
	}

	// Simulate podman machine stopped: empty output (but no error).
	c.pollFn = func() (string, error) { return "", nil }
	c.poll()
	if c.Running("lerd-nginx") {
		t.Error("should not be running after empty poll (machine stopped)")
	}
}

func TestPollErrorKeepsLastState(t *testing.T) {
	// On error (e.g. VM booting) the cache should go to all-false (safe default).
	calls := 0
	c := newTestCache(func() (string, error) {
		calls++
		if calls == 1 {
			return "lerd-nginx\trunning", nil
		}
		return "", errors.New("podman machine unreachable")
	})
	c.started = true
	c.poll() // first poll: nginx running
	if !c.Running("lerd-nginx") {
		t.Fatal("expected running after first poll")
	}

	c.poll() // second poll: error — fresh map is empty
	if c.Running("lerd-nginx") {
		t.Error("on error the cache should clear (all containers appear stopped)")
	}
}

func TestRunningUsesMapWhenStarted(t *testing.T) {
	// When started=true, Running() must read from the map, never calling
	// ContainerRunning (which would spawn a real podman subprocess).
	pollCalled := false
	c := newTestCache(func() (string, error) {
		pollCalled = true
		return "lerd-nginx\trunning", nil
	})
	c.started = true
	c.poll()

	if !pollCalled {
		t.Fatal("expected poll to be called")
	}
	if !c.Running("lerd-nginx") {
		t.Error("lerd-nginx should be running from cache")
	}
	if c.Running("lerd-notexist") {
		t.Error("unknown container should not be running")
	}
}

func TestRefreshTriggersImmediatePoll(t *testing.T) {
	polled := make(chan struct{}, 10)
	c := newTestCache(func() (string, error) {
		polled <- struct{}{}
		return "lerd-nginx\trunning", nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c.mu.Lock()
	c.started = true
	c.mu.Unlock()

	// Use a very long interval so the timer doesn't fire during the test.
	c.interval = 10 * time.Minute
	go c.loop(ctx)
	// Drain the initial poll that loop() does not perform (we call poll directly).

	// Signal a refresh.
	c.Refresh()

	select {
	case <-polled:
		// good — poll was triggered
	case <-time.After(2 * time.Second):
		t.Error("Refresh() did not trigger a poll within 2s")
	}
}

func TestSetIntervalTakeEffect(t *testing.T) {
	polled := 0
	var mu sync.Mutex
	c := newTestCache(func() (string, error) {
		mu.Lock()
		polled++
		mu.Unlock()
		return "", nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c.mu.Lock()
	c.started = true
	c.mu.Unlock()

	c.interval = 50 * time.Millisecond
	go c.loop(ctx)

	time.Sleep(200 * time.Millisecond)
	cancel()

	mu.Lock()
	count := polled
	mu.Unlock()

	// With 50ms interval over 200ms we expect ~3-4 polls (not 0, not hundreds).
	if count < 2 || count > 10 {
		t.Errorf("expected 2-10 polls in 200ms with 50ms interval, got %d", count)
	}

	// Now verify SetInterval slows things down.
	polled = 0
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	c.SetInterval(10 * time.Second)
	c.mu.Lock()
	c.started = true
	c.mu.Unlock()
	go c.loop(ctx2)

	time.Sleep(200 * time.Millisecond)
	cancel2()

	mu.Lock()
	count2 := polled
	mu.Unlock()
	if count2 > 2 {
		t.Errorf("expected ≤2 polls in 200ms with 10s interval, got %d", count2)
	}
}

func TestConcurrentReads(t *testing.T) {
	c := newTestCache(func() (string, error) {
		return "lerd-nginx\trunning\nlerd-mysql\texited", nil
	})
	c.started = true
	c.poll()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.Running("lerd-nginx")
			_ = c.Running("lerd-mysql")
			_ = c.Running("lerd-redis")
		}()
	}
	wg.Wait()
}

func TestConcurrentPollAndRead(t *testing.T) {
	// Ensure no data race between background poll and concurrent reads.
	var callsMu sync.Mutex
	calls := 0
	c := newTestCache(func() (string, error) {
		callsMu.Lock()
		calls++
		n := calls
		callsMu.Unlock()
		if n%2 == 0 {
			return "lerd-nginx\trunning", nil
		}
		return "lerd-mysql\trunning", nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c.mu.Lock()
	c.started = true
	c.mu.Unlock()
	c.interval = 10 * time.Millisecond
	go c.loop(ctx)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_ = c.Running("lerd-nginx")
				_ = c.Running("lerd-mysql")
			}
		}()
	}
	wg.Wait()
}

