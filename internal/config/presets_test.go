package config

import (
	"strings"
	"testing"
)

func TestListPresets_IncludesShippedPresets(t *testing.T) {
	presets, err := ListPresets()
	if err != nil {
		t.Fatalf("ListPresets() error = %v", err)
	}
	want := map[string]bool{
		"phpmyadmin":    false,
		"pgadmin":       false,
		"mongo":         false,
		"mongo-express": false,
		"selenium":      false,
		"stripe-mock":   false,
		"mysql":         false,
	}
	for _, p := range presets {
		if _, ok := want[p.Name]; ok {
			want[p.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("ListPresets() missing bundled preset %q", name)
		}
	}
}

func TestListPresets_SortedByName(t *testing.T) {
	presets, err := ListPresets()
	if err != nil {
		t.Fatalf("ListPresets() error = %v", err)
	}
	for i := 1; i < len(presets); i++ {
		if presets[i-1].Name > presets[i].Name {
			t.Errorf("ListPresets() not sorted: %q > %q", presets[i-1].Name, presets[i].Name)
		}
	}
}

func TestLoadPreset_PhpMyAdmin(t *testing.T) {
	p, err := LoadPreset("phpmyadmin")
	if err != nil {
		t.Fatalf("LoadPreset(phpmyadmin) error = %v", err)
	}
	if p.Name != "phpmyadmin" || p.Image == "" || len(p.Ports) == 0 || p.Dashboard == "" {
		t.Errorf("phpmyadmin preset missing required fields: %+v", p)
	}
	if len(p.DependsOn) != 1 || p.DependsOn[0] != "mysql" {
		t.Errorf("phpmyadmin should depend on mysql, got %v", p.DependsOn)
	}
	foundFramingCfg := false
	for _, f := range p.Files {
		if f.Target == "/etc/phpmyadmin/config.user.inc.php" && strings.Contains(f.Content, "AllowThirdPartyFraming") {
			foundFramingCfg = true
			break
		}
	}
	if !foundFramingCfg {
		t.Errorf("phpmyadmin preset must ship config.user.inc.php enabling AllowThirdPartyFraming for iframe embedding")
	}
}

func TestLoadPreset_PgAdmin(t *testing.T) {
	p, err := LoadPreset("pgadmin")
	if err != nil {
		t.Fatalf("LoadPreset(pgadmin) error = %v", err)
	}
	if len(p.DependsOn) != 1 || p.DependsOn[0] != "postgres" {
		t.Errorf("pgadmin should depend on postgres, got %v", p.DependsOn)
	}
	foundFramingCfg := false
	for _, f := range p.Files {
		if f.Target == "/pgadmin4/config_local.py" && strings.Contains(f.Content, "X_FRAME_OPTIONS") {
			foundFramingCfg = true
			break
		}
	}
	if !foundFramingCfg {
		t.Errorf("pgadmin preset must ship config_local.py clearing X_FRAME_OPTIONS for iframe embedding")
	}
}

func TestLoadPreset_MySQL_MultiVersion(t *testing.T) {
	p, err := LoadPreset("mysql")
	if err != nil {
		t.Fatalf("LoadPreset(mysql) error = %v", err)
	}
	if p.Image != "" {
		t.Errorf("multi-version preset must not declare top-level image, got %q", p.Image)
	}
	if len(p.Versions) < 2 {
		t.Errorf("expected at least 2 versions, got %d", len(p.Versions))
	}
	if p.DefaultVersion == "" {
		t.Errorf("DefaultVersion should be set (defaults to first version)")
	}
}

func TestPresetResolve_MultiVersion(t *testing.T) {
	p, err := LoadPreset("mysql")
	if err != nil {
		t.Fatalf("LoadPreset(mysql) error = %v", err)
	}
	svc, err := p.Resolve("5.7")
	if err != nil {
		t.Fatalf("Resolve(5.7): %v", err)
	}
	if svc.Name != "mysql-5-7" {
		t.Errorf("Name = %q, want mysql-5-7", svc.Name)
	}
	if svc.Image != "docker.io/library/mysql:5.7" {
		t.Errorf("Image = %q, want docker.io/library/mysql:5.7", svc.Image)
	}
	foundHost := false
	for _, kv := range svc.EnvVars {
		if kv == "DB_HOST=lerd-mysql-5-7" {
			foundHost = true
		}
	}
	if !foundHost {
		t.Errorf("expected DB_HOST=lerd-mysql-5-7 in env_vars, got %v", svc.EnvVars)
	}
}

func TestPresetResolve_DefaultVersion(t *testing.T) {
	p, err := LoadPreset("mysql")
	if err != nil {
		t.Fatalf("LoadPreset: %v", err)
	}
	svc, err := p.Resolve("")
	if err != nil {
		t.Fatalf("Resolve(\"\"): %v", err)
	}
	if svc.Name != "mysql-"+SanitizeImageTag(p.DefaultVersion) {
		t.Errorf("Resolve(\"\") should fall back to DefaultVersion, got Name=%q", svc.Name)
	}
}

func TestPresetResolve_UnknownVersion(t *testing.T) {
	p, err := LoadPreset("mysql")
	if err != nil {
		t.Fatalf("LoadPreset: %v", err)
	}
	if _, err := p.Resolve("9.9"); err == nil {
		t.Errorf("Resolve(9.9) should error for unknown version")
	}
}

func TestServicesInFamily_BuiltinAndCustom(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	// Built-in mysql is always in family "mysql".
	hosts := ServicesInFamily("mysql")
	if len(hosts) != 1 || hosts[0] != "lerd-mysql" {
		t.Errorf("expected [lerd-mysql], got %v", hosts)
	}

	// Install a fake mysql alternate.
	alt := &CustomService{
		Name:   "mysql-5-7",
		Image:  "docker.io/library/mysql:5.7",
		Family: "mysql",
	}
	if err := SaveCustomService(alt); err != nil {
		t.Fatalf("SaveCustomService: %v", err)
	}

	hosts = ServicesInFamily("mysql")
	if len(hosts) != 2 || hosts[0] != "lerd-mysql" || hosts[1] != "lerd-mysql-5-7" {
		t.Errorf("expected [lerd-mysql lerd-mysql-5-7], got %v", hosts)
	}
}

func TestResolveDynamicEnv_DiscoverFamily(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	svc := &CustomService{
		Name:  "phpmyadmin",
		Image: "phpmyadmin:latest",
		DynamicEnv: map[string]string{
			"PMA_HOSTS": "discover_family:mysql",
		},
	}
	if err := ResolveDynamicEnv(svc); err != nil {
		t.Fatalf("ResolveDynamicEnv: %v", err)
	}
	if got := svc.Environment["PMA_HOSTS"]; got != "lerd-mysql" {
		t.Errorf("PMA_HOSTS = %q, want lerd-mysql", got)
	}
}

func TestResolveDynamicEnv_UnknownDirective(t *testing.T) {
	svc := &CustomService{
		Name: "x",
		DynamicEnv: map[string]string{
			"FOO": "garbage:bar",
		},
	}
	if err := ResolveDynamicEnv(svc); err == nil {
		t.Errorf("expected error for unknown directive")
	}
}

func TestSanitizeImageTag(t *testing.T) {
	cases := map[string]string{
		"5.7":        "5-7",
		"8.0.34":     "8-0-34",
		"11.4-focal": "11-4-focal",
		"v1.7":       "v1-7",
		"latest":     "latest",
		"--weird--":  "weird",
	}
	for in, want := range cases {
		if got := SanitizeImageTag(in); got != want {
			t.Errorf("SanitizeImageTag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoadPreset_Selenium(t *testing.T) {
	p, err := LoadPreset("selenium")
	if err != nil {
		t.Fatalf("LoadPreset(selenium) error = %v", err)
	}
	if p.Name != "selenium" || p.Image == "" || len(p.Ports) == 0 || p.Dashboard == "" {
		t.Errorf("selenium preset missing required fields: %+v", p)
	}
	if !p.ShareHosts {
		t.Error("selenium preset should have share_hosts: true")
	}
}

func TestLoadPreset_Unknown(t *testing.T) {
	if _, err := LoadPreset("does-not-exist"); err == nil {
		t.Errorf("LoadPreset(does-not-exist) expected error, got nil")
	}
}

func TestPresetExists(t *testing.T) {
	if !PresetExists("phpmyadmin") {
		t.Errorf("PresetExists(phpmyadmin) = false, want true")
	}
	if !PresetExists("pgadmin") {
		t.Errorf("PresetExists(pgadmin) = false, want true")
	}
	if PresetExists("nope") {
		t.Errorf("PresetExists(nope) = true, want false")
	}
}
