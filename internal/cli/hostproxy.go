package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/geodro/lerd/internal/certs"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
	"github.com/geodro/lerd/internal/feedback"
	gitpkg "github.com/geodro/lerd/internal/git"
	"github.com/geodro/lerd/internal/nginx"
	"github.com/geodro/lerd/internal/siteops"
)

func init() {
	// certs.SecureSite/UnsecureSite regenerate worktree vhosts but can't import
	// cli (which owns host-proxy port allocation), so they call back through this
	// hook for host-proxy worktrees. Mirrors SetupHostProxyWorktree's vhost step.
	certs.RegenerateHostProxyWorktreeVhost = func(site config.Site, wtPath, wtDomain string, secured bool) error {
		proxy := parentProxyConfig(site)
		if proxy == nil {
			return nil
		}
		port := WorktreeHostPort(proxy.Port, wtPath, hostProxyPortEnvKey(proxy))
		return nginx.GenerateWorktreeHostProxyVhostFor(wtDomain, wtPath, site.PrimaryDomain(), port, proxy.SSL, secured)
	}
}

// RegenerateHostProxyVhostsOnGatewayChange rewrites every host-proxy site's
// nginx vhost so the host-gateway IP baked into proxy_pass (Linux only) tracks a
// network change, then reloads nginx. Wired to the host-gateway watcher from
// main. No-op on macOS, where the upstream is the gvproxy-resolved
// host.containers.internal hostname rather than a literal IP.
func RegenerateHostProxyVhostsOnGatewayChange() {
	if runtime.GOOS == "darwin" {
		return
	}
	reg, err := config.LoadSites()
	if err != nil {
		return
	}
	regenerated := false
	for i := range reg.Sites {
		s := reg.Sites[i]
		if !s.IsHostProxy() {
			continue
		}
		if err := siteops.RegenerateSiteVhost(&s, s.PrimaryDomain()); err != nil {
			feedback.Warn("regenerating host-proxy vhost for %s: %v", s.Name, err)
			continue
		}
		if wts, wErr := gitpkg.ServableWorktrees(s.Path, s.PrimaryDomain()); wErr == nil {
			for _, wt := range wts {
				_ = certs.RegenerateHostProxyWorktreeVhost(s, wt.Path, wt.Domain, s.Secured)
			}
		}
		regenerated = true
	}
	if regenerated {
		_ = nginx.Reload()
	}
}

// hostProxyWorkerName is the stable worker name for a host-proxy site's
// supervised dev server. Aliases the shared config constant so the unit name
// has a single source of truth (see config.HostProxyWorkerUnit).
const hostProxyWorkerName = config.HostProxyWorkerName

// hostProxyPortEnvKey returns the environment variable the port is injected
// as, defaulting to PORT (honoured by NestJS, Next, Nuxt, and most Node
// servers).
func hostProxyPortEnvKey(proxy *config.ProxyConfig) string {
	if proxy.PortEnvKey != "" {
		return proxy.PortEnvKey
	}
	return "PORT"
}

// hostProxyHostEnvKey returns the environment variable the bind address is
// injected as, defaulting to HOST (honoured by Nuxt, NestJS, and most Node
// servers). Set host_env_key to e.g. HOSTNAME for a Next.js standalone server.
func hostProxyHostEnvKey(proxy *config.ProxyConfig) string {
	if proxy.HostEnvKey != "" {
		return proxy.HostEnvKey
	}
	return "HOST"
}

// hostProxyBindAddr is the address the dev server must bind so the in-container
// nginx can reach it. nginx proxies to the host over the gateway IP, so a dev
// server that binds loopback (the common default) is unreachable: it surfaces
// as a connection error, or as a stray all-interfaces HMR socket answering
// every plain request with 426 Upgrade Required. Binding all interfaces fixes
// both. Note: env-only; pure Vite reads --host, not this env var.
const hostProxyBindAddr = "0.0.0.0"

