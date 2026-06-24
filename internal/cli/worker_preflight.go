package cli

import (
	"errors"
	"fmt"

	"github.com/geodro/lerd/internal/config"
)

// workerStartPreflight gates a WorkerStartForSite call on the framework's
// declared dependency rules. Two checks, in order:
//
//   - Check (Composer / File): the rule must MATCH for the worker to be
//     eligible. Used to gate optional packages — e.g. reverb's
//     `Composer: "laravel/reverb"` so a site without the package doesn't
//     try to launch `php artisan reverb:start`, which fails with
//     "There are no commands defined in the 'reverb' namespace".
//
//   - ExcludeCheck (Composer / File): the rule must NOT match. Used to
//     hide a worker when a superseding package is present — e.g. queue's
//     `ExcludeCheck: laravel/horizon` so we don't run `queue:work` on a
//     site where horizon owns queue management.
//
// Failures here return a typed-message error that callers (CLI handlers,
// the dashboard, the self-heal watcher) can surface verbatim. The
// watcher's per-unit cooldown then prevents thrashing on a permanent
// failure.
func workerStartPreflight(sitePath, workerName string, w config.FrameworkWorker) error {
	// A worker's command (from .lerd.yaml custom_workers or a framework def)
	// is interpolated into the unit's ExecStart line. Refuse newline/NUL so a
	// command from a cloned repo can't inject an extra systemd directive such
	// as ExecStartPost=/bin/sh -c '...' onto its own line.
	if config.ContainsUnitInjectionChars(w.Command) || config.ContainsUnitInjectionChars(w.ReloadCommand) {
		return fmt.Errorf("worker %q has an invalid command: must not contain newline or NUL", workerName)
	}
	if w.Check != nil && !config.MatchesRule(sitePath, *w.Check) {
		if msg := hostWorkerNotReadyMsg(workerName, sitePath, w); msg != "" {
			return errors.New(msg)
		}
		return fmt.Errorf(
			"worker %q skipped: required dependency not satisfied (rule: %s)",
			workerName, describeRule(*w.Check))
	}
	if w.ExcludeCheck != nil && config.MatchesRule(sitePath, *w.ExcludeCheck) {
		return fmt.Errorf(
			"worker %q skipped: superseded by another package on this site (rule: %s)",
			workerName, describeRule(*w.ExcludeCheck))
	}
	return nil
}

// hostWorkerNotReadyMsg returns an actionable message for a host worker (vite et
// al) that can't start because its dependency check fails. On a node project the
// cause is almost always JS deps not yet installed on the host, so it names the
// remedy; `lerd setup` is used rather than a bare `npm ci` so the advice holds
// for bun/yarn/pnpm projects too. Returns "" when the worker isn't a host node
// worker, so callers keep their own generic dependency message.
func hostWorkerNotReadyMsg(workerName, sitePath string, w config.FrameworkWorker) string {
	if !w.Host || !isNodeProject(sitePath) {
		return ""
	}
	return fmt.Sprintf("%s worker not started: JS dependencies are not installed. Run `lerd setup` to install them, then `lerd worker start %s`.", workerName, workerName)
}

// describeRule renders a FrameworkRule for an end-user error message.
// Keeps the message short — the user only needs to know which dependency
// triggered the gate, not the full rule shape.
func describeRule(r config.FrameworkRule) string {
	switch {
	case r.Composer != "":
		return "composer package " + r.Composer
	case r.File != "":
		return "file " + r.File
	default:
		return "(empty rule)"
	}
}
