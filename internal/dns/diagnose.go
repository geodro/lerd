package dns

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
)

// StepStatus is the outcome of a single rung in the layered DNS check.
type StepStatus string

const (
	StepOK   StepStatus = "ok"
	StepFail StepStatus = "fail"
	StepWarn StepStatus = "warn"
	StepSkip StepStatus = "skip"
)

// Step is a single rung in the layered diagnostic. Higher rungs can only
// pass when the lower ones do; once a rung fails, the orchestrator marks
// every subsequent rung Skip with a brief reason so the user sees exactly
// where the chain breaks without a wall of cascaded failures.
type Step struct {
	Name   string     `json:"name"`
	Status StepStatus `json:"status"`
	Detail string     `json:"detail,omitempty"`
	Hint   string     `json:"hint,omitempty"`
}

// Diagnostic is the result of walking the DNS chain end to end. Steps are
// returned in chain order; FirstFailure points at the index of the first
// rung that returned Fail (or -1 when everything passed).
type Diagnostic struct {
	TLD          string `json:"tld"`
	Steps        []Step `json:"steps"`
	FirstFailure int    `json:"first_failure"`
}

// probeFns is the set of OS / environment calls the diagnostic makes,
// extracted to a struct so tests can swap each rung's implementation
// without touching real podman / network state. Production callers use
// defaultProbes(); tests use their own fakes.
type probeFns struct {
	containerRunning func() bool
	dnsmasqConfigOK  func(tld string) (ok bool, detail string)
	portOpen         func(host string, port int) bool
	dnsmasqAnswer    func(tld string) (answer string, err error)
	resolverHookup   func() (kind string, exists bool, path string)
	interfaceRouting func(tld string) (interfaceName string, has5300 bool, hasTLD bool, err error)
	systemLookup     func(tld string) (addrs []string, err error)
	vpnActive        func() bool
}

// Diagnose walks the DNS chain top to bottom and returns a structured
// result. tld defaults to "test" when empty.
func Diagnose(tld string) Diagnostic {
	return diagnose(tld, defaultProbes())
}

