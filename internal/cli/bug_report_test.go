package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScrubHomePath_replacesHome(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")
	t.Setenv("USER", "testuser")
	t.Setenv("LOGNAME", "testuser")
	in := "log line referencing /home/testuser/.config/lerd/config.yaml here"
	out := scrubHomePath(in)
	if strings.Contains(out, "/home/testuser") {
		t.Fatalf("home path not scrubbed: %q", out)
	}
	if !strings.Contains(out, "$HOME/.config/lerd/config.yaml") {
		t.Fatalf("expected $HOME placeholder, got: %q", out)
	}
}

func TestScrubHomePath_replacesBareUsername(t *testing.T) {
	t.Setenv("HOME", "/home/alice")
	t.Setenv("USER", "alice")
	t.Setenv("LOGNAME", "alice")
	in := "user alice logged in; podman socket /run/user/1000/podman.sock; alice@host"
	out := scrubHomePath(in)
	if strings.Contains(out, "alice") {
		t.Fatalf("bare username not scrubbed: %q", out)
	}
	for _, want := range []string{"user $USER logged in", "$USER@host"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in output: %q", want, out)
		}
	}
}

func TestScrubHomePath_skipsShortUsername(t *testing.T) {
	t.Setenv("HOME", "/home/jo")
	t.Setenv("USER", "jo")
	t.Setenv("LOGNAME", "jo")
	in := "joined the channel; project=jojo"
	out := scrubHomePath(in)
	if out != in {
		t.Fatalf("short username should not be replaced: got %q", out)
	}
}

func TestScrubHomePath_emptyHome(t *testing.T) {
	t.Setenv("HOME", "")
	in := "no home set"
	if got := scrubHomePath(in); got != in {
		t.Fatalf("expected unchanged, got %q", got)
	}
}

func TestScrubHomePath_rootHome(t *testing.T) {
	t.Setenv("HOME", "/")
	in := "/etc/foo and / and /home/x"
	if got := scrubHomePath(in); got != in {
		t.Fatalf("expected unchanged when HOME=/, got %q", got)
	}
}

func TestWriteBugReportHeader_includesVersionAndOS(t *testing.T) {
	var buf bytes.Buffer
	writeBugReportHeader(&buf, nil)
	out := buf.String()
	for _, want := range []string{"Lerd bug report", "lerd:", "OS:", "Generated:"} {
		if !strings.Contains(out, want) {
			t.Errorf("header missing %q\n%s", want, out)
		}
	}
}

func TestWriteBugReport_createsFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "report.txt")
	got, err := writeBugReport(target, 5, false)
	if err != nil {
		t.Fatalf("writeBugReport: %v", err)
	}
	if got != target {
		t.Errorf("path mismatch: got %s want %s", got, target)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	for _, want := range []string{"Lerd bug report", "Doctor", "Config files", "Environment"} {
		if !bytes.Contains(data, []byte(want)) {
			t.Errorf("report missing %q", want)
		}
	}
}

func TestWriteBugReport_defaultPath(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	got, err := writeBugReport("", 5, false)
	if err != nil {
		t.Fatalf("writeBugReport: %v", err)
	}
	if !strings.HasPrefix(filepath.Base(got), "lerd-bug-report-") {
		t.Errorf("default filename doesn't start with lerd-bug-report-: %s", got)
	}
	// EvalSymlinks both sides because macOS resolves /var → /private/var,
	// so t.TempDir() and os.Getwd()-after-chdir return different forms.
	gotDir, _ := filepath.EvalSymlinks(filepath.Dir(got))
	wantDir, _ := filepath.EvalSymlinks(dir)
	if gotDir != wantDir {
		t.Errorf("default file not in cwd: %s (cwd=%s)", got, dir)
	}
}

// ── Anonymizer ───────────────────────────────────────────────────────────────

// setupAnonFixtures writes a sites.yaml and config.yaml into temp dirs so
// newAnonymizer has something deterministic to read from.
func setupAnonFixtures(t *testing.T, configYAML, sitesYAML string) {
	t.Helper()
	cfgDir := t.TempDir()
	dataDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	t.Setenv("XDG_DATA_HOME", dataDir)
	t.Setenv("HOME", t.TempDir())

	if err := os.MkdirAll(filepath.Join(cfgDir, "lerd"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "lerd", "config.yaml"), []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "lerd"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "lerd", "sites.yaml"), []byte(sitesYAML), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestAnonymizer_replacesSiteNamesAndDomains(t *testing.T) {
	setupAnonFixtures(t, "dns:\n    enabled: true\n    tld: test\n", `sites:
  - name: laravel
    domains: [laravel.test, api.laravel.test]
    path: /srv/laravel
`)
	a := newAnonymizer()
	in := "lerd-queue-laravel restarted, see http://laravel.test/health and http://api.laravel.test/x"
	out := a.Apply(in)
	if strings.Contains(out, "laravel") {
		t.Errorf("expected `laravel` to be replaced, got: %s", out)
	}
	for _, want := range []string{"lerd-queue-site-1", "site-1.test", "site-1-extra1.test"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q: %s", want, out)
		}
	}
}

