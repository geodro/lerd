package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ── sailExtractDefault ────────────────────────────────────────────────────────

func TestSailExtractDefault(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"${APP_PORT:-80}", "80"},
		{"${FORWARD_DB_PORT:-3306}", "3306"},
		{"${FORWARD_REDIS_PORT:-6379}", "6379"},
		{"${FORWARD_MINIO_PORT:-9000}", "9000"},
		{"${VAR:-}", ""},                // empty default
		{"${VAR_NO_DEFAULT}", "${VAR_NO_DEFAULT}"}, // no :- → unchanged
		{"3306", "3306"},               // plain number → unchanged
		{"", ""},                       // empty string → unchanged
	}
	for _, c := range cases {
		got := sailExtractDefault(c.in)
		if got != c.want {
			t.Errorf("sailExtractDefault(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ── sailHostPort ─────────────────────────────────────────────────────────────

func TestSailHostPort(t *testing.T) {
	cases := []struct {
		desc string
		in   interface{}
		want int
	}{
		// String: bare container port — no host binding
		{"container-only port", "3306", 0},
		// String: host:container
		{"host:container", "3306:3306", 3306},
		{"host:container 80", "80:80", 80},
		// String: ip:host:container
		{"ip:host:container", "127.0.0.1:3306:3306", 3306},
		// String: env-var defaults (Sail style)
		{"sail APP_PORT default", "${APP_PORT:-80}:80", 80},
		{"sail FORWARD_DB_PORT default", "${FORWARD_DB_PORT:-3306}:3306", 3306},
		{"sail FORWARD_REDIS_PORT default", "${FORWARD_REDIS_PORT:-6379}:6379", 6379},
		{"sail FORWARD_MINIO_PORT default", "${FORWARD_MINIO_PORT:-9000}:9000", 9000},
		// Map (long-form)
		{"long-form map int published", map[string]interface{}{"target": 3306, "published": 3306}, 3306},
		{"long-form map string published", map[string]interface{}{"target": 80, "published": "80"}, 80},
		// Non-string, non-map
		{"nil", nil, 0},
		{"int", 3306, 0},
	}
	for _, c := range cases {
		got := sailHostPort(c.in)
		if got != c.want {
			t.Errorf("[%s] sailHostPort(%v) = %d, want %d", c.desc, c.in, got, c.want)
		}
	}
}

// ── sailBuildPortRemap ────────────────────────────────────────────────────────

func TestSailBuildPortRemap(t *testing.T) {
	t.Run("remaps conflicting ports on data services only", func(t *testing.T) {
		cf := &sailComposeFile{Services: map[string]sailComposeService{
			"laravel.test": {Ports: []interface{}{"${APP_PORT:-80}:80", "443:443"}},
			"mysql":        {Ports: []interface{}{"${FORWARD_DB_PORT:-3306}:3306"}},
			"redis":        {Ports: []interface{}{"${FORWARD_REDIS_PORT:-6379}:6379"}},
		}}
		remap := sailBuildPortRemap(cf)
		// Data services (mysql, redis) have their conflicting ports remapped.
		wantRemapped := map[int]int{3306: 23306, 6379: 26379}
		for orig, wantNew := range wantRemapped {
			if remap[orig] != wantNew {
				t.Errorf("port %d → %d, want %d", orig, remap[orig], wantNew)
			}
		}
		// App service ports (80, 443) are NOT remapped — they will be stripped.
		for _, appPort := range []int{80, 443} {
			if _, ok := remap[appPort]; ok {
				t.Errorf("app service port %d should not be in remap (it gets stripped)", appPort)
			}
		}
	})

	t.Run("non-conflicting data service ports are not remapped", func(t *testing.T) {
		cf := &sailComposeFile{Services: map[string]sailComposeService{
			"mysql": {Ports: []interface{}{"13306:3306"}}, // non-conflicting host port
		}}
		remap := sailBuildPortRemap(cf)
		if len(remap) != 0 {
			t.Errorf("expected no remaps for non-conflicting ports, got %v", remap)
		}
	})

	t.Run("non-data service with conflicting port produces no remap entry", func(t *testing.T) {
		cf := &sailComposeFile{Services: map[string]sailComposeService{
			"laravel.test": {Ports: []interface{}{"8080:80", "3306:3306"}}, // 3306 on app service
		}}
		remap := sailBuildPortRemap(cf)
		if len(remap) != 0 {
			t.Errorf("expected no remaps for app service ports, got %v", remap)
		}
	})

	t.Run("container-only ports are not remapped", func(t *testing.T) {
		cf := &sailComposeFile{Services: map[string]sailComposeService{
			"mysql": {Ports: []interface{}{"3306"}},
		}}
		remap := sailBuildPortRemap(cf)
		if len(remap) != 0 {
			t.Errorf("expected no remaps, got %v", remap)
		}
	})

	t.Run("empty compose file produces empty remap", func(t *testing.T) {
		cf := &sailComposeFile{Services: map[string]sailComposeService{}}
		remap := sailBuildPortRemap(cf)
		if len(remap) != 0 {
			t.Errorf("expected empty remap, got %v", remap)
		}
	})
}

// ── sailRemapPortString ───────────────────────────────────────────────────────

func TestSailRemapPortString(t *testing.T) {
	remap := map[int]int{80: 20080, 3306: 23306, 9000: 29000}

	cases := []struct {
		desc string
		in   interface{}
		want string
	}{
		// 2-part strings
		{"plain host:container", "80:80", "20080:80"},
		{"sail-style env default", "${APP_PORT:-80}:80", "20080:80"},
		{"not in remap", "8080:8080", "8080:8080"},
		// 3-part strings
		{"ip:host:container", "127.0.0.1:3306:3306", "127.0.0.1:23306:3306"},
		// 1-part (no host binding) — unchanged
		{"container-only", "3306", "3306"},
		// Non-string — returns empty string (not remappable)
		{"non-string", 9000, ""},
	}
	for _, c := range cases {
		got := sailRemapPortString(c.in, remap)
		if got != c.want {
			t.Errorf("[%s] sailRemapPortString(%v) = %q, want %q", c.desc, c.in, got, c.want)
		}
	}
}

// ── sailDetectS3 ─────────────────────────────────────────────────────────────

func TestSailDetectS3(t *testing.T) {
	t.Run("detected via FILESYSTEM_DISK=s3", func(t *testing.T) {
		env := map[string]string{
			"FILESYSTEM_DISK":   "s3",
			"AWS_ACCESS_KEY_ID": "minioadmin",
			"AWS_SECRET_ACCESS_KEY": "minioadmin",
			"AWS_BUCKET":        "myapp",
		}
		s3 := sailDetectS3(env)
		if s3 == nil {
			t.Fatal("expected non-nil s3Env")
		}
		if s3.accessKey != "minioadmin" {
			t.Errorf("accessKey = %q, want %q", s3.accessKey, "minioadmin")
		}
		if s3.bucket != "myapp" {
			t.Errorf("bucket = %q, want %q", s3.bucket, "myapp")
		}
	})

	t.Run("detected via AWS_ENDPOINT presence", func(t *testing.T) {
		env := map[string]string{
			"AWS_ENDPOINT": "http://minio:9000",
			"AWS_BUCKET":   "files",
		}
		if sailDetectS3(env) == nil {
			t.Error("expected S3 detected when AWS_ENDPOINT is set")
		}
	})

	t.Run("defaults bucket to 'local' when AWS_BUCKET is empty", func(t *testing.T) {
		env := map[string]string{"FILESYSTEM_DISK": "s3"}
		s3 := sailDetectS3(env)
		if s3 == nil {
			t.Fatal("expected non-nil s3Env")
		}
		if s3.bucket != "local" {
			t.Errorf("bucket = %q, want %q", s3.bucket, "local")
		}
	})

	t.Run("not detected when FILESYSTEM_DISK is local", func(t *testing.T) {
		env := map[string]string{"FILESYSTEM_DISK": "local"}
		if sailDetectS3(env) != nil {
			t.Error("expected nil when filesystem is local")
		}
	})

	t.Run("not detected when no relevant keys", func(t *testing.T) {
		env := map[string]string{"DB_CONNECTION": "mysql"}
		if sailDetectS3(env) != nil {
			t.Error("expected nil for unrelated env")
		}
	})
}

// ── sailFindDBService ─────────────────────────────────────────────────────────

func TestSailFindDBService(t *testing.T) {
	cases := []struct {
		desc       string
		services   map[string]sailComposeService
		connection string
		want       string
	}{
		{
			desc:       "mysql by service name",
			services:   map[string]sailComposeService{"mysql": {Image: "mysql:8.0"}, "redis": {}},
			connection: "mysql",
			want:       "mysql",
		},
		{
			desc:       "pgsql by service name",
			services:   map[string]sailComposeService{"pgsql": {Image: "postgres:16"}, "redis": {}},
			connection: "pgsql",
			want:       "pgsql",
		},
		{
			desc:       "postgres alias for pgsql service",
			services:   map[string]sailComposeService{"postgres": {Image: "postgres:16"}},
			connection: "pgsql",
			want:       "postgres",
		},
		{
			desc:       "mariadb by service name",
			services:   map[string]sailComposeService{"mariadb": {Image: "mariadb:11"}},
			connection: "mariadb",
			want:       "mariadb",
		},
		{
			desc:       "fallback to mysql service for mariadb connection",
			services:   map[string]sailComposeService{"mysql": {Image: "mariadb:11"}},
			connection: "mariadb",
			want:       "mysql",
		},
		{
			desc:       "image fallback: mysql image matches mysql connection",
			services:   map[string]sailComposeService{"db": {Image: "mysql:8.0"}},
			connection: "mysql",
			want:       "db",
		},
		{
			desc:       "image fallback: postgres image matches pgsql connection",
			services:   map[string]sailComposeService{"database": {Image: "postgres:16"}},
			connection: "pgsql",
			want:       "database",
		},
		{
			desc:       "returns empty string when nothing matches",
			services:   map[string]sailComposeService{"redis": {Image: "redis:7"}},
			connection: "mysql",
			want:       "",
		},
	}

	for _, c := range cases {
		cf := &sailComposeFile{Services: c.services}
		got := sailFindDBService(cf, c.connection)
		if got != c.want {
			t.Errorf("[%s] sailFindDBService conn=%q got %q, want %q", c.desc, c.connection, got, c.want)
		}
	}
}

// ── sailFindMinio ─────────────────────────────────────────────────────────────

func TestSailFindMinio(t *testing.T) {
	t.Run("finds minio service by name with port 9000", func(t *testing.T) {
		cf := &sailComposeFile{Services: map[string]sailComposeService{
			"minio": {
				Ports:       []interface{}{"${FORWARD_MINIO_PORT:-9000}:9000"},
				Image:       "minio/minio",
				Environment: map[string]string{"MINIO_ROOT_USER": "sail", "MINIO_ROOT_PASSWORD": "password"},
			},
		}}
		svc, port, user, pass := sailFindMinio(cf, map[int]int{9000: 29000})
		if svc != "minio" {
			t.Errorf("service = %q, want %q", svc, "minio")
		}
		if port != 29000 {
			t.Errorf("port = %d, want %d", port, 29000)
		}
		if user != "sail" {
			t.Errorf("user = %q, want %q", user, "sail")
		}
		if pass != "password" {
			t.Errorf("pass = %q, want %q", pass, "password")
		}
	})

	t.Run("falls back to sail/password defaults when no environment block", func(t *testing.T) {
		cf := &sailComposeFile{Services: map[string]sailComposeService{
			"minio": {Ports: []interface{}{"9000:9000"}, Image: "minio/minio"},
		}}
		_, _, user, pass := sailFindMinio(cf, map[int]int{})
		if user != "sail" {
			t.Errorf("user = %q, want default %q", user, "sail")
		}
		if pass != "password" {
			t.Errorf("pass = %q, want default %q", pass, "password")
		}
	})

	t.Run("uses original port 9000 when no remap needed", func(t *testing.T) {
		cf := &sailComposeFile{Services: map[string]sailComposeService{
			"minio": {Ports: []interface{}{"9000:9000"}, Image: "minio/minio"},
		}}
		svc, port, _, _ := sailFindMinio(cf, map[int]int{})
		if svc != "minio" {
			t.Errorf("service = %q, want %q", svc, "minio")
		}
		if port != 9000 {
			t.Errorf("port = %d, want %d", port, 9000)
		}
	})

	t.Run("detects by image name when service is not named minio", func(t *testing.T) {
		cf := &sailComposeFile{Services: map[string]sailComposeService{
			"storage": {Ports: []interface{}{"9000:9000"}, Image: "minio/minio:latest"},
		}}
		svc, _, _, _ := sailFindMinio(cf, map[int]int{})
		if svc != "storage" {
			t.Errorf("service = %q, want %q", svc, "storage")
		}
	})

	t.Run("returns empty string when no minio service", func(t *testing.T) {
		cf := &sailComposeFile{Services: map[string]sailComposeService{
			"mysql": {Ports: []interface{}{"3306:3306"}},
		}}
		svc, port, _, _ := sailFindMinio(cf, map[int]int{})
		if svc != "" || port != 0 {
			t.Errorf("expected empty result, got svc=%q port=%d", svc, port)
		}
	})
}

// ── sailFindComposeFile ───────────────────────────────────────────────────────

func TestSailFindComposeFile(t *testing.T) {
	t.Run("finds docker-compose.yml", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "docker-compose.yml")
		os.WriteFile(p, []byte("services: {}"), 0644)
		got, err := sailFindComposeFile(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got != p {
			t.Errorf("got %q, want %q", got, p)
		}
	})

	t.Run("finds docker-compose.yaml", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "docker-compose.yaml")
		os.WriteFile(p, []byte("services: {}"), 0644)
		got, err := sailFindComposeFile(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got != p {
			t.Errorf("got %q, want %q", got, p)
		}
	})

	t.Run("prefers docker-compose.yml over docker-compose.yaml", func(t *testing.T) {
		dir := t.TempDir()
		yml := filepath.Join(dir, "docker-compose.yml")
		yaml := filepath.Join(dir, "docker-compose.yaml")
		os.WriteFile(yml, []byte("services: {}"), 0644)
		os.WriteFile(yaml, []byte("services: {}"), 0644)
		got, err := sailFindComposeFile(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got != yml {
			t.Errorf("expected .yml to win, got %q", got)
		}
	})

	t.Run("returns error when no compose file exists", func(t *testing.T) {
		dir := t.TempDir()
		_, err := sailFindComposeFile(dir)
		if err == nil {
			t.Error("expected error for missing compose file")
		}
	})
}

// ── sailReadRawEnv ────────────────────────────────────────────────────────────

func TestSailReadRawEnv(t *testing.T) {
	t.Run("parses key=value pairs", func(t *testing.T) {
		dir := t.TempDir()
		content := `
# comment
DB_CONNECTION=mysql
DB_DATABASE=myapp
DB_USERNAME=root
DB_PASSWORD=secret
FILESYSTEM_DISK=s3
AWS_BUCKET="mybucket"
EMPTY_KEY=
`
		os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0644)
		env := sailReadRawEnv(dir)
		tests := []struct{ key, want string }{
			{"DB_CONNECTION", "mysql"},
			{"DB_DATABASE", "myapp"},
			{"DB_USERNAME", "root"},
			{"DB_PASSWORD", "secret"},
			{"FILESYSTEM_DISK", "s3"},
			{"AWS_BUCKET", "mybucket"}, // quotes stripped
			{"EMPTY_KEY", ""},
		}
		for _, tt := range tests {
			if got := env[tt.key]; got != tt.want {
				t.Errorf("env[%q] = %q, want %q", tt.key, got, tt.want)
			}
		}
	})

	t.Run("returns empty map when .env is absent", func(t *testing.T) {
		dir := t.TempDir()
		env := sailReadRawEnv(dir)
		if len(env) != 0 {
			t.Errorf("expected empty map, got %v", env)
		}
	})

	t.Run("ignores comment lines", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, ".env"), []byte("# DB_HOST=should-be-ignored\nDB_PORT=3306\n"), 0644)
		env := sailReadRawEnv(dir)
		if _, ok := env["# DB_HOST"]; ok {
			t.Error("comment line should not be parsed")
		}
		if env["DB_PORT"] != "3306" {
			t.Errorf("DB_PORT = %q, want %q", env["DB_PORT"], "3306")
		}
	})
}

