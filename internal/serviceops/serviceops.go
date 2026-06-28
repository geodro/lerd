// Package serviceops contains the shared business logic for installing,
// starting, stopping, and removing lerd services. The CLI commands and the
// MCP tools both call into here so they enforce identical preset gating,
// dependency cascades, and dynamic_env regeneration.
package serviceops

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/feedback"
	"github.com/geodro/lerd/internal/freeport"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/registry"
)

// IsBuiltin reports whether name is a built-in (default-preset) lerd service.
// Kept as a passthrough so callers don't have to import config.
func IsBuiltin(name string) bool { return config.IsDefaultPreset(name) }

// ServiceInstalled is the single source of truth for whether a lerd service
// is installed on this host. It checks for the quadlet (lerd-<name>.container)
// because that's what podman actually uses to run the service, and it can
// outlive the YAML when the on-disk config drifts (older installs, partial
// removes, etc.). Use this instead of probing config.LoadCustomService when
// you only care about install presence.
func ServiceInstalled(name string) bool {
	return podman.QuadletInstalled("lerd-" + name)
}

// PortAvailable reports whether a TCP port is free to bind on both loopback
// stacks. It is the exported form of the guard's own bindability test (now the
// shared freeport.Bindable), used by the `lerd service port` pre-flight so the
// CLI and the guard agree on what "free" means — a plain dial test would miss a
// port reserved only on ::1.
func PortAvailable(port int) bool { return freeport.Bindable(port) }

// OnPublishedPortShift, if set, is invoked when the port-ownership guard moves a
// service's published port to avoid a host server (service name, new port). The
// CLI wires this to regenerate host-proxy sites' .env so their loopback DB target
// follows the moved port; it stays nil in pure serviceops tests (which don't
// import package cli). The MCP server shares the binary, so the CLI init sets it
// there too — that is fine: the env refresh is still the right thing to do, and
// the MCP server repoints os.Stdout at stderr so its output can't corrupt the
// protocol. Fired from the guard so any quadlet-write path (install, start,
// reinstall, `service port --reset`) refreshes followers, not just the explicit
// `lerd service port` command.
var OnPublishedPortShift func(service string, newPort int)

// unitActive reports whether a service's own systemd unit is currently up. The
// generic port guard uses it to avoid treating the service's *own* published
// listener as a foreign owner of the port (see maybeShiftPublishedPort).
func unitActive(name string) bool {
	status, _ := podman.UnitStatus("lerd-" + name)
	return status == "active" || status == "activating"
}

// maybeShiftPublishedPort decides whether a service whose primary host port is
// `primary` should be moved to a free port, returning the new port or 0 to keep
// the default. Port availability is the ONLY signal — lerd never inspects host
// files, sockets, or binaries to decide. A port is only reclaimed while the
// service is down (active == false): a running service holds its own port, and
// counting that as a collision would shuffle a healthy service on every quadlet
// rewrite (family regeneration, config edit, update). The next free port skips
// anything another lerd service already publishes (reserved), even when stopped,
// so two units don't collide at boot. Returns 0 when the port is fine, the
// service is up, or no free port exists.
func maybeShiftPublishedPort(primary int, active bool) int {
	if primary <= 0 || active || freeport.Bindable(primary) {
		return 0
	}
	reserved := lerdReservedPorts()
	return freeport.FirstFree(primary+1, func(p int) bool {
		return reserved[p] || !freeport.Bindable(p)
	})
}

// lerdReservedPorts collects the host ports already claimed by lerd's own services in
// global config — each service's published port, its preset-default port, and any extra
// published ports — so the port-ownership guard never auto-picks a port another lerd
// service will bind. The preset-default Port matters even for a STOPPED service: nothing
// is listening, so freeport.Bindable() would report it free, and handing it out would
// collide when both units start at boot (the failure this guard exists to prevent).
func lerdReservedPorts() map[int]bool {
	reserved := map[int]bool{}
	cfg, err := config.LoadGlobal()
	if err != nil || cfg == nil {
		return reserved
	}
	for _, svc := range cfg.Services {
		for _, p := range svc.HostPorts() {
			reserved[p] = true
		}
	}
	return reserved
}