func TestAnonymizer_replacesParkedDir(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	setupAnonFixtures(t, "parked_directories:\n  - /home/u/Projects\n  - /srv/extra\n", "sites: []\n")
	// Re-set HOME after setupAnonFixtures (it overrides it).
	t.Setenv("HOME", "/home/u")
	a := newAnonymizer()
	in := "site at /home/u/Projects/foo and /srv/extra/bar"
	out := a.Apply(in)
	if !strings.Contains(out, "$PARK_1/foo") {
		t.Errorf("expected $PARK_1, got: %s", out)
	}
	if !strings.Contains(out, "$PARK_2/bar") {
		t.Errorf("expected $PARK_2, got: %s", out)
	}
}

func TestAnonymizer_sitePathReplacedWholeBeforeBareName(t *testing.T) {
	// Site `foo15` has its name appearing inside another site's path
	// (`/srv/foo15.ro`). Without the full-path pair the bare-name
	// replacement would corrupt the second site's path.
	setupAnonFixtures(t, "", `sites:
  - name: alpha
    domains: [alpha.test]
    path: /srv/foo15.ro
  - name: foo15
    domains: [foo15.test]
    path: /srv/foo15
`)
	a := newAnonymizer()
	in := "alpha lives at /srv/foo15.ro and foo15 lives at /srv/foo15"
	out := a.Apply(in)
	// Both site paths must collapse to opaque tokens, not partial
	// substring rewrites.
	if !strings.Contains(out, "$SITE_1_PATH") {
		t.Errorf("expected $SITE_1_PATH, got: %s", out)
	}
	if !strings.Contains(out, "$SITE_2_PATH") {
		t.Errorf("expected $SITE_2_PATH, got: %s", out)
	}
	// Bare site name in prose still becomes site-2.
	if !strings.Contains(out, "site-2 lives at") {
		t.Errorf("expected bare-name replacement, got: %s", out)
	}
}

func TestAnonymizer_sitePathInsideParkedDir(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	setupAnonFixtures(t, "parked_directories:\n  - /home/u/Lerd\n", `sites:
  - name: laravel
    domains: [laravel.test]
    path: /home/u/Lerd/laravel
`)
	t.Setenv("HOME", "/home/u")
	a := newAnonymizer()
	in := "site path /home/u/Lerd/laravel"
	out := a.Apply(in)
	if !strings.Contains(out, "$PARK_1/site-1") {
		t.Errorf("expected $PARK_1/site-1, got: %s", out)
	}
}

func TestAnonymizer_nilAndEmptySafe(t *testing.T) {
	var a *anonymizer
	if got := a.Apply("hi"); got != "hi" {
		t.Errorf("nil receiver: got %q", got)
	}
	empty := &anonymizer{}
	if got := empty.Apply("hi"); got != "hi" {
		t.Errorf("empty: got %q", got)
	}
	if a.active() {
		t.Error("nil should not be active")
	}
	if empty.active() {
		t.Error("empty should not be active")
	}
}

// ── Content-unit and access-log filters ─────────────────────────────────────

