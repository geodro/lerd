//go:build darwin

package podman

import "testing"

func TestPlatformPodmanArgs_Postgres(t *testing.T) {
	cases := []struct {
		name, image, want string
	}{
		{"postgres", "docker.io/postgis/postgis:16-3.5-alpine", "--platform=linux/amd64"},
		{"postgres", "docker.io/postgis/postgis:16-3.5", "--platform=linux/amd64"},
		{"postgres", "docker.io/postgis/postgis:17-3.5-alpine", "--platform=linux/amd64"},
		{"postgres", "docker.io/library/postgres:16-alpine", ""},
		{"postgres", "docker.io/imresamu/postgis:16-3.5-alpine", ""},
		{"mysql", "docker.io/postgis/postgis:16-3.5-alpine", ""},
	}
	for _, tc := range cases {
		if got := PlatformPodmanArgs(tc.name, tc.image); got != tc.want {
			t.Errorf("PlatformPodmanArgs(%q, %q) = %q, want %q", tc.name, tc.image, got, tc.want)
		}
	}
}
