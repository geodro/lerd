package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/podman"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// NewImportCmd returns the import parent command with source subcommands.
func NewImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import data from other environments",
	}
	cmd.AddCommand(newImportSailCmd("sail"))
	return cmd
}

// NewSailCmd returns a top-level `lerd sail` command. Registering it as a
// known cobra command prevents the vendor-bin dispatcher from intercepting
// `lerd sail import`. The `import` subcommand is handled by lerd; every other
// argument is passed through to vendor/bin/sail so existing Sail workflows
// (lerd sail up, lerd sail artisan migrate, …) continue to work.
func NewSailCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sail",
		Short: "Sail import shortcut; other args are passed to vendor/bin/sail",
		// DisableFlagParsing lets flags like `lerd sail up -d` pass through
		// to vendor/bin/sail unchanged. The import subcommand is found by
		// cobra's traversal before flag parsing runs, so its own flags work.
		DisableFlagParsing: true,
		SilenceUsage:       true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// DisableFlagParsing suppresses cobra's --help interception, so
			// handle it here before falling through to vendor/bin/sail.
			for _, a := range args {
				if a == "--help" || a == "-h" {
					return cmd.Help()
				}
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			return RunPHP(cwd, append([]string{"vendor/bin/sail"}, args...))
		},
	}
	cmd.AddCommand(newImportSailCmd("import"))
	return cmd
}

