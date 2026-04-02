package cli

import (
	"bufio"
	crand "crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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

	// Load .lerd.yaml service hints so we can apply env vars for services
	// listed there even when they are not yet referenced in the env file.
	lerdYAMLServices := map[string]bool{}
	if proj, projErr := config.LoadProjectConfig(cwd); projErr == nil {
		for _, svc := range proj.Services {
			lerdYAMLServices[svc.Name] = true
		}
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
			detectedFromEnv := ok && detector(envMap)
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
		for k, v := range reverbEnvUpdates(envMap, site.Domain, site.Secured, cwd) {
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
		updates["REVERB_SERVER_PORT"] = strconv.Itoa(assignReverbServerPort(sitePath))
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

// assignReverbServerPort returns the port Reverb should listen on for the site at sitePath.
// It scans all other linked sites and picks the lowest port >= 8080 not already in use.
// A site's port is read from REVERB_SERVER_PORT in its .env; if absent but the site's Reverb
// unit is active, 8080 is assumed (pre-fix sites that started without an explicit port).
func assignReverbServerPort(sitePath string) int {
	used := map[int]bool{}
	if reg, err := config.LoadSites(); err == nil {
		for _, s := range reg.Sites {
			if filepath.Clean(s.Path) == filepath.Clean(sitePath) {
				continue
			}
			if v := envfile.ReadKey(filepath.Join(s.Path, ".env"), "REVERB_SERVER_PORT"); v != "" {
				if p, err := strconv.Atoi(v); err == nil {
					used[p] = true
				}
			} else {
				// No REVERB_SERVER_PORT in .env — if Reverb is actively running for this site
				// it must be on the default 8080.
				if status, _ := podman.UnitStatus("lerd-reverb-" + s.Name); status == "active" {
					used[8080] = true
				}
			}
		}
	}
	port := 8080
	for used[port] {
		port++
	}
	return port
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
