package cli

import (
	"bufio"
	crand "crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	neturl "net/url"

	"github.com/charmbracelet/huh"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/spf13/cobra"
)

// projectDBName returns a safe database name for the project at path.
// It uses the registered site name, falling back to the directory name,
// converting hyphens to underscores.
func projectDBName(path string) string {
	name := filepath.Base(path)
	if reg, err := config.LoadSites(); err == nil {
		for _, s := range reg.Sites {
			if s.Path == path {
				name = s.Name
				break
			}
		}
	}
	return strings.ReplaceAll(strings.ToLower(name), "-", "_")
}

// NewEnvCmd returns the env command.
func NewEnvCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "env",
		Short: "Configure .env for this project with lerd service connection settings",
		Long: `Sets up .env for the current project:
  - Creates .env from .env.example if it does not exist
  - Detects which services the project uses and sets lerd connection values
  - Starts any referenced services that are not already running
  - Generates APP_KEY if missing
  - Sets APP_URL to the registered .test domain`,
		RunE: runEnv,
	}
}

// serviceDetectors maps service names to a function that detects if the env references that service.
var serviceDetectors = map[string]func(map[string]string) bool{
	"mysql": func(env map[string]string) bool {
		v := strings.ToLower(env["DB_CONNECTION"])
		return v == "mysql" || v == "mariadb"
	},
	"postgres": func(env map[string]string) bool {
		return strings.ToLower(env["DB_CONNECTION"]) == "pgsql"
	},
	"redis": func(env map[string]string) bool {
		_, hasHost := env["REDIS_HOST"]
		return hasHost ||
			env["CACHE_STORE"] == "redis" ||
			env["SESSION_DRIVER"] == "redis" ||
			env["QUEUE_CONNECTION"] == "redis"
	},
	"meilisearch": func(env map[string]string) bool {
		return strings.ToLower(env["SCOUT_DRIVER"]) == "meilisearch"
	},
	"rustfs": func(env map[string]string) bool {
		_, hasEndpoint := env["AWS_ENDPOINT"]
		return strings.ToLower(env["FILESYSTEM_DISK"]) == "s3" || hasEndpoint
	},
	"mailpit": func(env map[string]string) bool {
		_, hasHost := env["MAIL_HOST"]
		return hasHost
	},
}

