package serviceops

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// Snapshot is the meta.json sidecar describing one stored database snapshot.
type Snapshot struct {
	Name         string    `json:"name"`
	Created      time.Time `json:"created"`
	Service      string    `json:"service"`
	Family       string    `json:"family"`
	Database     string    `json:"database"`
	AllDatabases bool      `json:"all_databases"`
	DumpFile     string    `json:"dump_file"`
	Compressed   bool      `json:"compressed"`
	SizeBytes    int64     `json:"size_bytes"`
	Site         string    `json:"site,omitempty"`
	GitBranch    string    `json:"git_branch,omitempty"`
}

// SnapshotTarget identifies the live database a snapshot is taken from or
// restored into. The cli and mcp layers build it from a resolved DB env.
type SnapshotTarget struct {
	Service      string
	Family       string
	Database     string
	AllDatabases bool
}

// SnapshotMeta carries the optional best-effort context recorded into a
// snapshot's meta.json.
type SnapshotMeta struct {
	Site      string
	GitBranch string
}

const (
	snapshotDumpFile = "dump.sql.gz"
	snapshotMetaFile = "meta.json"
	snapshotDBScope  = "databases"
	snapshotAllScope = "all"
	mysqldumpFlags   = "--single-transaction --quick --no-tablespaces --routines --triggers --events"
)

// reservedSnapshotName flags snapshot names that collide with command verbs,
// catching mistakes like `lerd db snapshot list` (which would otherwise create
// a snapshot literally named "list" instead of listing).
func reservedSnapshotName(name string) (hint string, reserved bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "list", "ls", "snapshots":
		return "did you mean `lerd db:snapshots` to list snapshots?", true
	case "rm", "remove", "delete", "del":
		return "use `lerd db:snapshot:rm <name>` to delete a snapshot", true
	case "restore":
		return "use `lerd db:restore <name>` to restore a snapshot", true
	}
	return "", false
}

