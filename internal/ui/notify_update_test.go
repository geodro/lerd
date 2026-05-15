package ui

import "testing"

func TestNewUpdatesAvailable_DetectsTransitions(t *testing.T) {
	prev := map[string]bool{"mysql": false, "redis": false}
	cur := map[string]string{"mysql": "9.0", "redis": "", "postgres": "16.2"}
	curAvail := map[string]bool{"mysql": true, "redis": false, "postgres": true}

	got := newUpdatesAvailable(prev, curAvail)
	want := map[string]bool{"mysql": true, "postgres": true}
	if len(got) != len(want) {
		t.Fatalf("got %d transitions, want %d: %v", len(got), len(want), got)
	}
	for k := range want {
		if !contains(got, k) {
			t.Errorf("missing %q in %v", k, got)
		}
	}
	_ = cur
}

func TestNewUpdatesAvailable_NoChange(t *testing.T) {
	prev := map[string]bool{"mysql": true}
	cur := map[string]bool{"mysql": true}
	if got := newUpdatesAvailable(prev, cur); len(got) != 0 {
		t.Errorf("expected no transitions for already-flagged service, got %v", got)
	}
}

func TestNewUpdatesAvailable_BecameUnavailable(t *testing.T) {
	prev := map[string]bool{"mysql": true}
	cur := map[string]bool{"mysql": false}
	if got := newUpdatesAvailable(prev, cur); len(got) != 0 {
		t.Errorf("update cleared shouldn't fire a 'new update' notification, got %v", got)
	}
}

func TestNotificationForServiceUpdate_Shape(t *testing.T) {
	n := notificationForServiceUpdate("mysql", "9.0")
	if n.Kind != "update_available" {
		t.Errorf("Kind = %q", n.Kind)
	}
	if n.Params["service"] != "mysql" {
		t.Errorf("Params.service = %q", n.Params["service"])
	}
	if n.Params["version"] != "9.0" {
		t.Errorf("Params.version = %q", n.Params["version"])
	}
	if n.URL != "#services/mysql" {
		t.Errorf("URL = %q", n.URL)
	}
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
