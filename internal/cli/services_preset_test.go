package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func TestInstallPresetByName_Unknown(t *testing.T) {
	_, err := InstallPresetByName("does-not-exist", "")
	if err == nil {
		t.Fatalf("expected error for unknown preset, got nil")
	}
	if !strings.Contains(err.Error(), "unknown preset") {
		t.Errorf("error = %v, want it to mention 'unknown preset'", err)
	}
}

func TestMissingPresetDependencies_BuiltinDepIsSatisfied(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	// MissingPresetDependencies now treats built-ins as installed only
	// when their quadlet is on disk (see the reinstall transactionality
	// PR). Materialise a lerd-mysql.container so the dep counts as
	// satisfied — matches the post-`lerd install` state on a real host.
	qdir := config.QuadletDir()
	if err := os.MkdirAll(qdir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(qdir, "lerd-mysql.container"), []byte("[Container]\nImage=docker.io/library/mysql:8\n"), 0644); err != nil {
		t.Fatal(err)
	}

	preset, err := config.LoadPreset("phpmyadmin")
	if err != nil {
		t.Fatalf("LoadPreset: %v", err)
	}
	svc, err := preset.Resolve("")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if missing := MissingPresetDependencies(svc); len(missing) != 0 {
		t.Errorf("expected no missing deps for phpmyadmin, got %v", missing)
	}
}

func TestMissingPresetDependencies_CustomDepReportsMissing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	preset, err := config.LoadPreset("mongo-express")
	if err != nil {
		t.Fatalf("LoadPreset: %v", err)
	}
	svc, err := preset.Resolve("")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	missing := MissingPresetDependencies(svc)
	if len(missing) != 1 || missing[0] != "mongo" {
		t.Errorf("expected missing=[mongo], got %v", missing)
	}
}

func writeInstalledQuadlet(t *testing.T, unit string) {
	t.Helper()
	qdir := config.QuadletDir()
	if err := os.MkdirAll(qdir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(qdir, unit+".container"), []byte("[Container]\nImage=x\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func resolvePresetForTest(t *testing.T, name string) *config.CustomService {
	t.Helper()
	p, err := config.LoadPreset(name)
	if err != nil {
		t.Fatalf("LoadPreset(%s): %v", name, err)
	}
	svc, err := p.Resolve("")
	if err != nil {
		t.Fatalf("Resolve(%s): %v", name, err)
	}
	return svc
}

func TestMissingPresetDependencies_VersionedMemberSatisfiesFamily(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	// A versioned postgres alternate with no bare default postgres installed:
	// its YAML places it in the postgres family, its quadlet marks it installed.
	if err := config.SaveCustomService(&config.CustomService{
		Name: "postgres-17", Image: "docker.io/postgis/postgis:17-3.6-alpine", Family: "postgres",
	}); err != nil {
		t.Fatal(err)
	}
	writeInstalledQuadlet(t, "lerd-postgres-17")

	svc := resolvePresetForTest(t, "pgadmin")
	if missing := MissingPresetDependencies(svc); len(missing) != 0 {
		t.Errorf("postgres-17 should satisfy pgadmin's postgres dep, got missing=%v", missing)
	}
}

func TestMissingPresetDependencies_SiblingFamilySatisfiesDep(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	// MariaDB only, no mysql. phpmyadmin depends on mysql but discovers
	// mysql,mariadb, so an installed mariadb satisfies the mysql dependency.
	if err := config.SaveCustomService(&config.CustomService{
		Name: "mariadb-11", Image: "docker.io/library/mariadb:11", Family: "mariadb",
	}); err != nil {
		t.Fatal(err)
	}
	writeInstalledQuadlet(t, "lerd-mariadb-11")

	svc := resolvePresetForTest(t, "phpmyadmin")
	if missing := MissingPresetDependencies(svc); len(missing) != 0 {
		t.Errorf("mariadb should satisfy phpmyadmin's mysql dep, got missing=%v", missing)
	}
}

func TestMissingPresetDependencies_NoFamilyMemberStillMissing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	// No postgres-family member installed: the always-listed bare lerd-postgres
	// must not count as installed, so the dependency is still reported missing.
	svc := resolvePresetForTest(t, "pgadmin")
	missing := MissingPresetDependencies(svc)
	if len(missing) != 1 || missing[0] != "postgres" {
		t.Errorf("expected missing=[postgres] with nothing installed, got %v", missing)
	}
}

func TestMissingPresetDependencies_NonDiscoveringDepStaysStrict(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	// mongo-express depends on mongo but does not declare discover_family, so it
	// connects to the bare host: a versioned mongo alternate must NOT satisfy it.
	if err := config.SaveCustomService(&config.CustomService{
		Name: "mongo-7", Image: "docker.io/library/mongo:7", Family: "mongo",
	}); err != nil {
		t.Fatal(err)
	}
	writeInstalledQuadlet(t, "lerd-mongo-7")

	svc := resolvePresetForTest(t, "mongo-express")
	missing := MissingPresetDependencies(svc)
	if len(missing) != 1 || missing[0] != "mongo" {
		t.Errorf("non-discovering mongo-express must stay strict, got missing=%v", missing)
	}
}
