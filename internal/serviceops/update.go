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
// UI can render an "update available → v8.4.3" badge without applying it.
type UpdateAvailability struct {
	Service       string `json:"service"`
	CurrentImage  string `json:"current_image"`
	CurrentTag    string `json:"current_tag"`
	LatestTag     string `json:"latest_tag,omitempty"`
	LatestImage   string `json:"latest_image,omitempty"`
	Available     bool   `json:"available"`
	Strategy      string `json:"strategy"`
	UpgradeTag    string `json:"upgrade_tag,omitempty"`
	UpgradeImage  string `json:"upgrade_image,omitempty"`
	PreviousImage string `json:"previous_image,omitempty"`
	// CanRollback is false when the most recent op was a migrate (rolling the
	// image back without restoring the pre-migrate data dir would corrupt it).
	CanRollback bool `json:"can_rollback"`
}

// CheckUpdateAvailable queries the registry for a newer tag matching the
// preset's update_strategy. Network and unsupported-registry errors are
// swallowed so the UI stays quiet on offline / custom-registry installs.
// Successful results are cached for updateAvailabilityTTL so snapshot
// rebuilds don't fork a `podman image inspect` per service per rebuild.
func CheckUpdateAvailable(name string) (*UpdateAvailability, error) {
	if cached := cachedUpdateAvailability(name); cached != nil {
		return cached, nil
	}
	out, err := computeUpdateAvailable(name)
	if err == nil && out != nil {
		storeUpdateAvailability(name, out)
	}
	return out, err
}

func computeUpdateAvailable(name string) (*UpdateAvailability, error) {
	svc, strategy, allowMajor, err := resolveServiceForUpdate(name)
	if err != nil {
		return nil, err
	}
	prevImage, _, lastOp, _ := previousImageFor(name)
	out := &UpdateAvailability{
		Service:       name,
		CurrentImage:  svc.Image,
		Strategy:      string(strategy),
		PreviousImage: prevImage,
		CanRollback:   prevImage != "" && lastOp != "migrate",
	}
	ref, parseErr := registry.ParseImage(svc.Image)
	if parseErr == nil {
		out.CurrentTag = ref.Tag
	}
	var newer *registry.TagInfo
	if strategy != registry.StrategyNone && strategy != "" {
		newer, _ = registry.MaybeNewerTag(svc.Image, strategy)
		if newer != nil && newer.Digest != "" && alreadyOnDigest(name, svc.Image, newer.Digest) {
			newer = nil
		}
	}
	if newer != nil {
		out.Available = true
		out.LatestTag = newer.Name
		if parseErr == nil {
			out.LatestImage = ref.Registry + "/" + ref.Repo + ":" + newer.Name
		}
	}
	// Cross-strategy upgrade: skipped for strategy=none and for patch presets
	// without a registered migrator (otherwise the in-place button is a trap).
	if strategy == registry.StrategyNone || strategy == "" {
		return out, nil
	}
	if strategy == registry.StrategyPatch && !SupportsMigration(name) {
		return out, nil
	}
	if upgrade, _ := registry.NewestStable(svc.Image, allowMajor); upgrade != nil {
		alreadyOn := upgrade.Digest != "" && alreadyOnDigest(name, svc.Image, upgrade.Digest)
		if !alreadyOn && (newer == nil || upgrade.Name != newer.Name) {
			out.UpgradeTag = upgrade.Name
			if parseErr == nil {
				out.UpgradeImage = ref.Registry + "/" + ref.Repo + ":" + upgrade.Name
			}
		}
	}
	return out, nil
}

// alreadyOnDigest reports whether a local image already matches the candidate
// digest. Both sides are lowercased so registry casing differences don't
// register as a mismatch.
func alreadyOnDigest(name, configuredImage, candidate string) bool {
	if candidate == "" {
		return false
	}
	want := strings.ToLower(strings.TrimSpace(candidate))
	check := func(local string) bool {
		return strings.ToLower(strings.TrimSpace(local)) == want
	}
	for _, local := range podman.LocalImageDigest(configuredImage) {
		if check(local) {
			return true
		}
	}
	if installed := podman.InstalledImage("lerd-" + name); installed != "" && installed != configuredImage {
		for _, local := range podman.LocalImageDigest(installed) {
			if check(local) {
				return true
			}
		}
	}
	return false
}

