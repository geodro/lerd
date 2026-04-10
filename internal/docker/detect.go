package docker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Container represents a running Docker container.
type Container struct {
	ID    string
	Name  string
	Ports string
	Image string
}

// Volume represents a Docker volume.
type Volume struct {
	Name string
}

// DatabaseVolume is a Docker volume that likely contains database data.
type DatabaseVolume struct {
	Name   string
	DBType string // "mysql" or "postgres"
}

// DatabaseContainer is a running Docker container that runs a database engine.
type DatabaseContainer struct {
	Name   string
	Image  string
	DBType string // "mysql" or "postgres"
}

// IsDockerInstalled checks whether the docker binary is in PATH and whether it
// is real Docker or the podman-docker compatibility shim. Returns the binary
// path and true when it is genuine Docker Engine / Docker Desktop.
func IsDockerInstalled() (path string, isReal bool) {
	p, err := exec.LookPath("docker")
	if err != nil {
		return "", false
	}
	out, err := exec.Command(p, "--version").CombinedOutput()
	if err != nil {
		return p, false
	}
	ver := string(out)
	if strings.Contains(strings.ToLower(ver), "podman") {
		return p, false
	}
	return p, true
}

// IsDaemonRunning returns true when the Docker daemon appears to be active.
func IsDaemonRunning() bool {
	// Check well-known sockets.
	for _, sock := range dockerSockets() {
		if fi, err := os.Stat(sock); err == nil && fi.Mode()&os.ModeSocket != 0 {
			return true
		}
	}
	// Fall back to systemctl.
	out, err := exec.Command("systemctl", "is-active", "docker.service").Output()
	if err == nil && strings.TrimSpace(string(out)) == "active" {
		return true
	}
	return false
}

func dockerSockets() []string {
	socks := []string{"/var/run/docker.sock"}
	if home, err := os.UserHomeDir(); err == nil {
		socks = append(socks, filepath.Join(home, ".docker", "desktop", "docker.sock"))
	}
	return socks
}

// ListRunningContainers returns all running Docker containers. If the docker
// CLI is inaccessible (e.g. user not in docker group) it returns nil, nil.
func ListRunningContainers() ([]Container, error) {
	out, err := exec.Command("docker", "ps", "--format", "{{.ID}}\t{{.Names}}\t{{.Ports}}\t{{.Image}}").CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "permission denied") || strings.Contains(string(out), "connect") {
			return nil, nil
		}
		return nil, fmt.Errorf("docker ps: %s", strings.TrimSpace(string(out)))
	}
	return ParseContainers(string(out)), nil
}

// ParseContainers parses the tab-separated output of docker ps --format.
func ParseContainers(output string) []Container {
	var containers []Container
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) < 4 {
			continue
		}
		containers = append(containers, Container{
			ID:    parts[0],
			Name:  parts[1],
			Ports: parts[2],
			Image: parts[3],
		})
	}
	return containers
}

// lerdPorts lists host ports that lerd may need.
var lerdPorts = map[string]bool{
	"80": true, "443": true, "5300": true,
	"3306": true, "5432": true, "6379": true,
	"7700": true, "9000": true, "8025": true,
}

// portRe matches a host port in Docker's Ports field, e.g. "0.0.0.0:3306->3306/tcp".
var portRe = regexp.MustCompile(`(?:[\d.]+:)?(\d+)->`)

// ConflictingPorts returns a map of host-port -> container-name for ports that
// conflict with lerd services.
func ConflictingPorts(containers []Container) map[string]string {
	conflicts := map[string]string{}
	for _, c := range containers {
		for _, m := range portRe.FindAllStringSubmatch(c.Ports, -1) {
			if lerdPorts[m[1]] {
				conflicts[m[1]] = c.Name
			}
		}
	}
	return conflicts
}

// ListVolumes returns all Docker volumes.
func ListVolumes() ([]Volume, error) {
	out, err := exec.Command("docker", "volume", "ls", "--format", "{{.Name}}").CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "permission denied") || strings.Contains(string(out), "connect") {
			return nil, nil
		}
		return nil, fmt.Errorf("docker volume ls: %s", strings.TrimSpace(string(out)))
	}
	return ParseVolumes(string(out)), nil
}

// ParseVolumes parses the output of docker volume ls --format.
func ParseVolumes(output string) []Volume {
	var vols []Volume
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		vols = append(vols, Volume{Name: strings.TrimSpace(line)})
	}
	return vols
}

// dbVolumePatterns maps substrings to database types for volume name detection.
var dbVolumePatterns = []struct {
	substr string
	dbType string
}{
	{"mysql", "mysql"},
	{"mariadb", "mysql"},
	{"postgres", "postgres"},
	{"pgsql", "postgres"},
}

// FindDatabaseVolumes filters volumes whose names suggest database data.
func FindDatabaseVolumes(volumes []Volume) []DatabaseVolume {
	var out []DatabaseVolume
	for _, v := range volumes {
		lower := strings.ToLower(v.Name)
		for _, p := range dbVolumePatterns {
			if strings.Contains(lower, p.substr) {
				out = append(out, DatabaseVolume{Name: v.Name, DBType: p.dbType})
				break
			}
		}
	}
	return out
}

// dbImagePatterns maps image name substrings to database types.
var dbImagePatterns = []struct {
	substr string
	dbType string
}{
	{"mysql", "mysql"},
	{"mariadb", "mysql"},
	{"postgres", "postgres"},
}

// FindDatabaseContainers returns running containers that appear to be database engines.
func FindDatabaseContainers(containers []Container) []DatabaseContainer {
	var out []DatabaseContainer
	for _, c := range containers {
		lower := strings.ToLower(c.Image)
		for _, p := range dbImagePatterns {
			if strings.Contains(lower, p.substr) {
				out = append(out, DatabaseContainer{
					Name:   c.Name,
					Image:  c.Image,
					DBType: p.dbType,
				})
				break
			}
		}
	}
	return out
}

// ContainerEnv returns the environment variables of a Docker container as a map.
func ContainerEnv(name string) map[string]string {
	out, err := exec.Command("docker", "inspect",
		"--format", "{{range .Config.Env}}{{println .}}{{end}}", name).CombinedOutput()
	if err != nil {
		return nil
	}
	env := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			env[k] = v
		}
	}
	return env
}

// HasIPTablesDockerChain returns true when Docker's DOCKER iptables chain exists.
func HasIPTablesDockerChain() bool {
	err := exec.Command("iptables", "-L", "DOCKER").Run()
	return err == nil
}
