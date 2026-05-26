//go:build darwin

package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHasNonZeroExitCode covers the failure-detection helper for both
// container plists (mysql / postgres / …) and runtime-mode worker plists
// (queue / schedule / horizon — `/bin/sh worker.sh` → `podman exec`).
// The (never exited) sentinel must NOT count as failure: it's the brief
// in-flight window between Start() returning and ContainerCache catching
// up, and surfacing it as failed would trigger spurious heal cycles on
// every fresh start.
func TestHasNonZeroExitCode(t *testing.T) {
	cases := map[string]bool{
		"state = waiting\n\tlast exit code = 0\n":                  false,
		"state = waiting\n\tlast exit code = 1\n":                  true,
		"state = not running\n\tlast exit code = 1\n":              true,
		"state = not running\n\tlast exit code = 137\n":            true,
		"state = not running\n\tlast exit code = (never exited)\n": false,
		"state = running\n":                                        false, // no exit code line at all
		"":                                                         false,
	}
	for in, want := range cases {
		if got := hasNonZeroExitCode(in); got != want {
			t.Errorf("hasNonZeroExitCode(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestIsContainerPlist verifies the heuristic distinguishing container
// launchers from runtime-mode workers and native-binary plists. Misclassifying
// a runtime worker as container would cause the "container crashed
// post-detach" failure branch in UnitStatus to fire on idle workers.
func TestIsContainerPlist(t *testing.T) {
	containerOut := []byte(`	program = /opt/homebrew/bin/podman
	arguments = {
		/opt/homebrew/bin/podman
		run
		-d
		--restart=always
		--name
		lerd-mysql
	}`)
	if !isContainerPlist(containerOut) {
		t.Error("expected container plist to be classified as container")
	}

	runtimeOut := []byte(`	program = /bin/sh
	arguments = {
		/bin/sh
		/Users/x/.local/share/lerd/run/workers/lerd-queue-app.sh
	}`)
	if isContainerPlist(runtimeOut) {
		t.Error("runtime worker plist must NOT be classified as container")
	}

	binaryOut := []byte(`	program = /Users/x/.local/bin/lerd-tray
	arguments = { /Users/x/.local/bin/lerd-tray }`)
	if isContainerPlist(binaryOut) {
		t.Error("native-binary plist must NOT be classified as container")
	}
}

func TestXmlEscStr(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"hello", "hello"},
		{"a<b>c", "a&lt;b&gt;c"},
		{"a&b", "a&amp;b"},
		// Single and double quotes are valid in XML PCDATA — must NOT be
		// escaped as &#39;/&#34; because Apple's plist parser passes those
		// numeric character references through literally instead of decoding them.
		{`a"b`, `a"b`},
		{"a'b", "a'b"},
		{"X_FRAME_OPTIONS=''", "X_FRAME_OPTIONS=''"},
		{"", ""},
	}
	for _, tt := range tests {
		got := xmlEscStr(tt.in)
		if got != tt.want {
			t.Errorf("xmlEscStr(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestBuildPlist(t *testing.T) {
	plist := buildPlist("com.lerd.test", []string{"/bin/sh", "--flag"}, true, keepAliveAlways, "/tmp/out.log", "/tmp/err.log")

	checks := []string{
		`<string>com.lerd.test</string>`,
		`<string>/bin/sh</string>`,
		`<string>--flag</string>`,
		`<key>RunAtLoad</key>`,
		`<true/>`,
		`<key>KeepAlive</key>`,
		`<key>StandardOutPath</key>`,
		`<string>/tmp/out.log</string>`,
		`<key>StandardErrorPath</key>`,
		`<string>/tmp/err.log</string>`,
		`<?xml version="1.0"`,
		`<!DOCTYPE plist`,
	}
	for _, want := range checks {
		if !strings.Contains(plist, want) {
			t.Errorf("buildPlist output missing %q", want)
		}
	}
}

func TestBuildPlistNoOptionalFields(t *testing.T) {
	plist := buildPlist("com.lerd.minimal", []string{"/bin/true"}, false, keepAliveNever, "", "")

	if strings.Contains(plist, "RunAtLoad") {
		t.Error("expected no RunAtLoad key")
	}
	if strings.Contains(plist, "KeepAlive") {
		t.Error("expected no KeepAlive key")
	}
	if strings.Contains(plist, "StandardOutPath") {
		t.Error("expected no StandardOutPath key")
	}
}

func TestBuildPlistOnFailureEmitsSuccessfulExitDict(t *testing.T) {
	plist := buildPlist("com.lerd.onfail", []string{"/bin/true"}, false, keepAliveOnFailure, "", "")

	if !strings.Contains(plist, "<key>KeepAlive</key>") {
		t.Fatal("expected KeepAlive key")
	}
	if !strings.Contains(plist, "<key>SuccessfulExit</key>") || !strings.Contains(plist, "<false/>") {
		t.Errorf("on-failure policy should emit KeepAlive: SuccessfulExit=false, got:\n%s", plist)
	}
	if strings.Contains(plist, "<key>KeepAlive</key>\n\t<true/>") {
		t.Errorf("on-failure must not emit bare KeepAlive=true, got:\n%s", plist)
	}
}

func TestBuildPlistXMLEscaping(t *testing.T) {
	plist := buildPlist("com.lerd.esc", []string{"/bin/echo", "a<b>&c"}, false, keepAliveNever, "", "")
	if !strings.Contains(plist, "a&lt;b&gt;&amp;c") {
		t.Error("arguments should be XML-escaped in plist")
	}
}

func TestParseSection(t *testing.T) {
	content := `[Service]
ExecStart=/bin/sh --flag
Type=simple

[Install]
WantedBy=default.target
`
	svc := parseSection(content, "Service")
	if got := svc["ExecStart"]; len(got) != 1 || got[0] != "/bin/sh --flag" {
		t.Errorf("ExecStart = %v, want [/bin/sh --flag]", got)
	}
	if got := svc["Type"]; len(got) != 1 || got[0] != "simple" {
		t.Errorf("Type = %v, want [simple]", got)
	}
	if got := svc["WantedBy"]; len(got) != 0 {
		t.Errorf("WantedBy should not be in [Service] section, got %v", got)
	}

	install := parseSection(content, "Install")
	if got := install["WantedBy"]; len(got) != 1 || got[0] != "default.target" {
		t.Errorf("WantedBy = %v, want [default.target]", got)
	}
}

func TestParseSectionMultipleValues(t *testing.T) {
	content := `[Container]
Volume=/home:/home:z
Volume=/var:/var:ro
Image=test:local
`
	c := parseSection(content, "Container")
	if got := c["Volume"]; len(got) != 2 {
		t.Errorf("expected 2 Volume entries, got %d", len(got))
	}
}

func TestParseSectionSkipsComments(t *testing.T) {
	content := `[Service]
# This is a comment
; This is also a comment
ExecStart=/bin/true
`
	svc := parseSection(content, "Service")
	if len(svc) != 1 {
		t.Errorf("expected 1 key, got %d: %v", len(svc), svc)
	}
}

func TestStripSELinuxVolOpts(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"/home:/home:z", "/home:/home"},
		{"/home:/home:Z", "/home:/home"},
		{"/home:/home:ro,z", "/home:/home:ro"},
		{"/home:/home:z,ro", "/home:/home:ro"},
		{"/home:/home:ro", "/home:/home:ro"},
		{"/home:/home", "/home:/home"},
		{"/home:/home:z,Z", "/home:/home"},
		{"/home:/home:ro,z,noexec", "/home:/home:ro,noexec"},
	}
	for _, tt := range tests {
		got := stripSELinuxVolOpts(tt.in)
		if got != tt.want {
			t.Errorf("stripSELinuxVolOpts(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestContainerToPodmanArgs(t *testing.T) {
	c := map[string][]string{
		"ContainerName": {"lerd-nginx"},
		"Image":         {"docker.io/library/nginx:latest"},
		"PublishPort":   {"127.0.0.1:80:80"},
		"Network":       {"lerd"},
		"Volume":        {"/home/user/sites:/home/user/sites:ro"},
		"Environment":   {"FOO=bar"},
	}
	args, err := containerToPodmanArgs(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argStr := strings.Join(args, " ")
	for _, want := range []string{
		"run", "-d", "--restart=always",
		"--name", "lerd-nginx", "--replace",
		"--network", "lerd",
		"-p", "80:80",
		"-e", "FOO=bar",
		"docker.io/library/nginx:latest",
	} {
		if !strings.Contains(argStr, want) {
			t.Errorf("args missing %q in: %s", want, argStr)
		}
	}
}

// Quadlet's HostName= maps to --hostname on Linux via podman-systemd; the
// macOS path was dropping it, so `lerd shell` showed root@<container-id>.
func TestContainerToPodmanArgs_HostName(t *testing.T) {
	c := map[string][]string{
		"ContainerName": {"lerd-php84-fpm"},
		"Image":         {"localhost/lerd-php-fpm:8.4"},
		"HostName":      {"laptop"},
	}
	args, err := containerToPodmanArgs(c)
	if err != nil {
		t.Fatal(err)
	}
	argStr := strings.Join(args, " ")
	if !strings.Contains(argStr, "--hostname laptop") {
		t.Errorf("HostName must produce --hostname: %s", argStr)
	}
}

func TestContainerToPodmanArgsNoImage(t *testing.T) {
	c := map[string][]string{
		"ContainerName": {"test"},
	}
	_, err := containerToPodmanArgs(c)
	if err == nil {
		t.Error("expected error for missing Image")
	}
}

func TestPlistLabel(t *testing.T) {
	if got := plistLabel("lerd-nginx"); got != "com.lerd.lerd-nginx" {
		t.Errorf("plistLabel = %q, want com.lerd.lerd-nginx", got)
	}
}

func TestParseServiceUnitRestartPolicy(t *testing.T) {
	cases := []struct {
		name string
		body string
		want keepAlivePolicy
	}{
		{"always", "[Service]\nExecStart=/bin/sh\nRestart=always\n", keepAliveAlways},
		{"on-failure", "[Service]\nExecStart=/bin/sh\nRestart=on-failure\n", keepAliveOnFailure},
		{"none", "[Service]\nExecStart=/bin/sh\n", keepAliveNever},
		{"no", "[Service]\nExecStart=/bin/sh\nRestart=no\n", keepAliveNever},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, got, err := parseServiceUnit("lerd-test", tc.body)
			if err != nil {
				t.Fatalf("parseServiceUnit: %v", err)
			}
			if got != tc.want {
				t.Errorf("Restart=%q → policy %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestWriteAndRemoveServiceUnit(t *testing.T) {
	tmp := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmp)
	defer os.Setenv("HOME", origHome)

	// Ensure LaunchAgents + Logs dirs exist
	os.MkdirAll(filepath.Join(tmp, "Library", "LaunchAgents"), 0755)
	os.MkdirAll(filepath.Join(tmp, "Library", "Logs", "lerd"), 0755)

	mgr := &darwinServiceManager{}
	content := "[Service]\nExecStart=/bin/sh --flag\n"

	if err := mgr.WriteServiceUnit("lerd-watcher", content); err != nil {
		t.Fatalf("WriteServiceUnit: %v", err)
	}

	path := filepath.Join(tmp, "Library", "LaunchAgents", "lerd-watcher.plist")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("plist not written: %v", err)
	}
	if !strings.Contains(string(data), "com.lerd.lerd-watcher") {
		t.Error("plist missing expected label")
	}
	if !strings.Contains(string(data), "/bin/sh") {
		t.Error("plist missing expected ExecStart binary")
	}

	// ListServiceUnits should find it
	units := mgr.ListServiceUnits("lerd-*")
	if len(units) != 1 || units[0] != "lerd-watcher" {
		t.Errorf("ListServiceUnits = %v, want [lerd-watcher]", units)
	}

	// Remove
	if err := mgr.RemoveServiceUnit("lerd-watcher"); err != nil {
		t.Fatalf("RemoveServiceUnit: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("plist should be removed")
	}
}

func TestWriteServiceUnitIfChangedNoChange(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	os.MkdirAll(filepath.Join(tmp, "Library", "LaunchAgents"), 0755)
	os.MkdirAll(filepath.Join(tmp, "Library", "Logs", "lerd"), 0755)

	mgr := &darwinServiceManager{}
	content := "[Service]\nExecStart=/bin/sh\n"

	mgr.WriteServiceUnit("lerd-test", content)

	changed, err := mgr.WriteServiceUnitIfChanged("lerd-test", content)
	if err != nil {
		t.Fatalf("WriteServiceUnitIfChanged: %v", err)
	}
	if changed {
		t.Error("expected no change on identical content")
	}
}

func TestWriteContainerUnit(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	os.MkdirAll(filepath.Join(tmp, "Library", "LaunchAgents"), 0755)
	os.MkdirAll(filepath.Join(tmp, "Library", "Logs", "lerd"), 0755)

	mgr := &darwinServiceManager{}
	content := "[Container]\nContainerName=lerd-nginx\nImage=nginx:latest\nPublishPort=80:80\n"

	if err := mgr.WriteContainerUnit("lerd-nginx", content); err != nil {
		t.Fatalf("WriteContainerUnit: %v", err)
	}

	path := filepath.Join(tmp, "Library", "LaunchAgents", "lerd-nginx.plist")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("plist not written: %v", err)
	}
	plist := string(data)
	if !strings.Contains(plist, "podman") {
		t.Error("container plist should contain podman command")
	}
	if !strings.Contains(plist, "nginx:latest") {
		t.Error("container plist should contain image name")
	}

	if !mgr.ContainerUnitInstalled("lerd-nginx") {
		t.Error("ContainerUnitInstalled should return true")
	}
	if mgr.ContainerUnitInstalled("lerd-nonexistent") {
		t.Error("ContainerUnitInstalled should return false for missing unit")
	}
}

func TestIsEnabledBasedOnPlistExistence(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	os.MkdirAll(filepath.Join(tmp, "Library", "LaunchAgents"), 0755)

	mgr := &darwinServiceManager{}

	if mgr.IsEnabled("lerd-test") {
		t.Error("should not be enabled before plist exists")
	}

	os.WriteFile(
		filepath.Join(tmp, "Library", "LaunchAgents", "lerd-test.plist"),
		[]byte("placeholder"), 0644,
	)

	if !mgr.IsEnabled("lerd-test") {
		t.Error("should be enabled when plist exists")
	}
}

func TestUnquoteSystemdValue(t *testing.T) {
	cases := []struct{ in, want string }{
		{`"FOO=bar"`, `FOO=bar`},
		{`"ES_JAVA_OPTS=-Xms512m -Xmx512m"`, `ES_JAVA_OPTS=-Xms512m -Xmx512m`},
		{`"http.cors.allow-origin=\"*\""`, `http.cors.allow-origin="*"`},
		{`FOO=bar`, `FOO=bar`},
		{`"unclosed`, `"unclosed`},
		{``, ``},
	}
	for _, tt := range cases {
		if got := unquoteSystemdValue(tt.in); got != tt.want {
			t.Errorf("unquoteSystemdValue(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestContainerToPodmanArgsQuotedEnv(t *testing.T) {
	c := map[string][]string{
		"ContainerName": {"lerd-elasticsearch"},
		"Image":         {"docker.elastic.co/elasticsearch/elasticsearch:8.13.4"},
		"Network":       {"lerd"},
		"Environment": {
			`"ES_JAVA_OPTS=-Xms512m -Xmx512m"`,
			`"http.cors.allow-origin=\"*\""`,
			`"discovery.type=single-node"`,
		},
		"UserNS": {"keep-id:uid=1000,gid=0"},
	}
	args, err := containerToPodmanArgs(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	argStr := strings.Join(args, " ")

	for _, want := range []string{
		"-e ES_JAVA_OPTS=-Xms512m -Xmx512m",
		`-e http.cors.allow-origin="*"`,
		"-e discovery.type=single-node",
		"--userns keep-id:uid=1000,gid=0",
	} {
		if !strings.Contains(argStr, want) {
			t.Errorf("args missing %q in:\n%s", want, argStr)
		}
	}
	// Confirm no literal quotes bleed through into env values
	for _, bad := range []string{`-e "ES_JAVA_OPTS`, `"discovery.type`} {
		if strings.Contains(argStr, bad) {
			t.Errorf("args contain literal quote in env: %q found in:\n%s", bad, argStr)
		}
	}
}

func TestStripPrivilegedIPBind_v4(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1:80:80":     "80:80",
		"127.0.0.1:443:443":   "443:443",
		"127.0.0.1:5300:5300": "127.0.0.1:5300:5300",
		"0.0.0.0:80:80":       "80:80",
		"3306:3306":           "3306:3306",
		"127.0.0.1:80:80/tcp": "80:80/tcp",
		"192.168.1.1:8080:80": "192.168.1.1:8080:80",
	}
	for in, want := range cases {
		if got := stripPrivilegedIPBind(in); got != want {
			t.Errorf("stripPrivilegedIPBind(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStripPrivilegedIPBind_v6(t *testing.T) {
	cases := map[string]string{
		"[::1]:80:80":         "80:80",
		"[::1]:443:443":       "443:443",
		"[::1]:5300:5300":     "[::1]:5300:5300",
		"[::1]:5300:5300/udp": "[::1]:5300:5300/udp",
		"[::]:80:80":          "80:80",
		"[::1]:443:443/tcp":   "443:443/tcp",
	}
	for in, want := range cases {
		if got := stripPrivilegedIPBind(in); got != want {
			t.Errorf("stripPrivilegedIPBind(%q) = %q, want %q", in, got, want)
		}
	}
}
