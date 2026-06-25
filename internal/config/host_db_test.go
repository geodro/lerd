package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSitesUsingService_HostBackendExcluded(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	mk := func(svc []string, external bool) string {
		dir := t.TempDir()
		var services []ProjectService
		for _, s := range svc {
			services = append(services, ProjectService{Name: s})
		}
		if err := SaveProjectConfig(dir, &ProjectConfig{Services: services, DB: ProjectDB{External: external}}); err != nil {
			t.Fatal(err)
		}
		return dir
	}

	containerMysql := mk([]string{"mysql"}, false) // counts for mysql
	hostMysql := mk([]string{"mysql"}, true)       // host backend → not counted
	hostMysqlContainerPg := mk([]string{"mysql", "postgres"}, true)
	pausedMysql := mk([]string{"mysql"}, false)

	reg := &SiteRegistry{Sites: []Site{
		{Name: "c", Path: containerMysql},
		{Name: "h", Path: hostMysql},
		{Name: "hp", Path: hostMysqlContainerPg},
		{Name: "p", Path: pausedMysql, Paused: true},
	}}
	if err := SaveSites(reg); err != nil {
		t.Fatal(err)
	}

	// Only the container-mode site counts for mysql; host-backend and paused are excluded.
	if got := SitesUsingService("mysql"); len(got) != 1 || got[0].Path != containerMysql {
		t.Errorf("SitesUsingService(mysql) = %v, want exactly the container-mode site %q", got, containerMysql)
	}
	// A host-MySQL site that also runs a container Postgres still counts for postgres.
	if got := SitesUsingService("postgres"); len(got) != 1 || got[0].Path != hostMysqlContainerPg {
		t.Errorf("SitesUsingService(postgres) = %v, want exactly %q", got, hostMysqlContainerPg)
	}
}

func TestProjectDB_HostSocketPath(t *testing.T) {
	// Restore the real OS at the end (even on a fatal).
	defer SetHostDBGOOSForTest("linux")()

	// The unset default is OS-aware: Debian/Ubuntu on Linux, Homebrew on macOS.
	if got := (ProjectDB{}).HostSocketPath(); got != DefaultHostMySQLSocket {
		t.Fatalf("linux default HostSocketPath = %q, want %q", got, DefaultHostMySQLSocket)
	}
	// Postgres resolves to its socket DIRECTORY, not a socket file.
	if got := (ProjectDB{Service: "postgres"}).HostSocketPath(); got != "/var/run/postgresql" {
		t.Fatalf("postgres HostSocketPath = %q, want /var/run/postgresql", got)
	}
	SetHostDBGOOSForTest("darwin")
	if got := (ProjectDB{}).HostSocketPath(); got != defaultHostMySQLSocketDarwin {
		t.Fatalf("darwin default HostSocketPath = %q, want %q", got, defaultHostMySQLSocketDarwin)
	}
	// An explicit socket wins on any OS.
	custom := "/var/run/mysqld/mysqld.sock"
	if got := (ProjectDB{Socket: custom}).HostSocketPath(); got != custom {
		t.Fatalf("custom HostSocketPath = %q, want %q", got, custom)
	}
}

func TestHostDBTransport_OSAware(t *testing.T) {
	defer SetHostDBGOOSForTest("linux")()

	if HostDBUsesTCP() {
		t.Error("Linux should reach the host DB over a unix socket, not TCP")
	}
	if got := DefaultHostDBSocketPath(); got != DefaultHostMySQLSocket {
		t.Errorf("linux default socket = %q, want %q", got, DefaultHostMySQLSocket)
	}

	SetHostDBGOOSForTest("darwin")
	if !HostDBUsesTCP() {
		t.Error("macOS should reach the host DB over TCP (sockets don't cross the podman-machine boundary)")
	}
	if got := DefaultHostDBSocketPath(); got != defaultHostMySQLSocketDarwin {
		t.Errorf("darwin default socket = %q, want %q", got, defaultHostMySQLSocketDarwin)
	}
}

func TestIsEmpty_HostDBExternal(t *testing.T) {
	if !(&ProjectConfig{}).IsEmpty() {
		t.Fatalf("zero ProjectConfig should be empty")
	}
	if (&ProjectConfig{DB: ProjectDB{External: true}}).IsEmpty() {
		t.Fatalf("config carrying only db.external should NOT be empty (else SaveProjectConfig-on-empty paths would drop it)")
	}
	if (&ProjectConfig{DB: ProjectDB{Socket: "/run/mysqld/mysqld.sock"}}).IsEmpty() {
		t.Fatalf("config carrying only db.socket should NOT be empty")
	}
}

