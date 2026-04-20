package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// writeProject writes a minimal .lerd.yaml at dir with the given AppURL.
func writeProject(t *testing.T, dir, appURL string) {
	t.Helper()
	body := ""
	if appURL != "" {
		body = "app_url: " + appURL + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, ".lerd.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveAppURL(t *testing.T) {
	t.Run(".lerd.yaml beats sites.yaml beats default", func(t *testing.T) {
		dir := t.TempDir()
		writeProject(t, dir, "https://from-project.test")
		site := &config.Site{AppURL: "https://from-sites.test"}
		got := resolveAppURL(dir, site)
		if got != "https://from-project.test" {
			t.Errorf("expected project value to win, got %q", got)
		}
	})

	t.Run("sites.yaml used when .lerd.yaml has no app_url", func(t *testing.T) {
		dir := t.TempDir()
		writeProject(t, dir, "") // .lerd.yaml exists but no app_url
		site := &config.Site{AppURL: "https://from-sites.test"}
		got := resolveAppURL(dir, site)
		if got != "https://from-sites.test" {
			t.Errorf("expected sites.yaml value, got %q", got)
		}
	})

	t.Run("sites.yaml used when no .lerd.yaml exists", func(t *testing.T) {
		dir := t.TempDir() // no .lerd.yaml
		site := &config.Site{AppURL: "https://from-sites.test"}
		got := resolveAppURL(dir, site)
		if got != "https://from-sites.test" {
			t.Errorf("expected sites.yaml value, got %q", got)
		}
	})

	t.Run("falls through to default generator when neither override is set", func(t *testing.T) {
		dir := t.TempDir() // no .lerd.yaml
		site := &config.Site{}
		// siteURL() reads the global registry; for an unregistered tempdir
		// it returns "", which is exactly the "leave APP_URL alone" signal.
		if got := resolveAppURL(dir, site); got != "" {
			t.Errorf("expected empty fallback for unregistered path, got %q", got)
		}
	})

	t.Run("nil site falls through to project then default", func(t *testing.T) {
		dir := t.TempDir()
		writeProject(t, dir, "https://only-project.test")
		got := resolveAppURL(dir, nil)
		if got != "https://only-project.test" {
			t.Errorf("expected project value with nil site, got %q", got)
		}
	})

	t.Run("whitespace in stored value is trimmed", func(t *testing.T) {
		dir := t.TempDir()
		writeProject(t, dir, "  https://padded.test  ")
		got := resolveAppURL(dir, nil)
		if got != "https://padded.test" {
			t.Errorf("expected trimmed value, got %q", got)
		}
	})
}

func TestS3BucketName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"admin_astrolov", "admin-astrolov"},
		{"Admin_Astrolov", "admin-astrolov"},
		{"my-app", "my-app"},
		{"MyApp 2", "myapp-2"},
		{"my.bucket.v2", "my.bucket.v2"},
		{"  ___  ", "lerd"},
		{"", "lerd"},
		{"--app--", "app"},
	}
	for _, tc := range cases {
		if got := s3BucketName(tc.in); got != tc.want {
			t.Errorf("s3BucketName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	long := strings.Repeat("a", 80)
	if got := s3BucketName(long); len(got) != 63 {
		t.Errorf("long input should be clamped to 63, got %d", len(got))
	}
}

func TestApplySiteHandleBucket(t *testing.T) {
	ctx := siteTemplateCtx{site: "admin_astrolov", bucket: "admin-astrolov"}
	got := applySiteHandle("AWS_BUCKET={{bucket}}", ctx)
	if got != "AWS_BUCKET=admin-astrolov" {
		t.Errorf("expected sanitised bucket, got %q", got)
	}
	gotSite := applySiteHandle("DB_DATABASE={{site}}", ctx)
	if gotSite != "DB_DATABASE=admin_astrolov" {
		t.Errorf("{{site}} should preserve underscores, got %q", gotSite)
	}
}
