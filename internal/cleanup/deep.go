package cleanup

import (
	"os"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/registry"
)

// serviceRepos and protectedImages are the seams tests override. serviceRepos
// is the set of normalized image repos lerd's preset catalog manages (mysql,
// redis, ...); protectedImages is every image a service currently uses or holds
// as a rollback target, which --deep must never remove.
var (
	serviceRepos    = realServiceRepos
	protectedImages = realProtectedImages
)

// deepTargets reclaims service images lerd pulled but nothing references any
// more: an image whose every tag is an unprotected catalog ref. Treating lerd's
// managed service images as lerd's own, this catches an old mysql:5.7 left
// behind after upgrading, while the protected set keeps the live image and the
// one-back rollback target, and an image carrying any non-catalog tag (one the
// user added themselves) is left entirely alone.
func deepTargets(imgs []image, repos, protected map[string]bool) []Target {
	var out []Target
	for _, img := range imgs {
		refs := removableServiceRefs(img, repos, protected)
		// Remove every owned tag so the image actually frees on the last one;
		// credit the reclaimable bytes once (on that last removal), since
		// untagging the earlier aliases frees nothing on its own.
		for i, ref := range refs {
			bytes := int64(0)
			if i == len(refs)-1 {
				bytes = reclaimable(img)
			}
			out = append(out, Target{Kind: "image", ID: ref, Desc: "unused service image", Bytes: bytes})
		}
	}
	return out
}

// removableServiceRefs returns the tags to remove for an unused service image,
// or nil to keep it. An image is removable only when EVERY one of its tags is an
// unprotected catalog ref: a protected tag (current image or rollback target) or
// a non-catalog tag the user added both mean "leave this whole image alone", so
// cleanup never untags an image something else still relies on.
func removableServiceRefs(img image, repos, protected map[string]bool) []string {
	if len(img.Names) == 0 {
		return nil
	}
	refs := make([]string, 0, len(img.Names))
	for _, n := range img.Names {
		if protected[canonRef(n)] || !repos[canonRepo(n)] {
			return nil
		}
		refs = append(refs, n)
	}
	return refs
}

// canonRef / canonRepo canonicalise an image reference through the shared
// registry parser, so a fully-qualified ref and its short form (mysql:8.4 ==
// docker.io/library/mysql:8.4) compare equal on both sides of every lookup.
func canonRef(ref string) string {
	r, err := registry.ParseImage(ref)
	if err != nil {
		return ref
	}
	return r.Registry + "/" + r.Repo + ":" + r.Tag
}

func canonRepo(ref string) string {
	r, err := registry.ParseImage(ref)
	if err != nil {
		return ref
	}
	return r.Registry + "/" + r.Repo
}

func realServiceRepos() (map[string]bool, error) {
	presets, err := config.ListPresets()
	if err != nil {
		return nil, err
	}
	repos := map[string]bool{}
	add := func(image string) {
		if image != "" {
			repos[canonRepo(image)] = true
		}
	}
	for _, p := range presets {
		add(p.Image)
		for _, v := range p.Versions {
			add(v.Image)
		}
	}
	return repos, nil
}

func realProtectedImages() (map[string]bool, error) {
	prot := map[string]bool{}
	add := func(ref string) {
		if ref != "" {
			prot[canonRef(ref)] = true
		}
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		return nil, err
	}
	for _, s := range cfg.Services {
		add(s.Image)
		add(s.PreviousImage)
	}
	customs, err := config.ListCustomServices()
	if err != nil {
		return nil, err
	}
	for _, s := range customs {
		add(s.Image)
		add(s.PreviousImage)
	}
	for _, ref := range installedServiceImages() {
		add(ref)
	}
	return prot, nil
}

// installedServiceImages returns the current Image= of every installed service
// quadlet, so a service that is installed but not running is still protected.
// PHP-FPM units are skipped: their images are handled by the safe tier.
func installedServiceImages() []string {
	entries, err := os.ReadDir(config.QuadletDir())
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "lerd-") || !strings.HasSuffix(name, ".container") {
			continue
		}
		unit := strings.TrimSuffix(name, ".container")
		if strings.HasPrefix(unit, "lerd-php") && strings.HasSuffix(unit, "-fpm") {
			continue
		}
		if img := podman.InstalledImage(unit); img != "" {
			out = append(out, img)
		}
	}
	return out
}
