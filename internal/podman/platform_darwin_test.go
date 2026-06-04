//go:build darwin

package podman

import (
	"strings"
	"testing"
)

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
		{"mysql", "docker.io/library/mysql:5.7", "--platform=linux/amd64"},
		{"mysql", "docker.io/library/mysql:8.4", ""},
		{"mysql", "docker.io/library/mysql:9.7", ""},
	}
	for _, tc := range cases {
		if got := PlatformPodmanArgs(tc.name, tc.image); got != tc.want {
			t.Errorf("PlatformPodmanArgs(%q, %q) = %q, want %q", tc.name, tc.image, got, tc.want)
		}
	}
}

func TestPlatformPullArgs_Postgis(t *testing.T) {
	cases := []struct {
		image string
		want  string
	}{
		{"docker.io/postgis/postgis:16-3.5-alpine", "--platform=linux/amd64"},
		{"docker.io/postgis/postgis:17-3.6-alpine", "--platform=linux/amd64"},
		{"docker.io/postgis/postgis:18-3.6-alpine", "--platform=linux/amd64"},
		{"docker.io/library/mysql:5.7", "--platform=linux/amd64"},
		{"docker.io/library/mysql:8.4", ""},
		{"docker.io/library/mysql:9.7", ""},
		{"docker.io/library/postgres:16-alpine", ""},
		{"docker.io/imresamu/postgis:16-3.5-alpine", ""},
	}
	for _, tc := range cases {
		got := strings.Join(PlatformPullArgs(tc.image), " ")
		if got != tc.want {
			t.Errorf("PlatformPullArgs(%q) = %q, want %q", tc.image, got, tc.want)
		}
	}
}