// buildHostProxyCommandPort prefixes the dev command with `env PORT=port
// HOST=0.0.0.0` so the app binds the port nginx proxies to, on an interface the
// proxy container can reach. The `env` utility (not a bare `KEY=value`
// assignment) is used because host workers exec the command both through a
// shell (macOS) and directly via `fnm exec --` (Linux); `env` is a real
// executable that works in both. A HOST the user sets later in their own
// command still wins (env evaluates left to right). Returns "" in proxy-only
// mode (no command).
func buildHostProxyCommandPort(proxy *config.ProxyConfig, port int) string {
	if proxy.Command == "" {
		return ""
	}
	return fmt.Sprintf("env %s=%d %s=%s %s",
		hostProxyPortEnvKey(proxy), port,
		hostProxyHostEnvKey(proxy), hostProxyBindAddr,
		proxy.Command)
}

func buildHostProxyCommand(proxy *config.ProxyConfig) string {
	return buildHostProxyCommandPort(proxy, proxy.Port)
}

// hostProxyWorkerForPort builds the supervised dev-server worker on a specific
// port. Worktrees mirror the parent command on their own port; the parent uses
// proxy.Port via hostProxyWorker. ok is false in proxy-only mode (no command).
func hostProxyWorkerForPort(proxy *config.ProxyConfig, port int) (config.FrameworkWorker, bool) {
	command := buildHostProxyCommandPort(proxy, port)
	if command == "" {
		return config.FrameworkWorker{}, false
	}
	return config.FrameworkWorker{
		Label:   "Dev Server",
		Command: command,
		Restart: "always",
		Host:    true,
	}, true
}

// hostProxyWorker builds the supervised dev-server worker for a host-proxy
// site on its configured port.
func hostProxyWorker(proxy *config.ProxyConfig) (config.FrameworkWorker, bool) {
	return hostProxyWorkerForPort(proxy, proxy.Port)
}

// hostProxyWorkerUnit returns the worker unit name for a host-proxy site.
func hostProxyWorkerUnit(siteName string) string {
	return config.HostProxyWorkerUnit(siteName)
}

// devScriptCandidates are the package.json scripts a host-proxy site might run
// as its dev server, in the order the wizard prefers them.
var devScriptCandidates = []string{"start:dev", "dev", "serve", "start"}

// packageManifest is the slice of package.json the host-proxy wizard reads.
type packageManifest struct {
	Scripts map[string]string `json:"scripts"`
}

// defaultDevServerPort is where host-port allocation starts when the command
// doesn't name a port; the allocator walks up from here to the first free port.
const defaultDevServerPort = 3000

// readPackageManifest parses package.json once; nil if absent or invalid. The
// methods below are nil-safe so callers don't have to branch.
func readPackageManifest(cwd string) *packageManifest {
	data, err := os.ReadFile(filepath.Join(cwd, "package.json"))
	if err != nil {
		return nil
	}
	var m packageManifest
	if json.Unmarshal(data, &m) != nil {
		return nil
	}
	return &m
}

// devScripts returns the present dev-server scripts in preference order, each
// rendered as "npm run <name>".
func (m *packageManifest) devScripts() []string {
	if m == nil {
		return nil
	}
	var out []string
	for _, c := range devScriptCandidates {
		if _, ok := m.Scripts[c]; ok {
			out = append(out, "npm run "+c)
		}
	}
	return out
}

// AvailableDevScripts returns the dev-server scripts present in the project's
// package.json, in preference order, each rendered as "npm run <name>".
func AvailableDevScripts(cwd string) []string {
	return readPackageManifest(cwd).devScripts()
}

// runsVite reports whether the wizard's chosen dev command launches Vite,
// resolving "npm run <script>" against package.json so the allowedHosts note
// only prints for Vite projects. A custom command naming vite counts too.
func (m *packageManifest) runsVite(command string) bool {
	if strings.Contains(command, "vite") {
		return true
	}
	if name, ok := strings.CutPrefix(command, "npm run "); ok && m != nil {
		return strings.Contains(m.Scripts[name], "vite")
	}
	return false
}

