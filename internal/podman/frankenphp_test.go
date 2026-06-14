package podman

import (
	"strings"
	"testing"
)

func TestFrankenPHPContainerName(t *testing.T) {
	got := FrankenPHPContainerName("myapp")
	if got != "lerd-fp-myapp" {
		t.Fatalf("FrankenPHPContainerName: want lerd-fp-myapp, got %s", got)
	}
}

func TestFrankenPHPImage(t *testing.T) {
	// FrankenPHPImage is now the lerd-derived image; the upstream tag it builds
	// FROM is FrankenPHPBaseImage.
	tests := []struct {
		version, wantDerived, wantBase string
	}{
		{"8.2", "localhost/lerd-frankenphp82:local", "docker.io/dunglas/frankenphp:php8.2-alpine"},
		{"8.4", "localhost/lerd-frankenphp84:local", "docker.io/dunglas/frankenphp:php8.4-alpine"},
		{"8.5", "localhost/lerd-frankenphp85:local", "docker.io/dunglas/frankenphp:php8.5-alpine"},
		{"8.1", "localhost/lerd-frankenphp85:local", "docker.io/dunglas/frankenphp:php8.5-alpine"}, // no frankenphp tag → latest
		{"", "localhost/lerd-frankenphp85:local", "docker.io/dunglas/frankenphp:php8.5-alpine"},
	}
	for _, tt := range tests {
		if got := FrankenPHPImage(tt.version); got != tt.wantDerived {
			t.Errorf("FrankenPHPImage(%q): want %s, got %s", tt.version, tt.wantDerived, got)
		}
		if got := FrankenPHPBaseImage(tt.version); got != tt.wantBase {
			t.Errorf("FrankenPHPBaseImage(%q): want %s, got %s", tt.version, tt.wantBase, got)
		}
	}
}

func TestGenerateFrankenPHPQuadlet(t *testing.T) {
	entry := []string{"php", "artisan", "octane:start", "--server=frankenphp"}
	env := map[string]string{"FRANKENPHP_CONFIG": "worker ./public/index.php"}
	content := GenerateFrankenPHPQuadlet("myapp", "/home/user/myapp", "8.4", entry, env)

	mustContain := []string{
		"ContainerName=lerd-fp-myapp",
		"Image=localhost/lerd-frankenphp84:local",
		"Network=lerd",
		"Volume=/home/user/myapp:/home/user/myapp:rw",
		"--workdir=/home/user/myapp",
		`Environment="FRANKENPHP_CONFIG=worker ./public/index.php"`,
		"Exec=php artisan octane:start --server=frankenphp",
		"Restart=always",
		// Debug tooling parity: the same conf.d inis and bridge dir the FPM
		// container mounts (dump bridge, devtools, xdebug). SPX is excluded.
		"/usr/local/etc/php/conf.d/97-lerd-dump.ini:ro",
		"/usr/local/etc/php/conf.d/96-lerd-devtools.ini:ro",
		"/usr/local/etc/php/conf.d/99-xdebug.ini:ro",
		"/usr/local/etc/php/conf.d/98-lerd-user.ini:ro",
		":/usr/local/etc/lerd:ro",
	}
	for _, s := range mustContain {
		if !strings.Contains(content, s) {
			t.Errorf("generated quadlet missing %q\n%s", s, content)
		}
	}
	// SPX is not wired for FrankenPHP (can't profile Octane workers).
	if strings.Contains(content, "spx") || strings.Contains(content, "/var/spx") {
		t.Errorf("SPX should not be mounted in the FrankenPHP quadlet:\n%s", content)
	}
}

func TestShellJoinQuotesWhitespace(t *testing.T) {
	got := shellJoin([]string{"frankenphp", "run", "--with spaces", `has"quote`})
	want := `frankenphp run "--with spaces" "has\"quote"`
	if got != want {
		t.Fatalf("shellJoin:\n  want %s\n  got  %s", want, got)
	}
}