func diagnose(tld string, p probeFns) Diagnostic {
	if tld == "" {
		tld = "test"
	}
	d := Diagnostic{TLD: tld, FirstFailure: -1}

	// Rung 1 — lerd-dns container.
	if p.containerRunning() {
		d.Steps = append(d.Steps, Step{Name: "lerd-dns container", Status: StepOK, Detail: "running"})
	} else {
		d.Steps = append(d.Steps, Step{
			Name:   "lerd-dns container",
			Status: StepFail,
			Detail: "not running",
			Hint:   "lerd start  (or check podman logs lerd-dns)",
		})
		return finalize(d)
	}

	// Rung 2 — dnsmasq config.
	if ok, detail := p.dnsmasqConfigOK(tld); ok {
		d.Steps = append(d.Steps, Step{Name: "dnsmasq config", Status: StepOK, Detail: detail})
	} else {
		d.Steps = append(d.Steps, Step{
			Name:   "dnsmasq config",
			Status: StepFail,
			Detail: detail,
			Hint:   "lerd start regenerates the config from your registered TLD",
		})
		return finalize(d)
	}

	// Rung 3 — port reachable on loopback.
	if p.portOpen("127.0.0.1", 5300) {
		d.Steps = append(d.Steps, Step{Name: "port 5300 listening", Status: StepOK, Detail: "127.0.0.1:5300"})
	} else {
		d.Steps = append(d.Steps, Step{
			Name:   "port 5300 listening",
			Status: StepFail,
			Detail: "no TCP listener on 127.0.0.1:5300",
			Hint:   "check whether another process owns the port: " + findListenerCmd(5300),
		})
		return finalize(d)
	}

	// Rung 4 — direct DNS query at port 5300.
	answer, err := p.dnsmasqAnswer(tld)
	switch {
	case err != nil:
		d.Steps = append(d.Steps, Step{
			Name:   "dig @127.0.0.1 -p 5300",
			Status: StepFail,
			Detail: err.Error(),
			Hint:   "lerd-dns config probably stale, restart with: systemctl --user restart lerd-dns",
		})
		return finalize(d)
	case answer != "127.0.0.1":
		d.Steps = append(d.Steps, Step{
			Name:   "dig @127.0.0.1 -p 5300",
			Status: StepFail,
			Detail: fmt.Sprintf("got %q, want 127.0.0.1", answer),
			Hint:   "lerd-dns address rule missing, restart with: systemctl --user restart lerd-dns",
		})
		return finalize(d)
	default:
		d.Steps = append(d.Steps, Step{Name: "dig @127.0.0.1 -p 5300", Status: StepOK, Detail: answer})
	}

	// Rung 5 — resolver hookup file.
	kind, exists, path := p.resolverHookup()
	if exists {
		d.Steps = append(d.Steps, Step{Name: "resolver hookup", Status: StepOK, Detail: kind + ": " + path})
	} else {
		d.Steps = append(d.Steps, Step{
			Name:   "resolver hookup",
			Status: StepFail,
			Detail: "no NetworkManager dispatcher or systemd-resolved drop-in installed",
			Hint:   "rerun: lerd install",
		})
		return finalize(d)
	}

	// Rung 6 — Linux interface-level routing (skip on macOS).
	if runtime.GOOS == "linux" {
		iface, has5300, hasTLD, err := p.interfaceRouting(tld)
		switch {
		case err != nil:
			d.Steps = append(d.Steps, Step{
				Name:   "interface routes .test to 5300",
				Status: StepWarn,
				Detail: err.Error(),
				Hint:   "could not parse resolvectl status, skipping check",
			})
		case !has5300:
			d.Steps = append(d.Steps, Step{
				Name:   "interface routes .test to 5300",
				Status: StepFail,
				Detail: fmt.Sprintf("interface %q has no 127.0.0.1:5300 entry", iface),
				Hint:   "sudo systemctl restart NetworkManager  (or sudo resolvectl revert " + iface + " && rerun lerd install)",
			})
			return finalize(d)
		case !hasTLD:
			d.Steps = append(d.Steps, Step{
				Name:   "interface routes .test to 5300",
				Status: StepFail,
				Detail: fmt.Sprintf("interface %q has 127.0.0.1:5300 but no ~%s domain", iface, tld),
				Hint:   "sudo resolvectl domain " + iface + " ~" + tld + " ~.",
			})
			return finalize(d)
		default:
			d.Steps = append(d.Steps, Step{Name: "interface routes ." + tld + " to 5300", Status: StepOK, Detail: iface})
		}
	}

	// Rung 7 — end to end resolution at port 53.
	addrs, err := p.systemLookup(tld)
	vpn := p.vpnActive != nil && p.vpnActive()
	switch {
	case err != nil:
		d.Steps = append(d.Steps, systemLookupFailStep(err.Error(), vpn))
	case !contains(addrs, "127.0.0.1"):
		d.Steps = append(d.Steps, systemLookupFailStep(
			fmt.Sprintf("got %v, want one entry to be 127.0.0.1", addrs), vpn))
	default:
		d.Steps = append(d.Steps, Step{Name: "system DNS lookup", Status: StepOK, Detail: "127.0.0.1"})
	}

	return finalize(d)
}

// systemLookupFailStep builds the Rung 7 failure step. When a VPN tunnel
// is up the system-resolver path failing is expected: the VPN client has
// taken over DNS, .test still resolves via lerd-dns directly, and the
// watcher re-syncs container DNS automatically. That is a warning, not a
// failure, so the chain doesn't flag a broken state lerd already handles.
func systemLookupFailStep(detail string, vpn bool) Step {
	if vpn {
		return Step{
			Name:   "system DNS lookup",
			Status: StepWarn,
			Detail: detail,
			Hint:   "a VPN tunnel is up and has taken over the system resolver; .test still resolves via lerd-dns directly and lerd re-syncs container DNS automatically when the VPN changes",
		}
	}
	return Step{
		Name:   "system DNS lookup",
		Status: StepFail,
		Detail: detail,
		Hint:   "lerd-dns is reachable directly but the system resolver isn't using it; check cloud-init or other tools that may overwrite resolved.conf",
	}
}

