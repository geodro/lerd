package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/podman"
)

// writeTempEnv creates a temp dir with a .env file containing the given
// key/value pairs and returns its path.
func writeTempEnv(t *testing.T, kv map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	var b strings.Builder
	for k, v := range kv {
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(v)
		b.WriteString("\n")
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(b.String()), 0644); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	return dir
}

func TestQueueDependencyUnits(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want []string
	}{
		{
			name: "redis backend",
			env:  map[string]string{"QUEUE_CONNECTION": "redis"},
			want: []string{"lerd-redis.service"},
		},
		{
			name: "database backend with mysql",
			env:  map[string]string{"QUEUE_CONNECTION": "database", "DB_CONNECTION": "mysql"},
			want: []string{"lerd-mysql.service"},
		},
		{
			name: "database backend with postgres",
			env:  map[string]string{"QUEUE_CONNECTION": "database", "DB_CONNECTION": "pgsql"},
			want: []string{"lerd-postgres.service"},
		},
		{
			name: "database backend with sqlite has no lerd service dep",
			env:  map[string]string{"QUEUE_CONNECTION": "database", "DB_CONNECTION": "sqlite"},
			want: nil,
		},
		{
			name: "sync backend has no service dep",
			env:  map[string]string{"QUEUE_CONNECTION": "sync"},
			want: nil,
		},
		{
			name: "external backend has no local service dep",
			env:  map[string]string{"QUEUE_CONNECTION": "sqs"},
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sitePath := writeTempEnv(t, tc.env)
			got := queueDependencyUnits(sitePath)
			if !equalStringSlices(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// Missing .env happens during install-time restore on a freshly-linked
// project before env_setup has run.
func TestQueueDependencyUnits_NoEnv(t *testing.T) {
	dir := t.TempDir() // no .env at all
	if got := queueDependencyUnits(dir); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

// Regression for the bug where install-time restore couldn't recreate the
// queue unit for QUEUE_CONNECTION=redis sites because the old preflight
// rejected the write before lerd-redis was up.
func TestBuildQueueUnit_RedisBackendRendersDependencies(t *testing.T) {
	sitePath := writeTempEnv(t, map[string]string{"QUEUE_CONNECTION": "redis"})
	unit := buildQueueUnit("whitewaters", sitePath, "8.4", "default", 3, 60)

	mustContain(t, unit, "Description=Lerd Queue Worker (whitewaters)")
	mustContain(t, unit, "After=network.target lerd-php84-fpm.service lerd-redis.service")
	mustContain(t, unit, "Wants=lerd-php84-fpm.service lerd-redis.service")
	mustContain(t, unit, "BindsTo=lerd-php84-fpm.service")
	mustContain(t, unit, "Restart=always")
	mustContain(t, unit, "ExecStart="+podman.PodmanBin()+" exec -w "+sitePath+" lerd-php84-fpm php artisan queue:work --queue=default --tries=3 --timeout=60")
	mustContain(t, unit, "WantedBy=default.target")
}

func TestBuildQueueUnit_SyncBackendOmitsServiceDep(t *testing.T) {
	sitePath := writeTempEnv(t, map[string]string{"QUEUE_CONNECTION": "sync"})
	unit := buildQueueUnit("astrolov", sitePath, "8.4", "default", 3, 60)

	mustContain(t, unit, "After=network.target lerd-php84-fpm.service")
	mustContain(t, unit, "Wants=lerd-php84-fpm.service")
	if strings.Contains(unit, "lerd-redis.service") {
		t.Errorf("sync backend should not depend on redis, unit:\n%s", unit)
	}
}

func TestBuildQueueUnit_DatabaseBackendUsesDBConnection(t *testing.T) {
	sitePath := writeTempEnv(t, map[string]string{
		"QUEUE_CONNECTION": "database",
		"DB_CONNECTION":    "pgsql",
	})
	unit := buildQueueUnit("admin-astrolov", sitePath, "8.3", "default", 3, 60)

	mustContain(t, unit, "After=network.target lerd-php83-fpm.service lerd-postgres.service")
	mustContain(t, unit, "Wants=lerd-php83-fpm.service lerd-postgres.service")
}

func TestBuildHorizonUnit_AlwaysDependsOnRedis(t *testing.T) {
	unit := buildHorizonUnit("score-diviner", "/home/u/score-diviner", "8.4")

	mustContain(t, unit, "Description=Lerd Horizon (score-diviner)")
	mustContain(t, unit, "After=network.target lerd-php84-fpm.service lerd-redis.service")
	mustContain(t, unit, "Wants=lerd-php84-fpm.service lerd-redis.service")
	mustContain(t, unit, "BindsTo=lerd-php84-fpm.service")
	mustContain(t, unit, "ExecStart="+podman.PodmanBin()+" exec -w /home/u/score-diviner lerd-php84-fpm php artisan horizon")
}

func mustContain(t *testing.T, body, needle string) {
	t.Helper()
	if !strings.Contains(body, needle) {
		t.Errorf("expected unit body to contain %q\n--- unit ---\n%s", needle, body)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