func TestGlobalConfig_DefaultDBBackend(t *testing.T) {
	// Nil receiver and the zero value both mean container (lerd's own MySQL).
	if got := (*GlobalConfig)(nil).DefaultDBBackend(); got != DBBackendContainer {
		t.Errorf("nil receiver = %q, want %q", got, DBBackendContainer)
	}
	if got := (&GlobalConfig{}).DefaultDBBackend(); got != DBBackendContainer {
		t.Errorf("zero value = %q, want %q", got, DBBackendContainer)
	}
	host := &GlobalConfig{}
	host.Database.DefaultBackend = DBBackendHost
	if got := host.DefaultDBBackend(); got != DBBackendHost {
		t.Errorf("host configured = %q, want %q", got, DBBackendHost)
	}
	// An unrecognised value normalises back to container rather than leaking out.
	junk := &GlobalConfig{}
	junk.Database.DefaultBackend = "nonsense"
	if got := junk.DefaultDBBackend(); got != DBBackendContainer {
		t.Errorf("junk value = %q, want %q", got, DBBackendContainer)
	}
}

func TestProjectConfig_IsHostBackendCapableDB(t *testing.T) {
	// Nil and unspecified DB both assume MySQL family (db.external originated as
	// the host-MySQL feature; an unset DB shouldn't block enabling it).
	if !(*ProjectConfig)(nil).IsHostBackendCapableDB() {
		t.Error("nil receiver should be host-backend capable (assumed MySQL)")
	}
	if !(&ProjectConfig{}).IsHostBackendCapableDB() {
		t.Error("empty config should be host-backend capable (assumed MySQL)")
	}
	// db.service wins over the services list.
	if !(&ProjectConfig{DB: ProjectDB{Service: "mariadb"}}).IsHostBackendCapableDB() {
		t.Error("db.service=mariadb should be host-backend capable")
	}
	// Postgres is now host-backend capable (no longer MySQL-only).
	if !(&ProjectConfig{DB: ProjectDB{Service: "postgres"}}).IsHostBackendCapableDB() {
		t.Error("db.service=postgres should be host-backend capable")
	}
	// Families with no host backend (redis, mongo, …) are not capable.
	if (&ProjectConfig{DB: ProjectDB{Service: "redis"}}).IsHostBackendCapableDB() {
		t.Error("db.service=redis should NOT be host-backend capable")
	}
	// Falls back to the services list when db.service is unset.
	if !(&ProjectConfig{Services: []ProjectService{{Name: "mysql"}}}).IsHostBackendCapableDB() {
		t.Error("services=[mysql] should be host-backend capable")
	}
	if !(&ProjectConfig{Services: []ProjectService{{Name: "postgres"}}}).IsHostBackendCapableDB() {
		t.Error("services=[postgres] should be host-backend capable")
	}
}

func TestSetProjectDBExternal_RoundTrip(t *testing.T) {
	dir := t.TempDir()

	// Enable with the default socket.
	if err := SetProjectDBExternal(dir, true, ""); err != nil {
		t.Fatalf("enable: %v", err)
	}
	cfg, err := LoadProjectConfig(dir)
	if err != nil {
		t.Fatalf("load after enable: %v", err)
	}
	if !cfg.DB.External {
		t.Fatalf("External = false after enable, want true")
	}
	if cfg.DB.Socket != "" {
		t.Fatalf("Socket = %q after default enable, want empty (falls back to default)", cfg.DB.Socket)
	}
	if got := cfg.DB.HostSocketPath(); got != DefaultHostDBSocketPath() {
		t.Fatalf("HostSocketPath = %q, want OS default %q", got, DefaultHostDBSocketPath())
	}

	// Persisted file carries the committed marker.
	raw, err := os.ReadFile(filepath.Join(dir, ".lerd.yaml"))
	if err != nil {
		t.Fatalf("read .lerd.yaml: %v", err)
	}
	if !strings.Contains(string(raw), "external: true") {
		t.Fatalf(".lerd.yaml missing the external marker:\n%s", raw)
	}

	// Custom socket overrides the default.
	custom := "/tmp/mysqld.sock"
	if err := SetProjectDBExternal(dir, true, custom); err != nil {
		t.Fatalf("enable custom: %v", err)
	}
	cfg, _ = LoadProjectConfig(dir)
	if cfg.DB.Socket != custom {
		t.Fatalf("Socket = %q, want %q", cfg.DB.Socket, custom)
	}
	if got := cfg.DB.HostSocketPath(); got != custom {
		t.Fatalf("HostSocketPath = %q, want %q", got, custom)
	}

	// Disable clears both fields.
	if err := SetProjectDBExternal(dir, false, ""); err != nil {
		t.Fatalf("disable: %v", err)
	}
	cfg, _ = LoadProjectConfig(dir)
	if cfg.DB.External || cfg.DB.Socket != "" {
		t.Fatalf("after disable: External=%v Socket=%q, want false/empty", cfg.DB.External, cfg.DB.Socket)
	}
}