// finalize walks the steps once to find the first failure, marking every
// later step Skip when the orchestrator returned early. Used by both the
// happy path and every early-return so callers always see one consistent
// chain shape.
func finalize(d Diagnostic) Diagnostic {
	for i, s := range d.Steps {
		if s.Status == StepFail && d.FirstFailure < 0 {
			d.FirstFailure = i
		}
	}
	return d
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// findListenerCmd returns the shell command the user can run to identify
// the process bound to a TCP port. macOS lacks ss(8), so we point users at
// lsof which ships with the OS; everywhere else we assume iproute2 ss.
func findListenerCmd(port int) string {
	if runtime.GOOS == "darwin" {
		return fmt.Sprintf("lsof -nP -iTCP:%d -sTCP:LISTEN", port)
	}
	return fmt.Sprintf("ss -tlnp sport = :%d", port)
}

// defaultProbes wires the production implementations for each rung.
func defaultProbes() probeFns {
	return probeFns{
		containerRunning: defaultContainerRunning,
		dnsmasqConfigOK:  defaultDnsmasqConfigOK,
		portOpen:         defaultPortOpen,
		dnsmasqAnswer:    defaultDnsmasqAnswer,
		resolverHookup:   defaultResolverHookup,
		interfaceRouting: defaultInterfaceRouting,
		systemLookup:     defaultSystemLookup,
		vpnActive:        VPNActive,
	}
}

func defaultContainerRunning() bool {
	out, err := exec.Command("systemctl", "--user", "is-active", "lerd-dns").Output()
	if err == nil && strings.TrimSpace(string(out)) == "active" {
		return true
	}
	cmd := exec.Command("podman", "ps", "--filter", "name=^lerd-dns$", "--format", "{{.Names}}")
	out, err = cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "lerd-dns"
}

func defaultDnsmasqConfigOK(tld string) (bool, string) {
	path := filepath.Join(config.DnsmasqDir(), "lerd.conf")
	data, err := os.ReadFile(path)
	if err != nil {
		return false, "missing " + path
	}
	if !strings.Contains(string(data), "port=5300") {
		return false, "config missing port=5300 directive"
	}
	want := "address=/." + tld + "/"
	if !strings.Contains(string(data), want) {
		return false, fmt.Sprintf("config missing %q rule", want)
	}
	return true, "address=/." + tld + "/127.0.0.1, port=5300"
}

func defaultPortOpen(host string, port int) bool {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		// Try UDP — dnsmasq listens on both, but the TCP listen is the
		// stable indicator that the daemon is actually up.
		udp, udpErr := net.DialTimeout("udp", addr, 500*time.Millisecond)
		if udpErr != nil {
			return false
		}
		_ = udp.Close()
		return true
	}
	_ = conn.Close()
	return true
}

func defaultDnsmasqAnswer(tld string) (string, error) {
	// Query A records only — lerd's dnsmasq config has
	// `address=/.tld/127.0.0.1`, which only matches A. Asking for the
	// host generically (LookupHost) brings AAAA into the mix and the
	// resolver may return ::1 first depending on system preference, even
	// though dnsmasq itself answered the IPv4 question correctly.
	host := "lerd-probe." + tld
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 1 * time.Second}
			return d.DialContext(ctx, network, "127.0.0.1:5300")
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	ips, err := r.LookupIP(ctx, "ip4", host)
	if err != nil {
		return "", err
	}
	if len(ips) == 0 {
		return "", fmt.Errorf("no A record")
	}
	return ips[0].String(), nil
}

func defaultResolverHookup() (string, bool, string) {
	if runtime.GOOS != "linux" {
		return "macOS native dnsmasq", true, "/usr/local/etc/dnsmasq.d/lerd.conf"
	}
	for kind, path := range map[string]string{
		"NetworkManager dispatcher": "/etc/NetworkManager/dispatcher.d/99-lerd-dns",
		"systemd-resolved drop-in":  "/etc/systemd/resolved.conf.d/lerd.conf",
		"NetworkManager dnsmasq":    "/etc/NetworkManager/dnsmasq.d/lerd.conf",
	} {
		if _, err := os.Stat(path); err == nil {
			return kind, true, path
		}
	}
	return "", false, ""
}

func defaultInterfaceRouting(tld string) (string, bool, bool, error) {
	out, err := exec.Command("resolvectl", "status").Output()
	if err != nil {
		return "", false, false, err
	}
	iface, has5300, hasTLD := parseInterfaceRouting(string(out), tld)
	return iface, has5300, hasTLD, nil
}

// parseInterfaceRouting walks `resolvectl status` output looking for the
// first interface that carries a 127.0.0.1:5300 entry, then reports whether
// the same interface block also lists ~<tld> as a routing domain. sawDomain
// is one-shot: once set on the right interface it persists through the
// remaining Link blocks (which would otherwise clobber it on reset).
func parseInterfaceRouting(output, tld string) (iface string, has5300 bool, hasTLD bool) {
	var current string
	var foundIface string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, " \t\r")
		if strings.HasPrefix(line, "Link ") {
			current = ""
			if start := strings.Index(line, "("); start >= 0 {
				if end := strings.Index(line, ")"); end > start {
					current = line[start+1 : end]
				}
			}
			continue
		}
		if foundIface == "" && strings.Contains(line, "127.0.0.1:5300") {
			foundIface = current
			has5300 = true
		}
		if foundIface != "" && current == foundIface {
			if strings.Contains(line, "~"+tld) {
				hasTLD = true
			}
		}
	}
	return foundIface, has5300, hasTLD
}

func defaultSystemLookup(tld string) ([]string, error) {
	host := "lerd-probe." + tld
	addrs, err := net.LookupHost(host)
	return addrs, err
}
