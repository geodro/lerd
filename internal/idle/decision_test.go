package idle

import (
	"testing"
	"time"
)

func TestDecide(t *testing.T) {
	const to = 30 * time.Minute
	cases := []struct {
		name      string
		enabled   bool
		idleFor   time.Duration
		hasRecord bool
		suspended bool
		want      Action
	}{
		{"disabled, running", false, time.Hour, true, false, ActionNone},
		{"disabled but suspended -> restore", false, time.Hour, true, true, ActionResume},
		{"enabled, no record yet (grace)", true, time.Hour, false, false, ActionNone},
		{"enabled, idle past timeout -> suspend", true, 31 * time.Minute, true, false, ActionSuspend},
		{"enabled, idle exactly timeout -> suspend", true, to, true, false, ActionSuspend},
		{"enabled, idle but already suspended", true, time.Hour, true, true, ActionNone},
		{"enabled, active and running", true, time.Minute, true, false, ActionNone},
		{"enabled, active but suspended -> resume", true, time.Minute, true, true, ActionResume},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Decide(tc.enabled, to, tc.idleFor, tc.hasRecord, tc.suspended)
			if got != tc.want {
				t.Errorf("Decide() = %v, want %v", got, tc.want)
			}
		})
	}
}
