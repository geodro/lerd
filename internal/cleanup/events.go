package cleanup

import "github.com/geodro/lerd/internal/config"

// autoEnabled reports whether automatic cleanup is on. Seam for tests.
var autoEnabled = defaultAutoEnabled

func defaultAutoEnabled() bool {
	cfg, err := config.LoadGlobal()
	return err == nil && cfg.AutoCleanupEnabled()
}

// SweepSafe runs the safe-tier cleanup immediately when auto_cleanup is on. It
// is the event hook for the moment a PHP image rebuild orphans the old image,
// reaping it at once instead of waiting for the daily watcher. Returns the
// number of images reaped and bytes freed; err is non-nil only when the image
// scan itself failed, so the watcher can tell a transient failure (retry) from
// "nothing to do" (throttle). All-zero with nil err when disabled or clean.
func SweepSafe() (images int, bytes int64, err error) {
	if !autoEnabled() {
		return 0, 0, nil
	}
	plan, err := Inspect(false)
	if err != nil {
		return 0, 0, err
	}
	return len(plan.Targets), Apply(plan), nil
}

// SweepRefs reaps the exact image references lerd is dropping (the superseded
// version after a service update, the removed service's images after a remove).
// Each ref is one lerd itself recorded, so it is provably lerd's; a ref another
// service still references (in the protected set) is skipped. Reaping precise
// refs rather than a whole repo means a user's own same-repo image is never
// touched. Gated by auto_cleanup.
func SweepRefs(refs ...string) {
	if !autoEnabled() {
		return
	}
	// De-dup the non-empty refs into canonical candidates, preserving order so a
	// repeated ref is reaped once.
	candidates := map[string]bool{}
	var order []string
	for _, ref := range refs {
		c := canonRef(ref)
		if ref == "" || candidates[c] {
			continue
		}
		candidates[c] = true
		order = append(order, ref)
	}
	if len(candidates) == 0 {
		return
	}
	protected := referencedImages(candidates)
	for _, ref := range order {
		if protected[canonRef(ref)] {
			continue
		}
		_ = removeImage(ref)
	}
}
