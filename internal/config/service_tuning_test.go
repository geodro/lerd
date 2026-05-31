package config

import (
	"os"
	"strings"
	"testing"
)

func TestServiceTuningMount_KnownFamilies(t *testing.T) {
	cases := []struct {
		name   string
		svc    *CustomService
		want   string
		wantOK bool
	}{
		{"mysql family", &CustomService{Name: "mysql", Family: "mysql"}, "/etc/mysql/conf.d/zz-lerd-user.cnf", true},
		{"mariadb family", &CustomService{Name: "mariadb-10-11", Family: "mariadb"}, "/etc/mysql/conf.d/zz-lerd-user.cnf", true},
		{"family inferred from name", &CustomService{Name: "mariadb-11"}, "/etc/mysql/conf.d/zz-lerd-user.cnf", true},
		{"redis family", &CustomService{Name: "redis", Family: "redis"}, "/etc/redis/lerd-user.conf", true},
		{"postgres family", &CustomService{Name: "postgres", Family: "postgres"}, "/etc/postgresql/conf.d/zz-lerd-user.conf", true},
		{"untuned family", &CustomService{Name: "meilisearch", Family: "meilisearch"}, "", false},
		{"unknown family", &CustomService{Name: "whatever"}, "", false},
		{"nil service", nil, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target, ok := ServiceTuningMount(tc.svc)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if target != tc.want {
				t.Errorf("target = %q, want %q", target, tc.want)
			}
		})
	}
}

func TestTuningFamilies_SortedAndComplete(t *testing.T) {
	families := TuningFamilies()
	want := []string{"mariadb", "mysql", "postgres", "redis"}
	if len(families) != len(want) {
		t.Fatalf("TuningFamilies length = %d %v, want %d %v", len(families), families, len(want), want)
	}
	for i, f := range want {
		if families[i] != f {
			t.Errorf("TuningFamilies[%d] = %q, want %q (full result: %v)", i, families[i], f, families)
		}
	}
	// Anchor the "supported: …" hint shape so the test catches accidental
	// formatting drift (commas, spacing) when new families land.
	if got := strings.Join(families, ", "); got != "mariadb, mysql, postgres, redis" {
		t.Errorf("joined hint = %q, want %q", got, "mariadb, mysql, postgres, redis")
	}
}

func TestResolveServiceForTuning(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// A custom service resolves from its on-disk YAML.
	if err := SaveCustomService(&CustomService{Name: "mariadb-10-11", Image: "docker.io/library/mariadb:10.11", Family: "mariadb"}); err != nil {
		t.Fatalf("SaveCustomService: %v", err)
	}
	svc, err := ResolveServiceForTuning("mariadb-10-11")
	if err != nil {
		t.Fatalf("custom resolve: %v", err)
	}
	if FamilyOf(svc) != "mariadb" {
		t.Errorf("custom family = %q, want mariadb", FamilyOf(svc))
	}

	// A built-in default preset resolves even with no YAML on disk.
	svc, err = ResolveServiceForTuning("mysql")
	if err != nil {
		t.Fatalf("default preset resolve: %v", err)
	}
	if FamilyOf(svc) != "mysql" {
		t.Errorf("default preset family = %q, want mysql", FamilyOf(svc))
	}

	// An unknown service is an error, not a panic.
	if _, err := ResolveServiceForTuning("does-not-exist"); err == nil {
		t.Errorf("expected error for unknown service")
	}
}

func TestMaterializeServiceTuning_SeedsTemplateForTunableFamily(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	svc := &CustomService{Name: "mariadb-10-11", Family: "mariadb"}

	if err := MaterializeServiceTuning(svc); err != nil {
		t.Fatalf("MaterializeServiceTuning: %v", err)
	}
	body, err := os.ReadFile(ServiceTuningFile(svc.Name))
	if err != nil {
		t.Fatalf("tuning file not created: %v", err)
	}
	if !strings.Contains(string(body), "[mysqld]") {
		t.Errorf("seed template missing [mysqld] header:\n%s", body)
	}
}

