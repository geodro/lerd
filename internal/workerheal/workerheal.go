// Package workerheal detects and recovers worker units stuck in systemd's
// "failed" state. The detector is deliberately cheap — it walks the existing
// batched unit-state cache shared with the dashboard, so polling stays free
// even on busy installs. The healer is a single primitive: reset-failed +
// start. It never writes .lerd.yaml or rewrites unit files; that belongs to
// `lerd worker add/remove/start/stop` and `lerd init`.
package workerheal

import (
	"fmt"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/siteinfo"
)

// UnhealthyWorker is a single failing/stuck worker unit.
type UnhealthyWorker struct {
	Site      string `json:"site"`
	Worker    string `json:"worker"`
	Unit      string `json:"unit"`
	State     string `json:"state"` // "failed" today; reserve for future "start-limit-hit", "expected-but-stopped"
	LastError string `json:"last_error,omitempty"`
}

// Event is one line in the streaming heal report. Dashboard, MCP, and TUI
// all consume these so progress is visible without polling.
type Event struct {
	Phase string `json:"phase"` // "starting" | "healed" | "failed" | "done"
	Site  string `json:"site,omitempty"`
	Unit  string `json:"unit,omitempty"`
	Error string `json:"error,omitempty"`
}

// Result is the aggregate report for non-streaming callers.
type Result struct {
	Healed []UnhealthyWorker `json:"healed"`
	Failed []Failure         `json:"failed"`
}

// Failure is one heal attempt that errored.
type Failure struct {
	Worker UnhealthyWorker `json:"worker"`
	Err    string          `json:"error"`
}

// nonWorkerPerSitePrefixes lists lerd-<X>-<site> patterns that match a
// registered site suffix but are NOT worker units (per-site containers
// rather than worker processes). Heal must skip these — restarting a
// crashed lerd-fp-myapp via this path is a different operation.
var nonWorkerPerSitePrefixes = map[string]bool{
	"custom": true, // lerd-custom-<site> — per-site custom container
	"fp":     true, // lerd-fp-<site>     — per-site FrankenPHP container
}

// Swappable for tests so the detector can be exercised without touching the
// real systemd unit-state cache or starting real units.
var (
	unitStatesFn  = siteinfo.AllUnitStates
	unitEnabledFn = isUnitEnabled
	healFn        = podman.StartUnit
	lastErrorFn   = readLastError
	isStoppedFn   = config.IsStopped
)

// HumanState renders an UnhealthyWorker.State for end-user copy. The machine
// values ("failed", "expected-but-stopped") read awkwardly in a sentence.
func HumanState(state string) string {
	switch state {
	case "expected-but-stopped":
		return "stopped"
	case "":
		return "failed"
	default:
		return state
	}
}

// lastErrorMaxLen caps how many characters of an error line are surfaced.
// Truncated lines keep the dashboard frame small and avoid leaking long
// stack traces over the WS push.
const lastErrorMaxLen = 220

// readLastError returns the last log line emitted for a failed worker unit.
// Best-effort: if no log source is available, the empty string is returned
// and the dashboard simply omits the error excerpt. On Linux it reads the
// systemd journal via journalctl; on macOS it tails ~/Library/Logs/lerd/
// where launchd redirects each unit's stdout+stderr.
func readLastError(unit string) string {
	if line := readLastErrorPlatform(unit); line != "" {
		if len(line) > lastErrorMaxLen {
			line = line[:lastErrorMaxLen] + "…"
		}
		return line
	}
	return ""
}

// enrichBudget caps total time spent reading journals across all units in
// one Enrich call so a slow journal can't stall the snapshot rebuild.
const enrichBudget = 500 * time.Millisecond

// Enrich populates LastError on every entry by reading the journal once per
// unit. Walks in slice order until the per-call budget is hit, leaving any
// remaining entries' LastError empty. Safe with a nil or empty slice.
// Intended for the dashboard pre-serialization step where there are
// typically 0–3 entries, so the budget is rarely exercised.
func Enrich(in []UnhealthyWorker) []UnhealthyWorker {
	deadline := time.Now().Add(enrichBudget)
	for i := range in {
		if in[i].LastError != "" {
			continue
		}
		// A cleanly-stopped worker has no error — its last journal line is just
		// the "Stopped …" / CPU-summary message, which reads as a false error.
		if in[i].State == "expected-but-stopped" {
			continue
		}
		if time.Now().After(deadline) {
			break
		}
		in[i].LastError = lastErrorFn(in[i].Unit)
	}
	return in
}

