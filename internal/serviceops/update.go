package serviceops

import (
	"fmt"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/registry"
)

// UpdateAvailability is the metadata returned by CheckUpdateAvailable so the
// UI can render an "update available → v8.4.3" badge without performing the
// update yet. Latest* fields surface newer versions outside the safe-update
// strategy (e.g. meilisearch v1.7.6 patch-update + v1.42.1 cross-minor) so
// the UI can offer them as an explicit, manual-migration upgrade.
type UpdateAvailability struct {
	Service      string `json:"service"`
	CurrentImage string `json:"current_image"`
	CurrentTag   string `json:"current_tag"`
	LatestTag    string `json:"latest_tag,omitempty"`
	LatestImage  string `json:"latest_image,omitempty"`
	Available    bool   `json:"available"`
	Strategy     string `json:"strategy"`
	// Upgrade* points at the newest version regardless of update_strategy.
	// Set only when it differs from LatestTag, i.e. there's a cross-strategy
	// upgrade the user could opt into manually.
	UpgradeTag   string `json:"upgrade_tag,omitempty"`
	UpgradeImage string `json:"upgrade_image,omitempty"`
	// PreviousImage is the image running before the most recent update; set
	// when the user can roll back to it without an extra registry roundtrip.
	PreviousImage string `json:"previous_image,omitempty"`
}

// CheckUpdateAvailable queries the registry for a newer tag matching the
// preset's update_strategy. Network and unsupported-registry errors are
// swallowed (returning Available=false) so the UI stays quiet on offline /
// custom-registry installs.
func CheckUpdateAvailable(name string) (*UpdateAvailability, error) {
	svc, strategy, allowMajor, err := resolveServiceForUpdate(name)
	if err != nil {
		return nil, err
	}
	prevImage, _, _ := previousImageFor(name)
	out := &UpdateAvailability{
		Service:       name,
		CurrentImage:  svc.Image,
		Strategy:      string(strategy),
		PreviousImage: prevImage,
	}
	ref, parseErr := registry.ParseImage(svc.Image)
	if parseErr == nil {
		out.CurrentTag = ref.Tag
	}
	// The safe in-strategy update suggestion (auto-applicable, no migration).
	var newer *registry.TagInfo
	if strategy != registry.StrategyNone && strategy != "" {
		newer, _ = registry.MaybeNewerTag(svc.Image, strategy)
		// If the locally-pulled image already has the same digest as the
		// recommended candidate, the user is effectively already on it.
		// Probe both the configured tag (svc.Image) and whatever's actually
		// in the on-disk quadlet (which may have drifted, e.g. config says
		// :8.4 but quadlet pinned :8.4.9 from a previous update).
		if newer != nil && newer.Digest != "" {
			candidates := podman.LocalImageDigest(svc.Image)
			if installed := podman.InstalledImage("lerd-" + name); installed != "" && installed != svc.Image {
				candidates = append(candidates, podman.LocalImageDigest(installed)...)
			}
			for _, local := range candidates {
				if local == newer.Digest {
					newer = nil
					break
				}
			}
		}
	}
	if newer != nil {
		out.Available = true
		out.LatestTag = newer.Name
		if parseErr == nil {
			out.LatestImage = ref.Registry + "/" + ref.Repo + ":" + newer.Name
		}
	}
	// The absolute newest stable tag, regardless of strategy. Surfaced as an
	// opt-in cross-strategy upgrade. Cross-major boundaries only when the
	// preset opts in via allow_major_upgrade. Skipped for strategy=none so
	// opted-out presets stay quiet, and skipped for patch-strategy presets
	// without a registered migrator — for those (e.g. meilisearch) crossing
	// the minor boundary corrupts data and an in-place Upgrade button would
	// be a trap; the user must follow upstream's manual upgrade guide.
	if strategy == registry.StrategyNone || strategy == "" {
		return out, nil
	}
	if strategy == registry.StrategyPatch && !SupportsMigration(name) {
		return out, nil
	}
	if upgrade, _ := registry.NewestStable(svc.Image, allowMajor); upgrade != nil {
		alreadyOn := false
		if upgrade.Digest != "" {
			candidates := podman.LocalImageDigest(svc.Image)
			if installed := podman.InstalledImage("lerd-" + name); installed != "" && installed != svc.Image {
				candidates = append(candidates, podman.LocalImageDigest(installed)...)
			}
			for _, local := range candidates {
				if local == upgrade.Digest {
					alreadyOn = true
					break
				}
			}
		}
		if !alreadyOn && (newer == nil || upgrade.Name != newer.Name) {
			out.UpgradeTag = upgrade.Name
			if parseErr == nil {
				out.UpgradeImage = ref.Registry + "/" + ref.Repo + ":" + upgrade.Name
			}
		}
	}
	return out, nil
}

