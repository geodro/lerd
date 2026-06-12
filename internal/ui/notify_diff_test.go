package ui

import (
	"testing"

	"github.com/geodro/lerd/internal/config"
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

func TestNotificationForWorkerFailures_SinglePassthrough(t *testing.T) {
	ws := []workerheal.UnhealthyWorker{uw("lerd-queue-a.service", "a.test", "queue", "failed")}
	got := notificationForWorkerFailures(ws)
	if got.TitleKey != "notify_worker_failed_title" {
		t.Errorf("single failure should use per-unit title key, got %q", got.TitleKey)
	}
	if got.Tag != "lerd-worker-lerd-queue-a.service" {
		t.Errorf("single failure should use per-unit tag, got %q", got.Tag)
	}
}

func TestNotificationForWorkerFailures_GroupedShape(t *testing.T) {
	ws := []workerheal.UnhealthyWorker{
		uw("lerd-queue-b.service", "b.test", "queue", "failed"),
		uw("lerd-horizon-a.service", "a.test", "horizon", "start-limit-hit"),
		uw("lerd-scheduler-a.service", "a.test", "scheduler", "failed"),
	}
	got := notificationForWorkerFailures(ws)
	if got.Kind != "worker_failed" {
		t.Errorf("Kind = %q", got.Kind)
	}
	if got.TitleKey != "notify_worker_failed_group_title" {
		t.Errorf("TitleKey = %q", got.TitleKey)
	}
	if got.BodyKey != "notify_worker_failed_group_body" {
		t.Errorf("BodyKey = %q", got.BodyKey)
	}
	if got.Params["count"] != "3" {
		t.Errorf("Params.count = %q, want 3", got.Params["count"])
	}
	if got.Params["sites"] != "a.test, b.test" {
		t.Errorf("Params.sites = %q, want sorted a.test, b.test", got.Params["sites"])
	}
	wantWorkers := "horizon@a.test, queue@b.test, scheduler@a.test"
	if got.Params["workers"] != wantWorkers {
		t.Errorf("Params.workers = %q, want %q", got.Params["workers"], wantWorkers)
	}
	if got.Tag != "lerd-workers-group" {
		t.Errorf("Tag = %q, want stable group tag for supersede", got.Tag)
	}
	if got.URL != "#sites" {
		t.Errorf("URL = %q, want top-level sites view", got.URL)
	}
	if got.Title != "3 workers need healing" {
		t.Errorf("Title = %q", got.Title)
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

// The Sites tab keys by primary domain, but workerheal.UnhealthyWorker.Site
// is the registered site name. The notification URL must resolve name to
// domain so the click handler lands on the matching site, not an empty
// state when name != domain (e.g. rapids / harborlist.test).
func TestNotificationForWorkerFailure_URLResolvesNameToDomain(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if err := config.AddSite(config.Site{
		Name:    "rapids",
		Domains: []string{"harborlist.test"},
		Path:    t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}

	n := notificationForWorkerFailure(uw("lerd-queue-rapids.service", "rapids", "queue", "failed"))
	if n.URL != "#sites/harborlist.test" {
		t.Errorf("URL = %q, want #sites/harborlist.test", n.URL)
	}
}
