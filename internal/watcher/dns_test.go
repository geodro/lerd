package watcher

import (
	"errors"
	"testing"
	"time"
)

// TestTickDNS pins the publish-on-transition behaviour. A regression
// that drops a publish would silently break post-boot dashboard
// staleness recovery without any other test catching it.
func TestTickDNS(t *testing.T) {
	type result struct {
		checks    int
		waits     int
		repairs   int
		publishes int
		logs      []string
	}

	cases := []struct {
		name string
		// state going in
		lastOK    *bool
		tickCount int
		// dependency outcomes for this tick
		idleOrLocked bool
		checkOK      bool
		waitErr      error
		repairErr    error
		// expectations
		wantChecks      int
		wantWaits       int
		wantRepairs     int
		wantPublishes   int
		wantLogs        int
		wantLogKinds    []string // levels in order
		wantLastOKAfter *bool
	}{
		{
			// Boot case: dashboard rendered dns.ok=false from the cold
			// snapshot. First observation must publish so the UI catches
			// up without a manual refresh.
			name:            "first tick up publishes",
			lastOK:          nil,
			checkOK:         true,
			wantChecks:      1,
			wantPublishes:   1,
			wantLastOKAfter: ptrBool(true),
		},
		{
			// First tick observes DNS down. Repair pipeline runs; a
			// successful repair publishes once for the down observation
			// and once for the post-repair up flip. Two publishes total.
			name:            "first tick down with successful repair publishes twice",
			lastOK:          nil,
			checkOK:         false,
			wantChecks:      1,
			wantWaits:       1,
			wantRepairs:     1,
			wantPublishes:   2,
			wantLogs:        2,
			wantLogKinds:    []string{"warn", "info"},
			wantLastOKAfter: ptrBool(true),
		},
		{
			// Steady state: nothing changed since last tick, no publish.
			// This is the bulk of ticks on a healthy machine and the path
			// that has to stay quiet to keep CPU near zero.
			name:            "steady up no publish",
			lastOK:          ptrBool(true),
			checkOK:         true,
			wantChecks:      1,
			wantPublishes:   0,
			wantLastOKAfter: ptrBool(true),
		},
		{
			// DNS broke between ticks. Publish the down transition, then
			// repair succeeds and publishes the up flip.
			name:            "up to down with successful repair publishes twice",
			lastOK:          ptrBool(true),
			checkOK:         false,
			wantChecks:      1,
			wantWaits:       1,
			wantRepairs:     1,
			wantPublishes:   2,
			wantLogs:        2,
			wantLogKinds:    []string{"warn", "info"},
			wantLastOKAfter: ptrBool(true),
		},
		{
			// DNS broke and the repair pipeline can't even confirm
			// lerd-dns is ready. Publish the down transition only — we
			// haven't restored anything, so don't lie about up.
			name:            "down with wait failure publishes once",
			lastOK:          ptrBool(true),
			checkOK:         false,
			waitErr:         errors.New("timeout"),
			wantChecks:      1,
			wantWaits:       1,
			wantRepairs:     0,
			wantPublishes:   1,
			wantLogs:        2,
			wantLogKinds:    []string{"warn", "error"},
			wantLastOKAfter: ptrBool(false),
		},
		{
			// Repair attempt actually ran but configureResolver errored.
			// Same outcome as wait failure for the publish count: down
			// transition only, no up flip.
			name:            "down with repair failure publishes once",
			lastOK:          ptrBool(true),
			checkOK:         false,
			repairErr:       errors.New("write resolv.conf: permission denied"),
			wantChecks:      1,
			wantWaits:       1,
			wantRepairs:     1,
			wantPublishes:   1,
			wantLogs:        2,
			wantLogKinds:    []string{"warn", "error"},
			wantLastOKAfter: ptrBool(false),
		},
		{
			// Steady-down: prior was already down, only the post-repair
			// up flip publishes; no down-transition publish since nothing
			// transitioned on the down side.
			name:            "steady down with successful repair publishes once",
			lastOK:          ptrBool(false),
			checkOK:         false,
			wantChecks:      1,
			wantWaits:       1,
			wantRepairs:     1,
			wantPublishes:   1,
			wantLogs:        2,
			wantLogKinds:    []string{"warn", "info"},
			wantLastOKAfter: ptrBool(true),
		},
		{
			// Idle-skip path: laptop locked, this is not the every-Nth
			// tick, watcher must do nothing — no probe, no publish, no
			// state mutation. This is the battery-saving guarantee.
			name:            "idle skipped tick is silent",
			lastOK:          ptrBool(true),
			tickCount:       0, // becomes 1 after increment, 1 % 10 != 0 -> skip
			idleOrLocked:    true,
			wantChecks:      0,
			wantPublishes:   0,
			wantLastOKAfter: ptrBool(true),
		},
		{
			// Idle-but-Nth-tick: must run a real probe even on a locked
			// session so a returning user hits a healed DNS without the
			// 0.5–1s resolver timeout that prompted idleSkipEveryN.
			name:            "idle Nth tick still probes",
			lastOK:          ptrBool(true),
			tickCount:       9, // becomes 10, 10 % 10 == 0 -> probe
			idleOrLocked:    true,
			checkOK:         true,
			wantChecks:      1,
			wantPublishes:   0,
			wantLastOKAfter: ptrBool(true),
		},
		{
			// Natural recovery: lerd-dns came up between ticks without
			// a repair. Publish the transition, take no other action.
			name:            "down to up natural recovery publishes",
			lastOK:          ptrBool(false),
			checkOK:         true,
			wantChecks:      1,
			wantPublishes:   1,
			wantLastOKAfter: ptrBool(true),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got result
			deps := dnsWatchDeps{
				check: func(string) (bool, error) {
					got.checks++
					return c.checkOK, nil
				},
				waitReady: func(time.Duration) error {
					got.waits++
					return c.waitErr
				},
				configureResolver: func() error {
					got.repairs++
					return c.repairErr
				},
				idleOrLocked: func() bool { return c.idleOrLocked },
				publishStatus: func() {
					got.publishes++
				},
				log: func(level, _ string, _ ...any) {
					got.logs = append(got.logs, level)
				},
			}
			state := &dnsWatchState{lastOK: c.lastOK, tickCount: c.tickCount}
			tickDNS(deps, state, "test")

			if got.checks != c.wantChecks {
				t.Errorf("check() calls=%d, want %d", got.checks, c.wantChecks)
			}
			if got.waits != c.wantWaits {
				t.Errorf("waitReady() calls=%d, want %d", got.waits, c.wantWaits)
			}
			if got.repairs != c.wantRepairs {
				t.Errorf("configureResolver() calls=%d, want %d", got.repairs, c.wantRepairs)
			}
			if got.publishes != c.wantPublishes {
				t.Errorf("publishes=%d, want %d", got.publishes, c.wantPublishes)
			}
			if len(got.logs) != c.wantLogs {
				t.Errorf("logs=%d, want %d (%v)", len(got.logs), c.wantLogs, got.logs)
			}
			for i, want := range c.wantLogKinds {
				if i >= len(got.logs) {
					t.Errorf("missing log[%d]: want %q", i, want)
					continue
				}
				if got.logs[i] != want {
					t.Errorf("log[%d]=%q, want %q", i, got.logs[i], want)
				}
			}
			if !ptrBoolEq(state.lastOK, c.wantLastOKAfter) {
				t.Errorf("lastOK after tick=%v, want %v", deref(state.lastOK), deref(c.wantLastOKAfter))
			}
		})
	}
}

func ptrBool(b bool) *bool { return &b }

func ptrBoolEq(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func deref(p *bool) any {
	if p == nil {
		return "<nil>"
	}
	return *p
}
