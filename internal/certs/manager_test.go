package certs

import (
	"os"
	"path/filepath"
	"testing"
)

// ── MkcertPath ────────────────────────────────────────────────────────────────

func TestMkcertPath_usesDataDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	got := MkcertPath()
	want := filepath.Join(tmp, "lerd", "bin", "mkcert")
	if got != want {
		t.Errorf("MkcertPath() = %q, want %q", got, want)
	}
}

// ── CertExists ────────────────────────────────────────────────────────────────

func TestCertExists_returnsFalseWhenMissing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	if CertExists("myapp.test") {
		t.Error("expected false for non-existent cert")
	}
}

func TestCertExists_returnsTrueWhenPresent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	// Create the expected cert file path
	certsDir := filepath.Join(tmp, "lerd", "certs", "sites")
	os.MkdirAll(certsDir, 0755)
	os.WriteFile(filepath.Join(certsDir, "myapp.test.crt"), []byte("fake cert"), 0644)

	if !CertExists("myapp.test") {
		t.Error("expected true when cert file exists")
	}
}

func TestCertExists_onlyCrtRequired(t *testing.T) {
	// CertExists checks for .crt only, not .key
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	certsDir := filepath.Join(tmp, "lerd", "certs", "sites")
	os.MkdirAll(certsDir, 0755)
	// .crt exists, no .key
	os.WriteFile(filepath.Join(certsDir, "site.test.crt"), []byte("fake cert"), 0644)

	if !CertExists("site.test") {
		t.Error("expected true when only .crt file exists")
	}
}

// ── IssueCertForce atomicity ─────────────────────────────────────────────────

// IssueCertForce must leave the existing cert/key intact when mkcert fails,
// otherwise a transient error would trip RepairVhosts into flipping the site
// to plain HTTP on the next start.
func TestIssueCertForce_failureLeavesExistingCertIntact(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	binDir := filepath.Join(tmp, "lerd", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Fake mkcert that exits non-zero.
	mkcertScript := "#!/bin/sh\necho 'simulated mkcert failure' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "mkcert"), []byte(mkcertScript), 0755); err != nil {
		t.Fatal(err)
	}

	certsDir := filepath.Join(tmp, "lerd", "certs", "sites")
	if err := os.MkdirAll(certsDir, 0755); err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(certsDir, "myapp.test.crt")
	keyPath := filepath.Join(certsDir, "myapp.test.key")
	originalCert := []byte("EXISTING-CERT-PEM")
	originalKey := []byte("EXISTING-KEY-PEM")
	if err := os.WriteFile(certPath, originalCert, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, originalKey, 0644); err != nil {
		t.Fatal(err)
	}

	err := IssueCertForce("myapp.test", []string{"myapp.test", "feat-x.myapp.test"}, certsDir)
	if err == nil {
		t.Fatal("expected error when mkcert fails, got nil")
	}

	gotCert, readErr := os.ReadFile(certPath)
	if readErr != nil {
		t.Fatalf("existing cert was deleted after failure: %v", readErr)
	}
	if string(gotCert) != string(originalCert) {
		t.Errorf("cert overwritten on failure path; got %q want %q", gotCert, originalCert)
	}
	gotKey, readErr := os.ReadFile(keyPath)
	if readErr != nil {
		t.Fatalf("existing key was deleted after failure: %v", readErr)
	}
	if string(gotKey) != string(originalKey) {
		t.Errorf("key overwritten on failure path")
	}

	// Temp paths must be cleaned up too — leftover .new files would
	// confuse a subsequent successful reissue.
	if _, err := os.Stat(certPath + ".new"); !os.IsNotExist(err) {
		t.Error("temp .crt.new not cleaned up after failure")
	}
	if _, err := os.Stat(keyPath + ".new"); !os.IsNotExist(err) {
		t.Error("temp .key.new not cleaned up after failure")
	}
}
