package cli

import "github.com/geodro/lerd/internal/workerheal"

// Re-exports of internal/workerheal so existing CLI / UI server callers
// can keep importing from internal/cli. The MCP package can't import cli
// (cli imports mcp for `lerd mcp:serve`), which is why the implementation
// itself lives in internal/workerheal.

type (
	UnhealthyWorker = workerheal.UnhealthyWorker
	HealEvent       = workerheal.Event
	HealResult      = workerheal.Result
	HealFailure     = workerheal.Failure
)

// DetectUnhealthyWorkers is the CLI/UI-facing alias for workerheal.Detect.
func DetectUnhealthyWorkers() ([]UnhealthyWorker, error) { return workerheal.Detect() }

// HealUnit is the CLI/UI-facing alias for workerheal.HealUnit.
func HealUnit(unit string) error { return workerheal.HealUnit(unit) }

// HealWorkers is the CLI/UI-facing alias for workerheal.HealAll.
func HealWorkers(emit func(HealEvent)) (HealResult, error) {
	return workerheal.HealAll(emit)
}

// HealSummary is the CLI-facing alias for workerheal.Summary.
func HealSummary(r HealResult) string { return workerheal.Summary(r) }
