package ui

import (
	"encoding/json"
	"sync"

	"github.com/geodro/lerd/internal/push"
)

var (
	updatesPrevMu sync.Mutex
	updatesPrev   map[string]bool
)

// notifyOnServiceUpdates parses the Services snapshot JSON, computes the
// false→true update_available transitions vs the previous snapshot, and
// dispatches one notification per newly-flagged service.
func notifyOnServiceUpdates(servicesJSON []byte) {
	if len(servicesJSON) == 0 {
		return
	}
	var list []struct {
		Name            string `json:"name"`
		UpdateAvailable bool   `json:"update_available"`
		LatestVersion   string `json:"latest_version"`
	}
	if err := json.Unmarshal(servicesJSON, &list); err != nil {
		return
	}
	cur := make(map[string]bool, len(list))
	versions := make(map[string]string, len(list))
	for _, s := range list {
		cur[s.Name] = s.UpdateAvailable
		versions[s.Name] = s.LatestVersion
	}

	updatesPrevMu.Lock()
	prev := updatesPrev
	updatesPrev = cur
	updatesPrevMu.Unlock()

	if prev == nil {
		return
	}
	for _, name := range newUpdatesAvailable(prev, cur) {
		dispatchNotification(notificationForServiceUpdate(name, versions[name]))
	}
}

// newUpdatesAvailable returns service names whose update_available flag
// flipped false → true between prev and cur. Already-flagged services and
// flipping in the opposite direction are ignored.
func newUpdatesAvailable(prev, cur map[string]bool) []string {
	var out []string
	for name, available := range cur {
		if !available {
			continue
		}
		if prev[name] {
			continue
		}
		out = append(out, name)
	}
	return out
}

func notificationForServiceUpdate(service, version string) push.Notification {
	displayVer := version
	if displayVer == "" {
		displayVer = "a newer version"
	}
	return push.Notification{
		Kind:     "update_available",
		TitleKey: "notify_update_title",
		Title:    "Update available: " + service,
		BodyKey:  "notify_update_body",
		Body:     "Version " + displayVer + " is available.",
		Params:   map[string]string{"service": service, "version": displayVer},
		Tag:      "lerd-update-" + service,
		URL:      "#services/" + service,
		Data:     map[string]string{"service": service, "version": displayVer},
		Urgency:  "low",
		TTL:      3600,
	}
}
