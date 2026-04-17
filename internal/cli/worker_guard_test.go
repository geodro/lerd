package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildWorkerGuard_WrapsCommand(t *testing.T) {
	pidFile := "/tmp/lerd-queue-alpha.pid"
	cmd := "podman exec -w /site lerd-php84-fpm php artisan queue:work"

	got := buildWorkerGuard(pidFile, cmd)

	for _, want := range []string{pidFile, cmd, "kill -0", "exec "} {
		if !strings.Contains(got, want) {
			t.Errorf("guard missing %q:\n%s", want, got)
		}
	}
}

func TestBuildWorkerGuard_ExitsZeroWhenPIDAlive(t *testing.T) {
	// End-to-end: run the guard script with a PID file pointing at this test
	// process (guaranteed alive) and verify the script exits 0 without
	// invoking the wrapped command. We use `false` as the wrapped command so
	// that if the guard fails, the outer script exits non-zero.
	tmp := t.TempDir()
	pidFile := filepath.Join(tmp, "worker.pid")
	// Write our own PID into the file — guaranteed alive for the duration
	// of the test.
	if err := os.WriteFile(pidFile, []byte(testPIDString()), 0644); err != nil {
		t.Fatal(err)
	}

	script := buildWorkerGuard(pidFile, "false")
	cmd := exec.Command("sh", "-c", script)
	if err := cmd.Run(); err != nil {
		t.Fatalf("guard should exit 0 when pid is alive, got: %v", err)
	}
}

func TestBuildWorkerGuard_ProceedsWhenPIDStale(t *testing.T) {
	tmp := t.TempDir()
	pidFile := filepath.Join(tmp, "worker.pid")
	// PID 1 is init; we'd never race with it, but the standard stale-PID
	// test is to use an unlikely-to-exist high PID.
	if err := os.WriteFile(pidFile, []byte("2147483646\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Wrapped command that touches a marker file so we can verify the
	// guard actually invoked it.
	marker := filepath.Join(tmp, "ran")
	script := buildWorkerGuard(pidFile, "touch "+marker)
	cmd := exec.Command("sh", "-c", script)
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
	script := buildWorkerGuard(pidFile, "touch "+marker)
	cmd := exec.Command("sh", "-c", script)
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
	// Wrap with a command that reads the pid file and confirms it has content.
	script := buildWorkerGuard(pidFile, "test -s "+pidFile)
	cmd := exec.Command("sh", "-c", script)
	if err := cmd.Run(); err != nil {
		t.Fatalf("pid file should be written before wrapped command runs: %v", err)
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
