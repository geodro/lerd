package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// guardArgs is a fixture covering the new buildWorkerGuard signature.
// `true` is used as the podman binary so the orphan-cleanup step always
// succeeds quickly without contacting a real podman machine.
type guardArgs struct {
	pidFile, podmanBin, container, sitePath, workerCmd, runCmd string
}

func defaultGuardArgs(pidFile, runCmd string) guardArgs {
	return guardArgs{
		pidFile:   pidFile,
		podmanBin: "true",
		container: "lerd-php84-fpm",
		sitePath:  "/Users/test/site",
		workerCmd: "php artisan queue:work",
		runCmd:    runCmd,
	}
}

func (a guardArgs) build() string {
	return buildWorkerGuard(a.pidFile, a.podmanBin, a.container, a.sitePath, a.workerCmd, a.runCmd)
}

func TestBuildWorkerGuard_WrapsCommand(t *testing.T) {
	a := defaultGuardArgs("/tmp/lerd-queue-alpha.pid", "podman exec -w /site lerd-php84-fpm php artisan queue:work")
	got := a.build()

	for _, want := range []string{a.pidFile, a.runCmd, "kill -0", "exec ", "pgrep -f", "readlink /proc/$p/cwd", "'php artisan queue:work'", "'/Users/test/site'"} {
		if !strings.Contains(got, want) {
			t.Errorf("guard missing %q:\n%s", want, got)
		}
	}
}

func TestBuildWorkerGuard_ExitsZeroWhenPIDAlive(t *testing.T) {
	tmp := t.TempDir()
	pidFile := filepath.Join(tmp, "worker.pid")
	if err := os.WriteFile(pidFile, []byte(testPIDString()), 0644); err != nil {
		t.Fatal(err)
	}

	a := defaultGuardArgs(pidFile, "false")
	cmd := exec.Command("sh", "-c", a.build())
	if err := cmd.Run(); err != nil {
		t.Fatalf("guard should exit 0 when pid is alive, got: %v", err)
	}
}

func TestBuildWorkerGuard_ProceedsWhenPIDStale(t *testing.T) {
	tmp := t.TempDir()
	pidFile := filepath.Join(tmp, "worker.pid")
	if err := os.WriteFile(pidFile, []byte("2147483646\n"), 0644); err != nil {
		t.Fatal(err)
	}

	marker := filepath.Join(tmp, "ran")
	a := defaultGuardArgs(pidFile, "touch "+marker)
	cmd := exec.Command("sh", "-c", a.build())
	if err := cmd.Run(); err != nil {
		t.Fatalf("guard with stale pid should run wrapped command: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("marker not created; wrapped command didn't run")
	}
}

func TestBuildWorkerGuard_ProceedsWhenPIDFileMissing(t *testing.T) {
	tmp := t.TempDir()
	pidFile := filepath.Join(tmp, "worker.pid")
	marker := filepath.Join(tmp, "ran")
	a := defaultGuardArgs(pidFile, "touch "+marker)
	cmd := exec.Command("sh", "-c", a.build())
	if err := cmd.Run(); err != nil {
		t.Fatalf("guard without pid file should run wrapped command: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("marker not created")
	}
}

func TestBuildWorkerGuard_WritesPIDFileBeforeExec(t *testing.T) {
	tmp := t.TempDir()
	pidFile := filepath.Join(tmp, "worker.pid")
	a := defaultGuardArgs(pidFile, "test -s "+pidFile)
	cmd := exec.Command("sh", "-c", a.build())
	if err := cmd.Run(); err != nil {
		t.Fatalf("pid file should be written before wrapped command runs: %v", err)
	}
}

// TestBuildWorkerGuard_RunsOrphanCleanupBeforeExec verifies the orphan
// cleanup step (step 2) runs even when the pid file is missing — this is
// the suspend/wake codepath. We swap `podman` for a stub that records
// each argument so the test asserts the inner sh -c script contains
// both the worker command and the site path (the cwd-scoping that
// keeps cross-site sibling workers from being killed).
func TestBuildWorkerGuard_RunsOrphanCleanupBeforeExec(t *testing.T) {
	tmp := t.TempDir()
	pidFile := filepath.Join(tmp, "worker.pid")
	pkillMarker := filepath.Join(tmp, "pkill-ran")

	stub := filepath.Join(tmp, "podman-stub")
	stubScript := "#!/bin/sh\nfor a in \"$@\"; do printf '<%s>' \"$a\" >> " + pkillMarker + "; done\nexit 0\n"
	if err := os.WriteFile(stub, []byte(stubScript), 0755); err != nil {
		t.Fatal(err)
	}

	a := guardArgs{
		pidFile:   pidFile,
		podmanBin: stub,
		container: "lerd-php84-fpm",
		sitePath:  "/Users/u/parkapp",
		workerCmd: "php artisan queue:work --queue=default",
		runCmd:    "true",
	}
	cmd := exec.Command("sh", "-c", a.build())
	if err := cmd.Run(); err != nil {
		t.Fatalf("guard run failed: %v", err)
	}

	got, err := os.ReadFile(pkillMarker)
	if err != nil {
		t.Fatalf("orphan cleanup did not invoke podman: %v", err)
	}
	// The first three args are the literal `exec`, container name, and
	// `sh -c`. The fourth is the inner script (one shell arg) — must
	// contain both the worker command and the site path so the cwd
	// scoping is in effect.
	gotStr := string(got)
	for _, want := range []string{
		"<exec>",
		"<lerd-php84-fpm>",
		"<sh>",
		"<-c>",
		"php artisan queue:work --queue=default",
		"/Users/u/parkapp",
		"readlink /proc/$p/cwd",
	} {
		if !strings.Contains(gotStr, want) {
			t.Errorf("orphan cleanup missing %q in args:\n%s", want, gotStr)
		}
	}
}

// TestBuildWorkerGuard_SkipsOrphanCleanupWhenOuterAlive ensures the live
// host-side process short-circuit happens before any podman call —
// otherwise we'd wake the podman machine for every launchd respawn.
func TestBuildWorkerGuard_SkipsOrphanCleanupWhenOuterAlive(t *testing.T) {
	tmp := t.TempDir()
	pidFile := filepath.Join(tmp, "worker.pid")
	pkillMarker := filepath.Join(tmp, "pkill-ran")

	if err := os.WriteFile(pidFile, []byte(testPIDString()), 0644); err != nil {
		t.Fatal(err)
	}

	stub := filepath.Join(tmp, "podman-stub")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\ntouch "+pkillMarker+"\n"), 0755); err != nil {
		t.Fatal(err)
	}

	a := guardArgs{
		pidFile:   pidFile,
		podmanBin: stub,
		container: "lerd-php84-fpm",
		sitePath:  "/Users/u/parkapp",
		workerCmd: "php artisan queue:work",
		runCmd:    "false",
	}
	if err := exec.Command("sh", "-c", a.build()).Run(); err != nil {
		t.Fatalf("guard should exit 0: %v", err)
	}
	if _, err := os.Stat(pkillMarker); err == nil {
		t.Error("podman was invoked even though outer process is alive")
	}
}

func testPIDString() string {
	return itoa(os.Getpid())
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
