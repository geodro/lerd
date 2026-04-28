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
		"memcached":     false,
		"rabbitmq":      false,
		"elasticsearch": false,
		"elasticvue":    false,
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
	for _, f := range PresetFiles("phpmyadmin") {
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
	for _, f := range PresetFiles("pgadmin") {
		if f.Target == "/pgadmin4/config_local.py" && strings.Contains(f.Content, "X_FRAME_OPTIONS") {
			foundFramingCfg = true
			break
		}
	}
	if !foundFramingCfg {
		t.Errorf("pgadmin preset must ship config_local.py clearing X_FRAME_OPTIONS for iframe embedding")
	}
}

func TestLoadPreset_Memcached(t *testing.T) {
	p, err := LoadPreset("memcached")
	if err != nil {
		t.Fatalf("LoadPreset(memcached) error = %v", err)
	}
	if p.Image == "" || len(p.Ports) != 1 || p.Ports[0] != "11211:11211" {
		t.Errorf("memcached preset missing image or 11211:11211 port, got: %+v", p)
	}
	if p.DataDir != "" {
		t.Errorf("memcached is in-memory and must not declare data_dir, got %q", p.DataDir)
	}
	if p.EnvDetect == nil || p.EnvDetect.Key != "MEMCACHED_HOST" {
		t.Errorf("memcached env_detect should be key=MEMCACHED_HOST, got %+v", p.EnvDetect)
	}
}

func TestLoadPreset_RabbitMQ(t *testing.T) {
	p, err := LoadPreset("rabbitmq")
	if err != nil {
		t.Fatalf("LoadPreset(rabbitmq) error = %v", err)
	}
	if len(p.Ports) != 2 {
		t.Errorf("rabbitmq should publish AMQP (5672) and management UI (15672), got %v", p.Ports)
	}
	if p.Dashboard == "" {
		t.Errorf("rabbitmq should expose the management UI as dashboard")
	}
	if p.DataDir == "" {
		t.Errorf("rabbitmq should persist /var/lib/rabbitmq for queue durability across restarts")
	}
	if !p.DashboardExternal {
		t.Errorf("rabbitmq must set dashboard_external because Cowboy's session cookie can't be carried by the iframe")
	}
}

func TestLoadPreset_Elasticvue(t *testing.T) {
	p, err := LoadPreset("elasticvue")
	if err != nil {
		t.Fatalf("LoadPreset(elasticvue) error = %v", err)
	}
	if len(p.DependsOn) != 1 || p.DependsOn[0] != "elasticsearch" {
		t.Errorf("elasticvue should depend on elasticsearch, got %v", p.DependsOn)
	}
	if p.Dashboard == "" {
		t.Errorf("elasticvue must expose its UI as dashboard")
	}
	if got := p.Environment["ELASTICVUE_CLUSTERS"]; got == "" {
		t.Errorf("elasticvue must pre-configure the lerd ES cluster via ELASTICVUE_CLUSTERS")
	}
}

func TestLoadPreset_ElasticsearchEnablesCors(t *testing.T) {
	p, err := LoadPreset("elasticsearch")
	if err != nil {
		t.Fatalf("LoadPreset(elasticsearch) error = %v", err)
	}
	if p.Environment["http.cors.enabled"] != "true" {
		t.Errorf("elasticsearch must enable CORS so the elasticvue browser SPA can reach it, got %q", p.Environment["http.cors.enabled"])
	}
	// The wildcard must be wrapped in literal quotes because ES parses env
	// vars as YAML and a bare '*' becomes an alias token that crashes the
	// SnakeYAML scanner on boot.
	if p.Environment["http.cors.allow-origin"] != `"*"` {
		t.Errorf(`elasticsearch must allow any origin for local dev (quoted to survive YAML parse), got %q`, p.Environment["http.cors.allow-origin"])
	}
}

