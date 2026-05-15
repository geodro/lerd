package ui

import (
	"github.com/geodro/lerd/internal/push"
	"github.com/geodro/lerd/internal/workerheal"
)

// newWorkerFailures returns workers in cur whose Unit names weren't in prev.
// Identity by unit only — a state change on a known-failed unit doesn't
// fire a fresh notification.
func newWorkerFailures(prev, cur []workerheal.UnhealthyWorker) []workerheal.UnhealthyWorker {
	if len(cur) == 0 {
		return nil
	}
	prevSet := make(map[string]struct{}, len(prev))
	for _, p := range prev {
		prevSet[p.Unit] = struct{}{}
	}
	var out []workerheal.UnhealthyWorker
	for _, c := range cur {
		if _, seen := prevSet[c.Unit]; !seen {
			out = append(out, c)
		}
	}
	return out
}

func notificationForWorkerFailure(w workerheal.UnhealthyWorker) push.Notification {
	site := w.Site
	if site == "" {
		site = w.Unit
	}
	worker := w.Worker
	if worker == "" {
		worker = w.Unit
	}
	state := w.State
	if state == "" {
		state = "failed"
	}
	return push.Notification{
		Kind:     "worker_failed",
		TitleKey: "notify_worker_failed_title",
		Title:    "Worker failed on " + site,
		BodyKey:  "notify_worker_failed_body",
		Body:     worker + " is in " + state + ". Open lerd to heal.",
		Params:   map[string]string{"site": site, "worker": worker, "state": state},
		Tag:      "lerd-worker-" + w.Unit,
		URL:      "#sites/" + site,
		Data:     map[string]string{"unit": w.Unit, "site": site},
		Urgency:  "high",
		TTL:      300,
	}
}
