package serviceops

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// ErrPortInUse is returned by SetPublishedPort when the requested host port is
// not bindable. Callers can errors.Is on it to add a surface-specific hint (the
// CLI appends the "check which process owns it" command).
var ErrPortInUse = errors.New("port already in use")

// ErrPortReserved is returned when a requested host port is already claimed by
// another lerd service (its default, published, or extra ports). A plain
// bindability test misses this while that sibling is stopped, so the two units
// would collide at boot — this rejects the clash up front.
var ErrPortReserved = errors.New("port already claimed by another lerd service")

// PortChange reports the outcome of SetPublishedPort so each surface (CLI, MCP,
// Web UI) renders its own message from one shared code path.
type PortChange struct {
	Requested int  // the port the caller asked for (0 = reset to default)
	Actual    int  // the resulting published port (the guard may shift a reset)
	Installed bool // false when the service isn't installed (override saved only)
	WasActive bool // whether the unit was restarted to apply the change
	NoOp      bool // the requested port already matched the current override
}

// SetPublishedPort moves a service's published host port (port > 0) or resets it
// to the preset default (port == 0), persisting the override and re-rendering and
// restarting the unit as needed. It is the single entry point shared by the CLI
// `service port` command, the MCP service:port action, and the Web UI ports
// endpoint, so all three enforce identical validation, the port-ownership guard,
// and the host-proxy follower refresh. The container-internal port is untouched.
func SetPublishedPort(name string, port int) (PortChange, error) {
	res := PortChange{Requested: port, Actual: port}
	if !config.IsDefaultPreset(name) && !ServiceInstalled(name) {
		return res, fmt.Errorf("%q is not a built-in or installed service", name)
	}
	if port < 0 || port > 65535 {
		return res, fmt.Errorf("invalid port %d: must be 0-65535", port)
	}
	// Silence the guard's shift hook during our own quadlet write; we fire it
	// once at the end with the actual resulting port, so a --reset that the guard
	// re-shifts off a host-owned default never refreshes followers twice.
	savedHook := OnPublishedPortShift
	OnPublishedPortShift = nil
	defer func() { OnPublishedPortShift = savedHook }()

	cfg, err := config.LoadGlobal()
	if err != nil {
		return res, err
	}
	svcCfg := cfg.Services[name]
	if port == svcCfg.PublishedPort {
		res.NoOp = true
		res.Actual = svcCfg.PublishedPort
		return res, nil
	}
	// A published port can't double as one of this service's own extra ports.
	if port > 0 {
		for _, ep := range svcCfg.ExtraPorts {
			if extraHostPort(ep) == port {
				return res, fmt.Errorf("%w: %d", ErrPortInUse, port)
			}
		}
	}
	// Reject a port another lerd service already claims (even a stopped one), so
	// the two units can't collide at boot.
	if port > 0 && portReservedByOther(name, port) {
		return res, fmt.Errorf("%w: %d", ErrPortReserved, port)
	}
	// Pre-flight on both loopback stacks so the restart can't fail to bind and
	// leave the service down. Uses the guard's own bindability test, not a dial,
	// so the surface and the guard agree on what "free" means.
	if port > 0 && !PortAvailable(port) {
		return res, fmt.Errorf("%w: %d", ErrPortInUse, port)
	}
	svcCfg.PublishedPort = port
	cfg.Services[name] = svcCfg
	if err := config.SaveGlobal(cfg); err != nil {
		return res, err
	}
	// Only (re)write the quadlet for an INSTALLED service; never resurrect a
	// removed unit (which would auto-start on boot and grab a host-owned port).
	// The override is saved above either way, so the next install picks it up.
	if !ServiceInstalled(name) {
		res.Installed = false
		res.Actual = port
		return res, nil
	}
	res.Installed = true
	unit := "lerd-" + name
	status, _ := podman.UnitStatus(unit)
	res.WasActive = status == "active" || status == "activating"
	// Stop a running unit before the write: its own live listener would look
	// like a foreign owner to the guard and suppress a needed shift, leaving it
	// unable to bind on restart. Stopped, the write/guard/start see the true state.
	if res.WasActive {
		if err := podman.StopUnit(unit); err != nil {
			return res, fmt.Errorf("stopping %s: %w", unit, err)
		}
	}
	if err := rerenderServiceQuadlet(name); err != nil {
		return res, err
	}
	// The guard inside the write may have overridden the request, so report the
	// actual resulting published port, not the requested one.
	res.Actual = port
	if cfg2, lerr := config.LoadGlobal(); lerr == nil && cfg2 != nil {
		if sc, ok := cfg2.Services[name]; ok {
			res.Actual = sc.PublishedPort
		}
	}
	if res.WasActive {
		if err := podman.StartUnit(unit); err != nil {
			return res, fmt.Errorf("starting %s: %w", unit, err)
		}
		_ = podman.WaitReady(name, 30*time.Second)
	}
	// Host-proxy sites reach the service over the published loopback port, so
	// refresh their .env to follow the change (no-op when none use it).
	if savedHook != nil {
		savedHook(name, res.Actual)
	}
	return res, nil
}