// sanitizeSnapshotName validates a user-supplied snapshot name so it is safe to
// use as a single filesystem path component.
func sanitizeSnapshotName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", fmt.Errorf("snapshot name cannot be empty")
	case name == "." || name == "..":
		return "", fmt.Errorf("invalid snapshot name %q", name)
	case strings.HasPrefix(name, "."):
		return "", fmt.Errorf("snapshot name cannot start with a dot: %q", name)
	case strings.ContainsAny(name, `/\`):
		return "", fmt.Errorf("snapshot name cannot contain path separators: %q", name)
	}
	return name, nil
}

// snapshotScopeDir returns the directory holding every snapshot for a given
// scope. When allDatabases is set the database argument is ignored and the
// service-wide scope is used.
func snapshotScopeDir(service, database string, allDatabases bool) string {
	if allDatabases {
		return filepath.Join(config.SnapshotsDir(), service, snapshotAllScope)
	}
	return filepath.Join(config.SnapshotsDir(), service, snapshotDBScope, database)
}

// snapshotDir returns the directory for one named snapshot.
func snapshotDir(service, database, name string, allDatabases bool) string {
	return filepath.Join(snapshotScopeDir(service, database, allDatabases), name)
}

func writeSnapshotMeta(dir string, s Snapshot) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, snapshotMetaFile), data, 0600)
}

// readSnapshotMeta loads a snapshot's meta.json. The os.ReadFile error is
// returned unwrapped so callers can test a missing snapshot with os.IsNotExist.
func readSnapshotMeta(dir string) (Snapshot, error) {
	var s Snapshot
	data, err := os.ReadFile(filepath.Join(dir, snapshotMetaFile))
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("parsing %s: %w", snapshotMetaFile, err)
	}
	return s, nil
}

// snapshotEnv returns the podman exec env pairs carrying the container admin
// password for the target family.
func snapshotEnv(family string) []string {
	if family == "postgres" {
		return []string{"PGPASSWORD=lerd"}
	}
	return []string{"MYSQL_PWD=lerd"}
}

// snapshotDumpCommand builds the in-container shell command that writes a
// gzipped SQL dump to stdout for the given target.
func snapshotDumpCommand(t SnapshotTarget) (string, error) {
	switch t.Family {
	case "mysql", "mariadb":
		bin := "$(command -v mysqldump || command -v mariadb-dump)"
		args := "-uroot " + mysqldumpFlags
		if t.AllDatabases {
			// --add-drop-database makes the dump self-cleaning so an
			// all-databases restore replaces each contained database.
			args += " --add-drop-database --all-databases"
		} else {
			args += " " + podman.ShellQuote(t.Database)
		}
		return bin + " " + args + " | gzip -c", nil
	case "postgres":
		if t.AllDatabases {
			return "pg_dumpall -U postgres --clean --if-exists | gzip -c", nil
		}
		return "pg_dump -U postgres --clean --if-exists " + podman.ShellQuote(t.Database) + " | gzip -c", nil
	default:
		return "", fmt.Errorf("snapshots are not supported for the %q database family", t.Family)
	}
}

// snapshotRestoreCommand builds the in-container shell command that loads a
// gzipped SQL dump piped onto its stdin.
func snapshotRestoreCommand(t SnapshotTarget) (string, error) {
	switch t.Family {
	case "mysql", "mariadb":
		bin := "$(command -v mysql || command -v mariadb)"
		if t.AllDatabases {
			return "gunzip -c | " + bin + " -uroot", nil
		}
		return "gunzip -c | " + bin + " -uroot " + podman.ShellQuote(t.Database), nil
	case "postgres":
		if t.AllDatabases {
			return "gunzip -c | psql -U postgres -d postgres", nil
		}
		return "gunzip -c | psql -U postgres -d " + podman.ShellQuote(t.Database), nil
	default:
		return "", fmt.Errorf("snapshots are not supported for the %q database family", t.Family)
	}
}

// SnapshotFamilySupported reports whether the named database family can be
// snapshotted (SQL engines only).
func SnapshotFamilySupported(family string) bool {
	switch family {
	case "mysql", "mariadb", "postgres":
		return true
	default:
		return false
	}
}

// ListSnapshots returns the stored snapshots for a service. A non-empty
// database narrows the result to that database; an empty database lists every
// database on the service. Service-wide all-databases snapshots are included
// when includeAll is set. Results are sorted newest first.
func ListSnapshots(service, database string, includeAll bool) ([]Snapshot, error) {
	var out []Snapshot

	collect := func(scopeDir string) error {
		entries, err := os.ReadDir(scopeDir)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			s, err := readSnapshotMeta(filepath.Join(scopeDir, e.Name()))
			if err != nil {
				continue // skip partial or unreadable snapshots
			}
			out = append(out, s)
		}
		return nil
	}

	if database != "" {
		if err := collect(snapshotScopeDir(service, database, false)); err != nil {
			return nil, err
		}
	} else {
		dbRoot := filepath.Join(config.SnapshotsDir(), service, snapshotDBScope)
		dbDirs, err := os.ReadDir(dbRoot)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		for _, d := range dbDirs {
			if !d.IsDir() {
				continue
			}
			if err := collect(filepath.Join(dbRoot, d.Name())); err != nil {
				return nil, err
			}
		}
	}

	if includeAll {
		if err := collect(snapshotScopeDir(service, "", true)); err != nil {
			return nil, err
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out, nil
}

// DeleteSnapshot removes a stored snapshot, erroring when it does not exist so
// callers can report the miss clearly.
func DeleteSnapshot(service, database, name string, allDatabases bool) error {
	clean, err := sanitizeSnapshotName(name)
	if err != nil {
		return err
	}
	dir := snapshotDir(service, database, clean, allDatabases)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("snapshot %q not found", name)
		}
		return err
	}
	return os.RemoveAll(dir)
}

// CreateSnapshot dumps the target database (or every database when
// t.AllDatabases is set) into a new named snapshot under config.SnapshotsDir().
// An empty name is auto-generated from the current UTC time.
func CreateSnapshot(t SnapshotTarget, name string, ctx SnapshotMeta, emit func(PhaseEvent)) (*Snapshot, error) {
	if emit == nil {
		emit = func(PhaseEvent) {}
	}
	dumpCmd, err := snapshotDumpCommand(t)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) == "" {
		name = "snapshot-" + timestamped()
	} else if hint, reserved := reservedSnapshotName(name); reserved {
		return nil, fmt.Errorf("%q is not a valid snapshot name — %s", strings.TrimSpace(name), hint)
	}
	clean, err := sanitizeSnapshotName(name)
	if err != nil {
		return nil, err
	}

	unlock := lockService(t.Service)
	defer unlock()

	dir := snapshotDir(t.Service, t.Database, clean, t.AllDatabases)
	if _, err := os.Stat(dir); err == nil {
		return nil, fmt.Errorf("snapshot %q already exists — delete it first with db:snapshot:rm", clean)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("creating snapshot dir: %w", err)
	}

	label := t.Database
	if t.AllDatabases {
		label = "all databases"
	}
	emit(PhaseEvent{Phase: "dumping_data", Message: "dumping " + label})
	dumpPath := filepath.Join(dir, snapshotDumpFile)
	if err := dumpToHost("lerd-"+t.Service, dumpCmd, snapshotEnv(t.Family), dumpPath, dumpRestoreTimeout); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("dumping %s: %w", label, err)
	}

	var size int64
	if fi, statErr := os.Stat(dumpPath); statErr == nil {
		size = fi.Size()
	}

	database := t.Database
	if t.AllDatabases {
		database = ""
	}
	snap := Snapshot{
		Name:         clean,
		Created:      time.Now().UTC(),
		Service:      t.Service,
		Family:       t.Family,
		Database:     database,
		AllDatabases: t.AllDatabases,
		DumpFile:     snapshotDumpFile,
		Compressed:   true,
		SizeBytes:    size,
		Site:         ctx.Site,
		GitBranch:    ctx.GitBranch,
	}
	if err := writeSnapshotMeta(dir, snap); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("writing snapshot metadata: %w", err)
	}
	emit(PhaseEvent{Phase: "done", Message: "snapshot " + clean + " created"})
	return &snap, nil
}

// RestoreSnapshot loads a stored snapshot back into its database. A per-database
// restore drops and recreates the target database first so no orphan tables
// survive; an all-databases restore replays the self-cleaning dump as-is.
func RestoreSnapshot(t SnapshotTarget, name string, emit func(PhaseEvent)) error {
	if emit == nil {
		emit = func(PhaseEvent) {}
	}
	restoreCmd, err := snapshotRestoreCommand(t)
	if err != nil {
		return err
	}
	clean, err := sanitizeSnapshotName(name)
	if err != nil {
		return err
	}

	unlock := lockService(t.Service)
	defer unlock()

	dir := snapshotDir(t.Service, t.Database, clean, t.AllDatabases)
	snap, err := readSnapshotMeta(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("snapshot %q not found", name)
		}
		return fmt.Errorf("reading snapshot %q: %w", name, err)
	}
	dumpPath := filepath.Join(dir, snap.DumpFile)

	if !t.AllDatabases {
		emit(PhaseEvent{Phase: "dropping_database", Message: "recreating " + t.Database})
		if _, err := DropDatabase(t.Service, t.Database); err != nil {
			return fmt.Errorf("dropping %s: %w", t.Database, err)
		}
		if _, err := CreateDatabase(t.Service, t.Database); err != nil {
			return fmt.Errorf("recreating %s: %w", t.Database, err)
		}
	}

	emit(PhaseEvent{Phase: "restoring_data", Message: "restoring " + clean})
	if err := restoreFromHost("lerd-"+t.Service, restoreCmd, snapshotEnv(t.Family), dumpPath, dumpRestoreTimeout); err != nil {
		return fmt.Errorf("restoring snapshot %q: %w", name, err)
	}
	emit(PhaseEvent{Phase: "done", Message: "snapshot " + clean + " restored"})
	return nil
}
