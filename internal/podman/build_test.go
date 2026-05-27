package podman

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func TestBuildCustomExtBlock_Empty(t *testing.T) {
	if got := buildCustomExtBlock(nil, nil); got != "" {
		t.Errorf("expected empty block for no extensions, got:\n%s", got)
	}
}

func TestBuildCustomExtBlock_BuiltinDeps(t *testing.T) {
	// imap's PECL build needs Alpine packages that aren't in the base image
	// (otherwise "U8T_CANONICAL is missing"), and it asks interactive prompts.
	block := buildCustomExtBlock([]string{"imap"}, nil)
	if !strings.Contains(block, "apk add --no-cache imap-dev krb5-dev openssl-dev c-client && ") {
		t.Errorf("imap block must apk add its built-in deps before installing:\n%s", block)
	}
	if !strings.Contains(block, "yes '' | pecl install imap") {
		t.Errorf("imap block must feed default answers to PECL prompts:\n%s", block)
	}
	if !strings.Contains(block, "|| true; }") {
		t.Errorf("block must keep the `|| true` resilience guard:\n%s", block)
	}
}

func TestBuildCustomExtBlock_UserDepsUnionedWithBuiltin(t *testing.T) {
	userDeps := map[string][]string{
		"imap": {"krb5-dev", "extra-pkg"}, // krb5-dev is also built-in → deduped
		"ssh2": {"libssh2-dev"},
	}
	block := buildCustomExtBlock([]string{"imap", "ssh2", "redis"}, userDeps)
	// imap: built-in (imap-dev krb5-dev openssl-dev c-client) ∪ user (krb5-dev extra-pkg)
	if !strings.Contains(block, "apk add --no-cache imap-dev krb5-dev openssl-dev c-client extra-pkg && ") {
		t.Errorf("imap block must union built-in + user deps without duplicates:\n%s", block)
	}
	// ssh2: only user deps
	if !strings.Contains(block, "apk add --no-cache libssh2-dev && ") {
		t.Errorf("ssh2 block must apk add the user-supplied dep:\n%s", block)
	}
	// redis: no deps anywhere → no apk add
	for _, line := range strings.Split(block, "\n") {
		if strings.Contains(line, "pecl install redis") && strings.Contains(line, "apk add") {
			t.Errorf("redis block should not have an apk add line:\n%s", line)
		}
	}
}

func TestBuildCustomExtRuntimeDeps_Empty(t *testing.T) {
	if got := buildCustomExtRuntimeDeps(nil, nil); got != "" {
		t.Errorf("empty input should produce empty runtime block, got: %q", got)
	}
	if got := buildCustomExtRuntimeDeps([]string{"redis"}, nil); got != "" {
		t.Errorf("extension with no apk deps should produce empty block, got: %q", got)
	}
}

func TestBuildCustomExtRuntimeDeps_MatchesBuilderDeps(t *testing.T) {
	// The runtime block must install exactly the apk packages that the
	// builder block (buildCustomExtBlock) installs for the same input.
	// Drift between the two would mean compiled .so files can dlopen at
	// build time but fail at runtime.
	cases := []struct {
		exts     []string
		userDeps map[string][]string
	}{
		{[]string{"imap"}, nil},
		{[]string{"imap", "ssh2"}, map[string][]string{"ssh2": {"libssh2-dev"}}},
		{[]string{"imap"}, map[string][]string{"imap": {"extra-pkg"}}},
	}
	for _, c := range cases {
		runtime := buildCustomExtRuntimeDeps(c.exts, c.userDeps)
		builder := buildCustomExtBlock(c.exts, c.userDeps)
		if runtime == "" {
			t.Errorf("exts=%v deps=%v: runtime block unexpectedly empty", c.exts, c.userDeps)
			continue
		}
		// Every package that appears in the builder block's `apk add` must
		// also appear in the runtime block.
		var seen []string
		for _, ext := range c.exts {
			seen = append(seen, apkDepsForExt(ext, c.userDeps)...)
		}
		for _, pkg := range seen {
			if !strings.Contains(runtime, " "+pkg+" ") && !strings.HasSuffix(strings.TrimSpace(runtime), pkg+" && rm -rf /var/cache/apk/*") {
				if !strings.Contains(runtime, pkg) {
					t.Errorf("exts=%v: runtime block missing pkg %q\n  builder: %s\n  runtime: %s", c.exts, pkg, builder, runtime)
				}
			}
		}
	}
}

