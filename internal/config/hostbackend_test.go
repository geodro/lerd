package config

import "testing"

func TestHostBackendFor(t *testing.T) {
	cases := []struct {
		family      string
		ok          bool
		port        int
		connKey     string
		socketIsDir bool
		container   string
	}{
		{"mysql", true, 3306, "mysql", false, "lerd-mysql"},
		{"mariadb", true, 3306, "mysql", false, "lerd-mariadb"},
		{"postgres", true, 5432, "pgsql", true, "lerd-postgres"},
		{"redis", false, 0, "", false, ""},
		{"mongo", false, 0, "", false, ""},
		{"", false, 0, "", false, ""},
	}
	for _, c := range cases {
		spec, ok := HostBackendFor(c.family)
		if ok != c.ok {
			t.Errorf("HostBackendFor(%q) ok = %v, want %v", c.family, ok, c.ok)
			continue
		}
		if !ok {
			continue
		}
		if spec.DefaultPort != c.port || spec.ConnEnvKey != c.connKey || spec.SocketIsDir != c.socketIsDir || spec.ContainerName != c.container {
			t.Errorf("HostBackendFor(%q) = %+v, want port=%d conn=%q dir=%v container=%q",
				c.family, spec, c.port, c.connKey, c.socketIsDir, c.container)
		}
	}
}

func TestHostBackendSpec_HostSocketPath_OSAware(t *testing.T) {
	defer SetHostDBGOOSForTest("linux")()
	my, _ := HostBackendFor("mysql")
	pg, _ := HostBackendFor("postgres")

	if got := my.HostSocketPath(); got != DefaultHostMySQLSocket {
		t.Errorf("mysql linux socket = %q, want %q", got, DefaultHostMySQLSocket)
	}
	if got := pg.HostSocketPath(); got != "/var/run/postgresql" {
		t.Errorf("postgres linux socket = %q, want /var/run/postgresql", got)
	}

	SetHostDBGOOSForTest("darwin")
	if got := my.HostSocketPath(); got != defaultHostMySQLSocketDarwin {
		t.Errorf("mysql darwin socket = %q, want %q", got, defaultHostMySQLSocketDarwin)
	}
	if got := pg.HostSocketPath(); got != "/tmp" {
		t.Errorf("postgres darwin socket = %q, want /tmp", got)
	}
}

func TestHostBackendForProject(t *testing.T) {
	// Unspecified DB ⇒ MySQL (db.external originated as the host-MySQL feature).
	if spec, ok := HostBackendForProject(nil); !ok || spec.Family != "mysql" {
		t.Errorf("nil project: spec=%+v ok=%v, want mysql", spec, ok)
	}
	if spec, ok := HostBackendForProject(&ProjectConfig{}); !ok || spec.Family != "mysql" {
		t.Errorf("empty project: spec=%+v ok=%v, want mysql", spec, ok)
	}
	// db.service drives the family.
	if spec, ok := HostBackendForProject(&ProjectConfig{DB: ProjectDB{Service: "postgres"}}); !ok || spec.Family != "postgres" {
		t.Errorf("postgres project: spec=%+v ok=%v, want postgres", spec, ok)
	}
	// Falls back to the services list when db.service is unset.
	if spec, ok := HostBackendForProject(&ProjectConfig{Services: []ProjectService{{Name: "postgres"}}}); !ok || spec.Family != "postgres" {
		t.Errorf("services=[postgres]: spec=%+v ok=%v, want postgres", spec, ok)
	}
	// A non-host-capable DB family has no spec.
	if _, ok := HostBackendForProject(&ProjectConfig{DB: ProjectDB{Service: "redis"}}); ok {
		t.Error("redis project should have no host backend spec")
	}
}

func TestDBFamiliesShareContainer(t *testing.T) {
	cases := []struct {
		serviceFam, projFam string
		want                bool
	}{
		{"mysql", "mysql", true},
		{"mysql", "mariadb", true}, // mariadb shares the MySQL container family
		{"mysql", "", true},        // unspecified project DB ⇒ MySQL
		{"mariadb", "mysql", true},
		{"postgres", "postgres", true},
		{"mysql", "postgres", false}, // cross-family: don't exclude
		{"postgres", "mysql", false},
		{"redis", "mysql", false}, // service has no host backend
	}
	for _, c := range cases {
		if got := dbFamiliesShareContainer(c.serviceFam, c.projFam); got != c.want {
			t.Errorf("dbFamiliesShareContainer(%q,%q) = %v, want %v", c.serviceFam, c.projFam, got, c.want)
		}
	}
}
