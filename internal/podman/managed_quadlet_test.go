package podman

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func TestGenerateCustomQuadlet_StampsManagedMarker(t *testing.T) {
	out := GenerateCustomQuadlet(&config.CustomService{Name: "gotenberg", Image: "docker.io/gotenberg/gotenberg:8"})
	if !strings.Contains(out, CustomServiceQuadletMarker) {
		t.Fatalf("expected managed-service marker in quadlet, got:\n%s", out)
	}
}

func TestListManagedServiceNames(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := config.QuadletDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	write := func(unit, body string) {
		if err := os.WriteFile(filepath.Join(dir, unit+".container"), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", unit, err)
		}
	}
	// Two marked service quadlets, plus an unmarked site container and a
	// non-.container file that must be ignored.
	write("lerd-gotenberg", CustomServiceQuadletMarker+"\n[Container]\nImage=x\n")
	write("lerd-acme-widget", CustomServiceQuadletMarker+"\n[Container]\nImage=y\n")
	write("lerd-custom-gonitro", "[Container]\nImage=z\n") // site container, no marker
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte(CustomServiceQuadletMarker), 0o644); err != nil {
		t.Fatalf("write notes: %v", err)
	}

	got := ListManagedServiceNames()
	want := map[string]bool{"gotenberg": true, "acme-widget": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want keys %v", got, want)
	}
	for _, n := range got {
		if !want[n] {
			t.Fatalf("unexpected managed name %q in %v", n, got)
		}
	}
}
