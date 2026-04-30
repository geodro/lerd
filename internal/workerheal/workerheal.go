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

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/siteinfo"
)

// UnhealthyWorker is a single failing/stuck worker unit.
type UnhealthyWorker struct {
	Site   string `json:"site"`
	Worker string `json:"worker"`
	Unit   string `json:"unit"`
	State  string `json:"state"` // "failed" today; reserve for future "start-limit-hit", "expected-but-stopped"
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
	unitStatesFn = siteinfo.AllUnitStates
	healFn       = podman.StartUnit
)

// Detect returns every worker unit systemd considers "failed". Cheap by
// design: it reads only the existing batched unit-state cache (one
// systemctl call per 3s, shared with the dashboard's enrichment path) plus
// sites.yaml. No per-site .lerd.yaml or composer.json reads, no extra
// subprocess calls. Safe to invoke from a hot endpoint.
//
// Heuristic kept narrow on purpose: worker units that hit Restart= rate
// limits or crash repeatedly land in "failed" and stay there until
// something resets them. "Inactive" is too broad — users routinely stop
// individual workers on purpose, and we can't tell intent apart from
// drift without an explicit per-worker desired-state field.
func Detect() ([]UnhealthyWorker, error) {
	reg, err := config.LoadSites()
	if err != nil {
		return nil, err
	}
	siteSet := make(map[string]bool, len(reg.Sites))
	for _, s := range reg.Sites {
		if s.Paused || s.Ignored {
			continue
		}
		siteSet[s.Name] = true
	}
	if len(siteSet) == 0 {
		return nil, nil
	}

	states := unitStatesFn()
	var out []UnhealthyWorker
	for unit, state := range states {
		if state != "failed" {
			continue
		}
		// The unit-state cache aliases each .service unit under both
		// "lerd-foo" and "lerd-foo.service"; pick one canonical form so
		// we don't emit duplicates. .timer units (paired oneshot
		// schedulers) are skipped — their .service sibling, if it ever
		// fails, will surface here under its own key.
		if !strings.HasSuffix(unit, ".service") {
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
		out = append(out, UnhealthyWorker{
			Site:   site,
			Worker: worker,
			Unit:   "lerd-" + worker + "-" + site,
			State:  "failed",
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
