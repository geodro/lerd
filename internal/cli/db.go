package cli

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
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
	var database, service string
	cmd := &cobra.Command{
		Use:   use + " <file.sql>",
		Short: "Import a SQL dump into a database (default: site DB from .env)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runDbImport(args[0], service, database)
		},
	}
	cmd.Flags().StringVarP(&database, "database", "d", "", "Database name (default: from .env or .lerd.yaml)")
	cmd.Flags().StringVarP(&service, "service", "s", "", "Lerd DB service to target (e.g. mysql, postgres)")
	return cmd
}

func newDbExportCmd(use string) *cobra.Command {
	var output, database, service string
	cmd := &cobra.Command{
		Use:   use,
		Short: "Export a database to a SQL dump (default: site DB from .env)",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runDbExport(output, service, database)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file (default: <database>.sql)")
	cmd.Flags().StringVarP(&database, "database", "d", "", "Database name (default: from .env or .lerd.yaml)")
	cmd.Flags().StringVarP(&service, "service", "s", "", "Lerd DB service to target (e.g. mysql, postgres)")
	return cmd
}

type dbEnv struct {
	service    string // lerd service name → container "lerd-<service>"
	connection string // dialect: "mysql"/"mariadb"/"pgsql"/"postgres"
	database   string
	username   string
	password   string
}

// serviceToDBEnv returns dialect and default credentials for a given lerd service name.
// The family is inferred from the service name prefix.
func serviceToDBEnv(name string) *dbEnv {
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, "postgres") || lower == "pgsql" {
		return &dbEnv{service: name, connection: "pgsql", username: "postgres", password: "lerd"}
	}
	// mysql, mariadb, mysql-5-7, etc.
	return &dbEnv{service: name, connection: "mysql", username: "root", password: "lerd"}
}

// resolveDB resolves database connection config using the following priority:
//  1. --service flag (flagService)
//  2. .lerd.yaml db: block (present even on unlinked sites)
//  3. Framework definition service detection (uses framework-specific env file + detect rules)
//  4. .env file with generic key inference (DB_CONNECTION, DB_TYPE, DATABASE_URL, DB_PORT…)
//  5. Error with instructions
//
// flagDatabase overrides the database name at any level.
func resolveDB(cwd, flagService, flagDatabase string) (*dbEnv, error) {
	var env *dbEnv

	switch {
	case flagService != "":
		env = serviceToDBEnv(flagService)

	default:
		// Try .lerd.yaml db: block
		if pc, err := config.LoadProjectConfig(cwd); err == nil && pc.DB.Service != "" {
			env = serviceToDBEnv(pc.DB.Service)
			if pc.DB.Database != "" {
				env.database = pc.DB.Database
			}
			break
		}
		// Try framework definition (reads the framework's own env file with its detect rules)
		if env = resolveDBFromFramework(cwd); env != nil {
			break
		}
		// Fall back to .env with generic key inference
		loaded, err := loadDBEnv(cwd)
		if err != nil {
			return nil, fmt.Errorf(
				"no DB config found — use --service <name>, add a db: block to .lerd.yaml, or create a .env file with DB_CONNECTION and DB_DATABASE",
			)
		}
		env = loaded
	}

	if flagDatabase != "" {
		env.database = flagDatabase
	}
	return env, nil
}

// resolveDBFromFramework detects the framework for cwd and uses its service
// detection rules to identify which DB service is in use and what database name
// to connect to. Returns nil when no framework is detected or no DB service matches.
func resolveDBFromFramework(cwd string) *dbEnv {
	fwName, ok := config.DetectFrameworkForDir(cwd)
	if !ok {
		return nil
	}
	fw, ok := config.GetFramework(fwName)
	if !ok || len(fw.Env.Services) == 0 {
		return nil
	}

	envRelPath, envFormat := fw.Env.Resolve(cwd)
	readKey := makeEnvReader(filepath.Join(cwd, envRelPath), envFormat)

	// Build a map of only the keys referenced in detect rules (avoid reading the whole file).
	detectKeys := map[string]string{}
	for _, def := range fw.Env.Services {
		for _, rule := range def.Detect {
			if _, seen := detectKeys[rule.Key]; !seen {
				detectKeys[rule.Key] = readKey(rule.Key)
			}
		}
	}

	// Check known DB services in priority order.
	for _, svc := range []string{"mysql", "postgres"} {
		def, ok := fw.Env.Services[svc]
		if !ok || !frameworkServiceDetected(def, detectKeys) {
			continue
		}
		env := serviceToDBEnv(svc)
		// Resolve database name from the framework's env file.
		env.database = readKey("DB_DATABASE")
		if env.database == "" {
			if urlEnv := parseDBURL(readKey("DATABASE_URL")); urlEnv != nil {
				env.database = urlEnv.database
			}
		}
		return env
	}
	return nil
}

