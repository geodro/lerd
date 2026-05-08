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

func TestUserPickedDBFromYAML(t *testing.T) {
	for _, tc := range []struct {
		name string
		yaml map[string]bool
		want bool
	}{
		{"empty", map[string]bool{}, false},
		{"sqlite", map[string]bool{"sqlite": true}, true},
		{"mysql builtin", map[string]bool{"mysql": true}, true},
		{"postgres builtin", map[string]bool{"postgres": true}, true},
		{"redis only", map[string]bool{"redis": true}, false},
		{"redis plus mysql", map[string]bool{"redis": true, "mysql": true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := userPickedDBFromYAML(tc.yaml); got != tc.want {
				t.Errorf("userPickedDBFromYAML(%v) = %v, want %v", tc.yaml, got, tc.want)
			}
		})
	}
}

func TestShouldApplyService(t *testing.T) {
	for _, tc := range []struct {
		name         string
		svc          string
		detected     bool
		picked       bool
		userPickedDB bool
		want         bool
	}{
		// Regression: fresh Laravel project, user picks mysql in `lerd init`.
		// Existing .env still says DB_CONNECTION=sqlite, so detection misses.
		// The .lerd.yaml pick must still cause mysql vars to be applied.
		{"mysql picked, not detected", "mysql", false, true, true, true},

		// Detection-driven application keeps working when the user did not
		// pre-pick a DB (e.g. an imported Sail project where .env already
		// references mysql).
		{"mysql detected, no yaml", "mysql", true, false, false, true},

		// User picked postgres but .env mentions mysql — don't reapply mysql
		// on top of postgres, otherwise switching DBs via the wizard silently
		// keeps the old credentials.
		{"mysql detected, postgres picked", "mysql", true, false, true, false},

		// Non-DB services aren't affected by the userPickedDB guard.
		{"redis detected", "redis", true, false, true, true},
		{"redis picked", "redis", false, true, false, true},
		{"redis neither", "redis", false, false, false, false},

		// Postgres mirror of the mysql cases.
		{"postgres picked, not detected", "postgres", false, true, true, true},
		{"postgres detected, mysql picked", "postgres", true, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldApplyService(tc.svc, tc.detected, tc.picked, tc.userPickedDB)
			if got != tc.want {
				t.Errorf("shouldApplyService(%q, det=%v, picked=%v, userPickedDB=%v) = %v, want %v",
					tc.svc, tc.detected, tc.picked, tc.userPickedDB, got, tc.want)
			}
		})
	}
}