// persistPublishedPort records port as service name's published port in global
// config, returning an error on any load/save failure. The port-ownership guard
// calls this BEFORE writing the quadlet so it can fail closed: if the choice can't
// be persisted, erroring is safer than writing a quadlet on the host-owned default
// port, which systemd's boot autostart would then bind and take the host server down.
func persistPublishedPort(name string, port int) error {
	cfg, err := config.LoadGlobal()
	if err != nil || cfg == nil {
		return fmt.Errorf("loading global config: %w", err)
	}
	entry := cfg.Services[name]
	entry.PublishedPort = port
	cfg.Services[name] = entry
	if err := config.SaveGlobal(cfg); err != nil {
		return fmt.Errorf("saving published port %d for %s: %w", port, name, err)
	}
	return nil
}

// WithURLPort returns rawURL with its host port set to port, preserving scheme,
// userinfo, host, and path. Used to keep a service's developer-facing connection
// URL in sync after its published port is overridden. Returns the input unchanged
// when it is empty, unparseable, or has no host.
func WithURLPort(rawURL string, port int) string {
	if rawURL == "" || port <= 0 {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return rawURL
	}
	u.Host = net.JoinHostPort(u.Hostname(), strconv.Itoa(port))
	return u.String()
}

// PhaseEvent is one step of the streaming preset-install flow.
type PhaseEvent struct {
	Phase   string `json:"phase"`
	Image   string `json:"image,omitempty"`
	Message string `json:"message,omitempty"`
	Dep     string `json:"dep,omitempty"`
	State   string `json:"state,omitempty"`
	Unit    string `json:"unit,omitempty"`
}

// InstallPresetStreaming runs the full install flow and emits a PhaseEvent
// at every step. The image is pulled before the service is registered (config
// + quadlet) so a failed pull never leaves a registered-but-broken service
// behind; pulling here also turns the hidden on-demand pull latency into
// visible progress in the UI. Registration precedes the dependency-start loop
// so a dependency that fails to start still leaves the service installed on
// disk; this matters for reinstall, which has already removed the prior copy.
func InstallPresetStreaming(name, version string, emit func(PhaseEvent)) (*config.CustomService, error) {
	svc, err := resolvePresetForInstall(name, version)
	if err != nil {
		return nil, err
	}

	if svc.Image != "" && !podman.ImageExists(svc.Image) {
		emit(PhaseEvent{Phase: "pulling_image", Image: svc.Image})
		pullErr := podman.PullImageWithProgress(svc.Image, func(line string) {
			emit(PhaseEvent{Phase: "pulling_image", Message: line})
		})
		if pullErr != nil {
			return nil, pullErr
		}
	}

	emit(PhaseEvent{Phase: "installing_config"})
	if err := registerPreset(svc); err != nil {
		return nil, err
	}

	for _, dep := range svc.DependsOn {
		emit(PhaseEvent{Phase: "starting_deps", Dep: dep, State: "starting"})
		if err := EnsureServiceRunning(dep); err != nil {
			return svc, fmt.Errorf("starting dependency %q: %w", dep, err)
		}
		emit(PhaseEvent{Phase: "starting_deps", Dep: dep, State: "ready"})
	}

	unit := "lerd-" + svc.Name
	emit(PhaseEvent{Phase: "starting_unit", Unit: unit})
	var startErr error
	for attempt := range 5 {
		startErr = podman.StartUnit(unit)
		if startErr == nil || !strings.Contains(startErr.Error(), "not found") {
			break
		}
		time.Sleep(time.Duration(attempt+1) * 300 * time.Millisecond)
	}
	if startErr != nil {
		return svc, startErr
	}
	_ = config.SetServicePaused(svc.Name, false)
	_ = config.SetServiceManuallyStarted(svc.Name, true)

	emit(PhaseEvent{Phase: "waiting_ready", Unit: unit})
	if err := podman.WaitReady(svc.Name, 60*time.Second); err != nil {
		return svc, err
	}
	return svc, nil
}

