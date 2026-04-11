package docker

import (
	"testing"
)

func TestParseContainers(t *testing.T) {
	input := "abc123\tsail-mysql-1\t0.0.0.0:3306->3306/tcp\tmysql:8.0\n" +
		"def456\tsail-redis-1\t0.0.0.0:6379->6379/tcp\tredis:alpine\n" +
		"ghi789\tmy-app\t\tnginx:latest\n"

	got := ParseContainers(input)
	if len(got) != 3 {
		t.Fatalf("got %d containers, want 3", len(got))
	}
	if got[0].Name != "sail-mysql-1" {
		t.Errorf("got Name=%q, want sail-mysql-1", got[0].Name)
	}
	if got[0].Image != "mysql:8.0" {
		t.Errorf("got Image=%q, want mysql:8.0", got[0].Image)
	}
	if got[2].Ports != "" {
		t.Errorf("got Ports=%q, want empty", got[2].Ports)
	}
}

func TestParseContainers_Empty(t *testing.T) {
	got := ParseContainers("")
	if len(got) != 0 {
		t.Fatalf("got %d containers, want 0", len(got))
	}
}

func TestConflictingPorts(t *testing.T) {
	containers := []Container{
		{Name: "sail-mysql-1", Ports: "0.0.0.0:3306->3306/tcp"},
		{Name: "traefik", Ports: "0.0.0.0:80->80/tcp, 0.0.0.0:443->443/tcp"},
		{Name: "app", Ports: "0.0.0.0:8080->80/tcp"},
	}
	got := ConflictingPorts(containers)

	if got["3306"] != "sail-mysql-1" {
		t.Errorf("port 3306: got %q, want sail-mysql-1", got["3306"])
	}
	if got["80"] != "traefik" {
		t.Errorf("port 80: got %q, want traefik", got["80"])
	}
	if got["443"] != "traefik" {
		t.Errorf("port 443: got %q, want traefik", got["443"])
	}
	if _, ok := got["8080"]; ok {
		t.Error("port 8080 should not be a conflict")
	}
}

func TestParseVolumes(t *testing.T) {
	input := "sail-mysql\nsail-redis\nmy-project_postgres-data\n"
	got := ParseVolumes(input)
	if len(got) != 3 {
		t.Fatalf("got %d volumes, want 3", len(got))
	}
	if got[0].Name != "sail-mysql" {
		t.Errorf("got %q, want sail-mysql", got[0].Name)
	}
}

func TestFindDatabaseVolumes(t *testing.T) {
	vols := []Volume{
		{Name: "sail-mysql"},
		{Name: "sail-redis"},
		{Name: "my-project_postgres-data"},
		{Name: "app-mariadb-vol"},
		{Name: "something-else"},
	}
	got := FindDatabaseVolumes(vols)
	if len(got) != 3 {
		t.Fatalf("got %d, want 3", len(got))
	}
	types := map[string]string{}
	for _, v := range got {
		types[v.Name] = v.DBType
	}
	if types["sail-mysql"] != "mysql" {
		t.Errorf("sail-mysql type=%q, want mysql", types["sail-mysql"])
	}
	if types["my-project_postgres-data"] != "postgres" {
		t.Errorf("postgres volume type=%q, want postgres", types["my-project_postgres-data"])
	}
	if types["app-mariadb-vol"] != "mysql" {
		t.Errorf("mariadb volume type=%q, want mysql", types["app-mariadb-vol"])
	}
}

func TestFindDatabaseContainers(t *testing.T) {
	containers := []Container{
		{Name: "sail-mysql-1", Image: "mysql:8.0"},
		{Name: "sail-redis-1", Image: "redis:alpine"},
		{Name: "pg-1", Image: "postgres:16"},
		{Name: "app", Image: "nginx:latest"},
		{Name: "maria", Image: "mariadb:11"},
	}
	got := FindDatabaseContainers(containers)
	if len(got) != 3 {
		t.Fatalf("got %d, want 3", len(got))
	}
	names := map[string]string{}
	for _, c := range got {
		names[c.Name] = c.DBType
	}
	if names["sail-mysql-1"] != "mysql" {
		t.Errorf("sail-mysql-1 type=%q, want mysql", names["sail-mysql-1"])
	}
	if names["pg-1"] != "postgres" {
		t.Errorf("pg-1 type=%q, want postgres", names["pg-1"])
	}
	if names["maria"] != "mysql" {
		t.Errorf("maria type=%q, want mysql", names["maria"])
	}
}
