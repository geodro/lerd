package watcher

import (
	"errors"
	"testing"
)

// tickHostGateway is the decision point for the host-gateway watcher.
// These table-driven tests pin its states so a future refactor that
// breaks any of them (e.g. rewriting when it shouldn't, or failing to
// rewrite when it should) fails loudly in CI.
func TestTickHostGateway(t *testing.T) {
	type result struct {
		wrote             bool
		reachableCalls    int
		detectFreshCalled bool
		logs              []string
	}

	cases := []struct {
		name                  string
		lastLAN               string
		currentLAN            string
		current               string
		reachable             bool
		fresh                 string
		writeErr              error
		wantWrote             bool
		wantReachableCalls    int
		wantDetectFreshCalled bool
		wantLogs              int
		wantLogKind           string // "info" or "warn" if a log was emitted
		wantLastLANAfterTick  string
	}{
		{
			// Fast-path: the common case on a stationary machine. LAN IP
			// is unchanged from the last tick, so we short-circuit before
			// touching podman. This is the ~99.99% path on a desktop and
			// the whole reason the optimization exists — a podman exec
			// per tick would burn 1-3 % CPU on macOS (gvproxy hop costs
			// ~300 ms – 1 s per exec).
			name:                 "lan unchanged, fast path",
			lastLAN:              "192.168.1.10",
			currentLAN:           "192.168.1.10",
			wantWrote:            false,
			wantReachableCalls:   0, // must NOT call reachable() — that's a podman exec
			wantLogs:             0,
			wantLastLANAfterTick: "192.168.1.10",
		},
		{
			// LAN changed but the old /etc/hosts entry still reaches the
			// host (e.g. moved VPNs but the old probe address is still
			// routable). No rewrite, but we did pay for the podman exec
			// because the LAN change triggered the slow path.
			name:                 "lan changed, current still reachable",
			lastLAN:              "192.168.1.10",
			currentLAN:           "10.0.0.50",
			current:              "192.168.1.10",
			reachable:            true,
			wantWrote:            false,
			wantReachableCalls:   1,
			wantLogs:             0,
			wantLastLANAfterTick: "10.0.0.50",
		},
		{
			// Coffee-shop case, the whole reason the watcher exists:
			// laptop moved networks, old IP no longer routes, probe
			// finds a new working one, watcher rewrites /etc/hosts and
			// Xdebug starts working again without user action.
			name:                  "lan changed, stale entry, probe finds new",
			lastLAN:               "192.168.1.10",
			currentLAN:            "10.0.0.50",
			current:               "192.168.1.10",
			reachable:             false,
			fresh:                 "10.0.0.50",
			wantWrote:             true,
			wantReachableCalls:    1,
			wantDetectFreshCalled: true,
			wantLogs:              1, wantLogKind: "info",
			wantLastLANAfterTick: "10.0.0.50",
		},
		{
			// LAN changed but the laptop is offline or lerd-nginx is
			// down between ticks: probe returns "". Must NOT overwrite
			// with the legacy fallback — that would make things worse.
			// Try again next tick.
			name:                  "lan changed, probe finds nothing",
			lastLAN:               "192.168.1.10",
			currentLAN:            "10.0.0.50",
			current:               "192.168.1.10",
			reachable:             false,
			fresh:                 "",
			wantWrote:             false,
			wantReachableCalls:    1,
			wantDetectFreshCalled: true,
			wantLogs:              0,
			wantLastLANAfterTick:  "10.0.0.50",
		},
		{
			// Regression: probe reports the same IP already on disk (can
			// happen on macOS where gvproxy's address doesn't depend on
			// LAN IP). Skip the write so we don't thrash the bind-mounted
			// file and trigger spurious inotify events.
			name:                  "lan changed but probe confirms current",
			lastLAN:               "192.168.1.10",
			currentLAN:            "10.0.0.50",
			current:               "192.168.127.254", // gvproxy address
			reachable:             false,
			fresh:                 "192.168.127.254",
			wantWrote:             false,
			wantReachableCalls:    1,
			wantDetectFreshCalled: true,
			wantLogs:              0,
			wantLastLANAfterTick:  "10.0.0.50",
		},
		{
			// Write fails mid-flight. Log warn, advance last-known LAN
			// anyway so we don't spin on the same failure every tick.
			name:                  "write error is surfaced",
			lastLAN:               "192.168.1.10",
			currentLAN:            "10.0.0.50",
			current:               "192.168.1.10",
			reachable:             false,
			fresh:                 "10.0.0.50",
			writeErr:              errors.New("disk full"),
			wantWrote:             true,
			wantReachableCalls:    1,
			wantDetectFreshCalled: true,
			wantLogs:              1, wantLogKind: "warn",
			wantLastLANAfterTick: "10.0.0.50",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got result
			deps := hostGatewayDeps{
				primaryLANIP: func() string { return c.currentLAN },
				readCurrent:  func() string { return c.current },
				reachable: func(ip string) bool {
					got.reachableCalls++
					return c.reachable && ip == c.current
				},
				detectFresh: func() string {
					got.detectFreshCalled = true
					return c.fresh
				},
				writeHosts: func() error {
					got.wrote = true
					return c.writeErr
				},
				log: func(level, _ string, _ ...any) {
					got.logs = append(got.logs, level)
				},
			}
			state := &hostGatewayState{lastLAN: c.lastLAN}
			tickHostGateway(deps, state)

			if got.wrote != c.wantWrote {
				t.Errorf("wrote=%v, want %v", got.wrote, c.wantWrote)
			}
			if got.reachableCalls != c.wantReachableCalls {
				t.Errorf("reachable() calls=%d, want %d", got.reachableCalls, c.wantReachableCalls)
			}
			if got.detectFreshCalled != c.wantDetectFreshCalled {
				t.Errorf("detectFresh() called=%v, want %v", got.detectFreshCalled, c.wantDetectFreshCalled)
			}
			if len(got.logs) != c.wantLogs {
				t.Errorf("logs=%d, want %d (%v)", len(got.logs), c.wantLogs, got.logs)
			}
			if c.wantLogs > 0 && len(got.logs) > 0 && got.logs[0] != c.wantLogKind {
				t.Errorf("log kind=%q, want %q", got.logs[0], c.wantLogKind)
			}
			if state.lastLAN != c.wantLastLANAfterTick {
				t.Errorf("lastLAN after tick=%q, want %q", state.lastLAN, c.wantLastLANAfterTick)
			}
		})
	}
}

