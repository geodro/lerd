package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// ── XDG overrides ─────────────────────────────────────────────────────────────

func TestConfigDir_UsesXDGConfigHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	got := ConfigDir()
	want := filepath.Join(tmp, "lerd")
	if got != want {
		t.Errorf("ConfigDir() = %q, want %q", got, want)
	}
}

func TestDataDir_UsesXDGDataHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	got := DataDir()
	want := filepath.Join(tmp, "lerd")
	if got != want {
		t.Errorf("DataDir() = %q, want %q", got, want)
	}
}

// ── Path suffix correctness ───────────────────────────────────────────────────

func TestPathFunctions_ContainExpectedSuffixes(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	cases := []struct {
		name   string
		got    string
		suffix string
	}{
		{"BinDir", BinDir(), "lerd/bin"},
		{"NginxDir", NginxDir(), "lerd/nginx"},
		{"NginxConfD", NginxConfD(), filepath.Join("nginx", "conf.d")},
		{"CertsDir", CertsDir(), "lerd/certs"},
		{"DnsmasqDir", DnsmasqDir(), "lerd/dnsmasq"},
		{"SitesFile", SitesFile(), "sites.yaml"},
		{"GlobalConfigFile", GlobalConfigFile(), "config.yaml"},
		{"QuadletDir", QuadletDir(), filepath.Join("containers", "systemd")},
		{"SystemdUserDir", SystemdUserDir(), filepath.Join("systemd", "user")},
		{"CustomServicesDir", CustomServicesDir(), filepath.Join("lerd", "services")},
		{"FrameworksDir", FrameworksDir(), filepath.Join("lerd", "frameworks")},
		{"UpdateCheckFile", UpdateCheckFile(), "update-check.json"},
		{"PausedDir", PausedDir(), "lerd/paused"},
	}

	for _, c := range cases {
		if !strings.HasSuffix(c.got, c.suffix) {
			t.Errorf("%s() = %q, expected suffix %q", c.name, c.got, c.suffix)
		}
	}
}

func TestPHPConfFile_ContainsVersionAndXdebug(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	got := PHPConfFile("8.3")
	if !strings.Contains(got, "8.3") {
		t.Errorf("PHPConfFile(8.3) = %q, expected to contain version", got)
	}
	if !strings.HasSuffix(got, "99-xdebug.ini") {
		t.Errorf("PHPConfFile(8.3) = %q, expected suffix 99-xdebug.ini", got)
	}
}

func TestPHPUserIniFile_ContainsVersion(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	got := PHPUserIniFile("8.4")
	if !strings.Contains(got, "8.4") {
		t.Errorf("PHPUserIniFile(8.4) = %q, expected to contain version", got)
	}
	if !strings.HasSuffix(got, "98-user.ini") {
		t.Errorf("PHPUserIniFile(8.4) = %q, expected suffix 98-user.ini", got)
	}
}

func TestDataSubDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	got := DataSubDir("mysql")
	if !strings.Contains(got, "mysql") {
		t.Errorf("DataSubDir(mysql) = %q, expected to contain mysql", got)
	}
}
