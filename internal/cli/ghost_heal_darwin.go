//go:build darwin

package cli

import (
	"os"

	"github.com/geodro/lerd/internal/feedback"
	"github.com/geodro/lerd/internal/podman"
)

// healGhostContainersIfNeeded recovers from a ghost container (see
// isGhostContainerError) on the service start pass, then asks the caller to
// retry once. The host podman client cannot purge the ghost: lerd's containers
// live in the VM's rootful storage, and `podman rm -f` from the host returns
// "container not known" against the missing storage layer. Removing it inside
// the VM, where the rootful podman tolerates the dead record, clears it so the
// retry's `podman run --replace` recreates the container on fresh storage.
// Site data is host bind-mounted, so the purge loses nothing. Returns true when
// recovery ran and the caller should retry the start pass.
func healGhostContainersIfNeeded(err error) bool {
	if !isGhostContainerError(err) {
		return false
	}
	purgeGhostLerdContainers()
	return true
}

// purgeGhostLerdContainers force-removes the stuck-Created lerd-* containers
// inside the Podman Machine's rootful scope, the only place the removal
// succeeds. It targets `status=created` so healthy running services are left
// alone; `xargs -r` makes the remove a no-op when nothing matches.
func purgeGhostLerdContainers() {
	name := selectedMachineName()
	if name == "" {
		return
	}
	feedback.Line("A container's storage was orphaned (likely an unclean Podman Machine shutdown); purging the ghost inside the VM so it can be recreated…")
	const script = `sudo podman ps -a --filter name=^lerd- --filter status=created -q | xargs -r sudo podman rm -f`
	cmd := podman.Cmd("machine", "ssh", name, script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		feedback.Warn("purge ghost containers: %v", err)
	}
}
