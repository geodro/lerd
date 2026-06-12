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
		{"admin_starlane", "admin-starlane"},
		{"Admin_Starlane", "admin-starlane"},
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
	ctx := siteTemplateCtx{site: "admin_starlane", bucket: "admin-starlane"}
	got := applySiteHandle("AWS_BUCKET={{bucket}}", ctx)
	if got != "AWS_BUCKET=admin-starlane" {
		t.Errorf("expected sanitised bucket, got %q", got)
	}
	gotSite := applySiteHandle("DB_DATABASE={{site}}", ctx)
	if gotSite != "DB_DATABASE=admin_starlane" {
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

func TestUserPickedDBFromYAML_CustomFamilyMember(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	if err := os.MkdirAll(filepath.Join(tmp, "lerd", "services"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yaml := `name: postgres-pgvector
family: postgres
image: docker.io/pgvector/pgvector:pg18
`
	if err := os.WriteFile(filepath.Join(tmp, "lerd", "services", "postgres-pgvector.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write fake service: %v", err)
	}
	if !userPickedDBFromYAML(map[string]bool{"postgres-pgvector": true}) {
		t.Errorf("userPickedDBFromYAML should count postgres-pgvector as a picked DB via Family=postgres")
	}
}

func TestBuildDatabaseOptions_IncludesInstalledFamilyAlternates(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	if err := os.MkdirAll(filepath.Join(tmp, "lerd", "services"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yaml := `name: postgres-pgvector
family: postgres
image: docker.io/pgvector/pgvector:pg18
`
	if err := os.WriteFile(filepath.Join(tmp, "lerd", "services", "postgres-pgvector.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write fake service: %v", err)
	}
	options, nameSet := buildDatabaseOptions()
	if !nameSet["sqlite"] || !nameSet["mysql"] || !nameSet["postgres"] {
		t.Errorf("buildDatabaseOptions must always include the built-in trio, got %v", nameSet)
	}
	if !nameSet["postgres-pgvector"] {
		t.Errorf("buildDatabaseOptions must surface installed postgres-family alternate, got %v", nameSet)
	}
	foundPgvector := false
	for _, opt := range options {
		if opt.Value == "postgres-pgvector" {
			foundPgvector = true
			break
		}
	}
	if !foundPgvector {
		t.Errorf("buildDatabaseOptions must offer postgres-pgvector as a selectable option")
	}
}

func TestShouldApplyService(t *testing.T) {
	for _, tc := range []struct {
		name         string
		svc          string
		detected     bool
		picked       bool
		userPickedDB bool
		valkeyPicked bool
		want         bool
	}{
		// Regression: fresh Laravel project, user picks mysql in `lerd init`.
		// Existing .env still says DB_CONNECTION=sqlite, so detection misses.
		// The .lerd.yaml pick must still cause mysql vars to be applied.
		{"mysql picked, not detected", "mysql", false, true, true, false, true},

		// Detection-driven application keeps working when the user did not
		// pre-pick a DB (e.g. an imported Sail project where .env already
		// references mysql).
		{"mysql detected, no yaml", "mysql", true, false, false, false, true},

		// User picked postgres but .env mentions mysql — don't reapply mysql
		// on top of postgres, otherwise switching DBs via the wizard silently
		// keeps the old credentials.
		{"mysql detected, postgres picked", "mysql", true, false, true, false, false},

		// Non-DB services aren't affected by the userPickedDB guard.
		{"redis detected", "redis", true, false, true, false, true},
		{"redis picked", "redis", false, true, false, false, true},
		{"redis neither", "redis", false, false, false, false, false},

		// Postgres mirror of the mysql cases.
		{"postgres picked, not detected", "postgres", false, true, true, false, true},
		{"postgres detected, mysql picked", "postgres", true, false, true, false, false},

		// Valkey is a redis replacement: a redis-shaped .env must not reapply
		// the built-in redis when the project picked valkey.
		{"redis detected, valkey picked", "redis", true, false, false, true, false},

		// ...unless redis itself is also explicitly picked alongside valkey.
		{"redis picked, valkey picked", "redis", false, true, false, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldApplyService(tc.svc, tc.detected, tc.picked, tc.userPickedDB, tc.valkeyPicked)
			if got != tc.want {
				t.Errorf("shouldApplyService(%q, det=%v, picked=%v, userPickedDB=%v, valkeyPicked=%v) = %v, want %v",
					tc.svc, tc.detected, tc.picked, tc.userPickedDB, tc.valkeyPicked, got, tc.want)
			}
		})
	}
}

// TestConsoleExecArgs guards that key generation runs the framework's own
// console binary, not a hardcoded "artisan". CodeIgniter (spark) was the first
// store framework to define key_generation on a non-artisan console, which is
// what surfaced the original bug.
func TestConsoleExecArgs(t *testing.T) {
	for _, tc := range []struct {
		name    string
		console string
		want    string // the console binary expected in the exec args
	}{
		{"codeigniter uses spark", "spark", "spark"},
		{"laravel uses artisan", "artisan", "artisan"},
		{"empty console falls back to artisan", "", "artisan"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := consoleExecArgs("/proj", "8.4", tc.console, "key:generate")
			want := []string{"exec", "-i", "-w", "/proj", "lerd-php84-fpm", "php", tc.want, "key:generate"}
			if strings.Join(got, " ") != strings.Join(want, " ") {
				t.Errorf("consoleExecArgs(console=%q) = %v, want %v", tc.console, got, want)
			}
		})
	}
}

// ── host-proxy env rewriting ──────────────────────────────────────────────────

func TestSplitHostContainerPort(t *testing.T) {
	cases := []struct {
		in         string
		host, cont string
		ok         bool
	}{
		{"3411:3306", "3411", "3306", true},
		{"6379:6379", "6379", "6379", true},
		{"127.0.0.1:3411:3306", "3411", "3306", true},
		{"3411:3306/tcp", "3411", "3306", true},
		{"3306", "", "", false},
		{"", "", "", false},
	}
	for _, c := range cases {
		host, cont, ok := splitHostContainerPort(c.in)
		if host != c.host || cont != c.cont || ok != c.ok {
			t.Errorf("splitHostContainerPort(%q) = (%q,%q,%v), want (%q,%q,%v)", c.in, host, cont, ok, c.host, c.cont, c.ok)
		}
	}
}

func TestApplyHostProxyEnv_rewritesHostAndPort(t *testing.T) {
	// mariadb: container 3306 publishes on host 3411.
	updates := map[string]string{
		"DB_HOST":     "lerd-mariadb-11",
		"DB_PORT":     "3306",
		"DB_DATABASE": "flowmeter",
		"REDIS_HOST":  "lerd-redis",
		"REDIS_PORT":  "6379",
		"APP_URL":     "https://flowmeter.test",
		"APP_NAME":    "ecom",
	}
	applyHostProxyEnv(updates, map[string]string{"3306": "3411", "6379": "6379"})

	if updates["DB_HOST"] != "127.0.0.1" {
		t.Errorf("DB_HOST = %q, want 127.0.0.1", updates["DB_HOST"])
	}
	if updates["REDIS_HOST"] != "127.0.0.1" {
		t.Errorf("REDIS_HOST = %q, want 127.0.0.1", updates["REDIS_HOST"])
	}
	if updates["DB_PORT"] != "3411" {
		t.Errorf("DB_PORT = %q, want 3411 (published host port)", updates["DB_PORT"])
	}
	if updates["REDIS_PORT"] != "6379" {
		t.Errorf("REDIS_PORT = %q, want 6379 (unchanged when host==container)", updates["REDIS_PORT"])
	}
	// Non-service values must be left alone.
	if updates["DB_DATABASE"] != "flowmeter" {
		t.Errorf("DB_DATABASE was mangled: %q", updates["DB_DATABASE"])
	}
	if updates["APP_URL"] != "https://flowmeter.test" {
		t.Errorf("APP_URL was mangled: %q", updates["APP_URL"])
	}
	if updates["APP_NAME"] != "ecom" {
		t.Errorf("APP_NAME was mangled: %q", updates["APP_NAME"])
	}
}

func TestApplyHostProxyEnv_rewritesEmbeddedUrlHosts(t *testing.T) {
	// Services configured via a URL (mongo, elasticsearch, …) carry the container
	// hostname inside the value; the host must be rewritten to loopback and the
	// embedded port remapped, while credentials and path survive.
	updates := map[string]string{
		"MONGO_DSN":         "mongodb://root:lerd@lerd-mongo:27017/site?authSource=admin",
		"ELASTICSEARCH_URL": "http://lerd-elasticsearch:9200",
		"DB_PORT":           "3306",
	}
	applyHostProxyEnv(updates, map[string]string{"27017": "27017", "9200": "9200", "3306": "3411"})

	if got := updates["MONGO_DSN"]; got != "mongodb://root:lerd@127.0.0.1:27017/site?authSource=admin" {
		t.Errorf("MONGO_DSN = %q", got)
	}
	if got := updates["ELASTICSEARCH_URL"]; got != "http://127.0.0.1:9200" {
		t.Errorf("ELASTICSEARCH_URL = %q", got)
	}
	// A standalone *_PORT with no host token is still remapped.
	if got := updates["DB_PORT"]; got != "3411" {
		t.Errorf("DB_PORT = %q, want 3411", got)
	}
}

func TestApplyHostProxyEnv_leavesNonConnectionKeysAlone(t *testing.T) {
	// A value carrying a "lerd-" token in a key that is NOT a connection target
	// must survive untouched — only host/port/url/dsn/endpoint keys get rewritten.
	updates := map[string]string{
		"APP_NAME":     "lerd-demo",                    // not a conn key: keep
		"CACHE_PREFIX": "lerd-cache",                   // not a conn key: keep
		"DB_HOST":      "lerd-mariadb",                 // conn key: rewrite to loopback
		"MONGO_DSN":    "mongodb://lerd-mongo:27017/x", // conn key: rewrite host
	}
	applyHostProxyEnv(updates, map[string]string{"27017": "27017"})

	if updates["APP_NAME"] != "lerd-demo" {
		t.Errorf("APP_NAME mangled: %q", updates["APP_NAME"])
	}
	if updates["CACHE_PREFIX"] != "lerd-cache" {
		t.Errorf("CACHE_PREFIX mangled: %q", updates["CACHE_PREFIX"])
	}
	if updates["DB_HOST"] != "127.0.0.1" {
		t.Errorf("DB_HOST = %q, want 127.0.0.1", updates["DB_HOST"])
	}
	if updates["MONGO_DSN"] != "mongodb://127.0.0.1:27017/x" {
		t.Errorf("MONGO_DSN = %q", updates["MONGO_DSN"])
	}
}

func TestRewriteEnvForHostProxy_usesPresetPorts(t *testing.T) {
	// postgres + redis are default presets resolvable from the embedded YAML,
	// so the full path (preset lookup -> port map -> rewrite) works offline.
	updates := map[string]string{
		"DB_HOST":    "lerd-postgres",
		"DB_PORT":    "5432",
		"REDIS_HOST": "lerd-redis",
		"REDIS_PORT": "6379",
	}
	rewriteEnvForHostProxy(updates, []string{"postgres", "redis"})
	if updates["DB_HOST"] != "127.0.0.1" || updates["REDIS_HOST"] != "127.0.0.1" {
		t.Errorf("hosts not rewritten to loopback: %+v", updates)
	}
	if updates["DB_PORT"] != "5432" || updates["REDIS_PORT"] != "6379" {
		t.Errorf("ports changed unexpectedly: %+v", updates)
	}
}
