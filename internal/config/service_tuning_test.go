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
		{"untuned family", &CustomService{Name: "redis", Family: "redis"}, "", false},
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
	svc := &CustomService{Name: "redis", Family: "redis"}

	if err := MaterializeServiceTuning(svc); err != nil {
		t.Fatalf("MaterializeServiceTuning: %v", err)
	}
	if _, err := os.Stat(ServiceTuningFile(svc.Name)); !os.IsNotExist(err) {
		t.Errorf("expected no tuning file for untuned family, stat err = %v", err)
	}
}
