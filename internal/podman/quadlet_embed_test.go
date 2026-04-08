package podman

import (
	"strings"
	"testing"
)

func TestBindForLANUnexposedPrependsLoopback(t *testing.T) {
	in := strings.Join([]string{
		"[Container]",
		"PublishPort=80:80",
		"PublishPort=443:443",
	}, "\n")

	out := BindForLAN(in, false)
	if !strings.Contains(out, "PublishPort=127.0.0.1:80:80") {
		t.Errorf("expected 80 to be prefixed with 127.0.0.1, got:\n%s", out)
	}
	if !strings.Contains(out, "PublishPort=127.0.0.1:443:443") {
		t.Errorf("expected 443 to be prefixed with 127.0.0.1, got:\n%s", out)
	}
	if strings.Contains(out, "PublishPort=80:80\n") || strings.HasSuffix(out, "PublishPort=80:80") {
		t.Errorf("unprefixed PublishPort=80:80 should have been rewritten")
	}
}

func TestBindForLANExposedKeepsBareForm(t *testing.T) {
	in := strings.Join([]string{
		"[Container]",
		"PublishPort=80:80",
	}, "\n")

	out := BindForLAN(in, true)
	if !strings.Contains(out, "PublishPort=80:80") {
		t.Errorf("expected unprefixed form to remain in exposed mode, got:\n%s", out)
	}
	if strings.Contains(out, "127.0.0.1:80:80") {
		t.Errorf("did not expect 127.0.0.1 prefix in exposed mode, got:\n%s", out)
	}
}

func TestBindForLANRoundTrip(t *testing.T) {
	// Toggling unexposed → exposed → unexposed should converge.
	in := "PublishPort=80:80\nPublishPort=443:443\n"
	step1 := BindForLAN(in, false)
	step2 := BindForLAN(step1, true)
	step3 := BindForLAN(step2, false)
	if step1 != step3 {
		t.Errorf("round-trip failed:\nstep1=%q\nstep3=%q", step1, step3)
	}
	if !strings.Contains(step2, "PublishPort=80:80") || strings.Contains(step2, "127.0.0.1:80:80") {
		t.Errorf("step2 (exposed) should have bare PublishPort, got:\n%s", step2)
	}
}

func TestBindForLANPreservesLerdDNS(t *testing.T) {
	// lerd-dns is the only quadlet that ships with explicit 127.0.0.1
	// because LAN access to DNS is via the userspace forwarder. Both
	// modes must leave it alone.
	in := "PublishPort=127.0.0.1:5300:5300/udp\nPublishPort=127.0.0.1:5300:5300/tcp\n"
	for _, exposed := range []bool{true, false} {
		out := BindForLAN(in, exposed)
		if !strings.Contains(out, "PublishPort=127.0.0.1:5300:5300/udp") ||
			!strings.Contains(out, "PublishPort=127.0.0.1:5300:5300/tcp") {
			t.Errorf("lerd-dns publish lines should be untouched (exposed=%v), got:\n%s", exposed, out)
		}
	}
}

func TestBindForLANIgnoresOperatorOverrides(t *testing.T) {
	// If the user has an explicit non-loopback IP (e.g. 192.168.1.5)
	// pinned in a quadlet, BindForLAN must not stomp it in either mode.
	in := "PublishPort=192.168.1.5:80:80\n"
	for _, exposed := range []bool{true, false} {
		out := BindForLAN(in, exposed)
		if !strings.Contains(out, "PublishPort=192.168.1.5:80:80") {
			t.Errorf("operator override should be preserved (exposed=%v), got:\n%s", exposed, out)
		}
	}
}

func TestBindForLANHandlesProtocolSuffixes(t *testing.T) {
	in := "PublishPort=5300:5300/udp\n"
	out := BindForLAN(in, false)
	if !strings.Contains(out, "PublishPort=127.0.0.1:5300:5300/udp") {
		t.Errorf("protocol suffix should be preserved when prefixing, got:\n%s", out)
	}
}
