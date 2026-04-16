package podman

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
)

// Fallback for podman rootless + pasta/netavark/slirp4netns when no other
// candidate works. Written so the file is well-formed even if every probe
// fails — Xdebug still won't connect, but `lerd doctor` will surface why.
const fallbackHostGatewayIP = "169.254.1.2"

// hostProbePort is a port that lerd-ui binds on the host (TCP 0.0.0.0:7073).
// The reachability probe checks whether candidate host IPs are routable from
// inside lerd-nginx by opening a TCP connection here. Any host service the
// containers can reach would do; lerd-ui is convenient because it's the only
// host-side TCP listener lerd guarantees.
const hostProbePort = "7073"

// WriteContainerHosts writes the shared /etc/hosts bind-mounted into every
// PHP-FPM container. host.containers.internal uses an IP that has been
// verified reachable from inside lerd-nginx; .test domains point at
// lerd-nginx directly on the lerd bridge network.
func WriteContainerHosts() error {
	reg, err := config.LoadSites()
	if err != nil {
		return fmt.Errorf("loading sites: %w", err)
	}

	hostIP := DetectHostGatewayIP()
	nginxIP := nginxContainerIP()

	content := renderContainerHosts(reg, hostIP, nginxIP)
	hostsPath := config.ContainerHostsFile()
	if err := os.MkdirAll(filepath.Dir(hostsPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(hostsPath, []byte(content), 0644); err != nil {
		return err
	}

	// Write the browser-testing variant: same domains but resolved to
	// lerd-nginx's IP on the Podman network so Chromium inside Selenium
	// (or similar containers) can reach sites via HTTP/HTTPS.
	return writeBrowserHosts(reg)
}

// renderContainerHosts builds the /etc/hosts contents for PHP-FPM containers.
// .test domains go to nginxIP (direct bridge), host.containers.internal to
// hostIP (host gateway for Xdebug and other host-side services).
func renderContainerHosts(reg *config.SiteRegistry, hostIP, nginxIP string) string {
	var sb strings.Builder
	sb.WriteString("127.0.0.1 localhost\n")
	sb.WriteString("::1 localhost\n")
	fmt.Fprintf(&sb, "%s host.containers.internal host.docker.internal\n", hostIP)

	for _, site := range reg.Sites {
		for _, domain := range site.Domains {
			fmt.Fprintf(&sb, "%s %s\n", nginxIP, domain)
		}
	}
	return sb.String()
}

// writeBrowserHosts writes the browser-testing hosts file. It resolves
// lerd-nginx's IP on the lerd Podman network and maps all .test domains
// to it. If nginx isn't running the file is still written with loopback
// entries (safe no-op — Selenium simply can't reach sites until nginx starts).
func writeBrowserHosts(reg *config.SiteRegistry) error {
	nginxIP := nginxContainerIP()

	var sb strings.Builder
	sb.WriteString("127.0.0.1 localhost\n")
	sb.WriteString("::1 localhost\n")

	for _, site := range reg.Sites {
		for _, domain := range site.Domains {
			fmt.Fprintf(&sb, "%s %s\n", nginxIP, domain)
		}
	}

	browserPath := config.BrowserHostsFile()
	if err := os.MkdirAll(filepath.Dir(browserPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(browserPath, []byte(sb.String()), 0644)
}

// DetectHostGatewayIP returns an IP that is reachable from inside lerd-nginx
// and resolves to the host. Tries each candidate by opening a TCP connection
// to lerd-ui on port 7073 — only an IP that actually routes back to the host
// will succeed. The first working candidate wins. If none work, returns the
// legacy fallback so /etc/hosts is still well-formed; `lerd doctor` reports
// the failure so the user gets a real diagnosis instead of silent timeouts.
//
// This replaces the previous "trust netavark" approach (PR #189), which
// trusted whatever `getent hosts host.containers.internal` returned without
// checking that the IP actually routed. On rootless Linux setups where
// netavark resolves to 169.254.1.2 but never wires up the bridge alias or
// DNAT for it, that IP is a dead end and Xdebug times out.
func DetectHostGatewayIP() string {
	if ip := probeReachableHostIP(); ip != "" {
		return ip
	}
	// Probe failed (lerd-ui not yet up, lerd-nginx not yet up, or no
	// candidate is routable). Fall back to whatever getent returns so the
	// file is well-formed. The next WriteContainerHosts call (after services
	// finish starting) gets a fresh probe and updates /etc/hosts in place.
	if ip := parseHostGatewayFromExec("lerd-nginx"); ip != "" {
		return ip
	}
	if ip := parseHostGatewayFromProbe(); ip != "" {
		return ip
	}
	return fallbackHostGatewayIP
}

// DetectHostGatewayIPProbeOnly is like DetectHostGatewayIP but returns ""
// when no candidate is actually reachable, instead of falling back to
// getent / the legacy constant. Used by `lerd doctor` to surface probe
// failures as a real diagnosis rather than the silent timeout Xdebug
// users otherwise see.
func DetectHostGatewayIPProbeOnly() string {
	return probeReachableHostIP()
}

// probeReachableHostIP returns the first candidate IP that lerd-nginx can
// open a TCP connection to on hostProbePort, or "" if no candidate works
// (or the probe can't run because lerd-nginx isn't up).
func probeReachableHostIP() string {
	if !ContainerRunningQuiet("lerd-nginx") {
		return ""
	}
	for _, ip := range hostCandidates(parseHostGatewayFromExec("lerd-nginx"), primaryLANIP()) {
		if probeHostFromNginx(ip, hostProbePort) {
			return ip
		}
	}
	return ""
}

// hostCandidates returns the ordered, deduplicated list of IPs to probe as
// the host gateway. Order: getent's host.containers.internal (works on
// macOS/gvproxy and well-configured Linux), the host's primary LAN IP
// (works whenever the host has any LAN address), and slirp4netns's default
// 10.0.2.2. host.containers.internal goes first because when it works
// it's the address Xdebug docs tell users to configure, and on macOS
// gvproxy makes it the canonical choice. Empty strings are skipped.
func hostCandidates(getentIP, lanIP string) []string {
	candidates := make([]string, 0, 3)
	seen := map[string]bool{}
	add := func(ip string) {
		if ip == "" || seen[ip] {
			return
		}
		seen[ip] = true
		candidates = append(candidates, ip)
	}
	add(getentIP)
	add(lanIP)
	add("10.0.2.2")
	return candidates
}

// HostReachable returns true when the given IP is reachable from lerd-nginx
// on the host probe port. Exported for the background watcher so it can
// cheaply verify whether the current /etc/hosts entry is still valid before
// running a full reprobe. Returns false when lerd-nginx isn't running.
func HostReachable(ip string) bool {
	if !ContainerRunningQuiet("lerd-nginx") {
		return false
	}
	return probeHostFromNginx(ip, hostProbePort)
}

// ReadHostGatewayFromFile reads the current host.containers.internal IP
// from the bind-mounted /etc/hosts file that PHP-FPM containers see.
// Returns "" if the file is missing or the entry isn't present. Used by
// the watcher to compare on-disk state against a fresh probe without
// rewriting the file when nothing has changed.
func ReadHostGatewayFromFile() string {
	data, err := os.ReadFile(config.ContainerHostsFile())
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		for _, name := range fields[1:] {
			if name == "host.containers.internal" {
				return fields[0]
			}
		}
	}
	return ""
}

// probeHostFromNginx returns true if lerd-nginx can open a TCP connection to
// ip:port within 2 seconds. Uses busybox nc (-z = scan only, -w = timeout).
func probeHostFromNginx(ip, port string) bool {
	cmd := exec.Command(PodmanBin(), "exec", "lerd-nginx", "nc", "-z", "-w", "2", ip, port)
	cmd.Stdout = nil
	cmd.Stderr = nil
	// Cap total wall time so a hung exec doesn't block lerd start.
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return false
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err == nil
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		return false
	}
}

// ContainerRunningQuiet wraps ContainerRunning, swallowing the error and
// returning false when podman exec is unavailable.
func ContainerRunningQuiet(name string) bool {
	running, err := ContainerRunning(name)
	return err == nil && running
}

func parseHostGatewayFromExec(container string) string {
	out, err := exec.Command(PodmanBin(), "exec", container,
		"getent", "hosts", "host.containers.internal").Output()
	if err != nil {
		return ""
	}
	return firstField(string(out))
}

func parseHostGatewayFromProbe() string {
	out, err := exec.Command(PodmanBin(), "run", "--rm", "--network", "lerd",
		"docker.io/library/alpine", "getent", "hosts", "host.containers.internal").Output()
	if err != nil {
		return ""
	}
	return firstField(string(out))
}

func firstField(s string) string {
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 {
			return fields[0]
		}
	}
	return ""
}

// nginxContainerIP returns the IP address of lerd-nginx on the lerd Podman
// network. Falls back to 127.0.0.1 if the container isn't running.
func nginxContainerIP() string {
	out, err := exec.Command(PodmanBin(), "inspect", "lerd-nginx",
		"--format", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}").Output()
	if err != nil {
		return "127.0.0.1"
	}
	ip := strings.TrimSpace(string(out))
	if ip == "" {
		return "127.0.0.1"
	}
	return ip
}

// primaryLANIP returns the local IPv4 address that the kernel would use to
// reach a public destination. Duplicates internal/dns/setup_common.go's
// helper because importing dns from podman would create a cycle.
func primaryLANIP() string {
	conn, err := net.Dial("udp4", "1.1.1.1:80")
	if err == nil {
		defer conn.Close()
		return conn.LocalAddr().(*net.UDPAddr).IP.String()
	}
	ifaces, ifErr := net.Interfaces()
	if ifErr != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok {
				if v4 := ipnet.IP.To4(); v4 != nil && !v4.IsLoopback() {
					return v4.String()
				}
			}
		}
	}
	return ""
}
