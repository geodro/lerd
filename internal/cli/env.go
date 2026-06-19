package cli

import (
	"bufio"
	crand "crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	neturl "net/url"

	"github.com/charmbracelet/huh"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
	"github.com/geodro/lerd/internal/feedback"
	gitpkg "github.com/geodro/lerd/internal/git"
	"github.com/geodro/lerd/internal/grouping"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/serviceops"
	"github.com/spf13/cobra"
)

// hostProxyLoopback is the address a host-proxy app uses to reach lerd services.
// Host-proxy apps run on the host, off the podman bridge, so they reach services
// through the loopback ports those services publish rather than container DNS.
const hostProxyLoopback = "127.0.0.1"

// rewriteEnvForHostProxy adapts lerd's computed service connection values for a
// host-proxy app. Bare "lerd-*" hostnames become 127.0.0.1, and *_PORT values
// map from the container port to the service's published host port (e.g. mariadb
// 3306 -> 3411). Containerised sites keep the container DNS names untouched.
func rewriteEnvForHostProxy(updates map[string]string, serviceNames []string) {
	// Sort so a container port shared by two services (e.g. mysql and mariadb
	// both on 3306) resolves to a stable host port rather than whichever map
	// iteration happened to land last.
	names := append([]string(nil), serviceNames...)
	sort.Strings(names)
	containerToHost := map[string]string{}
	for _, name := range names {
		for _, mapping := range servicePortMappings(name) {
			if host, container, ok := splitHostContainerPort(mapping); ok {
				if _, seen := containerToHost[container]; !seen {
					containerToHost[container] = host
				}
			}
		}
	}
	applyHostProxyEnv(updates, containerToHost)
}

// hostProxyConnKey reports whether an env key names a service connection target
// (a host, port, URL, DSN, or endpoint). The host-proxy rewrite only touches
// these so an unrelated value that happens to contain a "lerd-" token (e.g.
// APP_NAME=lerd-demo) is never mangled into 127.0.0.1.
func hostProxyConnKey(k string) bool {
	for _, suf := range []string{"_HOST", "_PORT", "_URL", "_DSN", "_ENDPOINT", "_SERVER"} {
		if strings.HasSuffix(k, suf) {
			return true
		}
	}
	return false
}

// lerdContainerHostRe matches a lerd container hostname with an optional
// trailing :port, both as a bare value (DB_HOST=lerd-redis) and embedded in a
// connection string (MONGO_DSN=mongodb://root:pw@lerd-mongo:27017/db).
var lerdContainerHostRe = regexp.MustCompile(`lerd-[a-z0-9-]+(?::\d+)?`)

// applyHostProxyEnv is the pure rewrite step: every "lerd-<name>[:port]" token
// (bare or inside a URL) becomes loopback with the service's published host
// port, and a discrete *_PORT value with no host alongside is remapped too.
// Split from rewriteEnvForHostProxy so the logic is testable without services.
func applyHostProxyEnv(updates, containerToHost map[string]string) {
	for k, v := range updates {
		if !hostProxyConnKey(k) {
			continue
		}
		nv := lerdContainerHostRe.ReplaceAllStringFunc(v, func(m string) string {
			_, port, found := strings.Cut(m, ":")
			if !found {
				return hostProxyLoopback
			}
			if mapped, ok := containerToHost[port]; ok {
				port = mapped
			}
			return hostProxyLoopback + ":" + port
		})
		if nv != v {
			updates[k] = nv
			continue
		}
		// No host token to anchor on: a standalone *_PORT (e.g. DB_PORT=3306)
		// still needs remapping to its published host port.
		if strings.HasSuffix(k, "_PORT") {
			if mapped, ok := containerToHost[v]; ok {
				updates[k] = mapped
			}
		}
	}
}

// servicePortMappings returns the "host:container" port mappings a service
// publishes. The installed/custom service is consulted first so a pinned or
// non-canonical version reports its real published port; the default preset is
// the fallback for services not separately registered.
func servicePortMappings(name string) []string {
	if svc, err := config.LoadCustomService(name); err == nil && len(svc.Ports) > 0 {
		return svc.Ports
	}
	if svc, err := config.DefaultPresetMeta(name); err == nil && len(svc.Ports) > 0 {
		return svc.Ports
	}
	return nil
}

// splitHostContainerPort parses a podman port mapping ("3411:3306", or
// "127.0.0.1:3411:3306" with an optional "/tcp" suffix) into host and container.
func splitHostContainerPort(mapping string) (host, container string, ok bool) {
	parts := strings.Split(mapping, ":")
	if len(parts) < 2 {
		return "", "", false
	}
	host = parts[len(parts)-2]
	container = strings.SplitN(parts[len(parts)-1], "/", 2)[0]
	if host == "" || container == "" {
		return "", "", false
	}
	return host, container, true
}