// UpdateServiceStreaming pulls the chosen image, persists it, rewrites the
// quadlet, and restarts the unit. Phases: checking_registry, pulling_image,
// writing_quadlet, restarting_unit, done.
func UpdateServiceStreaming(name, targetImage string, emit func(PhaseEvent)) error {
	unlock := lockService(name)
	defer unlock()

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
	} else if err := enforceMajorUpgradeGate(name, chosenImage); err != nil {
		return err
	}

	emit(PhaseEvent{Phase: "pulling_image", Image: chosenImage})
	if err := podman.PullImageWithProgress(chosenImage, func(line string) {
		emit(PhaseEvent{Phase: "pulling_image", Message: line})
	}); err != nil {
		return fmt.Errorf("pulling %s: %w", chosenImage, err)
	}

	emit(PhaseEvent{Phase: "writing_quadlet", Image: chosenImage})
	if err := persistImageChoice(name, chosenImage, "update"); err != nil {
		return err
	}

	unit := "lerd-" + name
	emit(PhaseEvent{Phase: "restarting_unit", Unit: unit})
	if err := restartWithRetry(unit); err != nil {
		return err
	}
	emit(PhaseEvent{Phase: "done", Image: chosenImage, Unit: unit})
	return nil
}

// restartWithRetry handles the "unit not found" race after writing a fresh
// quadlet. Other errors return immediately. Surfaces the last error rather
// than swallowing it after retries.
func restartWithRetry(unit string) error {
	var lastErr error
	for attempt := range 5 {
		err := podman.RestartUnit(unit)
		if err == nil {
			return nil
		}
		lastErr = err
		if !strings.Contains(err.Error(), "not found") {
			return err
		}
		time.Sleep(time.Duration(attempt+1) * 300 * time.Millisecond)
	}
	if lastErr == nil {
		return fmt.Errorf("restarting %s: gave up after retries", unit)
	}
	return lastErr
}

// enforceMajorUpgradeGate refuses an explicit cross-major target when the
// preset has not opted in via allow_major_upgrade — same gate as the registry
// recommendation path, so a CLI tag arg can't bypass it.
func enforceMajorUpgradeGate(name, target string) error {
	svc, _, allowMajor, err := resolveServiceForUpdate(name)
	if err != nil || svc == nil {
		return err
	}
	if allowMajor {
		return nil
	}
	currentRef, perr := registry.ParseImage(svc.Image)
	if perr != nil {
		return nil
	}
	targetRef, perr := registry.ParseImage(target)
	if perr != nil {
		return nil
	}
	cur := leadingMajor(currentRef.Tag)
	tgt := leadingMajor(targetRef.Tag)
	if cur < 0 || tgt < 0 || cur == tgt {
		return nil
	}
	return fmt.Errorf("refusing major-version jump %s → %s for %s; preset has allow_major_upgrade=false (use the Migrate flow instead)", currentRef.Tag, targetRef.Tag, name)
}

func leadingMajor(tag string) int {
	t := strings.TrimPrefix(tag, "v")
	end := 0
	for end < len(t) && t[end] >= '0' && t[end] <= '9' {
		end++
	}
	if end == 0 {
		return -1
	}
	n := 0
	for i := 0; i < end; i++ {
		n = n*10 + int(t[i]-'0')
	}
	return n
}

// resolveServiceForUpdate returns the resolved CustomService, update strategy,
// and major-upgrade policy. Alternates installed via service preset (e.g.
// mysql-8-0) downgrade the preset's strategy to patch.
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
		// Prefer the installed image — track_latest can drift the resolved
		// canonical ahead of what's actually on disk.
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

// persistImageChoice records the chosen image and regenerates the quadlet.
// Atomic: if the quadlet write fails the config write is rolled back so the
// on-disk pair (config + quadlet) stays consistent.
func persistImageChoice(name, newImage, op string) error {
	// Drop the cached availability so the next CheckUpdateAvailable reflects
	// the new image instead of the pre-mutation snapshot.
	defer invalidateUpdateAvailability(name)

	if config.IsDefaultPreset(name) {
		cfg, err := config.LoadGlobal()
		if err != nil {
			return err
		}
		prev := cfg.Services[name]
		next := prev
		if next.Image != "" && next.Image != newImage {
			next.PreviousImage = next.Image
		}
		next.Image = newImage
		next.LastOp = op
		if op != "migrate" {
			next.PreMigrateBackup = ""
		}
		cfg.Services[name] = next
		if err := config.SaveGlobal(cfg); err != nil {
			return fmt.Errorf("saving global config: %w", err)
		}
		if err := EnsureDefaultPresetQuadlet(name); err != nil {
			cfg.Services[name] = prev
			if rbErr := config.SaveGlobal(cfg); rbErr != nil {
				return fmt.Errorf("writing quadlet failed: %w; AND rolling config back failed: %v", err, rbErr)
			}
			return fmt.Errorf("writing quadlet: %w", err)
		}
		return nil
	}
	svc, err := config.LoadCustomService(name)
	if err != nil {
		return err
	}
	prevSvc := *svc
	if svc.Image != "" && svc.Image != newImage {
		svc.PreviousImage = svc.Image
	}
	svc.Image = newImage
	svc.LastOp = op
	if op != "migrate" {
		svc.PreMigrateBackup = ""
	}
	if err := config.SaveCustomService(svc); err != nil {
		return fmt.Errorf("saving service config: %w", err)
	}
	if err := EnsureCustomServiceQuadlet(svc); err != nil {
		if rbErr := config.SaveCustomService(&prevSvc); rbErr != nil {
			return fmt.Errorf("writing quadlet failed: %w; AND rolling config back failed: %v", err, rbErr)
		}
		return fmt.Errorf("writing quadlet: %w", err)
	}
	return nil
}