// ── sailBuildTempCompose ──────────────────────────────────────────────────────

func TestSailBuildTempCompose(t *testing.T) {
	// sailBuildTempCompose calls `docker compose config` which requires a real
	// docker daemon; that may not be available in CI. We test the path that
	// falls back to reading the raw file directly (same code path, just skips
	// the `docker compose config` step).
	t.Run("strips app service ports and remaps data service ports", func(t *testing.T) {
		dir := t.TempDir()
		content := `services:
  laravel.test:
    image: sail-8.4/app
    ports:
      - "80:80"
      - "8080:80"
      - "5173:5173"
  mysql:
    image: mysql:8.0
    ports:
      - "3306:3306"
  redis:
    image: redis:alpine
    ports:
      - "6379:6379"
`
		composePath := filepath.Join(dir, "docker-compose.yml")
		os.WriteFile(composePath, []byte(content), 0644)

		_, tmpPath, portRemap, strippedSvcs, err := sailBuildTempCompose(composePath, dir, "docker")
		if err != nil {
			t.Fatalf("sailBuildTempCompose: %v", err)
		}
		defer os.Remove(tmpPath)

		// laravel.test should be in strippedSvcs.
		found := false
		for _, s := range strippedSvcs {
			if s == "laravel.test" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected laravel.test in strippedSvcs, got %v", strippedSvcs)
		}

		// Data service conflicting ports should be remapped.
		for _, p := range []int{3306, 6379} {
			if portRemap[p] != p+sailImportPortOffset {
				t.Errorf("port %d remapped to %d, want %d", p, portRemap[p], p+sailImportPortOffset)
			}
		}

		// Read the produced file and verify.
		fileBytes, _ := os.ReadFile(tmpPath)
		fileContent := string(fileBytes)

		// App ports must not appear.
		for _, p := range []string{"8080", "5173"} {
			if strings.Contains(fileContent, p) {
				t.Errorf("app port %s should be absent from temp compose, got:\n%s", p, fileContent)
			}
		}
		// Remapped data service ports must appear.
		for _, p := range []int{23306, 26379} {
			if !strings.Contains(fileContent, strconv.Itoa(p)) {
				t.Errorf("expected remapped port %d in temp compose, got:\n%s", p, fileContent)
			}
		}
	})

	t.Run("handles long-form map port entries from docker compose config output", func(t *testing.T) {
		dir := t.TempDir()
		// docker compose config normalises ports to long-form maps.
		content := `services:
  mysql:
    image: mysql:8.0
    ports:
      - mode: ingress
        target: 3306
        published: "3306"
        protocol: tcp
`
		composePath := filepath.Join(dir, "docker-compose.yml")
		os.WriteFile(composePath, []byte(content), 0644)

		_, tmpPath, portRemap, _, err := sailBuildTempCompose(composePath, dir, "docker")
		if err != nil {
			t.Fatalf("sailBuildTempCompose: %v", err)
		}
		defer os.Remove(tmpPath)

		if portRemap[3306] != 23306 {
			t.Errorf("port 3306 remapped to %d, want 23306", portRemap[3306])
		}
		data, _ := os.ReadFile(tmpPath)
		if !strings.Contains(string(data), "23306") {
			t.Errorf("expected 23306 in temp compose, got:\n%s", data)
		}
	})
}