// UpdateServiceStreaming pulls the recommended newer image (or the
// caller-supplied targetImage when non-empty for explicit upgrades), persists
// it (in cfg.Services for default presets, in the on-disk YAML for installed
// custom services), rewrites the quadlet, and restarts the unit. Phase
// events: checking_registry, pulling_image, writing_quadlet, restarting_unit, done.
func UpdateServiceStreaming(name, targetImage string, emit func(PhaseEvent)) error {
	emit(PhaseEvent{Phase: "checking_registry"})
	chosenImage := targetImage
	if chosenImage == "" {
		avail, err := CheckUpdateAvailable(name)
		if err != nil {
			return err
		}
		if !avail.Available || avail.LatestImage == "" {
			emit(PhaseEvent{Phase: "done", Message: "already up to date"})
			return nil
		}
		chosenImage = avail.LatestImage
	}

	emit(PhaseEvent{Phase: "pulling_image", Image: chosenImage})
	if err := podman.PullImageWithProgress(chosenImage, func(line string) {
		emit(PhaseEvent{Phase: "pulling_image", Message: line})
	}); err != nil {
		return fmt.Errorf("pulling %s: %w", chosenImage, err)
	}

	emit(PhaseEvent{Phase: "writing_quadlet", Image: chosenImage})
	if err := persistImageChoice(name, chosenImage); err != nil {
		return err
	}

	unit := "lerd-" + name
	emit(PhaseEvent{Phase: "restarting_unit", Unit: unit})
	var restartErr error
	for attempt := range 5 {
		restartErr = podman.RestartUnit(unit)
		if restartErr == nil || !strings.Contains(restartErr.Error(), "not found") {
			break
		}
		time.Sleep(time.Duration(attempt+1) * 300 * time.Millisecond)
	}
	if restartErr != nil {
		return restartErr
	}
	emit(PhaseEvent{Phase: "done", Image: chosenImage, Unit: unit})
	return nil
}

// resolveServiceForUpdate returns the resolved CustomService, update strategy,
// and major-upgrade policy for either a default preset or an installed
// custom service. Alternates installed via service preset (e.g. mysql-8-0)
// override the preset's broader strategy with patch, so a user who explicitly
// pinned 8.0 doesn't get suggested 8.4.9 — that crosses a minor and (for
// mysql) a documented LTS line. Use the alternates picker for cross-minor.
func resolveServiceForUpdate(name string) (*config.CustomService, registry.Strategy, bool, error) {
	if config.IsDefaultPreset(name) {
		p, err := config.LoadPreset(name)
		if err != nil {
			return nil, "", false, err
		}
		svc, err := p.Resolve("")
		if err != nil {
			return nil, "", false, err
		}
		// Prefer the on-disk quadlet's image — it reflects what's actually
		// running. With track_latest, the freshly-resolved canonical can drift
		// ahead of the installed image (e.g. data dir is v1.7.6 but track_latest
		// would now resolve to v1.42), and the update-check must report the
		// real current state, not the would-be-fresh-install state.
		if installed := podman.InstalledImage("lerd-" + name); installed != "" {
			svc.Image = installed
		}
		if cfg, lErr := config.LoadGlobal(); lErr == nil {
			if svcCfg, ok := cfg.Services[name]; ok && svcCfg.Image != "" {
				svc.Image = svcCfg.Image
			}
		}
		return svc, registry.Strategy(p.UpdateStrategy), p.AllowMajorUpgrade, nil
	}
	svc, err := config.LoadCustomService(name)
	if err != nil {
		return nil, "", false, fmt.Errorf("unknown service %q", name)
	}
	strategy := registry.StrategyNone
	allowMajor := false
	if svc.Preset != "" {
		if p, err := config.LoadPreset(svc.Preset); err == nil {
			strategy = registry.Strategy(p.UpdateStrategy)
			allowMajor = p.AllowMajorUpgrade
			if svc.PresetVersion != "" && svc.Name != p.Name {
				strategy = registry.StrategyPatch
				allowMajor = false
			}
		}
	}
	return svc, strategy, allowMajor, nil
}

