package config

import (
	"os"
	"path/filepath"
	"runtime"
)

// DumpsTCPPort is the loopback port the dump receiver binds on darwin
// (where a unix socket on the macOS host can't be reached from FPM
// inside the podman-machine VM). Linux uses a unix socket instead so
// this is unused there. Picked to avoid Symfony var-dump-server's
// default :9912 — see internal/dumps.DefaultAddr.
const DumpsTCPPort = "9913"

func xdgConfigHome() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}

func xdgDataHome() string {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share")
}

// ConfigDir returns ~/.config/lerd/ (or $XDG_CONFIG_HOME/lerd/).
func ConfigDir() string {
	return filepath.Join(xdgConfigHome(), "lerd")
}

// DataDir returns ~/.local/share/lerd/ (or $XDG_DATA_HOME/lerd/).
func DataDir() string {
	return filepath.Join(xdgDataHome(), "lerd")
}

// BinDir returns the lerd bin directory.
func BinDir() string {
	return filepath.Join(DataDir(), "bin")
}

// NodeGlobalDir is the npm prefix lerd points its node shim at, so
// `npm install -g foo` lands in a stable per-user path instead of a
// version-specific fnm directory that nothing has on PATH.
func NodeGlobalDir() string {
	return filepath.Join(DataDir(), "node-global")
}

// NginxDir returns the nginx data directory.
func NginxDir() string {
	return filepath.Join(DataDir(), "nginx")
}

// NginxConfD returns the nginx conf.d directory.
func NginxConfD() string {
	return filepath.Join(NginxDir(), "conf.d")
}

// NginxCustomD holds user-authored nginx snippets included at the end of
// each per-site server block. Lerd never writes here, so edits survive
// vhost regeneration and `lerd update`.
func NginxCustomD() string {
	return filepath.Join(NginxDir(), "custom.d")
}

// CertsDir returns the certs directory.
func CertsDir() string {
	return filepath.Join(DataDir(), "certs")
}

// DataSubDir returns a named subdirectory under data.
func DataSubDir(name string) string {
	return filepath.Join(DataDir(), "data", name)
}

// BackupsDir returns the directory where migration dumps are stored so users
// can recover manually if an automated migration fails.
func BackupsDir() string {
	return filepath.Join(DataDir(), "backups")
}

// SnapshotsDir returns the directory where db:snapshot point-in-time database
// copies are stored, organised per service and database scope.
func SnapshotsDir() string {
	return filepath.Join(DataDir(), "snapshots")
}

// DnsmasqDir returns the dnsmasq config directory.
func DnsmasqDir() string {
	return filepath.Join(DataDir(), "dnsmasq")
}

// SitesFile returns the path to sites.yaml.
func SitesFile() string {
	return filepath.Join(DataDir(), "sites.yaml")
}

// GlobalConfigFile returns the path to config.yaml.
func GlobalConfigFile() string {
	return filepath.Join(ConfigDir(), "config.yaml")
}

// QuadletDir returns the Podman quadlet directory.
func QuadletDir() string {
	return filepath.Join(xdgConfigHome(), "containers", "systemd")
}

// SystemdUserDir returns the systemd user unit directory.
func SystemdUserDir() string {
	return filepath.Join(xdgConfigHome(), "systemd", "user")
}

// PHPImageHashFile returns the path to the stored PHP-FPM Containerfile hash.
func PHPImageHashFile() string {
	return filepath.Join(DataDir(), "php-image-hash")
}

// PHPConfFile returns the host path for the per-version xdebug ini file.
func PHPConfFile(version string) string {
	return filepath.Join(DataDir(), "php", version, "99-xdebug.ini")
}

// PHPUserIniFile returns the host path for the per-version user php.ini file.
func PHPUserIniFile(version string) string {
	return filepath.Join(DataDir(), "php", version, "98-user.ini")
}

// DumpsAssetsDir returns the host directory holding the version-agnostic dump
// bridge assets (PHP file + ini). Both files are bind-mounted read-only into
// every FPM container when `lerd dump on` is active. Single shared copy
// because the bridge is identical across PHP versions.
func DumpsAssetsDir() string {
	return filepath.Join(DataDir(), "php", "dumps")
}

// DumpsBridgeFile is the host path for dump-bridge.php (the auto-prepended
// PHP file).
func DumpsBridgeFile() string {
	return filepath.Join(DumpsAssetsDir(), "dump-bridge.php")
}

// DumpsIniFile is the host path for the conf.d ini that turns the bridge on.
func DumpsIniFile() string {
	return filepath.Join(DumpsAssetsDir(), "97-lerd-dump.ini")
}

// DumpsSocketPath is the Unix socket lerd-ui binds for dump payloads. Kept
// in RunDir so it sits alongside the UI socket and so the existing %h:%h
// volume in every FPM container surfaces it at the same path inside.
func DumpsSocketPath() string {
	return filepath.Join(RunDir(), "lerd-dumps.sock")
}

// DumpsEnabledFlagFile is the sentinel the dump bridge checks on every
// request. Present file = bridge captures dump()/dd() calls; absent file
// = bridge is a fast no-op. Toggling is a single touch/rm on this file
// so the FPM container never restarts.
func DumpsEnabledFlagFile() string {
	return filepath.Join(DumpsAssetsDir(), "enabled.flag")
}

