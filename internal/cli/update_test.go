package cli

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	lerdUpdate "github.com/geodro/lerd/internal/update"
)

// ── stripV ───────────────────────────────────────────────────────────────────

func TestStripV(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"v1.2.3", "1.2.3"},
		{"1.2.3", "1.2.3"},
		{"v0.1.0", "0.1.0"},
		{"", ""},
		{"v", ""},
	}
	for _, c := range cases {
		got := stripV(c.in)
		if got != c.want {
			t.Errorf("stripV(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ── fetchLatestVersion ───────────────────────────────────────────────────────

func TestFetchLatestVersion_success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.String()+"/tag/v1.2.3", http.StatusFound)
	}))
	defer srv.Close()

	orig := lerdUpdate.ReleasesBaseURL
	lerdUpdate.ReleasesBaseURL = srv.URL
	defer func() { lerdUpdate.ReleasesBaseURL = orig }()

	got, err := lerdUpdate.FetchLatestVersion()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "v1.2.3" {
		t.Errorf("got %q, want v1.2.3", got)
	}
}

func TestFetchLatestVersion_withoutVPrefix(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.String()+"/tag/0.9.0", http.StatusFound)
	}))
	defer srv.Close()

	orig := lerdUpdate.ReleasesBaseURL
	lerdUpdate.ReleasesBaseURL = srv.URL
	defer func() { lerdUpdate.ReleasesBaseURL = orig }()

	got, err := lerdUpdate.FetchLatestVersion()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "0.9.0" {
		t.Errorf("got %q, want 0.9.0", got)
	}
}

func TestFetchLatestVersion_notFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	orig := lerdUpdate.ReleasesBaseURL
	lerdUpdate.ReleasesBaseURL = srv.URL
	defer func() { lerdUpdate.ReleasesBaseURL = orig }()

	_, err := lerdUpdate.FetchLatestVersion()
	if err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}
}

func TestFetchLatestVersion_emptyTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.String()+"/tag/", http.StatusFound)
	}))
	defer srv.Close()

	orig := lerdUpdate.ReleasesBaseURL
	lerdUpdate.ReleasesBaseURL = srv.URL
	defer func() { lerdUpdate.ReleasesBaseURL = orig }()

	_, err := lerdUpdate.FetchLatestVersion()
	if err == nil {
		t.Fatal("expected error for empty tag, got nil")
	}
}

func TestFetchLatestVersion_serverError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	orig := lerdUpdate.ReleasesBaseURL
	lerdUpdate.ReleasesBaseURL = srv.URL
	defer func() { lerdUpdate.ReleasesBaseURL = orig }()

	_, err := lerdUpdate.FetchLatestVersion()
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

// ── downloadReleaseBinary ────────────────────────────────────────────────────

// makeFakeTarGz creates a .tar.gz archive in dir containing a file named "lerd"
// with the given content.
func makeFakeTarGz(t *testing.T, dir, content string) string {
	t.Helper()
	archivePath := filepath.Join(dir, "lerd_0.1.0_linux_amd64.tar.gz")
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	data := []byte(content)
	tw.WriteHeader(&tar.Header{Name: "lerd", Mode: 0755, Size: int64(len(data))})
	tw.Write(data)
	return archivePath
}

func TestDownloadReleaseBinary_success(t *testing.T) {
	// Build a fake tar.gz to serve
	tmp := t.TempDir()
	makeFakeTarGz(t, tmp, "#!/bin/sh\necho lerd")

	archiveBytes, err := os.ReadFile(filepath.Join(tmp, "lerd_0.1.0_linux_amd64.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.Write(archiveBytes)
	}))
	defer srv.Close()

	orig := githubDownloadBase
	githubDownloadBase = srv.URL
	defer func() { githubDownloadBase = orig }()

	binary, cleanup, err := downloadReleaseBinary("v0.1.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cleanup()

	if _, err := os.Stat(binary); err != nil {
		t.Errorf("binary not found at %s: %v", binary, err)
	}
}

func TestDownloadReleaseBinary_serverError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	orig := githubDownloadBase
	githubDownloadBase = srv.URL
	defer func() { githubDownloadBase = orig }()

	_, cleanup, err := downloadReleaseBinary("v0.1.0")
	cleanup()
	if err == nil {
		t.Fatal("expected error for 404 download, got nil")
	}
}

// ── copyFile ─────────────────────────────────────────────────────────────────

