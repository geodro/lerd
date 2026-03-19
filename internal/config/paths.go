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

// CustomServicesDir returns the directory for custom service YAML files.
func CustomServicesDir() string {
	return filepath.Join(ConfigDir(), "services")
}
