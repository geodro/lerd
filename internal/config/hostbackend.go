package config

// HostBackendSpec describes how lerd reaches a host-installed (system) instance
// of a database engine family in host mode (db.external). These are fixed
// connection mechanics of each engine — the default socket location, whether
// that socket is a FILE (MySQL) or a DIRECTORY (Postgres), the framework env-var
// contract, and the canonical TCP port — not user-tunable preset knobs, so they
// live in code rather than the preset YAML. Host mode's probe, FPM socket mount,
// env emission, and dashboard all read from one of these specs, so the feature
// generalises across engines instead of hardcoding MySQL/3306. A family without
// a spec has no host backend.
type HostBackendSpec struct {
	// Family is the canonical database family this spec applies to.
	Family string
	// Display is the human-readable engine name for CLI/log messages ("MySQL",
	// "MariaDB", "PostgreSQL").
	Display string
	// DefaultPort is the engine's canonical TCP port (mysql 3306, postgres 5432):
	// the port a host server listens on, and the macOS TCP target via gvproxy.
	DefaultPort int
	// LinuxSocket is the default unix socket a host server listens on for local
	// connections on Debian/Ubuntu. For MySQL this is a socket FILE; for Postgres
	// it is the socket DIRECTORY that holds .s.PGSQL.<port> (see SocketIsDir).
	LinuxSocket string
	// LinuxSocketFallbacks are alternative socket locations probed when
	// LinuxSocket is absent (older distros / packaging differences).
	LinuxSocketFallbacks []string
	// LinuxInstallMarkers are directories created by this engine's SERVER package
	// (its data/cluster dir), used as a liveness-INDEPENDENT "a host server is
	// installed" signal so the port-ownership guard avoids the engine default port
	// even when the host server is stopped at the moment lerd writes the quadlet.
	// Unlike the shared socket DIRECTORY, these are not created by client/common
	// packages (e.g. postgresql-common, pulled in by pgbouncer or *-server-dev), so
	// they don't false-positive on a server-less box.
	LinuxInstallMarkers []string
	// DarwinSocket is the conventional Homebrew socket location on macOS. macOS
	// host mode actually connects over TCP (a unix socket can't cross the
	// podman-machine boundary), so this is used only for probing/display.
	DarwinSocket string
	// SocketIsDir reports whether the socket path is a DIRECTORY (Postgres, which
	// keeps .s.PGSQL.<port> inside it) rather than a socket FILE (MySQL). It
	// drives both the FPM bind-mount (mount the directory itself vs the file's
	// parent) and the env contract (Postgres passes the directory via DB_HOST).
	SocketIsDir bool
	// ConnEnvKey is the DB_CONNECTION value frameworks expect: "mysql" (MySQL and
	// MariaDB share the MySQL driver) or "pgsql" (Postgres).
	ConnEnvKey string
	// ContainerName is lerd's own container for this family, used by the host
	// probe to tell lerd's server apart from a host server on the same port.
	ContainerName string
}

// legacyMySQLSocketPath is the older /var/run location some distros still
// symlink the MySQL socket to; probed as a fallback when /run is absent.
const legacyMySQLSocketPath = "/var/run/mysqld/mysqld.sock"

// hostBackendSpecs maps a database family to how lerd reaches a host-installed
// instance of it. MySQL and MariaDB share the same wire protocol, socket, and
// port, so both point at the MySQL conventions (differing only in container
// name). Adding a new host-capable engine is a single entry here.
var hostBackendSpecs = map[string]HostBackendSpec{
	"mysql": {
		Family:               "mysql",
		Display:              "MySQL",
		DefaultPort:          3306,
		LinuxSocket:          DefaultHostMySQLSocket,
		LinuxSocketFallbacks: []string{legacyMySQLSocketPath},
		LinuxInstallMarkers:  []string{"/var/lib/mysql"},
		DarwinSocket:         defaultHostMySQLSocketDarwin,
		SocketIsDir:          false,
		ConnEnvKey:           "mysql",
		ContainerName:        "lerd-mysql",
	},
	"mariadb": {
		Family:               "mariadb",
		Display:              "MariaDB",
		DefaultPort:          3306,
		LinuxSocket:          DefaultHostMySQLSocket,
		LinuxSocketFallbacks: []string{legacyMySQLSocketPath},
		LinuxInstallMarkers:  []string{"/var/lib/mysql"},
		DarwinSocket:         defaultHostMySQLSocketDarwin,
		SocketIsDir:          false,
		ConnEnvKey:           "mysql",
		ContainerName:        "lerd-mariadb",
	},
	"postgres": {
		Family:               "postgres",
		Display:              "PostgreSQL",
		DefaultPort:          5432,
		LinuxSocket:          "/var/run/postgresql",
		LinuxSocketFallbacks: []string{"/run/postgresql"},
		LinuxInstallMarkers:  []string{"/etc/postgresql", "/var/lib/pgsql"},
		DarwinSocket:         "/tmp",
		SocketIsDir:          true,
		ConnEnvKey:           "pgsql",
		ContainerName:        "lerd-postgres",
	},
}