// InstallPresetByName materialises a bundled preset as a custom service.
// version selects a tag for multi-version presets; empty falls back to the
// preset's DefaultVersion. It resolves and registers in one shot; callers that
// need to pull the image before registering (so a failed pull leaves nothing
// behind) should use resolvePresetForInstall + registerPreset instead.
func InstallPresetByName(name, version string) (*config.CustomService, error) {
	svc, err := resolvePresetForInstall(name, version)
	if err != nil {
		return nil, err
	}
	if err := registerPreset(svc); err != nil {
		return nil, err
	}
	return svc, nil
}

// resolvePresetForInstall loads and validates a preset into a CustomService
// without writing anything to disk. Separating resolution from registration
// lets the streaming install pull the image first and bail before any state is
// written when the pull fails.
func resolvePresetForInstall(name, version string) (*config.CustomService, error) {
	preset, err := config.LoadPreset(name)
	if err != nil {
		return nil, err
	}
	if version != "" && len(preset.Versions) == 0 {
		return nil, fmt.Errorf("preset %q does not declare versions", name)
	}
	svc, err := preset.Resolve(version)
	if err != nil {
		return nil, err
	}
	if IsBuiltin(svc.Name) {
		return nil, fmt.Errorf("%q collides with the built-in service of the same name", svc.Name)
	}
	// Quadlet presence is the install-state truth (see ServiceInstalled); a
	// yaml-only remnant from a partial install gets silently rewritten by
	// registerPreset as the heal path.
	if ServiceInstalled(svc.Name) {
		return nil, fmt.Errorf("custom service %q already exists; remove it first with: lerd service remove %s", svc.Name, svc.Name)
	}
	if missing := MissingPresetDependencies(svc); len(missing) > 0 {
		return nil, fmt.Errorf("preset %q requires service(s) %s to be installed first", svc.Name, strings.Join(missing, ", "))
	}
	return svc, nil
}

// registerPreset persists a resolved preset: it saves the YAML config, writes
// the quadlet, and regenerates family consumers. Run only after any required
// image pull has succeeded.
func registerPreset(svc *config.CustomService) error {
	if err := config.SaveCustomService(svc); err != nil {
		return fmt.Errorf("saving service config: %w", err)
	}
	if err := EnsureCustomServiceQuadlet(svc); err != nil {
		return fmt.Errorf("writing quadlet: %w", err)
	}
	if svc.Family != "" {
		RegenerateFamilyConsumers(svc.Family)
	}
	return nil
}

// MissingPresetDependencies returns declared dependencies that are not
// installed. A dependency the service discovers via discover_family is met by
// any installed member of that family or a sibling family it co-discovers.
func MissingPresetDependencies(svc *config.CustomService) []string {
	var missing []string
	for _, dep := range svc.DependsOn {
		if dependencyInstalled(svc, dep) {
			continue
		}
		missing = append(missing, dep)
	}
	return missing
}

// dependencyInstalled reports whether dep is met by an exact service match
// (quadlet presence) or by any installed member of a family that satisfies it.
func dependencyInstalled(svc *config.CustomService, dep string) bool {
	if ServiceInstalled(dep) {
		return true
	}
	for fam := range satisfyingFamilies(svc, dep) {
		for _, host := range config.ServicesInFamily(fam) {
			if ServiceInstalled(strings.TrimPrefix(host, "lerd-")) {
				return true
			}
		}
	}
	return false
}

