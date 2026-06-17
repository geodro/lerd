package cli

import (
	"fmt"

	"github.com/geodro/lerd/internal/podman"
)

// buildWorkerGuard wraps runCmd in a shell snippet that prevents duplicate
// workers on macOS under the podman-exec runtime mode.
//
// Two failure modes are addressed:
//
//  1. Brief podman-machine SSH hiccup. The outer `podman exec` exits but
//     the inner artisan process inside the FPM container survives. The
//     pid-file mutex (step 1) catches the case where launchd respawns
//     before the outer process is gone.
//
//  2. Suspend/wake. The laptop sleeps; on wake the host-side `podman exec`
//     dies (its TCP/vsock link to the machine was torn down) but the inner
//     artisan process inside the container resumed normally. The pid-file
//     mutex doesn't help — its EXIT trap removed the file when the outer
//     process died — so step 2 reaches into the container, finds processes
//     matching the worker command WHOSE WORKING DIR EQUALS THIS SITE'S
//     PATH, and graceful-stops them. Then step 3 launches a fresh one.
//
// Cwd-scoping in step 2 is critical: every Laravel site shares the same
// FPM container and runs identical argv for `php artisan queue:work` /
// `schedule:work` / `horizon`. A naive argv-only pkill would nuke the
// same worker type running in *other* sites. Each site's `podman exec
// -w <sitePath>` sets a unique cwd, so /proc/<pid>/cwd is the disambig.
//
// On launch:
//
//  1. If the pid file exists AND its PID is alive, the previous outer
//     process is still driving the worker — exit 0.
//  2. Otherwise SIGTERM any in-container process matching workerCmd
//     whose cwd is sitePath, wait for it to exit (SIGKILL after a grace
//     period), then proceed. The wait frees a held listening socket
//     before the replacement binds it. Failures are swallowed.
//  3. Record our own PID, install an EXIT trap to clean up, and replace
//     ourselves with runCmd.
//
// Stale pid files (previous process crashed) resolve on their own: the
// kill -0 check in step 1 fails and the new instance takes over.
func buildWorkerGuard(pidFile, podmanBin, container, sitePath, workerCmd, runCmd string) string {
	// Inner sh script: enumerate pgrep matches, filter by cwd. Single
	// quotes around literal arg interpolations because ShellQuote already
	// produces single-quoted strings; they nest correctly when the whole
	// inner is itself shell-quoted as a sh -c argument.
	// m() lists in-container PIDs whose command matches workerCmd AND whose cwd
	// is this site. SIGTERM them, then wait (up to ~5s) for them to actually
	// exit before SIGKILLing any straggler. The wait is what lets a worker that
	// holds a listening socket — e.g. Reverb bound to a fixed port — release it
	// before the replacement starts; without it the new instance races the old
	// one and dies with EADDRINUSE.
	inner := fmt.Sprintf(
		`m() { for p in $(pgrep -f -- %[1]s 2>/dev/null); do `+
			`[ "$(readlink /proc/$p/cwd 2>/dev/null)" = %[2]s ] && echo "$p"; done; }; `+
			`for p in $(m); do kill -TERM "$p" 2>/dev/null; done; `+
			`i=0; while [ -n "$(m)" ] && [ "$i" -lt 50 ]; do i=$((i+1)); sleep 0.1; done; `+
			`for p in $(m); do kill -KILL "$p" 2>/dev/null; done`,
		podman.ShellQuote(workerCmd), podman.ShellQuote(sitePath))

	return fmt.Sprintf(`if [ -f %[1]s ] && kill -0 "$(cat %[1]s 2>/dev/null)" 2>/dev/null; then
  exit 0
fi
%[2]s exec %[3]s sh -c %[4]s >/dev/null 2>&1 || true
echo $$ > %[1]s
trap 'rm -f %[1]s' EXIT
exec %[5]s
`, pidFile, podmanBin, container, podman.ShellQuote(inner), runCmd)
}