var portFlagRe = regexp.MustCompile(`(?:--port[ =]|PORT=)(\d+)`)

// portFromCommand extracts an explicit port from a command string, or 0 if none.
// A dev command that already names a port keeps it; otherwise the port is
// auto-assigned and injected via the PORT env var.
func portFromCommand(command string) int {
	m := portFlagRe.FindStringSubmatch(command)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// isNodeProject reports whether dir is a Node project, used to decide whether a
// host worker's command runs through fnm (Node) or directly (host-proxy sites in
// Python, Ruby, Go, and other languages). A package.json, .nvmrc, or
// .node-version all count; none of these exist in a non-Node project.
func isNodeProject(dir string) bool {
	for _, f := range []string{"package.json", ".nvmrc", ".node-version"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			return true
		}
	}
	return false
}

// firstFreePort returns the first port at or above start for which isTaken is
// false. Pure (isTaken is injected) so the search logic is unit-testable
// without binding real sockets. Falls back to start if nothing is free.
func firstFreePort(start int, isTaken func(int) bool) int {
	if start < 1 {
		start = 1
	}
	for p := start; p <= 65535; p++ {
		if !isTaken(p) {
			return p
		}
	}
	return start
}

// portBoundOnHost reports whether something is already listening on the host's
// loopback at port p. Used as the live half of host-port allocation so a dev
// server isn't assigned a port a lerd service (or any process) already holds.
// Both IPv4 and IPv6 loopback are probed so a service bound only to [::1] (as
// some lerd quadlets are) is still detected as taken.
func portBoundOnHost(p int) bool {
	// IPv4 loopback is authoritative: a failure here means the port is taken.
	if ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p))); err != nil {
		return true
	} else {
		_ = ln.Close()
	}
	// IPv6 loopback catches services bound only to [::1], but on a host with
	// IPv6 disabled the listen fails with EADDRNOTAVAIL / EAFNOSUPPORT for every
	// port. Only an in-use error counts as taken, else allocation would report
	// every port busy and fall back to the start port.
	if ln, err := net.Listen("tcp", net.JoinHostPort("::1", strconv.Itoa(p))); err != nil {
		return errors.Is(err, syscall.EADDRINUSE)
	} else {
		_ = ln.Close()
	}
	return false
}

// reservedHostPorts returns host ports already claimed by other host-proxy
// sites in the registry, so two sites never get assigned the same port even
// when the other site's dev server isn't currently running. exceptSite is
// skipped so re-running init on a site keeps its own port.
func reservedHostPorts(exceptSite string) map[int]bool {
	out := map[int]bool{}
	exceptPort := 0
	if reg, err := config.LoadSites(); err == nil {
		for _, s := range reg.Sites {
			if s.Name == exceptSite {
				exceptPort = s.HostPort
				continue
			}
			if s.HostPort != 0 {
				out[s.HostPort] = true
			}
			// Also reserve ports already assigned to this site's host-proxy
			// worktrees (persisted in each worktree's .env): a stopped worktree
			// dev server owns its port, which neither the registry HostPort nor a
			// bind-probe reveals, so two worktrees could otherwise collide.
			if s.IsHostProxy() {
				if proxy := parentProxyConfig(s); proxy != nil {
					key := hostProxyPortEnvKey(proxy)
					wts, _ := gitpkg.ServableWorktrees(s.Path, s.PrimaryDomain())
					for _, wt := range wts {
						if v := envfile.ReadKey(filepath.Join(wt.Path, ".env"), key); v != "" {
							if p, _ := strconv.Atoi(v); p > 0 {
								out[p] = true
							}
						}
					}
				}
			}
		}
	}
	// Reserve host ports lerd services publish (e.g. gotenberg on 3000) even when
	// the container is stopped: a stopped service still owns its published port
	// and would collide the moment it starts, which portBoundOnHost can't foresee.
	for p := range lerdServiceHostPorts() {
		out[p] = true
	}
	// Honour exceptSite across the whole set: re-running init on a site keeps its
	// own already-assigned port even when it coincides with a service's port, so
	// allocation stays idempotent instead of silently bumping the site.
	if exceptPort > 0 {
		delete(out, exceptPort)
	}
	return out
}

