package cli

import "testing"

// The "Next" hint must cd into the path the user actually typed. Using only the
// base name breaks for nested targets (lerd new apps/myapp → cd myapp).
func TestNewNextStep(t *testing.T) {
	cases := []struct {
		target string
		want   string
	}{
		{"myapp", "cd myapp && lerd link && lerd setup"},
		{"apps/myapp", "cd apps/myapp && lerd link && lerd setup"},
		{"/abs/path/myapp", "cd /abs/path/myapp && lerd link && lerd setup"},
	}
	for _, tc := range cases {
		if got := newNextStep(tc.target); got != tc.want {
			t.Errorf("newNextStep(%q) = %q, want %q", tc.target, got, tc.want)
		}
	}
}
