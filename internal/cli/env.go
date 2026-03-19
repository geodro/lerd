package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
	"minio": func(env map[string]string) bool {
		_, hasEndpoint := env["AWS_ENDPOINT"]
		return strings.ToLower(env["FILESYSTEM_DISK"]) == "s3" || hasEndpoint
	},
	"mailpit": func(env map[string]string) bool {
		_, hasHost := env["MAIL_HOST"]
		return hasHost
	},
	"soketi": func(env map[string]string) bool {
		_, hasPusherHost := env["PUSHER_HOST"]
		return strings.ToLower(env["BROADCAST_CONNECTION"]) == "pusher" || hasPusherHost
	},
}

func runEnv(_ *cobra.Command, _ []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	envPath := filepath.Join(cwd, ".env")
	examplePath := filepath.Join(cwd, ".env.example")

	// 1. Create .env from .env.example if it doesn't exist
	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		if _, err := os.Stat(examplePath); os.IsNotExist(err) {
			return fmt.Errorf("no .env or .env.example found in %s", cwd)
		}
		fmt.Println("Creating .env from .env.example...")
		if err := copyEnvFile(examplePath, envPath); err != nil {
			return fmt.Errorf("copying .env.example: %w", err)
		}
	} else {
		fmt.Println("Updating existing .env...")
	}

	// 2. Parse the .env into a key→value map (for detection)
	envMap, err := parseEnvMap(envPath)
	if err != nil {
		return fmt.Errorf("reading .env: %w", err)
	}

	// 3. Detect services and build the set of key→value updates to apply
	updates := map[string]string{}
	dbName := projectDBName(cwd)

	for _, svc := range knownServices {
		detector, ok := serviceDetectors[svc]
		if !ok || !detector(envMap) {
			continue
		}

		info, ok := serviceEnvVars[svc]
		if !ok {
			continue
		}

		fmt.Printf("  Detected %-12s — applying lerd connection values\n", svc)
		for _, kv := range info.envVars {
			k, v, _ := strings.Cut(kv, "=")
			updates[k] = v
		}

		// Use a per-project database instead of the shared "lerd" default.
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

		// Ensure the service is running
		if err := ensureServiceRunning(svc); err != nil {
			fmt.Printf("  [WARN] could not start %s: %v\n", svc, err)
		}
	}

	// 3b. Detect custom services
	customs, _ := config.ListCustomServices()
	for _, svc := range customs {
		if svc.EnvDetect == nil || len(svc.EnvVars) == 0 {
			continue
		}
		val, exists := envMap[svc.EnvDetect.Key]
		if !exists {
			continue
		}
		if svc.EnvDetect.ValuePrefix != "" && !strings.HasPrefix(val, svc.EnvDetect.ValuePrefix) {
			continue
		}
		fmt.Printf("  Detected %-12s — applying lerd connection values\n", svc.Name)
		for _, kv := range svc.EnvVars {
			k, v, _ := strings.Cut(kv, "=")
			updates[k] = applySiteHandle(v, dbName)
		}
		if err := ensureServiceRunning(svc.Name); err != nil {
			fmt.Printf("  [WARN] could not start %s: %v\n", svc.Name, err)
			continue
		}
		if svc.SiteInit != nil && svc.SiteInit.Exec != "" {
			runSiteInit(svc, dbName)
		}
	}

	// 4. Set APP_URL to the registered .test domain
	if url := siteURL(cwd); url != "" {
		updates["APP_URL"] = url
		fmt.Printf("  Setting APP_URL=%s\n", url)
	}

	// 5. Rewrite the .env preserving order, comments, and blank lines
	if len(updates) > 0 {
		if err := envfile.ApplyUpdates(envPath, updates); err != nil {
			return fmt.Errorf("writing .env: %w", err)
		}
	}

	// 6. Generate APP_KEY if missing or empty
	if strings.TrimSpace(envMap["APP_KEY"]) == "" {
		fmt.Println("  Generating APP_KEY...")
		if err := artisanIn(cwd, "key:generate"); err != nil {
			fmt.Printf("  [WARN] key:generate failed: %v\n", err)
		}
	}

	fmt.Println("Done.")
	return nil
}

// createDatabase creates a database with the given name in the mysql or postgres container.
// Returns (true, nil) if created, (false, nil) if it already existed, or (false, err) on failure.
func createDatabase(svc, name string) (bool, error) {
	switch svc {
	case "mysql":
		// Query row count before and after to detect whether the DB was created.
		check := exec.Command("podman", "exec", "lerd-mysql", "mysql", "-uroot", "-plerd",
			"-sNe", fmt.Sprintf("SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name='%s';", name))
		out, err := check.Output()
		if err == nil && strings.TrimSpace(string(out)) != "0" {
			return false, nil
		}
		cmd := exec.Command("podman", "exec", "lerd-mysql", "mysql", "-uroot", "-plerd",
			"-e", fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`;", name))
		cmd.Stderr = os.Stderr
		return true, cmd.Run()
	case "postgres":
		cmd := exec.Command("podman", "exec", "lerd-postgres", "psql", "-U", "postgres",
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

// ensureServiceRunning starts the service if it is not already active.
func ensureServiceRunning(name string) error {
	unit := "lerd-" + name
	status, _ := podman.UnitStatus(unit)
	if status == "active" {
		return nil
	}
	fmt.Printf("  Starting %s...\n", name)
	if isKnownService(name) {
		if err := ensureServiceQuadlet(name); err != nil {
			return err
		}
	} else {
		svc, err := config.LoadCustomService(name)
		if err != nil {
			return fmt.Errorf("custom service %q not found: %w", name, err)
		}
		if err := ensureCustomServiceQuadlet(svc); err != nil {
			return err
		}
	}
	return podman.StartUnit(unit)
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
			return scheme + "://" + s.Domain
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

// artisanIn runs php artisan <args> in the given directory using the project's PHP container.
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

// applySiteHandle replaces {{site}} and {{site_testing}} placeholders in s.
func applySiteHandle(s, site string) string {
	s = strings.ReplaceAll(s, "{{site}}", site)
	s = strings.ReplaceAll(s, "{{site_testing}}", site+"_testing")
	return s
}

// runSiteInit executes the site_init.exec command inside the service container.
func runSiteInit(svc *config.CustomService, site string) {
	container := svc.SiteInit.Container
	if container == "" {
		container = "lerd-" + svc.Name
	}
	script := applySiteHandle(svc.SiteInit.Exec, site)
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
