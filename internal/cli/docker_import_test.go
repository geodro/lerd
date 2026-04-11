package cli

import (
	"testing"

	dockerDet "github.com/geodro/lerd/internal/docker"
)

func TestDiscoverDockerDBPassword_MySQL(t *testing.T) {
	// discoverDockerDBPassword calls ContainerEnv which runs docker inspect.
	// For unit testing, we test the fallback path directly.
	c := dockerDet.DatabaseContainer{Name: "test", Image: "mysql:8.0", DBType: "mysql"}
	got := discoverDockerDBPassword(c)
	// Without a running container, ContainerEnv returns nil, so it falls back
	// to "password".
	if got != "password" {
		t.Errorf("got %q, want \"password\"", got)
	}
}

func TestDiscoverDockerDBPassword_Postgres(t *testing.T) {
	c := dockerDet.DatabaseContainer{Name: "test", Image: "postgres:16", DBType: "postgres"}
	got := discoverDockerDBPassword(c)
	if got != "password" {
		t.Errorf("got %q, want \"password\"", got)
	}
}

func TestMysqlSystemDBs(t *testing.T) {
	for _, db := range []string{"mysql", "information_schema", "performance_schema", "sys"} {
		if !mysqlSystemDBs[db] {
			t.Errorf("%q should be a system DB", db)
		}
	}
	if mysqlSystemDBs["laravel"] {
		t.Error("laravel should not be a system DB")
	}
}

func TestPostgresSystemDBs(t *testing.T) {
	for _, db := range []string{"postgres", "template0", "template1"} {
		if !postgresSystemDBs[db] {
			t.Errorf("%q should be a system DB", db)
		}
	}
	if postgresSystemDBs["myapp"] {
		t.Error("myapp should not be a system DB")
	}
}
