package serviceops

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

func TestEnsureCustomServiceQuadlet_reloadsOnlyWhenContentChanges(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))

	count := 0
	orig := podman.DaemonReloadFn
	t.Cleanup(func() { podman.DaemonReloadFn = orig })
	podman.DaemonReloadFn = func() error {
		count++
		return nil
	}

	svc := &config.CustomService{
		Name:  "mongo-express",
		Image: "docker.io/library/mongo-express:latest",
		Ports: []string{"127.0.0.1:8082:8081"},
	}

	if err := EnsureCustomServiceQuadlet(svc); err != nil {
		t.Fatalf("first EnsureCustomServiceQuadlet: %v", err)
	}
	if count != 1 {
		t.Errorf("first call should reload once, got %d", count)
	}

	if err := EnsureCustomServiceQuadlet(svc); err != nil {
		t.Fatalf("second EnsureCustomServiceQuadlet: %v", err)
	}
	if count != 1 {
		t.Errorf("second call with unchanged content must not reload, got %d total", count)
	}

	svc.Image = "docker.io/library/mongo-express:1.0.2"
	if err := EnsureCustomServiceQuadlet(svc); err != nil {
		t.Fatalf("third EnsureCustomServiceQuadlet: %v", err)
	}
	if count != 2 {
		t.Errorf("changed image should reload again, got %d total", count)
	}
}

// TestEnsureCustomServiceQuadlet_portShiftNoticeAvoidsStdout: when the port guard
// shifts a service off a busy port it must not write its notice to os.Stdout.
// EnsureCustomServiceQuadlet is called in-process by the MCP stdio server, which
// reserves os.Stdout for the JSON-RPC stream — any stray write there corrupts the
// protocol frame and breaks the client session.
func TestEnsureCustomServiceQuadlet_portShiftNoticeAvoidsStdout(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))

	orig := podman.DaemonReloadFn
	t.Cleanup(func() { podman.DaemonReloadFn = orig })
	podman.DaemonReloadFn = func() error { return nil }

	// Occupy the service's primary host port so the guard is forced to shift it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot bind a loopback port: %v", err)
	}
	defer ln.Close()
	busy := ln.Addr().(*net.TCPAddr).Port

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w

	svc := &config.CustomService{
		Name:  "mongo-express",
		Image: "docker.io/library/mongo-express:latest",
		Ports: []string{fmt.Sprintf("127.0.0.1:%d:8081", busy)},
	}
	ensErr := EnsureCustomServiceQuadlet(svc)
	_ = w.Close()
	os.Stdout = saved

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if ensErr != nil {
		t.Fatalf("EnsureCustomServiceQuadlet: %v", ensErr)
	}
	if config.ServicePublishedPort("mongo-express") == 0 {
		t.Fatal("expected the guard to shift the busy port and persist a published port")
	}
	if buf.Len() != 0 {
		t.Errorf("port-shift notice leaked to os.Stdout (would corrupt MCP JSON-RPC): %q", buf.String())
	}
}