// SetExtraPorts replaces a built-in service's extra published ports with ports
// (each a bare "host", "host:container", or "ip:host:container" mapping),
// de-duplicating and validating, then re-rendering and restarting the unit when
// it is running. Shared by the CLI `service expose`, MCP service:expose, and the
// Web UI ports endpoint. Custom services declare their ports in their own YAML,
// so this is preset-only.
func SetExtraPorts(name string, ports []string) error {
	if !config.IsDefaultPreset(name) {
		return fmt.Errorf("%q is not a built-in service", name)
	}
	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	svcCfg := cfg.Services[name]
	// The service's own main host port (published override, else preset default)
	// is reserved — an extra mapping must not republish it.
	mainHost := svcCfg.Port
	if svcCfg.PublishedPort > 0 {
		mainHost = svcCfg.PublishedPort
	}
	clean := make([]string, 0, len(ports))
	seenSpec := map[string]bool{}
	seenHost := map[int]bool{}
	for _, p := range ports {
		p = strings.TrimSpace(p)
		if p == "" || seenSpec[p] {
			continue
		}
		if err := ValidateExtraPort(p); err != nil {
			return err
		}
		host := extraHostPort(p)
		if host > 0 {
			if host == mainHost || seenHost[host] {
				return fmt.Errorf("%w: %d", ErrPortInUse, host)
			}
			if portReservedByOther(name, host) {
				return fmt.Errorf("%w: %d", ErrPortReserved, host)
			}
			seenHost[host] = true
		}
		seenSpec[p] = true
		clean = append(clean, p)
	}
	svcCfg.ExtraPorts = clean
	cfg.Services[name] = svcCfg
	if err := config.SaveGlobal(cfg); err != nil {
		return err
	}
	if !ServiceInstalled(name) {
		return nil
	}
	if err := EnsureDefaultPresetQuadlet(name); err != nil {
		return err
	}
	if status, _ := podman.UnitStatus("lerd-" + name); status == "active" {
		_ = podman.RestartUnit("lerd-" + name)
	}
	return nil
}

// AddExtraPort adds a single extra published port to a built-in service. Adding a
// mapping already present is a harmless re-render (SetExtraPorts de-duplicates).
func AddExtraPort(name, spec string) error {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	cur := cfg.Services[name].ExtraPorts
	return SetExtraPorts(name, append(append([]string{}, cur...), spec))
}

// RemoveExtraPort removes a single extra published port from a built-in service.
func RemoveExtraPort(name, spec string) error {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	return SetExtraPorts(name, removePort(append([]string{}, cfg.Services[name].ExtraPorts...), spec))
}

// ValidateExtraPort checks that spec is a usable podman port mapping: a bare host
// port, "host:container", or "ip:host:container", each port in 0-65535, with an
// optional "/tcp" or "/udp" suffix.
func ValidateExtraPort(spec string) error {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return fmt.Errorf("empty port mapping")
	}
	body := strings.SplitN(spec, "/", 2)[0]
	parts := strings.Split(body, ":")
	if len(parts) == 0 || len(parts) > 3 {
		return fmt.Errorf("invalid port mapping %q", spec)
	}
	nums := parts
	if len(parts) == 3 {
		nums = parts[1:] // ip:host:container — the IP isn't a port
	}
	for _, n := range nums {
		v, err := strconv.Atoi(strings.TrimSpace(n))
		if err != nil || v < 0 || v > 65535 {
			return fmt.Errorf("invalid port %q in mapping %q: must be 0-65535", strings.TrimSpace(n), spec)
		}
	}
	return nil
}

// rerenderServiceQuadlet rewrites a service's unit file from its current config,
// dispatching on whether it's a built-in preset or an installed custom service.
func rerenderServiceQuadlet(name string) error {
	if config.IsDefaultPreset(name) {
		return EnsureDefaultPresetQuadlet(name)
	}
	svc, err := config.LoadCustomService(name)
	if err != nil {
		return fmt.Errorf("loading custom service %q: %w", name, err)
	}
	return EnsureCustomServiceQuadlet(svc)
}

// portReservedByOther reports whether host port p is already claimed by a lerd
// service other than self (its default, published, or extra ports), reusing the
// same config.HostPorts() the port-ownership guard reserves from.
func portReservedByOther(self string, p int) bool {
	cfg, err := config.LoadGlobal()
	if err != nil || cfg == nil {
		return false
	}
	for svcName, svc := range cfg.Services {
		if svcName == self {
			continue
		}
		for _, hp := range svc.HostPorts() {
			if hp == p {
				return true
			}
		}
	}
	return false
}

// extraHostPort returns the host-side port of a "host", "host:container", or
// "ip:host:container" mapping (an optional /proto suffix is ignored), or 0 when
// none is parseable.
func extraHostPort(spec string) int {
	parts := strings.Split(strings.SplitN(spec, "/", 2)[0], ":")
	host := parts[0]
	if len(parts) > 1 {
		host = parts[len(parts)-2]
	}
	n, _ := strconv.Atoi(strings.TrimSpace(host))
	return n
}

func removePort(ports []string, port string) []string {
	out := ports[:0]
	for _, p := range ports {
		if p != port {
			out = append(out, p)
		}
	}
	return out
}