// satisfyingFamilies returns the families the service co-discovers with dep via
// discover_family (empty if it discovers none): a tool that discovers a family
// can use any member, so phpmyadmin's mysql dep is also met by a mariadb.
func satisfyingFamilies(svc *config.CustomService, dep string) map[string]bool {
	out := map[string]bool{}
	for _, directive := range svc.DynamicEnv {
		parts := strings.SplitN(directive, ":", 2)
		if len(parts) != 2 || parts[0] != "discover_family" {
			continue
		}
		fams := strings.Split(parts[1], ",")
		listed := false
		for _, f := range fams {
			if strings.TrimSpace(f) == dep {
				listed = true
				break
			}
		}
		if !listed {
			continue
		}
		for _, f := range fams {
			if f = strings.TrimSpace(f); config.IsKnownFamily(f) {
				out[f] = true
			}
		}
	}
	return out
}

// EnsureDefaultPresetQuadlet writes the quadlet for a default-preset service
// (mysql, postgres, redis, ...) by resolving the canonical CustomService from
// its YAML preset, layering the user's image / extra-port overrides from
// global config, applying the platform-specific image override last (matching
// the legacy "platform override wins" semantics), and finally writing through
// the shared custom-service quadlet writer.
//
// This replaces the older embedded-template flow (cli.ensureServiceQuadlet)
// so default services and add-on presets share one code path.
func EnsureDefaultPresetQuadlet(name string) error {
	return EnsureDefaultPresetQuadletPinned(name, "")
}

