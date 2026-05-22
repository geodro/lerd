package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/geodro/lerd/internal/serviceops"
)

func TestHumanSize(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1048576, "1.0 MiB"},
		{5 * 1048576, "5.0 MiB"},
		{1073741824, "1.0 GiB"},
	}
	for _, tt := range tests {
		if got := humanSize(tt.in); got != tt.want {
			t.Errorf("humanSize(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSnapshotTarget(t *testing.T) {
	withTempXDG(t)

	tests := []struct {
		name       string
		env        dbEnv
		all        bool
		wantFamily string
	}{
		{"mysql", dbEnv{service: "mysql", database: "myapp"}, false, "mysql"},
		{"postgres", dbEnv{service: "postgres", database: "shop"}, false, "postgres"},
		{"all databases", dbEnv{service: "mysql", database: "myapp"}, true, "mysql"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := tt.env
			got := snapshotTarget(&env, tt.all)
			if got.Service != tt.env.service {
				t.Errorf("Service = %q, want %q", got.Service, tt.env.service)
			}
			if got.Family != tt.wantFamily {
				t.Errorf("Family = %q, want %q", got.Family, tt.wantFamily)
			}
			if got.Database != tt.env.database {
				t.Errorf("Database = %q, want %q", got.Database, tt.env.database)
			}
			if got.AllDatabases != tt.all {
				t.Errorf("AllDatabases = %v, want %v", got.AllDatabases, tt.all)
			}
		})
	}
}

func TestPrintSnapshotTable(t *testing.T) {
	created := time.Date(2026, 5, 22, 14, 3, 0, 0, time.UTC)
	snaps := []serviceops.Snapshot{
		{Name: "pre-refactor", Created: created, Database: "myapp", SizeBytes: 184320, GitBranch: "feature/orders"},
		{Name: "full", Created: created, AllDatabases: true, SizeBytes: 1048576},
	}
	out := captureStdout(t, func() { printSnapshotTable(snaps) })

	for _, want := range []string{"NAME", "CREATED", "DATABASE", "SIZE", "BRANCH"} {
		if !strings.Contains(out, want) {
			t.Errorf("table header missing %q\n%s", want, out)
		}
	}
	if !strings.Contains(out, "pre-refactor") || !strings.Contains(out, "feature/orders") {
		t.Errorf("per-database row missing:\n%s", out)
	}
	if !strings.Contains(out, "(all)") {
		t.Errorf("all-databases row should show (all):\n%s", out)
	}
	if !strings.Contains(out, "180.0 KiB") {
		t.Errorf("size column missing:\n%s", out)
	}
}

func TestRunDbSnapshotsEmpty(t *testing.T) {
	withTempXDG(t)

	out := captureStdout(t, func() {
		if err := runDbSnapshots("mysql", "", false); err != nil {
			t.Fatalf("runDbSnapshots: %v", err)
		}
	})
	if !strings.Contains(out, "No snapshots") {
		t.Errorf("expected empty-state message, got:\n%s", out)
	}
}
