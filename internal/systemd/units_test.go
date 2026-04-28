package systemd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// PR #255 switches lerd-ui and lerd-watcher to Type=notify so systemctl start
// blocks until the process actually finishes setup (UI listener accepting,
// watcher goroutines live). The two unit files in this package's units/
// directory are the source of truth shipped by the installer; assert each
// one declares the type so the change can't silently regress.
func TestServiceUnitsDeclareTypeNotify(t *testing.T) {
	cases := []struct {
		file string
	}{
		{"lerd-ui.service"},
		{"lerd-watcher.service"},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("units", tc.file))
			if err != nil {
				t.Fatalf("read %s: %v", tc.file, err)
			}
			if !containsLine(string(data), "Type=notify") {
				t.Errorf("%s: missing Type=notify directive\n--- file ---\n%s", tc.file, data)
			}
		})
	}
}

func containsLine(content, line string) bool {
	for _, l := range strings.Split(content, "\n") {
		if strings.TrimSpace(l) == line {
			return true
		}
	}
	return false
}