func TestMaterializeServiceTuning_NeverOverwritesUserEdits(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	svc := &CustomService{Name: "mysql", Family: "mysql"}

	if err := MaterializeServiceTuning(svc); err != nil {
		t.Fatalf("first materialize: %v", err)
	}
	edited := "[mysqld]\nmax_allowed_packet = 1G\n"
	if err := os.WriteFile(ServiceTuningFile(svc.Name), []byte(edited), 0644); err != nil {
		t.Fatalf("write edit: %v", err)
	}
	if err := MaterializeServiceTuning(svc); err != nil {
		t.Fatalf("second materialize: %v", err)
	}
	got, err := os.ReadFile(ServiceTuningFile(svc.Name))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != edited {
		t.Errorf("user edits clobbered on re-materialize:\ngot:\n%s\nwant:\n%s", got, edited)
	}
}

func TestMaterializeServiceTuning_SkipsUntunedFamily(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	svc := &CustomService{Name: "meilisearch", Family: "meilisearch"}

	if err := MaterializeServiceTuning(svc); err != nil {
		t.Fatalf("MaterializeServiceTuning: %v", err)
	}
	if _, err := os.Stat(ServiceTuningFile(svc.Name)); !os.IsNotExist(err) {
		t.Errorf("expected no tuning file for untuned family, stat err = %v", err)
	}
}

// TestServiceTuningMount_InlineTuningSpec covers the user-extensible path:
// a custom service that doesn't match any known family can still expose
// the Config tab by declaring tuning: inline in its YAML. The inline
// spec wins over the family map, so users can opt arbitrary services
// into the editor without lerd having to recognise their image.
func TestServiceTuningMount_InlineTuningSpec(t *testing.T) {
	svc := &CustomService{
		Name:   "my-cache",
		Family: "memcached", // intentionally not in tuningMounts
		Tuning: &TuningSpec{
			Target:   "/etc/memcached.conf",
			Template: "# my memcached overrides\n",
			Command:  "memcached -f /etc/memcached.conf",
		},
	}
	target, ok := ServiceTuningMount(svc)
	if !ok {
		t.Fatal("inline tuning spec should make the service tunable")
	}
	if target != "/etc/memcached.conf" {
		t.Errorf("target = %q, want /etc/memcached.conf", target)
	}
	cmd, ok := ServiceTuningCommand(svc)
	if !ok || cmd != "memcached -f /etc/memcached.conf" {
		t.Errorf("command = %q ok=%v, want inline command", cmd, ok)
	}
	tmpl, ok := ServiceTuningTemplate(svc)
	if !ok || tmpl != "# my memcached overrides\n" {
		t.Errorf("template = %q ok=%v, want inline template", tmpl, ok)
	}
}

// TestServiceTuningMount_InlineWinsOverFamily verifies an inline spec
// takes precedence over the family-keyed fallback so a user can override
// the bundled mysql/mariadb/redis defaults if they ship a non-standard
// image whose conf path differs.
func TestServiceTuningMount_InlineWinsOverFamily(t *testing.T) {
	svc := &CustomService{
		Name:   "weird-mysql",
		Family: "mysql",
		Tuning: &TuningSpec{
			Target: "/custom/path/mysql.cnf",
		},
	}
	target, ok := ServiceTuningMount(svc)
	if !ok || target != "/custom/path/mysql.cnf" {
		t.Errorf("inline target should override family default: got %q ok=%v", target, ok)
	}
}

// TestServiceTuningMount_InlineRequiresTarget makes sure an empty / zero-
// value TuningSpec doesn't accidentally enable tuning on a service that
// would otherwise be untunable. Target is required; without it the spec
// is treated as absent and the family fallback applies normally.
func TestServiceTuningMount_InlineRequiresTarget(t *testing.T) {
	svc := &CustomService{
		Name:   "no-target",
		Family: "memcached",
		Tuning: &TuningSpec{Template: "anything"},
	}
	if _, ok := ServiceTuningMount(svc); ok {
		t.Error("inline spec without target must not enable tuning")
	}
}