// projectDBName returns a safe database name for the project at path.
// It uses the registered site name, falling back to the directory name,
// converting hyphens to underscores.
func projectDBName(path string) string {
	name := filepath.Base(path)
	if reg, err := config.LoadSites(); err == nil {
		for _, s := range reg.Sites {
			if s.Path == path {
				// A shared-DB group secondary uses the group main's database, so
				// env setup must not reset it to the secondary's own slug.
				if shared, ok := grouping.SharedDBNameFor(&s); ok {
					return shared
				}
				name = s.Name
				break
			}
		}
	}
	return config.SiteSlug(name)
}

// NewEnvCmd returns the env command.
func NewEnvCmd() *cobra.Command {
	var verbose bool
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Configure .env for this project with lerd service connection settings",
		Long: `Sets up .env for the current project:
  - Creates .env from .env.example if it does not exist
  - Detects which services the project uses and sets lerd connection values
  - Starts any referenced services that are not already running
  - Generates APP_KEY if missing
  - Sets APP_URL to the registered .test domain`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// --verbose (and any non-interactive output, e.g. the MCP server or a
			// pipe) prints the full per-step detail. An interactive terminal gets
			// the single live "configuring .env" line instead, even under NO_COLOR
			// where it degrades to a plain condensed line rather than verbose.
			if verbose || !feedback.Interactive() {
				return runEnv(cmd, args)
			}
			feedback.Begin()
			return runEnvLive(cmd, args)
		},
	}
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Print detailed per-service output (used by the MCP server)")
	return cmd
}

// envLive, when set, redirects runEnv's per-service reporting to a single live
// progress line instead of the detailed multi-line output.
var envLive *feedback.Live

// runEnvLive runs runEnv under a live "configuring .env" line that accumulates
// each service as it is applied. It saves and restores the previous live line
// so it is reentrant: `lerd env` in an unlinked dir links first, and that link
// runs its own setup env step (another runEnvLive) — nilling the global here
// instead of restoring it would crash the outer line's Done/Fail on return.
func runEnvLive(cmd *cobra.Command, args []string) error {
	prev := envLive
	live := feedback.StartLive("configuring .env")
	envLive = live
	err := runEnv(cmd, args)
	if err != nil {
		live.Fail(err)
	} else {
		live.Done()
	}
	envLive = prev
	return err
}

// envInfo prints a detailed runEnv line, suppressed while the live line is active.
func envInfo(format string, a ...any) {
	if envLive == nil {
		fmt.Printf(format, a...)
	}
}

// envInterrupt runs fn, pausing the live line first (when active) so fn's raw
// stdout — a service-start notice, a child process's output — prints cleanly
// above the spinner instead of fighting its in-place redraw. With no live line
// it just runs fn, so callers reached outside runEnvLive are unaffected.
func envInterrupt(fn func()) {
	if envLive != nil {
		envLive.Interrupt(fn)
		return
	}
	fn()
}

// envApplyLine reports a service as its connection values are applied: it feeds
// the live line when active, otherwise prints the detailed "applying" line.
func envApplyLine(svc string, detectedFromEnv bool) {
	if envLive != nil {
		envLive.Add(svc)
		return
	}
	if detectedFromEnv {
		fmt.Printf("  Detected %-12s — applying lerd connection values\n", svc)
	} else {
		fmt.Printf("  From .lerd.yaml %-4s — applying lerd connection values\n", svc)
	}
}

// userPickedDBFromYAML returns true when the user has named any database
// service in .lerd.yaml: sqlite, the built-in mysql/postgres, or a custom DB
// family alternate (mysql-5-6, mariadb-11, postgres-14, mongo-6, …). This
// signal is what lets us replace whatever the existing .env says about
// DB_CONNECTION with the user's actual pick.
func userPickedDBFromYAML(lerdYAMLServices map[string]bool) bool {
	if lerdYAMLServices["sqlite"] || lerdYAMLServices["mysql"] || lerdYAMLServices["postgres"] {
		return true
	}
	for name := range lerdYAMLServices {
		switch config.FamilyOfName(name) {
		case "mysql", "mariadb", "postgres", "mongo":
			return true
		}
	}
	return false
}

