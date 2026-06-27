package cleanup

import (
	"errors"
	"testing"
)

// withImages swaps the image-scan and layer-inspect seams for fixtures and
// restores them after. layers maps an image ID to its RootFS layers.
func withImages(t *testing.T, imgs []image, layers map[string][]string) {
	t.Helper()
	scanImages = func() ([]image, error) { return imgs, nil }
	imageLayers = func(ids []string) (map[string][]string, error) {
		m := map[string][]string{}
		for _, id := range ids {
			if l, ok := layers[id]; ok {
				m[id] = l
			}
		}
		return m, nil
	}
	t.Cleanup(func() {
		scanImages = podmanImages
		imageLayers = podmanImageLayers
	})
}

func TestInspect_ReclaimsOnlyOrphanedLerdImages(t *testing.T) {
	withImages(t, []image{
		// orphaned lerd FPM base (tag moved away on rebuild) → reclaim
		{ID: "sha256:aaa", Names: nil, Size: 100, Labels: map[string]string{"dev.lerd.fpm.containerfile-hash": "h1"}},
		// live lerd image (still tagged) → keep
		{ID: "sha256:bbb", Names: []string{"lerd-php84-fpm:local"}, Size: 200, Labels: map[string]string{"dev.lerd.fpm.containerfile-hash": "h2"}},
		// dangling but not lerd → keep (only touch lerd)
		{ID: "sha256:ccc", Names: nil, Size: 400, Labels: nil},
		// tagged non-lerd image → keep
		{ID: "sha256:ddd", Names: []string{"mysql:8.4"}, Size: 800, Labels: nil},
		// orphaned lerd FrankenPHP image; 600 of its 1600 bytes are shared layers,
		// so only the 1000 unique bytes are actually reclaimable → reclaim
		{ID: "sha256:eee", Names: []string{"<none>:<none>"}, Size: 1600, SharedSize: 600, Labels: map[string]string{"dev.lerd.frankenphp.containerfile-hash": "h3"}},
	}, nil)

	p, err := Inspect(false)
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]bool{}
	for _, tg := range p.Targets {
		got[tg.ID] = true
	}
	if len(got) != 2 || !got["aaa"] || !got["eee"] {
		t.Fatalf("want exactly orphaned lerd images {aaa,eee}, got %+v", p.Targets)
	}
	// aaa contributes its full 100 (no shared layers); eee contributes 1000
	// (1600 size minus 600 shared) — shared layers are not counted as reclaimed.
	if want := int64(1100); p.ReclaimBytes() != want {
		t.Errorf("ReclaimBytes = %d, want %d", p.ReclaimBytes(), want)
	}
}

// The "only touch lerd" contract: with nothing lerd-built present, the plan is
// empty even when the host is full of other reclaimable podman images.
func TestInspect_NeverTargetsNonLerd(t *testing.T) {
	withImages(t, []image{
		{ID: "sha256:111", Names: nil, Size: 999, Labels: nil},                                                        // dangling non-lerd
		{ID: "sha256:222", Names: []string{"redis:7"}, Size: 999, Labels: nil},                                        // tagged non-lerd
		{ID: "sha256:333", Names: []string{"<none>:<none>"}, Size: 999, Labels: map[string]string{"maintainer": "x"}}, // dangling, foreign label
	}, nil)

	p, err := Inspect(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Targets) != 0 {
		t.Fatalf("non-lerd images must never be targeted, got %+v", p.Targets)
	}
}

// A base image is reaped only when nothing live is built on it — covering both
// an old Containerfile hash for an installed version and a base for a PHP
// version no longer installed. The base whose layers the live image shares is
// kept (untagging it would force a needless re-pull and free nothing).
func TestInspect_ReclaimsOrphanBasesKeepsInUse(t *testing.T) {
	withImages(t,
		[]image{
			// live derived image, built on the current php84 base
			{ID: "local84", Names: []string{"localhost/lerd-php84-fpm:local"}, Size: 700, Labels: map[string]string{"dev.lerd.fpm.containerfile-hash": "h"}},
			// current php84 base: its top layer is in the live image → keep
			{ID: "baseCur", Names: []string{"ghcr.io/geodro/lerd-php84-fpm-base:cur"}, Size: 500},
			// old-hash php84 base: top layer used by nothing live → reclaim
			{ID: "baseOld", Names: []string{"ghcr.io/geodro/lerd-php84-fpm-base:old"}, Size: 500},
			// base for php82, a version no longer installed → reclaim
			{ID: "base82", Names: []string{"ghcr.io/geodro/lerd-php82-fpm-base:cur"}, Size: 500},
		},
		map[string][]string{
			"local84": {"L1", "L2", "L3", "Lcustom"}, // built on current base + custom layer
			"baseCur": {"L1", "L2", "L3"},            // top L3 ∈ live → in use
			"baseOld": {"L1", "L2", "Lold"},          // top Lold ∉ live → orphan
			"base82":  {"L1", "P82a", "P82b"},        // top P82b ∉ live → orphan
		},
	)

	p, err := Inspect(false)
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]bool{}
	for _, tg := range p.Targets {
		got[tg.ID] = true
	}
	want := map[string]bool{
		"ghcr.io/geodro/lerd-php84-fpm-base:old": true,
		"ghcr.io/geodro/lerd-php82-fpm-base:cur": true,
	}
	if len(got) != len(want) {
		t.Fatalf("want the two orphan bases reaped, got %+v", p.Targets)
	}
	for ref := range want {
		if !got[ref] {
			t.Errorf("expected %q reaped, missing", ref)
		}
	}
}

func TestApply_RemovesTargetsAndSumsReclaimed(t *testing.T) {
	var removed []string
	removeImage = func(id string) error { removed = append(removed, id); return nil }
	t.Cleanup(func() { removeImage = podmanRemoveImage })

	got := Apply(Plan{Targets: []Target{{ID: "aaa", Bytes: 100}, {ID: "bbb", Bytes: 250}}})

	if got != 350 {
		t.Errorf("reclaimed = %d, want 350", got)
	}
	if len(removed) != 2 || removed[0] != "aaa" || removed[1] != "bbb" {
		t.Errorf("removed = %v, want [aaa bbb]", removed)
	}
}

// A removal that fails (e.g. the image became referenced since Inspect) is
// skipped so one stuck image can't abort the sweep, and its bytes aren't counted.
func TestApply_SkipsFailedRemovalsButContinues(t *testing.T) {
	removeImage = func(id string) error {
		if id == "bad" {
			return errors.New("image is in use")
		}
		return nil
	}
	t.Cleanup(func() { removeImage = podmanRemoveImage })

	got := Apply(Plan{Targets: []Target{{ID: "bad", Bytes: 100}, {ID: "good", Bytes: 50}}})

	if got != 50 {
		t.Errorf("reclaimed = %d, want 50 (only the successful removal)", got)
	}
}