// HostBackendFor returns the host-backend spec for a database family (as
// produced by FamilyOfName) and whether one exists. Families without a spec
// (redis, mongo, sqlite, …) have no host backend.
func HostBackendFor(family string) (HostBackendSpec, bool) {
	spec, ok := hostBackendSpecs[family]
	return spec, ok
}

// HostBackendForProject resolves the host-backend spec for a project's database
// family. A nil config or a project with no explicit database resolves to MySQL,
// since db.external originated as the host-MySQL feature and an unspecified DB is
// treated as MySQL.
func HostBackendForProject(c *ProjectConfig) (HostBackendSpec, bool) {
	fam := c.DBFamily()
	if fam == "" {
		fam = "mysql"
	}
	return HostBackendFor(fam)
}

// HostSocketPath returns this engine's default host socket path for the current
// OS: the Linux socket (a file for MySQL, a directory for Postgres) or the
// Homebrew/Darwin location.
func (s HostBackendSpec) HostSocketPath() string {
	if hostDBGOOS == "darwin" {
		return s.DarwinSocket
	}
	return s.LinuxSocket
}

// DBFamily resolves the project's database family ("mysql", "postgres", …) from
// db.service, or failing that the first database service in the services list.
// Returns "" when the project specifies no database.
func (c *ProjectConfig) DBFamily() string {
	if c == nil {
		return ""
	}
	if c.DB.Service != "" {
		return FamilyOfName(c.DB.Service)
	}
	for _, s := range c.Services {
		if IsDBServiceName(s.Name) {
			return FamilyOfName(s.Name)
		}
	}
	return ""
}

// IsHostBackendCapableDB reports whether the project's database family supports
// the host (db.external) backend — i.e. lerd knows how to reach a host-installed
// instance of it (MySQL, MariaDB, Postgres). A nil receiver or an unspecified
// database counts as capable, since db.external originated as the host-MySQL
// feature and an unspecified DB is treated as MySQL. Shared by `lerd env`, the
// dashboard's backend switch, and the auto-stop accounting.
func (c *ProjectConfig) IsHostBackendCapableDB() bool {
	fam := c.DBFamily()
	if fam == "" {
		return true
	}
	_, ok := HostBackendFor(fam)
	return ok
}

// HostDBSocketPath returns the host database socket to use in external mode — a
// socket FILE for MySQL/MariaDB or the socket DIRECTORY for Postgres — resolved
// from the explicit db.socket override or, failing that, the OS-aware default
// for the project's database family. Unlike ProjectDB.HostSocketPath this sees
// the full config, so it resolves the family from the services list when
// db.service is unset.
func (c *ProjectConfig) HostDBSocketPath() string {
	if c != nil && c.DB.Socket != "" {
		return c.DB.Socket
	}
	if spec, ok := HostBackendForProject(c); ok {
		return spec.HostSocketPath()
	}
	return DefaultHostDBSocketPath()
}

// dbFamiliesShareContainer reports whether a host-backed site whose database
// family is projFam should be treated as "not using" the container service of
// family serviceFam, for auto-stop accounting. MySQL and MariaDB collapse to one
// MySQL family here because they share the containerized server's role; an
// unspecified project DB is treated as MySQL. Returns false when serviceFam has
// no host backend (so non-DB services are never excluded by this rule).
func dbFamiliesShareContainer(serviceFam, projFam string) bool {
	if _, ok := HostBackendFor(serviceFam); !ok {
		return false
	}
	norm := func(f string) string {
		if f == "" || f == "mariadb" {
			return "mysql"
		}
		return f
	}
	return norm(serviceFam) == norm(projFam)
}