// EnsureDefaultPresetQuadletPinned is the reinstall-aware sibling of
// EnsureDefaultPresetQuadlet. When pinnedImage is non-empty, it is used as
// the source-of-truth for the Image= line, taking precedence over both the
// preset.Image fallback and the on-disk preserved image. Reinstall captures
// the on-disk image *before* RemoveService deletes the quadlet, then passes
// it here so the fresh install pins the same tag the user was running —
// otherwise the rolling preset.Image bump that the v1.19.0-beta.6 fix was
// designed to prevent fires on every reinstall.
//
// Callers outside the reinstall path should use EnsureDefaultPresetQuadlet
// (which passes pinnedImage="").
func EnsureDefaultPresetQuadletPinned(name, pinnedImage string) error {
	if !config.IsDefaultPreset(name) {
		return fmt.Errorf("not a default preset: %q", name)
	}
	p, err := config.LoadPreset(name)
	if err != nil {
		return err
	}
	canonicalPin := ""
	pinnedUserImage := ""
	var extraPorts []string
	if cfg, loadErr := config.LoadGlobal(); loadErr == nil {
		if svcCfg, ok := cfg.Services[name]; ok {
			canonicalPin = svcCfg.CanonicalVersion
			pinnedUserImage = svcCfg.Image
			extraPorts = svcCfg.ExtraPorts
		}
	}
	// The published-port override and the generic port-availability guard are now
	// applied once, downstream, in EnsureCustomServiceQuadlet (the choke point every
	// service quadlet passes through), so this preset path just resolves the default
	// ports and hands the service off.
	hasUserPin := pinnedUserImage != ""
	// Backfill for pre-existing installs that pre-date this feature: if no
	// pin is recorded but a container is running, derive the major from the
	// installed image tag and pin against the matching version.
	if canonicalPin == "" && len(p.Versions) > 0 {
		var probe string
		if hasUserPin {
			probe = pinnedUserImage
		} else {
			probe = podman.InstalledImage("lerd-" + name)
		}
		if probe != "" {
			canonicalPin = matchVersionByImageTag(probe, p.Versions)
		}
	}
	var svc *config.CustomService
	if canonicalPin != "" && len(p.Versions) > 0 {
		svc, err = p.ResolvePinned(canonicalPin)
	} else {
		svc, err = p.Resolve("")
	}
	if err != nil {
		return err
	}
	if hasUserPin {
		svc.Image = pinnedUserImage
	}
	if len(extraPorts) > 0 {
		svc.Ports = append(svc.Ports, extraPorts...)
	}
	// First-install / backfill pin: persist the canonical tag so future YAML
	// canonical flips don't silently major-jump this install.
	if canonicalPin == "" && len(p.Versions) > 0 {
		canonicalPin = p.CanonicalTag()
	}
	if canonicalPin != "" {
		if cfg, _ := config.LoadGlobal(); cfg != nil {
			entry := cfg.Services[name]
			if entry.CanonicalVersion != canonicalPin {
				entry.CanonicalVersion = canonicalPin
				cfg.Services[name] = entry
				_ = config.SaveGlobal(cfg)
			}
		}
	}
	preservedExisting := false
	if pinnedImage != "" {
		// Reinstall path: preserve the user's pre-remove tag verbatim. Skip
		// the strategy / track_latest blocks below so a reinstall really
		// reinstalls "the same thing", not "the same thing + an upgrade".
		svc.Image = pinnedImage
		preservedExisting = true
	} else if !hasUserPin {
		// Honor the on-disk image when the preset's update_strategy says we
		// shouldn't auto-jump to a newer line. Without this, the install rewrite
		// (`lerd update` → `install --from-update` → this function) silently bumps
		// users from their installed minor (e.g. meilisearch v1.7.x) to whatever
		// the new preset.Image declares (v1.42), bypassing the per-service
		// migration UX that `lerd service update` enforces. Rolling-strategy
		// services (mailpit, rustfs, gotenberg) intentionally fall through to the
		// preset image and the track_latest block below.
		strategy := registry.Strategy(p.UpdateStrategy)
		if strategy == registry.StrategyPatch || strategy == registry.StrategyMinor || strategy == registry.StrategyNone {
			if installed := podman.InstalledImage("lerd-" + name); installed != "" {
				svc.Image = installed
				preservedExisting = true
				if strategy != registry.StrategyNone {
					if newer, _ := registry.MaybeNewerTag(installed, strategy); newer != nil {
						if at := strings.LastIndex(svc.Image, ":"); at > 0 {
							svc.Image = svc.Image[:at] + ":" + newer.Name
						}
					}
				}
			}
		}
	}
	// track_latest: when there's no user pin and we did not preserve an
	// existing on-disk image, query the registry for the actual newest tag
	// in the current major + variant line. The YAML preset.Image stays as a
	// fallback when the registry is unreachable.
	if !hasUserPin && !preservedExisting && p.TrackLatest {
		if latest, _ := registry.NewestStable(svc.Image, p.AllowMajorUpgrade); latest != nil {
			if at := strings.LastIndex(svc.Image, ":"); at > 0 {
				svc.Image = svc.Image[:at] + ":" + latest.Name
			}
		}
	}
	p.ApplyPlatformOverride(svc, runtime.GOOS)
	return EnsureCustomServiceQuadlet(svc)
}

// matchVersionByImageTag picks the longest version tag that is a prefix of
// the installed image's tag. Lets backfill recognise postgis:16.5-3.5-alpine
// as version "16" and mysql:8.4.9 as version "8.4".
func matchVersionByImageTag(image string, versions []config.PresetVersion) string {
	// Exact full-image match first: presets whose image tag isn't derived from
	// the version string (e.g. timescaledb's …/timescaledb:latest-pg17 for
	// version "17") can only be recovered this way. Without it the tag heuristic
	// below returns "", and canonical-pin sync silently flips the major on update.
	for _, v := range versions {
		if v.Image != "" && image == v.Image {
			return v.Tag
		}
	}
	at := strings.LastIndex(image, ":")
	if at < 0 {
		return ""
	}
	tag := image[at+1:]
	best := ""
	for _, v := range versions {
		if tag == v.Tag || strings.HasPrefix(tag, v.Tag+".") || strings.HasPrefix(tag, v.Tag+"-") {
			if len(v.Tag) > len(best) {
				best = v.Tag
			}
		}
	}
	return best
}