func TestBuildCustomExtRuntimeDeps_DedupsAcrossExts(t *testing.T) {
	// Two extensions sharing the same dep (krb5-dev appears in imap's
	// built-in list and is also a user dep) must list it exactly once.
	got := buildCustomExtRuntimeDeps(
		[]string{"imap", "ssh2"},
		map[string][]string{
			"imap": {"krb5-dev"},
			"ssh2": {"krb5-dev", "libssh2-dev"},
		},
	)
	if strings.Count(got, " krb5-dev ") > 1 {
		t.Errorf("krb5-dev should appear once in the runtime apk list, got:\n%s", got)
	}
}

func TestApkDepsForExt_Dedup(t *testing.T) {
	got := apkDepsForExt("imap", map[string][]string{"imap": {" krb5-dev ", "extra", "", "imap-dev"}})
	want := []string{"imap-dev", "krb5-dev", "openssl-dev", "c-client", "extra"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("apkDepsForExt(imap) = %v, want %v", got, want)
	}
	if apkDepsForExt("redis", nil) != nil {
		t.Errorf("apkDepsForExt(redis, nil) should be nil")
	}
}

func TestParseApkDeps(t *testing.T) {
	good := map[string][]string{
		"":                         nil,
		"   ":                      nil,
		"libssh2-dev":              {"libssh2-dev"},
		"imap-dev krb5-dev":        {"imap-dev", "krb5-dev"},
		"a, b ,c":                  {"a", "b", "c"},
		"libmemcached-dev\tzlib1g": {"libmemcached-dev", "zlib1g"},
		"openssl-dev c-client":     {"openssl-dev", "c-client"},
	}
	for in, want := range good {
		got, err := ParseApkDeps(in)
		if err != nil {
			t.Errorf("ParseApkDeps(%q) unexpected error: %v", in, err)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ParseApkDeps(%q) = %v, want %v", in, got, want)
		}
	}
	for _, bad := range []string{"foo; rm -rf /", "$(whoami)", "a&&b", "with space evil`"} {
		if _, err := ParseApkDeps(bad); err == nil {
			t.Errorf("ParseApkDeps(%q) should have errored", bad)
		}
	}
}

// git is needed at runtime by composer for VCS-typed repositories and
// any plugin that shells out to it. Re-dropped accidentally by #364.
func TestPhpFpmContainerfile_RuntimeIncludesGit(t *testing.T) {
	tmpl, err := GetQuadletTemplate("lerd-php-fpm.Containerfile")
	if err != nil {
		t.Fatalf("read containerfile: %v", err)
	}
	_, runtime, ok := strings.Cut(tmpl, "# ── Runtime stage")
	if !ok {
		t.Fatal("runtime stage marker missing from Containerfile")
	}
	if !strings.Contains(runtime, "\n        git \\\n") {
		t.Errorf("runtime stage must apk add git so composer can clone VCS repos:\n%s", runtime)
	}
}

func TestPhpExtensionLoaded(t *testing.T) {
	out := "Core\ndate\nimap\nPDO\nZend OPcache\n"
	cases := map[string]bool{
		"imap":         true,
		"IMAP":         true,
		" imap ":       true,
		"date":         true,
		"Zend OPcache": true,
		"pdo":          true,
		"imagick":      false,
		"":             false,
	}
	for ext, want := range cases {
		if got := phpExtensionLoaded(out, ext); got != want {
			t.Errorf("phpExtensionLoaded(out, %q) = %v, want %v", ext, got, want)
		}
	}
}

func TestNeedsFPMRebuild_CacheMatches_NoImages_NoRebuild(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	if err := os.MkdirAll(config.DataDir(), 0755); err != nil {
		t.Fatal(err)
	}
	current, err := ContainerfileHash()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.PHPImageHashFile(), []byte(current), 0644); err != nil {
		t.Fatal(err)
	}

	prev := installedFPMImagesFn
	installedFPMImagesFn = func() []string { return nil }
	t.Cleanup(func() { installedFPMImagesFn = prev })

	if NeedsFPMRebuild() {
		t.Error("expected no rebuild when cache matches and no images installed")
	}
}

func TestNeedsFPMRebuild_CacheMatches_LabelMatches_NoRebuild(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	if err := os.MkdirAll(config.DataDir(), 0755); err != nil {
		t.Fatal(err)
	}
	current, _ := ContainerfileHash()
	_ = os.WriteFile(config.PHPImageHashFile(), []byte(current), 0644)

	prevImages := installedFPMImagesFn
	prevLabel := imageLabelFn
	installedFPMImagesFn = func() []string { return []string{"lerd-php84-fpm:local"} }
	imageLabelFn = func(image, key string) string { return current }
	t.Cleanup(func() {
		installedFPMImagesFn = prevImages
		imageLabelFn = prevLabel
	})

	if NeedsFPMRebuild() {
		t.Error("expected no rebuild when both cache and label match the embedded Containerfile")
	}
}

