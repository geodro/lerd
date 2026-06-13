package systemd

import (
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// TestUnitBelongsToLongerSite guards the group-secondary leak: a unit named for
// admin-astrolov must not be claimed by astrolov when scanning astrolov's
// orphans, or idle-suspend (and pause) would stop the secondary's workers.
func TestUnitBelongsToLongerSite(t *testing.T) {
	sites := []config.Site{{Name: "astrolov"}, {Name: "admin-astrolov"}}

	cases := []struct {
		name     string
		unitFile string
		site     string
		want     bool
	}{
		{"secondary unit not claimed by parent", "lerd-queue-admin-astrolov.service", "astrolov", true},
		{"parent's own unit is not a longer site", "lerd-queue-astrolov.service", "astrolov", false},
		{"secondary scanning its own unit", "lerd-queue-admin-astrolov.service", "admin-astrolov", false},
		{"unrelated unit", "lerd-queue-other.service", "astrolov", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := unitBelongsToLongerSite(tc.unitFile, tc.site, sites); got != tc.want {
				t.Errorf("unitBelongsToLongerSite(%q, %q) = %v, want %v", tc.unitFile, tc.site, got, tc.want)
			}
		})
	}
}
