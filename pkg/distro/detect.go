package distro

import (
	"bufio"
	"os"
	"strings"
)

// Distro holds information about the current Linux distribution.
type Distro struct {
	ID         string
	IDLike     string
	PrettyName string
}

// Detect parses /etc/os-release and returns the distro information.
func Detect() (*Distro, error) {
	return detectFromPath("/etc/os-release")
}

func detectFromPath(path string) (*Distro, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	d := &Distro{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.Trim(strings.TrimSpace(parts[1]), `"`)
		switch key {
		case "ID":
			d.ID = val
		case "ID_LIKE":
			d.IDLike = val
		case "PRETTY_NAME":
			d.PrettyName = val
		}
	}
	return d, scanner.Err()
}

// IsArch returns true if the distro is Arch Linux or Arch-based.
func (d *Distro) IsArch() bool {
	return d.ID == "arch" || strings.Contains(d.IDLike, "arch")
}

// IsDebian returns true if the distro is Debian or Debian-based.
func (d *Distro) IsDebian() bool {
	return d.ID == "debian" || d.ID == "ubuntu" || strings.Contains(d.IDLike, "debian")
}

// IsFedora returns true if the distro is Fedora or Fedora-based.
func (d *Distro) IsFedora() bool {
	return d.ID == "fedora" || d.ID == "rhel" || d.ID == "centos" || strings.Contains(d.IDLike, "fedora") || strings.Contains(d.IDLike, "rhel")
}