// SpxAssetsDir returns the host directory holding the SPX profiler conf.d ini
// and the generated http key. The ini is bind-mounted read-only into every
// FPM container.
func SpxAssetsDir() string {
	return filepath.Join(DataDir(), "php", "spx")
}

// SpxIniFile is the host path for the SPX conf.d ini.
func SpxIniFile() string {
	return filepath.Join(SpxAssetsDir(), "zz-lerd-spx.ini")
}

// SpxKeyFile holds the generated SPX http key.
func SpxKeyFile() string {
	return filepath.Join(SpxAssetsDir(), "key")
}

// SpxDataDir is the host directory SPX writes profile reports into. Mounted
// read-write into every FPM container at /var/spx.
func SpxDataDir() string {
	return filepath.Join(DataDir(), "spx")
}

// DumpsListenNetwork reports the net.Listen network lerd-ui should bind
// for the dump receiver. On macOS we fall back to TCP because unix
// sockets don't traverse the podman-machine virtio-fs boundary as
// functional sockets (same constraint that drives EnsureLerdVhost's
// host.containers.internal:7073 fallback). On Linux the unix socket is
// reachable inside FPM via the %h:%h bind mount.
func DumpsListenNetwork() string {
	if runtime.GOOS == "darwin" {
		return "tcp"
	}
	return "unix"
}

// DumpsListenAddr is the address paired with DumpsListenNetwork.
func DumpsListenAddr() string {
	if runtime.GOOS == "darwin" {
		return "127.0.0.1:" + DumpsTCPPort
	}
	return DumpsSocketPath()
}

// DumpsBridgeTarget is the stream_socket_client target the PHP bridge
// reads from the conf.d ini. On macOS gvproxy forwards
// host.containers.internal:<port> from inside the podman-machine VM to
// the lerd-ui process on the host; on Linux the FPM container hits the
// host unix socket directly via the %h:%h bind mount.
func DumpsBridgeTarget() string {
	if runtime.GOOS == "darwin" {
		return "tcp://host.containers.internal:" + DumpsTCPPort
	}
	return "unix://" + DumpsSocketPath()
}

// CustomServicesDir returns the directory for custom service YAML files.
func CustomServicesDir() string {
	return filepath.Join(ConfigDir(), "services")
}

// ServiceFilesDir returns the directory holding rendered FileMount content
// for the named custom service. Each file is bind-mounted into the container
// at its declared target path.
func ServiceFilesDir(name string) string {
	return filepath.Join(DataDir(), "service-files", name)
}

// FrameworksDir returns the directory for user-defined framework YAML files.
func FrameworksDir() string {
	return filepath.Join(ConfigDir(), "frameworks")
}

// StoreFrameworksDir returns the directory for store-installed framework YAML files.
func StoreFrameworksDir() string {
	return filepath.Join(DataDir(), "frameworks")
}

// UpdateCheckFile returns the path to the cached update-check state file.
func UpdateCheckFile() string {
	return filepath.Join(DataDir(), "update-check.json")
}

// BackupBinaryFile returns the path to the backup lerd binary used for rollback.
func BackupBinaryFile() string {
	return filepath.Join(DataDir(), "lerd.bak")
}

// BackupTrayFile returns the path to the backup lerd-tray binary used for rollback.
func BackupTrayFile() string {
	return filepath.Join(DataDir(), "lerd-tray.bak")
}

// BackupVersionFile returns the path to the file storing the pre-update version string.
func BackupVersionFile() string {
	return filepath.Join(DataDir(), "rollback-version")
}

// PausedDir returns the directory where paused-site landing page HTML files are stored.
func PausedDir() string {
	return filepath.Join(DataDir(), "paused")
}

// ErrorPagesDir returns the directory where nginx error page HTML files are stored.
func ErrorPagesDir() string {
	return filepath.Join(DataDir(), "error-pages")
}

// RunDir returns the directory for runtime sockets shared between lerd-ui
// (host process) and lerd-nginx (container). Bind-mounted into lerd-nginx so
// the lerd.localhost vhost can reach lerd-ui without depending on container
// → host TCP routing (host.containers.internal / 169.254.1.2), which is
// unreliable across podman/netavark/pasta versions and host network changes.
func RunDir() string {
	return filepath.Join(DataDir(), "run")
}

// UISocketPath returns the path to the lerd-ui unix domain socket.
func UISocketPath() string {
	return filepath.Join(RunDir(), "lerd-ui.sock")
}

// ContainerHostsFile returns the path to the shared hosts file mounted into PHP containers.
func ContainerHostsFile() string {
	return filepath.Join(DataDir(), "hosts")
}

// BrowserHostsFile returns the path to the hosts file for browser testing
// containers (e.g. Selenium). It maps .test domains to the nginx container's
// IP so that Chromium inside the container can reach lerd sites directly over
// the Podman network instead of going through the host gateway.
func BrowserHostsFile() string {
	return filepath.Join(DataDir(), "browser-hosts")
}
