package cli

import "github.com/geodro/lerd/internal/config"

// workerArtifactKind is the on-disk artifact a worker lives as in a given
// mode. container mode writes a .container quadlet; exec mode writes a
// .service file plus (on macOS) a launchd plist, guard script, and pid
// file. The migration planner uses these to decide what to clean up.
type workerArtifactKind int

const (
	artifactContainer workerArtifactKind = iota
	artifactService
)

// workerMigrationStep is one per-worker migration action: stop the unit in
// its old shape, clean up the old artifacts, and (after config save) let
// writeWorkerUnitFile produce the new shape which the executor starts.
type workerMigrationStep struct {
	Unit    string
	From    string
	To      string
	OldKind workerArtifactKind
	NewKind workerArtifactKind
}

// planWorkerMigration maps an intended mode change into a per-worker
// action list. Pure function; no side effects, no platform checks. The
// executor is responsible for applying the plan (and for deciding whether
// to run it — on Linux it's always a no-op).
func planWorkerMigration(fromMode, toMode string, activeUnits []string) []workerMigrationStep {
	if fromMode == toMode {
		return nil
	}
	oldKind := kindForMode(fromMode)
	newKind := kindForMode(toMode)
	steps := make([]workerMigrationStep, 0, len(activeUnits))
	for _, unit := range activeUnits {
		steps = append(steps, workerMigrationStep{
			Unit:    unit,
			From:    fromMode,
			To:      toMode,
			OldKind: oldKind,
			NewKind: newKind,
		})
	}
	return steps
}

func kindForMode(mode string) workerArtifactKind {
	if mode == config.WorkerExecModeContainer {
		return artifactContainer
	}
	return artifactService
}
