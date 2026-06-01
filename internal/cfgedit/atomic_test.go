package cfgedit

import (
	"os"
	"path/filepath"
	"testing"
)

// Per-site vhosts include custom.d/<domain>.conf* (note the trailing wildcard).
// The atomic-write temp used during a restore or rollback must never match that
// glob, or a concurrent nginx reload could load the half-written temp file.
func TestAtomicTmpPrefixNeverMatchesNginxIncludeGlob(t *testing.T) {
	const glob = "site.test.conf*"

	// Sanity: the old sibling temp name (<path>.tmp) did match the glob, which
	// is exactly the regression this prefix guards against.
	if m, err := filepath.Match(glob, "site.test.conf.tmp"); err != nil {
		t.Fatal(err)
	} else if !m {
		t.Fatal("sanity: expected the old <domain>.conf.tmp sibling to match the include glob")
	}

	tempName := atomicTmpPrefix + "site.test.conf" + ".1234567890"
	if m, err := filepath.Match(glob, tempName); err != nil {
		t.Fatal(err)
	} else if m {
		t.Fatalf("atomic temp name %q matches the per-site include glob %q", tempName, glob)
	}
}

func TestWriteFileAtomicLeavesNoGlobMatchingTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "site.test.conf")
	body := []byte("server {}\n")
	if err := writeFileAtomic(path, body, 0o644); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("content mismatch: got %q want %q", got, body)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == "site.test.conf" {
			continue
		}
		if m, _ := filepath.Match("site.test.conf*", e.Name()); m {
			t.Errorf("leftover file %q matches nginx include glob site.test.conf*", e.Name())
		}
	}
}
