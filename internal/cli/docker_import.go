package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	dockerDet "github.com/geodro/lerd/internal/docker"
	"github.com/geodro/lerd/internal/podman"
	"github.com/spf13/cobra"
)

// NewDockerImportCmd returns the docker:import command.
func NewDockerImportCmd() *cobra.Command {
	var (
		list         bool
		source       string
		database     string
		dbType       string
		allDatabases bool
	)
	cmd := &cobra.Command{
		Use:   "docker:import",
		Short: "Import databases from Docker containers into Lerd services",
		Long: `Migrate MySQL/MariaDB or PostgreSQL databases from running Docker containers
into Lerd's Podman-managed services.

The export uses "docker exec" and the import uses "podman exec", so there are
no host-port conflicts even when both Docker and Lerd bind the same ports.

Examples:
  lerd docker:import --list
  lerd docker:import
  lerd docker:import --source sail-mysql-1 --database laravel
  lerd docker:import --source sail-mysql-1 --all-databases`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runDockerImport(list, source, database, dbType, allDatabases)
		},
	}
	cmd.Flags().BoolVar(&list, "list", false, "List Docker database containers and volumes without importing")
	cmd.Flags().StringVar(&source, "source", "", "Docker container name to export from")
	cmd.Flags().StringVar(&database, "database", "", "Database name to import")
	cmd.Flags().StringVar(&dbType, "type", "", "Database type: mysql or postgres (auto-detected from image)")
	cmd.Flags().BoolVar(&allDatabases, "all-databases", false, "Import all user databases")
	return cmd
}

