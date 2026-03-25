package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// NewDbCmd returns the db parent command with import/export/create/shell subcommands.
func NewDbCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Database shortcuts for the current site",
	}
	cmd.AddCommand(newDbImportCmd("import"))
	cmd.AddCommand(newDbExportCmd("export"))
	cmd.AddCommand(newDbCreateCmd("create"))
	cmd.AddCommand(newDbShellCmd("shell"))
	return cmd
}

// NewDbImportCmd returns the standalone db:import command.
func NewDbImportCmd() *cobra.Command { return newDbImportCmd("db:import") }

// NewDbExportCmd returns the standalone db:export command.
func NewDbExportCmd() *cobra.Command { return newDbExportCmd("db:export") }

// NewDbCreateCmd returns the standalone db:create command.
func NewDbCreateCmd() *cobra.Command { return newDbCreateCmd("db:create") }

// NewDbShellCmd returns the standalone db:shell command.
func NewDbShellCmd() *cobra.Command { return newDbShellCmd("db:shell") }

func newDbImportCmd(use string) *cobra.Command {
	var database string
	cmd := &cobra.Command{
		Use:   use + " <file.sql>",
		Short: "Import a SQL dump into a database (default: site DB from .env)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runDbImport(args[0], database)
		},
	}
	cmd.Flags().StringVarP(&database, "database", "d", "", "Database name (default: DB_DATABASE from .env)")
	return cmd
}

func newDbExportCmd(use string) *cobra.Command {
	var output, database string
	cmd := &cobra.Command{
		Use:   use,
		Short: "Export a database to a SQL dump (default: site DB from .env)",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runDbExport(output, database)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file (default: <database>.sql)")
	cmd.Flags().StringVarP(&database, "database", "d", "", "Database name (default: DB_DATABASE from .env)")
	return cmd
}

type dbEnv struct {
	connection string
	database   string
	username   string
	password   string
}

func loadDBEnv(cwd string) (*dbEnv, error) {
	envPath := filepath.Join(cwd, ".env")
	f, err := os.Open(envPath)
	if err != nil {
		return nil, fmt.Errorf("no .env found in %s", cwd)
	}
	defer f.Close()

	vals := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		k, v, _ := strings.Cut(line, "=")
		vals[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	conn := vals["DB_CONNECTION"]
	if conn == "" {
		return nil, fmt.Errorf("DB_CONNECTION not set in .env")
	}
	db := vals["DB_DATABASE"]
	if db == "" {
		return nil, fmt.Errorf("DB_DATABASE not set in .env")
	}
	return &dbEnv{
		connection: conn,
		database:   db,
		username:   vals["DB_USERNAME"],
		password:   vals["DB_PASSWORD"],
	}, nil
}

func runDbImport(file, database string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	env, err := loadDBEnv(cwd)
	if err != nil {
		return err
	}
	if database != "" {
		env.database = database
	}

	ensureServicesForCwd(cwd)

	f, err := os.Open(file)
	if err != nil {
		return fmt.Errorf("opening %s: %w", file, err)
	}
	defer f.Close()

	cmd, err := dbImportCmd(env)
	if err != nil {
		return err
	}
	cmd.Stdin = f
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	fmt.Printf("Importing %s into %s (%s)...\n", file, env.database, env.connection)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("import failed: %w", err)
	}
	fmt.Println("Import complete.")
	return nil
}

func dbImportCmd(env *dbEnv) (*exec.Cmd, error) {
	switch env.connection {
	case "mysql", "mariadb":
		return exec.Command("podman", "exec", "-i", "lerd-mysql",
			"mysql", "-u"+env.username, "-p"+env.password, env.database), nil
	case "pgsql", "postgres":
		return exec.Command("podman", "exec", "-i", "-e", "PGPASSWORD="+env.password,
			"lerd-postgres", "psql", "-U", env.username, env.database), nil
	default:
		return nil, fmt.Errorf("unsupported DB_CONNECTION: %q (supported: mysql, pgsql)", env.connection)
	}
}

func runDbExport(output, database string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	env, err := loadDBEnv(cwd)
	if err != nil {
		return err
	}
	if database != "" {
		env.database = database
	}

	ensureServicesForCwd(cwd)

	if output == "" {
		output = env.database + ".sql"
	}

	f, err := os.Create(output)
	if err != nil {
		return fmt.Errorf("creating %s: %w", output, err)
	}
	defer f.Close()

	cmd, err := dbExportCmd(env)
	if err != nil {
		_ = os.Remove(output)
		return err
	}
	cmd.Stdout = f
	cmd.Stderr = os.Stderr

	fmt.Printf("Exporting %s (%s) to %s...\n", env.database, env.connection, output)
	if err := cmd.Run(); err != nil {
		_ = os.Remove(output)
		return fmt.Errorf("export failed: %w", err)
	}
	fmt.Printf("Export complete: %s\n", output)
	return nil
}

func dbExportCmd(env *dbEnv) (*exec.Cmd, error) {
	switch env.connection {
	case "mysql", "mariadb":
		return exec.Command("podman", "exec", "-i", "lerd-mysql",
			"mysqldump", "-u"+env.username, "-p"+env.password, env.database), nil
	case "pgsql", "postgres":
		return exec.Command("podman", "exec", "-i", "-e", "PGPASSWORD="+env.password,
			"lerd-postgres", "pg_dump", "-U", env.username, env.database), nil
	default:
		return nil, fmt.Errorf("unsupported DB_CONNECTION: %q (supported: mysql, pgsql)", env.connection)
	}
}

func newDbCreateCmd(use string) *cobra.Command {
	return &cobra.Command{
		Use:   use + " [name]",
		Short: "Create a database (and testing database) for the current project",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runDbCreate(args)
		},
	}
}