func TestIsContentUnit(t *testing.T) {
	cases := map[string]bool{
		// Lerd-core infra: kept, .service/.container suffixes tolerated.
		"lerd-nginx":           false,
		"lerd-ui":              false,
		"lerd-watcher":         false,
		"lerd-dns":             false,
		"lerd-tray":            false,
		"lerd-autostart":       false,
		"lerd-fpm-init":        false,
		"lerd-nginx.container": false,
		"lerd-ui.service":      false,
		// Preset services: dropped — app-domain noise.
		"lerd-redis":       true,
		"lerd-mysql":       true,
		"lerd-postgres":    true,
		"lerd-gotenberg":   true,
		"lerd-meilisearch": true,
		// FPM and per-site workers: dropped.
		"lerd-php83-fpm":        true,
		"lerd-php85-fpm":        true,
		"lerd-queue-laravel":    true,
		"lerd-schedule-laravel": true,
		"lerd-horizon-myapp":    true,
		"lerd-stripe-myapp":     true,
		"lerd-reverb-myapp":     true,
		// Custom containers / FrankenPHP per-site: dropped.
		"lerd-custom-myapp": true,
		"lerd-fp-myapp":     true,
	}
	for name, want := range cases {
		if got := isContentUnit(name); got != want {
			t.Errorf("isContentUnit(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestIsPrivateUnit(t *testing.T) {
	custom := map[string]struct{}{
		"lerd-myservice": {},
	}
	cases := map[string]bool{
		"lerd-nginx":              false,
		"lerd-redis":              false, // preset, not private
		"lerd-myservice":          true,  // user-defined custom service
		"lerd-myservice.service":  true,
		"lerd-custom-myapp":       true,
		"lerd-custom-x.container": true,
		"lerd-fp-myapp":           true,
		"lerd-fp-x.container":     true,
		"lerd-queue-laravel":      false, // worker, not private
		"lerd-php83-fpm":          false,
	}
	for name, want := range cases {
		if got := isPrivateUnit(name, custom); got != want {
			t.Errorf("isPrivateUnit(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestLogFilter_dropsCLF(t *testing.T) {
	setupAnonFixtures(t, "", "sites: []\n")
	f := newLogFilter()
	in := strings.Join([]string{
		`May 03 17:54:56 host lerd-nginx[1872]: 10.89.0.8 - - [03/May/2026:14:54:56 +0000] "GET / HTTP/1.1" 200 1205 "-" "UA"`,
		`May 03 17:54:57 host lerd-nginx[1872]: 2026/05/03 14:54:57 [error] something broke`,
	}, "\n")
	out := f.clean(in)
	if strings.Contains(out, `"GET / HTTP/1.1"`) {
		t.Errorf("CLF line not dropped: %s", out)
	}
	if !strings.Contains(out, "something broke") {
		t.Errorf("error line lost: %s", out)
	}
}

func TestLogFilter_dropsMeilisearchHTTP(t *testing.T) {
	setupAnonFixtures(t, "", "sites: []\n")
	f := newLogFilter()
	in := `INFO HTTP request{method=GET host="localhost:7700" route=/}: meilisearch: close`
	if got := f.clean(in); got != "" {
		t.Errorf("meilisearch HTTP line not dropped: %q", got)
	}
}

func TestLogFilter_redactsRequestAndUpstream(t *testing.T) {
	setupAnonFixtures(t, "", "sites: []\n")
	f := newLogFilter()
	in := `[error] 61#61: connect() failed, request: "GET /app/sensitive?token=abc HTTP/1.1", upstream: "http://10.89.7.8:8080/app/sensitive?token=abc", referrer: "https://other.com/page"`
	out := f.clean(in)
	for _, leaked := range []string{"sensitive", "token=abc", "other.com"} {
		if strings.Contains(out, leaked) {
			t.Errorf("%q leaked through redaction: %s", leaked, out)
		}
	}
	for _, want := range []string{`"GET <redacted> HTTP/1.1"`, `upstream: "<redacted>"`, `referrer: "<redacted>"`, `connect() failed`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output: %s", want, out)
		}
	}
}

func TestLogFilter_passthroughWhenClean(t *testing.T) {
	setupAnonFixtures(t, "", "sites: []\n")
	f := newLogFilter()
	in := "no http here\n[error] real error"
	if got := f.clean(in); got != in {
		t.Errorf("unexpected mutation: got %q want %q", got, in)
	}
}

func TestLogFilter_dropsLinesMentioningUserDomain(t *testing.T) {
	setupAnonFixtures(t, "", `sites:
  - name: app
    domains: [secret.example.test, api.secret.example.test]
    path: /srv/app
`)
	f := newLogFilter()
	in := strings.Join([]string{
		`some infra line that is fine`,
		`error from worker calling SECRET.EXAMPLE.test/api`,
		`unrelated [error] keepme`,
		`https://api.secret.example.test/x failed`,
	}, "\n")
	out := f.clean(in)
	for _, leaked := range []string{"secret.example.test", "SECRET.EXAMPLE.test", "api.secret.example.test"} {
		if strings.Contains(strings.ToLower(out), strings.ToLower(leaked)) {
			t.Errorf("user domain %q leaked: %s", leaked, out)
		}
	}
	if !strings.Contains(out, "some infra line that is fine") {
		t.Errorf("infra line lost: %s", out)
	}
	if !strings.Contains(out, "unrelated [error] keepme") {
		t.Errorf("unrelated error lost: %s", out)
	}
}

func TestEnvAllowlist_excludesSecrets(t *testing.T) {
	for _, key := range envAllowlist {
		switch strings.ToUpper(key) {
		case "AWS_SECRET_ACCESS_KEY", "GITHUB_TOKEN", "ANTHROPIC_API_KEY":
			t.Errorf("envAllowlist must not contain %q", key)
		}
		if strings.Contains(strings.ToUpper(key), "TOKEN") ||
			strings.Contains(strings.ToUpper(key), "SECRET") ||
			strings.Contains(strings.ToUpper(key), "PASSWORD") ||
			strings.Contains(strings.ToUpper(key), "KEY") {
			t.Errorf("envAllowlist contains suspicious key: %q", key)
		}
	}
}
