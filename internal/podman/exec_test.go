package podman

import (
	"context"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestShellQuote(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"php artisan queue:work", "'php artisan queue:work'"},
		{"a b", "'a b'"},
		{"it's", `'it'\''s'`},
		{"", "''"},
	}
	for _, c := range cases {
		if got := ShellQuote(c.in); got != c.want {
			t.Errorf("ShellQuote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestServiceVersionLabel(t *testing.T) {
	tests := []struct {
		image string
		want  string
	}{
		{"docker.io/library/mysql:8.0", "v8.0"},
		{"docker.io/library/redis:7-alpine", "v7"},
		{"docker.io/postgis/postgis:16-3.5", "v16"},
		{"docker.io/postgis/postgis:16-3.5-alpine", "v16"},
		{"docker.io/getmeili/meilisearch:v1.7", "v1.7"},
		{"docker.io/axllent/mailpit:latest", "latest"},
		{"docker.io/rustfs/rustfs:latest", "latest"},
		{"docker.io/library/redis:main", "main"},
		{"nginx:alpine", "alpine"},
		{"nginx", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			if got := ServiceVersionLabel(tt.image); got != tt.want {
				t.Errorf("ServiceVersionLabel(%q) = %q, want %q", tt.image, got, tt.want)
			}
		})
	}
}

// fakeExec returns a drop-in for execCommand that re-executes the test binary
// as a helper process emitting the given stdout/stderr and exit code, so the
// exec helpers can be exercised without a real podman on the host.
func fakeExec(stdout, stderr string, exit int) func(string, ...string) *exec.Cmd {
	return func(name string, args ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GO_WANT_HELPER_PROCESS=1",
			"HELPER_STDOUT="+stdout,
			"HELPER_STDERR="+stderr,
			"HELPER_EXIT="+strconv.Itoa(exit),
		)
		return cmd
	}
}

// TestHelperProcess is not a real test; it's the child process spawned by
// fakeExec. It echoes the configured streams and exits with the wanted code.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	if s := os.Getenv("HELPER_STDOUT"); s != "" {
		os.Stdout.WriteString(s)
	}
	if s := os.Getenv("HELPER_STDERR"); s != "" {
		os.Stderr.WriteString(s)
	}
	code, _ := strconv.Atoi(os.Getenv("HELPER_EXIT"))
	os.Exit(code)
}

func TestCmdUsesPodmanBinAndArgs(t *testing.T) {
	cmd := Cmd("machine", "stop", "lerd")
	want := []string{PodmanBin(), "machine", "stop", "lerd"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Errorf("Cmd args = %v, want %v", cmd.Args, want)
	}
}

func TestCmdContextPassesContextAndArgs(t *testing.T) {
	type ctxKey struct{}
	var gotCtx context.Context
	var gotName string
	var gotArgs []string

	prev := execCommandContext
	t.Cleanup(func() { execCommandContext = prev })
	execCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		gotCtx, gotName, gotArgs = ctx, name, args
		return prev(ctx, name, args...)
	}

	ctx := context.WithValue(context.Background(), ctxKey{}, "v")
	_ = CmdContext(ctx, "logs", "-f", "lerd-nginx")

	if gotCtx == nil || gotCtx.Value(ctxKey{}) != "v" {
		t.Error("CmdContext did not pass the context through")
	}
	if gotName != PodmanBin() {
		t.Errorf("CmdContext binary = %q, want %q", gotName, PodmanBin())
	}
	if want := []string{"logs", "-f", "lerd-nginx"}; !reflect.DeepEqual(gotArgs, want) {
		t.Errorf("CmdContext args = %v, want %v", gotArgs, want)
	}
}

func TestRunTrimsStdout(t *testing.T) {
	prev := execCommand
	t.Cleanup(func() { execCommand = prev })
	execCommand = fakeExec("  hello world \n", "", 0)

	got, err := Run("ps")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got != "hello world" {
		t.Errorf("Run = %q, want %q", got, "hello world")
	}
}

func TestRunWrapsErrorWithStderr(t *testing.T) {
	prev := execCommand
	t.Cleanup(func() { execCommand = prev })
	execCommand = fakeExec("", "boom: no such container", 1)

	_, err := Run("inspect", "nope")
	if err == nil {
		t.Fatal("Run expected an error, got nil")
	}
	if msg := err.Error(); !strings.Contains(msg, "boom: no such container") || !strings.Contains(msg, "inspect nope") {
		t.Errorf("Run error = %q, want it to mention args and stderr", msg)
	}
}

func TestRunSilentReturnsExecErrors(t *testing.T) {
	prev := execCommand
	t.Cleanup(func() { execCommand = prev })

	execCommand = fakeExec("", "", 0)
	if err := RunSilent("ok"); err != nil {
		t.Errorf("RunSilent on exit 0 = %v, want nil", err)
	}

	execCommand = fakeExec("", "fail", 2)
	if err := RunSilent("bad"); err == nil {
		t.Error("RunSilent on exit 2 = nil, want error")
	}
}