// EnsureCustomServiceQuadlet writes the quadlet for a custom service and
// reloads systemd only when the file actually changed on disk. Materialises
// any declared file mounts and resolves dynamic_env directives so the
// rendered quadlet has the computed values.
func EnsureCustomServiceQuadlet(svc *config.CustomService) error {
	// Generic port-ownership guard — the single place every service quadlet
	// (DB presets, redis, meilisearch, custom) passes through. When this service
	// has no published port recorded yet and its primary host port can't be bound,
	// shift to the next free port and persist it. Port availability is the ONLY
	// signal: lerd never inspects host files, sockets, or binaries. Persist FIRST,
	// failing closed, so the quadlet never publishes a port the config doesn't
	// record. Once a port is recorded it sticks (the published_port>0 apply below
	// short-circuits the probe), never auto-reverting — `lerd service port` changes it.
	pp := config.ServicePublishedPort(svc.Name)
	if pp == 0 {
		primary := podman.PrimaryHostPort(svc.Ports)
		if free := maybeShiftPublishedPort(primary, unitActive(svc.Name)); free > 0 {
			if err := persistPublishedPort(svc.Name, free); err != nil {
				return fmt.Errorf("shifting lerd-%s off in-use port %d: %w", svc.Name, primary, err)
			}
			pp = free // use the just-persisted value directly — no second config read to diverge
			// Stderr, never stdout: this path runs in-process inside the MCP stdio
			// server, which reserves stdout for the JSON-RPC stream.
			fmt.Fprintf(os.Stderr, "Note: 127.0.0.1:%d is in use; publishing lerd-%s on 127.0.0.1:%d instead.\n", primary, svc.Name, free)
			fmt.Fprintf(os.Stderr, "      (override with: lerd service port %s <port>)\n", svc.Name)
			// Host-proxy sites reach this service over the published loopback port,
			// so their .env must follow the shift. The CLI registers the refresh hook.
			if OnPublishedPortShift != nil {
				OnPublishedPortShift(svc.Name, free)
			}
		}
	}
	// Apply the recorded published port (guard-shifted or set via `lerd service
	// port`) to the primary host mapping and the connection URL, leaving the
	// container-internal port — and every bridge/env reference to it — untouched.
	// 0 means "use the preset/version default", a no-op for the unmoved majority.
	if pp > 0 {
		svc.Ports = podman.SetPrimaryHostPort(svc.Ports, pp)
		svc.ConnectionURL = WithURLPort(svc.ConnectionURL, pp)
	}
	if svc.DataDir != "" {
		if err := os.MkdirAll(config.DataSubDir(svc.Name), 0755); err != nil {
			return fmt.Errorf("creating data directory for %s: %w", svc.Name, err)
		}
	}
	if err := config.MaterializeServiceFiles(svc); err != nil {
		return err
	}
	if err := config.MaterializeServiceTuning(svc); err != nil {
		return err
	}
	if err := config.ResolveDynamicEnv(svc); err != nil {
		return err
	}
	// Re-validate post dynamic_env and for inline services that skip
	// SaveCustomService: this is the choke point every quadlet passes through.
	if err := config.ValidateCustomService(svc); err != nil {
		return err
	}
	content := podman.GenerateCustomQuadlet(svc)
	quadletName := "lerd-" + svc.Name
	changed, err := podman.WriteQuadletDiff(quadletName, content)
	if err != nil {
		return fmt.Errorf("writing unit for %s: %w", svc.Name, err)
	}
	return podman.DaemonReloadIfNeeded(changed)
}

