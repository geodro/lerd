package cli

import "fmt"

// buildWorkerGuard wraps cmd in a shell snippet that prevents duplicate
// workers on macOS under the podman-exec runtime mode.
//
// The scenario it addresses: launchd supervises a `podman exec` process
// that in turn holds a worker alive inside the shared FPM container.
// Brief hiccups on the podman-machine SSH bridge can cause the outer
// `podman exec` to exit while the artisan process inside the container
// keeps running. launchd sees "job ended" and relaunches, producing a
// duplicate worker.
//
// The guard uses a PID file as a mutex. On launch:
//
//  1. If the file exists AND its PID is alive, another instance is
//     already driving the worker — exit 0 so launchd treats this
//     invocation as a normal successful run and waits for the real
//     process to exit before relaunching.
//  2. Otherwise record our own PID in the file, install a trap to
//     remove the file on exit, and replace ourselves with the wrapped
//     command so signals (TERM from launchd on shutdown) reach it
//     directly.
//
// Stale PID files (previous process crashed) resolve on their own: the
// kill -0 check fails and the new instance takes over.
func buildWorkerGuard(pidFile, cmd string) string {
	return fmt.Sprintf(`if [ -f %[1]s ] && kill -0 "$(cat %[1]s 2>/dev/null)" 2>/dev/null; then
  exit 0
fi
echo $$ > %[1]s
trap 'rm -f %[1]s' EXIT
exec %[2]s
`, pidFile, cmd)
}
