//go:build darwin

package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestXmlEscStr(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"hello", "hello"},
		{"a<b>c", "a&lt;b&gt;c"},
		{"a&b", "a&amp;b"},
		{`a"b`, "a&#34;b"},
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
	plist := buildPlist("com.lerd.test", []string{"/usr/bin/test", "--flag"}, true, true, "/tmp/out.log", "/tmp/err.log")

	checks := []string{
		`<string>com.lerd.test</string>`,
		`<string>/usr/bin/test</string>`,
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
	plist := buildPlist("com.lerd.minimal", []string{"/bin/true"}, false, false, "", "")

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

func TestBuildPlistXMLEscaping(t *testing.T) {
	plist := buildPlist("com.lerd.esc", []string{"/bin/echo", "a<b>&c"}, false, false, "", "")
	if !strings.Contains(plist, "a&lt;b&gt;&amp;c") {
		t.Error("arguments should be XML-escaped in plist")
	}
}

func TestParseSection(t *testing.T) {
	content := `[Service]
ExecStart=/usr/bin/test --flag
Type=simple

[Install]
WantedBy=default.target
`
	svc := parseSection(content, "Service")
	if got := svc["ExecStart"]; len(got) != 1 || got[0] != "/usr/bin/test --flag" {
		t.Errorf("ExecStart = %v, want [/usr/bin/test --flag]", got)
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
		"-p", "127.0.0.1:80:80",
		"-e", "FOO=bar",
		"docker.io/library/nginx:latest",
	} {
		if !strings.Contains(argStr, want) {
			t.Errorf("args missing %q in: %s", want, argStr)
		}
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

func TestWriteAndRemoveServiceUnit(t *testing.T) {
	tmp := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmp)
	defer os.Setenv("HOME", origHome)

	// Ensure LaunchAgents + Logs dirs exist
	os.MkdirAll(filepath.Join(tmp, "Library", "LaunchAgents"), 0755)
	os.MkdirAll(filepath.Join(tmp, "Library", "Logs", "lerd"), 0755)

	mgr := &darwinServiceManager{}
	content := "[Service]\nExecStart=/usr/bin/test --flag\n"

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
	if !strings.Contains(string(data), "/usr/bin/test") {
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
	content := "[Service]\nExecStart=/usr/bin/test\n"

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
