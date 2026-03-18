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
				if err := createDatabase(svc, dbName); err != nil {
					fmt.Printf("  [WARN] could not create database %q: %v\n", dbName, err)
				} else {
					fmt.Printf("  Created database %q\n", dbName)
				}
			}
			continue
		}

		// Ensure the service is running
		if err := ensureServiceRunning(svc); err != nil {
			fmt.Printf("  [WARN] could not start %s: %v\n", svc, err)
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
func createDatabase(svc, name string) error {
	switch svc {
	case "mysql":
		cmd := exec.Command("podman", "exec", "lerd-mysql", "mysql", "-uroot", "-plerd",
			"-e", fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`;", name))
		cmd.Stderr = os.Stderr
		return cmd.Run()
	case "postgres":
		cmd := exec.Command("podman", "exec", "lerd-postgres", "psql", "-U", "postgres",
			"-c", fmt.Sprintf(`CREATE DATABASE "%s";`, name))
		out, err := cmd.CombinedOutput()
		if err != nil && !strings.Contains(string(out), "already exists") {
			return fmt.Errorf("%s", strings.TrimSpace(string(out)))
		}
		return nil
	default:
		return nil
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
	if err := ensureServiceQuadlet(name); err != nil {
		return err
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

// copyEnvFile copies src to dst with 0644 permissions.
func copyEnvFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}
