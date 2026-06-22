package cli

import (
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func TestApplyHostDBExternalEnv_emitsSocketAndMarksExternal(t *testing.T) {
	defer config.SetHostDBGOOSForTest("linux")() // socket transport is the Linux path
	proj := &config.ProjectConfig{
		DB:       config.ProjectDB{External: true, Service: "mysql"},
		Services: []config.ProjectService{{Name: "mysql"}},
	}
	envMap := map[string]string{"DB_USERNAME": "lerd", "DB_PASSWORD": "secret"}
	extServices := map[string]bool{}
	envOverrides := map[string]string{}

	if !applyHostDBExternalEnv(proj, envMap, extServices, envOverrides) {
		t.Fatal("expected applied=true for a db.external mysql project")
	}
	// Marked external so runEnv skips ensureServiceRunning (no lerd-mysql container).
	if !extServices["mysql"] {
		t.Error(`extServices["mysql"] should be true so lerd-mysql is not started`)
	}
	// Connection vars layered into envOverrides (which win over def.Vars).
	if got := envOverrides["DB_HOST"]; got != "localhost" {
		t.Errorf("DB_HOST = %q, want localhost", got)
	}
	if got := envOverrides["DB_SOCKET"]; got != config.DefaultHostMySQLSocket {
		t.Errorf("DB_SOCKET = %q, want %q", got, config.DefaultHostMySQLSocket)
	}
	// DB_PORT must be present-and-empty (cleared), not absent.
	if v, ok := envOverrides["DB_PORT"]; !ok || v != "" {
		t.Errorf("DB_PORT override = %q present=%v, want present and empty", v, ok)
	}
	// Real host credentials preserved, not clobbered by container defaults.
	if envOverrides["DB_USERNAME"] != "lerd" || envOverrides["DB_PASSWORD"] != "secret" {
		t.Errorf("host creds not preserved: user=%q pass=%q", envOverrides["DB_USERNAME"], envOverrides["DB_PASSWORD"])
	}
}

func TestApplyHostDBExternalEnv_customSocket(t *testing.T) {
	defer config.SetHostDBGOOSForTest("linux")() // socket transport is the Linux path
	proj := &config.ProjectConfig{DB: config.ProjectDB{External: true, Socket: "/tmp/mysqld.sock"}}
	envOverrides := map[string]string{}
	if !applyHostDBExternalEnv(proj, map[string]string{}, map[string]bool{}, envOverrides) {
		t.Fatal("expected applied=true")
	}
	if got := envOverrides["DB_SOCKET"]; got != "/tmp/mysqld.sock" {
		t.Errorf("DB_SOCKET = %q, want /tmp/mysqld.sock", got)
	}
}

func TestApplyHostDBExternalEnv_macOSEmitsTCP(t *testing.T) {
	defer config.SetHostDBGOOSForTest("darwin")()
	// db.socket is set but must be ignored on macOS — the host socket can't be
	// reached from inside the podman-machine VM, so the connection goes over TCP.
	proj := &config.ProjectConfig{
		DB:       config.ProjectDB{External: true, Service: "mysql", Socket: "/tmp/ignored.sock"},
		Services: []config.ProjectService{{Name: "mysql"}},
	}
	extServices := map[string]bool{}
	envOverrides := map[string]string{}

	if !applyHostDBExternalEnv(proj, map[string]string{}, extServices, envOverrides) {
		t.Fatal("expected applied=true for a db.external mysql project")
	}
	if !extServices["mysql"] {
		t.Error(`extServices["mysql"] should be true so lerd-mysql is not started`)
	}
	if got := envOverrides["DB_HOST"]; got != config.HostDBTCPHost {
		t.Errorf("DB_HOST = %q, want %q (gvproxy host alias)", got, config.HostDBTCPHost)
	}
	if got := envOverrides["DB_PORT"]; got != config.HostDBTCPPort {
		t.Errorf("DB_PORT = %q, want %q", got, config.HostDBTCPPort)
	}
	// DB_SOCKET must be present-and-empty (cleared) so a stale socket can't win.
	if v, ok := envOverrides["DB_SOCKET"]; !ok || v != "" {
		t.Errorf("DB_SOCKET override = %q present=%v, want present and empty", v, ok)
	}
}

func TestApplyHostDBExternalEnv_noopWhenNotExternal(t *testing.T) {
	proj := &config.ProjectConfig{DB: config.ProjectDB{Service: "mysql"}} // External is false
	extServices := map[string]bool{}
	envOverrides := map[string]string{}
	if applyHostDBExternalEnv(proj, map[string]string{}, extServices, envOverrides) {
		t.Fatal("expected applied=false when db.external is unset")
	}
	if len(extServices) != 0 || len(envOverrides) != 0 {
		t.Errorf("maps must be untouched when not external: ext=%v overrides=%v", extServices, envOverrides)
	}
}

func TestApplyHostDBExternalEnv_skipsNonMySQLFamily(t *testing.T) {
	proj := &config.ProjectConfig{
		DB:       config.ProjectDB{External: true, Service: "postgres"},
		Services: []config.ProjectService{{Name: "postgres"}},
	}
	if applyHostDBExternalEnv(proj, map[string]string{}, map[string]bool{}, map[string]string{}) {
		t.Fatal("expected applied=false for postgres — host-socket mode is MySQL/MariaDB-only in milestone 1")
	}
}

func TestApplyHostDBExternalEnv_doesNotInventAbsentCreds(t *testing.T) {
	// When .env carries no DB_USERNAME/DB_PASSWORD, the helper must not set them,
	// so the framework defaults still apply rather than empty overrides.
	proj := &config.ProjectConfig{DB: config.ProjectDB{External: true, Service: "mysql"}}
	envOverrides := map[string]string{}
	applyHostDBExternalEnv(proj, map[string]string{}, map[string]bool{}, envOverrides)
	if _, ok := envOverrides["DB_USERNAME"]; ok {
		t.Error("DB_USERNAME should be absent when not present in .env")
	}
	if _, ok := envOverrides["DB_PASSWORD"]; ok {
		t.Error("DB_PASSWORD should be absent when not present in .env")
	}
}

func TestApplyHostDBExternalEnv_nilProject(t *testing.T) {
	if applyHostDBExternalEnv(nil, map[string]string{}, map[string]bool{}, map[string]string{}) {
		t.Fatal("expected applied=false for a nil project (no panic)")
	}
}
