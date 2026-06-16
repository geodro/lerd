package watcher

import (
	"net"
	"testing"
	"time"

	"github.com/geodro/lerd/internal/idle"
)

// TestPublishSitesChanged_nonBlockingAndCoalesces proves a burst of refresh
// requests never blocks the caller (no consumer here) and collapses to a single
// pending token, so an activity-ping flood can't fan out into many UI POSTs.
func TestPublishSitesChanged_nonBlockingAndCoalesces(t *testing.T) {
	select { // start from a clean channel regardless of other tests
	case <-notifyDirty:
	default:
	}

	for i := 0; i < 100; i++ {
		publishSitesChanged() // must not block without a running notifier
	}

	select {
	case <-notifyDirty:
	default:
		t.Fatal("expected one pending refresh token after a burst")
	}
	select {
	case <-notifyDirty:
		t.Fatal("burst should coalesce to a single token, not queue many")
	default:
	}
}

// TestEnableDisableIdle_lifecycle proves the session starts on enable and tears
// down on disable (the source watcher is started then stopped), and that both
// calls are idempotent — the core of "off does nothing".
func TestEnableDisableIdle_lifecycle(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Cleanup(func() {
		disableIdle()
		idleStartSrc = nil
		activityTracker = nil
		idleEng = nil
	})
	activityTracker = idle.NewTracker(nil)
	idleEng = newIdleEngine(activityTracker)

	srcStarted := make(chan struct{}, 2)
	srcStopped := make(chan struct{}, 1)
	idleStartSrc = func(stop <-chan struct{}) error {
		srcStarted <- struct{}{}
		<-stop
		srcStopped <- struct{}{}
		return nil
	}

	enableIdle()
	if !idleActive.Load() {
		t.Fatal("enableIdle must set idleActive")
	}
	select {
	case <-srcStarted:
	case <-time.After(time.Second):
		t.Fatal("source watcher was not started on enable")
	}

	enableIdle() // idempotent: must not start a second source watcher
	select {
	case <-srcStarted:
		t.Fatal("second enableIdle started a duplicate source watcher")
	case <-time.After(100 * time.Millisecond):
	}

	disableIdle()
	if idleActive.Load() {
		t.Fatal("disableIdle must clear idleActive")
	}
	select {
	case <-srcStopped:
	case <-time.After(time.Second):
		t.Fatal("source watcher was not stopped on disable")
	}

	disableIdle() // idempotent: a second disable is a no-op
}

// TestResumeUntilClear_exitsWhenClean proves the disable-path resume returns
// promptly once nothing is left suspended, rather than spinning its full bound.
func TestResumeUntilClear_exitsWhenClean(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	prev := activityTracker
	t.Cleanup(func() { activityTracker = prev; idleEng = nil; idleActive.Store(false) })
	activityTracker = idle.NewTracker(nil)
	idleEng = newIdleEngine(activityTracker)
	idleActive.Store(false)

	done := make(chan struct{})
	go func() { idleEng.resumeUntilClear(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("resumeUntilClear should return at once when nothing is suspended")
	}
}

// TestResumeUntilClear_bailsWhenReenabled proves the drain bails the moment the
// feature is re-enabled, so it never fights a fresh session's suspends.
func TestResumeUntilClear_bailsWhenReenabled(t *testing.T) {
	prev := activityTracker
	t.Cleanup(func() { activityTracker = prev; idleEng = nil; idleActive.Store(false) })
	activityTracker = idle.NewTracker(nil)
	idleEng = newIdleEngine(activityTracker)
	idleActive.Store(true)

	done := make(chan struct{})
	go func() { idleEng.resumeUntilClear(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("resumeUntilClear must bail when the feature is re-enabled")
	}
}

func TestParseControlMsg(t *testing.T) {
	cases := []struct {
		in, kind, arg string
	}{
		{"activity myapp", "activity", "myapp"},
		{"activity myapp\n", "activity", "myapp"},
		{"  activity   my site  ", "activity", "my site"},
		{"disable", "disable", ""},
		{"enable\n\x00", "enable", ""},
		{"", "", ""},
		{"   ", "", ""},
		{"activity", "activity", ""},
	}
	for _, c := range cases {
		kind, arg := parseControlMsg(c.in)
		if kind != c.kind || arg != c.arg {
			t.Errorf("parseControlMsg(%q) = (%q, %q), want (%q, %q)", c.in, kind, arg, c.kind, c.arg)
		}
	}
}

// TestDispatchControl_activityTouchesTracker proves an enabled "activity <site>"
// line records a site touch. idleEng stays nil (OnActivity is nil-safe; a
// not-suspended site is a no-op anyway).
func TestDispatchControl_activityTouchesTracker(t *testing.T) {
	prev := activityTracker
	t.Cleanup(func() { activityTracker = prev; idleActive.Store(false) })
	activityTracker = idle.NewTracker(nil)
	idleActive.Store(true)

	dispatchControl("activity myapp")

	if _, ok := activityTracker.LastActive("myapp"); !ok {
		t.Error("activity control message should record a site touch")
	}
}

// TestDispatchControl_activityIgnoredWhenDisabled proves a disabled feature does
// no work on an activity ping: the tracker is never touched, so off is dormant.
func TestDispatchControl_activityIgnoredWhenDisabled(t *testing.T) {
	prev := activityTracker
	t.Cleanup(func() { activityTracker = prev })
	activityTracker = idle.NewTracker(nil)
	idleActive.Store(false)

	dispatchControl("activity myapp")

	if _, ok := activityTracker.LastActive("myapp"); ok {
		t.Error("a disabled feature must ignore activity pings entirely")
	}
}

// TestServeControl_roundTrip drives a real unix datagram socket end to end:
// bind it, send an activity line, and assert it lands as a touch. Mirrors the
// access-feed socket test so the control path's framing is covered too.
func TestServeControl_roundTrip(t *testing.T) {
	prev := activityTracker
	t.Cleanup(func() { activityTracker = prev; idleActive.Store(false) })
	activityTracker = idle.NewTracker(nil)
	idleActive.Store(true)

	t.Chdir(t.TempDir())
	conn, err := net.ListenPacket("unixgram", "control.sock")
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	go readDatagrams(conn, func(b []byte) { dispatchControl(string(b)) })

	sender, err := net.Dial("unixgram", "control.sock")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer sender.Close()
	if _, err := sender.Write([]byte("activity myapp\n")); err != nil {
		t.Fatalf("send: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := activityTracker.LastActive("myapp"); ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("control datagram was not recorded as a site touch within 2s")
}
