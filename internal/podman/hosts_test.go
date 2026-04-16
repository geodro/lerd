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

func TestHostCandidates(t *testing.T) {
	cases := []struct {
		name     string
		getentIP string
		lanIP    string
		want     []string
	}{
		{
			// Happy path: all three candidates present and distinct. Probe
			// order is preserved so host.containers.internal gets the first
			// shot — the address Xdebug docs already point users at, and the
			// canonical choice on macOS/gvproxy.
			name:     "all distinct",
			getentIP: "169.254.1.2",
			lanIP:    "192.168.1.10",
			want:     []string{"169.254.1.2", "192.168.1.10", "10.0.2.2"},
		},
		{
			// Regression: when getent and the LAN IP collide (some setups
			// alias them), the duplicate must be dropped so we don't probe
			// the same address twice and waste a 2 s timeout.
			name:     "getent and lan IP collide",
			getentIP: "192.168.1.10",
			lanIP:    "192.168.1.10",
			want:     []string{"192.168.1.10", "10.0.2.2"},
		},
		{
			// getent fails (no host.containers.internal entry, fresh install,
			// or container not yet up) — fall back to LAN IP plus slirp4netns
			// default. This is the path that rescues the rootless-netns case
			// behind issue #186 when netavark hasn't wired up an alias.
			name:     "getent missing",
			getentIP: "",
			lanIP:    "192.168.1.10",
			want:     []string{"192.168.1.10", "10.0.2.2"},
		},
		{
			// No LAN connection (offline laptop). Still try the addresses we
			// know about so an Xdebug-over-loopback setup keeps a chance.
			name:     "lan IP missing",
			getentIP: "169.254.1.2",
			lanIP:    "",
			want:     []string{"169.254.1.2", "10.0.2.2"},
		},
		{
			// Worst case: nothing detected. The slirp4netns default is the
			// only thing left to try. Better than returning an empty list,
			// because on slirp4netns systems 10.0.2.2 actually routes.
			name:     "nothing detected",
			getentIP: "",
			lanIP:    "",
			want:     []string{"10.0.2.2"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := hostCandidates(c.getentIP, c.lanIP)
			if len(got) != len(c.want) {
				t.Fatalf("hostCandidates(%q,%q) = %v, want %v", c.getentIP, c.lanIP, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("hostCandidates(%q,%q)[%d] = %q, want %q", c.getentIP, c.lanIP, i, got[i], c.want[i])
				}
			}
		})
	}
}