func runDbCreate(args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	env, _ := loadDBEnvLenient(cwd)

	var dbName string
	switch {
	case len(args) > 0:
		dbName = args[0]
	case env != nil && env.database != "":
		dbName = env.database
	default:
		dbName = projectDBName(cwd)
	}

	conn := "mysql"
	if env != nil && env.connection != "" {
		conn = env.connection
	}
	svc := connToService(conn)

	if err := ensureServiceRunning(svc); err != nil {
		return fmt.Errorf("could not start %s: %w", svc, err)
	}

	for _, name := range []string{dbName, dbName + "_testing"} {
		created, err := createDatabase(svc, name)
		if err != nil {
			return fmt.Errorf("creating %q: %w", name, err)
		}
		if created {
			fmt.Printf("Created database %q\n", name)
		} else {
			fmt.Printf("Database %q already exists\n", name)
		}
	}
	return nil
}

func newDbShellCmd(use string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: "Open an interactive database shell for the current project",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runDbShell()
		},
	}
}

func runDbShell() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	conn := "mysql"
	var dbName string
	if env, _ := loadDBEnvLenient(cwd); env != nil {
		conn = env.connection
		dbName = env.database
	}

	ensureServicesForCwd(cwd)

	var cmd *exec.Cmd
	switch conn {
	case "pgsql", "postgres":
		cmdArgs := []string{"exec", "--tty", "-i", "lerd-postgres", "psql", "-U", "postgres"}
		if dbName != "" {
			cmdArgs = append(cmdArgs, dbName)
		}
		cmd = exec.Command("podman", cmdArgs...)
	default:
		cmdArgs := []string{"exec", "--tty", "-i", "lerd-mysql", "mysql", "-uroot", "-plerd"}
		if dbName != "" {
			cmdArgs = append(cmdArgs, dbName)
		}
		cmd = exec.Command("podman", cmdArgs...)
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// connToService maps a DB_CONNECTION value to the lerd service name.
func connToService(conn string) string {
	switch strings.ToLower(conn) {
	case "pgsql", "postgres":
		return "postgres"
	default:
		return "mysql"
	}
}

// loadDBEnvLenient reads DB connection info from .env without requiring DB_DATABASE.
func loadDBEnvLenient(cwd string) (*dbEnv, error) {
	envPath := filepath.Join(cwd, ".env")
	f, err := os.Open(envPath)
	if err != nil {
		return nil, fmt.Errorf("no .env found in %s", cwd)
	}
	defer f.Close()

	vals := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		k, v, _ := strings.Cut(line, "=")
		vals[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return &dbEnv{
		connection: vals["DB_CONNECTION"],
		database:   vals["DB_DATABASE"],
		username:   vals["DB_USERNAME"],
		password:   vals["DB_PASSWORD"],
	}, nil
}