// EnsureServiceRunning starts the service if it is not already active and
// waits until it is ready. Recurses through depends_on for custom services.
func EnsureServiceRunning(name string) error {
	unit := "lerd-" + name
	status, _ := podman.UnitStatus(unit)
	if status == "active" {
		if err := podman.WaitReady(name, 30*time.Second); err != nil {
			return fmt.Errorf("%s is active but not yet ready: %w", name, err)
		}
		return nil
	}
	if !IsBuiltin(name) {
		svc, err := config.LoadCustomService(name)
		if err != nil {
			return fmt.Errorf("custom service %q not found: %w", name, err)
		}
		for _, dep := range svc.DependsOn {
			if err := EnsureServiceRunning(dep); err != nil {
				return fmt.Errorf("starting dependency %q for %q: %w", dep, name, err)
			}
		}
		if err := EnsureCustomServiceQuadlet(svc); err != nil {
			return err
		}
	}
	if err := podman.StartUnit(unit); err != nil {
		return err
	}
	return podman.WaitReady(name, 60*time.Second)
}

// StartDependencies ensures every entry in svc.DependsOn is up and ready
// before the parent is started.
func StartDependencies(svc *config.CustomService) error {
	if svc == nil {
		return nil
	}
	for _, dep := range svc.DependsOn {
		if err := EnsureServiceRunning(dep); err != nil {
			return fmt.Errorf("starting dependency %q for %q: %w", dep, svc.Name, err)
		}
	}
	return nil
}

// StopWithDependents stops every custom service that depends on name
// (depth-first), then stops name itself.
func StopWithDependents(name string) {
	for _, dep := range config.CustomServicesDependingOn(name) {
		StopWithDependents(dep)
	}
	unit := "lerd-" + name
	status, _ := podman.UnitStatus(unit)
	if status == "active" || status == "activating" {
		// Show the service name (not the lerd- unit) in the shared feedback
		// vocabulary, so `lerd unlink`/`lerd stop` read as "stopping meilisearch"
		// rather than the old "Stopping lerd-meilisearch...".
		step := feedback.Start("stopping " + name)
		_ = podman.StopUnit(unit)
		step.OK("")
	}
}

// ServiceFamily returns the family of a service by name. Honours the
// explicit Family field on a custom service first, falls back to
// config.InferFamily for built-ins and pattern-matched alternates.
func ServiceFamily(name string) string { return config.FamilyOfName(name) }

// RegenerateFamilyConsumersForService is a convenience that wraps
// RegenerateFamilyConsumers in a no-op when name has no recognised family.
func RegenerateFamilyConsumersForService(name string) {
	if fam := ServiceFamily(name); fam != "" {
		RegenerateFamilyConsumers(fam)
	}
}

// RegenerateFamilyConsumers re-renders the quadlet of any installed custom
// service whose dynamic_env references the named family. Active consumers
// are stopped, removed, and started so the new generated unit is the one
// systemd loads.
func RegenerateFamilyConsumers(family string) {
	customs, err := config.ListCustomServices()
	if err != nil {
		return
	}
	for _, c := range customs {
		if !consumesFamily(c, family) {
			continue
		}
		if err := EnsureCustomServiceQuadlet(c); err != nil {
			fmt.Printf("  [WARN] regenerating %s quadlet: %v\n", c.Name, err)
			continue
		}
		unit := "lerd-" + c.Name
		status, _ := podman.UnitStatus(unit)
		if status != "active" && status != "activating" {
			continue
		}
		fmt.Printf("  Restarting %s to pick up updated %s family members...\n", unit, family)
		if err := podman.StopUnit(unit); err != nil {
			fmt.Printf("  [WARN] stopping %s: %v\n", unit, err)
		}
		podman.RemoveContainer(unit)
		if err := podman.StartUnit(unit); err != nil {
			fmt.Printf("  [WARN] starting %s: %v\n", unit, err)
		}
	}
}

func consumesFamily(svc *config.CustomService, family string) bool {
	for _, directive := range svc.DynamicEnv {
		parts := strings.SplitN(directive, ":", 2)
		if len(parts) != 2 || parts[0] != "discover_family" {
			continue
		}
		for _, fam := range strings.Split(parts[1], ",") {
			if strings.TrimSpace(fam) == family {
				return true
			}
		}
	}
	return false
}