func TestCopyFile(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	dst := filepath.Join(tmp, "dst")

	if err := os.WriteFile(src, []byte("hello lerd"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := copyFile(src, dst, 0755); err != nil {
		t.Fatalf("copyFile error: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello lerd" {
		t.Errorf("got %q, want %q", got, "hello lerd")
	}

	info, _ := os.Stat(dst)
	if info.Mode() != 0755 {
		t.Errorf("mode = %v, want 0755", info.Mode())
	}
}

func TestCopyFile_missingSource(t *testing.T) {
	tmp := t.TempDir()
	err := copyFile(filepath.Join(tmp, "nope"), filepath.Join(tmp, "dst"), 0644)
	if err == nil {
		t.Fatal("expected error for missing source, got nil")
	}
}

// ── runUpdate (integration-style) ────────────────────────────────────────────

func TestRunUpdate_alreadyLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.String()+"/tag/v1.0.0", http.StatusFound)
	}))
	defer srv.Close()

	orig := lerdUpdate.ReleasesBaseURL
	lerdUpdate.ReleasesBaseURL = srv.URL
	defer func() { lerdUpdate.ReleasesBaseURL = orig }()

	// Should return nil without downloading anything
	err := runUpdate("1.0.0", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ── VersionGreaterThan pre-release ──────────────────────────────────────────

func TestVersionGreaterThan_prerelease(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.5.0", "1.5.0-beta.1", true},
		{"1.5.0-beta.1", "1.5.0", false},
		{"1.5.0-beta.2", "1.5.0-beta.1", true},
		{"1.5.0-rc.1", "1.5.0-beta.1", true},
		{"1.5.0-beta.1", "1.5.0-rc.1", false},
		{"1.5.0-beta.1", "1.5.0-beta.1", false},
		{"2.0.0-beta.1", "1.5.0", true},
		{"1.4.0", "1.5.0-beta.1", false},
		// Existing stable comparisons still work.
		{"1.5.0", "1.4.0", true},
		{"1.4.0", "1.5.0", false},
		{"1.5.0", "1.5.0", false},
	}
	for _, c := range cases {
		got := lerdUpdate.VersionGreaterThan(c.a, c.b)
		if got != c.want {
			t.Errorf("VersionGreaterThan(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// ── stripGitDescribe ────────────────────────────────────────────────────────

func TestStripGitDescribe(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"1.5.0", "1.5.0"},
		{"1.5.0-dirty", "1.5.0"},
		{"1.5.0-5-gabcdef0", "1.5.0"},
		{"1.5.0-dirty", "1.5.0"},
		{"1.5.0-beta.1", "1.5.0-beta.1"},
		{"1.5.0-rc.1", "1.5.0-rc.1"},
		{"1.5.0-beta.1-dirty", "1.5.0-beta.1"},
		{"1.5.0-beta.1-3-g1a2b3c4", "1.5.0-beta.1"},
	}
	for _, c := range cases {
		got := lerdUpdate.StripGitDescribe(c.in)
		if got != c.want {
			t.Errorf("StripGitDescribe(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ── FetchLatestPrerelease ───────────────────────────────────────────────────

func TestFetchLatestPrerelease_success(t *testing.T) {
	releases := []lerdUpdate.GithubReleaseForTest{
		{TagName: "v1.5.0-beta.1", Prerelease: true, Draft: false},
		{TagName: "v1.4.0", Prerelease: false, Draft: false},
	}
	body, _ := json.Marshal(releases)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	orig := lerdUpdate.APIBaseURL
	lerdUpdate.APIBaseURL = srv.URL
	defer func() { lerdUpdate.APIBaseURL = orig }()

	got, err := lerdUpdate.FetchLatestPrerelease()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "v1.5.0-beta.1" {
		t.Errorf("got %q, want v1.5.0-beta.1", got)
	}
}

func TestFetchLatestPrerelease_noPrerelease(t *testing.T) {
	releases := []lerdUpdate.GithubReleaseForTest{
		{TagName: "v1.4.0", Prerelease: false, Draft: false},
	}
	body, _ := json.Marshal(releases)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	orig := lerdUpdate.APIBaseURL
	lerdUpdate.APIBaseURL = srv.URL
	defer func() { lerdUpdate.APIBaseURL = orig }()

	_, err := lerdUpdate.FetchLatestPrerelease()
	if err == nil {
		t.Fatal("expected error when no pre-release available, got nil")
	}
}

func TestFetchLatestPrerelease_serverError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	orig := lerdUpdate.APIBaseURL
	lerdUpdate.APIBaseURL = srv.URL
	defer func() { lerdUpdate.APIBaseURL = orig }()

	_, err := lerdUpdate.FetchLatestPrerelease()
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

// ── backupBinary ────────────────────────────────────────────────────────────

func TestBackupBinary(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	// Create a data dir for the backup files.
	dataDir := filepath.Join(tmp, "lerd")
	os.MkdirAll(dataDir, 0755)

	// Create a fake binary in a separate subdirectory to avoid collision.
	binDir := filepath.Join(tmp, "bin")
	os.MkdirAll(binDir, 0755)
	fakeBin := filepath.Join(binDir, "lerd")
	os.WriteFile(fakeBin, []byte("fake-binary"), 0755)

	backupBinary(fakeBin, "1.4.0")

	// Check backup file exists.
	bakData, err := os.ReadFile(filepath.Join(dataDir, "lerd.bak"))
	if err != nil {
		t.Fatalf("backup binary not created: %v", err)
	}
	if string(bakData) != "fake-binary" {
		t.Errorf("backup content = %q, want %q", bakData, "fake-binary")
	}

	// Check version file.
	verData, err := os.ReadFile(filepath.Join(dataDir, "rollback-version"))
	if err != nil {
		t.Fatalf("rollback-version not created: %v", err)
	}
	if string(verData) != "1.4.0" {
		t.Errorf("version = %q, want %q", verData, "1.4.0")
	}
}

// ── rollback ────────────────────────────────────────────────────────────────

func TestRunRollback_noBackup(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	os.MkdirAll(filepath.Join(tmp, "lerd"), 0755)

	err := runRollback()
	if err == nil {
		t.Fatal("expected error when no backup exists, got nil")
	}
}

// ── mutual exclusivity ─────────────────────────────────────────────────────

func TestUpdateCmd_mutuallyExclusive(t *testing.T) {
	cmd := NewUpdateCmd("1.0.0")
	cmd.SetArgs([]string{"--beta", "--rollback"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when both --beta and --rollback are set, got nil")
	}
}
