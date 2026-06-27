package cleanup

import "testing"

// SweepRefs reaps exactly the refs lerd hands it, skipping any the protected set
// still holds, and never scans the whole repo (so a user's same-repo image that
// wasn't passed is untouched).
func TestSweepRefs_ReapsGivenRefsSkipsProtected(t *testing.T) {
	autoEnabled = func() bool { return true }
	protectedImages = func() (map[string]bool, error) {
		return map[string]bool{"docker.io/library/mysql:8.4": true}, nil
	}
	var removed []string
	removeImage = func(id string) error { removed = append(removed, id); return nil }
	t.Cleanup(func() {
		autoEnabled = defaultAutoEnabled
		protectedImages = realProtectedImages
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
