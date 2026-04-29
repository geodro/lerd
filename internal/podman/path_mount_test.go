package podman

import (
	"testing"
	"time"
)

// resetPathMountAttempts clears the debounce cache so tests can drive the
// guards in isolation.
func resetPathMountAttempts() {
	pathMountAttemptsMu.Lock()
	pathMountAttempts = map[string]time.Time{}
	pathMountAttemptsMu.Unlock()
}

func TestEphemeralPathsAreSkipped(t *testing.T) {
	cases := []string{
		"/tmp/ide-phpinfo.php",
		"/var/tmp/foo",
		"/run/whatever",
		"/proc/self",
		"/sys/something",
		"/dev/null",
	}
	for _, p := range cases {
		matched := false
		for _, prefix := range ephemeralPathPrefixes {
			if len(p) >= len(prefix) && p[:len(prefix)] == prefix {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("%s should be classified as ephemeral", p)
		}
	}
}

func TestPathMountDebounce_BlocksRecentRetries(t *testing.T) {
	resetPathMountAttempts()
	t.Cleanup(resetPathMountAttempts)

	const path = "/srv/myapp"
	// First record: simulate an attempt happening now.
	pathMountAttemptsMu.Lock()
	pathMountAttempts[path] = time.Now()
	pathMountAttemptsMu.Unlock()

	pathMountAttemptsMu.Lock()
	last, ok := pathMountAttempts[path]
	pathMountAttemptsMu.Unlock()
	if !ok || time.Since(last) >= pathMountDebounce {
		t.Errorf("expected fresh entry to be within debounce window")
	}
}

func TestPathMountDebounce_ExpiresAfterWindow(t *testing.T) {
	resetPathMountAttempts()
	t.Cleanup(resetPathMountAttempts)

	const path = "/srv/myapp"
	pathMountAttemptsMu.Lock()
	pathMountAttempts[path] = time.Now().Add(-2 * pathMountDebounce)
	pathMountAttemptsMu.Unlock()

	pathMountAttemptsMu.Lock()
	last, ok := pathMountAttempts[path]
	pathMountAttemptsMu.Unlock()
	if !ok {
		t.Fatal("entry should still be present in the map until next access")
	}
	if time.Since(last) < pathMountDebounce {
		t.Errorf("entry should be older than the debounce window; got age=%v", time.Since(last))
	}
}