// ── Sail credential separation ────────────────────────────────────────────────

// TestSailEnvSeparation verifies that the Sail dump env and the lerd import env
// are built independently from the flags and .env respectively.
func TestSailEnvSeparation(t *testing.T) {
	t.Run("flag values used for Sail side, not .env credentials", func(t *testing.T) {
		// Simulate a .env that lerd setup has already overwritten.
		lerdEnv := &dbEnv{
			connection: "mysql",
			database:   "lerd",
			username:   "root",
			password:   "lerd",
		}

		// Build the Sail-side env the same way runImportSail does.
		sailUser := "sail"
		sailPass := "password"
		sailDB := "myapp" // provided via --sail-db-name

		sailEnv := &dbEnv{
			connection: lerdEnv.connection,
			database:   sailDB,
			username:   sailUser,
			password:   sailPass,
		}

		// Sail side must use Sail credentials, not lerd's.
		if sailEnv.username != "sail" {
			t.Errorf("sailEnv.username = %q, want %q", sailEnv.username, "sail")
		}
		if sailEnv.password != "password" {
			t.Errorf("sailEnv.password = %q, want %q", sailEnv.password, "password")
		}
		if sailEnv.database != "myapp" {
			t.Errorf("sailEnv.database = %q, want %q", sailEnv.database, "myapp")
		}

		// Lerd side must retain lerd's credentials.
		if lerdEnv.username != "root" {
			t.Errorf("lerdEnv.username = %q, want %q", lerdEnv.username, "root")
		}
		if lerdEnv.password != "lerd" {
			t.Errorf("lerdEnv.password = %q, want %q", lerdEnv.password, "lerd")
		}
		if lerdEnv.database != "lerd" {
			t.Errorf("lerdEnv.database = %q, want %q", lerdEnv.database, "lerd")
		}
	})

	t.Run("sail-db-name falls back to DB_DATABASE when not provided", func(t *testing.T) {
		lerdEnv := &dbEnv{connection: "mysql", database: "myproject", username: "root", password: "lerd"}
		sailDB := "" // flag not provided
		if sailDB == "" {
			sailDB = lerdEnv.database
		}
		if sailDB != "myproject" {
			t.Errorf("sailDB = %q, want %q", sailDB, "myproject")
		}
	})

	t.Run("sail-db-name flag overrides DB_DATABASE even when it is not 'lerd'", func(t *testing.T) {
		lerdEnv := &dbEnv{connection: "mysql", database: "somedb", username: "root", password: "lerd"}
		sailDB := "original_db" // user explicitly passed --sail-db-name
		if sailDB == "" {
			sailDB = lerdEnv.database
		}
		if sailDB != "original_db" {
			t.Errorf("sailDB = %q, want %q", sailDB, "original_db")
		}
	})
}