// resolveDBLenient is like resolveDB but does not require a database name to be
// present (used by db:shell and db:create where it is optional).
func resolveDBLenient(cwd, flagService, flagDatabase string) (*dbEnv, error) {
	var env *dbEnv

	switch {
	case flagService != "":
		env = serviceToDBEnv(flagService)

	default:
		if pc, err := config.LoadProjectConfig(cwd); err == nil && pc.DB.Service != "" {
			env = serviceToDBEnv(pc.DB.Service)
			if pc.DB.Database != "" {
				env.database = pc.DB.Database
			}
			break
		}
		if env = resolveDBFromFramework(cwd); env != nil {
			break
		}
		loaded, _ := loadDBEnvLenient(cwd)
		if loaded != nil {
			env = loaded
		} else {
			env = serviceToDBEnv("mysql") // default to mysql when nothing is configured
		}
	}

	if flagDatabase != "" {
		env.database = flagDatabase
	}
	return env, nil
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

	urlEnv := parseDBURL(vals["DATABASE_URL"])

	conn := inferDBConnection(vals)
	if conn == "" {
		return nil, fmt.Errorf("cannot determine DB type from .env — set DB_CONNECTION, DB_TYPE, TYPEORM_CONNECTION, DATABASE_URL, or DB_PORT")
	}

	db := vals["DB_DATABASE"]
	if db == "" {
		db = vals["TYPEORM_DATABASE"]
	}
	if db == "" && urlEnv != nil {
		db = urlEnv.database
	}
	if db == "" {
		return nil, fmt.Errorf("DB_DATABASE not set in .env")
	}

	// Credentials: use individual vars when present; fall back to lerd service defaults.
	// We do not use credentials from DATABASE_URL — lerd connects via podman exec
	// using the container's fixed root/lerd credentials.
	svcDefaults := serviceToDBEnv(connToService(conn))
	username := vals["DB_USERNAME"]
	if username == "" {
		username = svcDefaults.username
	}
	password := vals["DB_PASSWORD"]
	if password == "" {
		password = svcDefaults.password
	}

	return &dbEnv{
		service:    connToService(conn),
		connection: conn,
		database:   db,
		username:   username,
		password:   password,
	}, nil
}

func runDbImport(file, service, database string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	env, err := resolveDB(cwd, service, database)
	if err != nil {
		return err
	}

	if err := ensureServiceRunning(env.service); err != nil {
		return fmt.Errorf("could not start %s: %w", env.service, err)
	}

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
	container := "lerd-" + env.service
	switch env.connection {
	case "mysql", "mariadb":
		return podman.Cmd("exec", "-i",
			"-e", "MYSQL_PWD="+env.password,
			container,
			"mysql", "-u"+env.username, env.database), nil
	case "pgsql", "postgres":
		return podman.Cmd("exec", "-i", "-e", "PGPASSWORD="+env.password,
			container, "psql", "-U", env.username, env.database), nil
	default:
		return nil, fmt.Errorf("unsupported DB_CONNECTION: %q (supported: mysql, pgsql)", env.connection)
	}
}

func runDbExport(output, service, database string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	env, err := resolveDB(cwd, service, database)
	if err != nil {
		return err
	}

	if err := ensureServiceRunning(env.service); err != nil {
		return fmt.Errorf("could not start %s: %w", env.service, err)
	}

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
	container := "lerd-" + env.service
	switch env.connection {
	case "mysql", "mariadb":
		return podman.Cmd("exec", "-i",
			"-e", "MYSQL_PWD="+env.password,
			container,
			"mysqldump", "-u"+env.username, env.database), nil
	case "pgsql", "postgres":
		return podman.Cmd("exec", "-i", "-e", "PGPASSWORD="+env.password,
			container, "pg_dump", "-U", env.username, env.database), nil
	default:
		return nil, fmt.Errorf("unsupported DB_CONNECTION: %q (supported: mysql, pgsql)", env.connection)
	}
}