func newImportSailCmd(use string) *cobra.Command {
	var noStop, skipS3 bool
	var sailDBUser, sailDBPassword, sailDBName string
	var sailDBNameExplicit bool
	cmd := &cobra.Command{
		Use:   use,
		Short: "Import database (and S3 files) from a Laravel Sail project",
		Long: `Imports the database and optionally S3/MinIO storage from a Laravel Sail
Docker Compose project into lerd's running services.

Steps:
  1. Detects Sail port conflicts and remaps them to avoid clashing with lerd
  2. Starts Sail with the remapped ports
  3. Dumps the database from the Sail container and imports it into lerd
  4. Mirrors MinIO bucket into lerd's RustFS (if S3 is configured)
  5. Stops Sail (unless --no-stop is passed)

If lerd setup has already run, .env will contain lerd credentials. Use the
--sail-db-* flags to supply the original Sail database credentials instead.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			sailDBNameExplicit = cmd.Flags().Changed("sail-db-name")
			userExplicit := cmd.Flags().Changed("sail-db-user")
			passExplicit := cmd.Flags().Changed("sail-db-password")
			return runImportSail(noStop, skipS3, sailDBUser, sailDBPassword, sailDBName, sailDBNameExplicit, userExplicit, passExplicit)
		},
	}
	cmd.Flags().BoolVar(&noStop, "no-stop", false, "Leave Sail running after import is complete")
	cmd.Flags().BoolVar(&skipS3, "skip-s3", false, "Skip S3/MinIO storage import")
	cmd.Flags().StringVar(&sailDBUser, "sail-db-user", "sail", "Database username in the Sail environment (default \"sail\")")
	cmd.Flags().StringVar(&sailDBPassword, "sail-db-password", "password", "Database password in the Sail environment (default \"password\")")
	cmd.Flags().StringVar(&sailDBName, "sail-db-name", "", "Database name in the Sail environment (default: DB_DATABASE from .env)")
	return cmd
}

// lerdConflictPorts is the set of host ports that lerd occupies by default.
var lerdConflictPorts = map[int]bool{
	80:   true,
	443:  true,
	3306: true,
	5432: true,
	6379: true,
	7073: true,
	7700: true,
	8025: true,
	9000: true,
	9001: true,
}

const sailImportPortOffset = 20000

// sailComposeFile is a minimal docker-compose struct for port-remap purposes.
type sailComposeFile struct {
	Services map[string]sailComposeService `yaml:"services"`
}

type sailComposeService struct {
	Ports       []interface{}     `yaml:"ports"` // string or map[string]any
	Image       string            `yaml:"image"`
	Environment map[string]string `yaml:"environment"`
}

// sailS3Env holds S3-related env vars read from the Sail project's .env.
type sailS3Env struct {
	accessKey string
	secretKey string
	bucket    string
}

func runImportSail(noStop, skipS3 bool, sailDBUser, sailDBPassword, sailDBName string, sailDBNameExplicit, sailDBUserExplicit, sailDBPasswordExplicit bool) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	// --- Validate ---
	composeFilePath, err := sailFindComposeFile(cwd)
	if err != nil {
		return err
	}
	composeBin, err := sailDetectComposeRuntime()
	if err != nil {
		return err
	}
	// Warn (non-fatal) if vendor/laravel/sail is absent.
	if _, sailErr := os.Stat(filepath.Join(cwd, "vendor", "laravel", "sail")); os.IsNotExist(sailErr) {
		if _, sailErr2 := os.Stat(filepath.Join(cwd, "sail")); os.IsNotExist(sailErr2) {
			fmt.Println("Warning: vendor/laravel/sail not found — proceeding anyway.")
		}
	}

	// --- Read .env for lerd-side values ---
	// After `lerd setup`, .env contains lerd's credentials (DB_HOST=lerd-mysql,
	// DB_PASSWORD=lerd, etc.) which are correct for importing INTO lerd but wrong
	// for dumping FROM Sail. We keep these as the lerd import target.
	lerdEnv, err := loadDBEnv(cwd)
	if err != nil {
		return fmt.Errorf("reading .env: %w", err)
	}

	// --- Build Sail-side credentials for the dump ---
	// The MySQL container in Sail is configured from .env via MYSQL_USER / MYSQL_PASSWORD /
	// MYSQL_ROOT_PASSWORD environment variables.  If lerd has already overwritten .env (after
	// `lerd setup`), the container will have been started with the lerd credentials, so we
	// default to whatever DB_USERNAME / DB_PASSWORD are currently in .env.  The --sail-db-*
	// flags let the user override when the credentials differ from what's in .env.
	sailEnv := &dbEnv{
		connection: lerdEnv.connection,
		database:   sailDBName,
		username:   sailDBUser,
		password:   sailDBPassword,
	}
	if sailEnv.database == "" {
		sailEnv.database = lerdEnv.database
		// Warn when DB_DATABASE looks like lerd already overwrote it.
		if lerdEnv.database == "lerd" {
			fmt.Println("Warning: DB_DATABASE is 'lerd' — lerd may have already overwritten your .env.")
			fmt.Println("  If your Sail database had a different name, pass --sail-db-name <name>.")
		}
	}

	rawEnv := sailReadRawEnv(cwd)
	s3 := sailDetectS3(rawEnv)

	// --- Build a single resolved+modified compose file ---
	// We write one complete file rather than a base + override because Docker
	// Compose v2 merges `ports:` lists additively — `ports: []` in an override
	// does NOT clear the base file's ports.
	compose, tempComposePath, portRemap, strippedSvcs, err := sailBuildTempCompose(composeFilePath, cwd, composeBin)
	if err != nil {
		return fmt.Errorf("preparing compose: %w", err)
	}
	defer os.Remove(tempComposePath)

	// --project-directory preserves container naming (recruitirelandcom-mysql-1
	// etc.) even though we're passing a path to a temp file.
	composeArgs := []string{"compose", "--project-directory", cwd, "-f", tempComposePath}

	if len(strippedSvcs) > 0 {
		fmt.Printf("Stripping ports from app services: %s\n", strings.Join(strippedSvcs, ", "))
	}
	if len(portRemap) > 0 {
		fmt.Println("Remapping conflicting ports for Sail:")
		for orig, remapped := range portRemap {
			fmt.Printf("  %-5d → %d\n", orig, remapped)
		}
	}

	// Resolve only the services we need (DB + MinIO if S3) so Sail's app
	// container is never built — we only need data volumes for the import.
	dbService := sailFindDBService(compose, sailEnv.connection)
	if dbService == "" {
		return fmt.Errorf("could not detect a database service in docker-compose for connection %q", sailEnv.connection)
	}
	// Prefer credentials from the compose service environment over .env, since
	// the container is started with whatever is in the compose file (which may
	// substitute ${DB_USERNAME:-default} from the original Sail .env).
	if detectedUser, detectedPass := sailDetectDBCreds(compose, dbService); detectedUser != "" {
		if !sailDBUserExplicit {
			sailEnv.username = detectedUser
		}
		if !sailDBPasswordExplicit && detectedPass != "" {
			sailEnv.password = detectedPass
		}
	}
	servicesToStart := []string{dbService}

	var minioSvc, minioUser, minioPass string
	var minioPort int
	if !skipS3 && s3 != nil {
		minioSvc, minioPort, minioUser, minioPass = sailFindMinio(compose, portRemap)
		if minioSvc != "" {
			servicesToStart = append(servicesToStart, minioSvc)
		}
	}

	// mysql/mysql-server:8.0 stores its socket in /var/lib/mysql (persistent),
	// so a prior unclean shutdown leaves mysql.sock.lock and mysqld refuses to
	// start. Clean it via a throwaway container that mounts the same volume.
	if isMysqlLikeService(compose, dbService) {
		cleanArgs := append([]string{}, composeArgs...)
		cleanArgs = append(cleanArgs, "run", "--rm", "--no-deps", "--entrypoint", "sh",
			dbService, "-c",
			"rm -f /var/lib/mysql/*.sock /var/lib/mysql/*.sock.lock /var/lib/mysql/*.pid 2>/dev/null || true")
		cleanCmd := exec.Command(composeBin, cleanArgs...)
		// Silence noisy output from the throwaway container.
		_ = cleanCmd.Run()
	}

	// --force-recreate guarantees a clean container even if a previous run
	// left one behind. Volumes are preserved.
	fmt.Printf("Starting Sail services: %s\n", strings.Join(servicesToStart, ", "))
	upArgs := append(composeArgs, "up", "-d", "--no-deps", "--force-recreate")
	upArgs = append(upArgs, servicesToStart...)
	upCmd := exec.Command(composeBin, upArgs...)
	upCmd.Stdout = os.Stdout
	upCmd.Stderr = os.Stderr
	if err := upCmd.Run(); err != nil {
		return fmt.Errorf("sail up: %w", err)
	}

	// Tear Sail down when we're done (deferred so it runs even on error).
	if !noStop {
		defer func() {
			fmt.Println("Stopping Sail...")
			downArgs := append(composeArgs, "down")
			downCmd := exec.Command(composeBin, downArgs...)
			downCmd.Stdout = os.Stdout
			downCmd.Stderr = os.Stderr
			_ = downCmd.Run()
		}()
	}

	// --- Wait for DB readiness ---
	fmt.Printf("Waiting for Sail %s to be ready...\n", dbService)
	if err := sailWaitDB(composeArgs, dbService, sailEnv, composeBin); err != nil {
		return fmt.Errorf("Sail DB not ready: %w", err)
	}

	// Auto-detect the Sail database name when no --sail-db-name is passed, since
	// lerd setup may have overwritten DB_DATABASE in .env.
	if !sailDBNameExplicit {
		if detected, err := sailDetectDatabase(composeArgs, dbService, sailEnv, composeBin); err == nil && detected != "" {
			sailEnv.database = detected
		}
	}

	// Count tables before dump: refuse to wipe lerd's DB with an empty or
	// missing Sail database, and prompt the user for confirmation.
	tableCount, err := sailCountTables(composeArgs, dbService, sailEnv, composeBin)
	if err != nil {
		return fmt.Errorf("inspecting Sail database %q: %w", sailEnv.database, err)
	}
	if tableCount == 0 {
		fmt.Printf("Sail database %q has no tables — refusing to overwrite lerd DB with empty data.\n", sailEnv.database)
		fmt.Println("Skipping database import.")
	} else {
		fmt.Printf("Found database %q with %d tables. ", sailEnv.database, tableCount)
		if !promptConfirm(fmt.Sprintf("Import into lerd (will overwrite %q)?", lerdEnv.database)) {
			fmt.Println("Database import skipped.")
		} else {
			fmt.Printf("Dumping database %q from Sail...\n", sailEnv.database)
			dumpFile, err := sailDumpDB(composeArgs, dbService, sailEnv, composeBin)
			if err != nil {
				return fmt.Errorf("dumping Sail DB: %w", err)
			}
			defer os.Remove(dumpFile)

			fmt.Printf("Importing into lerd (%s / %s)...\n", lerdEnv.connection, lerdEnv.database)
			if err := ensureServiceRunning(connToService(lerdEnv.connection)); err != nil {
				return fmt.Errorf("starting lerd DB service: %w", err)
			}
			if err := sailRecreateDB(lerdEnv); err != nil {
				return fmt.Errorf("recreating database: %w", err)
			}
			if err := sailImportDump(dumpFile, lerdEnv); err != nil {
				return fmt.Errorf("importing dump: %w", err)
			}
			fmt.Println("Database imported.")
		}
	}

	// --- S3 import ---
	if !skipS3 && s3 != nil {
		if minioSvc != "" {
			// Credentials for Sail's MinIO come from the compose environment block
			// (MINIO_ROOT_USER / MINIO_ROOT_PASSWORD), NOT from .env's AWS_ACCESS_KEY_ID
			// / AWS_SECRET_ACCESS_KEY — lerd setup may have overwritten those.
			s3.accessKey = minioUser
			s3.secretKey = minioPass
			fmt.Println("Importing S3/MinIO files into lerd RustFS...")
			if err := sailImportS3(s3, minioPort, lerdEnv.database); err != nil {
				fmt.Printf("  Warning: S3 import failed: %v\n", err)
				fmt.Println("  Re-run with --skip-s3 to skip this step.")
			} else {
				fmt.Println("S3 import complete.")
			}
		} else {
			fmt.Println("No MinIO service found in docker-compose — skipping S3 import.")
		}
	}

	fmt.Println("\nImport complete.")
	return nil
}

// sailDetectComposeRuntime finds the first available compose runtime and returns
// the binary name ("docker" or "podman").  It checks both so that users who run
// Laravel Sail via Podman Compose are supported without any extra configuration.
func sailDetectComposeRuntime() (string, error) {
	for _, bin := range []string{"docker", "podman"} {
		if _, err := exec.LookPath(bin); err != nil {
			continue
		}
		// Verify the compose plugin / subcommand is available and the runtime is
		// operational (Docker daemon running, Podman socket active, etc.).
		if err := exec.Command(bin, "compose", "version").Run(); err == nil {
			return bin, nil
		}
	}
	return "", fmt.Errorf("neither 'docker compose' nor 'podman compose' found\n" +
		"Install Docker Desktop, Podman Desktop, or the docker-compose / podman-compose plugin and try again.")
}

// sailFindComposeFile returns the path to docker-compose.yml or docker-compose.yaml.
func sailFindComposeFile(dir string) (string, error) {
	for _, name := range []string{"docker-compose.yml", "docker-compose.yaml"} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no docker-compose.yml or docker-compose.yaml found in %s", dir)
}

// sailReadRawEnv parses all key=value pairs from .env (or .env.before_lerd when
// it exists) into a map.  .env.before_lerd is preferred because it preserves the
// original Sail credentials before `lerd env` overwrites them.
func sailReadRawEnv(dir string) map[string]string {
	vals := map[string]string{}
	// Prefer the pre-lerd backup so we pick up the original Sail S3/bucket config.
	candidates := []string{".env.before_lerd", ".env"}
	var f *os.File
	for _, name := range candidates {
		var err error
		f, err = os.Open(filepath.Join(dir, name))
		if err == nil {
			break
		}
	}
	if f == nil {
		return vals
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		k, v, _ := strings.Cut(line, "=")
		vals[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	return vals
}

// sailDetectS3 returns a sailS3Env if .env indicates S3 storage is in use.
func sailDetectS3(env map[string]string) *sailS3Env {
	_, hasEndpoint := env["AWS_ENDPOINT"]
	if strings.ToLower(env["FILESYSTEM_DISK"]) != "s3" && !hasEndpoint {
		return nil
	}
	bucket := env["AWS_BUCKET"]
	if bucket == "" {
		bucket = "local"
	}
	return &sailS3Env{
		accessKey: env["AWS_ACCESS_KEY_ID"],
		secretKey: env["AWS_SECRET_ACCESS_KEY"],
		bucket:    bucket,
	}
}

// sailBuildTempCompose resolves the docker-compose file, strips ports from
// non-data services, remaps conflicting data-service ports, and writes the
// result to a single self-contained temp file. Returning a single file avoids
// Docker Compose v2's additive list-merge behaviour where `ports: []` in an
// override does not clear the base file's ports.
//
// Returns: typed compose struct (for service detection), temp file path,
// port remap table, list of services whose ports were stripped.
func sailBuildTempCompose(composeFilePath, cwd, composeBin string) (*sailComposeFile, string, map[int]int, []string, error) {
	// Prefer `<composeBin> compose config` which resolves env-var defaults and
	// normalises the YAML. Fall back to the raw file when unavailable.
	resolvedBytes, err := sailResolvedComposeBytes(composeFilePath, cwd, composeBin)
	if err != nil {
		return nil, "", nil, nil, err
	}

	// Parse into typed struct for service-level detection (DB, MinIO, …).
	var cf sailComposeFile
	_ = yaml.Unmarshal(resolvedBytes, &cf)

	// Build the port remap (data services only).
	portRemap := sailBuildPortRemap(&cf)

	// Parse into a raw map so we can modify arbitrary fields without losing
	// unknown keys (healthchecks, labels, networks, volumes, …).
	var raw map[string]interface{}
	if err := yaml.Unmarshal(resolvedBytes, &raw); err != nil {
		return nil, "", nil, nil, fmt.Errorf("parsing compose YAML: %w", err)
	}

	services, _ := raw["services"].(map[string]interface{})
	var strippedSvcs []string

	// Under rootless podman, named volumes need ":U" so podman chowns them to
	// the container user. Docker compose rejects ":U", so podman-only.
	isPodman := strings.Contains(composeBin, "podman")

	for name, svcRaw := range services {
		svc, ok := svcRaw.(map[string]interface{})
		if !ok {
			continue
		}
		if isPodman {
			sailAddPodmanVolumeChown(svc)
		}
		if !sailDataServices[name] {
			if _, hasPorts := svc["ports"]; hasPorts {
				delete(svc, "ports")
				strippedSvcs = append(strippedSvcs, name)
			}
			continue
		}
		// Data service — rewrite ports in place.
		ports, _ := svc["ports"].([]interface{})
		if len(ports) == 0 {
			continue
		}
		var newPorts []string
		for _, p := range ports {
			hp := sailHostPort(p)
			if newPort, ok := portRemap[hp]; ok {
				tgt := sailRawContainerPort(p)
				if tgt > 0 {
					newPorts = append(newPorts, fmt.Sprintf("%d:%d", newPort, tgt))
				}
			} else {
				if s := sailPortToString(p); s != "" {
					newPorts = append(newPorts, s)
				}
			}
		}
		svc["ports"] = newPorts
	}

	// Serialise the modified compose to a temp file.
	data, err := yaml.Marshal(raw)
	if err != nil {
		return nil, "", nil, nil, fmt.Errorf("serialising compose: %w", err)
	}
	tmp, err := os.CreateTemp("", "lerd-sail-compose-*.yml")
	if err != nil {
		return nil, "", nil, nil, err
	}
	tmp.Close()
	if err := os.WriteFile(tmp.Name(), data, 0644); err != nil {
		os.Remove(tmp.Name())
		return nil, "", nil, nil, err
	}
	return &cf, tmp.Name(), portRemap, strippedSvcs, nil
}

// sailResolvedComposeBytes returns the fully-resolved compose YAML bytes,
// preferring `<composeBin> compose config` and falling back to the raw file.
func sailResolvedComposeBytes(composeFilePath, cwd, composeBin string) ([]byte, error) {
	filename := filepath.Base(composeFilePath)
	cmd := exec.Command(composeBin, "compose", "-f", filename, "config")
	cmd.Dir = cwd
	if out, err := cmd.Output(); err == nil && len(out) > 0 {
		return out, nil
	}
	return os.ReadFile(composeFilePath)
}

// sailRawContainerPort extracts the container (target) port from a port
// binding, whether it is a string ("3306:3306") or a long-form map.
func sailRawContainerPort(raw interface{}) int {
	switch v := raw.(type) {
	case string:
		resolved := sailResolveAllEnvVars(v)
		parts := strings.Split(resolved, ":")
		n, _ := strconv.Atoi(parts[len(parts)-1])
		return n
	case map[string]interface{}:
		return sailMapTargetPort(v)
	}
	return 0
}

// sailExtractDefault extracts the default value from a ${VAR:-default} expression,
// or returns the string unchanged if it is not in that form.
func sailExtractDefault(s string) string {
	if !strings.HasPrefix(s, "${") || !strings.HasSuffix(s, "}") {
		return s
	}
	inner := s[2 : len(s)-1]
	if idx := strings.Index(inner, ":-"); idx >= 0 {
		return inner[idx+2:]
	}
	return s
}

// sailResolveAllEnvVars replaces every ${VAR:-default} expression in s with
// its default value. This is needed before splitting on ":" because the ":-"
// inside a Sail port like "${APP_PORT:-80}:80" would otherwise produce too many
// parts when naively splitting on ":".
func sailResolveAllEnvVars(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], "${") {
			end := strings.Index(s[i:], "}")
			if end < 0 {
				b.WriteString(s[i:])
				break
			}
			b.WriteString(sailExtractDefault(s[i : i+end+1]))
			i += end + 1
		} else {
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}

// sailHostPort returns the host port from a compose port entry string, resolving
// ${VAR:-default} patterns and returning 0 when no host port is declared.
func sailHostPort(raw interface{}) int {
	s, ok := raw.(string)
	if !ok {
		// Long-form map: {target: N, published: N}
		if m, ok := raw.(map[string]interface{}); ok {
			switch p := m["published"].(type) {
			case int:
				return p
			case string:
				n, _ := strconv.Atoi(sailExtractDefault(p))
				return n
			}
		}
		return 0
	}
	// Resolve env-var expressions before splitting — prevents "${APP_PORT:-80}:80"
	// from yielding three parts due to the colon inside ":-".
	parts := strings.Split(sailResolveAllEnvVars(s), ":")
	switch len(parts) {
	case 1:
		// Container-only binding — no host port published.
		return 0
	case 2:
		n, _ := strconv.Atoi(parts[0])
		return n
	default:
		// ip:host:container — host port is second-to-last segment.
		n, _ := strconv.Atoi(parts[len(parts)-2])
		return n
	}
}

// Runs a COUNT(*) against information_schema.tables / pg_tables for the
// target database. Returns 0 when the database is missing or has no tables.
func sailCountTables(composeArgs []string, service string, env *dbEnv, composeBin string) (int, error) {
	var args []string
	args = append(args, composeArgs...)
	args = append(args, "exec", "-T")
	switch env.connection {
	case "mysql", "mariadb":
		args = append(args, "-e", "MYSQL_PWD="+env.password,
			service, "mysql", "-h", "127.0.0.1", "-u"+env.username, "-N", "-B", "-e",
			fmt.Sprintf("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=%q;", env.database))
	case "pgsql", "postgres":
		args = append(args, "-e", "PGPASSWORD="+env.password,
			service, "psql", "-U", env.username, "-d", env.database, "-tAc",
			"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='public';")
	default:
		return 0, nil
	}
	out, err := exec.Command(composeBin, args...).Output()
	if err != nil {
		// Database may not exist — treat as zero.
		return 0, nil
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n, nil
}

// Counts objects in a Sail MinIO bucket via `mc ls --recursive`.
func sailCountBucketObjects(mcImage, sailMCEnv, bucket string) (int, error) {
	cmd := podman.Cmd("run", "--rm", "-e", sailMCEnv, mcImage, "ls", "--recursive", "sail/"+bucket)
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count, nil
}

// Prints a numbered list and returns the chosen option. An invalid or empty
// answer re-prompts; we return "" only if options is empty.
func promptSelect(prompt string, options []string) string {
	if len(options) == 0 {
		return ""
	}
	for {
		fmt.Printf("%s:\n", prompt)
		for i, o := range options {
			fmt.Printf("  %d) %s\n", i+1, o)
		}
		fmt.Print("Choice [1]: ")
		var answer string
		fmt.Scanln(&answer) //nolint:errcheck
		answer = strings.TrimSpace(answer)
		if answer == "" {
			return options[0]
		}
		idx, err := strconv.Atoi(answer)
		if err == nil && idx >= 1 && idx <= len(options) {
			return options[idx-1]
		}
		fmt.Printf("Invalid choice %q — please enter a number between 1 and %d.\n", answer, len(options))
	}
}

// Prompts the user with "[y/N]" and returns true only on an explicit yes.
func promptConfirm(question string) bool {
	fmt.Printf("%s [y/N] ", question)
	var answer string
	fmt.Scanln(&answer) //nolint:errcheck
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "y" || answer == "yes"
}

// Reads DB credentials from the compose service's environment block. Covers
// both MYSQL_* (mysql/mariadb) and POSTGRES_* (pgsql) keys.
func sailDetectDBCreds(cf *sailComposeFile, service string) (user, password string) {
	svc, ok := cf.Services[service]
	if !ok {
		return "", ""
	}
	env := svc.Environment
	if u := env["MYSQL_USER"]; u != "" {
		return u, env["MYSQL_PASSWORD"]
	}
	if u := env["POSTGRES_USER"]; u != "" {
		return u, env["POSTGRES_PASSWORD"]
	}
	return "", ""
}

// Only matches mysql/mysql-server — mariadb and standard mysql images put
// their socket in /var/run/mysqld (ephemeral) and don't need cleanup.
func isMysqlLikeService(cf *sailComposeFile, service string) bool {
	svc, ok := cf.Services[service]
	if !ok {
		return false
	}
	img := strings.ToLower(svc.Image)
	return strings.Contains(img, "mysql/mysql-server")
}

// Appends ":U" to named-volume mounts so rootless podman chowns the volume
// to the container user on start. Bind mounts and long-form entries are left
// alone; only applied under podman because docker-compose rejects ":U".
func sailAddPodmanVolumeChown(svc map[string]interface{}) {
	vols, ok := svc["volumes"].([]interface{})
	if !ok {
		return
	}
	for i, v := range vols {
		s, ok := v.(string)
		if !ok {
			continue
		}
		parts := strings.SplitN(s, ":", 3)
		if len(parts) < 2 {
			continue
		}
		src := parts[0]
		if strings.HasPrefix(src, "/") || strings.HasPrefix(src, ".") || strings.HasPrefix(src, "~") {
			continue // bind mount
		}
		if len(parts) == 3 {
			opts := parts[2]
			if strings.Contains(","+opts+",", ",U,") {
				continue
			}
			vols[i] = s + ",U"
		} else {
			vols[i] = s + ":U"
		}
	}
	svc["volumes"] = vols
}

// sailDataServices is the set of Sail backing-service names whose port bindings
// are relevant during import (DB dump and S3 mirror). Any service not in this
// set is treated as an app/worker service whose ports are stripped entirely from
// the override — they are never needed and could conflict with the host.
var sailDataServices = map[string]bool{
	"mysql":       true,
	"mariadb":     true,
	"pgsql":       true,
	"postgres":    true,
	"redis":       true,
	"meilisearch": true,
	"minio":       true,
	"mailpit":     true,
	"soketi":      true,
	"selenium":    true,
}

// sailBuildPortRemap returns origHostPort → remappedPort for any port that
// conflicts with a lerd service. Only data services are considered; app/worker
// services have their ports stripped entirely (see sailWritePortOverride).
func sailBuildPortRemap(cf *sailComposeFile) map[int]int {
	remap := map[int]int{}
	for name, svc := range cf.Services {
		if !sailDataServices[name] {
			continue
		}
		for _, raw := range svc.Ports {
			hp := sailHostPort(raw)
			if hp > 0 && lerdConflictPorts[hp] {
				if remapped := hp + sailImportPortOffset; remapped <= 65535 {
					remap[hp] = remapped
				}
			}
		}
	}
	return remap
}

// sailRemapPortString rewrites a port binding (string or long-form map) with
// the new host port, returning a "published:target" string for the override.
// Env-var expressions like ${APP_PORT:-80} are resolved before parsing so that
// the colon inside ":-" does not confuse the split.
func sailRemapPortString(raw interface{}, remap map[int]int) string {
	switch v := raw.(type) {
	case string:
		parts := strings.Split(sailResolveAllEnvVars(v), ":")
		switch len(parts) {
		case 1:
			// Container-only — no host port to remap.
			return v
		case 2:
			origPort, _ := strconv.Atoi(parts[0])
			if newPort, ok := remap[origPort]; ok {
				return fmt.Sprintf("%d:%s", newPort, parts[1])
			}
			return v
		default:
			// ip:host:container — host port is second-to-last.
			origPort, _ := strconv.Atoi(parts[len(parts)-2])
			if newPort, ok := remap[origPort]; ok {
				container := parts[len(parts)-1]
				ip := strings.Join(parts[:len(parts)-2], ":")
				return fmt.Sprintf("%s:%d:%s", ip, newPort, container)
			}
			return v
		}
	case map[string]interface{}:
		// Long-form entry produced by `docker compose config`:
		// {target: 3306, published: "3306", protocol: "tcp", mode: "ingress"}
		origPort := sailHostPort(raw)
		if newPort, ok := remap[origPort]; ok {
			target := sailMapTargetPort(v)
			if target > 0 {
				return fmt.Sprintf("%d:%d", newPort, target)
			}
		}
	}
	return ""
}

// sailMapTargetPort extracts the container (target) port from a long-form
// docker compose port map.
func sailMapTargetPort(m map[string]interface{}) int {
	switch t := m["target"].(type) {
	case int:
		return t
	case string:
		n, _ := strconv.Atoi(t)
		return n
	}
	return 0
}

// sailPortToString converts a port binding (string or long-form map) to a
// "published:target" string suitable for a docker-compose override file.
func sailPortToString(raw interface{}) string {
	switch v := raw.(type) {
	case string:
		return sailResolveAllEnvVars(v) // resolve ${VAR:-default} so override is literal
	case map[string]interface{}:
		pub := sailHostPort(raw)
		tgt := sailMapTargetPort(v)
		if pub > 0 && tgt > 0 {
			return fmt.Sprintf("%d:%d", pub, tgt)
		}
	}
	return ""
}

// sailFindDBService returns the docker-compose service name for the DB.
func sailFindDBService(cf *sailComposeFile, connection string) string {
	candidates := map[string][]string{
		"mysql":    {"mysql"},
		"mariadb":  {"mariadb", "mysql"},
		"pgsql":    {"pgsql", "postgres"},
		"postgres": {"postgres", "pgsql"},
	}
	conn := strings.ToLower(connection)
	for _, name := range candidates[conn] {
		if _, ok := cf.Services[name]; ok {
			return name
		}
	}
	// Fall back: detect by image name.
	for name, svc := range cf.Services {
		img := strings.ToLower(svc.Image)
		switch {
		case (strings.Contains(img, "mysql") || strings.Contains(img, "mariadb")) &&
			(conn == "mysql" || conn == "mariadb"):
			return name
		case strings.Contains(img, "postgres") && (conn == "pgsql" || conn == "postgres"):
			return name
		}
	}
	return ""
}

// sailDetectDatabase lists user-accessible databases in the Sail container and returns
// the best single candidate, ignoring MySQL/PostgreSQL system databases.  Returns ""
// if there are zero or multiple candidates (caller falls back to sailEnv.database).
func sailDetectDatabase(composeArgs []string, service string, env *dbEnv, composeBin string) (string, error) {
	var args []string
	args = append(args, composeArgs...)
	args = append(args, "exec", "-T")

	systemDBs := map[string]bool{
		"information_schema": true,
		"performance_schema": true,
		"mysql":              true,
		"sys":                true,
		"testing":            true, // Sail creates this automatically
		"postgres":           true,
		"template0":          true,
		"template1":          true,
	}

	switch env.connection {
	case "mysql", "mariadb":
		args = append(args, "-e", "MYSQL_PWD="+env.password,
			service, "mysql", "-h", "127.0.0.1", "-u"+env.username,
			"-N", "-e", "SHOW DATABASES;")
	case "pgsql", "postgres":
		args = append(args, "-e", "PGPASSWORD="+env.password,
			service, "psql", "-U", env.username,
			"-t", "-c", "SELECT datname FROM pg_database WHERE datistemplate = false;")
	default:
		return "", nil
	}

	out, err := exec.Command(composeBin, args...).Output()
	if err != nil {
		return "", err
	}

	var candidates []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		db := strings.TrimSpace(line)
		if db != "" && !systemDBs[db] {
			candidates = append(candidates, db)
		}
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if len(candidates) > 1 {
		return promptSelect("Multiple Sail databases found, pick one", candidates), nil
	}
	return "", nil
}

// sailWaitDB polls until the Sail database is accepting connections (up to 60 s).
func sailWaitDB(composeArgs []string, service string, env *dbEnv, composeBin string) error {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		var args []string
		args = append(args, composeArgs...)
		args = append(args, "exec", "-T")
		switch env.connection {
		case "mysql", "mariadb":
			// -h 127.0.0.1 forces TCP: Sail sets MYSQL_ROOT_HOST="%" which allows
			// TCP but not Unix socket (localhost) connections.
			args = append(args, "-e", "MYSQL_PWD="+env.password,
				service, "mysqladmin", "ping", "-h", "127.0.0.1", "-u"+env.username, "--silent")
		case "pgsql", "postgres":
			args = append(args, service, "pg_isready", "-U", env.username)
		default:
			return nil
		}
		cmd := exec.Command(composeBin, args...)
		if cmd.Run() == nil {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timed out after 60s")
}

// sailDumpDB exports the Sail database to a temporary file and returns its path.
func sailDumpDB(composeArgs []string, service string, env *dbEnv, composeBin string) (string, error) {
	tmp, err := os.CreateTemp("", "lerd-sail-dump-*.sql")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	tmp.Close()

	var args []string
	args = append(args, composeArgs...)
	args = append(args, "exec", "-T")

	switch env.connection {
	case "mysql", "mariadb":
		args = append(args, "-e", "MYSQL_PWD="+env.password,
			service, "mysqldump", "--no-tablespaces", "-h", "127.0.0.1", "-u"+env.username, env.database)
	case "pgsql", "postgres":
		args = append(args, "-e", "PGPASSWORD="+env.password,
			service, "pg_dump", "-U", env.username, env.database)
	default:
		os.Remove(tmpPath)
		return "", fmt.Errorf("unsupported DB_CONNECTION: %q", env.connection)
	}

	out, err := os.Create(tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return "", err
	}
	defer out.Close()

	cmd := exec.Command(composeBin, args...)
	cmd.Stdout = out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		os.Remove(tmpPath)
		return "", err
	}
	return tmpPath, nil
}

// sailRecreateDB drops and recreates the target database in lerd.
func sailRecreateDB(env *dbEnv) error {
	switch env.connection {
	case "mysql", "mariadb":
		sql := fmt.Sprintf("DROP DATABASE IF EXISTS `%s`; CREATE DATABASE `%s`;",
			env.database, env.database)
		cmd := podman.Cmd("exec", "-i",
			"-e", "MYSQL_PWD="+env.password,
			"lerd-mysql",
			"mysql", "-u"+env.username, "-e", sql)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%s", strings.TrimSpace(string(out)))
		}
		return nil
	case "pgsql", "postgres":
		// Connect to the 'postgres' maintenance db to drop/create the target.
		for _, sql := range []string{
			fmt.Sprintf(`DROP DATABASE IF EXISTS "%s";`, env.database),
			fmt.Sprintf(`CREATE DATABASE "%s";`, env.database),
		} {
			cmd := podman.Cmd("exec", "-i",
				"-e", "PGPASSWORD="+env.password,
				"lerd-postgres",
				"psql", "-U", env.username, "postgres", "-c", sql)
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("postgres: %s", strings.TrimSpace(string(out)))
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported connection: %q", env.connection)
	}
}

// sailImportDump pipes a SQL dump file into the lerd database.
func sailImportDump(dumpPath string, env *dbEnv) error {
	f, err := os.Open(dumpPath)
	if err != nil {
		return err
	}
	defer f.Close()
	cmd, err := dbImportCmd(env)
	if err != nil {
		return err
	}
	cmd.Stdin = f
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// sailFindMinio returns the MinIO service name, its (possibly remapped) host port,
// and the MINIO_ROOT_USER / MINIO_ROOT_PASSWORD credentials from the service's
// environment block.  Falls back to Sail's well-known defaults ("sail"/"password").
func sailFindMinio(cf *sailComposeFile, portRemap map[int]int) (name string, port int, user string, password string) {
	for svcName, svc := range cf.Services {
		if svcName != "minio" && !strings.Contains(strings.ToLower(svc.Image), "minio") {
			continue
		}
		// Read credentials directly from the compose environment — these are
		// hardcoded in Sail's docker-compose.yml and are NOT affected by lerd
		// overwriting .env.
		user = svc.Environment["MINIO_ROOT_USER"]
		if user == "" {
			user = "sail"
		}
		password = svc.Environment["MINIO_ROOT_PASSWORD"]
		if password == "" {
			password = "password"
		}

		// Find the S3 API port (9000 is the MinIO default).
		for _, rawPort := range svc.Ports {
			hp := sailHostPort(rawPort)
			if hp == 9000 {
				if remapped, ok := portRemap[9000]; ok {
					return svcName, remapped, user, password
				}
				return svcName, 9000, user, password
			}
		}
		// MinIO service found but no explicit port; use the remapped port if available.
		if remapped, ok := portRemap[9000]; ok {
			return svcName, remapped, user, password
		}
		return svcName, 9000, user, password
	}
	return "", 0, "", ""
}

// sailImportS3 mirrors a Sail MinIO bucket into lerd's RustFS using mc.
func sailImportS3(s3 *sailS3Env, minioPort int, dbName string) error {
	const mcImage = "docker.io/minio/mc:latest"

	if err := ensureServiceRunning("rustfs"); err != nil {
		return fmt.Errorf("starting rustfs: %w", err)
	}

	lerdBucket := s3BucketName(dbName)
	if _, err := createS3Bucket(lerdBucket); err != nil {
		return fmt.Errorf("creating lerd bucket %q: %w", lerdBucket, err)
	}

	const hostGW = "host.containers.internal"
	sailMCEnv := fmt.Sprintf("MC_HOST_sail=http://%s:%s@%s:%d",
		s3.accessKey, s3.secretKey, hostGW, minioPort)
	lerdMCEnv := fmt.Sprintf("MC_HOST_lerd=http://lerd:lerdpassword@%s:9000", hostGW)

	sourceBucket, err := sailResolveSourceBucket(mcImage, sailMCEnv, s3.bucket)
	if err != nil {
		return err
	}
	if sourceBucket != s3.bucket {
		fmt.Printf("  Configured bucket %q not found on Sail MinIO; using %q instead.\n", s3.bucket, sourceBucket)
	}

	// Count objects before mirror: skip (and don't touch lerd) if the source
	// bucket is empty, and otherwise prompt before overwriting lerd.
	objectCount, err := sailCountBucketObjects(mcImage, sailMCEnv, sourceBucket)
	if err != nil {
		return fmt.Errorf("listing bucket %q: %w", sourceBucket, err)
	}
	if objectCount == 0 {
		fmt.Printf("  Bucket %q is empty — skipping S3 import.\n", sourceBucket)
		return nil
	}
	fmt.Printf("  Found %d files in bucket %q. ", objectCount, sourceBucket)
	if !promptConfirm(fmt.Sprintf("Mirror into lerd bucket %q?", lerdBucket)) {
		fmt.Println("  S3 import skipped.")
		return nil
	}

	mirrorCmd := podman.Cmd("run", "--rm",
		"-e", sailMCEnv,
		"-e", lerdMCEnv,
		mcImage,
		"mirror", "--overwrite",
		"sail/"+sourceBucket,
		"lerd/"+lerdBucket,
	)
	mirrorCmd.Stdout = os.Stdout
	mirrorCmd.Stderr = os.Stderr
	return mirrorCmd.Run()
}

// Lists buckets on the Sail MinIO via `mc ls sail/` and picks the right one:
// prefer the configured name, fall back to the only one present, or error
// with the available options.
func sailResolveSourceBucket(mcImage, sailMCEnv, configured string) (string, error) {
	lsCmd := podman.Cmd("run", "--rm", "-e", sailMCEnv, mcImage, "ls", "sail")
	out, err := lsCmd.Output()
	if err != nil {
		return "", fmt.Errorf("listing Sail buckets: %w", err)
	}
	var buckets []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		// `mc ls` output: "[date] PREFIX bucket/"
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name := strings.TrimSuffix(fields[len(fields)-1], "/")
		if name != "" {
			buckets = append(buckets, name)
		}
	}
	if len(buckets) == 0 {
		return "", fmt.Errorf("no buckets found on Sail MinIO")
	}
	for _, b := range buckets {
		if b == configured {
			return b, nil
		}
	}
	if len(buckets) == 1 {
		return buckets[0], nil
	}
	return promptSelect(fmt.Sprintf("Bucket %q not found on Sail MinIO, pick one", configured), buckets), nil
}
