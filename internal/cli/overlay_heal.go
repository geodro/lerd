package cli

import "strings"

// isOverlayStorageError reports whether err is a podman overlay-storage failure
// that surfaces after an ungraceful host shutdown leaves the Podman Machine's
// container storage corrupt. Every container start fails until the VM remounts
// its storage and the stale containers are rebuilt. Two variants are matched,
// both anchored on the overlay store path so unrelated failures (port
// conflicts, missing images, bad flags, or graph-driver/readlink errors that
// don't touch the overlay store) don't trip the destructive heal:
//
//  1. graph-driver info query: `getting graph driver info "<id>": readlink
//     /var/lib/containers/storage/overlay: invalid argument`
//  2. container mount: `mounting storage for container <id>: readlink
//     /var/lib/containers/storage/overlay/<layer>/diff: no such file or directory`
//
// Variant 2 was observed reproducing a broken container layer (image layers
// intact, so a fresh `podman run` still works); rebuilding the containers
// recovers it, which is exactly what the heal does.
func isOverlayStorageError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())

	if strings.Contains(msg, "graph driver") {
		return strings.Contains(msg, "/containers/storage/overlay")
	}

	if strings.Contains(msg, "mounting storage for container") {
		return strings.Contains(msg, "/containers/storage/overlay") &&
			strings.Contains(msg, "readlink")
	}

	return false
}
