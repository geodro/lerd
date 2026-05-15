package ui

import (
	"testing"

	"github.com/geodro/lerd/internal/workerheal"
)

func uw(unit, site, worker, state string) workerheal.UnhealthyWorker {
	return workerheal.UnhealthyWorker{Unit: unit, Site: site, Worker: worker, State: state}
}

func TestNewWorkerFailures_EmptyPrev_ReturnsAll(t *testing.T) {
	cur := []workerheal.UnhealthyWorker{
		uw("lerd-queue-a.service", "a.test", "queue", "failed"),
	}
	got := newWorkerFailures(nil, cur)
	if len(got) != 1 || got[0].Unit != "lerd-queue-a.service" {
		t.Errorf("got %+v", got)
	}
}

func TestNewWorkerFailures_KnownUnitsAreFiltered(t *testing.T) {
	prev := []workerheal.UnhealthyWorker{
		uw("lerd-queue-a.service", "a.test", "queue", "failed"),
	}
	cur := []workerheal.UnhealthyWorker{
		uw("lerd-queue-a.service", "a.test", "queue", "failed"),
		uw("lerd-horizon-b.service", "b.test", "horizon", "failed"),
	}
	got := newWorkerFailures(prev, cur)
	if len(got) != 1 || got[0].Unit != "lerd-horizon-b.service" {
		t.Errorf("expected only the newly-failed unit, got %+v", got)
	}
}

func TestNewWorkerFailures_NoDeltas(t *testing.T) {
	cur := []workerheal.UnhealthyWorker{
		uw("lerd-queue-a.service", "a.test", "queue", "failed"),
	}
	got := newWorkerFailures(cur, cur)
	if len(got) != 0 {
		t.Errorf("expected empty delta, got %+v", got)
	}
}

func TestNewWorkerFailures_StateChangeIsNotNewFailure(t *testing.T) {
	// Same unit transitioning from failed → start-limit-hit shouldn't fire a
	// "new failure" notification — the worker was already broken; only the
	// reason changed.
	prev := []workerheal.UnhealthyWorker{uw("lerd-queue-a.service", "a", "queue", "failed")}
	cur := []workerheal.UnhealthyWorker{uw("lerd-queue-a.service", "a", "queue", "start-limit-hit")}
	got := newWorkerFailures(prev, cur)
	if len(got) != 0 {
		t.Errorf("state-only transition should not be a new failure, got %+v", got)
	}
}

func TestNotificationForWorkerFailure_Shape(t *testing.T) {
	n := notificationForWorkerFailure(uw("lerd-queue-default-a.service", "a.test", "queue-default", "failed"))
	if n.Kind != "worker_failed" {
		t.Errorf("Kind = %q", n.Kind)
	}
	if n.TitleKey != "notify_worker_failed_title" {
		t.Errorf("TitleKey = %q", n.TitleKey)
	}
	if n.BodyKey != "notify_worker_failed_body" {
		t.Errorf("BodyKey = %q", n.BodyKey)
	}
	if n.Params["site"] != "a.test" {
		t.Errorf("Params.site = %q", n.Params["site"])
	}
	if n.Params["worker"] != "queue-default" {
		t.Errorf("Params.worker = %q", n.Params["worker"])
	}
	if n.Params["state"] != "failed" {
		t.Errorf("Params.state = %q", n.Params["state"])
	}
	if n.Tag != "lerd-worker-lerd-queue-default-a.service" {
		t.Errorf("Tag = %q", n.Tag)
	}
	if n.URL == "" {
		t.Errorf("URL is empty; need a deep-link target")
	}
}
