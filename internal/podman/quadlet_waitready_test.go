package podman

import (
	"strings"
	"testing"
)

func TestMySQLReadinessProbeForcesTCP(t *testing.T) {
	// The probe runs inside the container. With no host, mysqladmin falls back
	// to the Unix socket, whose path differs between the mysql and mariadb
	// images, so a socket probe times out even when the server is up.
	joined := strings.Join(mysqlReadyArgs, " ")

	if mysqlReadyArgs[0] != "mysqladmin" || mysqlReadyArgs[1] != "ping" {
		t.Fatalf("mysql readiness probe should be a mysqladmin ping, got: %s", joined)
	}
	if !strings.Contains(joined, "-h127.0.0.1") {
		t.Errorf("mysql readiness probe must force TCP via -h127.0.0.1, got: %s", joined)
	}
	if strings.Contains(joined, "localhost") {
		t.Errorf("mysql readiness probe must not use localhost — mysqladmin resolves it to the Unix socket, got: %s", joined)
	}
}
