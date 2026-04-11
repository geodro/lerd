package siteops

import "testing"

func TestIsParkedSite(t *testing.T) {
	cases := []struct {
		sitePath   string
		parkedDirs []string
		want       bool
	}{
		{"/home/user/Projects/myapp", []string{"/home/user/Projects"}, true},
		{"/home/user/Projects/myapp", []string{"/home/user/Other"}, false},
		{"/home/user/Projects/myapp", []string{"/home/user/Projects", "/home/user/Lerd"}, true},
		{"/home/user/Lerd/myapp", []string{"/home/user/Projects", "/home/user/Lerd"}, true},
		{"/home/user/Projects/myapp", []string{}, false},
		{"/home/user/Projects/sub/deep", []string{"/home/user/Projects"}, false}, // not direct child
	}

	for _, c := range cases {
		got := IsParkedSite(c.sitePath, c.parkedDirs)
		if got != c.want {
			t.Errorf("IsParkedSite(%q, %v) = %v, want %v", c.sitePath, c.parkedDirs, got, c.want)
		}
	}
}