func TestNeedsFPMRebuild_CacheMatches_LabelMismatch_TriggersRebuild(t *testing.T) {
	// Poisoned-state recovery: an older lerd binary advanced the cache file
	// without rebuilding, so the cache says "up to date" but the actual
	// image was built from a previous Containerfile and carries the old
	// hash as its label.
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	if err := os.MkdirAll(config.DataDir(), 0755); err != nil {
		t.Fatal(err)
	}
	current, _ := ContainerfileHash()
	_ = os.WriteFile(config.PHPImageHashFile(), []byte(current), 0644)

	prevImages := installedFPMImagesFn
	prevLabel := imageLabelFn
	installedFPMImagesFn = func() []string { return []string{"lerd-php84-fpm:local"} }
	imageLabelFn = func(image, key string) string { return "deadbeefoldhash" }
	t.Cleanup(func() {
		installedFPMImagesFn = prevImages
		imageLabelFn = prevLabel
	})

	if !NeedsFPMRebuild() {
		t.Error("expected rebuild when image label disagrees with the embedded Containerfile hash (the poisoned-state recovery path)")
	}
}

func TestNeedsFPMRebuild_CacheMatches_LegacyImageWithoutLabel_TriggersRebuild(t *testing.T) {
	// Images built by an even older lerd that predates the label entirely
	// must also recover automatically.
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	if err := os.MkdirAll(config.DataDir(), 0755); err != nil {
		t.Fatal(err)
	}
	current, _ := ContainerfileHash()
	_ = os.WriteFile(config.PHPImageHashFile(), []byte(current), 0644)

	prevImages := installedFPMImagesFn
	prevLabel := imageLabelFn
	installedFPMImagesFn = func() []string { return []string{"lerd-php84-fpm:local"} }
	imageLabelFn = func(image, key string) string { return "" }
	t.Cleanup(func() {
		installedFPMImagesFn = prevImages
		imageLabelFn = prevLabel
	})

	if !NeedsFPMRebuild() {
		t.Error("expected rebuild when the image carries no fpm-containerfile-hash label (pre-label lerd build)")
	}
}

func TestNeedsFPMRebuild_CacheMismatch_TriggersRebuild(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	if err := os.MkdirAll(config.DataDir(), 0755); err != nil {
		t.Fatal(err)
	}
	// Stored hash from a hypothetical old template.
	_ = os.WriteFile(config.PHPImageHashFile(), []byte("stale-cached-hash"), 0644)

	prevImages := installedFPMImagesFn
	installedFPMImagesFn = func() []string { return nil }
	t.Cleanup(func() { installedFPMImagesFn = prevImages })

	if !NeedsFPMRebuild() {
		t.Error("expected rebuild when the cache file disagrees with the embedded Containerfile")
	}
}

func TestFPMBuildArgs_ContainsHashLabel(t *testing.T) {
	args := fpmBuildArgs("lerd-php84-fpm:local", "abc123", false)
	if !sliceContainsPair(args, "--label", fpmContainerfileHashLabel+"=abc123") {
		t.Errorf("build args missing the containerfile-hash label\nargs: %v", args)
	}
	if sliceContains(args, "--no-cache") {
		t.Errorf("force=false should not add --no-cache, got: %v", args)
	}
}

func TestFPMBuildArgs_ForceAddsNoCache(t *testing.T) {
	args := fpmBuildArgs("lerd-php84-fpm:local", "abc123", true)
	if !sliceContains(args, "--no-cache") {
		t.Errorf("force=true should add --no-cache, got: %v", args)
	}
	// Label must still be present in the force path so a forced rebuild
	// stamps the current hash and clears any poisoned-state label drift.
	if !sliceContainsPair(args, "--label", fpmContainerfileHashLabel+"=abc123") {
		t.Errorf("force path lost the containerfile-hash label\nargs: %v", args)
	}
}

func TestFPMBuildArgs_TagsImageName(t *testing.T) {
	args := fpmBuildArgs("lerd-php85-fpm:local", "h", false)
	if !sliceContainsPair(args, "-t", "lerd-php85-fpm:local") {
		t.Errorf("missing -t <image> pair\nargs: %v", args)
	}
}

// sliceContains reports whether needle appears in haystack.
func sliceContains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// sliceContainsPair reports whether haystack contains `flag` immediately
// followed by `value` (the exec.Command space-separated arg/value form).
func sliceContainsPair(haystack []string, flag, value string) bool {
	for i := 0; i < len(haystack)-1; i++ {
		if haystack[i] == flag && haystack[i+1] == value {
			return true
		}
	}
	return false
}
