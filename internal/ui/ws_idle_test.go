package ui

import (
	"testing"
	"time"
)

// chooseInterval is the only branching point that decides the cache poll
// cadence. The full state space is 4 combinations of (visible>0, idle);
// table covers them and a couple of edge values for the visible counter.
func TestChooseInterval(t *testing.T) {
	cases := []struct {
		name    string
		visible int32
		idle    bool
		want    time.Duration
	}{
		{"focused tab, active session", 1, false, intervalFocused},
		{"focused tab, idle session", 1, true, intervalIdle},
		{"no tabs, active session", 0, false, intervalIdle},
		{"no tabs, idle session", 0, true, intervalIdle},
		{"many tabs, active session", 7, false, intervalFocused},
		{"many tabs, idle session", 7, true, intervalIdle},
		{"negative counter, active session", -1, false, intervalIdle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := chooseInterval(tc.visible, tc.idle); got != tc.want {
				t.Errorf("chooseInterval(%d, %v) = %v, want %v", tc.visible, tc.idle, got, tc.want)
			}
		})
	}
}

// intervalFocused must be strictly shorter than intervalIdle, otherwise the
// "fast cadence on focused tabs" promise is meaningless. Pin the invariant
// so a future tweak to the constants can't silently break it.
func TestFocusedIntervalIsFasterThanIdle(t *testing.T) {
	if intervalFocused >= intervalIdle {
		t.Errorf("intervalFocused (%v) must be < intervalIdle (%v)", intervalFocused, intervalIdle)
	}
}