// lerdServiceHostPorts returns every host port a lerd service may publish:
// installed/default services (with their resolved ports), all bundled presets
// (including optional ones like gotenberg that aren't in the default set), and
// installed custom services. Used so a host-proxy dev server is never assigned a
// port a service will reclaim.
func lerdServiceHostPorts() map[int]bool {
	out := map[int]bool{}
	add := func(mappings []string) {
		for _, m := range mappings {
			if host, _, ok := splitHostContainerPort(m); ok {
				if n, _ := strconv.Atoi(host); n > 0 {
					out[n] = true
				}
			}
		}
	}
	if cfg, err := config.LoadGlobal(); err == nil {
		for _, svc := range cfg.Services {
			if svc.Port > 0 {
				out[svc.Port] = true
			}
			add(svc.ExtraPorts)
		}
	}
	if presets, err := config.ListPresets(); err == nil {
		for _, p := range presets {
			if pr, err := config.LoadPreset(p.Name); err == nil {
				add(pr.Ports)
			}
		}
	}
	if customs, err := config.ListCustomServices(); err == nil {
		for _, svc := range customs {
			add(svc.Ports)
		}
	}
	return out
}

// allocateHostPort picks a free host port for a dev server, starting from the
// tool's conventional default and walking up past anything another host-proxy
// site reserves or any process currently binds (e.g. lerd-gotenberg on 3000).
func allocateHostPort(start int, exceptSite string) int {
	reserved := reservedHostPorts(exceptSite)
	return firstFreePort(start, func(p int) bool {
		return reserved[p] || portBoundOnHost(p)
	})
}

// WorktreeHostPort returns the dev-server port for a host-proxy site's worktree:
// the value persisted in the worktree's .env if present, otherwise a freshly
// allocated free port (best-effort persisted so it stays stable). A worktree is
// reached by its domain, so a floating port until a .env exists is harmless.
func WorktreeHostPort(parentPort int, wtPath, portEnvKey string) int {
	envPath := filepath.Join(wtPath, ".env")
	if v := envfile.ReadKey(envPath, portEnvKey); v != "" {
		if n, _ := strconv.Atoi(v); n > 0 {
			return n
		}
	}
	port := allocateHostPort(parentPort+1, "")
	if _, err := os.Stat(envPath); err == nil {
		_ = envfile.ApplyUpdates(envPath, map[string]string{portEnvKey: strconv.Itoa(port)})
	}
	return port
}

// parentProxyConfig returns the host-proxy config a worktree should mirror. It
// prefers the parent's committed .lerd.yaml proxy block, falling back to the
// fields persisted on the registered Site (proxy config is local config, so a
// worktree checkout usually can't see it).
func parentProxyConfig(site config.Site) *config.ProxyConfig {
	if proj, err := config.LoadProjectConfig(site.Path); err == nil && proj.Proxy != nil {
		return proj.Proxy
	}
	if site.HostCommand == "" && site.HostPort == 0 {
		return nil
	}
	return &config.ProxyConfig{Command: site.HostCommand, Port: site.HostPort, SSL: site.HostSSL}
}

