package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFamilyOf_PrefersExplicitFamily(t *testing.T) {
	svc := &CustomService{Name: "postgres-pgvector", Family: "postgres"}
	if got := FamilyOf(svc); got != "postgres" {
		t.Errorf("FamilyOf(postgres-pgvector w/ Family=postgres) = %q, want postgres (this is the postgres-pgvector fix)", got)
	}
}

func TestFamilyOf_FallsBackToInferWhenFamilyEmpty(t *testing.T) {
	svc := &CustomService{Name: "mariadb-10-11"}
	if got := FamilyOf(svc); got != "mariadb" {
		t.Errorf("FamilyOf(mariadb-10-11 w/o Family) = %q, want mariadb via InferFamily fallback", got)
	}
}

func TestFamilyOf_NilSafe(t *testing.T) {
	if got := FamilyOf(nil); got != "" {
		t.Errorf("FamilyOf(nil) = %q, want empty", got)
	}
}

func TestFamilyOfName_BuiltinViaInferFamily(t *testing.T) {
	if got := FamilyOfName("postgres"); got != "postgres" {
		t.Errorf("FamilyOfName(postgres) = %q, want postgres (built-in family lookup)", got)
	}
}

func TestFamilyOfName_UnknownReturnsEmpty(t *testing.T) {
	if got := FamilyOfName("not-a-real-service"); got != "" {
		t.Errorf("FamilyOfName(not-a-real-service) = %q, want empty", got)
	}
}

func TestSaveCustomService_RejectsNewlineInEnv(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	svc := &CustomService{
		Name:  "evil",
		Image: "alpine",
		Environment: map[string]string{
			"PAYLOAD": "ok\nExec=/bin/sh -c pwned",
		},
	}
	err := SaveCustomService(svc)
	if err == nil || !strings.Contains(err.Error(), "newline") {
		t.Fatalf("expected newline rejection, got: %v", err)
	}
}

func TestSaveCustomService_RejectsNewlineInQuadletFields(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	// Each of these fields is written into the generated .container quadlet, so
	// a newline must be rejected to stop directive injection (e.g. a repo's
	// inline service smuggling PodmanArgs=--privileged / Volume=/:/host).
	cases := []func(*CustomService){
		func(s *CustomService) { s.Exec = "redis-server\nPodmanArgs=--privileged" },
		func(s *CustomService) { s.Image = "alpine\nVolume=/:/host:rw" },
		func(s *CustomService) { s.Userns = "keep-id\nPodmanArgs=--pid=host" },
		func(s *CustomService) { s.DataDir = "/data\nExec=/bin/sh -c pwned" },
		func(s *CustomService) { s.Description = "x\nExecStartPost=/bin/sh -c pwned" },
		func(s *CustomService) { s.Ports = []string{"8080:80\nVolume=/:/host"} },
	}
	for i, mutate := range cases {
		svc := &CustomService{Name: "evil", Image: "alpine"}
		mutate(svc)
		if err := SaveCustomService(svc); err == nil {
			t.Errorf("case %d: expected rejection of a newline-bearing quadlet field", i)
		}
	}
	// A clean service still saves.
	if err := SaveCustomService(&CustomService{Name: "good", Image: "alpine", Exec: "redis-server"}); err != nil {
		t.Errorf("clean service should save: %v", err)
	}
}

func TestLoadCustomServiceFromFile_StripsLegacyFilesField(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "phpmyadmin.yaml")
	// Legacy YAML with a files: block (from older lerd versions, or a
	// malicious tamper). Must be stripped on load and the file re-saved
	// without it. The authoritative source is presetFiles in Go.
	yaml := `name: phpmyadmin
image: docker.io/library/phpmyadmin:latest
preset: phpmyadmin
files:
  - target: /etc/hosts
    content: "0.0.0.0 example.com"
`
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	svc, err := LoadCustomServiceFromFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(svc.Files) != 0 {
		t.Errorf("svc.Files should be empty after load, got %+v", svc.Files)
	}
	migrated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migrated: %v", err)
	}
	if strings.Contains(string(migrated), "files:") || strings.Contains(string(migrated), "/etc/hosts") {
		t.Errorf("legacy files: entry survived on-disk migration:\n%s", migrated)
	}
}

func TestMaterializeServiceFiles_OverwritesReadOnlyFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	// Use the real pgadmin preset so the materialiser picks up the
	// hardcoded file definitions (including the chown-required /pgpass).
	svc := &CustomService{
		Name:   "pgadmin",
		Image:  "docker.io/dpage/pgadmin4:latest",
		Preset: "pgadmin",
	}

	if err := MaterializeServiceFiles(svc); err != nil {
		t.Fatalf("first materialize: %v", err)
	}

	path := ServiceFilePath(svc.Name, "/pgpass")
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatalf("chmod 0400: %v", err)
	}

	// Re-materialise: MaterializeServiceFiles must be able to overwrite the
	// read-only file (which is why it unlinks first).
	if err := MaterializeServiceFiles(svc); err != nil {
		t.Fatalf("rewrite over read-only file: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}

	if got, want := filepath.Dir(path), ServiceFilesDir(svc.Name); got != want {
		t.Errorf("dir = %q, want %q", got, want)
	}
}

func TestValidateCustomService_rejectsEnvInjection(t *testing.T) {
	svc := &CustomService{Name: "evil", Image: "alpine",
		Environment: map[string]string{"X": "ok\nPodmanArgs=--privileged"}}
	if err := ValidateCustomService(svc); err == nil {
		t.Error("expected error for newline in environment value")
	}
}

func TestValidateCustomService_rejectsResolvedDynamicEnvInjection(t *testing.T) {
	// The injected directive rides in the dynamic_env KEY; after ResolveDynamicEnv
	// copies it into Environment, the generation-boundary validation must catch it.
	svc := &CustomService{Name: "evil", Image: "alpine",
		DynamicEnv: map[string]string{"X=safe\nPodmanArgs=--privileged\nY": "discover_family:mysql"}}
	if err := ResolveDynamicEnv(svc); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if err := ValidateCustomService(svc); err == nil {
		t.Error("expected error for injection char in resolved dynamic_env key")
	}
}

func TestValidateCustomService_allowsClean(t *testing.T) {
	svc := &CustomService{Name: "ok", Image: "alpine",
		Environment: map[string]string{"FOO": "bar"}}
	if err := ValidateCustomService(svc); err != nil {
		t.Errorf("clean service rejected: %v", err)
	}
}

func TestCustomServiceExists(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if CustomServiceExists("gotenberg") {
		t.Fatalf("expected gotenberg to not exist on a clean tree")
	}

	svc := &CustomService{Name: "gotenberg", Image: "docker.io/gotenberg/gotenberg:8"}
	if err := SaveCustomService(svc); err != nil {
		t.Fatalf("save: %v", err)
	}
	if !CustomServiceExists("gotenberg") {
		t.Fatalf("expected gotenberg to exist after SaveCustomService")
	}

	if err := RemoveCustomService("gotenberg"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if CustomServiceExists("gotenberg") {
		t.Fatalf("expected gotenberg to not exist after RemoveCustomService")
	}
}
