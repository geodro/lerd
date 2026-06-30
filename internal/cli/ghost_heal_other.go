//go:build !darwin

package cli

// healGhostContainersIfNeeded is a no-op on non-darwin platforms. The ghost is
// recoverable on the host there: native podman runs in the same scope as the
// containers, so a plain `podman rm -f` (already on the existing heal paths)
// reaches the dead record without going through a VM.
func healGhostContainersIfNeeded(_ error) bool { return false }
