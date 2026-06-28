package ui

import (
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// effectiveHostPort takes the caller's already-loaded services map (so a whole
// services-list rebuild loads the global config once, not once per service) and
// must keep the published-port > configured-port > default-mapping precedence.
func TestEffectiveHostPort_precedenceFromSharedMap(t *testing.T) {
	services := map[string]config.ServiceConfig{
		"mysql":    {PublishedPort: 3307, Port: 3306},
		"postgres": {Port: 5418},
	}

	if got := effectiveHostPort(services, "mysql", []string{"3306:3306"}); got != 3307 {
		t.Errorf("published port override must win: got %d, want 3307", got)
	}
	if got := effectiveHostPort(services, "postgres", []string{"5432:5432"}); got != 5418 {
		t.Errorf("configured port: got %d, want 5418", got)
	}
	// Absent from the map -> the default mapping's primary host port.
	if got := effectiveHostPort(services, "redis", []string{"6379:6379"}); got != 6379 {
		t.Errorf("default mapping fallback: got %d, want 6379", got)
	}
	// A nil map (config load failed) still falls back cleanly.
	if got := effectiveHostPort(nil, "redis", []string{"6379:6379"}); got != 6379 {
		t.Errorf("nil services map must fall back to the default mapping: got %d, want 6379", got)
	}
}
