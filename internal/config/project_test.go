package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestProjectService_UnmarshalYAML_Named(t *testing.T) {
	input := `- redis
- postgres
`
	var services []ProjectService
	if err := yaml.Unmarshal([]byte(input), &services); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(services) != 2 {
		t.Fatalf("want 2 services, got %d", len(services))
	}
	for i, want := range []string{"redis", "postgres"} {
		if services[i].Name != want {
			t.Errorf("services[%d].Name = %q, want %q", i, services[i].Name, want)
		}
		if services[i].Custom != nil {
			t.Errorf("services[%d].Custom should be nil for named reference", i)
		}
	}
}

func TestProjectService_UnmarshalYAML_Inline(t *testing.T) {
	input := `- redis
- mongodb:
    image: docker.io/library/mongo:7
    ports:
      - 27017:27017
    description: MongoDB
`
	var services []ProjectService
	if err := yaml.Unmarshal([]byte(input), &services); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(services) != 2 {
		t.Fatalf("want 2 services, got %d", len(services))
	}

	if services[0].Name != "redis" || services[0].Custom != nil {
		t.Errorf("unexpected first service: %+v", services[0])
	}

	svc := services[1]
	if svc.Name != "mongodb" {
		t.Errorf("Name = %q, want \"mongodb\"", svc.Name)
	}
	if svc.Custom == nil {
		t.Fatal("Custom is nil for inline service")
	}
	if svc.Custom.Image != "docker.io/library/mongo:7" {
		t.Errorf("Image = %q, want docker.io/library/mongo:7", svc.Custom.Image)
	}
	if len(svc.Custom.Ports) != 1 || svc.Custom.Ports[0] != "27017:27017" {
		t.Errorf("Ports = %v", svc.Custom.Ports)
	}
	if svc.Custom.Description != "MongoDB" {
		t.Errorf("Description = %q", svc.Custom.Description)
	}
}

func TestProjectService_RoundTrip(t *testing.T) {
	original := []ProjectService{
		{Name: "redis"},
		{Name: "mongodb", Custom: &CustomService{
			Name:        "mongodb",
			Image:       "mongo:7",
			Description: "MongoDB",
		}},
	}

	data, err := yaml.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var restored []ProjectService
	if err := yaml.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(restored) != 2 {
		t.Fatalf("want 2, got %d", len(restored))
	}
	if restored[0].Name != "redis" || restored[0].Custom != nil {
		t.Errorf("first service: %+v", restored[0])
	}
	if restored[1].Name != "mongodb" || restored[1].Custom == nil {
		t.Errorf("second service: %+v", restored[1])
	}
	if restored[1].Custom.Image != "mongo:7" {
		t.Errorf("Image = %q", restored[1].Custom.Image)
	}
}

func TestProjectConfig_ServiceNames(t *testing.T) {
	cfg := &ProjectConfig{
		Services: []ProjectService{
			{Name: "mysql"},
			{Name: "redis"},
			{Name: "mongodb", Custom: &CustomService{Name: "mongodb", Image: "mongo:7"}},
		},
	}
	names := cfg.ServiceNames()
	want := []string{"mysql", "redis", "mongodb"}
	if len(names) != len(want) {
		t.Fatalf("want %v, got %v", want, names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("names[%d] = %q, want %q", i, names[i], want[i])
		}
	}
}

func TestProjectConfig_Domains(t *testing.T) {
	input := `domains:
  - myapp
  - api
php_version: "8.4"
`
	var cfg ProjectConfig
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(cfg.Domains) != 2 {
		t.Fatalf("want 2 domains, got %d", len(cfg.Domains))
	}
	if cfg.Domains[0] != "myapp" || cfg.Domains[1] != "api" {
		t.Errorf("Domains = %v", cfg.Domains)
	}
}

func TestProjectConfig_Domains_RoundTrip(t *testing.T) {
	cfg := ProjectConfig{
		Domains:    []string{"myapp", "admin"},
		PHPVersion: "8.4",
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var restored ProjectConfig
	if err := yaml.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if len(restored.Domains) != 2 || restored.Domains[0] != "myapp" || restored.Domains[1] != "admin" {
		t.Errorf("round-trip Domains = %v", restored.Domains)
	}
}

func TestProjectConfig_NoDomains(t *testing.T) {
	input := `php_version: "8.4"
`
	var cfg ProjectConfig
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Domains) != 0 {
		t.Errorf("expected no domains, got %v", cfg.Domains)
	}
}

func TestProjectConfig_Workers(t *testing.T) {
	input := `php_version: "8.4"
workers:
  - queue
  - schedule
  - reverb
`
	var cfg ProjectConfig
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(cfg.Workers) != 3 {
		t.Fatalf("want 3 workers, got %d", len(cfg.Workers))
	}
	want := []string{"queue", "schedule", "reverb"}
	for i, w := range want {
		if cfg.Workers[i] != w {
			t.Errorf("Workers[%d] = %q, want %q", i, cfg.Workers[i], w)
		}
	}
}

func TestProjectConfig_Workers_RoundTrip(t *testing.T) {
	cfg := ProjectConfig{
		PHPVersion: "8.4",
		Workers:    []string{"queue", "horizon"},
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var restored ProjectConfig
	if err := yaml.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if len(restored.Workers) != 2 || restored.Workers[0] != "queue" || restored.Workers[1] != "horizon" {
		t.Errorf("round-trip Workers = %v", restored.Workers)
	}
}

func TestProjectConfig_NoWorkers(t *testing.T) {
	input := `php_version: "8.4"
`
	var cfg ProjectConfig
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Workers) != 0 {
		t.Errorf("expected no workers, got %v", cfg.Workers)
	}
}

func TestProjectConfig_DomainsAndWorkers(t *testing.T) {
	cfg := ProjectConfig{
		Domains:  []string{"myapp", "api"},
		Workers:  []string{"queue", "schedule"},
		Secured:  true,
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var restored ProjectConfig
	if err := yaml.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if len(restored.Domains) != 2 || len(restored.Workers) != 2 {
		t.Errorf("Domains=%v, Workers=%v", restored.Domains, restored.Workers)
	}
	if !restored.Secured {
		t.Error("expected Secured=true")
	}
}

func TestProjectConfig_OldFormatCompat(t *testing.T) {
	// Old .lerd.yaml used services: [mysql, redis] — must still parse.
	input := `php_version: "8.3"
secured: true
services:
  - mysql
  - redis
`
	var cfg ProjectConfig
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.PHPVersion != "8.3" {
		t.Errorf("PHPVersion = %q", cfg.PHPVersion)
	}
	names := cfg.ServiceNames()
	if len(names) != 2 || names[0] != "mysql" || names[1] != "redis" {
		t.Errorf("ServiceNames = %v", names)
	}
}
