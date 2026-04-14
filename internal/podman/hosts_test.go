package podman

import (
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func TestFirstField(t *testing.T) {
	cases := map[string]string{
		"169.254.1.2     host.containers.internal host.docker.internal\n": "169.254.1.2",
		"  10.0.2.2 host.containers.internal\n":                           "10.0.2.2",
		"":                                                                "",
		"\n\n":                                                            "",
		"only-one-tok":                                                    "only-one-tok",
	}
	for in, want := range cases {
		if got := firstField(in); got != want {
			t.Errorf("firstField(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderContainerHosts_EmptyRegistry(t *testing.T) {
	got := renderContainerHosts(&config.SiteRegistry{}, "169.254.1.2", "10.89.0.2")
	want := "127.0.0.1 localhost\n" +
		"::1 localhost\n" +
		"169.254.1.2 host.containers.internal host.docker.internal\n"
	if got != want {
		t.Errorf("renderContainerHosts empty = %q, want %q", got, want)
	}
}

func TestRenderContainerHosts_SitesPointAtNginx(t *testing.T) {
	reg := &config.SiteRegistry{Sites: []config.Site{
		{Name: "foo", Domains: []string{"foo.test"}},
		{Name: "bar", Domains: []string{"bar.test", "admin-bar.test"}},
	}}

	got := renderContainerHosts(reg, "169.254.1.2", "10.89.0.2")

	// host.containers.internal must use the host gateway, never nginx.
	if !strings.Contains(got, "169.254.1.2 host.containers.internal host.docker.internal\n") {
		t.Errorf("missing host.containers.internal line:\n%s", got)
	}
	// .test domains must use the nginx container IP, never the host gateway.
	for _, d := range []string{"foo.test", "bar.test", "admin-bar.test"} {
		wantLine := "10.89.0.2 " + d + "\n"
		if !strings.Contains(got, wantLine) {
			t.Errorf("missing %q in output:\n%s", wantLine, got)
		}
		if strings.Contains(got, "169.254.1.2 "+d) {
			t.Errorf("site %q incorrectly points at host gateway:\n%s", d, got)
		}
	}
}

func TestRenderContainerHosts_DistinctIPs(t *testing.T) {
	// Regression: host-gateway (Xdebug) and nginx IP (.test) must stay separate.
	reg := &config.SiteRegistry{Sites: []config.Site{
		{Name: "x", Domains: []string{"x.test"}},
	}}
	got := renderContainerHosts(reg, "169.254.1.2", "10.89.0.2")

	if strings.Contains(got, "169.254.1.2 x.test") {
		t.Errorf("x.test must not resolve to host gateway:\n%s", got)
	}
	if strings.Contains(got, "10.89.0.2 host.containers.internal") {
		t.Errorf("host.containers.internal must not resolve to nginx IP:\n%s", got)
	}
}

func TestRenderContainerHosts_PreservesLoopback(t *testing.T) {
	got := renderContainerHosts(&config.SiteRegistry{}, "1.2.3.4", "5.6.7.8")
	if !strings.HasPrefix(got, "127.0.0.1 localhost\n::1 localhost\n") {
		t.Errorf("loopback entries missing or out of order:\n%s", got)
	}
}
