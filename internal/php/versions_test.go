package php

import (
	"os"
	"path/filepath"
	"testing"
)

// --- listInstalledFromServiceDir ---

func TestListInstalledFromServiceDir_linux(t *testing.T) {
	// On linux this is a no-op
	result := listInstalledFromServiceDir()
	if result != nil {
		t.Errorf("expected nil on linux, got %v", result)
	}
}

// --- fpmQuadletRe ---

func TestFpmQuadletRe_matchesValidNames(t *testing.T) {
	tests := []struct {
		name    string
		want    string
		matches bool
	}{
		{"lerd-php84-fpm.container", "8.4", true},
		{"lerd-php83-fpm.container", "8.3", true},
		{"lerd-php74-fpm.container", "7.4", true},
		{"lerd-nginx.container", "", false},
		{"lerd-php-fpm.container", "", false},
		{"lerd-php8-fpm.container", "", false},
	}
	for _, tt := range tests {
		sub := fpmQuadletRe.FindStringSubmatch(tt.name)
		if tt.matches {
			if len(sub) != 3 {
				t.Errorf("%s: expected match, got %v", tt.name, sub)
				continue
			}
			got := sub[1] + "." + sub[2]
			if got != tt.want {
				t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
			}
		} else {
			if len(sub) == 3 {
				t.Errorf("%s: expected no match, got %v", tt.name, sub)
			}
		}
	}
}

// --- quadletExists ---

func TestQuadletExists_found(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	dir := filepath.Join(tmp, "containers", "systemd")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "lerd-php84-fpm.container"), []byte("[Container]\n"), 0644)

	if !quadletExists("8.4") {
		t.Error("expected quadletExists to return true")
	}
}

func TestQuadletExists_missing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	if quadletExists("8.4") {
		t.Error("expected quadletExists to return false for missing quadlet")
	}
}

// --- ListInstalled (quadlet source) ---

func TestListInstalled_fromQuadlets(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	dir := filepath.Join(tmp, "containers", "systemd")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "lerd-php84-fpm.container"), []byte("[Container]\n"), 0644)
	os.WriteFile(filepath.Join(dir, "lerd-php83-fpm.container"), []byte("[Container]\n"), 0644)

	versions, err := ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}

	found := map[string]bool{}
	for _, v := range versions {
		found[v] = true
	}
	if !found["8.4"] {
		t.Error("expected 8.4 in installed versions")
	}
	if !found["8.3"] {
		t.Error("expected 8.3 in installed versions")
	}
}