// TestMaterializeServiceTuning_InlineSeedsTemplate covers the seeding
// path for inline specs: lerd writes the template once and never
// clobbers afterwards, same contract as the family-keyed path.
func TestMaterializeServiceTuning_InlineSeedsTemplate(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	svc := &CustomService{
		Name: "my-cache",
		Tuning: &TuningSpec{
			Target:   "/etc/memcached.conf",
			Template: "# user-defined override hints\n",
		},
	}
	if err := MaterializeServiceTuning(svc); err != nil {
		t.Fatalf("MaterializeServiceTuning: %v", err)
	}
	body, err := os.ReadFile(ServiceTuningFile(svc.Name))
	if err != nil {
		t.Fatalf("tuning file not created: %v", err)
	}
	if string(body) != "# user-defined override hints\n" {
		t.Errorf("inline template not seeded:\ngot:\n%s", body)
	}
}

func TestServiceTuningCommand(t *testing.T) {
	cases := []struct {
		name   string
		svc    *CustomService
		want   string
		wantOK bool
	}{
		{"redis needs a command", &CustomService{Name: "redis", Family: "redis"}, "redis-server /etc/redis/lerd-user.conf", true},
		{"postgres points at the wrapper config_file", &CustomService{Name: "postgres", Family: "postgres"}, "postgres -c config_file=/etc/postgresql/lerd.conf", true},
		{"mysql auto-includes, no command", &CustomService{Name: "mysql", Family: "mysql"}, "", false},
		{"untuned family", &CustomService{Name: "meilisearch", Family: "meilisearch"}, "", false},
		{"nil service", nil, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, ok := ServiceTuningCommand(tc.svc)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if cmd != tc.want {
				t.Errorf("command = %q, want %q", cmd, tc.want)
			}
		})
	}
}

// TestServiceTuningAux covers the lerd-managed helper file: only postgres
// declares one today, and it must carry the wrapper config_file target plus
// the include_dir content the Command depends on.
func TestServiceTuningAux(t *testing.T) {
	target, content, ok := ServiceTuningAux(&CustomService{Name: "postgres", Family: "postgres"})
	if !ok {
		t.Fatal("postgres should declare a tuning aux file")
	}
	if target != "/etc/postgresql/lerd.conf" {
		t.Errorf("aux target = %q, want /etc/postgresql/lerd.conf", target)
	}
	if !strings.Contains(content, "include_dir = '/etc/postgresql/conf.d'") {
		t.Errorf("aux content missing include_dir directive:\n%s", content)
	}

	for _, svc := range []*CustomService{
		{Name: "mysql", Family: "mysql"},
		{Name: "redis", Family: "redis"},
		nil,
	} {
		if _, _, ok := ServiceTuningAux(svc); ok {
			t.Errorf("only postgres should have an aux file, got ok for %+v", svc)
		}
	}
}

// TestMaterializeServiceTuning_WritesAndRefreshesAux verifies the helper file is
// materialised for postgres and, unlike the user override, is always rewritten
// so a new lerd version's wrapper lands without a reinstall.
func TestMaterializeServiceTuning_WritesAndRefreshesAux(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	svc := &CustomService{Name: "postgres", Family: "postgres"}

	if err := MaterializeServiceTuning(svc); err != nil {
		t.Fatalf("MaterializeServiceTuning: %v", err)
	}
	auxPath := ServiceTuningAuxFile(svc.Name)
	body, err := os.ReadFile(auxPath)
	if err != nil {
		t.Fatalf("aux file not created: %v", err)
	}
	if !strings.Contains(string(body), "include_dir = '/etc/postgresql/conf.d'") {
		t.Errorf("aux file missing include_dir:\n%s", body)
	}

	// Aux is lerd-managed: a stale value must be overwritten on re-materialize.
	if err := os.WriteFile(auxPath, []byte("# stale\n"), 0644); err != nil {
		t.Fatalf("write stale aux: %v", err)
	}
	if err := MaterializeServiceTuning(svc); err != nil {
		t.Fatalf("second materialize: %v", err)
	}
	got, err := os.ReadFile(auxPath)
	if err != nil {
		t.Fatalf("read back aux: %v", err)
	}
	if strings.Contains(string(got), "stale") {
		t.Errorf("aux file should be rewritten, still stale:\n%s", got)
	}
}
