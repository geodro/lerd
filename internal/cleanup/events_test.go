package cleanup

import (
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// SweepRefs reaps exactly the refs lerd hands it, skipping any the protected set
// still holds, and never scans the whole repo (so a user's same-repo image that
// wasn't passed is untouched).
func TestSweepRefs_ReapsGivenRefsSkipsProtected(t *testing.T) {
	autoEnabled = func() bool { return true }
	referencedImages = func(candidates map[string]bool) map[string]bool {
		out := map[string]bool{}
		if candidates[canonRef("docker.io/library/mysql:8.4")] {
			out[canonRef("docker.io/library/mysql:8.4")] = true
		}
		return out
	}
	var removed []string
	removeImage = func(id string) error { removed = append(removed, id); return nil }
	t.Cleanup(func() {
		autoEnabled = defaultAutoEnabled
		referencedImages = realReferencedImages
		removeImage = podmanRemoveImage
	})

	// 5.7 is the superseded ref → removed; 8.4 is still current → protected, skipped;
	// "" is ignored; the duplicate is collapsed.
	SweepRefs("docker.io/library/mysql:5.7", "docker.io/library/mysql:8.4", "", "docker.io/library/mysql:5.7")

	if len(removed) != 1 || removed[0] != "docker.io/library/mysql:5.7" {
		t.Fatalf("want only the superseded mysql:5.7 removed, got %v", removed)
	}
}

func TestSweepRefs_GatedOffNoOp(t *testing.T) {
	autoEnabled = func() bool { return false }
	called := false
	removeImage = func(string) error { called = true; return nil }
	t.Cleanup(func() {
		autoEnabled = defaultAutoEnabled
		removeImage = podmanRemoveImage
	})

	SweepRefs("docker.io/library/mysql:5.7")
	if called {
		t.Error("gated off must not remove anything")
	}
}

func TestSweepSafe_GatedOffNoOp(t *testing.T) {
	autoEnabled = func() bool { return false }
	scanned := false
	scanImages = func() ([]image, error) { scanned = true; return nil, nil }
	t.Cleanup(func() {
		autoEnabled = defaultAutoEnabled
		scanImages = podmanImages
	})

	images, bytes, err := SweepSafe()
	if err != nil || images != 0 || bytes != 0 || scanned {
		t.Errorf("gated off must no-op without scanning: images=%d bytes=%d err=%v scanned=%v", images, bytes, err, scanned)
	}
}

// SweepRefs must skip the per-quadlet image reads when the cheap config sources
// already account for every candidate ref — the efficiency the short-circuit
// buys over building the full protected set on each update/remove.
func TestSweepRefs_shortCircuitsQuadletScanWhenConfigCovers(t *testing.T) {
	autoEnabled = func() bool { return true }
	referencedImages = realReferencedImages
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Services["mysql-8.4"] = config.ServiceConfig{Enabled: true, Image: "docker.io/library/mysql:8.4"}
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatal(err)
	}

	scanned := false
	installedServiceImages = func() []string { scanned = true; return nil }
	var removed []string
	removeImage = func(id string) error { removed = append(removed, id); return nil }
	t.Cleanup(func() {
		autoEnabled = defaultAutoEnabled
		installedServiceImages = realInstalledServiceImages
		removeImage = podmanRemoveImage
	})

	// The only candidate is still in config, so it is protected and the quadlet
	// scan must never run.
	SweepRefs("docker.io/library/mysql:8.4")

	if scanned {
		t.Error("quadlet image reads must be skipped when config already covers every candidate")
	}
	if len(removed) != 0 {
		t.Errorf("a still-referenced ref must not be reaped, got %v", removed)
	}
}
