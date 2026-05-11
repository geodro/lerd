package podman

import (
	"strings"
	"testing"
)

func TestBuildCustomExtBlock_Empty(t *testing.T) {
	if got := buildCustomExtBlock(nil); got != "" {
		t.Errorf("expected empty block for no extensions, got:\n%s", got)
	}
}

func TestBuildCustomExtBlock_ImapPullsApkDeps(t *testing.T) {
	// imap's PECL build needs Alpine packages that aren't in the base image;
	// without them it fails with "U8T_CANONICAL is missing", and it also asks
	// interactive prompts that must be fed defaults. See #334.
	block := buildCustomExtBlock([]string{"imap"})
	for _, pkg := range []string{"imap-dev", "krb5-dev", "openssl-dev", "c-client"} {
		if !strings.Contains(block, pkg) {
			t.Errorf("imap block must apk add %q before pecl install:\n%s", pkg, block)
		}
	}
	if !strings.Contains(block, "apk add --no-cache imap-dev krb5-dev openssl-dev c-client && ") {
		t.Errorf("imap block must apk add the deps before installing:\n%s", block)
	}
	if !strings.Contains(block, "yes '' | pecl install imap") {
		t.Errorf("imap block must feed default answers to PECL prompts:\n%s", block)
	}
}

func TestBuildCustomExtBlock_UnmappedExtNoApkAdd(t *testing.T) {
	// redis' deps are already in the base image, so no extra apk add line, but
	// it still goes through the prompt-feeding pecl install.
	block := buildCustomExtBlock([]string{"redis"})
	if strings.Contains(block, "apk add") {
		t.Errorf("redis block should not add an apk line:\n%s", block)
	}
	if !strings.Contains(block, "yes '' | pecl install redis") {
		t.Errorf("redis block must pecl install redis:\n%s", block)
	}
}

func TestBuildCustomExtBlock_KeepsResilienceGuard(t *testing.T) {
	// The trailing "|| true" keeps a later `lerd php:rebuild` from bricking the
	// whole image if a previously-good extension stops building; php:ext add
	// relies on VerifyExtensionLoaded (not the build exit code) to catch failures.
	for _, ext := range []string{"imap", "redis"} {
		block := buildCustomExtBlock([]string{ext})
		if !strings.Contains(block, "|| true; }") {
			t.Errorf("%s block must keep the `|| true` resilience guard:\n%s", ext, block)
		}
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
