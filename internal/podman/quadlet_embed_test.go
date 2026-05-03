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

func TestBindForLANTogglesIPv6InLockstep(t *testing.T) {
	// Both stacks must flip together. Leaving [::1] behind on expose
	// dedups against the bare v4 line and loses LAN reach; leaving [::]
	// behind on unexpose loses loopback-only safety.
	loopback := "PublishPort=127.0.0.1:80:80\nPublishPort=[::1]:80:80\n"
	exposed := BindForLAN(loopback, true)
	if strings.Contains(exposed, "127.0.0.1:") || strings.Contains(exposed, "[::1]:") {
		t.Errorf("loopback prefixes must be stripped on expose, got:\n%s", exposed)
	}
	if !strings.Contains(exposed, "PublishPort=80:80") || !strings.Contains(exposed, "PublishPort=[::]:80:80") {
		t.Errorf("expected bare + [::] after expose, got:\n%s", exposed)
	}
	back := BindForLAN(exposed, false)
	if !strings.Contains(back, "PublishPort=127.0.0.1:80:80") || !strings.Contains(back, "PublishPort=[::1]:80:80") {
		t.Errorf("expected 127.0.0.1 + [::1] after unexpose, got:\n%s", back)
	}
	if strings.Contains(back, "PublishPort=[::]:80:80") {
		t.Errorf("[::] should be converted back to [::1] on unexpose, got:\n%s", back)
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

func TestInjectPodmanArgs_AppendsAfterImage(t *testing.T) {
	in := "[Container]\nImage=docker.io/postgis/postgis:16-3.5-alpine\nContainerName=lerd-postgres\n"
	out := InjectPodmanArgs(in, "--platform=linux/amd64")
	if !strings.Contains(out, "Image=docker.io/postgis/postgis:16-3.5-alpine\nPodmanArgs=--platform=linux/amd64\n") {
		t.Errorf("PodmanArgs should land directly under Image=, got:\n%s", out)
	}
}

func TestInjectPodmanArgs_Idempotent(t *testing.T) {
	in := "[Container]\nImage=docker.io/postgis/postgis:16-3.5-alpine\nPodmanArgs=--platform=linux/amd64\n"
	if out := InjectPodmanArgs(in, "--platform=linux/amd64"); out != in {
		t.Errorf("re-injecting an arg already present must return content unchanged, got:\n%s", out)
	}
	in2 := "[Container]\nImage=x\nPodmanArgs=--security-opt=label=disable --platform=linux/amd64\n"
	if out := InjectPodmanArgs(in2, "--platform=linux/amd64"); out != in2 {
		t.Errorf("must detect arg even when concatenated with other args, got:\n%s", out)
	}
}

func TestInjectPodmanArgs_NoImageLineNoChange(t *testing.T) {
	in := "[Container]\nContainerName=lerd-x\n"
	if out := InjectPodmanArgs(in, "--platform=linux/amd64"); out != in {
		t.Errorf("content with no Image= line must be returned unchanged, got:\n%s", out)
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

func TestGenerateCustomQuadlet_UsernsAndChownData(t *testing.T) {
	svc := &config.CustomService{
		Name:      "elasticsearch",
		Image:     "docker.elastic.co/elasticsearch/elasticsearch:8.13.4",
		DataDir:   "/usr/share/elasticsearch/data",
		Userns:    "keep-id:uid=1000,gid=0",
		ChownData: true,
	}
	out := GenerateCustomQuadlet(svc)
	if !strings.Contains(out, "UserNS=keep-id:uid=1000,gid=0") {
		t.Errorf("expected UserNS line when Userns set, got:\n%s", out)
	}
	if !strings.Contains(out, ":/usr/share/elasticsearch/data:z,U") {
		t.Errorf("expected :z,U flags on data_dir mount when ChownData=true, got:\n%s", out)
	}
}

func TestGenerateCustomQuadlet_DataDirDefaultsToZOnly(t *testing.T) {
	svc := &config.CustomService{
		Name:    "postgres-test",
		Image:   "docker.io/library/postgres:16",
		DataDir: "/var/lib/postgresql/data",
	}
	out := GenerateCustomQuadlet(svc)
	if !strings.Contains(out, ":/var/lib/postgresql/data:z\n") {
		t.Errorf("data_dir mount must default to :z (no ,U) when ChownData unset, got:\n%s", out)
	}
	if strings.Contains(out, "UserNS=") {
		t.Errorf("must not emit UserNS line when Userns unset, got:\n%s", out)
	}
}

func TestGenerateCustomQuadlet_EnvWithJSONPreservesQuotes(t *testing.T) {
	svc := &config.CustomService{
		Name:  "elasticvue",
		Image: "docker.io/cars10/elasticvue:latest",
		Environment: map[string]string{
			"ELASTICVUE_CLUSTERS": `[{"name":"Lerd","uri":"http://localhost:9200"}]`,
			"WILDCARD":            `"*"`,
		},
	}
	out := GenerateCustomQuadlet(svc)
	wantClusters := `Environment="ELASTICVUE_CLUSTERS=[{\"name\":\"Lerd\",\"uri\":\"http://localhost:9200\"}]"`
	if !strings.Contains(out, wantClusters) {
		t.Errorf("env value with JSON quotes must be wrapped + escaped (otherwise systemd strips inner quotes), got:\n%s", out)
	}
	if !strings.Contains(out, `Environment="WILDCARD=\"*\""`) {
		t.Errorf("env value with quoted wildcard must round-trip, got:\n%s", out)
	}
}

func TestGenerateCustomQuadlet_StopTimeout(t *testing.T) {
	// Images like selenium/standalone-chromium hang for 30s+ on graceful
	// shutdown. StopTimeout=5 bounds podman's SIGTERM-wait so systemctl stop
	// returns promptly instead of blocking the UI.
	svc := &config.CustomService{
		Name:  "selenium",
		Image: "docker.io/selenium/standalone-chromium:latest",
	}
	out := GenerateCustomQuadlet(svc)
	if !strings.Contains(out, "StopTimeout=5") {
		t.Errorf("expected StopTimeout=5 in [Container] section, got:\n%s", out)
	}
}

func TestNginxQuadletMountsCustomD(t *testing.T) {
	// Per-site override files under ~/.local/share/lerd/nginx/custom.d must
	// be bind-mounted into the container; without this the include directive
	// in the vhost templates resolves to nothing and user edits are silent.
	content, err := GetQuadletTemplate("lerd-nginx.container")
	if err != nil {
		t.Fatalf("GetQuadletTemplate: %v", err)
	}
	if !strings.Contains(content, "%h/.local/share/lerd/nginx/custom.d:/etc/nginx/custom.d") {
		t.Errorf("lerd-nginx.container missing custom.d volume mount:\n%s", content)
	}
}

func TestSortPaths(t *testing.T) {
	paths := []string{"/var/www/app", "/opt", "/var/www"}
	sortPaths(paths)
	if paths[0] != "/opt" || paths[1] != "/var/www" || paths[2] != "/var/www/app" {
		t.Errorf("expected sorted by length then lex, got: %v", paths)
	}
}

// --- PairIPv6Binds ---

func TestPairIPv6Binds_rewritesBareToDualStack(t *testing.T) {
	// Bare binds are rewritten to [::] (single dual-stack socket), not
	// paired. Keeping both 80:80 and [::]:80:80 collides on Linux default
	// bindv6only=0 and crashes nginx with `bind: address already in use`.
	in := "[Container]\nNetwork=lerd\nPublishPort=80:80\nPublishPort=443:443\n"
	out := PairIPv6Binds(in)
	if !strings.Contains(out, "PublishPort=[::]:80:80") {
		t.Errorf("expected [::]:80:80, got:\n%s", out)
	}
	if !strings.Contains(out, "PublishPort=[::]:443:443") {
		t.Errorf("expected [::]:443:443, got:\n%s", out)
	}
	if strings.Contains(out, "PublishPort=80:80\n") || strings.HasSuffix(out, "PublishPort=80:80") {
		t.Errorf("bare 80:80 must be replaced, not paired (conflicts with [::]:80:80 on bindv6only=0):\n%s", out)
	}
	if strings.Contains(out, "PublishPort=443:443\n") || strings.HasSuffix(out, "PublishPort=443:443") {
		t.Errorf("bare 443:443 must be replaced, not paired:\n%s", out)
	}
}

func TestPairIPv6Binds_pairsLoopbackWithLinkLocal(t *testing.T) {
	in := "Network=lerd\nPublishPort=127.0.0.1:5300:5300/udp\nPublishPort=127.0.0.1:5300:5300/tcp\n"
	out := PairIPv6Binds(in)
	if !strings.Contains(out, "PublishPort=[::1]:5300:5300/udp") {
		t.Errorf("expected v6 pair [::1]:5300:5300/udp, got:\n%s", out)
	}
	if !strings.Contains(out, "PublishPort=[::1]:5300:5300/tcp") {
		t.Errorf("expected v6 pair [::1]:5300:5300/tcp, got:\n%s", out)
	}
}

func TestPairIPv6Binds_idempotent(t *testing.T) {
	in := "Network=lerd\nPublishPort=80:80\n"
	once := PairIPv6Binds(in)
	twice := PairIPv6Binds(once)
	if once != twice {
		t.Errorf("PairIPv6Binds is not idempotent:\nonce:\n%s\ntwice:\n%s", once, twice)
	}
	if strings.Count(twice, "PublishPort=[::]:80:80") != 1 {
		t.Errorf("expected exactly one v6 pair, got:\n%s", twice)
	}
}

func TestPairIPv6Binds_preservesOperatorOverrides(t *testing.T) {
	in := "Network=lerd\nPublishPort=192.168.1.10:80:80\nPublishPort=[fe80::1%eth0]:80:80\n"
	out := PairIPv6Binds(in)
	if out != in {
		t.Errorf("operator overrides should be preserved verbatim:\nin:\n%s\nout:\n%s", in, out)
	}
}

func TestPairIPv6Binds_handles0000(t *testing.T) {
	in := "Network=lerd\nPublishPort=0.0.0.0:80:80\n"
	out := PairIPv6Binds(in)
	if !strings.Contains(out, "PublishPort=[::]:80:80") {
		t.Errorf("expected [::]:80:80, got:\n%s", out)
	}
	if strings.Contains(out, "PublishPort=0.0.0.0:80:80") {
		t.Errorf("0.0.0.0 must be rewritten, not paired:\n%s", out)
	}
}

func TestPairIPv6Binds_skipsWhenNoNetworkDirective(t *testing.T) {
	// pasta (the rootless default when no Network= is set) cannot bind v6
	// ports. Adding [::1] pairs would crash the container at startup.
	in := "[Container]\nPublishPort=127.0.0.1:5300:5300/udp\nPublishPort=127.0.0.1:5300:5300/tcp\n"
	out := PairIPv6Binds(in)
	if out != in {
		t.Errorf("expected no v6 pairs when Network= absent (pasta path); got:\n%s", out)
	}
}
