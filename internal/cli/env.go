package cli

import (
	"bufio"
	crand "crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
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

	isLaravel := fwName == "laravel"

	envRelPath, envFormat := fw.Env.Resolve(cwd)
	envPath := filepath.Join(cwd, envRelPath)

	exampleRelPath := fw.Env.ExampleFile
	if exampleRelPath == "" {
		exampleRelPath = ".env.example"
	}
	examplePath := filepath.Join(cwd, exampleRelPath)

	// 1. Create env file from example if it doesn't exist
	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		if _, err := os.Stat(examplePath); os.IsNotExist(err) {
			return fmt.Errorf("no %s or %s found in %s", envRelPath, exampleRelPath, cwd)
		}
		fmt.Printf("Creating %s from %s...\n", envRelPath, exampleRelPath)
		if err := copyEnvFile(examplePath, envPath); err != nil {
			return fmt.Errorf("copying %s: %w", exampleRelPath, err)
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
				updates[k] = applySiteHandle(v, dbName)
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
				updates["AWS_BUCKET"] = dbName
				updates["AWS_URL"] = "http://localhost:9000/" + dbName
				if err := ensureServiceRunning(svc); err != nil {
					fmt.Printf("  [WARN] could not start %s: %v\n", svc, err)
				} else {
					created, err := createS3Bucket(dbName)
					if err != nil {
						fmt.Printf("  [WARN] could not create bucket %q: %v\n", dbName, err)
					} else if created {
						fmt.Printf("  Created bucket %q\n", dbName)
					} else {
						fmt.Printf("  Bucket %q already exists\n", dbName)
					}
				}
				continue
			}

			if err := ensureServiceRunning(svc); err != nil {
				fmt.Printf("  [WARN] could not start %s: %v\n", svc, err)
			}
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

	// 3c. Generate REVERB_ env vars if BROADCAST_CONNECTION=reverb (Laravel only)
	if isLaravel && strings.ToLower(strings.Trim(envMap["BROADCAST_CONNECTION"], `"'`)) == "reverb" {
		fmt.Println("  Detected reverb     — configuring REVERB_ connection values")
		for k, v := range reverbEnvUpdates(envMap, site.Domain, site.Secured) {
			updates[k] = v
		}
	}

	// 4. Set the URL key to the registered .test domain
	urlKey := fw.Env.URLKey
	if urlKey == "" {
		urlKey = "APP_URL"
	}
	if url := siteURL(cwd); url != "" {
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

	// 6. Generate APP_KEY if missing or empty (Laravel only)
	if isLaravel && strings.TrimSpace(envMap["APP_KEY"]) == "" {
		fmt.Println("  Generating APP_KEY...")
		if err := artisanIn(cwd, "key:generate"); err != nil {
			fmt.Printf("  [WARN] key:generate failed: %v\n", err)
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
func createDatabase(svc, name string) (bool, error) {
	switch svc {
	case "mysql":
		// Query row count before and after to detect whether the DB was created.
		check := exec.Command(podman.PodmanBin(), "exec", "lerd-mysql", "mysql", "-uroot", "-plerd",
			"-sNe", fmt.Sprintf("SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name='%s';", name))
		out, err := check.Output()
		if err == nil && strings.TrimSpace(string(out)) != "0" {
			return false, nil
		}
		cmd := exec.Command(podman.PodmanBin(), "exec", "lerd-mysql", "mysql", "-uroot", "-plerd",
			"-e", fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`;", name))
		cmd.Stderr = os.Stderr
		return true, cmd.Run()
	case "postgres":
		cmd := exec.Command(podman.PodmanBin(), "exec", "lerd-postgres", "psql", "-U", "postgres",
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

// createS3Bucket creates a bucket for the given name in lerd-rustfs using an ephemeral mc container.
// Returns (true, nil) if created, (false, nil) if it already existed, or (false, err) on failure.
func createS3Bucket(name string) (bool, error) {
	const (
		alias   = "lerd"
		mcImage = "docker.io/minio/mc:latest"
		mcEnv   = "MC_HOST_lerd=http://lerd:lerdpassword@lerd-rustfs:9000"
	)

	lsCmd := exec.Command(podman.PodmanBin(), "run", "--rm", "--network", "lerd",
		"-e", mcEnv, mcImage, "ls", alias+"/"+name)
	if err := lsCmd.Run(); err == nil {
		return false, nil
	}

	mbCmd := exec.Command(podman.PodmanBin(), "run", "--rm", "--network", "lerd",
		"-e", mcEnv, mcImage, "mb", alias+"/"+name)
	if out, err := mbCmd.CombinedOutput(); err != nil {
		return false, fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}

	pubCmd := exec.Command(podman.PodmanBin(), "run", "--rm", "--network", "lerd",
		"-e", mcEnv, mcImage, "anonymous", "set", "public", alias+"/"+name)
	if out, err := pubCmd.CombinedOutput(); err != nil {
		return false, fmt.Errorf("mc anonymous set public: %s", strings.TrimSpace(string(out)))
	}
	return true, nil
}

// ensureServiceRunning starts the service if it is not already active.
func ensureServiceRunning(name string) error {
	unit := "lerd-" + name
	// On macOS, UnitStatus returns "active" only after launchd has kicked the
	// podman run command; the container itself may still be starting. Use
	// ContainerRunning as the authoritative check.
	if running, _ := podman.ContainerRunning(unit); running {
		return waitForServiceReady(name, unit)
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
	if err := services.Mgr.Start(unit); err != nil {
		return err
	}
	// Wait up to 15s for the container to actually be running.
	for range 30 {
		time.Sleep(500 * time.Millisecond)
		if running, _ := podman.ContainerRunning(unit); running {
			return waitForServiceReady(name, unit)
		}
	}
	return fmt.Errorf("timed out waiting for %s container to start", unit)
}

// waitForServiceReady waits for the service inside the container to be ready to
// accept connections. For databases this is essential — the container may be
// running but MySQL/Postgres can take additional seconds to initialise.
func waitForServiceReady(name, container string) error {
	var readyCmd func() *exec.Cmd
	switch name {
	case "mysql":
		readyCmd = func() *exec.Cmd {
			return exec.Command(podman.PodmanBin(), "exec", container,
				"mysqladmin", "ping", "-h", "127.0.0.1", "-uroot", "-plerd", "--silent")
		}
	case "postgres":
		readyCmd = func() *exec.Cmd {
			return exec.Command(podman.PodmanBin(), "exec", container,
				"pg_isready", "-U", "postgres")
		}
	default:
		return nil // no readiness probe for other services
	}
	// Poll up to 60s for the service to be ready.
	for range 120 {
		time.Sleep(500 * time.Millisecond)
		if readyCmd().Run() == nil {
			return nil
		}
	}
	return fmt.Errorf("%s started but is not accepting connections after 60s", name)
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

	cmd := exec.Command(podman.PodmanBin(), cmdArgs...)
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
	cmd := exec.Command(podman.PodmanBin(), "exec", container, "sh", "-c", script)
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
// Connection values (HOST, PORT, SCHEME) are always set from the site's domain and TLS state
// so that Vite and the browser can reach Reverb via the nginx WebSocket proxy.
func reverbEnvUpdates(envMap map[string]string, domain string, secured bool) map[string]string {
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

	// Connection values are derived from the site — always update so they stay in sync
	// with the site's domain and TLS state.
	scheme := "http"
	port := "80"
	if secured {
		scheme = "https"
		port = "443"
	}
	updates["REVERB_HOST"] = domain
	updates["REVERB_PORT"] = port
	updates["REVERB_SCHEME"] = scheme

	// VITE_ vars mirror the connection values so the browser can connect via nginx.
	appKey := envMap["REVERB_APP_KEY"]
	if v, ok := updates["REVERB_APP_KEY"]; ok {
		appKey = v
	}
	updates["VITE_REVERB_APP_KEY"] = appKey
	updates["VITE_REVERB_HOST"] = domain
	updates["VITE_REVERB_PORT"] = port
	updates["VITE_REVERB_SCHEME"] = scheme

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