// recordMigrateBackup stamps the post-migrate state onto the service config
// so a later rollback can detect and refuse the unsafe path.
func recordMigrateBackup(name, backup string) error {
	if config.IsDefaultPreset(name) {
		cfg, err := config.LoadGlobal()
		if err != nil {
			return err
		}
		entry := cfg.Services[name]
		entry.LastOp = "migrate"
		entry.PreMigrateBackup = backup
		cfg.Services[name] = entry
		return config.SaveGlobal(cfg)
	}
	svc, err := config.LoadCustomService(name)
	if err != nil {
		return err
	}
	svc.LastOp = "migrate"
	svc.PreMigrateBackup = backup
	return config.SaveCustomService(svc)
}

// RollbackService swaps a service back to its previously-running image.
// Refuses when no previous image is recorded or when the most recent op was
// a migrate (binary/schema mismatch would corrupt the data dir).
func RollbackService(name string, emit func(PhaseEvent)) error {
	unlock := lockService(name)
	defer unlock()

	prev, current, lastOp, err := previousImageFor(name)
	if err != nil {
		return err
	}
	if prev == "" {
		return fmt.Errorf("no previous image recorded for %s — nothing to roll back to", name)
	}
	if lastOp == "migrate" {
		return fmt.Errorf("refusing rollback for %s: last op was a migrate. Restoring %s would mismatch binary against schema. Restore the pre-migrate data dir manually if you really want to revert", name, prev)
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
	if err := restartWithRetry(unit); err != nil {
		return err
	}
	emit(PhaseEvent{Phase: "done", Image: prev, Unit: unit})
	return nil
}

// previousImageFor returns the recorded previous image, current pinned image,
// and the kind of the most recent op for a service. Any field may be empty.
func previousImageFor(name string) (prev, current, lastOp string, err error) {
	if config.IsDefaultPreset(name) {
		cfg, lErr := config.LoadGlobal()
		if lErr != nil {
			return "", "", "", lErr
		}
		svcCfg := cfg.Services[name]
		return svcCfg.PreviousImage, svcCfg.Image, svcCfg.LastOp, nil
	}
	svc, lErr := config.LoadCustomService(name)
	if lErr != nil {
		return "", "", "", fmt.Errorf("unknown service %q", name)
	}
	return svc.PreviousImage, svc.Image, svc.LastOp, nil
}

// swapImagePin moves PreviousImage→Image and old Image→PreviousImage so the
// rollback is reversible. Atomic: rolls back the config write if the quadlet
// regeneration fails.
func swapImagePin(name, newImage, newPrev string) error {
	if config.IsDefaultPreset(name) {
		cfg, err := config.LoadGlobal()
		if err != nil {
			return err
		}
		prev := cfg.Services[name]
		next := prev
		next.Image = newImage
		next.PreviousImage = newPrev
		next.LastOp = "update"
		next.PreMigrateBackup = ""
		cfg.Services[name] = next
		if err := config.SaveGlobal(cfg); err != nil {
			return fmt.Errorf("saving global config: %w", err)
		}
		if err := EnsureDefaultPresetQuadlet(name); err != nil {
			cfg.Services[name] = prev
			if rbErr := config.SaveGlobal(cfg); rbErr != nil {
				return fmt.Errorf("writing quadlet failed: %w; AND rolling config back failed: %v", err, rbErr)
			}
			return fmt.Errorf("writing quadlet: %w", err)
		}
		return nil
	}
	svc, err := config.LoadCustomService(name)
	if err != nil {
		return err
	}
	prevSvc := *svc
	svc.Image = newImage
	svc.PreviousImage = newPrev
	svc.LastOp = "update"
	svc.PreMigrateBackup = ""
	if err := config.SaveCustomService(svc); err != nil {
		return fmt.Errorf("saving service config: %w", err)
	}
	if err := EnsureCustomServiceQuadlet(svc); err != nil {
		if rbErr := config.SaveCustomService(&prevSvc); rbErr != nil {
			return fmt.Errorf("writing quadlet failed: %w; AND rolling config back failed: %v", err, rbErr)
		}
		return fmt.Errorf("writing quadlet: %w", err)
	}
	return nil
}
