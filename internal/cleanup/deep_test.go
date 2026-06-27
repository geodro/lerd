package cleanup

import "testing"

// Catalog repos and protected refs are keyed by canonical form (the same
// registry.ParseImage canonicalisation deepTargets applies to image names).
func TestDeepTargets_ReapsUnusedServiceImagesKeepsProtected(t *testing.T) {
	imgs := []image{
		{Names: []string{"docker.io/library/mysql:8.4"}, Size: 500},   // current → protected
		{Names: []string{"docker.io/library/mysql:5.7"}, Size: 400},   // unused old → reap
		{Names: []string{"docker.io/library/redis:7"}, Size: 100},     // one-back rollback → protected
		{Names: []string{"docker.io/library/postgres:16"}, Size: 900}, // repo not in catalog → keep
		{Names: []string{"ubuntu:22.04"}, Size: 700},                  // user's own image → keep
		// multi-tag image: the rolling alias is unprotected but the pinned tag is
		// in use, so the whole image must be kept (per-image protection)
		{Names: []string{"docker.io/library/redis:7-alpine", "docker.io/library/redis:7.4.9-alpine"}, Size: 40},
	}
	repos := map[string]bool{"docker.io/library/mysql": true, "docker.io/library/redis": true}
	protected := map[string]bool{
		"docker.io/library/mysql:8.4":          true,
		"docker.io/library/redis:7":            true,
		"docker.io/library/redis:7.4.9-alpine": true,
	}

	got := deepTargets(imgs, repos, protected)

	if len(got) != 1 || got[0].ID != "docker.io/library/mysql:5.7" {
		t.Fatalf("want only mysql:5.7 reaped, got %+v", got)
	}
	if got[0].Bytes != 400 {
		t.Errorf("bytes = %d, want 400", got[0].Bytes)
	}
}

// A multi-tag image whose tags are ALL unprotected catalog refs must have every
// tag removed (so the image actually frees), with the reclaimable bytes credited
// exactly once. An image carrying a non-catalog tag is left entirely alone.
func TestDeepTargets_MultiTagRemovesAllTagsCreditsOnce(t *testing.T) {
	imgs := []image{
		// both tags catalog + unprotected → remove both, count bytes once
		{Names: []string{"docker.io/library/mysql:5.7", "docker.io/library/mysql:5.7.40"}, Size: 400},
		// catalog tag + a user's own tag → keep the whole image
		{Names: []string{"docker.io/library/mysql:8.0", "myown:tag"}, Size: 999},
	}
	repos := map[string]bool{"docker.io/library/mysql": true}
	protected := map[string]bool{}

	got := deepTargets(imgs, repos, protected)

	removed := map[string]int64{}
	for _, tg := range got {
		removed[tg.ID] = tg.Bytes
	}
	if len(removed) != 2 ||
		!hasKey(removed, "docker.io/library/mysql:5.7") ||
		!hasKey(removed, "docker.io/library/mysql:5.7.40") {
		t.Fatalf("want both mysql:5.7 tags removed, none from the user-tagged image, got %+v", got)
	}
	var total int64
	for _, b := range removed {
		total += b
	}
	if total != 400 {
		t.Errorf("reclaimable credited = %d, want 400 (once for the image)", total)
	}
}

func hasKey(m map[string]int64, k string) bool { _, ok := m[k]; return ok }

// The deep tier is additive: the safe tier never touches service images, and
// only --deep reaps the unused one, leaving the current image alone.
func TestInspect_DeepAppendsUnusedServiceImages(t *testing.T) {
	withImages(t, []image{
		{ID: "m57", Names: []string{"docker.io/library/mysql:5.7"}, Size: 400}, // unused → deep reap
		{ID: "m84", Names: []string{"docker.io/library/mysql:8.4"}, Size: 500}, // current → keep
	}, nil)
	serviceRepos = func() (map[string]bool, error) {
		return map[string]bool{"docker.io/library/mysql": true}, nil
	}
	protectedImages = func() (map[string]bool, error) {
		return map[string]bool{"docker.io/library/mysql:8.4": true}, nil
	}
	t.Cleanup(func() {
		serviceRepos = realServiceRepos
		protectedImages = realProtectedImages
	})

	safe, err := Inspect(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(safe.Targets) != 0 {
		t.Fatalf("safe tier must not touch service images, got %+v", safe.Targets)
	}

	deep, err := Inspect(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(deep.Targets) != 1 || deep.Targets[0].ID != "docker.io/library/mysql:5.7" {
		t.Fatalf("deep tier should reap only mysql:5.7, got %+v", deep.Targets)
	}
}

func TestCanonRefAndRepo(t *testing.T) {
	refCases := map[string]string{
		"mysql:8.4":                   "docker.io/library/mysql:8.4",
		"docker.io/library/mysql:8.4": "docker.io/library/mysql:8.4",
		"getmeili/meilisearch:v1.7":   "docker.io/getmeili/meilisearch:v1.7",
		"ghcr.io/x/y:tag":             "ghcr.io/x/y:tag",
	}
	for in, want := range refCases {
		if got := canonRef(in); got != want {
			t.Errorf("canonRef(%q) = %q, want %q", in, got, want)
		}
	}
	if got := canonRepo("docker.io/library/mysql:8.4"); got != "docker.io/library/mysql" {
		t.Errorf("canonRepo = %q, want docker.io/library/mysql", got)
	}
}
