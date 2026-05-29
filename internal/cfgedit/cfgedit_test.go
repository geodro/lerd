package cfgedit

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func tmpFile(t *testing.T) File {
	t.Helper()
	dir := t.TempDir()
	return File{
		Path:     filepath.Join(dir, "live", "acme.test.conf"),
		BkpDir:   filepath.Join(dir, "bkp"),
		BkpName:  "acme.test.conf",
		Template: "# template\n",
	}
}

func okValidate(string) (string, error) { return "ok", nil }

func TestRead_seedsTemplateWhenMissing(t *testing.T) {
	f := tmpFile(t)
	got, err := f.Read()
	if err != nil {
		t.Fatal(err)
	}
	if got.Exists || got.Body != "# template\n" {
		t.Fatalf("got %+v", got)
	}
}

func TestSave_writesValidatesApplies(t *testing.T) {
	f := tmpFile(t)
	applied := 0
	res, err := f.Save("# v1\n", SaveOpts{Validate: okValidate, Apply: func() error { applied++; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || applied != 1 {
		t.Fatalf("res=%+v applied=%d", res, applied)
	}
	b, _ := os.ReadFile(f.Path)
	if string(b) != "# v1\n" {
		t.Fatalf("on-disk = %q", b)
	}
}

func TestSave_rollsBackWhenValidationOwned(t *testing.T) {
	f := tmpFile(t)
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.Path, []byte("# good\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	applied := 0
	res, _ := f.Save("# bad\n", SaveOpts{
		Validate: func(string) (string, error) { return "error in acme.test.conf:1", errors.New("invalid") },
		Owns:     MentionsFile,
		Apply:    func() error { applied++; return nil },
	})
	if res.OK || applied != 0 {
		t.Fatalf("expected owned rollback, res=%+v applied=%d", res, applied)
	}
	b, _ := os.ReadFile(f.Path)
	if string(b) != "# good\n" {
		t.Fatalf("expected rollback to prior content, got %q", b)
	}
}

func TestSave_neighbourFailureKeepsOurWrite(t *testing.T) {
	f := tmpFile(t)
	res, _ := f.Save("# mine\n", SaveOpts{
		Validate: func(string) (string, error) { return "error in other.test.conf:9", errors.New("invalid") },
		Owns:     MentionsFile,
		Apply:    func() error { return nil },
	})
	if !res.OK {
		t.Fatalf("neighbour failure should not roll us back, res=%+v", res)
	}
	b, _ := os.ReadFile(f.Path)
	if string(b) != "# mine\n" {
		t.Fatalf("our write should survive, got %q", b)
	}
}

func TestSave_applyFailureKeepsBytes(t *testing.T) {
	f := tmpFile(t)
	res, _ := f.Save("# v1\n", SaveOpts{Apply: func() error { return errors.New("reload boom") }})
	if res.OK || res.Content != "# v1\n" || !res.Exists {
		t.Fatalf("apply failure should keep bytes, res=%+v", res)
	}
	b, _ := os.ReadFile(f.Path)
	if string(b) != "# v1\n" {
		t.Fatalf("bytes should remain on disk, got %q", b)
	}
}

func TestSave_backupThenListAndRestore(t *testing.T) {
	f := tmpFile(t)
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.Path, []byte("# v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, _ := f.Save("# v2\n", SaveOpts{Backup: true, Apply: func() error { return nil }})
	if !res.OK || res.BackupName == "" {
		t.Fatalf("expected backup, res=%+v", res)
	}
	list, err := f.ListBackups()
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v", list, err)
	}
	rr, _ := f.Restore(list[0].Name, func() error { return nil })
	if !rr.OK || rr.Content != "# v1\n" {
		t.Fatalf("restore=%+v", rr)
	}
	b, _ := os.ReadFile(f.Path)
	if string(b) != "# v1\n" {
		t.Fatalf("after restore = %q", b)
	}
}

func TestSave_backupSkippedWhenFileMissing(t *testing.T) {
	f := tmpFile(t)
	res, _ := f.Save("# fresh\n", SaveOpts{Backup: true, Apply: func() error { return nil }})
	if !res.OK || res.BackupName != "" {
		t.Fatalf("no backup expected when the file was missing, got %+v", res)
	}
}

func TestRestore_usesBackupModeWhenTargetMissing(t *testing.T) {
	f := tmpFile(t)
	if err := os.MkdirAll(f.BkpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A backup exists but the live file does not; restore must not widen the
	// 0600 secret to a default 0644.
	bkp := filepath.Join(f.BkpDir, f.BkpName+".bkp.20260528-103045")
	if err := os.WriteFile(bkp, []byte("SECRET=42\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rr, _ := f.Restore("", func() error { return nil })
	if !rr.OK {
		t.Fatalf("restore failed: %+v", rr)
	}
	info, err := os.Stat(f.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("restored mode = %o, want 0600 (the backup's mode)", info.Mode().Perm())
	}
}

func TestReset_removesFile(t *testing.T) {
	f := tmpFile(t)
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.Path, []byte("# x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := f.Reset(func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.Path); !os.IsNotExist(err) {
		t.Fatal("expected file removed")
	}
}

func TestValidBackupName_rejectsForeignAndTraversal(t *testing.T) {
	f := tmpFile(t)
	for _, bad := range []string{"../etc/passwd", "other.conf.bkp.20260101-101010", "x_acme.test.conf.bkp.20260101-101010"} {
		if f.ValidBackupName(bad) {
			t.Errorf("should reject %q", bad)
		}
	}
	good := "acme.test.conf.bkp.20260101-101010"
	if !f.ValidBackupName(good) {
		t.Errorf("should accept %q", good)
	}
}

func TestStagedWrite_noTempLeak(t *testing.T) {
	f := tmpFile(t)
	if err := f.StagedWrite([]byte("# x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{filepath.Dir(f.Path), f.BkpDir} {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if strings.Contains(e.Name(), ".tmp.") {
				t.Errorf("temp leak in %s: %s", dir, e.Name())
			}
		}
	}
}

func TestUniqueBackupPath_errorsOnExhaustion(t *testing.T) {
	f := tmpFile(t)
	if err := os.MkdirAll(f.BkpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 5, 28, 10, 10, 10, 0, time.UTC)
	stamp := now.Format("20060102-150405")
	if err := os.WriteFile(filepath.Join(f.BkpDir, f.BkpName+".bkp."+stamp), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < 1000; i++ {
		if err := os.WriteFile(filepath.Join(f.BkpDir, f.BkpName+".bkp."+stamp+"-"+strconv.Itoa(i)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.uniqueBackupPath(now); err == nil {
		t.Fatal("expected exhaustion error")
	}
}
