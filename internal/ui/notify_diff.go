package ui

import (
	"sort"
	"strconv"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/push"
	"github.com/geodro/lerd/internal/workerheal"
)

// siteDomainForRoute resolves a registered site name to its primary domain
// so the notification URL can be opened by the dashboard's hash router,
// which keys the Sites tab by domain. Falls back to the input when no
// registered site matches (test fixtures, races between unlink and a
// late-arriving notification).
func siteDomainForRoute(name string) string {
	if s, err := config.FindSite(name); err == nil && s != nil {
		if d := s.PrimaryDomain(); d != "" {
			return d
		}
	}
	return name
}

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
		URL:      "#sites/" + siteDomainForRoute(site),
		Data:     map[string]string{"unit": w.Unit, "site": site},
		Urgency:  "high",
		TTL:      300,
	}
}

// notificationForWorkerFailures collapses a batch of new failures into a
// single push payload. A one-element batch falls through to the per-unit
// shape so existing tag-based dedupe on a single worker still works; two
// or more failures get a grouped title/body and a stable group tag so a
// later supersedes-an-earlier grouped push doesn't pile up.
func notificationForWorkerFailures(ws []workerheal.UnhealthyWorker) push.Notification {
	if len(ws) == 1 {
		return notificationForWorkerFailure(ws[0])
	}
	siteSet := make(map[string]struct{}, len(ws))
	entries := make([]string, 0, len(ws))
	for _, w := range ws {
		site := w.Site
		if site == "" {
			site = w.Unit
		}
		worker := w.Worker
		if worker == "" {
			worker = w.Unit
		}
		siteSet[site] = struct{}{}
		entries = append(entries, worker+"@"+site)
	}
	sort.Strings(entries)
	sites := make([]string, 0, len(siteSet))
	for s := range siteSet {
		sites = append(sites, s)
	}
	sort.Strings(sites)
	count := strconv.Itoa(len(ws))
	workers := strings.Join(entries, ", ")
	return push.Notification{
		Kind:     "worker_failed",
		TitleKey: "notify_worker_failed_group_title",
		Title:    count + " workers failed",
		BodyKey:  "notify_worker_failed_group_body",
		Body:     workers + ". Open lerd to heal.",
		Params: map[string]string{
			"count":   count,
			"workers": workers,
			"sites":   strings.Join(sites, ", "),
		},
		Tag:     "lerd-workers-group",
		URL:     "#sites",
		Data:    map[string]string{"count": count},
		Urgency: "high",
		TTL:     300,
	}
}