func runEnv(_ *cobra.Command, _ []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	// Determine framework-specific env file path and format
	site, _ := config.FindSiteByPath(cwd)
	if site == nil {
		return fmt.Errorf("no site registered for this directory\nRun 'lerd link' first")
	}

	fwName := site.Framework
	if fwName == "" {
		fwName, _ = config.DetectFramework(cwd)
	}
	if fwName == "" {
		return fmt.Errorf("no framework detected for this site\nDefine one with 'lerd framework add' or add a framework YAML to %s", config.FrameworksDir())
	}

	fw, ok := config.GetFramework(fwName)
	if !ok {
		return fmt.Errorf("framework %q is not defined\nDefine it with 'lerd framework add'", fwName)
	}

	if fw.Env.File == "" && fw.Env.Format == "" && len(fw.Env.Services) == 0 {
		return fmt.Errorf("'lerd env' is not supported for %s\nConfigure the env section in the framework YAML to enable it", fw.Label)
	}

	envRelPath, envFormat := fw.Env.Resolve(cwd)
	envPath := filepath.Join(cwd, envRelPath)

	exampleRelPath := fw.Env.ExampleFile
	if exampleRelPath == "" {
		exampleRelPath = ".env.example"
	}
	examplePath := filepath.Join(cwd, exampleRelPath)

	// 1. Create env file from example if it doesn't exist
	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		if _, err := os.Stat(examplePath); err == nil {
			fmt.Printf("Creating %s from %s...\n", envRelPath, exampleRelPath)
			if err := copyEnvFile(examplePath, envPath); err != nil {
				return fmt.Errorf("copying %s: %w", exampleRelPath, err)
			}
		} else if len(fw.Env.Services) > 0 {
			// No env or example file, but framework defines services — create
			// an empty env file so service detection can populate it.
			if dir := filepath.Dir(envPath); dir != "." {
				_ = os.MkdirAll(dir, 0755)
			}
			fmt.Printf("Creating empty %s (no example file found)...\n", envRelPath)
			if err := os.WriteFile(envPath, []byte(""), 0644); err != nil {
				return fmt.Errorf("creating %s: %w", envRelPath, err)
			}
		} else {
			return fmt.Errorf("no %s or %s found in %s", envRelPath, exampleRelPath, cwd)
		}
	} else {
		fmt.Printf("Updating existing %s...\n", envRelPath)
	}

	// 2. Parse the env file into a key→value map (for detection)
	var envMap map[string]string
	switch envFormat {
	case "php-const":
		envMap, err = envfile.ReadPhpConst(envPath)
	default:
		envMap, err = parseEnvMap(envPath)
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", envRelPath, err)
	}

	// 3. Detect services and build the set of key→value updates to apply
	updates := map[string]string{}
	dbName := projectDBName(cwd)

	scheme := "http"
	if site.Secured {
		scheme = "https"
	}
	tplCtx := siteTemplateCtx{
		site:   dbName,
		domain: site.PrimaryDomain(),
		scheme: scheme,
	}

	// Load .lerd.yaml service hints so we can apply env vars for services
	// listed there even when they are not yet referenced in the env file.
	lerdYAMLServices := map[string]bool{}
	if proj, projErr := config.LoadProjectConfig(cwd); projErr == nil {
		for _, svc := range proj.Services {
			lerdYAMLServices[svc.Name] = true
		}
	}

	// Laravel ships .env / .env.example with DB_CONNECTION=sqlite. If the user
	// hasn't yet picked a DB service for this project, offer to swap sqlite for
	// a lerd-managed mysql/postgres. Skipped for frameworks with explicit env
	// service rules (e.g. wordpress, symfony) — they don't use DB_CONNECTION.
	// "Keep SQLite" is persisted by adding "sqlite" to the project's services
	// list so we don't re-ask on every subsequent `lerd env` run.
	if len(fw.Env.Services) == 0 && isInteractive() &&
		!lerdYAMLServices["mysql"] && !lerdYAMLServices["postgres"] && !lerdYAMLServices["sqlite"] &&
		strings.EqualFold(strings.TrimSpace(envMap["DB_CONNECTION"]), "sqlite") {

		dbChoice := "sqlite"
		dbForm := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("Database").
				Description(envRelPath+" uses SQLite. Use a lerd-managed database service instead?").
				Options(
					huh.NewOption("Keep SQLite", "sqlite"),
					huh.NewOption("MySQL (lerd-mysql)", "mysql"),
					huh.NewOption("PostgreSQL (lerd-postgres)", "postgres"),
				).
				Value(&dbChoice),
		)).WithTheme(huh.ThemeCatppuccin())
		if err := dbForm.Run(); err != nil {
			return fmt.Errorf("database prompt: %w", err)
		}

		// Persist the choice to .lerd.yaml so future runs don't re-ask, and
		// flip the in-memory map so the service loop below picks it up.
		proj, _ := config.LoadProjectConfig(cwd)
		if proj == nil {
			proj = &config.ProjectConfig{}
		}
		proj.Services = append(proj.Services, config.ProjectService{Name: dbChoice})
		if err := config.SaveProjectConfig(cwd, proj); err != nil {
			fmt.Printf("  [WARN] could not save .lerd.yaml: %v\n", err)
		}
		lerdYAMLServices[dbChoice] = true
	}

	if len(fw.Env.Services) > 0 {
		// Framework defines its own service detection and vars — use those.
		for svc, def := range fw.Env.Services {
			if !frameworkServiceDetected(def, envMap) {
				continue
			}
			fmt.Printf("  Detected %-12s — applying lerd connection values\n", svc)
			isDB := svc == "mysql" || svc == "postgres"
			for _, kv := range def.Vars {
				k, v, _ := strings.Cut(kv, "=")
				updates[k] = applySiteHandle(v, tplCtx)
			}
			if isDB {
				if err := ensureServiceRunning(svc); err != nil {
					fmt.Printf("  [WARN] could not start %s: %v\n", svc, err)
				} else {
					for _, name := range []string{dbName, dbName + "_testing"} {
						created, err := createDatabase(svc, name)
						if err != nil {
							fmt.Printf("  [WARN] could not create database %q: %v\n", name, err)
						} else if created {
							fmt.Printf("  Created database %q\n", name)
						} else {
							fmt.Printf("  Database %q already exists\n", name)
						}
					}
				}
				continue
			}
			if err := ensureServiceRunning(svc); err != nil {
				fmt.Printf("  [WARN] could not start %s: %v\n", svc, err)
			}
		}
	} else {
		// Default Laravel-style detection.
		// If the user has an explicit DB choice in .lerd.yaml (sqlite, a
		// built-in mysql/postgres, or any custom DB family alternate like
		// mysql-5-6 / mariadb-11 / mongo-6), it overrides whatever the
		// existing .env happens to say about DB_CONNECTION — otherwise
		// switching DB types via the wizard would silently keep the old
		// credentials.
		userPickedDB := lerdYAMLServices["sqlite"] || lerdYAMLServices["mysql"] || lerdYAMLServices["postgres"]
		for name := range lerdYAMLServices {
			if fam := config.InferFamily(name); fam == "mysql" || fam == "mariadb" || fam == "postgres" || fam == "mongo" {
				userPickedDB = true
				break
			}
		}

		for _, svc := range knownServices {
			detector, ok := serviceDetectors[svc]
			detectedFromEnv := ok && detector(envMap)

			// Skip auto-detected DBs the user didn't pick.
			if userPickedDB && (svc == "mysql" || svc == "postgres") && !lerdYAMLServices[svc] {
				continue
			}

			if !detectedFromEnv && !lerdYAMLServices[svc] {
				continue
			}

			info, ok := serviceEnvVars[svc]
			if !ok {
				continue
			}

			if detectedFromEnv {
				fmt.Printf("  Detected %-12s — applying lerd connection values\n", svc)
			} else {
				fmt.Printf("  From .lerd.yaml %-4s — applying lerd connection values\n", svc)
			}
			for _, kv := range info.envVars {
				k, v, _ := strings.Cut(kv, "=")
				updates[k] = v
			}

			if svc == "mysql" || svc == "postgres" {
				updates["DB_DATABASE"] = dbName
				if err := ensureServiceRunning(svc); err != nil {
					fmt.Printf("  [WARN] could not start %s: %v\n", svc, err)
				} else {
					for _, name := range []string{dbName, dbName + "_testing"} {
						created, err := createDatabase(svc, name)
						if err != nil {
							fmt.Printf("  [WARN] could not create database %q: %v\n", name, err)
						} else if created {
							fmt.Printf("  Created database %q\n", name)
						} else {
							fmt.Printf("  Database %q already exists\n", name)
						}
					}
				}
				continue
			}

			if svc == "rustfs" {
				bucketName := s3BucketName(dbName)
				updates["AWS_BUCKET"] = bucketName
				updates["AWS_URL"] = "http://localhost:9000/" + bucketName
				if err := ensureServiceRunning(svc); err != nil {
					fmt.Printf("  [WARN] could not start %s: %v\n", svc, err)
				} else {
					created, err := createS3Bucket(bucketName)
					if err != nil {
						fmt.Printf("  [WARN] could not create bucket %q: %v\n", bucketName, err)
					} else if created {
						fmt.Printf("  Created bucket %q\n", bucketName)
					} else {
						fmt.Printf("  Bucket %q already exists\n", bucketName)
					}
				}
				continue
			}

			if err := ensureServiceRunning(svc); err != nil {
				fmt.Printf("  [WARN] could not start %s: %v\n", svc, err)
			}
		}
	}

	// 3a-bis. SQLite is not a containerized service but is a valid choice from
	// the init wizard / runtime DB prompt. When listed in .lerd.yaml, apply the
	// standard Laravel sqlite env vars and ensure the database file exists so
	// migrations can run immediately. No service to start, no SQL DB to create.
	if lerdYAMLServices["sqlite"] {
		fmt.Printf("  From .lerd.yaml %-4s — applying lerd connection values\n", "sqlite")
		for _, kv := range serviceEnvVars["sqlite"].envVars {
			k, v, _ := strings.Cut(kv, "=")
			updates[k] = v
		}
		sqlitePath := filepath.Join(cwd, "database", "database.sqlite")
		if _, statErr := os.Stat(sqlitePath); os.IsNotExist(statErr) {
			if err := os.MkdirAll(filepath.Dir(sqlitePath), 0o755); err == nil {
				if f, err := os.Create(sqlitePath); err == nil {
					_ = f.Close()
					fmt.Printf("  Created %s\n", filepath.Join("database", "database.sqlite"))
				}
			}
		}
	}

	// 3b. Custom services. Three triggers:
	//   - the service is listed in .lerd.yaml (user explicitly picked it)
	//   - env_detect matches an existing key in the project's .env
	//   - both
	// DB family alternates (mysql-5-6, mariadb-11, postgres-14) need
	// DB_DATABASE rewritten to the project name and the database created
	// inside the container, mirroring what built-in mysql/postgres do above.
	customs, _ := config.ListCustomServices()
	for _, svc := range customs {
		pickedFromYAML := lerdYAMLServices[svc.Name]
		detectedFromEnv := false
		if svc.EnvDetect != nil {
			if svc.EnvDetect.Key != "" {
				if val, exists := envMap[svc.EnvDetect.Key]; exists {
					if svc.EnvDetect.ValuePrefix == "" || strings.HasPrefix(val, svc.EnvDetect.ValuePrefix) {
						detectedFromEnv = true
					}
				}
			}
			if svc.EnvDetect.Composer != "" && config.ComposerHasPackage(cwd, svc.EnvDetect.Composer) {
				detectedFromEnv = true
			}
		}
		if !pickedFromYAML && !detectedFromEnv {
			continue
		}
		if len(svc.EnvVars) == 0 {
			// Nothing to write — still ensure the container is up so the
			// project can reach it once running.
			if err := ensureServiceRunning(svc.Name); err != nil {
				fmt.Printf("  [WARN] could not start %s: %v\n", svc.Name, err)
			}
			continue
		}
		if pickedFromYAML {
			fmt.Printf("  From .lerd.yaml %-4s — applying lerd connection values\n", svc.Name)
		} else {
			fmt.Printf("  Detected %-12s — applying lerd connection values\n", svc.Name)
		}
		for _, kv := range svc.EnvVars {
			k, v, _ := strings.Cut(kv, "=")
			updates[k] = applySiteHandle(v, tplCtx)
		}
		family := config.InferFamily(svc.Name)
		isDB := family == "mysql" || family == "mariadb" || family == "postgres"
		if isDB {
			updates["DB_DATABASE"] = dbName
		}
		if err := ensureServiceRunning(svc.Name); err != nil {
			fmt.Printf("  [WARN] could not start %s: %v\n", svc.Name, err)
			continue
		}
		if isDB {
			for _, name := range []string{dbName, dbName + "_testing"} {
				created, err := createDatabase(svc.Name, name)
				if err != nil {
					fmt.Printf("  [WARN] could not create database %q: %v\n", name, err)
				} else if created {
					fmt.Printf("  Created database %q\n", name)
				} else {
					fmt.Printf("  Database %q already exists\n", name)
				}
			}
		}
		if svc.SiteInit != nil && svc.SiteInit.Exec != "" {
			runSiteInit(svc, tplCtx)
		}
	}

	// 3c. Patch DuskTestCase.php for lerd's Selenium container if applicable.
	if _, hasDriver := updates["DUSK_DRIVER_URL"]; hasDriver {
		patchDuskTestCase(cwd)
	}

	// 3d. Generate REVERB_ env vars if a worker with proxy config is detected and
	// BROADCAST_CONNECTION=reverb is set.
	if fw.HasWorker("reverb", cwd) &&
		strings.ToLower(strings.Trim(envMap["BROADCAST_CONNECTION"], `"'`)) == "reverb" {
		fmt.Println("  Detected reverb     — configuring REVERB_ connection values")
		for k, v := range reverbEnvUpdates(envMap, site.PrimaryDomain(), site.Secured, cwd) {
			updates[k] = v
		}
	}

	// 4. Set the URL key. Precedence (matching other lerd settings):
	//    1. .lerd.yaml `app_url` — committed, shared across machines
	//    2. sites.yaml `app_url` — per-machine override
	//    3. <scheme>://<primary-domain> default generator
	urlKey := fw.Env.URLKey
	if urlKey == "" {
		urlKey = "APP_URL"
	}
	if url := resolveAppURL(cwd, site); url != "" {
		updates[urlKey] = url
		fmt.Printf("  Setting %s=%s\n", urlKey, url)
	}

	// 5. Rewrite the env file preserving order, comments, and blank lines
	if len(updates) > 0 {
		var writeErr error
		switch envFormat {
		case "php-const":
			writeErr = envfile.ApplyPhpConstUpdates(envPath, updates)
		default:
			writeErr = envfile.ApplyUpdates(envPath, updates)
		}
		if writeErr != nil {
			return fmt.Errorf("writing %s: %w", envRelPath, writeErr)
		}
	}

	// 6. Generate application key if the framework defines key generation and the key is missing.
	if kg := fw.Env.KeyGeneration; kg != nil && strings.TrimSpace(envMap[kg.EnvKey]) == "" {
		if kg.Command != "" {
			if _, statErr := os.Stat(filepath.Join(cwd, "vendor")); statErr == nil {
				fmt.Printf("  Generating %s...\n", kg.EnvKey)
				if err := artisanIn(cwd, kg.Command); err != nil {
					fmt.Printf("  [WARN] %s failed: %v\n", kg.Command, err)
				}
			} else if kg.FallbackPrefix != "" {
				fmt.Printf("  Generating %s (vendor not installed yet)...\n", kg.EnvKey)
				key := generateRandomKey(kg.FallbackPrefix)
				if err := envfile.ApplyUpdates(envPath, map[string]string{kg.EnvKey: key}); err != nil {
					fmt.Printf("  [WARN] writing %s: %v\n", kg.EnvKey, err)
				}
			}
		} else if kg.FallbackPrefix != "" {
			fmt.Printf("  Generating %s...\n", kg.EnvKey)
			key := generateRandomKey(kg.FallbackPrefix)
			if err := envfile.ApplyUpdates(envPath, map[string]string{kg.EnvKey: key}); err != nil {
				fmt.Printf("  [WARN] writing %s: %v\n", kg.EnvKey, err)
			}
		}
	}

	fmt.Println("Done.")
	return nil
}

