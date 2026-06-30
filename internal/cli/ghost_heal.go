package cli

import "strings"

// isGhostContainerError reports whether err is the "ghost container" failure:
// a container whose libpod DB entry survives (typically stuck Created) but whose
// c/storage layer is gone, a DB/storage desync an unclean Podman Machine
// shutdown can cause. `podman run --replace` then fails trying to swap the
// dead record, surfacing as `getting container from store "<id>": container not
// known`. Both substrings are required so an ordinary "container not known" (a
// genuinely absent container) does not trip the heal. Distinct from
// isOverlayStorageError, which matches corrupt overlay layers.
func isGhostContainerError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "from store") &&
		strings.Contains(msg, "container not known")
}