// SetupHostProxyWorktree wires a host-proxy site's worktree: it mirrors the
// parent's dev command on a per-worktree port, generates the worktree proxy
// vhost, and starts the dev server from the worktree checkout. The unit name
// (lerd-app-<site>-<branch>) and teardown are handled by the shared per-worktree
// worker machinery.
func SetupHostProxyWorktree(site config.Site, wtPath, wtDomain string) error {
	proxy := parentProxyConfig(site)
	if proxy == nil {
		return fmt.Errorf("parent site %s has no proxy config to mirror", site.Name)
	}
	port := WorktreeHostPort(proxy.Port, wtPath, hostProxyPortEnvKey(proxy))
	if err := nginx.GenerateWorktreeHostProxyVhostFor(wtDomain, wtPath, site.PrimaryDomain(), port, proxy.SSL, site.Secured); err != nil {
		return err
	}
	if w, ok := hostProxyWorkerForPort(proxy, port); ok {
		if err := gateHostProxyAutostart(site, proxy.Command); err != nil {
			return err
		}
		if err := WorkerStartForSite(site.Name, wtPath, "", hostProxyWorkerName, w, false); err != nil {
			return fmt.Errorf("starting worktree dev server: %w", err)
		}
	}
	return nil
}

// startHostProxyWorker supervises the dev command for a host-proxy site as a
// host-mode worker (launchd/fnm on macOS), reusing the standard worker
// machinery for auto-restart, logs, and health. No-op in proxy-only mode.
func startHostProxyWorker(site config.Site, proxy *config.ProxyConfig) {
	w, ok := hostProxyWorker(proxy)
	if !ok {
		return
	}
	if err := gateHostProxyAutostart(site, proxy.Command); err != nil {
		feedback.Warn("dev server not started: %v", err)
		return
	}
	if err := WorkerStartForSite(site.Name, site.Path, "", hostProxyWorkerName, w, false); err != nil {
		feedback.Warn("starting dev server: %v", err)
	}
}

// gateHostProxyAutostart authorises auto-(re)starting a host-proxy dev command
// on a non-link path (watcher, unpause): the command must match the one approved
// at link and host_proxy.disabled must be off, else it is refused unattended.
func gateHostProxyAutostart(site config.Site, command string) error {
	approved := command != "" && command == site.HostCommand
	return approveHostProxyCommand(site.Name, command, approved)
}

// hostProxyPreApproved lets the init wizard mark the command the user just chose
// as approved, so the link it triggers doesn't prompt again for the same command.
var hostProxyPreApproved bool

// hostProxyGate is the pure decision for whether lerd may install and start a
// host-proxy dev-server command. It is split out so the policy is unit-testable
// without a TTY or global-config file. proceed reports whether to run without
// asking; prompt reports that an interactive confirmation is still owed; reason
// explains a refusal. A site can only run a command the user has consented to.
func hostProxyGate(command string, disabled, skipConfirm, approved, interactive bool) (proceed, prompt bool, reason string) {
	if command == "" {
		// Proxy-only: lerd supervises nothing, so neither the disable switch
		// nor the confirmation applies.
		return true, false, ""
	}
	if disabled {
		return false, false, "host-proxy dev servers are disabled (set host_proxy.disabled: false to enable)"
	}
	if approved || hostProxyPreApproved || skipConfirm {
		return true, false, ""
	}
	if !interactive {
		return false, false, "command not approved; re-run `lerd link` interactively, pass --yes, or set host_proxy.skip_confirmation: true"
	}
	return false, true, ""
}

// approveHostProxyCommand enforces the consent gates before lerd supervises a
// dev-server command on the host: the global disable switch and an interactive
// confirmation of the exact command. approved short-circuits the prompt when the
// command already matches the registry-approved one or the caller passed --yes.
func approveHostProxyCommand(siteName, command string, approved bool) error {
	gcfg, _ := config.LoadGlobal()
	proceed, prompt, reason := hostProxyGate(command, gcfg.HostProxy.Disabled, gcfg.HostProxy.SkipConfirmation, approved, isInteractive())
	if proceed {
		return nil
	}
	if !prompt {
		return fmt.Errorf("host-proxy %s: %s", siteName, reason)
	}
	fmt.Printf("\nlerd supervises this dev-server command on your host, outside any container:\n\n  %s\n", command)
	if !promptConfirm(fmt.Sprintf("Start and auto-restart it for %s?", siteName)) {
		return fmt.Errorf("host-proxy setup declined for %s", siteName)
	}
	return nil
}