func TestLoadPreset_Elasticsearch(t *testing.T) {
	p, err := LoadPreset("elasticsearch")
	if err != nil {
		t.Fatalf("LoadPreset(elasticsearch) error = %v", err)
	}
	if p.Environment["discovery.type"] != "single-node" {
		t.Errorf("elasticsearch must run in single-node mode for local dev (skips production bootstrap checks), got %q", p.Environment["discovery.type"])
	}
	if p.Environment["xpack.security.enabled"] != "false" {
		t.Errorf("elasticsearch must disable xpack security so apps can connect without TLS+auth in dev, got %q", p.Environment["xpack.security.enabled"])
	}
	if p.EnvDetect == nil || p.EnvDetect.Composer != "elasticsearch/elasticsearch" {
		t.Errorf("elasticsearch env_detect should fire on composer elasticsearch/elasticsearch, got %+v", p.EnvDetect)
	}
	if p.Userns != "keep-id:uid=1000,gid=0" {
		t.Errorf("elasticsearch must map host user to container UID 1000 via keep-id, got %q", p.Userns)
	}
	if !p.ChownData {
		t.Errorf("elasticsearch must set chown_data so the host data dir is owned by the container UID at mount time")
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
		t.Errorf("expected at least 2 versions (8.4 canonical + 8.0 alternate), got %d", len(p.Versions))
	}
	if p.DefaultVersion != "8.4" {
		t.Errorf("DefaultVersion should be 8.4 (the canonical LTS default), got %q", p.DefaultVersion)
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
	// mysql 8.4 LTS is canonical: bare family name, top-level ports, no version suffix.
	if svc.Name != "mysql" {
		t.Errorf("Resolve(\"\") should return canonical bare name, got Name=%q", svc.Name)
	}
	if svc.Image != "docker.io/library/mysql:8.4" {
		t.Errorf("canonical mysql Image = %q, want docker.io/library/mysql:8.4", svc.Image)
	}
	if len(svc.Ports) != 1 || svc.Ports[0] != "3306:3306" {
		t.Errorf("canonical mysql Ports = %v, want [3306:3306]", svc.Ports)
	}
	for _, kv := range svc.EnvVars {
		if strings.Contains(kv, "{{") {
			t.Errorf("canonical mysql env_vars must have no template placeholders, got %q", kv)
		}
	}
	for _, kv := range svc.EnvVars {
		if kv == "DB_HOST=lerd-mysql" {
			return
		}
	}
	t.Errorf("expected DB_HOST=lerd-mysql in canonical env_vars, got %v", svc.EnvVars)
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

func TestResolveDynamicEnv_RepeatFamily(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	svc := &CustomService{
		Name:  "phpmyadmin",
		Image: "phpmyadmin:latest",
		DynamicEnv: map[string]string{
			"PMA_HOSTS":     "discover_family:mysql,mariadb",
			"PMA_USERS":     "repeat_family:mysql,mariadb=root",
			"PMA_PASSWORDS": "repeat_family:mysql,mariadb=lerd",
		},
	}
	if err := ResolveDynamicEnv(svc); err != nil {
		t.Fatalf("ResolveDynamicEnv: %v", err)
	}
	hosts := strings.Split(svc.Environment["PMA_HOSTS"], ",")
	users := strings.Split(svc.Environment["PMA_USERS"], ",")
	passes := strings.Split(svc.Environment["PMA_PASSWORDS"], ",")
	if len(hosts) == 0 {
		t.Fatalf("expected at least one host, got %q", svc.Environment["PMA_HOSTS"])
	}
	if len(users) != len(hosts) || len(passes) != len(hosts) {
		t.Errorf("users/passwords length mismatch: hosts=%d users=%d passes=%d",
			len(hosts), len(users), len(passes))
	}
	for _, u := range users {
		if u != "root" {
			t.Errorf("user = %q, want root", u)
		}
	}
	for _, p := range passes {
		if p != "lerd" {
			t.Errorf("password = %q, want lerd", p)
		}
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

func TestLoadPreset_DefaultsTrackLatest(t *testing.T) {
	// Auto-bumping via track_latest is the user-facing promise that lerd, not
	// users, keeps fresh installs current. The 4 versioned default presets
	// must opt in. Mailpit/rustfs already use rolling :latest tags so the
	// flag is redundant for them.
	for _, name := range []string{"mysql", "postgres", "redis", "meilisearch"} {
		p, err := LoadPreset(name)
		if err != nil {
			t.Errorf("LoadPreset(%s): %v", name, err)
			continue
		}
		if !p.TrackLatest {
			t.Errorf("preset %q must declare track_latest: true so fresh installs land on current upstream", name)
		}
	}
}

func TestListPresets_CanonicalHiddenFromAlternates(t *testing.T) {
	// The canonical version is the default install — listing it as an
	// "alternate" would let users pick it from the picker and clobber the
	// default service. Filter it out of PresetMeta.Versions.
	metas, err := ListPresets()
	if err != nil {
		t.Fatalf("ListPresets: %v", err)
	}
	var mysql *PresetMeta
	for i := range metas {
		if metas[i].Name == "mysql" {
			mysql = &metas[i]
			break
		}
	}
	if mysql == nil {
		t.Fatal("mysql preset missing from ListPresets")
	}
	for _, v := range mysql.Versions {
		if v.Canonical {
			t.Errorf("ListPresets must not surface canonical version %q in the alternates picker", v.Tag)
		}
		if v.Tag == "8.4" {
			t.Errorf("8.4 is canonical and should be filtered out of mysql alternates")
		}
	}
	wantAlts := map[string]bool{"8.0": false, "5.7": false}
	for _, v := range mysql.Versions {
		if _, ok := wantAlts[v.Tag]; ok {
			wantAlts[v.Tag] = true
		}
	}
	for tag, found := range wantAlts {
		if !found {
			t.Errorf("expected mysql alternate %q in picker, got %v", tag, mysql.Versions)
		}
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

func TestDefaultPresetNames_ContainsAllSix(t *testing.T) {
	names := DefaultPresetNames()
	want := map[string]bool{
		"mysql": false, "redis": false, "postgres": false,
		"meilisearch": false, "rustfs": false, "mailpit": false,
	}
	for _, n := range names {
		if _, ok := want[n]; ok {
			want[n] = true
		}
	}
	for n, found := range want {
		if !found {
			t.Errorf("DefaultPresetNames missing %q (got %v)", n, names)
		}
	}
	// Sorted output for deterministic iteration order.
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Errorf("DefaultPresetNames not sorted: %q > %q", names[i-1], names[i])
		}
	}
}

func TestIsDefaultPreset(t *testing.T) {
	for _, name := range []string{"mysql", "postgres", "redis", "meilisearch", "rustfs", "mailpit"} {
		if !IsDefaultPreset(name) {
			t.Errorf("IsDefaultPreset(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"phpmyadmin", "pgadmin", "mongo", "rabbitmq", "elasticsearch", "nope"} {
		if IsDefaultPreset(name) {
			t.Errorf("IsDefaultPreset(%q) = true, want false", name)
		}
	}
}

func TestLoadPreset_DefaultsHaveFlag(t *testing.T) {
	for _, name := range []string{"mysql", "postgres", "redis", "meilisearch", "rustfs", "mailpit"} {
		p, err := LoadPreset(name)
		if err != nil {
			t.Fatalf("LoadPreset(%s): %v", name, err)
		}
		if !p.Default {
			t.Errorf("preset %q must declare default: true", name)
		}
		if p.UpdateStrategy == "" {
			t.Errorf("preset %q must declare update_strategy", name)
		}
		if p.Family == "" {
			t.Errorf("preset %q must declare family", name)
		}
	}
}

func TestPresetResolve_AlternatesUseHostPort(t *testing.T) {
	// Regression: when the canonical version of a multi-version preset is added,
	// non-canonical alternates must keep their dedicated host_port so they don't
	// collide with the canonical container on the same port.
	p, err := LoadPreset("mysql")
	if err != nil {
		t.Fatalf("LoadPreset: %v", err)
	}
	cases := map[string]string{
		"5.7": "3357:3306",
		"8.0": "3380:3306",
	}
	for tag, wantPort := range cases {
		svc, err := p.Resolve(tag)
		if err != nil {
			t.Fatalf("Resolve(%s): %v", tag, err)
		}
		if len(svc.Ports) == 0 || svc.Ports[0] != wantPort {
			t.Errorf("mysql %s: Ports[0] = %v, want %q", tag, svc.Ports, wantPort)
		}
	}
}

func TestPresetResolve_MysqlCanonical(t *testing.T) {
	p, err := LoadPreset("mysql")
	if err != nil {
		t.Fatalf("LoadPreset: %v", err)
	}
	svc, err := p.Resolve("8.4")
	if err != nil {
		t.Fatalf("Resolve(8.4): %v", err)
	}
	if svc.Name != "mysql" {
		t.Errorf("canonical version Name = %q, want bare mysql", svc.Name)
	}
	if svc.PresetVersion != "8.4" {
		t.Errorf("PresetVersion = %q, want 8.4", svc.PresetVersion)
	}
	// Non-canonical resolve still produces the suffixed name.
	alt, err := p.Resolve("5.7")
	if err != nil {
		t.Fatalf("Resolve(5.7): %v", err)
	}
	if alt.Name != "mysql-5-7" {
		t.Errorf("non-canonical Name = %q, want mysql-5-7", alt.Name)
	}
	for _, port := range alt.Ports {
		if strings.Contains(port, "{{") {
			t.Errorf("non-canonical ports must be substituted, got %q", port)
		}
	}
}

func TestPresetCanonical_ValidationOnlyOne(t *testing.T) {
	yamlData := []byte(`name: testfam
default: true
family: testfam
default_version: "1"
versions:
  - tag: "1"
    image: example/test:1
    canonical: true
  - tag: "2"
    image: example/test:2
    canonical: true
ports:
  - "1234:1234"
`)
	if err := ValidatePresetYAML(yamlData, "testfam"); err == nil {
		t.Errorf("expected validation error for two canonical versions")
	}
}

func TestPresetApplyPlatformOverride(t *testing.T) {
	p := &Preset{
		CustomService: CustomService{
			Name:  "postgres",
			Image: "docker.io/postgis/postgis:16-3.5-alpine",
		},
		PlatformOverrides: []PresetPlatformImage{
			{OS: "darwin", ImageMatch: "postgis/postgis*alpine*", Image: "docker.io/imresamu/postgis:{{tag}}"},
		},
	}
	svc := &CustomService{Image: p.Image}
	p.ApplyPlatformOverride(svc, "darwin")
	if svc.Image != "docker.io/imresamu/postgis:16-3.5-alpine" {
		t.Errorf("darwin override not applied: got %q", svc.Image)
	}
	// Regression: track_latest may resolve to a newer tag (e.g. 16.4-3.5-alpine)
	// before the override runs. The override must preserve the resolved tag,
	// not hardcode it back to the YAML default.
	svc3 := &CustomService{Image: "docker.io/postgis/postgis:16.4-3.5-alpine"}
	p.ApplyPlatformOverride(svc3, "darwin")
	if svc3.Image != "docker.io/imresamu/postgis:16.4-3.5-alpine" {
		t.Errorf("darwin override must preserve resolved tag: got %q, want imresamu/postgis:16.4-3.5-alpine", svc3.Image)
	}
	svc2 := &CustomService{Image: p.Image}
	p.ApplyPlatformOverride(svc2, "linux")
	if svc2.Image != p.Image {
		t.Errorf("linux must keep upstream image, got %q", svc2.Image)
	}
}

func TestLoadPreset_PostgresPlatformOverride(t *testing.T) {
	p, err := LoadPreset("postgres")
	if err != nil {
		t.Fatalf("LoadPreset(postgres): %v", err)
	}
	if len(p.PlatformOverrides) == 0 {
		t.Fatal("postgres preset must declare a darwin platform_override (lifted from cli.platformImageOverride)")
	}
	foundDarwin := false
	for _, po := range p.PlatformOverrides {
		if po.OS == "darwin" && strings.Contains(po.Image, "imresamu") {
			foundDarwin = true
		}
	}
	if !foundDarwin {
		t.Errorf("postgres preset must override to imresamu/postgis on darwin, got %+v", p.PlatformOverrides)
	}
}

func TestDefaultPresetMeta_Caches(t *testing.T) {
	a, err := DefaultPresetMeta("mysql")
	if err != nil {
		t.Fatalf("DefaultPresetMeta(mysql): %v", err)
	}
	if a.Name != "mysql" {
		t.Errorf("Name = %q, want mysql", a.Name)
	}
	if got := DefaultPresetEnvVars("mysql"); len(got) == 0 {
		t.Errorf("DefaultPresetEnvVars(mysql) is empty")
	}
	if got := DefaultPresetEnvVars("sqlite"); got != nil {
		t.Errorf("DefaultPresetEnvVars(sqlite) must return nil for non-default presets")
	}
	if DefaultPresetDashboard("mailpit") != "http://localhost:8025" {
		t.Errorf("DefaultPresetDashboard(mailpit) wrong")
	}
	if DefaultPresetConnectionURL("postgres") != "postgresql://postgres:lerd@127.0.0.1:5432/lerd" {
		t.Errorf("DefaultPresetConnectionURL(postgres) wrong")
	}
}

func TestLoadPreset_DefaultEnvVarsParity(t *testing.T) {
	// Each default preset must encode the same env_vars that lerd shipped from
	// the hardcoded cli.serviceEnvVars map. This is the no-regression test for
	// the migration: swapping the map for preset reads must not change a single
	// .env line a Laravel project sees.
	cases := map[string][]string{
		"mysql": {
			"DB_CONNECTION=mysql",
			"DB_HOST=lerd-mysql",
			"DB_PORT=3306",
			"DB_DATABASE=lerd",
			"DB_USERNAME=root",
			"DB_PASSWORD=lerd",
		},
		"postgres": {
			"DB_CONNECTION=pgsql",
			"DB_HOST=lerd-postgres",
			"DB_PORT=5432",
			"DB_DATABASE=lerd",
			"DB_USERNAME=postgres",
			"DB_PASSWORD=lerd",
		},
		"redis": {
			"REDIS_HOST=lerd-redis",
			"REDIS_PORT=6379",
			"REDIS_PASSWORD=null",
			"CACHE_STORE=redis",
			"SESSION_DRIVER=redis",
			"QUEUE_CONNECTION=redis",
		},
		"meilisearch": {
			"SCOUT_DRIVER=meilisearch",
			"MEILISEARCH_HOST=http://lerd-meilisearch:7700",
		},
		"rustfs": {
			"FILESYSTEM_DISK=s3",
			"AWS_ACCESS_KEY_ID=lerd",
			"AWS_SECRET_ACCESS_KEY=lerdpassword",
			"AWS_DEFAULT_REGION=us-east-1",
			"AWS_BUCKET=lerd",
			"AWS_URL=http://localhost:9000",
			"AWS_ENDPOINT=http://lerd-rustfs:9000",
			"AWS_USE_PATH_STYLE_ENDPOINT=true",
		},
		"mailpit": {
			"MAIL_MAILER=smtp",
			"MAIL_HOST=lerd-mailpit",
			"MAIL_PORT=1025",
			"MAIL_USERNAME=null",
			"MAIL_PASSWORD=null",
			"MAIL_ENCRYPTION=null",
		},
	}
	for name, want := range cases {
		p, err := LoadPreset(name)
		if err != nil {
			t.Errorf("LoadPreset(%s): %v", name, err)
			continue
		}
		svc, err := p.Resolve("")
		if err != nil {
			t.Errorf("Resolve(%s): %v", name, err)
			continue
		}
		if len(svc.EnvVars) != len(want) {
			t.Errorf("%s: EnvVars length = %d, want %d (got %v, want %v)", name, len(svc.EnvVars), len(want), svc.EnvVars, want)
			continue
		}
		for i, kv := range want {
			if svc.EnvVars[i] != kv {
				t.Errorf("%s: EnvVars[%d] = %q, want %q", name, i, svc.EnvVars[i], kv)
			}
		}
	}
}