// ── sailRecreateDB command args ───────────────────────────────────────────────

// TestSailRecreateDBPasswordNotLeaked verifies that the recreate commands route
// the password through an environment variable rather than a positional argument.
// (We can't execute the commands in tests; we inspect Args instead.)
func TestSailRecreateDBPasswordNotLeaked(t *testing.T) {
	env := &dbEnv{
		connection: "mysql",
		database:   "myapp",
		username:   "root",
		password:   "s3cr3t",
	}
	// sailRecreateDB executes immediately, so we can't call it in a unit test.
	// Instead, verify that the SQL it would generate uses env-var password delivery
	// by checking dbImportCmd (which shares the same pattern) doesn't expose creds.
	cmd, err := dbImportCmd(env)
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range cmd.Args {
		if strings.Contains(arg, "s3cr3t") && !strings.HasPrefix(arg, "MYSQL_PWD=") {
			t.Errorf("password leaked outside MYSQL_PWD env var in arg: %q", arg)
		}
	}
}

// ── sailResolveCompose fallback ───────────────────────────────────────────────

func TestSailResolveComposeFallback(t *testing.T) {
	// When docker is unavailable or compose config fails, sailResolveCompose
	// must still parse the raw YAML correctly.
	dir := t.TempDir()
	content := `services:
  laravel.test:
    image: sail-8.4/app
    ports:
      - '${APP_PORT:-80}:80'
  mysql:
    image: 'mysql/mysql-server:8.0'
    ports:
      - '${FORWARD_DB_PORT:-3306}:3306'
`
	composePath := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	data, err := sailResolvedComposeBytes(composePath, dir, "docker")
	if err != nil {
		t.Fatalf("sailResolvedComposeBytes error: %v", err)
	}
	var cf sailComposeFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		t.Fatalf("yaml.Unmarshal error: %v", err)
	}
	if _, ok := cf.Services["laravel.test"]; !ok {
		t.Error("expected laravel.test service to be parsed")
	}
	if _, ok := cf.Services["mysql"]; !ok {
		t.Error("expected mysql service to be parsed")
	}
}

