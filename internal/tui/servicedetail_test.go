package tui

import (
	"strings"
	"testing"
)

func TestServiceDetail_RendersHeader(t *testing.T) {
	m := NewModel("test")
	svc := &ServiceRow{Name: "redis", Version: "7.2.4", State: stateRunning}
	lines := serviceDetailContentLines(m, svc, 120)
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "redis") {
		t.Errorf("expected service name in header:\n%s", joined)
	}
	if !strings.Contains(joined, "7.2.4") {
		t.Errorf("expected version:\n%s", joined)
	}
	if !strings.Contains(joined, "running") {
		t.Errorf("expected state line:\n%s", joined)
	}
}

func TestServiceDetail_ListsDependencies(t *testing.T) {
	m := NewModel("test")
	svc := &ServiceRow{Name: "phpmyadmin", State: stateRunning, DependsOn: []string{"mysql"}}
	lines := serviceDetailContentLines(m, svc, 120)
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "Depends on") {
		t.Errorf("expected 'Depends on' header:\n%s", joined)
	}
	if !strings.Contains(joined, "mysql") {
		t.Errorf("expected mysql dep:\n%s", joined)
	}
}

func TestServiceDetail_NoDependenciesHidesSection(t *testing.T) {
	m := NewModel("test")
	svc := &ServiceRow{Name: "redis", State: stateRunning}
	lines := serviceDetailContentLines(m, svc, 120)
	joined := stripANSI(strings.Join(lines, "\n"))
	if strings.Contains(joined, "Depends on") {
		t.Errorf("Depends on header should be hidden when empty:\n%s", joined)
	}
}

func TestServiceDetail_ShowsActionsHint(t *testing.T) {
	m := NewModel("test")
	svc := &ServiceRow{Name: "redis", State: stateRunning}
	lines := serviceDetailContentLines(m, svc, 120)
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "s start") || !strings.Contains(joined, "r restart") {
		t.Errorf("expected actions hint:\n%s", joined)
	}
}

func TestServiceDetail_WorkerRowRendersWorkerVariant(t *testing.T) {
	m := NewModel("test")
	svc := &ServiceRow{
		Name: "queue-acme", State: stateRunning,
		WorkerKind: "queue", WorkerSite: "acme", WorkerPath: "/home/u/Code/acme",
	}
	lines := serviceDetailContentLines(m, svc, 120)
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "kind:") || !strings.Contains(joined, "queue") {
		t.Errorf("expected worker kind row:\n%s", joined)
	}
	if !strings.Contains(joined, "site:") || !strings.Contains(joined, "acme") {
		t.Errorf("expected worker site row:\n%s", joined)
	}
	if !strings.Contains(joined, "lerd-queue-acme") {
		t.Errorf("expected unit name:\n%s", joined)
	}
	// Workers have no preset/env block, so the regular Sites-using header
	// must not appear.
	if strings.Contains(joined, "Sites using") {
		t.Errorf("worker variant should not show Sites-using:\n%s", joined)
	}
}

func TestServiceDetail_NilServiceShowsPlaceholder(t *testing.T) {
	m := NewModel("test")
	lines := serviceDetailContentLines(m, nil, 120)
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "no service selected") {
		t.Errorf("expected placeholder for nil service:\n%s", joined)
	}
}

func TestPresetSuggestionFor_KnownService(t *testing.T) {
	svc := &ServiceRow{Name: "mysql"}
	// May or may not return a string depending on whether phpmyadmin is
	// installed on the dev box; just assert the function doesn't panic
	// and that an unknown name returns "".
	_ = presetSuggestionFor(svc)
	if got := presetSuggestionFor(&ServiceRow{Name: "redis"}); got != "" {
		t.Errorf("redis has no suggestion mapping, got %q", got)
	}
	if got := presetSuggestionFor(nil); got != "" {
		t.Errorf("nil svc must return empty, got %q", got)
	}
}