// tickNginxDrift rewrites the container-hosts file when lerd-nginx has been
// renumbered on the lerd bridge without a LAN change (container rebalance,
// network recreate). These tests pin each decision branch.
func TestTickNginxDrift(t *testing.T) {
	cases := []struct {
		name      string
		onDisk    string
		live      string
		writeErr  error
		wantWrote bool
		wantLogs  int
		wantKind  string
	}{
		{
			name:   "no .test entries yet, skip (no sites linked)",
			onDisk: "", live: "10.89.0.25",
			wantWrote: false, wantLogs: 0,
		},
		{
			name:   "nginx not running, skip",
			onDisk: "10.89.0.15", live: "",
			wantWrote: false, wantLogs: 0,
		},
		{
			name:   "disk and live agree, no rewrite",
			onDisk: "10.89.0.25", live: "10.89.0.25",
			wantWrote: false, wantLogs: 0,
		},
		{
			name:   "drift detected, rewrite and log",
			onDisk: "10.89.0.15", live: "10.89.0.25",
			wantWrote: true, wantLogs: 1, wantKind: "info",
		},
		{
			name:   "drift detected but write fails, warn",
			onDisk: "10.89.0.15", live: "10.89.0.25",
			writeErr:  errors.New("disk full"),
			wantWrote: true, wantLogs: 1, wantKind: "warn",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wrote := false
			var logs []string
			deps := hostGatewayDeps{
				readNginxOnDisk: func() string { return c.onDisk },
				liveNginxIP:     func() string { return c.live },
				writeHosts: func() error {
					wrote = true
					return c.writeErr
				},
				log: func(level, _ string, _ ...any) {
					logs = append(logs, level)
				},
			}
			tickNginxDrift(deps)
			if wrote != c.wantWrote {
				t.Errorf("wrote=%v, want %v", wrote, c.wantWrote)
			}
			if len(logs) != c.wantLogs {
				t.Errorf("logs=%v, want %d", logs, c.wantLogs)
			}
			if c.wantLogs > 0 && len(logs) > 0 && logs[0] != c.wantKind {
				t.Errorf("log kind=%q, want %q", logs[0], c.wantKind)
			}
		})
	}
}

func TestTickNginxDrift_nilFuncsAreSafe(t *testing.T) {
	// Defensive: if a caller constructs hostGatewayDeps without the new
	// fields the tick must not panic — existing LAN-only tests exercise
	// this same path.
	tickNginxDrift(hostGatewayDeps{})
}

// TestTickHostGateway_driftGated verifies the battery-friendly cadence:
// nginx drift only fires once per driftEveryN ticks, regardless of the
// LAN path. Keeps podman inspect cost off the critical path on laptops.
func TestTickHostGateway_driftGated(t *testing.T) {
	cases := []struct {
		name         string
		driftEveryN  int
		ticks        int
		wantDriftRun int
	}{
		{name: "every tick when N=1", driftEveryN: 1, ticks: 5, wantDriftRun: 5},
		{name: "every 3rd tick when N=3", driftEveryN: 3, ticks: 9, wantDriftRun: 3},
		{name: "every 10th tick when N=10 (Linux default)", driftEveryN: 10, ticks: 30, wantDriftRun: 3},
		{name: "every 30th tick when N=30 (macOS default)", driftEveryN: 30, ticks: 60, wantDriftRun: 2},
		{name: "N=0 treated as 1 (safe default)", driftEveryN: 0, ticks: 4, wantDriftRun: 4},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			driftCalls := 0
			deps := hostGatewayDeps{
				primaryLANIP: func() string { return "10.0.0.1" },
				readCurrent:  func() string { return "10.0.0.1" },
				reachable:    func(string) bool { return true },
				readNginxOnDisk: func() string {
					driftCalls++
					return ""
				},
				liveNginxIP: func() string { return "" },
				driftEveryN: c.driftEveryN,
				log:         func(string, string, ...any) {},
			}
			state := &hostGatewayState{lastLAN: "10.0.0.1"}
			for i := 0; i < c.ticks; i++ {
				tickHostGateway(deps, state)
			}
			if driftCalls != c.wantDriftRun {
				t.Errorf("drift ran %d times in %d ticks with N=%d, want %d",
					driftCalls, c.ticks, c.driftEveryN, c.wantDriftRun)
			}
		})
	}
}

// TestNginxDriftDefault documents the platform-specific polling cadence
// so a future refactor that changes these constants fails CI loudly and
// forces a human to think about the battery tradeoff.
func TestNginxDriftDefault(t *testing.T) {
	n := nginxDriftDefault()
	if n < 1 {
		t.Errorf("nginxDriftDefault() = %d, must be >= 1 (0 would spam every tick)", n)
	}
	if n > 60 {
		t.Errorf("nginxDriftDefault() = %d, pushing past 30min (60 ticks) makes drift detection effectively useless", n)
	}
}