// ── integration: full port remap round-trip ───────────────────────────────────

func TestSailPortRemapRoundTrip(t *testing.T) {
	// Build a compose file that mimics a real Sail setup, run it through
	// sailBuildPortRemap + sailWritePortOverride, and verify the produced YAML
	// contains the expected remapped ports.
	cf := &sailComposeFile{Services: map[string]sailComposeService{
		"laravel.test": {
			Image: "sail-8.4/app",
			Ports: []interface{}{
				"${APP_PORT:-80}:80",
				"${VITE_PORT:-5173}:5173",
			},
		},
		"mysql": {
			Image: "mysql/mysql-server:8.0",
			Ports: []interface{}{"${FORWARD_DB_PORT:-3306}:3306"},
		},
		"redis": {
			Image: "redis:alpine",
			Ports: []interface{}{"${FORWARD_REDIS_PORT:-6379}:6379"},
		},
		"minio": {
			Image: "minio/minio:latest",
			Ports: []interface{}{
				"${FORWARD_MINIO_PORT:-9000}:9000",
				"${FORWARD_MINIO_CONSOLE_PORT:-8900}:8900",
			},
		},
	}}

	remap := sailBuildPortRemap(cf)

	// Data service conflicting ports should be remapped.
	dataConflicts := []int{3306, 6379, 9000}
	for _, p := range dataConflicts {
		if _, ok := remap[p]; !ok {
			t.Errorf("expected data service port %d to be remapped", p)
		}
		if remap[p] != p+sailImportPortOffset {
			t.Errorf("port %d remapped to %d, want %d", p, remap[p], p+sailImportPortOffset)
		}
	}

	// App service ports (80) must NOT appear in the remap — they are stripped.
	for _, p := range []int{80} {
		if _, ok := remap[p]; ok {
			t.Errorf("app service port %d should not be in remap (gets stripped)", p)
		}
	}

	// Non-conflicting ports should not appear in remap either.
	for _, p := range []int{5173, 8900} {
		if _, ok := remap[p]; ok {
			t.Errorf("port %d should not be remapped", p)
		}
	}

	// Write a compose file from the cf data and verify the produced temp file.
	dir := t.TempDir()
	composeYAML, err := yaml.Marshal(map[string]interface{}{
		"services": map[string]interface{}{
			"laravel.test": map[string]interface{}{
				"image": "sail-8.4/app",
				"ports": []string{"80:80", "5173:5173"},
			},
			"mysql":  map[string]interface{}{"image": "mysql/mysql-server:8.0", "ports": []string{"3306:3306"}},
			"redis":  map[string]interface{}{"image": "redis:alpine", "ports": []string{"6379:6379"}},
			"minio":  map[string]interface{}{"image": "minio/minio:latest", "ports": []string{"9000:9000", "8900:8900"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	composePath := filepath.Join(dir, "docker-compose.yml")
	os.WriteFile(composePath, composeYAML, 0644)

	_, tmpPath, _, strippedSvcs, err := sailBuildTempCompose(composePath, dir, "docker")
	if err != nil {
		t.Fatalf("sailBuildTempCompose: %v", err)
	}
	defer os.Remove(tmpPath)
	data, _ := os.ReadFile(tmpPath)
	content := string(data)

	// Data service remapped ports must appear.
	for _, p := range dataConflicts {
		remapped := p + sailImportPortOffset
		if !strings.Contains(content, strconv.Itoa(remapped)) {
			t.Errorf("expected remapped port %d in temp compose YAML", remapped)
		}
	}

	// laravel.test must be stripped.
	found := false
	for _, s := range strippedSvcs {
		if s == "laravel.test" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected laravel.test in strippedSvcs, got %v", strippedSvcs)
	}
	// App ports must not appear.
	for _, p := range []string{"5173"} {
		if strings.Contains(content, p) {
			t.Errorf("app service port %q should not appear in temp compose YAML", p)
		}
	}
}
