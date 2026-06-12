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
	tests := []struct {
		version, want string
	}{
		{"8.2", "docker.io/dunglas/frankenphp:php8.2-alpine"},
		{"8.3", "docker.io/dunglas/frankenphp:php8.3-alpine"},
		{"8.4", "docker.io/dunglas/frankenphp:php8.4-alpine"},
		{"8.5", "docker.io/dunglas/frankenphp:php8.5-alpine"},
		{"8.1", "docker.io/dunglas/frankenphp:php8.5-alpine"}, // no frankenphp tag → latest
		{"", "docker.io/dunglas/frankenphp:php8.5-alpine"},
	}
	for _, tt := range tests {
		if got := FrankenPHPImage(tt.version); got != tt.want {
			t.Errorf("FrankenPHPImage(%q): want %s, got %s", tt.version, tt.want, got)
		}
	}
}

func TestGenerateFrankenPHPQuadlet(t *testing.T) {
	entry := []string{"php", "artisan", "octane:start", "--server=frankenphp"}
	env := map[string]string{"FRANKENPHP_CONFIG": "worker ./public/index.php"}
	content := GenerateFrankenPHPQuadlet("myapp", "/home/user/myapp", "8.4", entry, env)

	mustContain := []string{
		"ContainerName=lerd-fp-myapp",
		"Image=docker.io/dunglas/frankenphp:php8.4-alpine",
		"Network=lerd",
		"Volume=/home/user/myapp:/home/user/myapp:rw",
		"--workdir=/home/user/myapp",
		`Environment="FRANKENPHP_CONFIG=worker ./public/index.php"`,
		"Exec=php artisan octane:start --server=frankenphp",
		"Restart=always",
	}
	for _, s := range mustContain {
		if !strings.Contains(content, s) {
			t.Errorf("generated quadlet missing %q\n%s", s, content)
		}
	}
}

func TestShellJoinQuotesWhitespace(t *testing.T) {
	got := shellJoin([]string{"frankenphp", "run", "--with spaces", `has"quote`})
	want := `frankenphp run "--with spaces" "has\"quote"`
	if got != want {
		t.Fatalf("shellJoin:\n  want %s\n  got  %s", want, got)
	}
}