// persistImageChoice records the chosen image so a subsequent service start
// picks it up. For default presets this means writing to global config's
// Services[name].Image; for installed custom services it means rewriting the
// on-disk YAML and regenerating the quadlet. The previous image is preserved
// alongside so a subsequent rollback can swap back without re-querying the
// registry.
func persistImageChoice(name, newImage string) error {
	if config.IsDefaultPreset(name) {
		cfg, err := config.LoadGlobal()
		if err != nil {
			return err
		}
		svcCfg := cfg.Services[name]
		if svcCfg.Image != "" && svcCfg.Image != newImage {
			svcCfg.PreviousImage = svcCfg.Image
		}
		svcCfg.Image = newImage
		cfg.Services[name] = svcCfg
		if err := config.SaveGlobal(cfg); err != nil {
			return fmt.Errorf("saving global config: %w", err)
		}
		return EnsureDefaultPresetQuadlet(name)
	}
	svc, err := config.LoadCustomService(name)
	if err != nil {
		return err
	}
	if svc.Image != "" && svc.Image != newImage {
		svc.PreviousImage = svc.Image
	}
	svc.Image = newImage
	if err := config.SaveCustomService(svc); err != nil {
		return fmt.Errorf("saving service config: %w", err)
	}
	return EnsureCustomServiceQuadlet(svc)
}

// RollbackService swaps a service back to its previously-running image.
// The previous image is recorded on every update via persistImageChoice; a
// rollback toggles current/previous so the next rollback redoes the update.
// Errors when no previous image is recorded (nothing to roll back to).
func RollbackService(name string, emit func(PhaseEvent)) error {
	prev, current, err := previousImageFor(name)
	if err != nil {
		return err
	}
	if prev == "" {
		return fmt.Errorf("no previous image recorded for %s — nothing to roll back to", name)
	}
	emit(PhaseEvent{Phase: "pulling_image", Image: prev})
	if err := podman.PullImageWithProgress(prev, func(line string) {
		emit(PhaseEvent{Phase: "pulling_image", Message: line})
	}); err != nil {
		return fmt.Errorf("pulling %s: %w", prev, err)
	}
	emit(PhaseEvent{Phase: "writing_quadlet", Image: prev})
	if err := swapImagePin(name, prev, current); err != nil {
		return err
	}
	unit := "lerd-" + name
	emit(PhaseEvent{Phase: "restarting_unit", Unit: unit})
	var restartErr error
	for attempt := range 5 {
		restartErr = podman.RestartUnit(unit)
		if restartErr == nil || !strings.Contains(restartErr.Error(), "not found") {
			break
		}
		time.Sleep(time.Duration(attempt+1) * 300 * time.Millisecond)
	}
	if restartErr != nil {
		return restartErr
	}
	emit(PhaseEvent{Phase: "done", Image: prev, Unit: unit})
	return nil
}

// previousImageFor returns the recorded previous image and the current pinned
// image for a service. Either may be empty.
func previousImageFor(name string) (prev, current string, err error) {
	if config.IsDefaultPreset(name) {
		cfg, lErr := config.LoadGlobal()
		if lErr != nil {
			return "", "", lErr
		}
		svcCfg := cfg.Services[name]
		return svcCfg.PreviousImage, svcCfg.Image, nil
	}
	svc, lErr := config.LoadCustomService(name)
	if lErr != nil {
		return "", "", fmt.Errorf("unknown service %q", name)
	}
	return svc.PreviousImage, svc.Image, nil
}

// swapImagePin moves PreviousImage→Image and the old Image→PreviousImage so
// the rollback is reversible: clicking rollback again returns to the original.
func swapImagePin(name, newImage, newPrev string) error {
	if config.IsDefaultPreset(name) {
		cfg, err := config.LoadGlobal()
		if err != nil {
			return err
		}
		svcCfg := cfg.Services[name]
		svcCfg.Image = newImage
		svcCfg.PreviousImage = newPrev
		cfg.Services[name] = svcCfg
		if err := config.SaveGlobal(cfg); err != nil {
			return fmt.Errorf("saving global config: %w", err)
		}
		return EnsureDefaultPresetQuadlet(name)
	}
	svc, err := config.LoadCustomService(name)
	if err != nil {
		return err
	}
	svc.Image = newImage
	svc.PreviousImage = newPrev
	if err := config.SaveCustomService(svc); err != nil {
		return fmt.Errorf("saving service config: %w", err)
	}
	return EnsureCustomServiceQuadlet(svc)
}
