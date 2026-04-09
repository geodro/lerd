package cli

import (
	"reflect"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func TestFilterConflictingDomains(t *testing.T) {
	allSites := []config.Site{
		{Name: "blog", Path: "/home/me/blog", Domains: []string{"blog.test"}},
		{Name: "shop", Path: "/home/me/shop", Domains: []string{"shop.test", "store.test"}},
	}

	cases := []struct {
		name        string
		desired     []string
		ownPath     string
		wantKept    []string
		wantRemoved []string
	}{
		{
			name:     "no conflicts — everything kept in order",
			desired:  []string{"newsite.test", "alias.test"},
			ownPath:  "/home/me/newsite",
			wantKept: []string{"newsite.test", "alias.test"},
		},
		{
			name:        "primary conflicts — alias becomes primary",
			desired:     []string{"shop.test", "alias.test"},
			ownPath:     "/home/me/newshop",
			wantKept:    []string{"alias.test"},
			wantRemoved: []string{"shop.test"},
		},
		{
			name:        "all desired conflict — empty kept list",
			desired:     []string{"shop.test", "store.test"},
			ownPath:     "/home/me/newshop",
			wantRemoved: []string{"shop.test", "store.test"},
		},
		{
			name:     "re-link of same path is not a conflict",
			desired:  []string{"shop.test", "store.test"},
			ownPath:  "/home/me/shop",
			wantKept: []string{"shop.test", "store.test"},
		},
		{
			name:     "mix of own + new",
			desired:  []string{"shop.test", "newdomain.test"},
			ownPath:  "/home/me/shop",
			wantKept: []string{"shop.test", "newdomain.test"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kept, removed := filterConflictingDomains(c.desired, c.ownPath, allSites)
			if !sliceEq(kept, c.wantKept) {
				t.Errorf("kept = %v, want %v", kept, c.wantKept)
			}
			if !sliceEq(removed, c.wantRemoved) {
				t.Errorf("removed = %v, want %v", removed, c.wantRemoved)
			}
		})
	}
}

// sliceEq compares two string slices treating nil and empty as equal.
func sliceEq(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}
