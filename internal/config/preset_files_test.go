package config

import (
	"strings"
	"testing"
)

func TestMySQLPresetContainsCompatDirectives(t *testing.T) {
	files := PresetFiles("mysql")
	if len(files) == 0 {
		t.Fatal("mysql preset has no file mounts")
	}

	cnf := files[0].Content

	for _, directive := range []string{
		"mysql-native-password=ON",
		"authentication_policy='mysql_native_password,,'",
		"restrict-fk-on-non-standard-key=OFF",
	} {
		if !strings.Contains(cnf, directive) {
			t.Errorf("mysql lerd.cnf missing %q", directive)
		}
	}
}

// Removed in MySQL 8.0; kept silent on 5.7/8.x via the loose- prefix but
// generated a startup warning on every container start. lerd no longer
// ships 5.6, so they should not be re-added.
func TestMySQLPresetExcludesRemovedDirectives(t *testing.T) {
	files := PresetFiles("mysql")
	if len(files) == 0 {
		t.Fatal("mysql preset has no file mounts")
	}

	cnf := files[0].Content

	for _, directive := range []string{
		"innodb_large_prefix",
		"innodb_file_format",
	} {
		if strings.Contains(cnf, directive) {
			t.Errorf("mysql lerd.cnf still contains removed-in-8.0 directive %q", directive)
		}
	}
}