func newDbCreateCmd(use string) *cobra.Command {
	var service string
	cmd := &cobra.Command{
		Use:   use + " [name]",
		Short: "Create a database (and testing database) for the current project",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runDbCreate(service, args)
		},
	}
	cmd.Flags().StringVarP(&service, "service", "s", "", "Lerd DB service to target (e.g. mysql, postgres)")
	return cmd
}

func runDbCreate(flagService string, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	env, err := resolveDBLenient(cwd, flagService, "")
	if err != nil {
		return err
	}

	var dbName string
	switch {
	case len(args) > 0:
		dbName = args[0]
	case env.database != "":
		dbName = env.database
	default:
		dbName = projectDBName(cwd)
	}

	if err := ensureServiceRunning(env.service); err != nil {
		return fmt.Errorf("could not start %s: %w", env.service, err)
	}

	for _, name := range []string{dbName, dbName + "_testing"} {
		created, err := createDatabase(env.service, name)
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
	var service, database string
	cmd := &cobra.Command{
		Use:   use,
		Short: "Open an interactive database shell for the current project",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runDbShell(service, database)
		},
	}
	cmd.Flags().StringVarP(&service, "service", "s", "", "Lerd DB service to target (e.g. mysql, postgres)")
	cmd.Flags().StringVarP(&database, "database", "d", "", "Database to connect to")
	return cmd
}

func runDbShell(flagService, flagDatabase string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	env, err := resolveDBLenient(cwd, flagService, flagDatabase)
	if err != nil {
		return err
	}

	if err := ensureServiceRunning(env.service); err != nil {
		return fmt.Errorf("could not start %s: %w", env.service, err)
	}

	container := "lerd-" + env.service
	var cmd *exec.Cmd
	switch env.connection {
	case "pgsql", "postgres":
		cmdArgs := []string{"exec", "--tty", "-i", container, "psql", "-U", env.username}
		if env.database != "" {
			cmdArgs = append(cmdArgs, env.database)
		}
		cmd = podman.Cmd(cmdArgs...)
	default:
		cmdArgs := []string{"exec", "--tty", "-i", container, "mysql", "-u" + env.username, "-p" + env.password}
		if env.database != "" {
			cmdArgs = append(cmdArgs, env.database)
		}
		cmd = podman.Cmd(cmdArgs...)
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

// inferDBConnection resolves the database dialect from a parsed .env map.
// Checks in priority order: DB_CONNECTION → DB_TYPE → TYPEORM_CONNECTION → DATABASE_URL → DB_PORT.
func inferDBConnection(vals map[string]string) string {
	for _, key := range []string{"DB_CONNECTION", "DB_TYPE", "TYPEORM_CONNECTION"} {
		if v := vals[key]; v != "" {
			return v
		}
	}
	if u := parseDBURL(vals["DATABASE_URL"]); u != nil {
		return u.connection
	}
	switch vals["DB_PORT"] {
	case "5432":
		return "pgsql"
	case "3306", "3307":
		return "mysql"
	}
	return ""
}

// parseDBURL parses a DATABASE_URL connection string and returns the dialect and
// database name. Supports postgresql://, postgres://, mysql://, mysql2://, mariadb://.
// Credentials from the URL are intentionally ignored — lerd always connects via
// podman exec using the container's fixed credentials.
func parseDBURL(rawURL string) *dbEnv {
	if rawURL == "" {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return nil
	}
	var conn string
	switch strings.ToLower(u.Scheme) {
	case "postgresql", "postgres":
		conn = "pgsql"
	case "mysql", "mysql2", "mariadb":
		conn = "mysql"
	default:
		return nil
	}
	db := strings.TrimPrefix(u.Path, "/")
	// Strip Prisma-style query params (e.g. ?schema=public)
	if i := strings.IndexByte(db, '?'); i >= 0 {
		db = db[:i]
	}
	return &dbEnv{
		service:    connToService(conn),
		connection: conn,
		database:   db,
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

	urlEnv := parseDBURL(vals["DATABASE_URL"])

	conn := inferDBConnection(vals)

	db := vals["DB_DATABASE"]
	if db == "" {
		db = vals["TYPEORM_DATABASE"]
	}
	if db == "" && urlEnv != nil {
		db = urlEnv.database
	}

	svcDefaults := serviceToDBEnv(connToService(conn))
	username := vals["DB_USERNAME"]
	if username == "" {
		username = svcDefaults.username
	}
	password := vals["DB_PASSWORD"]
	if password == "" {
		password = svcDefaults.password
	}

	return &dbEnv{
		service:    connToService(conn),
		connection: conn,
		database:   db,
		username:   username,
		password:   password,
	}, nil
}