// Detect returns every worker unit systemd considers "failed". Cheap by
// design: it reads only the existing batched unit-state cache (one
// systemctl call per 3s, shared with the dashboard's enrichment path) plus
// sites.yaml. No per-site .lerd.yaml or composer.json reads, no extra
// subprocess calls. Safe to invoke from a hot endpoint.
//
// Two health problems are detected. "failed" — units that hit Restart= rate
// limits or crash repeatedly and stay stuck until reset. "expected-but-stopped"
// — units that are still enabled (systemd's wants-symlink present) yet inactive:
// `lerd worker stop` disables the unit, so an enabled-yet-stopped worker was
// knocked out some other way (an FPM restart cascading through BindsTo, a
// manual `systemctl stop`, a clean exit) and is drift, not intent. Plain
// "inactive" alone is NOT enough — that's why the enabled check matters — and
// timer-driven oneshots (a .timer sibling owns the lifecycle) are normally idle
// between ticks, so they're left alone to avoid false positives.
func Detect() ([]UnhealthyWorker, error) {
	// When lerd was intentionally stopped, its workers are meant to be down, so
	// reporting them as failing/expected-but-stopped is noise: the fix is `lerd
	// start`, not a heal. Suppress detection (and the notifications, banners, and
	// heals that read it) until the next start clears the marker.
	if isStoppedFn() {
		return nil, nil
	}
	reg, err := config.LoadSites()
	if err != nil {
		return nil, err
	}
	siteSet := make(map[string]bool, len(reg.Sites))
	// Workers a site has intentionally idle-suspended must never be reported as
	// failing or drifted — they are asleep on purpose and resume on the next
	// request, so flagging or healing them would be noise (and a heal would wake
	// the site). Index them so detection can skip them.
	suspended := make(map[string]map[string]bool, len(reg.Sites))
	for _, s := range reg.Sites {
		if s.Paused || s.Ignored {
			continue
		}
		siteSet[s.Name] = true
		if len(s.IdleSuspendedWorkers) > 0 {
			set := make(map[string]bool, len(s.IdleSuspendedWorkers))
			for _, w := range s.IdleSuspendedWorkers {
				set[w] = true
			}
			suspended[s.Name] = set
		}
	}
	if len(siteSet) == 0 {
		return nil, nil
	}

	states := unitStatesFn()
	var out []UnhealthyWorker
	for unit, state := range states {
		// The unit-state cache aliases each .service unit under both
		// "lerd-foo" and "lerd-foo.service"; pick one canonical form so
		// we don't emit duplicates. .timer units (paired oneshot
		// schedulers) are skipped — their .service sibling, if it ever
		// fails, will surface here under its own key.
		if !strings.HasSuffix(unit, ".service") {
			continue
		}
		var detected string
		switch state {
		case "failed":
			detected = "failed"
		case "inactive":
			// A timer-driven oneshot is meant to sit idle between ticks; its
			// .timer sibling owns the lifecycle, so don't flag the service.
			if _, hasTimer := states[strings.TrimSuffix(unit, ".service")+".timer"]; hasTimer {
				continue
			}
			// Only flag drift, not an intentionally-stopped (disabled) worker.
			if !unitEnabledFn(unit) {
				continue
			}
			detected = "expected-but-stopped"
		default:
			continue
		}
		body := strings.TrimPrefix(unit, "lerd-")
		body = strings.TrimSuffix(body, ".service")
		// Find the longest site-name suffix match so worker names with
		// embedded hyphens (e.g. emit-events) survive intact.
		var site, worker string
		for s := range siteSet {
			if strings.HasSuffix(body, "-"+s) {
				if len(s) > len(site) {
					site = s
					worker = strings.TrimSuffix(body, "-"+s)
				}
			}
		}
		if site == "" || worker == "" {
			continue
		}
		if nonWorkerPerSitePrefixes[worker] {
			continue
		}
		if suspended[site][worker] {
			continue // intentionally idle-suspended, not a failure
		}
		out = append(out, UnhealthyWorker{
			Site:   site,
			Worker: worker,
			Unit:   "lerd-" + worker + "-" + site,
			State:  detected,
		})
	}
	return out, nil
}

// HealUnit clears any failed state and starts the named worker unit. The
// single "fix this" primitive — every surface (CLI / UI / TUI / MCP) goes
// through here. Crucially, it does NOT touch .lerd.yaml or rewrite the
// unit file: a failed worker is a transient runtime condition, not a
// change of user intent. The reset-failed step is implicit: on Linux,
// systemd.DBusStartUnit calls DBusResetFailed first; on macOS launchd's
// bootstrap path replaces the job entirely.
func HealUnit(unit string) error {
	return healFn(unit)
}

// HealAll detects every unhealthy worker and heals them in order. emit,
// when non-nil, receives one Event per phase transition so the dashboard's
// banner and the MCP tool can stream progress instead of blocking on a
// final summary.
func HealAll(emit func(Event)) (Result, error) {
	if emit == nil {
		emit = func(Event) {}
	}
	unhealthy, err := Detect()
	if err != nil {
		emit(Event{Phase: "failed", Error: err.Error()})
		return Result{}, err
	}
	report := Result{}
	for _, u := range unhealthy {
		emit(Event{Phase: "starting", Site: u.Site, Unit: u.Unit})
		if err := HealUnit(u.Unit); err != nil {
			report.Failed = append(report.Failed, Failure{Worker: u, Err: err.Error()})
			emit(Event{Phase: "failed", Site: u.Site, Unit: u.Unit, Error: err.Error()})
			continue
		}
		report.Healed = append(report.Healed, u)
		emit(Event{Phase: "healed", Site: u.Site, Unit: u.Unit})
	}
	emit(Event{Phase: "done"})
	return report, nil
}

// Summary renders a one-line CLI-friendly summary of a Result.
func Summary(r Result) string {
	if len(r.Healed) == 0 && len(r.Failed) == 0 {
		return "No unhealthy workers."
	}
	if len(r.Failed) == 0 {
		return fmt.Sprintf("Healed %d worker(s).", len(r.Healed))
	}
	return fmt.Sprintf("Healed %d worker(s), %d failed.", len(r.Healed), len(r.Failed))
}
