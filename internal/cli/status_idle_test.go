package cli

import (
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func TestSiteWorkerIdleSuspended(t *testing.T) {
	s := config.Site{IdleSuspendedWorkers: []string{"queue", "horizon"}}
	if !siteWorkerIdleSuspended(s, "queue") {
		t.Error("queue should report idle-suspended")
	}
	if !siteWorkerIdleSuspended(s, "horizon") {
		t.Error("horizon should report idle-suspended")
	}
	if siteWorkerIdleSuspended(s, "schedule") {
		t.Error("schedule is running, not suspended")
	}
	if siteWorkerIdleSuspended(config.Site{}, "queue") {
		t.Error("a site with no suspended workers must report false")
	}
}