// frameworkServiceDetected returns true if any detect rule in def matches the env map.
func frameworkServiceDetected(def config.FrameworkServiceDef, envMap map[string]string) bool {
	for _, rule := range def.Detect {
		val, exists := envMap[rule.Key]
		if !exists {
			continue
		}
		if rule.ValuePrefix == "" || strings.HasPrefix(val, rule.ValuePrefix) {
			return true
		}
	}
	return false
}

// createDatabase creates a database with the given name in the mysql or postgres container.
// Returns (true, nil) if created, (false, nil) if it already existed, or (false, err) on failure.
// createDatabase creates dbName inside the named service container if it does
// not already exist. svc is the service name (e.g. "mysql", "mysql-5-6",
// "mariadb-11", "postgres-14"); the container is always "lerd-<svc>". The
// SQL client used is determined by the family inferred from svc.
func createDatabase(svc, name string) (bool, error) {
	container := "lerd-" + svc
	family := svc
	if inferred := config.InferFamily(svc); inferred != "" {
		family = inferred
	}
	switch family {
	case "mysql", "mariadb":
		// MariaDB 11 removed the `mysql` client symlink. Try `mariadb` first
		// for the mariadb family, `mysql` first for the mysql family, then
		// fall back to the other so old MariaDB images and new MySQL images
		// both work.
		binaries := []string{"mysql", "mariadb"}
		if family == "mariadb" {
			binaries = []string{"mariadb", "mysql"}
		}
		var lastErr error
		for _, bin := range binaries {
			check := exec.Command("podman", "exec", container, bin, "-uroot", "-plerd",
				"-sNe", fmt.Sprintf("SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name='%s';", name))
			out, err := check.Output()
			if err != nil {
				lastErr = err
				continue
			}
			if strings.TrimSpace(string(out)) != "0" {
				return false, nil
			}
			cmd := exec.Command("podman", "exec", container, bin, "-uroot", "-plerd",
				"-e", fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`;", name))
			cmd.Stderr = os.Stderr
			return true, cmd.Run()
		}
		return false, lastErr
	case "postgres":
		cmd := exec.Command("podman", "exec", container, "psql", "-U", "postgres",
			"-c", fmt.Sprintf(`CREATE DATABASE "%s";`, name))
		out, err := cmd.CombinedOutput()
		if err != nil {
			if strings.Contains(string(out), "already exists") {
				return false, nil
			}
			return false, fmt.Errorf("%s", strings.TrimSpace(string(out)))
		}
		return true, nil
	default:
		return false, nil
	}
}

// s3BucketName converts a project handle into a valid S3 bucket name:
// lowercase, hyphens instead of underscores, leading/trailing non-alphanumerics
// stripped, max length 63. Reuses the standard MinIO/S3 naming rules so the
// resulting bucket is portable.
func s3BucketName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.':
			b.WriteRune(r)
		case r == '_', r == '-', r == ' ':
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-.")
	if len(out) > 63 {
		out = out[:63]
	}
	if out == "" {
		out = "lerd"
	}
	return out
}

// createS3Bucket creates a bucket for the given name in lerd-rustfs using an ephemeral mc container.
// Returns (true, nil) if created, (false, nil) if it already existed, or (false, err) on failure.
func createS3Bucket(name string) (bool, error) {
	const (
		alias   = "lerd"
		mcImage = "docker.io/minio/mc:latest"
		mcEnv   = "MC_HOST_lerd=http://lerd:lerdpassword@lerd-rustfs:9000"
	)

	lsCmd := exec.Command("podman", "run", "--rm", "--network", "lerd",
		"-e", mcEnv, mcImage, "ls", alias+"/"+name)
	if err := lsCmd.Run(); err == nil {
		return false, nil
	}

	mbCmd := exec.Command("podman", "run", "--rm", "--network", "lerd",
		"-e", mcEnv, mcImage, "mb", alias+"/"+name)
	if out, err := mbCmd.CombinedOutput(); err != nil {
		return false, fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}

	pubCmd := exec.Command("podman", "run", "--rm", "--network", "lerd",
		"-e", mcEnv, mcImage, "anonymous", "set", "public", alias+"/"+name)
	if out, err := pubCmd.CombinedOutput(); err != nil {
		return false, fmt.Errorf("mc anonymous set public: %s", strings.TrimSpace(string(out)))
	}
	return true, nil
}

// ensureServiceRunning starts the service if it is not already active, then
// waits until it is ready to accept connections before returning.
func ensureServiceRunning(name string) error {
	unit := "lerd-" + name
	status, _ := podman.UnitStatus(unit)
	if status == "active" {
		if err := podman.WaitReady(name, 30*time.Second); err != nil {
			return fmt.Errorf("%s is active but not yet ready: %w", name, err)
		}
		return nil
	}
	if isKnownService(name) {
		fmt.Printf("  Starting %s...\n", name)
		if err := ensureServiceQuadlet(name); err != nil {
			return err
		}
	} else {
		svc, err := config.LoadCustomService(name)
		if err != nil {
			return fmt.Errorf("custom service %q not found: %w", name, err)
		}
		for _, dep := range svc.DependsOn {
			if err := ensureServiceRunning(dep); err != nil {
				return fmt.Errorf("starting dependency %q for %q: %w", dep, name, err)
			}
		}
		fmt.Printf("  Starting %s...\n", name)
		if err := ensureCustomServiceQuadlet(svc); err != nil {
			return err
		}
	}
	if err := podman.StartUnit(unit); err != nil {
		return err
	}
	return podman.WaitReady(name, 60*time.Second)
}

// resolveAppURL returns the URL lerd should write to APP_URL for the project,
// applying the standard lerd precedence chain:
//
//  1. .lerd.yaml `app_url` (committed, shared across machines)
//  2. sites.yaml `app_url` (per-machine override)
//  3. `<scheme>://<primary-domain>` default generator
//
// The .lerd.yaml `app_url` is suppressed when its host is one of the project's
// declared domains that got filtered out at registration time (i.e. another
// site already owns it on this machine). External hosts and unrelated values
// pass through unchanged — only the conflict-filtered case is rejected.
//
// Returns an empty string only when the site is unregistered and no override
// is set anywhere — callers should treat that as "leave APP_URL alone".
func resolveAppURL(cwd string, site *config.Site) string {
	proj, _ := config.LoadProjectConfig(cwd)
	if proj != nil && strings.TrimSpace(proj.AppURL) != "" {
		val := strings.TrimSpace(proj.AppURL)
		if !appURLPointsToFilteredDomain(val, proj, site) {
			return val
		}
	}
	if site != nil && strings.TrimSpace(site.AppURL) != "" {
		return strings.TrimSpace(site.AppURL)
	}
	return siteURL(cwd)
}

// appURLPointsToFilteredDomain reports whether the given URL's host matches
// a domain that the project declared in .lerd.yaml but that did NOT survive
// the conflict filter at registration time. When true, the caller should
// fall through to the next precedence level instead of writing a value that
// points at a domain owned by another site.
func appURLPointsToFilteredDomain(rawURL string, proj *config.ProjectConfig, site *config.Site) bool {
	if proj == nil || site == nil || len(proj.Domains) == 0 {
		return false
	}
	parsed, err := neturl.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return false
	}
	host := parsed.Hostname()

	cfg, cfgErr := config.LoadGlobal()
	if cfgErr != nil {
		return false
	}
	suffix := "." + cfg.DNS.TLD

	// Was this host in the .lerd.yaml-declared list?
	declared := false
	for _, d := range proj.Domains {
		if strings.ToLower(d)+suffix == host {
			declared = true
			break
		}
	}
	if !declared {
		return false
	}
	// Declared but did it survive into the registered site?
	for _, d := range site.Domains {
		if d == host {
			return false
		}
	}
	return true
}

// siteURL returns the APP_URL for the project registered at path, or "".
func siteURL(path string) string {
	reg, err := config.LoadSites()
	if err != nil {
		return ""
	}
	for _, s := range reg.Sites {
		if s.Path == path {
			scheme := "http"
			if s.Secured {
				scheme = "https"
			}
			return scheme + "://" + s.PrimaryDomain()
		}
	}
	return ""
}

// parseEnvMap parses a .env file into a key→value map, stripping surrounding quotes.
func parseEnvMap(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	m := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		k, v, _ := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		m[k] = v
	}
	return m, scanner.Err()
}

// generateRandomKey generates a random 32-byte key with the given prefix.
// Example: generateRandomKey("base64:") returns "base64:<base64-encoded-32-bytes>".
func generateRandomKey(prefix string) string {
	key := make([]byte, 32)
	crand.Read(key) //nolint:errcheck
	return prefix + base64.StdEncoding.EncodeToString(key)
}

func artisanIn(dir string, args ...string) error {
	version, err := phpDet.DetectVersion(dir)
	if err != nil {
		cfg, _ := config.LoadGlobal()
		version = cfg.PHP.DefaultVersion
	}

	short := strings.ReplaceAll(version, ".", "")
	container := "lerd-php" + short + "-fpm"

	cmdArgs := []string{"exec", "-i", "-w", dir, container, "php", "artisan"}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.Command("podman", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// siteTemplateCtx holds the values available to {{…}} placeholders in
// framework env var templates.
type siteTemplateCtx struct {
	site   string // database / handle name
	domain string // primary domain (e.g. myapp.test)
	scheme string // "http" or "https"
}

// applySiteHandle replaces {{site}}, {{site_testing}}, {{domain}}, {{scheme}},
// and service version placeholders (e.g. {{mysql_version}}, {{postgres_version}}) in s.
func applySiteHandle(s string, ctx siteTemplateCtx) string {
	s = strings.ReplaceAll(s, "{{site}}", ctx.site)
	s = strings.ReplaceAll(s, "{{site_testing}}", ctx.site+"_testing")
	if ctx.domain != "" {
		s = strings.ReplaceAll(s, "{{domain}}", ctx.domain)
	}
	if ctx.scheme != "" {
		s = strings.ReplaceAll(s, "{{scheme}}", ctx.scheme)
	}
	// Lazy-resolve service version placeholders only when present.
	for _, svc := range []string{"mysql", "postgres", "redis", "meilisearch"} {
		placeholder := "{{" + svc + "_version}}"
		if strings.Contains(s, placeholder) {
			s = strings.ReplaceAll(s, placeholder, podman.ServiceVersion("lerd-"+svc))
		}
	}
	return s
}

// runSiteInit executes the site_init.exec command inside the service container.
func runSiteInit(svc *config.CustomService, ctx siteTemplateCtx) {
	container := svc.SiteInit.Container
	if container == "" {
		container = "lerd-" + svc.Name
	}
	script := applySiteHandle(svc.SiteInit.Exec, ctx)
	cmd := exec.Command("podman", "exec", container, "sh", "-c", script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("  [WARN] site_init for %s failed: %v\n", svc.Name, err)
	}
}

// copyEnvFile copies src to dst with 0644 permissions.
func copyEnvFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

// reverbEnvUpdates returns REVERB_ and VITE_REVERB_ env key→value pairs.
// Random secrets (APP_ID, APP_KEY, APP_SECRET) are only generated when missing.
//
// REVERB_HOST/PORT/SCHEME are always set to localhost:REVERB_SERVER_PORT over HTTP.
// The queue worker runs inside the PHP-FPM container (via podman exec) alongside
// Reverb, so it must connect to Reverb directly rather than routing through the
// nginx reverse proxy on the host (which is not reachable from inside the container).
//
// VITE_REVERB_HOST/PORT/SCHEME are set to the site's domain and external port so
// the browser can reach Reverb through the nginx WebSocket proxy.
//
// REVERB_SERVER_PORT is auto-assigned when missing to avoid port collisions between sites.
func reverbEnvUpdates(envMap map[string]string, domain string, secured bool, sitePath string) map[string]string {
	updates := map[string]string{}
	missing := func(key string) bool {
		return strings.TrimSpace(envMap[key]) == ""
	}

	if missing("REVERB_APP_ID") {
		updates["REVERB_APP_ID"] = randNumeric(6)
	}
	if missing("REVERB_APP_KEY") {
		updates["REVERB_APP_KEY"] = randAlphanumeric(20)
	}
	if missing("REVERB_APP_SECRET") {
		updates["REVERB_APP_SECRET"] = randAlphanumeric(20)
	}

	// REVERB_SERVER_PORT is the port Reverb listens on inside the PHP-FPM container.
	// Auto-assign a unique port per site to prevent collisions when multiple apps run Reverb.
	if missing("REVERB_SERVER_PORT") {
		updates["REVERB_SERVER_PORT"] = strconv.Itoa(assignWorkerProxyPort(sitePath, "REVERB_SERVER_PORT", 8080))
	}
	serverPort := envMap["REVERB_SERVER_PORT"]
	if v, ok := updates["REVERB_SERVER_PORT"]; ok {
		serverPort = v
	}
	if serverPort == "" {
		serverPort = "8080"
	}

	// REVERB_HOST/PORT/SCHEME — server-side broadcasting (queue worker → Reverb).
	// Always point to localhost:REVERB_SERVER_PORT so the queue worker, which runs
	// inside the same PHP-FPM container as Reverb, connects directly without going
	// through nginx.
	updates["REVERB_HOST"] = "localhost"
	updates["REVERB_PORT"] = serverPort
	updates["REVERB_SCHEME"] = "http"

	// VITE_ vars — browser-side (Echo → nginx → Reverb).
	// Use the site's domain and external port so the browser can connect via nginx.
	externalPort := "80"
	externalScheme := "http"
	if secured {
		externalScheme = "https"
		externalPort = "443"
	}
	appKey := envMap["REVERB_APP_KEY"]
	if v, ok := updates["REVERB_APP_KEY"]; ok {
		appKey = v
	}
	updates["VITE_REVERB_APP_KEY"] = appKey
	updates["VITE_REVERB_HOST"] = domain
	updates["VITE_REVERB_PORT"] = externalPort
	updates["VITE_REVERB_SCHEME"] = externalScheme

	return updates
}

const alphanumChars = "abcdefghijklmnopqrstuvwxyz0123456789"

func randAlphanumeric(n int) string {
	b := make([]byte, n)
	_, _ = crand.Read(b)
	for i, c := range b {
		b[i] = alphanumChars[int(c)%len(alphanumChars)]
	}
	return string(b)
}

func randNumeric(n int) string {
	const digits = "0123456789"
	b := make([]byte, n)
	_, _ = crand.Read(b)
	for i, c := range b {
		b[i] = digits[int(c)%len(digits)]
	}
	return string(b)
}

// patchDuskTestCase modifies tests/DuskTestCase.php so it works with lerd's
// Selenium container out of the box:
//   - Skips starting a local ChromeDriver when DUSK_DRIVER_URL is set
//   - Adds --ignore-certificate-errors so Chromium accepts mkcert certificates
func patchDuskTestCase(dir string) {
	path := filepath.Join(dir, "tests", "DuskTestCase.php")
	data, err := os.ReadFile(path)
	if err != nil {
		return // no DuskTestCase — nothing to patch
	}
	src := string(data)
	changed := false

	// 1. Skip local ChromeDriver when DUSK_DRIVER_URL is set.
	// The default Dusk scaffold has:
	//   if (! static::runningInSail()) {
	//       static::startChromeDriver(...)
	// We add a check for DUSK_DRIVER_URL so the local driver isn't started
	// when a remote Selenium container is configured.
	old := "if (! static::runningInSail()) {"
	replacement := "if (! static::runningInSail() && ! env('DUSK_DRIVER_URL')) {"
	if strings.Contains(src, old) && !strings.Contains(src, replacement) {
		src = strings.Replace(src, old, replacement, 1)
		changed = true
	}

	// 2. Add --ignore-certificate-errors so Chromium trusts mkcert certs.
	if !strings.Contains(src, "--ignore-certificate-errors") {
		// Insert after --disable-smooth-scrolling or --disable-search-engine-choice-screen
		for _, anchor := range []string{
			"'--disable-smooth-scrolling',",
			"'--disable-search-engine-choice-screen',",
		} {
			if idx := strings.Index(src, anchor); idx != -1 {
				insertAt := idx + len(anchor)
				// Detect indentation from the anchor line.
				lineStart := strings.LastIndex(src[:idx], "\n") + 1
				indent := ""
				for _, ch := range src[lineStart:idx] {
					if ch == ' ' || ch == '\t' {
						indent += string(ch)
					} else {
						break
					}
				}
				insert := "\n" + indent + "'--ignore-certificate-errors',"
				src = src[:insertAt] + insert + src[insertAt:]
				changed = true
				break
			}
		}
	}

	if changed {
		if err := os.WriteFile(path, []byte(src), 0644); err != nil {
			fmt.Printf("  [WARN] could not patch DuskTestCase.php: %v\n", err)
			return
		}
		fmt.Println("  Patched tests/DuskTestCase.php for lerd Selenium")
	}
}
