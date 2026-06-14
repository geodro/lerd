package podman

import (
	"strings"
	"testing"
)

// TestFrankenPHPRuntimeExtensionParity guards against the FPM image gaining a
// runtime extension that the FrankenPHP image silently misses. It parses the
// docker-php-ext-install list from the FPM Containerfile and asserts each entry
// is either baked into the FrankenPHP image (frankenPHPRuntimeExtensions) or is
// a base-image builtin the dunglas image already provides. dev-only tooling
// (xdebug/pcov/spx) is handled separately and excluded.
func TestFrankenPHPRuntimeExtensionParity(t *testing.T) {
	cf, err := GetQuadletTemplate("lerd-php-fpm.Containerfile")
	if err != nil {
		t.Fatalf("reading FPM Containerfile: %v", err)
	}
	fpmExts := fpmDockerExtInstallList(cf)
	if len(fpmExts) < 10 {
		t.Fatalf("parsed only %d FPM extensions, parser likely broke: %v", len(fpmExts), fpmExts)
	}

	franken := map[string]bool{}
	for _, e := range frankenPHPRuntimeExtensions {
		franken[e] = true
	}
	// Builtins the dunglas/frankenphp base already ships, verified present in the
	// built image, so they need no explicit entry in the FrankenPHP list.
	builtin := map[string]bool{"curl": true, "mbstring": true, "xml": true}

	for _, ext := range fpmExts {
		if !franken[ext] && !builtin[ext] {
			t.Errorf("FPM installs %q but the FrankenPHP image neither bakes it nor gets it from the base — add it to frankenPHPRuntimeExtensions (or the builtin set)", ext)
		}
	}
	// The pecl-installed runtime extensions must also be baked into FrankenPHP.
	for _, ext := range []string{"redis", "imagick", "igbinary", "mongodb"} {
		if !franken[ext] {
			t.Errorf("FrankenPHP image missing pecl runtime extension %q present in FPM", ext)
		}
	}
}

// fpmDockerExtInstallList extracts the extension tokens from the FPM
// Containerfile's `docker-php-ext-install -j$(nproc) \ ... ` block.
func fpmDockerExtInstallList(containerfile string) []string {
	var out []string
	collecting := false
	for _, ln := range strings.Split(containerfile, "\n") {
		s := strings.TrimSpace(ln)
		if strings.Contains(s, "docker-php-ext-install") {
			collecting = true
			continue
		}
		if !collecting {
			continue
		}
		if strings.HasPrefix(s, "&") { // end of the continued install list
			break
		}
		tok := strings.TrimSpace(strings.TrimSuffix(s, "\\"))
		if tok != "" {
			out = append(out, tok)
		}
	}
	return out
}

func TestFrankenPHPImageName(t *testing.T) {
	if got := FrankenPHPImageName("8.4"); got != "localhost/lerd-frankenphp84:local" {
		t.Errorf("FrankenPHPImageName(8.4) = %q", got)
	}
	if got := FrankenPHPImage("8.4"); got != "localhost/lerd-frankenphp84:local" {
		t.Errorf("FrankenPHPImage(8.4) = %q, want the derived image", got)
	}
	if got := FrankenPHPBaseImage("8.4"); got != "docker.io/dunglas/frankenphp:php8.4-alpine" {
		t.Errorf("FrankenPHPBaseImage(8.4) = %q, want the upstream base", got)
	}
}

// TestRenderFrankenPHPContainerfile checks the derived image is built FROM the
// dunglas base and bakes the standard extension set with no leftover template
// placeholders.
func TestRenderFrankenPHPContainerfile(t *testing.T) {
	exts := append(append([]string{}, frankenPHPRuntimeExtensions...), "myext")
	cf, err := renderFrankenPHPContainerfile("8.4", exts, []string{"jq"}, "# mkcert\n")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(cf, "{{") {
		t.Errorf("unsubstituted placeholder remains:\n%s", cf)
	}
	if !strings.Contains(cf, "FROM docker.io/dunglas/frankenphp:php8.4-alpine") {
		t.Errorf("missing FROM base:\n%s", cf)
	}
	for _, want := range []string{
		"install-php-extensions", "redis", "gd", "pdo_mysql", "intl", "myext",
		// dev-tooling baked into the image (xdebug + the compiled lerd_devtools)
		"xdebug", "lerd_devtools",
	} {
		if !strings.Contains(cf, want) {
			t.Errorf("rendered Containerfile missing %q", want)
		}
	}
	// SPX is intentionally not baked: it can't profile Octane's resident workers.
	if strings.Contains(cf, " spx ") || strings.Contains(cf, "spx \\") {
		t.Errorf("spx should not be baked into the FrankenPHP image:\n%s", cf)
	}
	if !strings.Contains(cf, "jq") {
		t.Errorf("custom package not rendered:\n%s", cf)
	}
}

// TestNeedsFrankenPHPRebuild exercises the label-vs-hash comparison through the
// imageLabelFn seam so a stale or missing image triggers a rebuild and a current
// one does not.
func TestNeedsFrankenPHPRebuild(t *testing.T) {
	current, err := frankenPHPContainerfileHash()
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	prev := imageLabelFn
	t.Cleanup(func() { imageLabelFn = prev })

	imageLabelFn = func(string, string) string { return current }
	if NeedsFrankenPHPRebuild([]string{"8.4"}) {
		t.Error("matching label should not need a rebuild")
	}

	imageLabelFn = func(string, string) string { return "stale" }
	if !NeedsFrankenPHPRebuild([]string{"8.4"}) {
		t.Error("mismatched label should need a rebuild")
	}

	imageLabelFn = func(string, string) string { return "" } // image missing
	if !NeedsFrankenPHPRebuild([]string{"8.4"}) {
		t.Error("missing image should need a rebuild")
	}

	if NeedsFrankenPHPRebuild(nil) {
		t.Error("no active versions should never need a rebuild")
	}
}

func TestSanitizeExtNames(t *testing.T) {
	got := sanitizeExtNames([]string{"redis", "  gd ", "", "bad name", "a;b", "pdo_mysql"})
	want := []string{"redis", "gd", "pdo_mysql"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("sanitizeExtNames = %v, want %v", got, want)
	}
}