func runDockerImport(list bool, source, database, dbType string, allDatabases bool) error {
	// Verify Docker is available.
	dockerPath, _ := dockerDet.IsDockerInstalled()
	if dockerPath == "" {
		return fmt.Errorf("docker CLI not found in PATH — install Docker to use this command")
	}
	if !dockerDet.IsDaemonRunning() {
		return fmt.Errorf("Docker daemon is not running — start it with: sudo systemctl start docker")
	}

	containers, err := dockerDet.ListRunningContainers()
	if err != nil {
		return fmt.Errorf("listing Docker containers: %w", err)
	}

	dbContainers := dockerDet.FindDatabaseContainers(containers)

	// --list mode: print what's available and exit.
	if list {
		return runDockerImportList(dbContainers)
	}

	// Resolve the source container.
	var selected dockerDet.DatabaseContainer
	switch {
	case source != "":
		found := false
		for _, c := range dbContainers {
			if c.Name == source {
				selected = c
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("no running database container named %q — run: lerd docker:import --list", source)
		}
	case len(dbContainers) == 0:
		fmt.Println("No running Docker database containers found.")
		if vols, err := dockerDet.ListVolumes(); err == nil {
			if dbVols := dockerDet.FindDatabaseVolumes(vols); len(dbVols) > 0 {
				fmt.Println("\nDatabase volumes exist — start the Docker containers first:")
				for _, v := range dbVols {
					fmt.Printf("  %s (%s)\n", v.Name, v.DBType)
				}
			}
		}
		return fmt.Errorf("start your Docker database containers and try again")
	case len(dbContainers) == 1:
		selected = dbContainers[0]
		fmt.Printf("Found Docker container: %s (%s)\n\n", selected.Name, selected.Image)
	default:
		fmt.Println("Multiple Docker database containers found:")
		for i, c := range dbContainers {
			fmt.Printf("  [%d] %s (%s)\n", i+1, c.Name, c.Image)
		}
		fmt.Print("\nSelect container [1]: ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(answer)
		idx := 0
		if answer != "" {
			fmt.Sscanf(answer, "%d", &idx)
			idx--
		}
		if idx < 0 || idx >= len(dbContainers) {
			return fmt.Errorf("invalid selection")
		}
		selected = dbContainers[idx]
	}

	// Override type if specified.
	if dbType != "" {
		switch strings.ToLower(dbType) {
		case "mysql", "mariadb":
			selected.DBType = "mysql"
		case "postgres", "pgsql":
			selected.DBType = "postgres"
		default:
			return fmt.Errorf("unsupported --type %q (use mysql or postgres)", dbType)
		}
	}

	// Discover credentials from the container environment.
	password := discoverDockerDBPassword(selected)

	// List databases in the Docker container.
	databases, err := listDockerDatabases(selected, password)
	if err != nil {
		return fmt.Errorf("listing databases in %s: %w", selected.Name, err)
	}
	if len(databases) == 0 {
		return fmt.Errorf("no user databases found in %s", selected.Name)
	}

	// Select which databases to import.
	var toImport []string
	switch {
	case database != "":
		toImport = []string{database}
	case allDatabases:
		toImport = databases
	default:
		fmt.Println("Available databases:")
		for i, db := range databases {
			fmt.Printf("  [%d] %s\n", i+1, db)
		}
		fmt.Print("\nSelect database(s) (comma-separated, or 'all') [1]: ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(answer)
		if answer == "" {
			answer = "1"
		}
		if strings.ToLower(answer) == "all" {
			toImport = databases
		} else {
			for _, part := range strings.Split(answer, ",") {
				part = strings.TrimSpace(part)
				var idx int
				if _, err := fmt.Sscanf(part, "%d", &idx); err == nil && idx >= 1 && idx <= len(databases) {
					toImport = append(toImport, databases[idx-1])
				} else {
					toImport = append(toImport, part)
				}
			}
		}
	}

	if len(toImport) == 0 {
		return fmt.Errorf("no databases selected")
	}

	// Determine the lerd service name.
	lerdService := selected.DBType
	if lerdService == "" {
		lerdService = "mysql"
	}

	fmt.Printf("\nEnsuring lerd-%s is running...\n", lerdService)
	if err := ensureServiceRunning(lerdService); err != nil {
		return fmt.Errorf("could not start lerd-%s: %w\n\nIf the port is held by Docker, the export/import still works via\n\"docker exec\" and \"podman exec\" — no host port needed.\nTry stopping the Docker container's port mapping and retry.", lerdService, err)
	}

	// Import each database.
	imported := 0
	for _, dbName := range toImport {
		fmt.Printf("\n── %s ──\n", dbName)

		// Export from Docker.
		fmt.Printf("  Exporting from Docker container %s...\n", selected.Name)
		tmpFile, err := os.CreateTemp("", "lerd-docker-import-*.sql")
		if err != nil {
			return fmt.Errorf("creating temp file: %w", err)
		}
		tmpPath := tmpFile.Name()

		exportCmd := dockerExportCmd(selected, password, dbName)
		exportCmd.Stdout = tmpFile
		exportCmd.Stderr = os.Stderr
		if err := exportCmd.Run(); err != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
			fmt.Printf("  WARN: export of %q failed: %v — skipping\n", dbName, err)
			continue
		}
		tmpFile.Close()

		// Check the dump is not empty.
		info, _ := os.Stat(tmpPath)
		if info == nil || info.Size() == 0 {
			os.Remove(tmpPath)
			fmt.Printf("  WARN: export of %q produced empty dump — skipping\n", dbName)
			continue
		}
		fmt.Printf("  Exported %d bytes\n", info.Size())

		// Create database in lerd.
		created, err := createDatabase(lerdService, dbName)
		if err != nil {
			os.Remove(tmpPath)
			fmt.Printf("  WARN: creating database %q: %v — skipping\n", dbName, err)
			continue
		}
		if created {
			fmt.Printf("  Created database %q\n", dbName)
		} else {
			fmt.Printf("  Database %q already exists\n", dbName)
		}

		// Import into lerd.
		fmt.Printf("  Importing into lerd-%s...\n", lerdService)
		importCmd := lerdImportCmd(lerdService, dbName)
		f, err := os.Open(tmpPath)
		if err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("opening temp file: %w", err)
		}
		importCmd.Stdin = f
		importCmd.Stderr = os.Stderr
		err = importCmd.Run()
		f.Close()
		os.Remove(tmpPath)
		if err != nil {
			fmt.Printf("  WARN: import of %q failed: %v — skipping\n", dbName, err)
			continue
		}

		fmt.Printf("  Done\n")
		imported++
	}

	// Summary.
	fmt.Printf("\nImported %d/%d database(s).\n", imported, len(toImport))
	if imported > 0 {
		fmt.Println("\nUpdate your .env to point to lerd services:")
		switch lerdService {
		case "mysql":
			fmt.Println("  DB_CONNECTION=mysql")
			fmt.Println("  DB_HOST=lerd-mysql")
			fmt.Println("  DB_PORT=3306")
			fmt.Println("  DB_USERNAME=root")
			fmt.Println("  DB_PASSWORD=lerd")
		case "postgres":
			fmt.Println("  DB_CONNECTION=pgsql")
			fmt.Println("  DB_HOST=lerd-postgres")
			fmt.Println("  DB_PORT=5432")
			fmt.Println("  DB_USERNAME=postgres")
			fmt.Println("  DB_PASSWORD=lerd")
		}
		fmt.Println("\nOr run: lerd env")
	}
	return nil
}

func runDockerImportList(dbContainers []dockerDet.DatabaseContainer) error {
	if len(dbContainers) > 0 {
		fmt.Println("Running Docker database containers:")
		for _, c := range dbContainers {
			fmt.Printf("  %-30s %-30s %s\n", c.Name, c.Image, c.DBType)
		}
	} else {
		fmt.Println("No running Docker database containers found.")
	}

	vols, err := dockerDet.ListVolumes()
	if err != nil {
		return nil
	}
	dbVols := dockerDet.FindDatabaseVolumes(vols)
	if len(dbVols) > 0 {
		fmt.Println("\nDocker database volumes:")
		for _, v := range dbVols {
			fmt.Printf("  %-40s %s\n", v.Name, v.DBType)
		}
	}
	return nil
}

// discoverDockerDBPassword reads the container environment to find the root
// database password. Falls back to common defaults.
func discoverDockerDBPassword(c dockerDet.DatabaseContainer) string {
	env := dockerDet.ContainerEnv(c.Name)

	switch c.DBType {
	case "mysql":
		if v := env["MYSQL_ROOT_PASSWORD"]; v != "" {
			return v
		}
		if env["MYSQL_ALLOW_EMPTY_PASSWORD"] == "yes" || env["MYSQL_ALLOW_EMPTY_PASSWORD"] == "1" || env["MYSQL_ALLOW_EMPTY_PASSWORD"] == "true" {
			return ""
		}
		if v := env["MARIADB_ROOT_PASSWORD"]; v != "" {
			return v
		}
		if env["MARIADB_ALLOW_EMPTY_ROOT_PASSWORD"] == "yes" || env["MARIADB_ALLOW_EMPTY_ROOT_PASSWORD"] == "1" {
			return ""
		}
	case "postgres":
		if v := env["POSTGRES_PASSWORD"]; v != "" {
			return v
		}
	}

	// Common defaults from Laravel Sail, Laradock, etc.
	return "password"
}

// mysqlSystemDBs are databases to skip when listing user databases.
var mysqlSystemDBs = map[string]bool{
	"mysql": true, "information_schema": true, "performance_schema": true, "sys": true,
}

var postgresSystemDBs = map[string]bool{
	"postgres": true, "template0": true, "template1": true,
}

func listDockerDatabases(c dockerDet.DatabaseContainer, password string) ([]string, error) {
	switch c.DBType {
	case "mysql":
		return listDockerMySQLDatabases(c.Name, password)
	case "postgres":
		return listDockerPostgresDatabases(c.Name, password)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", c.DBType)
	}
}

func listDockerMySQLDatabases(container, password string) ([]string, error) {
	args := []string{"exec", container}
	// Try mariadb client first, fall back to mysql.
	for _, bin := range []string{"mariadb", "mysql"} {
		cmdArgs := append(args, bin, "-uroot")
		if password != "" {
			cmdArgs = append(cmdArgs, "-p"+password)
		}
		cmdArgs = append(cmdArgs, "-sNe", "SHOW DATABASES")
		cmd := exec.Command("docker", cmdArgs...)
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		var dbs []string
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			db := strings.TrimSpace(line)
			if db != "" && !mysqlSystemDBs[db] {
				dbs = append(dbs, db)
			}
		}
		return dbs, nil
	}
	return nil, fmt.Errorf("could not connect to MySQL in container %s — check credentials", container)
}

