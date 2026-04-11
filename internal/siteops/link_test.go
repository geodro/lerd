package siteops

import (
	"testing"
)

func TestDetectSiteVersions_Defaults(t *testing.T) {
	dir := t.TempDir()
	php, node := DetectSiteVersions(dir, "", "8.4", "22")
	if php != "8.4" {
		t.Errorf("phpVersion = %q, want 8.4 (default)", php)
	}
	if node != "22" {
		t.Errorf("nodeVersion = %q, want 22 (default)", node)
	}
}

func TestDetectSiteVersions_UnknownFramework(t *testing.T) {
	dir := t.TempDir()
	php, node := DetectSiteVersions(dir, "nonexistent", "8.4", "22")
	if php != "8.4" {
		t.Errorf("phpVersion = %q, want 8.4 (no framework found)", php)
	}
	if node != "22" {
		t.Errorf("nodeVersion = %q, want 22", node)
	}
}

func TestCleanupRelink_NoExisting(t *testing.T) {
	secured := CleanupRelink("/tmp/nonexistent-path-"+t.Name(), "mysite")
	if secured {
		t.Error("expected secured=false for non-existent path")
	}
}
