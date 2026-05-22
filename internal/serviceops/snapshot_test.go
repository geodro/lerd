package serviceops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/geodro/lerd/internal/config"
)

func TestSanitizeSnapshotName(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"plain", "pre-refactor", "pre-refactor", false},
		{"trims whitespace", "  nightly  ", "nightly", false},
		{"with spaces", "before big change", "before big change", false},
		{"empty", "", "", true},
		{"whitespace only", "   ", "", true},
		{"dot", ".", "", true},
		{"dotdot", "..", "", true},
		{"leading dot", ".hidden", "", true},
		{"forward slash", "a/b", "", true},
		{"back slash", `a\b`, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sanitizeSnapshotName(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReservedSnapshotName(t *testing.T) {
	for _, name := range []string{"list", "LS", "snapshots", "rm", "delete", "remove", "restore"} {
		if _, reserved := reservedSnapshotName(name); !reserved {
			t.Errorf("%q should be reserved", name)
		}
	}
	for _, name := range []string{"pre-refactor", "nightly", "before big change", "listing"} {
		if _, reserved := reservedSnapshotName(name); reserved {
			t.Errorf("%q should not be reserved", name)
		}
	}
}

func TestCreateSnapshotRejectsReservedName(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	_, err := CreateSnapshot(SnapshotTarget{Service: "mysql", Family: "mysql", Database: "myapp"}, "list", SnapshotMeta{}, nil)
	if err == nil {
		t.Fatal("expected reserved-name error")
	}
	if !strings.Contains(err.Error(), "db:snapshots") {
		t.Errorf("error should point to db:snapshots, got: %v", err)
	}
}

func TestSnapshotDirPaths(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	perDB := snapshotDir("mysql", "myapp", "nightly", false)
	wantPerDB := filepath.Join(config.SnapshotsDir(), "mysql", "databases", "myapp", "nightly")
	if perDB != wantPerDB {
		t.Errorf("per-database dir: got %q, want %q", perDB, wantPerDB)
	}

	allDB := snapshotDir("postgres", "ignored", "full", true)
	wantAll := filepath.Join(config.SnapshotsDir(), "postgres", "all", "full")
	if allDB != wantAll {
		t.Errorf("all-databases dir: got %q, want %q", allDB, wantAll)
	}
}

func TestWriteReadSnapshotMeta(t *testing.T) {
	dir := t.TempDir()
	want := Snapshot{
		Name:         "nightly",
		Created:      time.Date(2026, 5, 22, 14, 3, 0, 0, time.UTC),
		Service:      "mysql",
		Family:       "mysql",
		Database:     "myapp",
		AllDatabases: false,
		DumpFile:     snapshotDumpFile,
		Compressed:   true,
		SizeBytes:    184320,
		Site:         "myapp",
		GitBranch:    "feature/orders",
	}
	if err := writeSnapshotMeta(dir, want); err != nil {
		t.Fatalf("writeSnapshotMeta: %v", err)
	}
	got, err := readSnapshotMeta(dir)
	if err != nil {
		t.Fatalf("readSnapshotMeta: %v", err)
	}
	if !got.Created.Equal(want.Created) {
		t.Errorf("Created: got %v, want %v", got.Created, want.Created)
	}
	got.Created, want.Created = time.Time{}, time.Time{}
	if got != want {
		t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestReadSnapshotMetaMissing(t *testing.T) {
	_, err := readSnapshotMeta(t.TempDir())
	if !os.IsNotExist(err) {
		t.Fatalf("expected not-exist error, got %v", err)
	}
}

// seedSnapshot writes a snapshot directory with a meta.json for list/delete tests.
func seedSnapshot(t *testing.T, s Snapshot) {
	t.Helper()
	dir := snapshotDir(s.Service, s.Database, s.Name, s.AllDatabases)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir snapshot: %v", err)
	}
	if err := writeSnapshotMeta(dir, s); err != nil {
		t.Fatalf("seed meta: %v", err)
	}
}

func TestListSnapshots(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	base := time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC)
	seedSnapshot(t, Snapshot{Name: "old", Created: base, Service: "mysql", Family: "mysql", Database: "myapp"})
	seedSnapshot(t, Snapshot{Name: "new", Created: base.Add(time.Hour), Service: "mysql", Family: "mysql", Database: "myapp"})
	seedSnapshot(t, Snapshot{Name: "other", Created: base.Add(2 * time.Hour), Service: "mysql", Family: "mysql", Database: "shop"})
	seedSnapshot(t, Snapshot{Name: "whole", Created: base.Add(3 * time.Hour), Service: "mysql", Family: "mysql", AllDatabases: true})

	t.Run("single database includes all-scope", func(t *testing.T) {
		got, err := ListSnapshots("mysql", "myapp", true)
		if err != nil {
			t.Fatalf("ListSnapshots: %v", err)
		}
		// myapp/new, myapp/old, plus the all-databases snapshot — sorted newest first.
		want := []string{"whole", "new", "old"}
		if len(got) != len(want) {
			t.Fatalf("got %d snapshots, want %d (%+v)", len(got), len(want), got)
		}
		for i, name := range want {
			if got[i].Name != name {
				t.Errorf("position %d: got %q, want %q", i, got[i].Name, name)
			}
		}
	})

	t.Run("single database without all-scope", func(t *testing.T) {
		got, err := ListSnapshots("mysql", "myapp", false)
		if err != nil {
			t.Fatalf("ListSnapshots: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d snapshots, want 2 (%+v)", len(got), got)
		}
	})

	t.Run("every database", func(t *testing.T) {
		got, err := ListSnapshots("mysql", "", true)
		if err != nil {
			t.Fatalf("ListSnapshots: %v", err)
		}
		if len(got) != 4 {
			t.Fatalf("got %d snapshots, want 4 (%+v)", len(got), got)
		}
		if got[0].Name != "whole" {
			t.Errorf("newest first: got %q, want %q", got[0].Name, "whole")
		}
	})

	t.Run("unknown service is empty", func(t *testing.T) {
		got, err := ListSnapshots("nope", "", true)
		if err != nil {
			t.Fatalf("ListSnapshots: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("got %d snapshots, want 0", len(got))
		}
	})
}

func TestDeleteSnapshot(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	seedSnapshot(t, Snapshot{Name: "gone-soon", Created: time.Now().UTC(), Service: "postgres", Family: "postgres", Database: "myapp"})

	if err := DeleteSnapshot("postgres", "myapp", "gone-soon", false); err != nil {
		t.Fatalf("DeleteSnapshot: %v", err)
	}
	if _, err := os.Stat(snapshotDir("postgres", "myapp", "gone-soon", false)); !os.IsNotExist(err) {
		t.Errorf("snapshot dir still present after delete")
	}

	if err := DeleteSnapshot("postgres", "myapp", "gone-soon", false); err == nil {
		t.Errorf("expected error deleting a missing snapshot")
	}
}

func TestSnapshotDumpCommand(t *testing.T) {
	tests := []struct {
		name   string
		target SnapshotTarget
		want   string
	}{
		{
			"mysql one database",
			SnapshotTarget{Family: "mysql", Database: "myapp"},
			`$(command -v mysqldump || command -v mariadb-dump) -uroot --single-transaction --quick --no-tablespaces --routines --triggers --events 'myapp' | gzip -c`,
		},
		{
			"mariadb one database",
			SnapshotTarget{Family: "mariadb", Database: "shop"},
			`$(command -v mysqldump || command -v mariadb-dump) -uroot --single-transaction --quick --no-tablespaces --routines --triggers --events 'shop' | gzip -c`,
		},
		{
			"mysql all databases",
			SnapshotTarget{Family: "mysql", AllDatabases: true},
			`$(command -v mysqldump || command -v mariadb-dump) -uroot --single-transaction --quick --no-tablespaces --routines --triggers --events --add-drop-database --all-databases | gzip -c`,
		},
		{
			"postgres one database",
			SnapshotTarget{Family: "postgres", Database: "myapp"},
			`pg_dump -U postgres --clean --if-exists 'myapp' | gzip -c`,
		},
		{
			"postgres all databases",
			SnapshotTarget{Family: "postgres", AllDatabases: true},
			`pg_dumpall -U postgres --clean --if-exists | gzip -c`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := snapshotDumpCommand(tt.target)
			if err != nil {
				t.Fatalf("snapshotDumpCommand: %v", err)
			}
			if got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}

	if _, err := snapshotDumpCommand(SnapshotTarget{Family: "mongo", Database: "x"}); err == nil {
		t.Errorf("expected error for unsupported family")
	}
}

func TestSnapshotRestoreCommand(t *testing.T) {
	tests := []struct {
		name   string
		target SnapshotTarget
		want   string
	}{
		{
			"mysql one database",
			SnapshotTarget{Family: "mysql", Database: "myapp"},
			`gunzip -c | $(command -v mysql || command -v mariadb) -uroot 'myapp'`,
		},
		{
			"mysql all databases",
			SnapshotTarget{Family: "mariadb", AllDatabases: true},
			`gunzip -c | $(command -v mysql || command -v mariadb) -uroot`,
		},
		{
			"postgres one database",
			SnapshotTarget{Family: "postgres", Database: "myapp"},
			`gunzip -c | psql -U postgres -d 'myapp'`,
		},
		{
			"postgres all databases",
			SnapshotTarget{Family: "postgres", AllDatabases: true},
			`gunzip -c | psql -U postgres -d postgres`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := snapshotRestoreCommand(tt.target)
			if err != nil {
				t.Fatalf("snapshotRestoreCommand: %v", err)
			}
			if got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}

	if _, err := snapshotRestoreCommand(SnapshotTarget{Family: "redis"}); err == nil {
		t.Errorf("expected error for unsupported family")
	}
}

func TestSnapshotFamilySupported(t *testing.T) {
	for _, fam := range []string{"mysql", "mariadb", "postgres"} {
		if !SnapshotFamilySupported(fam) {
			t.Errorf("%q should be supported", fam)
		}
	}
	for _, fam := range []string{"mongo", "redis", "valkey", ""} {
		if SnapshotFamilySupported(fam) {
			t.Errorf("%q should not be supported", fam)
		}
	}
}
