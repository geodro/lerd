package serviceops

import (
	"testing"
)

func TestInstallPresetStreaming_UnknownPresetErrorsBeforeAnyPhase(t *testing.T) {
	var events []PhaseEvent
	svc, err := InstallPresetStreaming("definitely-not-a-real-preset-xyz", "", func(e PhaseEvent) {
		events = append(events, e)
	})
	if err == nil {
		t.Fatalf("expected error for unknown preset, got svc=%+v", svc)
	}
	if svc != nil {
		t.Fatalf("expected nil service on error, got %+v", svc)
	}
	// Resolution now happens before any side effect, so an unresolvable preset
	// errors before a single phase event (and before any config is written).
	if len(events) != 0 {
		t.Fatalf("expected no phase events before the resolution error, got %+v", events)
	}
}