// shouldApplyService decides whether to apply env vars for svc. A built-in
// DB service that wasn't explicitly picked is skipped when the user picked a
// different DB, so a fresh-Laravel DB_CONNECTION=sqlite never re-imprints
// itself when the wizard selected mysql. The built-in redis is likewise
// skipped when the project picked valkey. Otherwise apply when the env file
// references the service or .lerd.yaml lists it.
func shouldApplyService(svc string, detectedFromEnv, pickedFromYAML, userPickedDB, valkeyPicked bool) bool {
	if userPickedDB && (svc == "mysql" || svc == "postgres") && !pickedFromYAML {
		return false
	}
	// Valkey writes the same REDIS_* keys, so a redis-shaped .env re-detects
	// the built-in redis on every later run; skip it so valkey projects don't
	// also boot a redundant redis container.
	if svc == "redis" && valkeyPicked && !pickedFromYAML {
		return false
	}
	return detectedFromEnv || pickedFromYAML
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

// projectDBIsMySQLFamily reports whether the project's database is MySQL or
// MariaDB — the families that support host access over a unix socket. Gates
// the db.external host-database mode, which is MySQL/MariaDB-only for now.
func projectDBIsMySQLFamily(proj *config.ProjectConfig) bool {
	if proj.DB.Service != "" {
		fam := config.FamilyOfName(proj.DB.Service)
		return fam == "mysql" || fam == "mariadb"
	}
	for _, s := range proj.Services {
		if config.IsDBServiceName(s.Name) {
			fam := config.FamilyOfName(s.Name)
			return fam == "mysql" || fam == "mariadb"
		}
	}
	// Unspecified DB → assume MySQL, since db.external is the host-MySQL feature.
	return true
}

// applyHostDBExternalEnv translates a project's committed db.external host-database
// choice into the same effect as LERD_EXTERNAL_SERVICES + a DB_HOST/DB_SOCKET
// override. For a MySQL/MariaDB project it marks the DB service(s) externally
// managed in extServices (so runEnv skips ensureServiceRunning/createDatabase — no
// lerd-mysql container) and layers the host unix-socket connection vars into
// envOverrides (which are applied last, so they win over the framework's default
// DB_HOST=lerd-mysql). envMap is the parsed .env, read only to preserve any host
// DB credentials the user already set. Returns true when it applied (so the caller
// can print the notice); a no-op (returns false) unless the project sets
// db.external on a MySQL/MariaDB database.
func applyHostDBExternalEnv(proj *config.ProjectConfig, envMap map[string]string, extServices map[string]bool, envOverrides map[string]string) bool {
	if proj == nil || !proj.DB.External || !projectDBIsMySQLFamily(proj) {
		return false
	}
	// Mark the DB service externally managed. "mysql"/"mariadb" cover the keys the
	// framework service loops consult; the project's own DB service names cover the
	// custom-services branch and family alternates (e.g. mariadb-11).
	extServices["mysql"] = true
	extServices["mariadb"] = true
	if proj.DB.Service != "" {
		extServices[strings.ToLower(proj.DB.Service)] = true
	}
	for _, s := range proj.Services {
		if config.IsDBServiceName(s.Name) {
			extServices[strings.ToLower(s.Name)] = true
		}
	}
	envOverrides["DB_HOST"] = "localhost"
	envOverrides["DB_SOCKET"] = proj.DB.HostSocketPath()
	// Clear DB_PORT: the connection is over the unix socket, so a container port
	// lingering from the framework's default DB vars is misleading and could push
	// non-socket tooling onto TCP against a wrong port.
	envOverrides["DB_PORT"] = ""
	// Don't let the container's default root/lerd creds clobber the host
	// credentials: preserve whatever the .env already has for these keys.
	if v, ok := envMap["DB_USERNAME"]; ok {
		envOverrides["DB_USERNAME"] = v
	}
	if v, ok := envMap["DB_PASSWORD"]; ok {
		envOverrides["DB_PASSWORD"] = v
	}
	return true
}

func runEnv(_ *cobra.Command, _ []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	// Determine framework-specific env file path and format
	site, err := ensureSiteForCwd()
	if err != nil {
		return err
	}

	fwName := site.Framework
	if fwName == "" {
		fwName, _ = config.DetectFrameworkForDir(cwd)
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
			envInfo("Creating %s from %s...\n", envRelPath, exampleRelPath)
			if err := copyEnvFile(examplePath, envPath); err != nil {
				return fmt.Errorf("copying %s: %w", exampleRelPath, err)
			}
		} else if len(fw.Env.Services) > 0 {
			// No env or example file, but framework defines services — create
			// an empty env file so service detection can populate it.
			if dir := filepath.Dir(envPath); dir != "." {
				_ = os.MkdirAll(dir, 0755)
			}
			envInfo("Creating empty %s (no example file found)...\n", envRelPath)
			if err := os.WriteFile(envPath, []byte(""), 0644); err != nil {
				return fmt.Errorf("creating %s: %w", envRelPath, err)
			}
		} else {
			return fmt.Errorf("no %s or %s found in %s", envRelPath, exampleRelPath, cwd)
		}
	} else {
		envInfo("Updating existing %s...\n", envRelPath)
		// Back up the original .env the first time lerd modifies it (so the user
		// can inspect what changed and restore with `lerd env:restore`), but only
		// if lerd hasn't already written to it — detected by presence of the word
		// "lerd" in the file (e.g. DB_HOST=lerd-mysql).
		backupPath := filepath.Join(cwd, ".env.before_lerd")
		if !envFileHasLerd(envPath) {
			if _, err := os.Stat(backupPath); os.IsNotExist(err) {
				if err := copyEnvFile(envPath, backupPath); err != nil {
					feedback.Warn("could not back up %s: %v", envRelPath, err)
				} else {
					envInfo("  Backed up original %s → .env.before_lerd\n", envRelPath)
					addToGitignore(cwd, ".env.before_lerd")
				}
			}
		}
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
		bucket: s3BucketName(dbName),
		domain: site.PrimaryDomain(),
		scheme: scheme,
	}

	// Framework default env vars: seeded only when the key is absent, so a value
	// the user set directly survives re-runs (link/secure/domain/db:move). The
	// detected-service values and .env.lerd_override still win and can force one.
	for _, kv := range fw.Env.Vars {
		k, v, _ := strings.Cut(kv, "=")
		if _, present := envMap[k]; present {
			continue
		}
		val := applySiteHandle(v, tplCtx)
		updates[k] = val
		envInfo("  Setting %s=%s\n", k, val)
	}

	// Load .lerd.yaml service hints so we can apply env vars for services
	// listed there even when they are not yet referenced in the env file.
	lerdYAMLServices := map[string]bool{}
	if proj, projErr := config.LoadProjectConfig(cwd); projErr == nil {
		for _, svc := range proj.Services {
			lerdYAMLServices[svc.Name] = true
		}
	}

	// Personal, gitignored per-project overrides (.env.lerd_override). envOverrides
	// are layered on last so they win over every computed value; extServices names
	// services lerd writes connection vars for but must not start or provision.
	envOverrides, extServices := readEnvOverride(cwd)
	if _, statErr := os.Stat(filepath.Join(cwd, envOverrideFile)); statErr == nil {
		ensureOverrideGitignored(cwd)
	}

	// First-class committed host-database mode: .lerd.yaml `db.external: true`
	// is the shareable equivalent of LERD_EXTERNAL_SERVICES + a DB_HOST/DB_SOCKET
	// override (see applyHostDBExternalEnv).
	if proj, _ := config.LoadProjectConfig(cwd); applyHostDBExternalEnv(proj, envMap, extServices, envOverrides) {
		fmt.Printf("  Host MySQL (db.external) — connecting via socket %s; not starting lerd-mysql\n", proj.DB.HostSocketPath())
	}

	// Laravel ships .env / .env.example with DB_CONNECTION=sqlite. If the user
	// hasn't yet picked a DB service for this project, offer to swap sqlite for
	// a lerd-managed mysql/postgres. Skipped for frameworks with explicit env
	// service rules (e.g. wordpress, symfony) — they don't use DB_CONNECTION.
	// Non-interactive callers (MCP, scripts) fall through to sqlite by default
	// so they don't hit a 500 from the missing .sqlite file; the user can
	// still switch later with `lerd db set mysql` or the db_set MCP tool.
	if len(fw.Env.Services) == 0 &&
		!userPickedDBFromYAML(lerdYAMLServices) && !externalDBPicked(extServices) &&
		strings.EqualFold(strings.TrimSpace(envMap["DB_CONNECTION"]), "sqlite") {

		dbChoice := "sqlite"
		if isInteractive() {
			options, _ := buildDatabaseOptions()
			dbForm := huh.NewForm(huh.NewGroup(
				huh.NewSelect[string]().
					Title("Database").
					Description(envRelPath + " uses SQLite. Use a lerd-managed database service instead?").
					Options(options...).
					Value(&dbChoice),
			)).WithTheme(huh.ThemeCatppuccin())
			var formErr error
			envInterrupt(func() { formErr = dbForm.Run() })
			if formErr != nil {
				return fmt.Errorf("database prompt: %w", formErr)
			}
		} else {
			envInfo("  Defaulting to SQLite (non-interactive). Run `lerd db set <service>` or call db_set to switch.\n")
		}

		// Persist the choice to .lerd.yaml so future runs don't re-ask, and
		// flip the in-memory map so the service loop below picks it up.
		proj, _ := config.LoadProjectConfig(cwd)
		if proj == nil {
			proj = &config.ProjectConfig{}
		}
		proj.Services = append(proj.Services, config.ProjectService{Name: dbChoice})
		if err := config.SaveProjectConfig(cwd, proj); err != nil {
			feedback.Warn("could not save .lerd.yaml: %v", err)
		}
		lerdYAMLServices[dbChoice] = true
	}

	userPickedDB := userPickedDBFromYAML(lerdYAMLServices) || externalDBPicked(extServices)
	valkeyPicked := lerdYAMLServices["valkey"]

	if len(fw.Env.Services) > 0 {
		// Framework defines its own service detection and vars — use those.
		// A service applies when its env_detect rule matches the existing env
		// file OR it is listed in .lerd.yaml. The .lerd.yaml hint is what
		// lets `lerd init` swap a fresh Laravel project from sqlite to mysql:
		// the env file still says DB_CONNECTION=sqlite, so detection misses,
		// but the user picked mysql in the wizard.
		for svc, def := range fw.Env.Services {
			detectedFromEnv := frameworkServiceDetected(def, envMap)
			pickedFromYAML := lerdYAMLServices[svc] || extServices[svc]

			if !shouldApplyService(svc, detectedFromEnv, pickedFromYAML, userPickedDB, valkeyPicked) {
				continue
			}

			envApplyLine(svc, detectedFromEnv)
			isDB := svc == "mysql" || svc == "postgres"
			for _, kv := range def.Vars {
				k, v, _ := strings.Cut(kv, "=")
				updates[k] = applySiteHandle(v, tplCtx)
			}
			if externalManaged(svc, extServices) {
				continue
			}
			if isDB {
				if err := ensureServiceRunning(svc); err != nil {
					feedback.Warn("could not start %s: %v", svc, err)
				} else {
					for _, name := range []string{dbName, dbName + "_testing"} {
						created, err := createDatabase(svc, name)
						if err != nil {
							feedback.Warn("could not create database %q: %v", name, err)
						} else if created {
							envInfo("  Created database %q\n", name)
						} else {
							envInfo("  Database %q already exists\n", name)
						}
					}
				}
				continue
			}
			if svc == "rustfs" {
				// Always sanitise through s3BucketName: rustfs/S3 reject underscores,
				// uppercase, etc. A historical invalid value in .env (from an older
				// lerd, a Sail import, or manual edit) gets auto-healed on this run.
				bucketName := s3BucketName(envMap["AWS_BUCKET"])
				if bucketName == "lerd" {
					bucketName = s3BucketName(dbName)
				}
				updates["AWS_BUCKET"] = bucketName
				updates["AWS_URL"] = "http://localhost:9000/" + bucketName
				if err := ensureServiceRunning(svc); err != nil {
					feedback.Warn("could not start %s: %v", svc, err)
				}
				created, err := createS3Bucket(bucketName)
				if err != nil {
					feedback.Warn("could not create bucket %q: %v", bucketName, err)
				} else if created {
					envInfo("  Created bucket %q\n", bucketName)
				} else {
					envInfo("  Bucket %q already exists\n", bucketName)
				}
				continue
			}
			if err := ensureServiceRunning(svc); err != nil {
				feedback.Warn("could not start %s: %v", svc, err)
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
		for _, svc := range knownServices() {
			detector, ok := serviceDetectors[svc]
			detectedFromEnv := ok && detector(envMap)
			pickedFromYAML := lerdYAMLServices[svc] || extServices[svc]

			if !shouldApplyService(svc, detectedFromEnv, pickedFromYAML, userPickedDB, valkeyPicked) {
				continue
			}

			envs := serviceEnvVars(svc)
			if len(envs) == 0 {
				continue
			}

			envApplyLine(svc, detectedFromEnv)
			for _, kv := range envs {
				k, v, _ := strings.Cut(kv, "=")
				updates[k] = v
			}

			isDB := svc == "mysql" || svc == "postgres"
			if isDB {
				updates["DB_DATABASE"] = dbName
			}
			if externalManaged(svc, extServices) {
				continue
			}

			if isDB {
				if err := ensureServiceRunning(svc); err != nil {
					feedback.Warn("could not start %s: %v", svc, err)
				} else {
					for _, name := range []string{dbName, dbName + "_testing"} {
						created, err := createDatabase(svc, name)
						if err != nil {
							feedback.Warn("could not create database %q: %v", name, err)
						} else if created {
							envInfo("  Created database %q\n", name)
						} else {
							envInfo("  Database %q already exists\n", name)
						}
					}
				}
				continue
			}

			if svc == "rustfs" {
				// Always sanitise through s3BucketName: rustfs/S3 reject underscores,
				// uppercase, etc. A historical invalid value in .env (from an older
				// lerd, a Sail import, or manual edit) gets auto-healed on this run.
				bucketName := s3BucketName(envMap["AWS_BUCKET"])
				if bucketName == "lerd" {
					bucketName = s3BucketName(dbName)
				}
				updates["AWS_BUCKET"] = bucketName
				updates["AWS_URL"] = "http://localhost:9000/" + bucketName
				if err := ensureServiceRunning(svc); err != nil {
					feedback.Warn("could not start %s: %v", svc, err)
				}
				// Always attempt bucket creation — ensureServiceRunning may have
				// timed out on the host probe while the container network is already
				// up, or rustfs was already running before lerd env ran.
				created, err := createS3Bucket(bucketName)
				if err != nil {
					feedback.Warn("could not create bucket %q: %v", bucketName, err)
				} else if created {
					envInfo("  Created bucket %q\n", bucketName)
				} else {
					envInfo("  Bucket %q already exists\n", bucketName)
				}
				continue
			}

			if err := ensureServiceRunning(svc); err != nil {
				feedback.Warn("could not start %s: %v", svc, err)
			}
		}
	}

	// 3a-bis. SQLite is not a containerized service but is a valid choice from
	// the init wizard / runtime DB prompt. When listed in .lerd.yaml, apply the
	// standard Laravel sqlite env vars and ensure the database file exists so
	// migrations can run immediately. No service to start, no SQL DB to create.
	if lerdYAMLServices["sqlite"] {
		envApplyLine("sqlite", false)
		for _, kv := range serviceEnvVars("sqlite") {
			k, v, _ := strings.Cut(kv, "=")
			updates[k] = v
		}
		sqlitePath := filepath.Join(cwd, "database", "database.sqlite")
		if _, statErr := os.Stat(sqlitePath); os.IsNotExist(statErr) {
			if err := os.MkdirAll(filepath.Dir(sqlitePath), 0o755); err == nil {
				if f, err := os.Create(sqlitePath); err == nil {
					_ = f.Close()
					envInfo("  Created %s\n", filepath.Join("database", "database.sqlite"))
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
		pickedFromYAML := lerdYAMLServices[svc.Name] || extServices[svc.Name]
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
			// project can reach it once running, unless it's externally managed.
			if !extServices[svc.Name] {
				if err := ensureServiceRunning(svc.Name); err != nil {
					feedback.Warn("could not start %s: %v", svc.Name, err)
				}
			}
			continue
		}
		envApplyLine(svc.Name, !pickedFromYAML)
		for _, kv := range svc.EnvVars {
			k, v, _ := strings.Cut(kv, "=")
			updates[k] = applySiteHandle(v, tplCtx)
		}
		family := config.FamilyOf(svc)
		isDB := family == "mysql" || family == "mariadb" || family == "postgres"
		if isDB {
			updates["DB_DATABASE"] = dbName
		}
		if externalManaged(svc.Name, extServices) {
			continue
		}
		if err := ensureServiceRunning(svc.Name); err != nil {
			feedback.Warn("could not start %s: %v", svc.Name, err)
			continue
		}
		if isDB {
			for _, name := range []string{dbName, dbName + "_testing"} {
				created, err := createDatabase(svc.Name, name)
				if err != nil {
					feedback.Warn("could not create database %q: %v", name, err)
				} else if created {
					envInfo("  Created database %q\n", name)
				} else {
					envInfo("  Database %q already exists\n", name)
				}
			}
		}
		if svc.SiteInit != nil && svc.SiteInit.Exec != "" {
			envInterrupt(func() { runSiteInit(svc, tplCtx) })
		}
	}

	// 3c. Patch DuskTestCase.php for lerd's Selenium container if applicable.
	if _, hasDriver := updates["DUSK_DRIVER_URL"]; hasDriver {
		patchDuskTestCase(cwd)
	}

	// 3c-bis. Pest browser testing drives Playwright in-container, which the
	// Selenium preset does not provide. Surface the one-time setup command when
	// the plugin is present but chromium isn't baked into the FPM image yet.
	if config.ComposerHasPackage(cwd, "pestphp/pest-plugin-browser") {
		if v, derr := phpDet.DetectVersion(cwd); derr == nil && pestBrowserSupportedVersion(v) == nil {
			if gcfg, cerr := config.LoadGlobal(); cerr == nil && !slices.Contains(gcfg.GetPackages(v), pestBrowserPkg) {
				envInfo("  Detected pest-plugin-browser — run `lerd pest:browser install` to enable in-container browser testing\n")
			}
		}
	}

	// 3d. Generate REVERB_ env vars if a worker with proxy config is detected and
	// BROADCAST_CONNECTION=reverb is set.
	if fw.HasWorker("reverb", cwd) &&
		strings.ToLower(strings.Trim(overrideOr(envOverrides, envMap, "BROADCAST_CONNECTION"), `"'`)) == "reverb" {
		envApplyLine("reverb", true)
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
		envInfo("  Setting %s=%s\n", urlKey, url)
	}

	// 4d. Host-proxy apps run on the host, so point service connections at
	// loopback and the published host ports instead of container DNS names.
	if site.IsHostProxy() {
		names := make([]string, 0, len(lerdYAMLServices))
		for n := range lerdYAMLServices {
			names = append(names, n)
		}
		rewriteEnvForHostProxy(updates, names)
	}

	// 4e. Apply personal .env.lerd_override values last so they win over lerd's
	// defaults and every computed value (DB_DATABASE, APP_URL, reverb, …).
	if len(envOverrides) > 0 {
		envInfo("  Applying %d override(s) from %s\n", len(envOverrides), envOverrideFile)
		for k, v := range envOverrides {
			updates[k] = v
		}
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
	if kg := fw.Env.KeyGeneration; kg != nil && strings.TrimSpace(overrideOr(envOverrides, envMap, kg.EnvKey)) == "" {
		if kg.Command != "" {
			if _, statErr := os.Stat(filepath.Join(cwd, "vendor")); statErr == nil {
				envInfo("  Generating %s...\n", kg.EnvKey)
				// Use the framework's console binary (e.g. "spark" for
				// CodeIgniter), not a hardcoded "artisan", so key generation
				// works for non-Laravel frameworks.
				var keyErr error
				envInterrupt(func() { keyErr = consoleIn(cwd, fw.Console, kg.Command) })
				if keyErr != nil {
					feedback.Warn("%s failed: %v", kg.Command, keyErr)
				}
			} else if kg.FallbackPrefix != "" {
				envInfo("  Generating %s (vendor not installed yet)...\n", kg.EnvKey)
				key := generateRandomKey(kg.FallbackPrefix)
				if err := envfile.ApplyUpdates(envPath, map[string]string{kg.EnvKey: key}); err != nil {
					feedback.Warn("writing %s: %v", kg.EnvKey, err)
				}
			}
		} else if kg.FallbackPrefix != "" {
			envInfo("  Generating %s...\n", kg.EnvKey)
			key := generateRandomKey(kg.FallbackPrefix)
			if err := envfile.ApplyUpdates(envPath, map[string]string{kg.EnvKey: key}); err != nil {
				feedback.Warn("writing %s: %v", kg.EnvKey, err)
			}
		}
	}

	// 7. Worktrees share the parent site's database SERVER (only DB_DATABASE
	// differs, and only when isolated). Their .env was copied once at creation,
	// so after this run realigned the parent to the selected services, mirror
	// the connection coordinates into each worktree .env too — otherwise a
	// worktree captured before a service switch (e.g. postgres -> postgres-18)
	// keeps pointing at a host that no longer resolves.
	alignWorktreeEnvDBConnection(site, envPath, envRelPath, envFormat)

	envInfo("Done.\n")
	return nil
}

// worktreeDBConnectionKeys are the shared database SERVER coordinates a worktree
// inherits from its parent. DB_DATABASE is deliberately excluded: it is owned by
// the isolated-DB logic (or the parent value for a non-isolated worktree).
var worktreeDBConnectionKeys = []string{"DB_CONNECTION", "DB_HOST", "DB_PORT", "DB_USERNAME", "DB_PASSWORD"}

// alignWorktreeEnvDBConnection mirrors the parent's (just-aligned) DB connection
// coordinates into each existing worktree env file. It is the worktree arm of
// `lerd env`'s "make the env match the selected services" guarantee. Best
// effort: a worktree without an env file yet is skipped (it'll be seeded on its
// next sync), and ApplyUpdates no-ops when nothing changed.
//
// Scoped to the dotenv format: the connection keys are Laravel-style and
// php-const frameworks use a different writer and key set, so they are left to
// their own env detection. envRelPath is the framework-resolved env file path so
// a worktree of a framework whose env file isn't ".env" is still targeted.
func alignWorktreeEnvDBConnection(site *config.Site, mainEnvPath, envRelPath, envFormat string) {
	if site == nil || envFormat == "php-const" {
		return
	}
	worktrees, err := gitpkg.DetectWorktrees(site.Path, site.PrimaryDomain())
	if err != nil || len(worktrees) == 0 {
		return
	}
	mainVals := envfile.ReadValues(mainEnvPath)
	coords := map[string]string{}
	for _, k := range worktreeDBConnectionKeys {
		if v := mainVals[k]; v != "" {
			coords[k] = v
		}
	}
	if len(coords) == 0 {
		return
	}
	for _, wt := range worktrees {
		wtEnv := filepath.Join(wt.Path, envRelPath)
		if _, statErr := os.Stat(wtEnv); statErr != nil {
			continue
		}
		// Only touch worktrees whose coordinates actually drifted, so a steady
		// state stays silent and the file's mtime is left alone. One read of the
		// worktree env covers every key compared.
		wtVals := envfile.ReadValues(wtEnv)
		drifted := false
		for k, v := range coords {
			if wtVals[k] != v {
				drifted = true
				break
			}
		}
		if !drifted {
			continue
		}
		if err := envfile.ApplyUpdates(wtEnv, coords); err != nil {
			feedback.Warn("aligning worktree %s .env: %v", wt.Branch, err)
			continue
		}
		envInfo("  Aligned worktree %s DB connection\n", wt.Branch)
	}
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

// CreateDatabase is the exported variant of createDatabase. Used by callers
// outside the cli package (e.g. the worktree DB-isolation flow).
func CreateDatabase(svc, name string) (bool, error) { return serviceops.CreateDatabase(svc, name) }

// CloneDatabase copies the schema and data from src into dst inside the same
// service container. dst must already exist. Returns an error if the family
// has no clone strategy or the dump/restore fails.
func CloneDatabase(svc, src, dst string) error {
	container := "lerd-" + svc
	family := svc
	if inferred := config.FamilyOfName(svc); inferred != "" {
		family = inferred
	}
	switch family {
	case "mysql", "mariadb":
		dumpBin := "mysqldump"
		clientBin := "mysql"
		if family == "mariadb" {
			dumpBin = "mariadb-dump"
			clientBin = "mariadb"
		}
		shellCmd := fmt.Sprintf(
			"%s -uroot -plerd --single-transaction --quick --no-tablespaces %s | %s -uroot -plerd %s",
			dumpBin, src, clientBin, dst,
		)
		cmd := podman.Cmd("exec", container, "sh", "-c", shellCmd)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("clone %s -> %s: %s", src, dst, strings.TrimSpace(string(out)))
		}
		return nil
	case "postgres":
		shellCmd := fmt.Sprintf(`pg_dump -U postgres %s | psql -U postgres -d %s`, src, dst)
		cmd := podman.Cmd("exec", container, "sh", "-c", shellCmd)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("clone %s -> %s: %s", src, dst, strings.TrimSpace(string(out)))
		}
		return nil
	default:
		return fmt.Errorf("clone unsupported for service family %q", family)
	}
}

// DropDatabase delegates to serviceops.DropDatabase for cli-package callers.
func DropDatabase(svc, name string) (bool, error) { return serviceops.DropDatabase(svc, name) }

// createDatabase delegates to serviceops.CreateDatabase. Kept as a package-local
// alias so existing call sites inside the cli package compile unchanged.
func createDatabase(svc, name string) (bool, error) { return serviceops.CreateDatabase(svc, name) }

// s3BucketName delegates to serviceops.S3BucketName.
func s3BucketName(name string) string { return serviceops.S3BucketName(name) }

// createS3Bucket delegates to serviceops.EnsureS3Bucket.
func createS3Bucket(name string) (bool, error) { return serviceops.EnsureS3Bucket(name) }

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
		envInterrupt(func() { fmt.Printf("  Starting %s...\n", name) })
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
		envInterrupt(func() { fmt.Printf("  Starting %s...\n", name) })
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

// consoleExecArgs builds the podman exec args to run a framework console
// command (php <console> <args...>) inside the PHP-FPM container for the given
// PHP version. An empty console defaults to "artisan" for Laravel.
func consoleExecArgs(dir, version, console string, args ...string) []string {
	if console == "" {
		console = "artisan"
	}

	container := fpmContainerForDir(dir, version)

	cmdArgs := []string{"exec", "-i", "-w", dir, container, "php", console}
	return append(cmdArgs, args...)
}

func consoleIn(dir, console string, args ...string) error {
	version, err := phpDet.DetectVersion(dir)
	if err != nil {
		cfg, _ := config.LoadGlobal()
		version = cfg.PHP.DefaultVersion
	}

	cmd := podman.Cmd(consoleExecArgs(dir, version, console, args...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// siteTemplateCtx holds the values available to {{…}} placeholders in
// framework env var templates.
type siteTemplateCtx struct {
	site   string // database / handle name (underscored)
	bucket string // S3-safe bucket name (lowercase, hyphens)
	domain string // primary domain (e.g. myapp.test)
	scheme string // "http" or "https"
}

// applySiteHandle replaces {{site}}, {{site_testing}}, {{bucket}}, {{domain}},
// {{scheme}}, and service version placeholders (e.g. {{mysql_version}}) in s.
func applySiteHandle(s string, ctx siteTemplateCtx) string {
	s = strings.ReplaceAll(s, "{{site}}", ctx.site)
	s = strings.ReplaceAll(s, "{{site_testing}}", ctx.site+"_testing")
	if ctx.bucket != "" {
		s = strings.ReplaceAll(s, "{{bucket}}", ctx.bucket)
	}
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
	cmd := podman.Cmd("exec", container, "sh", "-c", script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		feedback.Warn("site_init for %s failed: %v", svc.Name, err)
	}
}

// NewEnvRestoreCmd returns the env:restore command.
func NewEnvRestoreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "env:restore",
		Short: "Restore .env from the pre-lerd backup (.env.before_lerd)",
		Long: `Restores the .env file from the backup that 'lerd env' created the first
time it was run on this project (.env.before_lerd).

Useful when switching back from lerd to Laravel Sail or another environment.`,
		RunE: runEnvRestore,
	}
}

func runEnvRestore(_ *cobra.Command, _ []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	backupPath := filepath.Join(cwd, ".env.before_lerd")
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf(".env.before_lerd not found — run 'lerd env' first to create a backup")
	}

	envPath := filepath.Join(cwd, ".env")
	if err := copyEnvFile(backupPath, envPath); err != nil {
		return fmt.Errorf("restoring .env: %w", err)
	}
	fmt.Println("Restored .env from .env.before_lerd")
	fmt.Println("Run 'lerd env' again to re-apply lerd connection settings.")
	return nil
}

// addToGitignore appends entry to dir/.gitignore if the file exists and the
// entry is not already present. Failures are silently ignored.
func addToGitignore(dir, entry string) {
	gitignorePath := filepath.Join(dir, ".gitignore")
	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		return // file doesn't exist or can't be read — skip
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == entry {
			return // already present
		}
	}
	content := string(data)
	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += entry + "\n"
	_ = os.WriteFile(gitignorePath, []byte(content), 0644)
}

// envFileHasLerd reports whether path already contains lerd-written values.
// lerd always writes hostnames like "lerd-mysql", "lerd-redis", etc., so a
// simple substring search is sufficient.
func envFileHasLerd(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), "lerd")
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
			feedback.Warn("could not patch DuskTestCase.php: %v", err)
			return
		}
		fmt.Println("  Patched tests/DuskTestCase.php for lerd Selenium")
	}
}
