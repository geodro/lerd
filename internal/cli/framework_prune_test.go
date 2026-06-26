package cli

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func setFrameworkTestDirs(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
}

func seedStoreFramework(t *testing.T, name string) string {
	t.Helper()
	dir := config.StoreFrameworksDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name+".yaml")
	if err := os.WriteFile(path, []byte("name: "+name+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFrameworkPrune_RemovesUnusedKeepsUsed(t *testing.T) {
	setFrameworkTestDirs(t)

	usedPath := seedStoreFramework(t, "wordpress")
	unusedPath := seedStoreFramework(t, "drupal")

	reg := &config.SiteRegistry{Sites: []config.Site{
		{Name: "shop", Framework: "wordpress"},
	}}
	if err := config.SaveSites(reg); err != nil {
		t.Fatalf("SaveSites: %v", err)
	}

	cmd := newFrameworkPruneCmd()
	cmd.SetArgs([]string{"--force"})
	cmd.SetOut(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("prune: %v", err)
	}

	if _, err := os.Stat(unusedPath); !os.IsNotExist(err) {
		t.Error("expected unused framework to be pruned")
	}
	if _, err := os.Stat(usedPath); err != nil {
		t.Errorf("expected used framework to remain, got: %v", err)
	}
}

func TestFrameworkPrune_NothingToPrune(t *testing.T) {
	setFrameworkTestDirs(t)

	usedPath := seedStoreFramework(t, "wordpress")
	reg := &config.SiteRegistry{Sites: []config.Site{
		{Name: "shop", Framework: "wordpress"},
	}}
	if err := config.SaveSites(reg); err != nil {
		t.Fatalf("SaveSites: %v", err)
	}

	cmd := newFrameworkPruneCmd()
	cmd.SetArgs([]string{})
	cmd.SetOut(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("prune: %v", err)
	}

	if _, err := os.Stat(usedPath); err != nil {
		t.Errorf("expected used framework to remain, got: %v", err)
	}
}

func TestFrameworkRemove_ForceSkipsInUseConfirm(t *testing.T) {
	setFrameworkTestDirs(t)

	path := seedStoreFramework(t, "wordpress")
	reg := &config.SiteRegistry{Sites: []config.Site{
		{Name: "blog", Framework: "wordpress"},
	}}
	if err := config.SaveSites(reg); err != nil {
		t.Fatalf("SaveSites: %v", err)
	}

	cmd := newFrameworkRemoveCmd()
	cmd.SetArgs([]string{"wordpress", "--force"})
	cmd.SetOut(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("remove --force: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("expected framework to be removed with --force despite being in use")
	}
}

func TestFrameworkIsOrphaned(t *testing.T) {
	setFrameworkTestDirs(t)

	seedStoreFramework(t, "wordpress") // still used
	seedStoreFramework(t, "drupal")    // orphaned
	seedStoreFramework(t, "symfony")   // built-in overlay, never orphaned
	reg := &config.SiteRegistry{Sites: []config.Site{
		{Name: "shop", Framework: "wordpress"},
	}}
	if err := config.SaveSites(reg); err != nil {
		t.Fatalf("SaveSites: %v", err)
	}

	if frameworkIsOrphaned("wordpress") {
		t.Error("wordpress is still used, should not be orphaned")
	}
	if !frameworkIsOrphaned("drupal") {
		t.Error("drupal has no site, should be orphaned")
	}
	if frameworkIsOrphaned("symfony") {
		t.Error("symfony is built-in, should never be orphaned")
	}
	if frameworkIsOrphaned("") {
		t.Error("empty framework should not be orphaned")
	}
}

func TestFrameworkRemove_VersionSpecificNotBlockedByInUse(t *testing.T) {
	setFrameworkTestDirs(t)

	dir := config.StoreFrameworksDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	v5 := filepath.Join(dir, "drupal@5.yaml")
	v7 := filepath.Join(dir, "drupal@7.yaml")
	os.WriteFile(v5, []byte("name: drupal\nversion: \"5\"\n"), 0644)
	os.WriteFile(v7, []byte("name: drupal\nversion: \"7\"\n"), 0644)

	reg := &config.SiteRegistry{Sites: []config.Site{
		{Name: "site", Framework: "drupal"},
	}}
	if err := config.SaveSites(reg); err != nil {
		t.Fatalf("SaveSites: %v", err)
	}

	cmd := newFrameworkRemoveCmd()
	cmd.SetArgs([]string{"drupal@5"})
	cmd.SetOut(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("remove drupal@5: %v", err)
	}

	if _, err := os.Stat(v5); !os.IsNotExist(err) {
		t.Error("expected drupal@5 to be removed without the in-use guard")
	}
	if _, err := os.Stat(v7); err != nil {
		t.Errorf("expected drupal@7 to remain, got: %v", err)
	}
}
