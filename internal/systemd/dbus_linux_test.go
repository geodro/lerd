//go:build linux

package systemd

import "testing"

// withServiceSuffix is the gate every DBus unit op passes through. A bug here
// breaks Start/Stop/Restart/Enable/Disable/IsEnabled for every caller, so the
// table covers the inputs each callsite actually emits.
func TestWithServiceSuffix(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"lerd-ui", "lerd-ui.service"},
		{"lerd-watcher", "lerd-watcher.service"},
		{"lerd-queue-myapp", "lerd-queue-myapp.service"},
		{"lerd-ui.service", "lerd-ui.service"},
		{"lerd-test.timer", "lerd-test.timer"},
		{"lerd-stripe-my.app", "lerd-stripe-my.app"},
		{"", ".service"},
	}
	for _, tc := range cases {
		if got := withServiceSuffix(tc.in); got != tc.want {
			t.Errorf("withServiceSuffix(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// withDefaultSuffix is used by property lookups that may legitimately target
// a .timer or a .service. Bare names default to .service; explicit suffixes
// pass through verbatim so IsTimerActive's name+".timer" composition works.
func TestWithDefaultSuffix(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"lerd-ui", "lerd-ui.service"},
		{"lerd-test.timer", "lerd-test.timer"},
		{"lerd-test.service", "lerd-test.service"},
		{"lerd-queue-myapp.timer", "lerd-queue-myapp.timer"},
		{"some.weird.name", "some.weird.name"},
	}
	for _, tc := range cases {
		if got := withDefaultSuffix(tc.in); got != tc.want {
			t.Errorf("withDefaultSuffix(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// IsTimerActive composes name+".timer" and asks DBus. The suffix rule must
// pass ".timer" through unchanged so IsTimerActive doesn't end up querying
// "lerd-foo.timer.service".
func TestTimerSuffixIsNotDoubleAppended(t *testing.T) {
	if got := withServiceSuffix("lerd-foo.timer"); got != "lerd-foo.timer" {
		t.Errorf("withServiceSuffix(%q) = %q, want passthrough — IsTimerActive would query the wrong unit", "lerd-foo.timer", got)
	}
	if got := withDefaultSuffix("lerd-foo.timer"); got != "lerd-foo.timer" {
		t.Errorf("withDefaultSuffix(%q) = %q, want passthrough", "lerd-foo.timer", got)
	}
}

// NotifyReady and NotifyStopping must be safe to call outside a systemd
// notify-socket context: bare CLI runs (lerd serve-ui from a terminal,
// go test) have no $NOTIFY_SOCKET and the underlying daemon.SdNotify
// returns (false, nil) — no panic, no error surfaced.
func TestNotifyReadyAndStoppingAreSafeWithoutSocket(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "")
	NotifyReady()
	NotifyStopping()
}
