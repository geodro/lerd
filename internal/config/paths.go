package config

import (
	"os"
	"path/filepath"
)

func xdgConfigHome() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}

func xdgCacheHome() string {
	if v := os.Getenv("XDG_CACHE_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache")
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

// NginxDir returns the nginx data directory.
func NginxDir() string {
	return filepath.Join(DataDir(), "nginx")
}

// NginxConfD returns the nginx conf.d directory.
func NginxConfD() string {
	return filepath.Join(NginxDir(), "conf.d")
}

// CertsDir returns the certs directory.
func CertsDir() string {
	return filepath.Join(DataDir(), "certs")
}

// DataSubDir returns a named subdirectory under data.
func DataSubDir(name string) string {
	return filepath.Join(DataDir(), "data", name)
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

// StoreCacheDir returns the directory for cached framework store data.
func StoreCacheDir() string {
	return filepath.Join(xdgCacheHome(), "lerd", "store")
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
