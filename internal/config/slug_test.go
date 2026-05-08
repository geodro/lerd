package config

import "testing"

func TestSiteSlug(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"my-app", "my_app"},
		{"My-App", "my_app"},
		{"feat-x", "feat_x"},
		{"feat.acme.test", "feat_acme_test"},
		{"already_underscored", "already_underscored"},
		{"UPPER-CASE", "upper_case"},
		{"no-change", "no_change"},
		{"mixed.dots-and-hyphens", "mixed_dots_and_hyphens"},
		{"simple", "simple"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := SiteSlug(tc.in); got != tc.want {
			t.Errorf("SiteSlug(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
