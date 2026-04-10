package podman

import (
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func TestStripInstallSectionNoOpWhenEnabled(t *testing.T) {
	in := "[Container]\nImage=foo\n\n[Install]\nWantedBy=default.target\n"
	if got := StripInstallSection(in, false); got != in {
		t.Errorf("expected unchanged content when autostartDisabled=false\ngot:\n%s", got)
	}
}

func TestStripInstallSectionRemovesInstallBlock(t *testing.T) {
	in := strings.Join([]string{
		"[Container]",
		"Image=foo",
		"PublishPort=80:80",
		"",
		"[Install]",
		"WantedBy=default.target",
		"",
	}, "\n")

	out := StripInstallSection(in, true)
	if strings.Contains(out, "[Install]") {
		t.Errorf("expected [Install] section to be removed:\n%s", out)
	}
	if strings.Contains(out, "WantedBy=") {
		t.Errorf("expected WantedBy line to be removed:\n%s", out)
	}
	if !strings.Contains(out, "Image=foo") {
		t.Errorf("expected [Container] section to be preserved:\n%s", out)
	}
	if !strings.Contains(out, "PublishPort=80:80") {
		t.Errorf("expected PublishPort to be preserved:\n%s", out)
	}
}

func TestStripInstallSectionPreservesIntermediateSections(t *testing.T) {
	// Some quadlets have a [Service] section between [Container] and
	// [Install]. The strip must only drop [Install], not anything else.
	in := strings.Join([]string{
		"[Container]",
		"Image=foo",
		"",
		"[Service]",
		"Restart=on-failure",
		"",
		"[Install]",
		"WantedBy=default.target",
		"",
	}, "\n")

	out := StripInstallSection(in, true)
	if !strings.Contains(out, "[Service]") {
		t.Errorf("expected [Service] section to be preserved:\n%s", out)
	}
	if !strings.Contains(out, "Restart=on-failure") {
		t.Errorf("expected Restart line to be preserved:\n%s", out)
	}
	if strings.Contains(out, "[Install]") {
		t.Errorf("expected [Install] to be removed:\n%s", out)
	}
}

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

func TestInjectExtraVolumesAfterHomeMount(t *testing.T) {
	in := strings.Join([]string{
		"[Container]",
		"Image=foo",
		"Volume=%h:%h:rw",
		"PodmanArgs=--security-opt=label=disable",
	}, "\n")

	out := InjectExtraVolumes(in, []string{"/var/www", "/opt/projects"})
	if !strings.Contains(out, "Volume=/var/www:/var/www:rw") {
		t.Errorf("expected /var/www volume, got:\n%s", out)
	}
	if !strings.Contains(out, "Volume=/opt/projects:/opt/projects:rw") {
		t.Errorf("expected /opt/projects volume, got:\n%s", out)
	}
	// Extra volumes should appear after %h:%h:rw.
	homeIdx := strings.Index(out, "Volume=%h:%h:rw")
	varIdx := strings.Index(out, "Volume=/var/www:/var/www:rw")
	podmanIdx := strings.Index(out, "PodmanArgs=")
	if varIdx < homeIdx {
		t.Errorf("extra volume should appear after home mount, got:\n%s", out)
	}
	if varIdx > podmanIdx {
		t.Errorf("extra volume should appear before PodmanArgs, got:\n%s", out)
	}
}

func TestInjectExtraVolumesNoPaths(t *testing.T) {
	in := "Volume=%h:%h:rw\n"
	out := InjectExtraVolumes(in, nil)
	if out != in {
		t.Errorf("expected unchanged content with no paths, got:\n%s", out)
	}
}

func TestInjectExtraVolumesNoDuplicates(t *testing.T) {
	in := "Volume=%h:%h:rw\nVolume=/var/www:/var/www:rw\n"
	out := InjectExtraVolumes(in, []string{"/var/www"})
	if strings.Count(out, "/var/www:/var/www:rw") != 1 {
		t.Errorf("should not duplicate existing volume, got:\n%s", out)
	}
}

func TestGenerateCustomQuadlet_ShareHosts(t *testing.T) {
	svc := &config.CustomService{
		Name:       "selenium",
		Image:      "docker.io/selenium/standalone-chromium:latest",
		ShareHosts: true,
		Ports:      []string{"4444:4444"},
	}
	out := GenerateCustomQuadlet(svc)
	hostsVolume := "Volume=" + config.BrowserHostsFile() + ":/etc/hosts:ro,z"
	if !strings.Contains(out, hostsVolume) {
		t.Errorf("expected hosts volume mount when ShareHosts=true, got:\n%s", out)
	}
}

func TestGenerateCustomQuadlet_NoShareHosts(t *testing.T) {
	svc := &config.CustomService{
		Name:  "mongo",
		Image: "docker.io/library/mongo:7",
	}
	out := GenerateCustomQuadlet(svc)
	if strings.Contains(out, "/etc/hosts") {
		t.Errorf("should not mount hosts file when ShareHosts=false, got:\n%s", out)
	}
}

func TestSortPaths(t *testing.T) {
	paths := []string{"/var/www/app", "/opt", "/var/www"}
	sortPaths(paths)
	if paths[0] != "/opt" || paths[1] != "/var/www" || paths[2] != "/var/www/app" {
		t.Errorf("expected sorted by length then lex, got: %v", paths)
	}
}
