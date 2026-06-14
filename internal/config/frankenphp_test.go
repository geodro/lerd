package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFrameworkFrankenPHPEntrypoint(t *testing.T) {
	// Framework with full declaration.
	fw := &Framework{
		PublicDir: "public",
		FrankenPHP: &FrameworkFrankenPHP{
			Entrypoint:       []string{"php", "artisan", "serve"},
			WorkerEntrypoint: []string{"php", "artisan", "serve", "--worker"},
			SupportsWorker:   true,
		},
	}
	if got := fw.FrankenPHPEntrypoint(false); !reflect.DeepEqual(got, []string{"php", "artisan", "serve"}) {
		t.Fatalf("regular entrypoint: got %v", got)
	}
	if got := fw.FrankenPHPEntrypoint(true); !reflect.DeepEqual(got, []string{"php", "artisan", "serve", "--worker"}) {
		t.Fatalf("worker entrypoint: got %v", got)
	}

	// Framework without worker support: worker flag falls back to regular.
	noWorker := &Framework{
		FrankenPHP: &FrameworkFrankenPHP{
			Entrypoint:     []string{"frankenphp", "php-server"},
			SupportsWorker: false,
		},
	}
	if got := noWorker.FrankenPHPEntrypoint(true); !reflect.DeepEqual(got, []string{"frankenphp", "php-server"}) {
		t.Fatalf("no-worker fallback: got %v", got)
	}

	// Framework with no FrankenPHP declaration: generic fallback rooted at PublicDir.
	generic := &Framework{PublicDir: "web"}
	wantGeneric := []string{"frankenphp", "php-server", "-l", ":8000", "-r", "web"}
	if got := generic.FrankenPHPEntrypoint(false); !reflect.DeepEqual(got, wantGeneric) {
		t.Fatalf("generic fallback: want %v, got %v", wantGeneric, got)
	}
}

func TestFrameworkFrankenPHPEnvMerging(t *testing.T) {
	fw := &Framework{
		FrankenPHP: &FrameworkFrankenPHP{
			Env:            map[string]string{"APP_ENV": "prod"},
			WorkerEnv:      map[string]string{"FRANKENPHP_CONFIG": "worker ./public/index.php", "APP_ENV": "worker"},
			SupportsWorker: true,
		},
	}
	regular := fw.FrankenPHPEnv(false)
	if regular["APP_ENV"] != "prod" || len(regular) != 1 {
		t.Fatalf("regular env: got %v", regular)
	}
	worker := fw.FrankenPHPEnv(true)
	if worker["APP_ENV"] != "worker" {
		t.Errorf("worker env APP_ENV: got %s", worker["APP_ENV"])
	}
	if worker["FRANKENPHP_CONFIG"] != "worker ./public/index.php" {
		t.Errorf("worker env FRANKENPHP_CONFIG: got %s", worker["FRANKENPHP_CONFIG"])
	}
}

func TestSymfonyFrameworkIsBuiltIn(t *testing.T) {
	if GetFrameworkSource("symfony") != SourceBuiltIn {
		t.Fatalf("symfony source: want built-in, got %s", GetFrameworkSource("symfony"))
	}
	fw, ok := GetFramework("symfony")
	if !ok {
		t.Fatal("GetFramework symfony: not found")
	}
	if fw.Name != "symfony" {
		t.Fatalf("symfony name: got %s", fw.Name)
	}
	if fw.FrankenPHP == nil {
		t.Fatal("symfony framework missing FrankenPHP adapter")
	}
}

func TestSymfonyBuiltinMatchesRuntimePackage(t *testing.T) {
	dir := t.TempDir()
	composer := `{"require":{"symfony/runtime":"^7.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(composer), 0644); err != nil {
		t.Fatal(err)
	}
	fw := copyBuiltin("symfony")
	if fw == nil {
		t.Fatal("copyBuiltin(symfony) nil")
	}
	if !matchesFramework(dir, fw) {
		t.Fatal("symfony built-in should match composer.json with symfony/runtime")
	}
}

func TestSymfonyBuiltinMatchesLockFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "symfony.lock"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	fw := copyBuiltin("symfony")
	if !matchesFramework(dir, fw) {
		t.Fatal("symfony built-in should match symfony.lock")
	}
}

func TestLaravelOctaneFrankenPHPAdapter(t *testing.T) {
	fw, ok := GetFramework("laravel")
	if !ok {
		t.Fatal("GetFramework laravel: not found")
	}
	if fw.FrankenPHP == nil {
		t.Fatal("laravel has no FrankenPHP adapter")
	}
	// Non-worker falls back to plain frankenphp php-server for dev ergonomics.
	if e := fw.FrankenPHPEntrypoint(false); !strings.Contains(strings.Join(e, " "), "frankenphp php-server") {
		t.Fatalf("laravel non-worker should use frankenphp php-server: %v", e)
	}
	// Worker mode runs Octane. Extensions are baked into lerd's derived image now,
	// so the entrypoint must not install them at container start.
	worker := strings.Join(fw.FrankenPHPEntrypoint(true), " ")
	if !strings.Contains(worker, "octane:start") || !strings.Contains(worker, "frankenphp") {
		t.Fatalf("laravel worker should use Octane: %s", worker)
	}
	if strings.Contains(worker, "install-php-extensions") || strings.Contains(worker, "apk add") {
		t.Fatalf("worker entrypoint should not install anything at boot: %s", worker)
	}
}

func TestNormalizeFrankenPHPVersion(t *testing.T) {
	// A supported version passes through unchanged.
	if got := NormalizeFrankenPHPVersion(LatestFrankenPHPVersion()); got != LatestFrankenPHPVersion() {
		t.Errorf("supported version changed: %q", got)
	}
	// An unsupported/empty version falls back to the latest frankenphp version.
	if got := NormalizeFrankenPHPVersion("5.6"); got != LatestFrankenPHPVersion() {
		t.Errorf("NormalizeFrankenPHPVersion(5.6) = %q, want %q", got, LatestFrankenPHPVersion())
	}
}