func listDockerPostgresDatabases(container, password string) ([]string, error) {
	cmd := exec.Command("docker", "exec", "-e", "PGPASSWORD="+password,
		container, "psql", "-U", "postgres", "-lqt")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("could not connect to PostgreSQL in container %s: %w", container, err)
	}
	var dbs []string
	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) < 2 {
			continue
		}
		db := strings.TrimSpace(parts[0])
		if db != "" && !postgresSystemDBs[db] {
			dbs = append(dbs, db)
		}
	}
	return dbs, nil
}

func dockerExportCmd(c dockerDet.DatabaseContainer, password, database string) *exec.Cmd {
	switch c.DBType {
	case "mysql":
		args := []string{"exec", c.Name}
		// Try mysqldump first (works for both mysql and mariadb images).
		args = append(args, "mysqldump", "-uroot")
		if password != "" {
			args = append(args, "-p"+password)
		}
		args = append(args, "--single-transaction", database)
		return exec.Command("docker", args...)
	case "postgres":
		return exec.Command("docker", "exec", "-e", "PGPASSWORD="+password,
			c.Name, "pg_dump", "-U", "postgres", database)
	default:
		return exec.Command("false")
	}
}

func lerdImportCmd(service, database string) *exec.Cmd {
	switch service {
	case "mysql":
		container := "lerd-mysql"
		// MariaDB 11 removed the `mysql` symlink. Use `mariadb` if available.
		if checkOut, err := exec.Command("podman", "exec", container, "which", "mariadb").Output(); err == nil && strings.TrimSpace(string(checkOut)) != "" {
			return exec.Command("podman", "exec", "-i", container,
				"mariadb", "-uroot", "-plerd", database)
		}
		return exec.Command("podman", "exec", "-i", container,
			"mysql", "-uroot", "-plerd", database)
	case "postgres":
		return exec.Command("podman", "exec", "-i", "-e", "PGPASSWORD=lerd",
			"lerd-postgres", "psql", "-U", "postgres", database)
	default:
		return exec.Command("false")
	}
}

// ensureServicesForDockerImport starts the lerd service if not already running.
// This is a thin wrapper around ensureServiceRunning for clarity.
func ensureServicesForDockerImport(service string) error {
	if !podman.QuadletInstalled("lerd-" + service) {
		if err := ensureServiceQuadlet(service); err != nil {
			return err
		}
	}
	return ensureServiceRunning(service)
}
