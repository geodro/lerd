package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/geodro/lerd/internal/certs"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dns"
	"github.com/geodro/lerd/internal/envfile"
	"github.com/geodro/lerd/internal/nginx"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/serviceops"
	"github.com/geodro/lerd/internal/siteinfo"
	"github.com/geodro/lerd/internal/siteops"
	"github.com/geodro/lerd/internal/store"
	lerdSystemd "github.com/geodro/lerd/internal/systemd"
	lerdUpdate "github.com/geodro/lerd/internal/update"
	"github.com/geodro/lerd/internal/version"
)

const protocolVersion = "2024-11-05"

var knownServices = []string{"mysql", "redis", "postgres", "meilisearch", "rustfs", "mailpit"}

// builtinServiceEnv mirrors the serviceEnvVars map in internal/cli/services.go.
// Returns the recommended Laravel .env KEY=VALUE pairs for each built-in service.
var builtinServiceEnv = map[string][]string{
	"mysql": {
		"DB_CONNECTION=mysql",
		"DB_HOST=lerd-mysql",
		"DB_PORT=3306",
		"DB_DATABASE=lerd",
		"DB_USERNAME=root",
		"DB_PASSWORD=lerd",
	},
	"postgres": {
		"DB_CONNECTION=pgsql",
		"DB_HOST=lerd-postgres",
		"DB_PORT=5432",
		"DB_DATABASE=lerd",
		"DB_USERNAME=postgres",
		"DB_PASSWORD=lerd",
	},
	"redis": {
		"REDIS_HOST=lerd-redis",
		"REDIS_PORT=6379",
		"REDIS_PASSWORD=null",
		"CACHE_STORE=redis",
		"SESSION_DRIVER=redis",
		"QUEUE_CONNECTION=redis",
	},
	"meilisearch": {
		"SCOUT_DRIVER=meilisearch",
		"MEILISEARCH_HOST=http://lerd-meilisearch:7700",
	},
	"rustfs": {
		"FILESYSTEM_DISK=s3",
		"AWS_ACCESS_KEY_ID=lerd",
		"AWS_SECRET_ACCESS_KEY=lerdpassword",
		"AWS_DEFAULT_REGION=us-east-1",
		"AWS_BUCKET=lerd",
		"AWS_URL=http://localhost:9000",
		"AWS_ENDPOINT=http://lerd-rustfs:9000",
		"AWS_USE_PATH_STYLE_ENDPOINT=true",
	},
	"mailpit": {
		"MAIL_MAILER=smtp",
		"MAIL_HOST=lerd-mailpit",
		"MAIL_PORT=1025",
		"MAIL_USERNAME=null",
		"MAIL_PASSWORD=null",
		"MAIL_ENCRYPTION=null",
	},
}

// phpVersionRe matches PHP version strings like "8.4" or "8.3" — digits only, no domain names.
var phpVersionRe = regexp.MustCompile(`^\d+\.\d+$`)
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// defaultSitePath is resolved at startup: LERD_SITE_PATH takes precedence (injected by
// mcp:inject for project-scoped use); if not set, the working directory is used so that
// global MCP sessions (registered via mcp:enable-global) are automatically context-aware.
var defaultSitePath = func() string {
	if p := os.Getenv("LERD_SITE_PATH"); p != "" {
		return p
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return ""
}()

// resolvedPath returns the "path" argument from args, falling back to defaultSitePath.
func resolvedPath(args map[string]any) string {
	if p := strArg(args, "path"); p != "" {
		return p
	}
	return defaultSitePath
}

// ---- JSON-RPC wire types ----

type rpcRequest struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id"`
	Result  any              `json:"result,omitempty"`
	Error   *rpcError        `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ---- MCP schema types ----

type mcpTool struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	InputSchema mcpSchema `json:"inputSchema"`
}

type mcpSchema struct {
	Type       string             `json:"type"`
	Properties map[string]mcpProp `json:"properties"`
	Required   []string           `json:"required,omitempty"`
}

type mcpProp struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Enum        []string `json:"enum,omitempty"`
}

// Serve runs the MCP server, reading JSON-RPC messages from stdin and writing responses to stdout.
// All diagnostic output goes to stderr so it never corrupts the JSON-RPC stream on stdout.
func Serve() error {
	enc := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1 MB — handle large artisan output

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			_ = enc.Encode(rpcResponse{
				JSONRPC: "2.0",
				Error:   &rpcError{Code: -32700, Message: "parse error"},
			})
			continue
		}

		// Notifications have no id field — do not respond.
		if req.ID == nil {
			continue
		}

		result, rpcErr := dispatch(&req)
		resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
		if rpcErr != nil {
			resp.Error = rpcErr
		} else {
			resp.Result = result
		}
		_ = enc.Encode(resp)
	}
	return scanner.Err()
}

func dispatch(req *rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "lerd", "version": "1.0"},
		}, nil
	case "tools/list":
		return map[string]any{"tools": toolList()}, nil
	case "tools/call":
		return handleToolCall(req.Params)
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found: " + req.Method}
	}
}

// ---- Tool definitions ----

// siteHasConsole returns true when the site's framework defines a console command.
func siteHasConsole() bool {
	fw, ok := siteFramework()
	return ok && fw.Console != ""
}

// siteHasWorker returns true when the site's framework defines the named worker
// and its check rule passes.
func siteHasWorker(name string) bool {
	fw, ok := siteFramework()
	if !ok {
		return false
	}
	return fw.HasWorker(name, defaultSitePath)
}

// siteFramework returns the framework definition for the configured site path.
// Returns (nil, false) when no path is set or no framework is found.
func siteFramework() (*config.Framework, bool) {
	if defaultSitePath == "" {
		return nil, false
	}
	site, err := config.FindSiteByPath(defaultSitePath)
	if err != nil {
		return nil, false
	}
	return config.GetFrameworkForDir(site.Framework, site.Path)
}

func toolList() []mcpTool {
	tools := []mcpTool{
		{
			Name:        "sites",
			Description: "List all sites registered with lerd, including domain, path, PHP version, Node version, TLS status, and queue worker status. Call this first to find site names for other tools.",
			InputSchema: mcpSchema{
				Type:       "object",
				Properties: map[string]mcpProp{},
			},
		},
		{
			Name:        "service_start",
			Description: "Start a lerd infrastructure service (built-in or custom). Ensures the quadlet is written and the systemd unit is running.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"name": {
						Type:        "string",
						Description: "Service to start (built-in: mysql, redis, postgres, meilisearch, rustfs, mailpit — or any custom service name registered with service_add)",
					},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:        "service_stop",
			Description: "Stop a running lerd infrastructure service (built-in or custom).",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"name": {
						Type:        "string",
						Description: "Service to stop (built-in: mysql, redis, postgres, meilisearch, rustfs, mailpit — or any custom service name registered with service_add)",
					},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:        "logs",
			Description: `Fetch recent container logs for a lerd service or PHP-FPM container. When target is omitted, logs for the current site's FPM container are returned. Valid targets: "nginx", a service name (mysql, redis, etc.), a PHP version (8.4, 8.5), or a site name.`,
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"target": {
						Type:        "string",
						Description: `Optional. "nginx", service name like "mysql", PHP version like "8.4", or site name. Defaults to the current site's FPM container.`,
					},
					"lines": {
						Type:        "integer",
						Description: "Number of lines to return from the tail (default: 50)",
					},
				},
				Required: []string{},
			},
		},
		{
			Name:        "composer",
			Description: "Run a Composer command inside the lerd PHP-FPM container for the project. Use this to install dependencies, require packages, run scripts, or any other composer command.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {
						Type:        "string",
						Description: "Absolute path to the Laravel project root (e.g. /home/user/code/myapp). Defaults to LERD_SITE_PATH when omitted.",
					},
					"args": {
						Type:        "array",
						Description: `Composer arguments as an array, e.g. ["install"] or ["require", "laravel/sanctum"] or ["dump-autoload"]`,
					},
				},
				Required: []string{"args"},
			},
		},
		{
			Name:        "vendor_bins",
			Description: "List composer-installed binaries available in the project's vendor/bin directory. Use this to discover tools like pest, phpunit, pint, phpstan, rector, etc. before invoking vendor_run.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {
						Type:        "string",
						Description: "Absolute path to the project root. Defaults to LERD_SITE_PATH when omitted.",
					},
				},
			},
		},
		{
			Name:        "vendor_run",
			Description: "Run a composer-installed binary from the project's vendor/bin directory inside the lerd PHP-FPM container. Use vendor_bins first to see what's available.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {
						Type:        "string",
						Description: "Absolute path to the project root. Defaults to LERD_SITE_PATH when omitted.",
					},
					"bin": {
						Type:        "string",
						Description: `Name of the binary in vendor/bin, e.g. "pest", "phpunit", "pint"`,
					},
					"args": {
						Type:        "array",
						Description: `Arguments to pass to the binary, e.g. ["--filter", "UserTest"]`,
					},
				},
				Required: []string{"bin"},
			},
		},
		{
			Name:        "node_install",
			Description: "Install a Node.js version via fnm so it can be used by lerd sites. Accepts a version number (e.g. \"20\", \"20.11.0\") or alias (e.g. \"lts\").",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"version": {
						Type:        "string",
						Description: `Node.js version or alias to install, e.g. "20", "20.11.0", "lts"`,
					},
				},
				Required: []string{"version"},
			},
		},
		{
			Name:        "node_uninstall",
			Description: "Uninstall a Node.js version via fnm.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"version": {
						Type:        "string",
						Description: `Node.js version to uninstall, e.g. "20.11.0"`,
					},
				},
				Required: []string{"version"},
			},
		},
		{
			Name:        "runtime_versions",
			Description: "List installed PHP and Node.js versions managed by lerd, plus default versions. Use this to check what runtimes are available before running commands.",
			InputSchema: mcpSchema{
				Type:       "object",
				Properties: map[string]mcpProp{},
			},
		},
		{
			Name:        "status",
			Description: "Return the health status of core lerd services: DNS resolution, nginx, PHP-FPM containers, and the file watcher. Use this to diagnose why a site isn't loading or before suggesting start/stop commands.",
			InputSchema: mcpSchema{
				Type:       "object",
				Properties: map[string]mcpProp{},
			},
		},
		{
			Name:        "doctor",
			Description: "Run a full lerd environment diagnostic: checks podman, systemd, DNS resolution, port conflicts, PHP images, config validity, and update availability. Use this when the user reports setup issues or unexpected behaviour.",
			InputSchema: mcpSchema{
				Type:       "object",
				Properties: map[string]mcpProp{},
			},
		},
		{
			Name:        "service_add",
			Description: "Register a new custom OCI-based service with lerd (e.g. RabbitMQ, Cassandra, a hand-rolled image). Writes a systemd quadlet so the service can be started/stopped like built-in services. For commonly-used services that ship as bundled presets (phpmyadmin, pgadmin, mongo, mongo-express, mysql alternates, mariadb, stripe-mock) prefer service_preset_install instead.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"name": {
						Type:        "string",
						Description: "Service slug, lowercase letters/digits/hyphens only (e.g. \"mongodb\")",
					},
					"image": {
						Type:        "string",
						Description: "OCI image reference (e.g. \"docker.io/library/mongo:7\")",
					},
					"ports": {
						Type:        "array",
						Description: `Port mappings as \"host:container\" strings, e.g. ["27017:27017"]`,
					},
					"environment": {
						Type:        "array",
						Description: `Container environment variables as \"KEY=VALUE\" strings`,
					},
					"env_vars": {
						Type:        "array",
						Description: `Project .env variables to inject (shown by lerd env), as \"KEY=VALUE\" strings`,
					},
					"data_dir": {
						Type:        "string",
						Description: "Mount path inside the container for persistent data (host directory is auto-created)",
					},
					"description": {
						Type:        "string",
						Description: "Human-readable description of the service",
					},
					"dashboard": {
						Type:        "string",
						Description: "URL to open for this service's web dashboard (e.g. \"http://localhost:8080\")",
					},
					"depends_on": {
						Type:        "array",
						Description: `Services that must be running before this one starts, e.g. ["mysql"]. When this service starts its dependencies start first; when a dependency is stopped this service is stopped first.`,
					},
				},
				Required: []string{"name", "image"},
			},
		},
		{
			Name:        "service_remove",
			Description: "Stop and remove a custom lerd service. Built-in services (mysql, redis, etc.) cannot be removed. Persistent data is NOT deleted.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"name": {
						Type:        "string",
						Description: "Name of the custom service to remove",
					},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:        "service_expose",
			Description: "Add or remove an extra published port on a built-in lerd service (mysql, redis, etc.). The port mapping is persisted in the global config. The service is restarted automatically if running.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"name": {
						Type:        "string",
						Description: "Built-in service name (mysql, redis, postgres, meilisearch, rustfs, mailpit)",
					},
					"port": {
						Type:        "string",
						Description: `Port mapping as "host:container", e.g. "13306:3306"`,
					},
					"remove": {
						Type:        "boolean",
						Description: "Set to true to remove the port mapping instead of adding it",
					},
				},
				Required: []string{"name", "port"},
			},
		},
		{
			Name:        "service_env",
			Description: "Return the recommended Laravel .env connection variables for a lerd service (built-in or custom). Use this to see what keys a service needs before calling env_setup or editing .env manually.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"name": {
						Type:        "string",
						Description: "Service name (e.g. \"mysql\", \"redis\", \"mongodb\")",
					},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:        "service_preset_list",
			Description: "List bundled service presets that ship with lerd. Each entry includes the preset name, description, dashboard URL, declared dependencies, available versions (for multi-version presets like mysql or mariadb), and which versions are already installed locally. Use this before service_preset_install to see what's available.",
			InputSchema: mcpSchema{
				Type:       "object",
				Properties: map[string]mcpProp{},
			},
		},
		{
			Name:        "service_preset_install",
			Description: "Install a bundled service preset as a local custom service. Single-version presets (phpmyadmin, pgadmin, mongo, mongo-express, stripe-mock) are installed by name. Multi-version presets (mysql, mariadb) require a version argument; available versions come from service_preset_list. Dependencies that are themselves presets must be installed first — installing mongo-express without mongo errors out with a clear hint. After install the service can be started, stopped, removed, exposed, or pinned with the usual service_* tools.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"name": {
						Type:        "string",
						Description: "Preset name (e.g. \"phpmyadmin\", \"pgadmin\", \"mongo\", \"mongo-express\", \"stripe-mock\", \"mysql\", \"mariadb\"). Run service_preset_list to see all bundled presets.",
					},
					"version": {
						Type:        "string",
						Description: "Version tag for multi-version presets (e.g. \"5.7\" for mysql, \"11\" for mariadb). Required when the preset declares versions. Empty for single-version presets.",
					},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:        "env_setup",
			Description: "Configure the project's .env for lerd: creates .env from .env.example if missing, detects services (mysql, redis, etc.), starts them, creates databases, generates APP_KEY (works even before composer install), and sets APP_URL. Run this once after cloning a project. Note: when run on a fresh Laravel project where .env still says DB_CONNECTION=sqlite, env_setup leaves the database choice alone — call db_set first to pick a database explicitly.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {
						Type:        "string",
						Description: "Absolute path to the Laravel project root. Defaults to LERD_SITE_PATH when omitted.",
					},
				},
			},
		},
		{
			Name:        "db_set",
			Description: "Pick the database for a Laravel project: sqlite (local file, no service), mysql (lerd-mysql), or postgres (lerd-postgres). Persists the choice to .lerd.yaml, rewrites the relevant DB_ keys in .env, starts the service if needed, and creates the project database (and a _testing variant) for mysql/postgres. For sqlite, creates database/database.sqlite if missing. Picking a database is exclusive — switching from one to another removes the previous entry from .lerd.yaml. Use this on fresh Laravel clones before env_setup.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {
						Type:        "string",
						Description: "Absolute path to the Laravel project root. Defaults to LERD_SITE_PATH when omitted.",
					},
					"database": {
						Type:        "string",
						Description: `Database to use: "sqlite", "mysql", or "postgres"`,
						Enum:        []string{"sqlite", "mysql", "postgres"},
					},
				},
				Required: []string{"database"},
			},
		},
		{
			Name:        "env_check",
			Description: "Compare .env against .env.example and flag missing or extra keys. Useful for catching 'works on my machine' bugs caused by env drift.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {
						Type:        "string",
						Description: "Absolute path to the project root. Defaults to LERD_SITE_PATH when omitted.",
					},
				},
			},
		},
		{
			Name:        "site_link",
			Description: "Register a directory as a lerd site, generating an nginx vhost and a <name>.test domain. Use this to set up a new project.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {
						Type:        "string",
						Description: "Absolute path to the project directory. Defaults to LERD_SITE_PATH when omitted.",
					},
					"name": {
						Type:        "string",
						Description: "Domain name without TLD (e.g. 'myapp' becomes myapp.test). Defaults to the directory name, cleaned up.",
					},
				},
			},
		},
		{
			Name:        "site_unlink",
			Description: "Unregister a lerd site and remove its nginx vhost (all domains). The project files are NOT deleted.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {
						Type:        "string",
						Description: "Absolute path to the project directory. Defaults to LERD_SITE_PATH when omitted.",
					},
				},
			},
		},
		{
			Name:        "site_domain_add",
			Description: "Add an additional domain to a lerd site. The domain name should not include the .test TLD.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {
						Type:        "string",
						Description: "Absolute path to the project directory. Defaults to LERD_SITE_PATH when omitted.",
					},
					"domain": {
						Type:        "string",
						Description: "Domain name to add (without .test TLD, e.g. 'api')",
					},
				},
				Required: []string{"domain"},
			},
		},
		{
			Name:        "site_domain_remove",
			Description: "Remove a domain from a lerd site. Cannot remove the last domain.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {
						Type:        "string",
						Description: "Absolute path to the project directory. Defaults to LERD_SITE_PATH when omitted.",
					},
					"domain": {
						Type:        "string",
						Description: "Domain name to remove (without .test TLD, e.g. 'api')",
					},
				},
				Required: []string{"domain"},
			},
		},
		{
			Name:        "secure",
			Description: "Enable HTTPS for a lerd site using a locally-trusted mkcert certificate. Updates APP_URL in .env automatically.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"site": {
						Type:        "string",
						Description: "Site name as shown by the sites tool",
					},
				},
				Required: []string{"site"},
			},
		},
		{
			Name:        "unsecure",
			Description: "Disable HTTPS for a lerd site and revert APP_URL in .env to http://.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"site": {
						Type:        "string",
						Description: "Site name as shown by the sites tool",
					},
				},
				Required: []string{"site"},
			},
		},
		{
			Name:        "xdebug_on",
			Description: "Enable Xdebug for a PHP version and restart the FPM container. Xdebug listens on port 9003 (host.containers.internal).",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"version": {
						Type:        "string",
						Description: "PHP version (e.g. \"8.4\"). Defaults to the project or global default.",
					},
				},
			},
		},
		{
			Name:        "xdebug_off",
			Description: "Disable Xdebug for a PHP version and restart the FPM container.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"version": {
						Type:        "string",
						Description: "PHP version (e.g. \"8.4\"). Defaults to the project or global default.",
					},
				},
			},
		},
		{
			Name:        "xdebug_status",
			Description: "Show Xdebug enabled/disabled status for all installed PHP versions.",
			InputSchema: mcpSchema{
				Type:       "object",
				Properties: map[string]mcpProp{},
			},
		},
		{
			Name:        "db_export",
			Description: "Export a database to a SQL dump file. Reads connection details from the project .env.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {
						Type:        "string",
						Description: "Absolute path to the Laravel project root. Defaults to LERD_SITE_PATH when omitted.",
					},
					"database": {
						Type:        "string",
						Description: "Database name to export (defaults to DB_DATABASE from .env)",
					},
					"output": {
						Type:        "string",
						Description: "Output file path (defaults to <database>.sql in the project root)",
					},
				},
			},
		},
		{
			Name:        "db_import",
			Description: "Import a SQL dump file into the project database. Reads connection details from the project .env. The database service must be running.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {
						Type:        "string",
						Description: "Absolute path to the project root. Defaults to LERD_SITE_PATH when omitted.",
					},
					"file": {
						Type:        "string",
						Description: "Absolute path to the SQL dump file to import",
					},
					"database": {
						Type:        "string",
						Description: "Database name to import into (defaults to DB_DATABASE from .env)",
					},
				},
				Required: []string{"file"},
			},
		},
		{
			Name:        "db_create",
			Description: "Create a database (and a _testing variant) for the project. Reads the connection type from .env and starts the service if needed. Defaults to the DB_DATABASE name from .env, falling back to the project directory name.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {
						Type:        "string",
						Description: "Absolute path to the project root. Defaults to LERD_SITE_PATH when omitted.",
					},
					"name": {
						Type:        "string",
						Description: "Database name (defaults to DB_DATABASE from .env, then to the project directory name)",
					},
				},
			},
		},
		{
			Name:        "php_list",
			Description: "List all PHP versions installed by lerd, marking the global default. Use this to check available versions before calling site_php or php_ext_add.",
			InputSchema: mcpSchema{
				Type:       "object",
				Properties: map[string]mcpProp{},
			},
		},
		{
			Name:        "php_ext_list",
			Description: "List custom PHP extensions configured for a PHP version. These are extensions added on top of the bundled lerd image.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"version": {
						Type:        "string",
						Description: "PHP version (e.g. \"8.4\"). Defaults to the project or global default.",
					},
				},
			},
		},
		{
			Name:        "php_ext_add",
			Description: "Install a custom PHP extension for a PHP version. Rebuilds the FPM image and restarts the container. This may take a minute.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"extension": {
						Type:        "string",
						Description: "Extension name to install (e.g. \"imagick\", \"redis\", \"swoole\")",
					},
					"version": {
						Type:        "string",
						Description: "PHP version (e.g. \"8.4\"). Defaults to the project or global default.",
					},
				},
				Required: []string{"extension"},
			},
		},
		{
			Name:        "php_ext_remove",
			Description: "Remove a custom PHP extension from a PHP version. Rebuilds the FPM image and restarts the container.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"extension": {
						Type:        "string",
						Description: "Extension name to remove",
					},
					"version": {
						Type:        "string",
						Description: "PHP version (e.g. \"8.4\"). Defaults to the project or global default.",
					},
				},
				Required: []string{"extension"},
			},
		},
		{
			Name:        "park",
			Description: "Register a directory as a lerd park: scans all subdirectories and auto-registers any PHP projects as sites. Use this to manage many projects under a single parent directory.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {
						Type:        "string",
						Description: "Absolute path to the directory to park. Defaults to LERD_SITE_PATH when omitted.",
					},
				},
			},
		},
		{
			Name:        "unpark",
			Description: "Remove a parked directory from lerd and unlink all sites whose paths are under it. Project files are NOT deleted.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {
						Type:        "string",
						Description: "Absolute path to the parked directory to remove",
					},
				},
				Required: []string{"path"},
			},
		},
		{
			Name:        "which",
			Description: "Show the resolved PHP version, Node version, document root, and nginx config path for a site. Useful for confirming which runtime versions a project will use.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {
						Type:        "string",
						Description: "Absolute path to the project root. Defaults to LERD_SITE_PATH when omitted.",
					},
				},
			},
		},
		{
			Name:        "check",
			Description: "Validate a project's .lerd.yaml file — checks YAML syntax, PHP version format, service definitions, and framework references. Reports OK/WARN/FAIL per field.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {
						Type:        "string",
						Description: "Absolute path to the project root containing .lerd.yaml. Defaults to LERD_SITE_PATH when omitted.",
					},
				},
			},
		},
	}

	if siteHasConsole() {
		tools = append(tools,
			mcpTool{
				Name:        "artisan",
				Description: "Run a php artisan command inside the lerd PHP-FPM container for the project. Use this to run migrations, generate files, seed databases, clear caches, or any other artisan command.",
				InputSchema: mcpSchema{
					Type: "object",
					Properties: map[string]mcpProp{
						"path": {
							Type:        "string",
							Description: "Absolute path to the Laravel project root (e.g. /home/user/code/myapp). Defaults to LERD_SITE_PATH when omitted.",
						},
						"args": {
							Type:        "array",
							Description: `Artisan arguments as an array, e.g. ["migrate"] or ["make:model", "Post", "-m"] or ["tinker", "--execute=App\\Models\\User::count()"]`,
						},
					},
					Required: []string{"args"},
				},
			},
			mcpTool{
				Name:        "queue_start",
				Description: "Start a Laravel queue worker for a registered site as a systemd user service. The worker runs php artisan queue:work inside the PHP-FPM container.",
				InputSchema: mcpSchema{
					Type: "object",
					Properties: map[string]mcpProp{
						"site": {
							Type:        "string",
							Description: "Site name as shown by the sites tool",
						},
						"queue": {
							Type:        "string",
							Description: `Queue name to process (default: "default")`,
						},
						"tries": {
							Type:        "integer",
							Description: "Max job attempts before marking failed (default: 3)",
						},
						"timeout": {
							Type:        "integer",
							Description: "Seconds a job may run before timing out (default: 60)",
						},
					},
					Required: []string{"site"},
				},
			},
			mcpTool{
				Name:        "queue_stop",
				Description: "Stop the Laravel queue worker systemd service for a registered site.",
				InputSchema: mcpSchema{
					Type: "object",
					Properties: map[string]mcpProp{
						"site": {
							Type:        "string",
							Description: "Site name as shown by the sites tool",
						},
					},
					Required: []string{"site"},
				},
			},
			mcpTool{
				Name:        "reverb_start",
				Description: "Start the Laravel Reverb WebSocket server for a registered site as a systemd user service. The server runs php artisan reverb:start inside the PHP-FPM container.",
				InputSchema: mcpSchema{
					Type: "object",
					Properties: map[string]mcpProp{
						"site": {
							Type:        "string",
							Description: "Site name as shown by the sites tool",
						},
					},
					Required: []string{"site"},
				},
			},
			mcpTool{
				Name:        "reverb_stop",
				Description: "Stop the Laravel Reverb WebSocket server for a registered site.",
				InputSchema: mcpSchema{
					Type: "object",
					Properties: map[string]mcpProp{
						"site": {
							Type:        "string",
							Description: "Site name as shown by the sites tool",
						},
					},
					Required: []string{"site"},
				},
			},
			mcpTool{
				Name:        "horizon_start",
				Description: "Start Laravel Horizon for a registered site as a systemd user service. Horizon runs php artisan horizon inside the PHP-FPM container and replaces the standard queue worker. Only available for sites that have laravel/horizon in composer.json.",
				InputSchema: mcpSchema{
					Type: "object",
					Properties: map[string]mcpProp{
						"site": {
							Type:        "string",
							Description: "Site name as shown by the sites tool",
						},
					},
					Required: []string{"site"},
				},
			},
			mcpTool{
				Name:        "horizon_stop",
				Description: "Stop the Laravel Horizon service for a registered site.",
				InputSchema: mcpSchema{
					Type: "object",
					Properties: map[string]mcpProp{
						"site": {
							Type:        "string",
							Description: "Site name as shown by the sites tool",
						},
					},
					Required: []string{"site"},
				},
			},
			mcpTool{
				Name:        "schedule_start",
				Description: "Start the Laravel task scheduler (php artisan schedule:work) for a registered site as a systemd user service.",
				InputSchema: mcpSchema{
					Type: "object",
					Properties: map[string]mcpProp{
						"site": {
							Type:        "string",
							Description: "Site name as shown by the sites tool",
						},
					},
					Required: []string{"site"},
				},
			},
			mcpTool{
				Name:        "schedule_stop",
				Description: "Stop the Laravel task scheduler for a registered site.",
				InputSchema: mcpSchema{
					Type: "object",
					Properties: map[string]mcpProp{
						"site": {
							Type:        "string",
							Description: "Site name as shown by the sites tool",
						},
					},
					Required: []string{"site"},
				},
			},
			mcpTool{
				Name:        "stripe_listen",
				Description: "Start a Stripe webhook listener for a registered site using the Stripe CLI container. Reads STRIPE_SECRET from the site's .env. Forwards webhooks to the site's /stripe/webhook route by default.",
				InputSchema: mcpSchema{
					Type: "object",
					Properties: map[string]mcpProp{
						"site": {
							Type:        "string",
							Description: "Site name as shown by the sites tool",
						},
						"api_key": {
							Type:        "string",
							Description: "Stripe secret key (defaults to STRIPE_SECRET in the site's .env)",
						},
						"webhook_path": {
							Type:        "string",
							Description: `Webhook route path on the app (default: "/stripe/webhook")`,
						},
					},
					Required: []string{"site"},
				},
			},
			mcpTool{
				Name:        "stripe_listen_stop",
				Description: "Stop the Stripe webhook listener for a registered site.",
				InputSchema: mcpSchema{
					Type: "object",
					Properties: map[string]mcpProp{
						"site": {
							Type:        "string",
							Description: "Site name as shown by the sites tool",
						},
					},
					Required: []string{"site"},
				},
			},
		)
	}

	if fw, ok := siteFramework(); ok && fw.Console != "" && fw.Console != "artisan" {
		tools = append(tools, mcpTool{
			Name:        "console",
			Description: fmt.Sprintf("Run a framework console command (php %s) inside the lerd PHP-FPM container for the project.", fw.Console),
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {
						Type:        "string",
						Description: "Absolute path to the project root. Defaults to LERD_SITE_PATH when omitted.",
					},
					"args": {
						Type:        "array",
						Description: fmt.Sprintf(`Console arguments as an array, e.g. ["%s", "cache:clear"]`, fw.Console),
					},
				},
				Required: []string{"args"},
			},
		})
	}

	tools = append(tools,
		mcpTool{
			Name:        "worker_start",
			Description: "Start a framework-defined worker for a registered site as a systemd user service. The worker command is taken from the framework definition. Use worker_list to see available workers.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"site": {
						Type:        "string",
						Description: "Site name as shown by the sites tool",
					},
					"worker": {
						Type:        "string",
						Description: "Worker name as defined in the framework (e.g. messenger, horizon, pulse)",
					},
				},
				Required: []string{"site", "worker"},
			},
		},
		mcpTool{
			Name:        "worker_stop",
			Description: "Stop a framework-defined worker for a registered site.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"site": {
						Type:        "string",
						Description: "Site name as shown by the sites tool",
					},
					"worker": {
						Type:        "string",
						Description: "Worker name (e.g. messenger, horizon)",
					},
				},
				Required: []string{"site", "worker"},
			},
		},
		mcpTool{
			Name:        "worker_list",
			Description: "List all workers defined for a site's framework, including their running status.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"site": {
						Type:        "string",
						Description: "Site name as shown by the sites tool",
					},
				},
				Required: []string{"site"},
			},
		},
		mcpTool{
			Name:        "worker_add",
			Description: "Add or update a custom worker for a project. Saves to .lerd.yaml custom_workers by default, or to the global user framework overlay (~/.config/lerd/frameworks/) with global: true. Does not auto-start — use worker_start afterwards.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"site":               {Type: "string", Description: "Site name as shown by the sites tool"},
					"name":               {Type: "string", Description: "Worker name (slug, e.g. pdf-generator)"},
					"command":            {Type: "string", Description: "Command to run inside the PHP-FPM container"},
					"label":              {Type: "string", Description: "Human-readable label (optional)"},
					"restart":            {Type: "string", Description: "Restart policy: always or on-failure (default: always)"},
					"check_file":         {Type: "string", Description: "Only show worker when this file exists (optional)"},
					"check_composer":     {Type: "string", Description: "Only show worker when this Composer package is installed (optional)"},
					"conflicts_with":     {Type: "array", Description: "Workers to stop before starting this one (optional)"},
					"proxy_path":         {Type: "string", Description: "URL path to proxy (optional, e.g. /app)"},
					"proxy_port_env_key": {Type: "string", Description: "Env key holding the worker port (optional)"},
					"proxy_default_port": {Type: "number", Description: "Default port if env key is missing (optional)"},
					"global":             {Type: "boolean", Description: "Save to global framework overlay instead of project .lerd.yaml (default: false)"},
				},
				Required: []string{"site", "name", "command"},
			},
		},
		mcpTool{
			Name:        "worker_remove",
			Description: "Remove a custom worker from a project's .lerd.yaml or global framework overlay. Stops the worker if running.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"site":   {Type: "string", Description: "Site name as shown by the sites tool"},
					"name":   {Type: "string", Description: "Worker name to remove"},
					"global": {Type: "boolean", Description: "Remove from global framework overlay instead of .lerd.yaml (default: false)"},
				},
				Required: []string{"site", "name"},
			},
		},
		mcpTool{
			Name:        "framework_list",
			Description: "List all available framework definitions (laravel built-in plus any user-defined YAMLs), including their defined workers and setup commands. Use this before framework_add to see what is already defined.",
			InputSchema: mcpSchema{Type: "object", Properties: map[string]mcpProp{}},
		},
		mcpTool{
			Name:        "framework_add",
			Description: "Create or update a framework definition. For laravel, only the workers and setup fields are used (built-in settings are always preserved). For other frameworks, creates a full definition at ~/.config/lerd/frameworks/<name>.yaml.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"name": {
						Type:        "string",
						Description: `Framework identifier (slug). Use "laravel" to add custom workers to Laravel (e.g. horizon, pulse). For new frameworks use e.g. symfony, wordpress, drupal.`,
					},
					"label": {
						Type:        "string",
						Description: "Human-readable name, e.g. Symfony (not required when name is laravel)",
					},
					"public_dir": {
						Type:        "string",
						Description: `Document root relative to project path (e.g. "public", "web", "."). Not required when name is laravel.`,
					},
					"detect_files": {
						Type:        "array",
						Description: `List of filenames whose presence signals this framework, e.g. ["wp-login.php"]`,
					},
					"detect_packages": {
						Type:        "array",
						Description: `List of Composer package names that signal this framework, e.g. ["symfony/framework-bundle"]`,
					},
					"env_file": {
						Type:        "string",
						Description: `Primary env file path relative to project root (default: ".env")`,
					},
					"env_format": {
						Type:        "string",
						Description: `Env file format: "dotenv" (default) or "php-const" (for wp-config.php style)`,
					},
					"env_fallback_file": {
						Type:        "string",
						Description: `Fallback env file if primary doesn't exist (e.g. "wp-config.php")`,
					},
					"env_fallback_format": {
						Type:        "string",
						Description: `Format for fallback env file`,
					},
					"workers": {
						Type:        "object",
						Description: `Map of worker name → {label, command, restart, check} definitions. "check" is optional — an object with "file" or "composer" field; the worker is only shown when the check passes. e.g. {"messenger": {"label": "Messenger", "command": "php bin/console messenger:consume async", "restart": "always", "check": {"composer": "symfony/messenger"}}}`,
					},
					"setup": {
						Type:        "array",
						Description: `List of one-off setup commands shown in "lerd setup" wizard. Each element is an object with "label" (string), "command" (string), optional "default" (boolean, pre-selected in wizard), and optional "check" (object with "file" or "composer" field — command is only shown when the check passes). e.g. [{"label": "Load fixtures", "command": "php bin/console doctrine:fixtures:load", "check": {"composer": "doctrine/doctrine-fixtures-bundle"}}]`,
					},
					"logs": {
						Type:        "array",
						Description: `List of log source definitions for the app log viewer. Each element is an object with "path" (glob relative to project root, e.g. "storage/logs/*.log") and optional "format" ("monolog" for Monolog format, "raw" for plain text; default: "raw"). e.g. [{"path": "storage/logs/*.log", "format": "monolog"}]`,
					},
				},
				Required: []string{"name"},
			},
		},
		mcpTool{
			Name:        "framework_remove",
			Description: "Delete a framework definition (user-defined or store-installed) by name. For laravel, removes only custom worker additions (built-in definition remains). Use version to remove a specific version, or omit to remove all.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"name": {
						Type:        "string",
						Description: "Framework name to remove (e.g. 'symfony')",
					},
					"version": {
						Type:        "string",
						Description: "Specific version to remove (e.g. '7'). Omit to remove all versions.",
					},
				},
				Required: []string{"name"},
			},
		},
		mcpTool{
			Name:        "framework_search",
			Description: "Search the community framework store for available definitions. Returns matching frameworks with their available versions.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"query": {
						Type:        "string",
						Description: "Search query (matches framework name or label, case-insensitive)",
					},
				},
				Required: []string{"query"},
			},
		},
		mcpTool{
			Name:        "framework_install",
			Description: "Install a framework definition from the community store. If no version is specified, auto-detects from composer.lock or uses the latest available version.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"name": {
						Type:        "string",
						Description: "Framework name to install (e.g. symfony, wordpress)",
					},
					"version": {
						Type:        "string",
						Description: "Framework major version (e.g. 11, 7). Omit to auto-detect or use latest.",
					},
				},
				Required: []string{"name"},
			},
		},
		mcpTool{
			Name:        "project_new",
			Description: "Scaffold a new PHP project using a framework's create command. For Laravel this runs `composer create-project --no-install --no-plugins --no-scripts laravel/laravel <path>`. Other frameworks must have a `create` field in their YAML definition. After creation, use site_link to register the site.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {
						Type:        "string",
						Description: "Absolute path for the new project directory (e.g. /home/user/code/myapp)",
					},
					"framework": {
						Type:        "string",
						Description: `Framework to use (default: "laravel"). Must have a 'create' field defined.`,
					},
					"args": {
						Type:        "array",
						Description: `Extra arguments to pass to the scaffold command, e.g. ["--no-interaction"]`,
					},
				},
				Required: []string{"path"},
			},
		},
		mcpTool{
			Name:        "site_php",
			Description: "Change the PHP version for a registered lerd site. Writes a .php-version file, updates the site registry, and regenerates the nginx vhost.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"site": {
						Type:        "string",
						Description: "Site name as shown by the sites tool",
					},
					"version": {
						Type:        "string",
						Description: "PHP version to use, e.g. \"8.4\", \"8.3\"",
					},
				},
				Required: []string{"site", "version"},
			},
		},
		mcpTool{
			Name:        "site_node",
			Description: "Change the Node.js version for a registered lerd site. Writes a .node-version file, installs the version via fnm if needed, and updates the site registry.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"site": {
						Type:        "string",
						Description: "Site name as shown by the sites tool",
					},
					"version": {
						Type:        "string",
						Description: "Node.js version to use, e.g. \"22\", \"20\", \"lts\"",
					},
				},
				Required: []string{"site", "version"},
			},
		},
		mcpTool{
			Name:        "site_pause",
			Description: "Pause a site: stop all its running workers (queue, schedule, reverb, stripe, custom) and replace its nginx vhost with a landing page. Auto-stops services no longer needed by any active site.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"site": {
						Type:        "string",
						Description: "Site name as shown by the sites tool",
					},
				},
				Required: []string{"site"},
			},
		},
		mcpTool{
			Name:        "site_unpause",
			Description: "Resume a paused site: restore its nginx vhost, restart any workers that were running when it was paused, and ensure required services are running.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"site": {
						Type:        "string",
						Description: "Site name as shown by the sites tool",
					},
				},
				Required: []string{"site"},
			},
		},
		mcpTool{
			Name:        "service_pin",
			Description: "Pin a service so it is never auto-stopped, even when no sites reference it. Starts the service if it is not already running.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"name": {
						Type:        "string",
						Description: "Service name to pin (built-in: mysql, redis, postgres, meilisearch, rustfs, mailpit — or any custom service name)",
					},
				},
				Required: []string{"name"},
			},
		},
		mcpTool{
			Name:        "service_unpin",
			Description: "Unpin a service so it can be auto-stopped when no active sites reference it.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"name": {
						Type:        "string",
						Description: "Service name to unpin",
					},
				},
				Required: []string{"name"},
			},
		},
	)

	return tools
}

// ---- Tool dispatch ----

type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func handleToolCall(params json.RawMessage) (any, *rpcError) {
	var p callParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: -32602, Message: "invalid params"}
	}

	var args map[string]any
	if len(p.Arguments) > 0 {
		_ = json.Unmarshal(p.Arguments, &args)
	}
	if args == nil {
		args = map[string]any{}
	}

	switch p.Name {
	case "artisan":
		return execArtisan(args)
	case "console":
		return execArtisan(args)
	case "sites":
		return execSites()
	case "service_start":
		return execServiceStart(args)
	case "service_stop":
		return execServiceStop(args)
	case "queue_start":
		return execQueueStart(args)
	case "queue_stop":
		return execQueueStop(args)
	case "reverb_start":
		return execReverbStart(args)
	case "reverb_stop":
		return execReverbStop(args)
	case "horizon_start":
		return execHorizonStart(args)
	case "horizon_stop":
		return execHorizonStop(args)
	case "schedule_start":
		return execScheduleStart(args)
	case "schedule_stop":
		return execScheduleStop(args)
	case "stripe_listen":
		return execStripeListen(args)
	case "stripe_listen_stop":
		return execStripeListenStop(args)
	case "worker_start":
		return execWorkerStart(args)
	case "worker_stop":
		return execWorkerStop(args)
	case "worker_add":
		return execWorkerAdd(args)
	case "worker_remove":
		return execWorkerRemove(args)
	case "worker_list":
		return execWorkerList(args)
	case "logs":
		return execLogs(args)
	case "composer":
		return execComposer(args)
	case "vendor_bins":
		return execVendorBins(args)
	case "vendor_run":
		return execVendorRun(args)
	case "node_install":
		return execNodeInstall(args)
	case "node_uninstall":
		return execNodeUninstall(args)
	case "runtime_versions":
		return execRuntimeVersions()
	case "status":
		return execStatus()
	case "doctor":
		return execDoctor()
	case "which":
		return execWhich(args)
	case "check":
		return execCheck(args)
	case "service_env":
		return execServiceEnv(args)
	case "service_add":
		return execServiceAdd(args)
	case "service_remove":
		return execServiceRemove(args)
	case "service_expose":
		return execServiceExpose(args)
	case "service_preset_list":
		return execServicePresetList(args)
	case "service_preset_install":
		return execServicePresetInstall(args)
	case "env_setup":
		return execEnvSetup(args)
	case "db_set":
		return execDbSet(args)
	case "env_check":
		return execEnvCheck(args)
	case "site_link":
		return execSiteLink(args)
	case "site_unlink":
		return execSiteUnlink(args)
	case "site_domain_add":
		return execSiteDomainAdd(args)
	case "site_domain_remove":
		return execSiteDomainRemove(args)
	case "secure":
		return execSecure(args)
	case "unsecure":
		return execUnsecure(args)
	case "xdebug_on":
		return execXdebugToggle(args, true)
	case "xdebug_off":
		return execXdebugToggle(args, false)
	case "xdebug_status":
		return execXdebugStatus()
	case "db_export":
		return execDBExport(args)
	case "framework_list":
		return execFrameworkList()
	case "framework_add":
		return execFrameworkAdd(args)
	case "framework_remove":
		return execFrameworkRemove(args)
	case "framework_search":
		return execFrameworkSearch(args)
	case "framework_install":
		return execFrameworkInstall(args)
	case "project_new":
		return execProjectNew(args)
	case "site_php":
		return execSitePHP(args)
	case "site_node":
		return execSiteNode(args)
	case "site_pause":
		return execSitePause(args)
	case "site_unpause":
		return execSiteUnpause(args)
	case "service_pin":
		return execServicePin(args)
	case "service_unpin":
		return execServiceUnpin(args)
	case "db_import":
		return execDBImport(args)
	case "db_create":
		return execDBCreate(args)
	case "php_list":
		return execPHPList()
	case "php_ext_list":
		return execPHPExtList(args)
	case "php_ext_add":
		return execPHPExtAdd(args)
	case "php_ext_remove":
		return execPHPExtRemove(args)
	case "park":
		return execPark(args)
	case "unpark":
		return execUnpark(args)
	default:
		return toolErr("unknown tool: " + p.Name), nil
	}
}

// ---- Helpers ----

func toolOK(text string) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
	}
}

func toolErr(text string) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": true,
	}
}

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

func strArg(args map[string]any, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func intArg(args map[string]any, key string, def int) int {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		}
	}
	return def
}

func strSliceArg(args map[string]any, key string) []string {
	v, ok := args[key]
	if !ok {
		return nil
	}
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func boolArg(args map[string]any, key string) bool {
	v, _ := args[key].(bool)
	return v
}

func isKnownService(name string) bool {
	for _, s := range knownServices {
		if s == name {
			return true
		}
	}
	return false
}

// ---- Tool implementations ----

func execArtisan(args map[string]any) (any, *rpcError) {
	projectPath := resolvedPath(args)
	if projectPath == "" {
		return toolErr("path is required — pass a path argument or open Claude in the project directory"), nil
	}
	artisanArgs := strSliceArg(args, "args")
	if len(artisanArgs) == 0 {
		return toolErr("args is required and must be a non-empty array"), nil
	}

	phpVersion, err := phpDet.DetectVersion(projectPath)
	if err != nil {
		cfg, cfgErr := config.LoadGlobal()
		if cfgErr != nil {
			return toolErr("failed to detect PHP version: " + err.Error()), nil
		}
		phpVersion = cfg.PHP.DefaultVersion
	}

	short := strings.ReplaceAll(phpVersion, ".", "")
	container := "lerd-php" + short + "-fpm"

	consoleCmd, err := config.GetConsoleCommand(projectPath)
	if err != nil {
		return toolErr(err.Error()), nil
	}

	// No -it flags — non-interactive, output captured to buffer.
	cmdArgs := []string{"exec", "-w", projectPath, container, "php", consoleCmd}
	cmdArgs = append(cmdArgs, artisanArgs...)

	var out bytes.Buffer
	cmd := podman.Cmd(cmdArgs...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return toolErr(fmt.Sprintf("artisan failed (%v):\n%s", err, stripANSI(out.String()))), nil
	}
	return toolOK(stripANSI(strings.TrimSpace(out.String()))), nil
}

func execSites() (any, *rpcError) {
	enriched, err := siteinfo.LoadAll(siteinfo.EnrichMCP)
	if err != nil {
		return toolErr("failed to load sites: " + err.Error()), nil
	}

	type workerStatus struct {
		Name    string `json:"name"`
		Running bool   `json:"running"`
	}
	type siteInfoResp struct {
		Name        string         `json:"name"`
		Domain      string         `json:"domain"`
		Domains     []string       `json:"domains"`
		Path        string         `json:"path"`
		PHPVersion  string         `json:"php_version"`
		NodeVersion string         `json:"node_version"`
		TLS         bool           `json:"tls"`
		Framework   string         `json:"framework,omitempty"`
		Workers     []workerStatus `json:"workers,omitempty"`
	}

	var out []siteInfoResp
	for _, e := range enriched {
		var workers []workerStatus
		// Collect all worker statuses from enriched data
		for _, w := range []struct {
			name    string
			running bool
		}{
			{"queue", e.QueueRunning},
			{"schedule", e.ScheduleRunning},
			{"reverb", e.ReverbRunning},
			{"horizon", e.HorizonRunning},
		} {
			// Only include if the site has this worker
			switch w.name {
			case "queue":
				if !e.HasQueueWorker {
					continue
				}
			case "schedule":
				if !e.HasScheduleWorker {
					continue
				}
			case "reverb":
				if !e.HasReverb {
					continue
				}
			case "horizon":
				if !e.HasHorizon {
					continue
				}
			}
			workers = append(workers, workerStatus{Name: w.name, Running: w.running})
		}
		for _, fw := range e.FrameworkWorkers {
			workers = append(workers, workerStatus{Name: fw.Name, Running: fw.Running})
		}

		out = append(out, siteInfoResp{
			Name:        e.Name,
			Domain:      e.PrimaryDomain(),
			Domains:     e.Domains,
			Path:        e.Path,
			PHPVersion:  e.PHPVersion,
			NodeVersion: e.NodeVersion,
			TLS:         e.Secured,
			Framework:   e.FrameworkName,
			Workers:     workers,
		})
	}
	if out == nil {
		out = []siteInfoResp{}
	}
	data, _ := json.MarshalIndent(out, "", "  ")
	return toolOK(string(data)), nil
}

func execServiceStart(args map[string]any) (any, *rpcError) {
	name := strArg(args, "name")
	if name == "" {
		return toolErr("name is required"), nil
	}

	unitName := "lerd-" + name

	if isKnownService(name) {
		content, err := podman.GetQuadletTemplate(unitName + ".container")
		if err != nil {
			return toolErr("no quadlet template for " + name + ": " + err.Error()), nil
		}
		if cfg, loadErr := config.LoadGlobal(); loadErr == nil {
			if svcCfg, ok := cfg.Services[name]; ok {
				content = podman.ApplyImage(content, svcCfg.Image)
				if len(svcCfg.ExtraPorts) > 0 {
					content = podman.ApplyExtraPorts(content, svcCfg.ExtraPorts)
				}
			}
		}
		if err := podman.WriteContainerUnitFn(unitName, content); err != nil {
			return toolErr("writing quadlet: " + err.Error()), nil
		}
	} else {
		svc, err := config.LoadCustomService(name)
		if err != nil {
			return toolErr("unknown service: " + name + ". Use service_add to register a custom service first."), nil
		}
		if err := serviceops.EnsureCustomServiceQuadlet(svc); err != nil {
			return toolErr("writing quadlet: " + err.Error()), nil
		}
	}

	if err := podman.DaemonReloadFn(); err != nil {
		return toolErr("daemon-reload: " + err.Error()), nil
	}
	if err := podman.StartUnit(unitName); err != nil {
		return toolErr("starting " + name + ": " + err.Error()), nil
	}
	return toolOK(name + " started"), nil
}

func execServiceStop(args map[string]any) (any, *rpcError) {
	name := strArg(args, "name")
	if name == "" {
		return toolErr("name is required"), nil
	}
	if err := podman.StopUnit("lerd-" + name); err != nil {
		return toolErr("stopping " + name + ": " + err.Error()), nil
	}
	return toolOK(name + " stopped"), nil
}

func execQueueStart(args map[string]any) (any, *rpcError) {
	siteName := strArg(args, "site")
	if siteName == "" {
		return toolErr("site is required"), nil
	}

	site, err := config.FindSite(siteName)
	if err != nil {
		return toolErr("site not found: " + siteName), nil
	}

	phpVersion := site.PHPVersion
	if detected, err := phpDet.DetectVersion(site.Path); err == nil && detected != "" {
		phpVersion = detected
	}

	queue := strArg(args, "queue")
	if queue == "" {
		queue = "default"
	}
	tries := intArg(args, "tries", 3)
	timeout := intArg(args, "timeout", 60)

	versionShort := strings.ReplaceAll(phpVersion, ".", "")
	fpmUnit := "lerd-php" + versionShort + "-fpm"
	container := "lerd-php" + versionShort + "-fpm"
	unitName := "lerd-queue-" + siteName

	artisanArgs := fmt.Sprintf("queue:work --queue=%s --tries=%d --timeout=%d", queue, tries, timeout)
	unit := fmt.Sprintf(`[Unit]
Description=Lerd Queue Worker (%s)
After=network.target %s.service
BindsTo=%s.service

[Service]
Type=simple
Restart=on-failure
RestartSec=5
ExecStart=%s exec -w %s %s php artisan %s

[Install]
WantedBy=default.target
`, siteName, fpmUnit, fpmUnit, podman.PodmanBin(), site.Path, container, artisanArgs)

	if err := lerdSystemd.WriteService(unitName, unit); err != nil {
		return toolErr("writing service unit: " + err.Error()), nil
	}
	if err := podman.DaemonReloadFn(); err != nil {
		return toolErr("daemon-reload: " + err.Error()), nil
	}
	_ = lerdSystemd.EnableService(unitName)
	if err := lerdSystemd.StartService(unitName); err != nil {
		return toolErr("starting queue worker: " + err.Error()), nil
	}
	return toolOK(fmt.Sprintf("Queue worker started for %s (queue: %s)\nLogs: journalctl --user -u %s -f", siteName, queue, unitName)), nil
}

func execQueueStop(args map[string]any) (any, *rpcError) {
	siteName := strArg(args, "site")
	if siteName == "" {
		return toolErr("site is required"), nil
	}

	unitName := "lerd-queue-" + siteName
	unitFile := filepath.Join(config.SystemdUserDir(), unitName+".service")

	_ = lerdSystemd.DisableService(unitName)
	_ = podman.StopUnit(unitName)
	if err := os.Remove(unitFile); err != nil && !os.IsNotExist(err) {
		return toolErr("removing unit file: " + err.Error()), nil
	}
	_ = podman.DaemonReloadFn()
	return toolOK("Queue worker stopped for " + siteName), nil
}

func execReverbStart(args map[string]any) (any, *rpcError) {
	siteName := strArg(args, "site")
	if siteName == "" {
		return toolErr("site is required"), nil
	}
	site, err := config.FindSite(siteName)
	if err != nil {
		return toolErr("site not found: " + siteName), nil
	}
	phpVersion := site.PHPVersion
	if detected, err := phpDet.DetectVersion(site.Path); err == nil && detected != "" {
		phpVersion = detected
	}
	versionShort := strings.ReplaceAll(phpVersion, ".", "")
	fpmUnit := "lerd-php" + versionShort + "-fpm"
	container := "lerd-php" + versionShort + "-fpm"
	unitName := "lerd-reverb-" + siteName

	unit := fmt.Sprintf(`[Unit]
Description=Lerd Reverb (%s)
After=network.target %s.service
BindsTo=%s.service

[Service]
Type=simple
Restart=on-failure
RestartSec=5
ExecStart=%s exec -w %s %s php artisan reverb:start

[Install]
WantedBy=default.target
`, siteName, fpmUnit, fpmUnit, podman.PodmanBin(), site.Path, container)

	if err := lerdSystemd.WriteService(unitName, unit); err != nil {
		return toolErr("writing service unit: " + err.Error()), nil
	}
	if err := podman.DaemonReloadFn(); err != nil {
		return toolErr("daemon-reload: " + err.Error()), nil
	}
	_ = lerdSystemd.EnableService(unitName)
	if err := lerdSystemd.StartService(unitName); err != nil {
		return toolErr("starting reverb: " + err.Error()), nil
	}
	return toolOK(fmt.Sprintf("Reverb started for %s\nLogs: journalctl --user -u %s -f", siteName, unitName)), nil
}

func execReverbStop(args map[string]any) (any, *rpcError) {
	siteName := strArg(args, "site")
	if siteName == "" {
		return toolErr("site is required"), nil
	}
	unitName := "lerd-reverb-" + siteName
	unitFile := filepath.Join(config.SystemdUserDir(), unitName+".service")
	_ = lerdSystemd.DisableService(unitName)
	_ = podman.StopUnit(unitName)
	if err := os.Remove(unitFile); err != nil && !os.IsNotExist(err) {
		return toolErr("removing unit file: " + err.Error()), nil
	}
	_ = podman.DaemonReloadFn()
	return toolOK("Reverb stopped for " + siteName), nil
}

func execHorizonStart(args map[string]any) (any, *rpcError) {
	siteName := strArg(args, "site")
	if siteName == "" {
		return toolErr("site is required"), nil
	}
	site, err := config.FindSite(siteName)
	if err != nil {
		return toolErr("site not found: " + siteName), nil
	}
	// Check composer.json for laravel/horizon
	composerData, readErr := os.ReadFile(filepath.Join(site.Path, "composer.json"))
	if readErr != nil || !strings.Contains(string(composerData), `"laravel/horizon"`) {
		return toolErr("laravel/horizon is not installed in " + siteName), nil
	}
	phpVersion := site.PHPVersion
	if detected, err := phpDet.DetectVersion(site.Path); err == nil && detected != "" {
		phpVersion = detected
	}
	versionShort := strings.ReplaceAll(phpVersion, ".", "")
	fpmUnit := "lerd-php" + versionShort + "-fpm"
	container := "lerd-php" + versionShort + "-fpm"
	unitName := "lerd-horizon-" + siteName

	unit := fmt.Sprintf(`[Unit]
Description=Lerd Horizon (%s)
After=network.target %s.service
BindsTo=%s.service

[Service]
Type=simple
Restart=always
RestartSec=5
ExecStart=%s exec -w %s %s php artisan horizon

[Install]
WantedBy=default.target
`, siteName, fpmUnit, fpmUnit, podman.PodmanBin(), site.Path, container)

	if err := lerdSystemd.WriteService(unitName, unit); err != nil {
		return toolErr("writing service unit: " + err.Error()), nil
	}
	if err := podman.DaemonReloadFn(); err != nil {
		return toolErr("daemon-reload: " + err.Error()), nil
	}
	_ = lerdSystemd.EnableService(unitName)
	if err := lerdSystemd.StartService(unitName); err != nil {
		return toolErr("starting horizon: " + err.Error()), nil
	}
	return toolOK(fmt.Sprintf("Horizon started for %s\nLogs: journalctl --user -u %s -f", siteName, unitName)), nil
}

func execHorizonStop(args map[string]any) (any, *rpcError) {
	siteName := strArg(args, "site")
	if siteName == "" {
		return toolErr("site is required"), nil
	}
	unitName := "lerd-horizon-" + siteName
	unitFile := filepath.Join(config.SystemdUserDir(), unitName+".service")
	_ = lerdSystemd.DisableService(unitName)
	_ = podman.StopUnit(unitName)
	if err := os.Remove(unitFile); err != nil && !os.IsNotExist(err) {
		return toolErr("removing unit file: " + err.Error()), nil
	}
	_ = podman.DaemonReloadFn()
	return toolOK("Horizon stopped for " + siteName), nil
}

func execScheduleStart(args map[string]any) (any, *rpcError) {
	siteName := strArg(args, "site")
	if siteName == "" {
		return toolErr("site is required"), nil
	}
	site, err := config.FindSite(siteName)
	if err != nil {
		return toolErr("site not found: " + siteName), nil
	}
	phpVersion := site.PHPVersion
	if detected, err := phpDet.DetectVersion(site.Path); err == nil && detected != "" {
		phpVersion = detected
	}
	versionShort := strings.ReplaceAll(phpVersion, ".", "")
	fpmUnit := "lerd-php" + versionShort + "-fpm"
	container := "lerd-php" + versionShort + "-fpm"
	unitName := "lerd-schedule-" + siteName

	unit := fmt.Sprintf(`[Unit]
Description=Lerd Scheduler (%s)
After=network.target %s.service
BindsTo=%s.service

[Service]
Type=simple
Restart=always
RestartSec=5
ExecStart=%s exec -w %s %s php artisan schedule:work

[Install]
WantedBy=default.target
`, siteName, fpmUnit, fpmUnit, podman.PodmanBin(), site.Path, container)

	if err := lerdSystemd.WriteService(unitName, unit); err != nil {
		return toolErr("writing service unit: " + err.Error()), nil
	}
	if err := podman.DaemonReloadFn(); err != nil {
		return toolErr("daemon-reload: " + err.Error()), nil
	}
	_ = lerdSystemd.EnableService(unitName)
	if err := lerdSystemd.StartService(unitName); err != nil {
		return toolErr("starting scheduler: " + err.Error()), nil
	}
	return toolOK(fmt.Sprintf("Scheduler started for %s\nLogs: journalctl --user -u %s -f", siteName, unitName)), nil
}

func execScheduleStop(args map[string]any) (any, *rpcError) {
	siteName := strArg(args, "site")
	if siteName == "" {
		return toolErr("site is required"), nil
	}
	unitName := "lerd-schedule-" + siteName
	unitFile := filepath.Join(config.SystemdUserDir(), unitName+".service")
	_ = lerdSystemd.DisableService(unitName)
	_ = podman.StopUnit(unitName)
	if err := os.Remove(unitFile); err != nil && !os.IsNotExist(err) {
		return toolErr("removing unit file: " + err.Error()), nil
	}
	_ = podman.DaemonReloadFn()
	return toolOK("Scheduler stopped for " + siteName), nil
}

func execStripeListen(args map[string]any) (any, *rpcError) {
	siteName := strArg(args, "site")
	if siteName == "" {
		return toolErr("site is required"), nil
	}
	site, err := config.FindSite(siteName)
	if err != nil {
		return toolErr("site not found: " + siteName), nil
	}
	apiKey := strArg(args, "api_key")
	if apiKey == "" {
		apiKey = envfile.ReadKey(filepath.Join(site.Path, ".env"), "STRIPE_SECRET")
	}
	if apiKey == "" {
		return toolErr("Stripe API key required: pass api_key or set STRIPE_SECRET in the site's .env"), nil
	}
	webhookPath := strArg(args, "webhook_path")
	if webhookPath == "" {
		webhookPath = "/stripe/webhook"
	}
	scheme := "http"
	if site.Secured {
		scheme = "https"
	}
	forwardTo := scheme + "://" + site.PrimaryDomain() + webhookPath
	unitName := "lerd-stripe-" + siteName
	containerName := unitName

	unit := fmt.Sprintf(`[Unit]
Description=Lerd Stripe Listener (%s)
After=network.target

[Service]
Type=simple
Restart=on-failure
RestartSec=5
ExecStart=%s run --rm --replace --name %s --network host docker.io/stripe/stripe-cli:latest listen --api-key %s --forward-to %s --skip-verify

[Install]
WantedBy=default.target
`, siteName, podman.PodmanBin(), containerName, apiKey, forwardTo)

	if err := lerdSystemd.WriteService(unitName, unit); err != nil {
		return toolErr("writing service unit: " + err.Error()), nil
	}
	if err := podman.DaemonReloadFn(); err != nil {
		return toolErr("daemon-reload: " + err.Error()), nil
	}
	_ = lerdSystemd.EnableService(unitName)
	if err := lerdSystemd.StartService(unitName); err != nil {
		return toolErr("starting stripe listener: " + err.Error()), nil
	}
	return toolOK(fmt.Sprintf("Stripe listener started for %s\nForwarding to: %s\nLogs: journalctl --user -u %s -f", siteName, forwardTo, unitName)), nil
}

func execStripeListenStop(args map[string]any) (any, *rpcError) {
	siteName := strArg(args, "site")
	if siteName == "" {
		return toolErr("site is required"), nil
	}
	unitName := "lerd-stripe-" + siteName
	unitFile := filepath.Join(config.SystemdUserDir(), unitName+".service")
	_ = lerdSystemd.DisableService(unitName)
	_ = podman.StopUnit(unitName)
	if err := os.Remove(unitFile); err != nil && !os.IsNotExist(err) {
		return toolErr("removing unit file: " + err.Error()), nil
	}
	_ = podman.DaemonReloadFn()
	return toolOK("Stripe listener stopped for " + siteName), nil
}

func execLogs(args map[string]any) (any, *rpcError) {
	target := strArg(args, "target")
	lines := intArg(args, "lines", 50)

	// When no target is given, derive the FPM container from the current site path.
	if target == "" {
		projectPath := resolvedPath(args)
		if projectPath == "" {
			return toolErr("target is required (or set LERD_SITE_PATH via mcp:inject)"), nil
		}
		site, err := config.FindSiteByPath(projectPath)
		if err != nil {
			return toolErr("could not find site for path: " + projectPath), nil
		}
		target = site.Name
	}

	container, err := resolveLogsContainer(target)
	if err != nil {
		return toolErr(err.Error()), nil
	}

	var out bytes.Buffer
	cmd := podman.Cmd("logs", "--tail", fmt.Sprintf("%d", lines), container)
	cmd.Stdout = &out
	cmd.Stderr = &out
	_ = cmd.Run() // non-zero exit if container not running is fine — we return what we have

	return toolOK(stripANSI(strings.TrimSpace(out.String()))), nil
}

func resolveLogsContainer(target string) (string, error) {
	if target == "nginx" {
		return "lerd-nginx", nil
	}
	if isKnownService(target) {
		return "lerd-" + target, nil
	}
	// PHP version like "8.4" — match digits.digits only, not domain names
	if phpVersionRe.MatchString(target) {
		short := strings.ReplaceAll(target, ".", "")
		return "lerd-php" + short + "-fpm", nil
	}
	// Site name — look up PHP version from registry
	if site, err := config.FindSite(target); err == nil {
		phpVersion := site.PHPVersion
		if detected, err := phpDet.DetectVersion(site.Path); err == nil && detected != "" {
			phpVersion = detected
		}
		short := strings.ReplaceAll(phpVersion, ".", "")
		return "lerd-php" + short + "-fpm", nil
	}
	return "", fmt.Errorf("unknown log target %q — valid: nginx, service name, PHP version (e.g. 8.4), or site name", target)
}

func execComposer(args map[string]any) (any, *rpcError) {
	projectPath := resolvedPath(args)
	if projectPath == "" {
		return toolErr("path is required — pass a path argument or open Claude in the project directory"), nil
	}
	composerArgs := strSliceArg(args, "args")
	if len(composerArgs) == 0 {
		return toolErr("args is required and must be a non-empty array"), nil
	}

	phpVersion, err := phpDet.DetectVersion(projectPath)
	if err != nil {
		cfg, cfgErr := config.LoadGlobal()
		if cfgErr != nil {
			return toolErr("failed to detect PHP version: " + err.Error()), nil
		}
		phpVersion = cfg.PHP.DefaultVersion
	}

	short := strings.ReplaceAll(phpVersion, ".", "")
	container := "lerd-php" + short + "-fpm"

	cmdArgs := []string{"exec", "-w", projectPath, container, "composer"}
	cmdArgs = append(cmdArgs, composerArgs...)

	var out bytes.Buffer
	cmd := podman.Cmd(cmdArgs...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return toolErr(fmt.Sprintf("composer failed (%v):\n%s", err, stripANSI(out.String()))), nil
	}
	return toolOK(stripANSI(strings.TrimSpace(out.String()))), nil
}

func execVendorBins(args map[string]any) (any, *rpcError) {
	projectPath := resolvedPath(args)
	if projectPath == "" {
		return toolErr("path is required — pass a path argument or open Claude in the project directory"), nil
	}
	dir := filepath.Join(projectPath, "vendor", "bin")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return toolErr("no vendor/bin directory — run composer install first"), nil
		}
		return toolErr("failed to read vendor/bin: " + err.Error()), nil
	}
	var bins []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		bins = append(bins, e.Name())
	}
	sort.Strings(bins)
	if len(bins) == 0 {
		return toolOK("vendor/bin is empty"), nil
	}
	return toolOK(strings.Join(bins, "\n")), nil
}

func execVendorRun(args map[string]any) (any, *rpcError) {
	projectPath := resolvedPath(args)
	if projectPath == "" {
		return toolErr("path is required — pass a path argument or open Claude in the project directory"), nil
	}
	bin := strArg(args, "bin")
	if bin == "" {
		return toolErr("bin is required"), nil
	}
	// Reject path separators — composer bins are flat filenames.
	if strings.ContainsAny(bin, "/\\") {
		return toolErr("bin must be a plain filename, not a path"), nil
	}
	binPath := filepath.Join(projectPath, "vendor", "bin", bin)
	info, statErr := os.Stat(binPath)
	if statErr != nil || info.IsDir() {
		return toolErr(fmt.Sprintf("vendor/bin/%s not found in %s", bin, projectPath)), nil
	}
	binArgs := strSliceArg(args, "args")

	phpVersion, err := phpDet.DetectVersion(projectPath)
	if err != nil {
		cfg, cfgErr := config.LoadGlobal()
		if cfgErr != nil {
			return toolErr("failed to detect PHP version: " + err.Error()), nil
		}
		phpVersion = cfg.PHP.DefaultVersion
	}

	short := strings.ReplaceAll(phpVersion, ".", "")
	container := "lerd-php" + short + "-fpm"

	cmdArgs := []string{"exec", "-w", projectPath, container, "php", "vendor/bin/" + bin}
	cmdArgs = append(cmdArgs, binArgs...)

	var out bytes.Buffer
	cmd := podman.Cmd(cmdArgs...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return toolErr(fmt.Sprintf("vendor/bin/%s failed (%v):\n%s", bin, err, stripANSI(out.String()))), nil
	}
	return toolOK(stripANSI(strings.TrimSpace(out.String()))), nil
}

func execNodeInstall(args map[string]any) (any, *rpcError) {
	version := strArg(args, "version")
	if version == "" {
		return toolErr("version is required"), nil
	}

	fnmPath := filepath.Join(config.BinDir(), "fnm")
	if _, err := os.Stat(fnmPath); err != nil {
		return toolErr("fnm not found — run 'lerd install' to set up Node.js management"), nil
	}

	var out bytes.Buffer
	cmd := exec.Command(fnmPath, "install", version)
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return toolErr(fmt.Sprintf("fnm install %s failed (%v):\n%s", version, err, stripANSI(out.String()))), nil
	}
	return toolOK(stripANSI(strings.TrimSpace(out.String()))), nil
}

func execNodeUninstall(args map[string]any) (any, *rpcError) {
	version := strArg(args, "version")
	if version == "" {
		return toolErr("version is required"), nil
	}

	fnmPath := filepath.Join(config.BinDir(), "fnm")
	if _, err := os.Stat(fnmPath); err != nil {
		return toolErr("fnm not found — run 'lerd install' to set up Node.js management"), nil
	}

	var out bytes.Buffer
	cmd := exec.Command(fnmPath, "uninstall", version)
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return toolErr(fmt.Sprintf("fnm uninstall %s failed (%v):\n%s", version, err, stripANSI(out.String()))), nil
	}
	return toolOK(stripANSI(strings.TrimSpace(out.String()))), nil
}

func execRuntimeVersions() (any, *rpcError) {
	cfg, _ := config.LoadGlobal()

	// PHP versions
	phpVersions, _ := phpDet.ListInstalled()
	defaultPHP := ""
	if cfg != nil {
		defaultPHP = cfg.PHP.DefaultVersion
	}

	// Node.js versions via fnm
	fnmPath := filepath.Join(config.BinDir(), "fnm")
	var nodeVersions []string
	defaultNode := ""
	if cfg != nil {
		defaultNode = cfg.Node.DefaultVersion
	}
	if _, err := os.Stat(fnmPath); err == nil {
		var out bytes.Buffer
		cmd := exec.Command(fnmPath, "list")
		cmd.Stdout = &out
		cmd.Stderr = &out
		if cmd.Run() == nil {
			for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
				line = strings.TrimSpace(line)
				// fnm list output: "* v20.11.0 default" or "  v18.20.0"
				line = strings.TrimPrefix(line, "* ")
				line = strings.TrimPrefix(line, "  ")
				if line != "" {
					nodeVersions = append(nodeVersions, line)
				}
			}
		}
	}

	type runtimeEntry struct {
		Installed      []string `json:"installed"`
		DefaultVersion string   `json:"default_version"`
	}
	type runtimeResult struct {
		PHP  runtimeEntry `json:"php"`
		Node runtimeEntry `json:"node"`
	}

	if phpVersions == nil {
		phpVersions = []string{}
	}
	if nodeVersions == nil {
		nodeVersions = []string{}
	}

	data, _ := json.MarshalIndent(runtimeResult{
		PHP:  runtimeEntry{Installed: phpVersions, DefaultVersion: defaultPHP},
		Node: runtimeEntry{Installed: nodeVersions, DefaultVersion: defaultNode},
	}, "", "  ")
	return toolOK(string(data)), nil
}

func execStatus() (any, *rpcError) {
	cfg, _ := config.LoadGlobal()
	tld := "test"
	if cfg != nil && cfg.DNS.TLD != "" {
		tld = cfg.DNS.TLD
	}

	type phpStatus struct {
		Version string `json:"version"`
		Running bool   `json:"running"`
	}
	type result struct {
		DNS struct {
			OK  bool   `json:"ok"`
			TLD string `json:"tld"`
		} `json:"dns"`
		Nginx struct {
			Running bool `json:"running"`
		} `json:"nginx"`
		Watcher struct {
			Running bool `json:"running"`
		} `json:"watcher"`
		PHPFPMs []phpStatus `json:"php_fpms"`
	}

	var r result
	r.DNS.TLD = tld
	r.DNS.OK, _ = dns.Check(tld)
	r.Nginx.Running, _ = podman.ContainerRunning("lerd-nginx")
	r.Watcher.Running = exec.Command("systemctl", "--user", "is-active", "--quiet", "lerd-watcher").Run() == nil

	versions, _ := phpDet.ListInstalled()
	for _, v := range versions {
		short := strings.ReplaceAll(v, ".", "")
		running, _ := podman.ContainerRunning("lerd-php" + short + "-fpm")
		r.PHPFPMs = append(r.PHPFPMs, phpStatus{Version: v, Running: running})
	}

	data, _ := json.MarshalIndent(r, "", "  ")
	return toolOK(string(data)), nil
}

func execDoctor() (any, *rpcError) {
	type checkResult struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		Detail string `json:"detail,omitempty"`
	}
	type doctorResult struct {
		Version      string        `json:"version"`
		Checks       []checkResult `json:"checks"`
		Failures     int           `json:"failures"`
		Warnings     int           `json:"warnings"`
		UpdateAvail  string        `json:"update_available,omitempty"`
		PHPInstalled []string      `json:"php_installed"`
		PHPDefault   string        `json:"php_default,omitempty"`
		NodeDefault  string        `json:"node_default,omitempty"`
	}

	var r doctorResult
	r.Version = version.String()
	var checks []checkResult

	add := func(name, status, detail string) {
		checks = append(checks, checkResult{Name: name, Status: status, Detail: detail})
	}

	// Prerequisites
	if _, err := exec.LookPath("podman"); err != nil {
		add("podman", "fail", "not found in PATH")
	} else if err := podman.RunSilent("info"); err != nil {
		add("podman", "fail", "podman info failed — daemon not running?")
	} else {
		add("podman", "ok", "")
	}

	if out, err := exec.Command("systemctl", "--user", "is-system-running").Output(); err != nil {
		state := strings.TrimSpace(string(out))
		if state == "degraded" {
			add("systemd_user_session", "warn", "degraded — some units have failed")
		} else {
			add("systemd_user_session", "fail", "state="+state)
		}
	} else {
		add("systemd_user_session", "ok", "")
	}

	currentUser := os.Getenv("USER")
	if currentUser == "" {
		currentUser = os.Getenv("LOGNAME")
	}
	if currentUser != "" {
		out, err := exec.Command("loginctl", "show-user", currentUser).Output()
		if err != nil || !strings.Contains(string(out), "Linger=yes") {
			add("systemd_linger", "warn", "services won't survive logout")
		} else {
			add("systemd_linger", "ok", "")
		}
	}

	quadletDir := config.QuadletDir()
	if err := dirWritable(quadletDir); err != nil {
		add("quadlet_dir", "fail", err.Error())
	} else {
		add("quadlet_dir", "ok", "")
	}

	dataDir := config.DataDir()
	if err := dirWritable(dataDir); err != nil {
		add("data_dir", "fail", err.Error())
	} else {
		add("data_dir", "ok", "")
	}

	// Configuration
	cfg, cfgErr := config.LoadGlobal()
	if cfgErr != nil {
		add("config", "fail", cfgErr.Error())
		cfg = nil
	} else {
		add("config", "ok", "")
	}

	if cfg != nil {
		if cfg.PHP.DefaultVersion == "" {
			add("php_default_version", "warn", "not set")
		} else {
			add("php_default_version", "ok", cfg.PHP.DefaultVersion)
			r.PHPDefault = cfg.PHP.DefaultVersion
		}
		r.NodeDefault = cfg.Node.DefaultVersion

		if cfg.Nginx.HTTPPort <= 0 || cfg.Nginx.HTTPSPort <= 0 {
			add("nginx_ports", "fail", fmt.Sprintf("http=%d https=%d", cfg.Nginx.HTTPPort, cfg.Nginx.HTTPSPort))
		} else {
			add("nginx_ports", "ok", fmt.Sprintf("%d/%d", cfg.Nginx.HTTPPort, cfg.Nginx.HTTPSPort))
		}
	}

	// DNS
	tld := "test"
	if cfg != nil && cfg.DNS.TLD != "" {
		tld = cfg.DNS.TLD
	}
	if resolved, _ := dns.Check(tld); resolved {
		add("dns_resolution", "ok", "."+tld)
	} else {
		add("dns_resolution", "fail", "."+tld+" not resolving")
	}

	// Ports
	nginxRunning, _ := podman.ContainerRunning("lerd-nginx")
	if nginxRunning {
		add("nginx", "ok", "running")
	} else {
		add("nginx", "warn", "not running")
	}

	// PHP images
	phpVersions, _ := phpDet.ListInstalled()
	r.PHPInstalled = phpVersions
	if r.PHPInstalled == nil {
		r.PHPInstalled = []string{}
	}
	for _, v := range phpVersions {
		short := strings.ReplaceAll(v, ".", "")
		image := "lerd-php" + short + "-fpm:local"
		if !podman.ImageExists(image) {
			add("php_"+v+"_image", "fail", "missing")
		} else {
			add("php_"+v+"_image", "ok", "")
		}
	}

	// Update check
	if updateInfo, _ := lerdUpdate.CachedUpdateCheck(version.Version); updateInfo != nil {
		r.UpdateAvail = updateInfo.LatestVersion
	}

	r.Checks = checks
	for _, c := range checks {
		switch c.Status {
		case "fail":
			r.Failures++
		case "warn":
			r.Warnings++
		}
	}

	data, _ := json.MarshalIndent(r, "", "  ")
	return toolOK(string(data)), nil
}

func dirWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot create: %v", err)
	}
	tmp, err := os.CreateTemp(dir, ".lerd-mcp-*")
	if err != nil {
		return fmt.Errorf("not writable: %v", err)
	}
	tmp.Close()
	os.Remove(tmp.Name())
	return nil
}

func execWhich(args map[string]any) (any, *rpcError) {
	projectPath := resolvedPath(args)
	if projectPath == "" {
		return toolErr("path is required — pass a path argument or open Claude in the project directory"), nil
	}

	self, err := os.Executable()
	if err != nil {
		return toolErr("could not resolve lerd executable: " + err.Error()), nil
	}

	var out bytes.Buffer
	cmd := exec.Command(self, "which")
	cmd.Dir = projectPath
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return toolErr(fmt.Sprintf("which failed (%v):\n%s", err, stripANSI(out.String()))), nil
	}
	return toolOK(stripANSI(strings.TrimSpace(out.String()))), nil
}

func execCheck(args map[string]any) (any, *rpcError) {
	projectPath := resolvedPath(args)
	if projectPath == "" {
		return toolErr("path is required — pass a path argument or open Claude in the project directory"), nil
	}

	path := filepath.Join(projectPath, ".lerd.yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return toolErr("no .lerd.yaml found in " + projectPath), nil
	}

	cfg, err := config.LoadProjectConfig(projectPath)
	if err != nil {
		return toolErr("invalid .lerd.yaml: " + err.Error()), nil
	}

	type checkItem struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		Detail string `json:"detail,omitempty"`
	}
	type checkResult struct {
		Valid    bool        `json:"valid"`
		Errors   int         `json:"errors"`
		Warnings int         `json:"warnings"`
		Items    []checkItem `json:"items"`
	}

	var r checkResult
	add := func(name, status, detail string) {
		r.Items = append(r.Items, checkItem{Name: name, Status: status, Detail: detail})
		switch status {
		case "fail":
			r.Errors++
		case "warn":
			r.Warnings++
		}
	}

	// PHP version
	if cfg.PHPVersion != "" {
		if err := validatePHPVersionMCP(cfg.PHPVersion); err != nil {
			add("php_version", "fail", cfg.PHPVersion+" — "+err.Error())
		} else if !phpDet.IsInstalled(cfg.PHPVersion) {
			add("php_version", "warn", cfg.PHPVersion+" not installed")
		} else {
			add("php_version", "ok", cfg.PHPVersion)
		}
	}

	// Node version
	if cfg.NodeVersion != "" {
		add("node_version", "ok", cfg.NodeVersion)
	}

	// Framework
	if cfg.Framework != "" {
		if cfg.FrameworkDef != nil {
			add("framework", "ok", cfg.Framework+" (inline)")
		} else if _, ok := config.GetFramework(cfg.Framework); ok {
			add("framework", "ok", cfg.Framework)
		} else {
			add("framework", "warn", cfg.Framework+" is not a known framework")
		}
	}

	// Workers
	if len(cfg.Workers) > 0 {
		fwName := cfg.Framework
		if fwName == "" {
			fwName, _ = config.DetectFrameworkForDir(projectPath)
		}
		fw, hasFw := config.GetFramework(fwName)

		hasQueue, hasHorizon := false, false
		for _, w := range cfg.Workers {
			if w == "queue" {
				hasQueue = true
			}
			if w == "horizon" {
				hasHorizon = true
			}
			switch w {
			case "horizon":
				if !siteHasComposerPkg(projectPath, `"laravel/horizon"`) {
					add("worker_"+w, "warn", "laravel/horizon not installed")
				} else {
					add("worker_"+w, "ok", "")
				}
			case "reverb":
				if !siteUsesReverb(projectPath) {
					add("worker_"+w, "warn", "reverb not configured")
				} else {
					add("worker_"+w, "ok", "")
				}
			case "queue", "schedule":
				if hasFw && fw.Workers != nil {
					if _, ok := fw.Workers[w]; ok {
						add("worker_"+w, "ok", "")
					} else {
						add("worker_"+w, "warn", "not defined for framework "+fwName)
					}
				} else {
					add("worker_"+w, "warn", "no framework detected")
				}
			default:
				if hasFw && fw.Workers != nil {
					if _, ok := fw.Workers[w]; ok {
						add("worker_"+w, "ok", "")
					} else {
						add("worker_"+w, "fail", "not defined for framework "+fwName)
					}
				} else {
					add("worker_"+w, "fail", "no framework worker definition found")
				}
			}
		}
		if hasQueue && hasHorizon {
			add("workers_conflict", "warn", "both queue and horizon listed — horizon manages queues")
		}
		if hasQueue && siteHasComposerPkg(projectPath, `"laravel/horizon"`) {
			add("workers_conflict", "warn", "queue listed but horizon installed — horizon will be started instead")
		}
	}

	// Services
	for _, svc := range cfg.Services {
		if svc.Custom != nil {
			if svc.Custom.Image == "" {
				add("service_"+svc.Name, "fail", "inline definition missing image")
			} else {
				add("service_"+svc.Name, "ok", "inline, image: "+svc.Custom.Image)
			}
			continue
		}
		if isKnownService(svc.Name) {
			add("service_"+svc.Name, "ok", "")
			continue
		}
		if _, err := config.LoadCustomService(svc.Name); err == nil {
			add("service_"+svc.Name, "ok", "custom")
		} else {
			add("service_"+svc.Name, "fail", "not a built-in and no definition found")
		}
	}

	r.Valid = r.Errors == 0
	data, _ := json.MarshalIndent(r, "", "  ")
	return toolOK(string(data)), nil
}

func validatePHPVersionMCP(s string) error {
	parts := strings.SplitN(s, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("must be MAJOR.MINOR format")
	}
	for _, p := range parts {
		for _, c := range p {
			if c < '0' || c > '9' {
				return fmt.Errorf("must be MAJOR.MINOR format")
			}
		}
	}
	return nil
}

func siteHasComposerPkg(sitePath, pkg string) bool {
	data, err := os.ReadFile(filepath.Join(sitePath, "composer.json"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), pkg)
}

func siteUsesReverb(sitePath string) bool {
	if siteHasComposerPkg(sitePath, `"laravel/reverb"`) {
		return true
	}
	for _, name := range []string{".env", ".env.example"} {
		if envfile.ReadKey(filepath.Join(sitePath, name), "BROADCAST_CONNECTION") == "reverb" {
			return true
		}
	}
	return false
}

func execServiceAdd(args map[string]any) (any, *rpcError) {
	name := strArg(args, "name")
	if name == "" {
		return toolErr("name is required"), nil
	}
	image := strArg(args, "image")
	if image == "" {
		return toolErr("image is required"), nil
	}

	if isKnownService(name) {
		return toolErr(name + " is a built-in service and cannot be redefined"), nil
	}
	if _, err := config.LoadCustomService(name); err == nil {
		return toolErr("custom service " + name + " already exists; remove it first with service_remove"), nil
	}

	svc := &config.CustomService{
		Name:        name,
		Image:       image,
		Ports:       strSliceArg(args, "ports"),
		EnvVars:     strSliceArg(args, "env_vars"),
		Description: strArg(args, "description"),
		Dashboard:   strArg(args, "dashboard"),
		DataDir:     strArg(args, "data_dir"),
		DependsOn:   strSliceArg(args, "depends_on"),
	}

	if envList := strSliceArg(args, "environment"); len(envList) > 0 {
		svc.Environment = make(map[string]string, len(envList))
		for _, kv := range envList {
			k, v, _ := strings.Cut(kv, "=")
			svc.Environment[k] = v
		}
	}

	if err := config.SaveCustomService(svc); err != nil {
		return toolErr("saving service config: " + err.Error()), nil
	}

	if err := serviceops.EnsureCustomServiceQuadlet(svc); err != nil {
		return toolErr("writing quadlet: " + err.Error()), nil
	}

	return toolOK(fmt.Sprintf("Custom service %q added. Start it with service_start(name: %q).", name, name)), nil
}

func execServiceRemove(args map[string]any) (any, *rpcError) {
	name := strArg(args, "name")
	if name == "" {
		return toolErr("name is required"), nil
	}
	if isKnownService(name) {
		return toolErr(name + " is a built-in service and cannot be removed"), nil
	}

	unit := "lerd-" + name
	_ = podman.StopUnit(unit)
	podman.RemoveContainer(unit)
	if err := podman.RemoveQuadlet(unit); err != nil {
		return toolErr("removing quadlet: " + err.Error()), nil
	}
	_ = podman.DaemonReloadFn()
	if err := config.RemoveCustomService(name); err != nil {
		return toolErr("removing service config: " + err.Error()), nil
	}

	return toolOK(fmt.Sprintf("Service %q removed. Persistent data was NOT deleted.", name)), nil
}

func execServicePresetList(_ map[string]any) (any, *rpcError) {
	presets, err := config.ListPresets()
	if err != nil {
		return toolErr("listing presets: " + err.Error()), nil
	}
	type versionEntry struct {
		Tag       string `json:"tag"`
		Label     string `json:"label,omitempty"`
		Image     string `json:"image"`
		Installed bool   `json:"installed"`
	}
	type entry struct {
		Name           string         `json:"name"`
		Description    string         `json:"description,omitempty"`
		Image          string         `json:"image,omitempty"`
		Dashboard      string         `json:"dashboard,omitempty"`
		DependsOn      []string       `json:"depends_on,omitempty"`
		Installed      bool           `json:"installed"`
		DefaultVersion string         `json:"default_version,omitempty"`
		Versions       []versionEntry `json:"versions,omitempty"`
	}
	out := make([]entry, 0, len(presets))
	for _, p := range presets {
		e := entry{
			Name:           p.Name,
			Description:    p.Description,
			Image:          p.Image,
			Dashboard:      p.Dashboard,
			DependsOn:      p.DependsOn,
			DefaultVersion: p.DefaultVersion,
		}
		if len(p.Versions) == 0 {
			if _, err := config.LoadCustomService(p.Name); err == nil {
				e.Installed = true
			}
		} else {
			anyInstalled := false
			for _, v := range p.Versions {
				name := p.Name + "-" + config.SanitizeImageTag(v.Tag)
				_, loadErr := config.LoadCustomService(name)
				vi := versionEntry{
					Tag:       v.Tag,
					Label:     v.Label,
					Image:     v.Image,
					Installed: loadErr == nil,
				}
				if vi.Installed {
					anyInstalled = true
				}
				e.Versions = append(e.Versions, vi)
			}
			e.Installed = anyInstalled
		}
		out = append(out, e)
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return toolErr("encoding presets: " + err.Error()), nil
	}
	return toolOK(string(data)), nil
}

func execServicePresetInstall(args map[string]any) (any, *rpcError) {
	name := strArg(args, "name")
	if name == "" {
		return toolErr("name is required"), nil
	}
	version := strArg(args, "version")
	svc, err := serviceops.InstallPresetByName(name, version)
	if err != nil {
		return toolErr(err.Error()), nil
	}
	msg := fmt.Sprintf("Installed preset %q. Start it with service_start(name: %q).", svc.Name, svc.Name)
	if svc.Dashboard != "" {
		msg += " Dashboard: " + svc.Dashboard
	}
	if len(svc.DependsOn) > 0 {
		msg += " Dependencies (auto-started on start): " + strings.Join(svc.DependsOn, ", ")
	}
	return toolOK(msg), nil
}

func execServiceExpose(args map[string]any) (any, *rpcError) {
	name := strArg(args, "name")
	port := strArg(args, "port")
	if name == "" {
		return toolErr("name is required"), nil
	}
	if port == "" {
		return toolErr("port is required"), nil
	}
	if !isKnownService(name) {
		return toolErr(name + " is not a built-in service"), nil
	}
	remove := boolArg(args, "remove")

	cfg, err := config.LoadGlobal()
	if err != nil {
		return toolErr("loading config: " + err.Error()), nil
	}
	svcCfg := cfg.Services[name]
	if remove {
		filtered := svcCfg.ExtraPorts[:0]
		for _, p := range svcCfg.ExtraPorts {
			if p != port {
				filtered = append(filtered, p)
			}
		}
		svcCfg.ExtraPorts = filtered
	} else {
		found := false
		for _, p := range svcCfg.ExtraPorts {
			if p == port {
				found = true
				break
			}
		}
		if !found {
			svcCfg.ExtraPorts = append(svcCfg.ExtraPorts, port)
		}
	}
	cfg.Services[name] = svcCfg
	if err := config.SaveGlobal(cfg); err != nil {
		return toolErr("saving config: " + err.Error()), nil
	}

	unitName := "lerd-" + name
	content, err := podman.GetQuadletTemplate(unitName + ".container")
	if err != nil {
		return toolErr("quadlet template not found: " + err.Error()), nil
	}
	content = podman.ApplyImage(content, svcCfg.Image)
	if len(svcCfg.ExtraPorts) > 0 {
		content = podman.ApplyExtraPorts(content, svcCfg.ExtraPorts)
	}
	if err := podman.WriteContainerUnitFn(unitName, content); err != nil {
		return toolErr("writing quadlet: " + err.Error()), nil
	}
	if err := podman.DaemonReloadFn(); err != nil {
		return toolErr("daemon-reload: " + err.Error()), nil
	}

	status, _ := podman.UnitStatus(unitName)
	if status == "active" {
		_ = podman.RestartUnit(unitName)
	}

	action := "added to"
	if remove {
		action = "removed from"
	}
	return toolOK(fmt.Sprintf("Port %s %s %s.", port, action, name)), nil
}

func execServiceEnv(args map[string]any) (any, *rpcError) {
	name := strArg(args, "name")
	if name == "" {
		return toolErr("name is required"), nil
	}

	// Check built-in services first.
	if pairs, ok := builtinServiceEnv[name]; ok {
		vars := make(map[string]string, len(pairs))
		for _, kv := range pairs {
			k, v, _ := strings.Cut(kv, "=")
			vars[k] = v
		}
		return map[string]any{"service": name, "vars": vars}, nil
	}

	// Fall back to custom service env_vars.
	svc, err := config.LoadCustomService(name)
	if err != nil {
		return toolErr(fmt.Sprintf("unknown service %q — not a built-in and no custom service registered with that name", name)), nil
	}
	vars := make(map[string]string, len(svc.EnvVars))
	for _, kv := range svc.EnvVars {
		k, v, _ := strings.Cut(kv, "=")
		vars[k] = v
	}
	return map[string]any{"service": name, "vars": vars}, nil
}

func execEnvSetup(args map[string]any) (any, *rpcError) {
	projectPath := resolvedPath(args)
	if projectPath == "" {
		return toolErr("path is required — pass a path argument or open Claude in the project directory"), nil
	}

	self, err := os.Executable()
	if err != nil {
		return toolErr("could not resolve lerd executable: " + err.Error()), nil
	}

	var out bytes.Buffer
	cmd := exec.Command(self, "env")
	cmd.Dir = projectPath
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return toolErr(fmt.Sprintf("env setup failed (%v):\n%s", err, stripANSI(out.String()))), nil
	}
	return toolOK(stripANSI(strings.TrimSpace(out.String()))), nil
}

// execDbSet sets the database for a Laravel project: persists the choice to
// .lerd.yaml (replacing any existing sqlite/mysql/postgres entry) and re-runs
// `lerd env` so the .env file is rewritten and any required service is started
// + database created (or, for sqlite, the database file is touched).
func execDbSet(args map[string]any) (any, *rpcError) {
	projectPath := resolvedPath(args)
	if projectPath == "" {
		return toolErr("path is required — pass a path argument or open Claude in the project directory"), nil
	}
	choice := strings.ToLower(strings.TrimSpace(strArg(args, "database")))
	switch choice {
	case "sqlite", "mysql", "postgres":
	case "":
		return toolErr("database is required — must be one of: sqlite, mysql, postgres"), nil
	default:
		return toolErr(fmt.Sprintf("invalid database %q — must be one of: sqlite, mysql, postgres", choice)), nil
	}

	// Check existing DB for the summary message.
	previous := ""
	if proj, _ := config.LoadProjectConfig(projectPath); proj != nil {
		dbNames := map[string]bool{"sqlite": true, "mysql": true, "postgres": true}
		for _, svc := range proj.Services {
			if dbNames[svc.Name] {
				previous = svc.Name
				break
			}
		}
	}
	if err := config.ReplaceProjectDBService(projectPath, choice); err != nil {
		return toolErr("saving .lerd.yaml: " + err.Error()), nil
	}

	// Re-exec `lerd env` so the choice is applied to .env immediately. We
	// shell out to the same binary so the existing service-loop, sqlite file
	// creation, and database provisioning logic all run unchanged.
	self, err := os.Executable()
	if err != nil {
		return toolErr("could not resolve lerd executable: " + err.Error()), nil
	}
	var out bytes.Buffer
	cmd := exec.Command(self, "env")
	cmd.Dir = projectPath
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return toolErr(fmt.Sprintf("db_set saved .lerd.yaml but lerd env failed (%v):\n%s", err, out.String())), nil
	}

	summary := fmt.Sprintf("Database set to %s", choice)
	if previous != "" && previous != choice {
		summary = fmt.Sprintf("Database changed from %s to %s", previous, choice)
	}
	return toolOK(summary + "\n\n" + strings.TrimSpace(out.String())), nil
}

func execEnvCheck(args map[string]any) (any, *rpcError) {
	projectPath := resolvedPath(args)
	if projectPath == "" {
		return toolErr("path is required — pass a path argument or open Claude in the project directory"), nil
	}

	examplePath := filepath.Join(projectPath, ".env.example")
	if _, err := os.Stat(examplePath); os.IsNotExist(err) {
		return toolErr("no .env.example found in " + projectPath), nil
	}

	exampleKeys, err := envfile.ReadKeys(examplePath)
	if err != nil {
		return toolErr("reading .env.example: " + err.Error()), nil
	}
	exampleSet := make(map[string]bool, len(exampleKeys))
	for _, k := range exampleKeys {
		exampleSet[k] = true
	}

	// Find all .env* files (excluding .env.example).
	entries, err := os.ReadDir(projectPath)
	if err != nil {
		return toolErr("reading directory: " + err.Error()), nil
	}
	type fileInfo struct {
		name   string
		keySet map[string]bool
		keys   []string
	}
	var files []fileInfo
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, ".env") || e.IsDir() || name == ".env.example" {
			continue
		}
		keys, err := envfile.ReadKeys(filepath.Join(projectPath, name))
		if err != nil {
			continue
		}
		set := make(map[string]bool, len(keys))
		for _, k := range keys {
			set[k] = true
		}
		files = append(files, fileInfo{name: name, keySet: set, keys: keys})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })

	if len(files) == 0 {
		return toolErr("no .env files found — run env_setup first"), nil
	}

	// Build per-key status across all files.
	type keyStatus struct {
		Key     string          `json:"key"`
		Example bool            `json:"in_example"`
		Files   map[string]bool `json:"files"`
	}

	// Collect keys with at least one mismatch.
	mismatched := make(map[string]bool)
	for _, k := range exampleKeys {
		for _, f := range files {
			if !f.keySet[k] {
				mismatched[k] = true
				break
			}
		}
	}
	for _, f := range files {
		for _, k := range f.keys {
			if !exampleSet[k] {
				mismatched[k] = true
			}
		}
	}

	var keys []keyStatus
	sortedKeys := make([]string, 0, len(mismatched))
	for k := range mismatched {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	for _, k := range sortedKeys {
		fs := make(map[string]bool, len(files))
		for _, f := range files {
			fs[f.name] = f.keySet[k]
		}
		keys = append(keys, keyStatus{Key: k, Example: exampleSet[k], Files: fs})
	}

	type result struct {
		InSync bool        `json:"in_sync"`
		Keys   []keyStatus `json:"keys,omitempty"`
		Count  int         `json:"out_of_sync_count"`
	}
	r := result{
		InSync: len(keys) == 0,
		Keys:   keys,
		Count:  len(keys),
	}

	data, _ := json.MarshalIndent(r, "", "  ")
	return toolOK(string(data)), nil
}

func execSiteLink(args map[string]any) (any, *rpcError) {
	projectPath := resolvedPath(args)
	if projectPath == "" {
		return toolErr("path is required — pass a path argument or open Claude in the project directory"), nil
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		return toolErr("loading config: " + err.Error()), nil
	}

	rawName := strArg(args, "name")
	if rawName == "" {
		rawName = filepath.Base(projectPath)
	}
	name, domain := siteops.SiteNameAndDomain(rawName, cfg.DNS.TLD)
	domains := []string{domain}

	// Validate domains are not used by other sites.
	for _, d := range domains {
		if existing, err := config.IsDomainUsed(d); err == nil && existing != nil && existing.Path != projectPath {
			return toolErr(fmt.Sprintf("domain %q is already used by site %q", d, existing.Name)), nil
		}
	}

	// Detect framework so the correct public_dir and workers are used.
	framework := ""
	if name, ok := config.DetectFrameworkForDir(projectPath); ok {
		framework = name
	}

	versions := siteops.DetectSiteVersions(projectPath, framework, cfg.PHP.DefaultVersion, cfg.Node.DefaultVersion)
	phpVersion, nodeVersion := versions.PHP, versions.Node

	secured := siteops.CleanupRelink(projectPath, name)

	site := config.Site{
		Name:        name,
		Domains:     domains,
		Path:        projectPath,
		PHPVersion:  phpVersion,
		NodeVersion: nodeVersion,
		Secured:     secured,
		Framework:   framework,
	}

	if err := config.AddSite(site); err != nil {
		return toolErr("registering site: " + err.Error()), nil
	}

	if err := siteops.FinishLink(site, phpVersion); err != nil {
		return toolErr(err.Error()), nil
	}

	return toolOK(fmt.Sprintf("Linked %s -> %s (PHP %s, Node %s)", name, strings.Join(domains, ", "), phpVersion, nodeVersion)), nil
}

func execSiteUnlink(args map[string]any) (any, *rpcError) {
	projectPath := resolvedPath(args)
	if projectPath == "" {
		return toolErr("path is required — pass a path argument or open Claude in the project directory"), nil
	}

	site, err := config.FindSiteByPath(projectPath)
	if err != nil {
		return toolErr(fmt.Sprintf("no site registered for %s", projectPath)), nil
	}

	cfg, _ := config.LoadGlobal()
	var parkedDirs []string
	if cfg != nil {
		parkedDirs = cfg.ParkedDirectories
	}

	if err := siteops.UnlinkSiteCore(site, parkedDirs); err != nil {
		return toolErr("unlinking site: " + err.Error()), nil
	}

	return toolOK(fmt.Sprintf("Unlinked %s (%s)", site.Name, strings.Join(site.Domains, ", "))), nil
}

func execSiteDomainAdd(args map[string]any) (any, *rpcError) {
	projectPath := resolvedPath(args)
	if projectPath == "" {
		return toolErr("path is required — pass a path argument or open Claude in the project directory"), nil
	}
	domainName := strArg(args, "domain")
	if domainName == "" {
		return toolErr("domain is required"), nil
	}

	site, err := config.FindSiteByPath(projectPath)
	if err != nil {
		return toolErr(fmt.Sprintf("no site registered for %s", projectPath)), nil
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		return toolErr("loading config: " + err.Error()), nil
	}

	fullDomain := strings.ToLower(domainName) + "." + cfg.DNS.TLD

	if site.HasDomain(fullDomain) {
		return toolErr(fmt.Sprintf("site %q already has domain %q", site.Name, fullDomain)), nil
	}
	if existing, err := config.IsDomainUsed(fullDomain); err == nil && existing != nil {
		return toolErr(fmt.Sprintf("domain %q is already used by site %q", fullDomain, existing.Name)), nil
	}

	oldPrimary := site.PrimaryDomain()
	site.Domains = append(site.Domains, fullDomain)

	if err := config.AddSite(*site); err != nil {
		return toolErr("updating site: " + err.Error()), nil
	}

	_ = config.SyncProjectDomains(site.Path, site.Domains, cfg.DNS.TLD)

	if err := siteops.RegenerateSiteVhost(site, oldPrimary); err != nil {
		return toolErr("regenerating vhost: " + err.Error()), nil
	}

	if site.Secured {
		certsDir := filepath.Join(config.CertsDir(), "sites")
		_ = certs.IssueCert(site.PrimaryDomain(), site.Domains, certsDir)
	}

	_ = podman.WriteContainerHosts()
	_ = nginx.Reload()

	if site.PrimaryDomain() != oldPrimary {
		_ = envfile.SyncPrimaryDomain(site.Path, site.PrimaryDomain(), site.Secured)
	}

	return toolOK(fmt.Sprintf("Added domain %s to site %s", fullDomain, site.Name)), nil
}

func execSiteDomainRemove(args map[string]any) (any, *rpcError) {
	projectPath := resolvedPath(args)
	if projectPath == "" {
		return toolErr("path is required — pass a path argument or open Claude in the project directory"), nil
	}
	domainName := strArg(args, "domain")
	if domainName == "" {
		return toolErr("domain is required"), nil
	}

	site, err := config.FindSiteByPath(projectPath)
	if err != nil {
		return toolErr(fmt.Sprintf("no site registered for %s", projectPath)), nil
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		return toolErr("loading config: " + err.Error()), nil
	}

	fullDomain := strings.ToLower(domainName) + "." + cfg.DNS.TLD

	if !site.HasDomain(fullDomain) {
		return toolErr(fmt.Sprintf("site %q does not have domain %q", site.Name, fullDomain)), nil
	}
	if len(site.Domains) <= 1 {
		return toolErr(fmt.Sprintf("cannot remove the last domain from site %q", site.Name)), nil
	}

	oldPrimary := site.PrimaryDomain()
	var newDomains []string
	for _, d := range site.Domains {
		if d != fullDomain {
			newDomains = append(newDomains, d)
		}
	}
	site.Domains = newDomains

	if err := config.AddSite(*site); err != nil {
		return toolErr("updating site: " + err.Error()), nil
	}

	_ = config.SyncProjectDomains(site.Path, site.Domains, cfg.DNS.TLD)

	if err := siteops.RegenerateSiteVhost(site, oldPrimary); err != nil {
		return toolErr("regenerating vhost: " + err.Error()), nil
	}

	if site.Secured {
		certsDir := filepath.Join(config.CertsDir(), "sites")
		_ = certs.IssueCert(site.PrimaryDomain(), site.Domains, certsDir)
	}

	_ = podman.WriteContainerHosts()
	_ = nginx.Reload()

	if site.PrimaryDomain() != oldPrimary {
		_ = envfile.SyncPrimaryDomain(site.Path, site.PrimaryDomain(), site.Secured)
	}

	return toolOK(fmt.Sprintf("Removed domain %s from site %s", fullDomain, site.Name)), nil
}

func execSecure(args map[string]any) (any, *rpcError) {
	siteName := strArg(args, "site")
	if siteName == "" {
		return toolErr("site is required"), nil
	}

	site, err := config.FindSite(siteName)
	if err != nil {
		return toolErr(fmt.Sprintf("site %q not found — run site_link first", siteName)), nil
	}

	if err := certs.SecureSite(*site); err != nil {
		return toolErr("issuing certificate: " + err.Error()), nil
	}

	site.Secured = true
	if err := config.AddSite(*site); err != nil {
		return toolErr("updating site registry: " + err.Error()), nil
	}

	if err := envfile.ApplyUpdates(site.Path, map[string]string{
		"APP_URL": "https://" + site.PrimaryDomain(),
	}); err != nil {
		// Non-fatal — .env may not exist.
		_ = err
	}

	_ = config.SetProjectSecured(site.Path, true)

	if err := nginx.Reload(); err != nil {
		return toolErr("reloading nginx: " + err.Error()), nil
	}

	return toolOK(fmt.Sprintf("Secured: https://%s", site.PrimaryDomain())), nil
}

func execUnsecure(args map[string]any) (any, *rpcError) {
	siteName := strArg(args, "site")
	if siteName == "" {
		return toolErr("site is required"), nil
	}

	site, err := config.FindSite(siteName)
	if err != nil {
		return toolErr(fmt.Sprintf("site %q not found", siteName)), nil
	}

	if err := certs.UnsecureSite(*site); err != nil {
		return toolErr("removing certificate: " + err.Error()), nil
	}

	site.Secured = false
	if err := config.AddSite(*site); err != nil {
		return toolErr("updating site registry: " + err.Error()), nil
	}

	if err := envfile.ApplyUpdates(site.Path, map[string]string{
		"APP_URL": "http://" + site.PrimaryDomain(),
	}); err != nil {
		_ = err
	}

	_ = config.SetProjectSecured(site.Path, false)

	if err := nginx.Reload(); err != nil {
		return toolErr("reloading nginx: " + err.Error()), nil
	}

	return toolOK(fmt.Sprintf("Unsecured: http://%s", site.PrimaryDomain())), nil
}

func execXdebugToggle(args map[string]any, enable bool) (any, *rpcError) {
	version := strArg(args, "version")
	if version == "" {
		cfg, err := config.LoadGlobal()
		if err != nil {
			return toolErr("loading config: " + err.Error()), nil
		}
		version = cfg.PHP.DefaultVersion
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		return toolErr("loading config: " + err.Error()), nil
	}

	state := "disabled"
	if enable {
		state = "enabled"
	}

	if cfg.IsXdebugEnabled(version) == enable {
		return toolOK(fmt.Sprintf("Xdebug is already %s for PHP %s", state, version)), nil
	}

	cfg.SetXdebug(version, enable)
	if err := config.SaveGlobal(cfg); err != nil {
		return toolErr("saving config: " + err.Error()), nil
	}

	if err := podman.WriteXdebugIni(version, enable); err != nil {
		return toolErr("writing xdebug ini: " + err.Error()), nil
	}

	if err := podman.WriteFPMQuadlet(version); err != nil {
		return toolErr("updating FPM quadlet: " + err.Error()), nil
	}

	short := strings.ReplaceAll(version, ".", "")
	unit := "lerd-php" + short + "-fpm"
	if err := podman.RestartUnit(unit); err != nil {
		return toolOK(fmt.Sprintf("Xdebug %s for PHP %s\n[WARN] FPM restart failed: %v\nRun: systemctl --user restart %s", state, version, err, unit)), nil
	}

	return toolOK(fmt.Sprintf("Xdebug %s for PHP %s (port 9003, host.containers.internal)", state, version)), nil
}

func execXdebugStatus() (any, *rpcError) {
	versions, err := phpDet.ListInstalled()
	if err != nil {
		return toolErr("listing PHP versions: " + err.Error()), nil
	}
	if len(versions) == 0 {
		return toolOK("No PHP versions installed."), nil
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		return toolErr("loading config: " + err.Error()), nil
	}

	type entry struct {
		Version string `json:"version"`
		Enabled bool   `json:"enabled"`
	}
	result := make([]entry, 0, len(versions))
	for _, v := range versions {
		result = append(result, entry{Version: v, Enabled: cfg.IsXdebugEnabled(v)})
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	return toolOK(string(data)), nil
}

func execDBExport(args map[string]any) (any, *rpcError) {
	projectPath := resolvedPath(args)
	if projectPath == "" {
		return toolErr("path is required — pass a path argument or open Claude in the project directory"), nil
	}

	env, err := readDBEnv(projectPath)
	if err != nil {
		return toolErr(err.Error()), nil
	}

	if db := strArg(args, "database"); db != "" {
		env.database = db
	}

	output := strArg(args, "output")
	if output == "" {
		output = filepath.Join(projectPath, env.database+".sql")
	}

	f, err := os.Create(output)
	if err != nil {
		return toolErr(fmt.Sprintf("creating %s: %v", output, err)), nil
	}
	defer f.Close()

	var cmd *exec.Cmd
	switch env.connection {
	case "mysql", "mariadb":
		cmd = podman.Cmd("exec", "-i", "lerd-mysql",
			"mysqldump", "-u"+env.username, "-p"+env.password, env.database)
	case "pgsql", "postgres":
		cmd = podman.Cmd("exec", "-i", "-e", "PGPASSWORD="+env.password,
			"lerd-postgres", "pg_dump", "-U", env.username, env.database)
	default:
		_ = os.Remove(output)
		return toolErr("unsupported DB_CONNECTION: " + env.connection), nil
	}

	var stderr bytes.Buffer
	cmd.Stdout = f
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = os.Remove(output)
		return toolErr(fmt.Sprintf("export failed (%v):\n%s", err, stripANSI(stderr.String()))), nil
	}
	return toolOK(fmt.Sprintf("Exported %s (%s) to %s", env.database, env.connection, output)), nil
}

type mcpDBEnv struct {
	connection string
	database   string
	username   string
	password   string
}

func readDBEnv(projectPath string) (*mcpDBEnv, error) {
	envPath := filepath.Join(projectPath, ".env")
	data, err := os.ReadFile(envPath)
	if err != nil {
		return nil, fmt.Errorf("no .env found in %s", projectPath)
	}
	vals := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		k, v, _ := strings.Cut(line, "=")
		vals[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	conn := vals["DB_CONNECTION"]
	if conn == "" {
		return nil, fmt.Errorf("DB_CONNECTION not set in .env")
	}
	return &mcpDBEnv{
		connection: conn,
		database:   vals["DB_DATABASE"],
		username:   vals["DB_USERNAME"],
		password:   vals["DB_PASSWORD"],
	}, nil
}

// ---- Framework management tools ----

func execFrameworkList() (any, *rpcError) {
	frameworks := config.ListFrameworks()
	type checkInfo struct {
		File     string `json:"file,omitempty"`
		Composer string `json:"composer,omitempty"`
	}
	type workerInfo struct {
		Label   string     `json:"label,omitempty"`
		Command string     `json:"command"`
		Restart string     `json:"restart,omitempty"`
		Check   *checkInfo `json:"check,omitempty"`
	}
	type setupInfo struct {
		Label   string     `json:"label"`
		Command string     `json:"command"`
		Default bool       `json:"default,omitempty"`
		Check   *checkInfo `json:"check,omitempty"`
	}
	type logSourceInfo struct {
		Path   string `json:"path"`
		Format string `json:"format,omitempty"`
	}
	type frameworkInfo struct {
		Name      string                `json:"name"`
		Label     string                `json:"label"`
		PublicDir string                `json:"public_dir"`
		EnvFile   string                `json:"env_file"`
		EnvFormat string                `json:"env_format"`
		BuiltIn   bool                  `json:"built_in"`
		Workers   map[string]workerInfo `json:"workers,omitempty"`
		Setup     []setupInfo           `json:"setup,omitempty"`
		Logs      []logSourceInfo       `json:"logs,omitempty"`
	}
	var result []frameworkInfo
	for _, fw := range frameworks {
		// For laravel, use the merged definition (includes user-defined workers)
		merged := fw
		if fw.Name == "laravel" {
			if m, ok := config.GetFramework("laravel"); ok {
				merged = m
			}
		}
		ef := merged.Env.File
		if ef == "" {
			ef = ".env"
		}
		efmt := merged.Env.Format
		if efmt == "" {
			efmt = "dotenv"
		}
		var workers map[string]workerInfo
		if len(merged.Workers) > 0 {
			workers = make(map[string]workerInfo, len(merged.Workers))
			for n, w := range merged.Workers {
				wi := workerInfo{Label: w.Label, Command: w.Command, Restart: w.Restart}
				if w.Check != nil {
					wi.Check = &checkInfo{File: w.Check.File, Composer: w.Check.Composer}
				}
				workers[n] = wi
			}
		}
		var setup []setupInfo
		for _, sc := range merged.Setup {
			si := setupInfo{Label: sc.Label, Command: sc.Command, Default: sc.Default}
			if sc.Check != nil {
				si.Check = &checkInfo{File: sc.Check.File, Composer: sc.Check.Composer}
			}
			setup = append(setup, si)
		}
		var logSources []logSourceInfo
		for _, ls := range merged.Logs {
			logSources = append(logSources, logSourceInfo{Path: ls.Path, Format: ls.Format})
		}
		result = append(result, frameworkInfo{
			Name:      merged.Name,
			Label:     merged.Label,
			PublicDir: merged.PublicDir,
			EnvFile:   ef,
			EnvFormat: efmt,
			BuiltIn:   merged.Name == "laravel",
			Workers:   workers,
			Setup:     setup,
			Logs:      logSources,
		})
	}
	if result == nil {
		result = []frameworkInfo{}
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	return toolOK(string(data)), nil
}

func execFrameworkAdd(args map[string]any) (any, *rpcError) {
	name := strArg(args, "name")
	if name == "" {
		return toolErr("name is required"), nil
	}

	// Parse workers map if provided
	var workers map[string]config.FrameworkWorker
	if raw, ok := args["workers"]; ok {
		if wmap, ok := raw.(map[string]any); ok {
			workers = make(map[string]config.FrameworkWorker, len(wmap))
			for wname, wval := range wmap {
				if wobj, ok := wval.(map[string]any); ok {
					label, _ := wobj["label"].(string)
					command, _ := wobj["command"].(string)
					restart, _ := wobj["restart"].(string)
					w := config.FrameworkWorker{Label: label, Command: command, Restart: restart}
					if chk, ok := wobj["check"].(map[string]any); ok {
						rule := &config.FrameworkRule{}
						rule.File, _ = chk["file"].(string)
						rule.Composer, _ = chk["composer"].(string)
						if rule.File != "" || rule.Composer != "" {
							w.Check = rule
						}
					}
					workers[wname] = w
				}
			}
		}
	}

	// Parse setup commands if provided
	var setup []config.FrameworkSetupCmd
	if raw, ok := args["setup"]; ok {
		if arr, ok := raw.([]any); ok {
			for _, item := range arr {
				if obj, ok := item.(map[string]any); ok {
					label, _ := obj["label"].(string)
					command, _ := obj["command"].(string)
					dflt, _ := obj["default"].(bool)
					if label != "" && command != "" {
						sc := config.FrameworkSetupCmd{Label: label, Command: command, Default: dflt}
						if chk, ok := obj["check"].(map[string]any); ok {
							rule := &config.FrameworkRule{}
							rule.File, _ = chk["file"].(string)
							rule.Composer, _ = chk["composer"].(string)
							if rule.File != "" || rule.Composer != "" {
								sc.Check = rule
							}
						}
						setup = append(setup, sc)
					}
				}
			}
		}
	}

	// Parse logs sources if provided
	var logs []config.FrameworkLogSource
	if raw, ok := args["logs"]; ok {
		if arr, ok := raw.([]any); ok {
			for _, item := range arr {
				if obj, ok := item.(map[string]any); ok {
					path, _ := obj["path"].(string)
					format, _ := obj["format"].(string)
					if path != "" {
						logs = append(logs, config.FrameworkLogSource{Path: path, Format: format})
					}
				}
			}
		}
	}

	if name == "laravel" {
		// For Laravel, only persist custom workers, setup, and logs (built-in handles everything else)
		if len(workers) == 0 && len(setup) == 0 && len(logs) == 0 {
			return toolErr("workers, setup, or logs is required when customising laravel"), nil
		}
		fw := &config.Framework{Name: "laravel", Workers: workers, Setup: setup, Logs: logs}
		if err := config.SaveFramework(fw); err != nil {
			return toolErr(fmt.Sprintf("saving framework: %v", err)), nil
		}
		var parts []string
		if len(workers) > 0 {
			names := make([]string, 0, len(workers))
			for n := range workers {
				names = append(names, n)
			}
			parts = append(parts, "Workers: "+strings.Join(names, ", "))
		}
		if len(setup) > 0 {
			names := make([]string, 0, len(setup))
			for _, s := range setup {
				names = append(names, s.Label)
			}
			parts = append(parts, "Setup commands: "+strings.Join(names, ", "))
		}
		return toolOK(fmt.Sprintf("Laravel customisations saved: %s\nWorkers are merged with built-in queue/schedule/reverb. Setup commands replace built-in storage:link/migrate/db:seed.", strings.Join(parts, ". "))), nil
	}

	label := strArg(args, "label")
	if label == "" {
		label = name
	}

	fw := &config.Framework{
		Name:      name,
		Label:     label,
		PublicDir: strArg(args, "public_dir"),
		Composer:  "auto",
		NPM:       "auto",
		Workers:   workers,
		Setup:     setup,
		Logs:      logs,
	}
	if fw.PublicDir == "" {
		fw.PublicDir = "public"
	}

	// Detection rules
	if files, ok := args["detect_files"]; ok {
		if fileSlice, ok := files.([]any); ok {
			for _, f := range fileSlice {
				if s, ok := f.(string); ok {
					fw.Detect = append(fw.Detect, config.FrameworkRule{File: s})
				}
			}
		}
	}
	if pkgs, ok := args["detect_packages"]; ok {
		if pkgSlice, ok := pkgs.([]any); ok {
			for _, p := range pkgSlice {
				if s, ok := p.(string); ok {
					fw.Detect = append(fw.Detect, config.FrameworkRule{Composer: s})
				}
			}
		}
	}

	// Env config
	fw.Env = config.FrameworkEnvConf{
		File:           strArg(args, "env_file"),
		Format:         strArg(args, "env_format"),
		FallbackFile:   strArg(args, "env_fallback_file"),
		FallbackFormat: strArg(args, "env_fallback_format"),
	}
	if fw.Env.File == "" {
		fw.Env.File = ".env"
	}

	if err := config.SaveFramework(fw); err != nil {
		return toolErr(fmt.Sprintf("saving framework: %v", err)), nil
	}

	return toolOK(fmt.Sprintf("Framework %q saved. Use site_link to register a project using this framework.", name)), nil
}

func execWorkerStart(args map[string]any) (any, *rpcError) {
	siteName := strArg(args, "site")
	if siteName == "" {
		return toolErr("site is required"), nil
	}
	workerName := strArg(args, "worker")
	if workerName == "" {
		return toolErr("worker is required"), nil
	}

	site, err := config.FindSite(siteName)
	if err != nil {
		return toolErr("site not found: " + siteName), nil
	}

	fwName := site.Framework
	if fwName == "" {
		return toolErr("site has no framework assigned — run lerd link first"), nil
	}
	fw, ok := config.GetFrameworkForDir(fwName, site.Path)
	if !ok {
		return toolErr("framework not found: " + fwName), nil
	}
	worker, ok := fw.Workers[workerName]
	if !ok {
		return toolErr(fmt.Sprintf("worker %q not found in framework %q — use worker_list to see available workers", workerName, fwName)), nil
	}

	if worker.Check != nil && !config.MatchesRule(site.Path, *worker.Check) {
		return toolErr(fmt.Sprintf("worker %q requires a dependency that is not installed (check the framework definition for required packages)", workerName)), nil
	}

	phpVersion := site.PHPVersion
	if detected, err := phpDet.DetectVersion(site.Path); err == nil && detected != "" {
		phpVersion = detected
	}
	versionShort := strings.ReplaceAll(phpVersion, ".", "")
	fpmUnit := "lerd-php" + versionShort + "-fpm"
	container := "lerd-php" + versionShort + "-fpm"
	unitName := "lerd-" + workerName + "-" + siteName

	label := worker.Label
	if label == "" {
		label = workerName
	}
	restart := worker.Restart
	if restart == "" {
		restart = "always"
	}

	unit := fmt.Sprintf(`[Unit]
Description=Lerd %s (%s)
After=network.target %s.service
BindsTo=%s.service

[Service]
Type=simple
Restart=%s
RestartSec=5
ExecStart=%s exec -w %s %s %s

[Install]
WantedBy=default.target
`, label, siteName, fpmUnit, fpmUnit, restart, podman.PodmanBin(), site.Path, container, worker.Command)

	if err := lerdSystemd.WriteService(unitName, unit); err != nil {
		return toolErr("writing service unit: " + err.Error()), nil
	}
	if err := podman.DaemonReloadFn(); err != nil {
		return toolErr("daemon-reload: " + err.Error()), nil
	}
	_ = lerdSystemd.EnableService(unitName)
	if err := lerdSystemd.StartService(unitName); err != nil {
		return toolErr(fmt.Sprintf("starting %s: %v", workerName, err)), nil
	}
	return toolOK(fmt.Sprintf("%s started for %s\nLogs: journalctl --user -u %s -f", label, siteName, unitName)), nil
}

func execWorkerStop(args map[string]any) (any, *rpcError) {
	siteName := strArg(args, "site")
	if siteName == "" {
		return toolErr("site is required"), nil
	}
	workerName := strArg(args, "worker")
	if workerName == "" {
		return toolErr("worker is required"), nil
	}
	unitName := "lerd-" + workerName + "-" + siteName
	unitFile := filepath.Join(config.SystemdUserDir(), unitName+".service")
	_ = lerdSystemd.DisableService(unitName)
	_ = podman.StopUnit(unitName)
	if err := os.Remove(unitFile); err != nil && !os.IsNotExist(err) {
		return toolErr("removing unit file: " + err.Error()), nil
	}
	_ = podman.DaemonReloadFn()
	return toolOK(fmt.Sprintf("%s worker stopped for %s", workerName, siteName)), nil
}

func execWorkerList(args map[string]any) (any, *rpcError) {
	siteName := strArg(args, "site")
	if siteName == "" {
		return toolErr("site is required"), nil
	}

	site, err := config.FindSite(siteName)
	if err != nil {
		return toolErr("site not found: " + siteName), nil
	}

	fwName := site.Framework
	if fwName == "" {
		data, _ := json.MarshalIndent([]struct{}{}, "", "  ")
		return toolOK(string(data)), nil
	}
	fw, ok := config.GetFrameworkForDir(fwName, site.Path)
	if !ok || len(fw.Workers) == 0 {
		data, _ := json.MarshalIndent([]struct{}{}, "", "  ")
		return toolOK(string(data)), nil
	}

	type workerInfo struct {
		Name     string `json:"name"`
		Label    string `json:"label"`
		Command  string `json:"command"`
		Restart  string `json:"restart"`
		Running  bool   `json:"running"`
		Unit     string `json:"unit"`
		Orphaned bool   `json:"orphaned,omitempty"`
	}

	known := make(map[string]bool, len(fw.Workers))
	var result []workerInfo
	for wname, w := range fw.Workers {
		known[wname] = true
		unitName := "lerd-" + wname + "-" + siteName
		status, _ := podman.UnitStatus(unitName)
		label := w.Label
		if label == "" {
			label = wname
		}
		restart := w.Restart
		if restart == "" {
			restart = "always"
		}
		result = append(result, workerInfo{
			Name:    wname,
			Label:   label,
			Command: w.Command,
			Restart: restart,
			Running: status == "active",
			Unit:    unitName,
		})
	}

	// Detect orphaned workers — running units with no framework definition.
	orphans := lerdSystemd.FindOrphanedWorkers(siteName, known)
	for _, wname := range orphans {
		unitName := "lerd-" + wname + "-" + siteName
		result = append(result, workerInfo{
			Name:     wname,
			Label:    wname + " (orphaned)",
			Running:  true,
			Unit:     unitName,
			Orphaned: true,
		})
	}

	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })

	data, _ := json.MarshalIndent(result, "", "  ")
	return toolOK(string(data)), nil
}

func execWorkerAdd(args map[string]any) (any, *rpcError) {
	siteName := strArg(args, "site")
	if siteName == "" {
		return toolErr("site is required"), nil
	}
	name := strArg(args, "name")
	if name == "" {
		return toolErr("name is required"), nil
	}
	command := strArg(args, "command")
	if command == "" {
		return toolErr("command is required"), nil
	}

	site, err := config.FindSite(siteName)
	if err != nil {
		return toolErr("site not found: " + siteName), nil
	}

	w := config.FrameworkWorker{
		Label:   strArg(args, "label"),
		Command: command,
		Restart: strArg(args, "restart"),
	}
	checkFile := strArg(args, "check_file")
	checkComposer := strArg(args, "check_composer")
	if checkFile != "" || checkComposer != "" {
		w.Check = &config.FrameworkRule{File: checkFile, Composer: checkComposer}
	}
	if cw := strSliceArg(args, "conflicts_with"); len(cw) > 0 {
		w.ConflictsWith = cw
	}
	proxyPath := strArg(args, "proxy_path")
	if proxyPath != "" {
		w.Proxy = &config.WorkerProxy{
			Path:        proxyPath,
			PortEnvKey:  strArg(args, "proxy_port_env_key"),
			DefaultPort: intArg(args, "proxy_default_port", 0),
		}
	}

	action := "added"
	if boolArg(args, "global") {
		fwName := site.Framework
		if fwName == "" {
			return toolErr("site has no framework assigned"), nil
		}
		fw := config.LoadUserFramework(fwName)
		if fw == nil {
			fw = &config.Framework{Name: fwName}
		}
		if fw.Workers == nil {
			fw.Workers = make(map[string]config.FrameworkWorker)
		}
		if _, exists := fw.Workers[name]; exists {
			action = "updated"
		}
		fw.Workers[name] = w
		if err := config.SaveFramework(fw); err != nil {
			return toolErr("saving framework overlay: " + err.Error()), nil
		}
		return toolOK(fmt.Sprintf("Custom worker %q %s in global %s overlay. Start it with worker_start(site: %q, worker: %q).", name, action, fwName, siteName, name)), nil
	}

	if proj, _ := config.LoadProjectConfig(site.Path); proj.CustomWorkers != nil {
		if _, exists := proj.CustomWorkers[name]; exists {
			action = "updated"
		}
	}
	if err := config.SetProjectCustomWorker(site.Path, name, w); err != nil {
		return toolErr("saving .lerd.yaml: " + err.Error()), nil
	}
	return toolOK(fmt.Sprintf("Custom worker %q %s in .lerd.yaml. Start it with worker_start(site: %q, worker: %q).", name, action, siteName, name)), nil
}

func execWorkerRemove(args map[string]any) (any, *rpcError) {
	siteName := strArg(args, "site")
	if siteName == "" {
		return toolErr("site is required"), nil
	}
	name := strArg(args, "name")
	if name == "" {
		return toolErr("name is required"), nil
	}

	site, err := config.FindSite(siteName)
	if err != nil {
		return toolErr("site not found: " + siteName), nil
	}

	// Stop the worker if running.
	unitName := "lerd-" + name + "-" + siteName
	if status, _ := podman.UnitStatus(unitName); status == "active" {
		_ = lerdSystemd.DisableService(unitName)
		podman.StopUnit(unitName) //nolint:errcheck
		unitFile := filepath.Join(config.SystemdUserDir(), unitName+".service")
		_ = os.Remove(unitFile)
		_ = podman.DaemonReloadFn()
	}

	if boolArg(args, "global") {
		fwName := site.Framework
		if fwName == "" {
			return toolErr("site has no framework assigned"), nil
		}
		fw := config.LoadUserFramework(fwName)
		if fw == nil || fw.Workers == nil {
			return toolErr(fmt.Sprintf("no global overlay for framework %q", fwName)), nil
		}
		if _, exists := fw.Workers[name]; !exists {
			return toolErr(fmt.Sprintf("worker %q not found in global %s overlay", name, fwName)), nil
		}
		delete(fw.Workers, name)
		if len(fw.Workers) == 0 {
			fw.Workers = nil
		}
		if err := config.SaveFramework(fw); err != nil {
			return toolErr("saving framework overlay: " + err.Error()), nil
		}
		return toolOK(fmt.Sprintf("Custom worker %q removed from global %s overlay", name, fwName)), nil
	}

	if err := config.RemoveProjectCustomWorker(site.Path, name); err != nil {
		if _, ok := err.(*config.WorkerNotFoundError); ok {
			return toolErr(fmt.Sprintf("custom worker %q not found in .lerd.yaml for site %q", name, siteName)), nil
		}
		return toolErr("saving .lerd.yaml: " + err.Error()), nil
	}
	return toolOK(fmt.Sprintf("Custom worker %q removed from %s", name, siteName)), nil
}

func execFrameworkRemove(args map[string]any) (any, *rpcError) {
	name := strArg(args, "name")
	if name == "" {
		return toolErr("name is required"), nil
	}
	version := strArg(args, "version")

	if name == "laravel" {
		if err := config.RemoveFramework(name); err != nil {
			if os.IsNotExist(err) {
				return toolErr("no custom workers defined for laravel"), nil
			}
			return toolErr(fmt.Sprintf("removing framework: %v", err)), nil
		}
		return toolOK("Custom Laravel worker additions removed. Built-in queue/schedule/reverb workers remain."), nil
	}

	if version != "" {
		files := config.ListFrameworkFiles(name)
		for _, f := range files {
			if f.Version == version {
				if err := config.RemoveFrameworkFile(f.Path); err != nil {
					return toolErr(fmt.Sprintf("removing framework: %v", err)), nil
				}
				return toolOK(fmt.Sprintf("Removed %s@%s.", name, version)), nil
			}
		}
		return toolErr(fmt.Sprintf("framework %q version %q not found", name, version)), nil
	}

	if err := config.RemoveFramework(name); err != nil {
		if os.IsNotExist(err) {
			return toolErr(fmt.Sprintf("framework %q not found", name)), nil
		}
		return toolErr(fmt.Sprintf("removing framework: %v", err)), nil
	}
	return toolOK(fmt.Sprintf("Framework %q removed.", name)), nil
}

func execFrameworkSearch(args map[string]any) (any, *rpcError) {
	query := strArg(args, "query")
	if query == "" {
		return toolErr("query is required"), nil
	}

	client := store.NewClient()
	results, err := client.Search(query)
	if err != nil {
		return toolErr(fmt.Sprintf("searching store: %v", err)), nil
	}

	type searchResult struct {
		Name     string   `json:"name"`
		Label    string   `json:"label"`
		Versions []string `json:"versions"`
		Latest   string   `json:"latest"`
	}
	out := make([]searchResult, len(results))
	for i, r := range results {
		out[i] = searchResult{
			Name:     r.Name,
			Label:    r.Label,
			Versions: r.Versions,
			Latest:   r.Latest,
		}
	}
	data, _ := json.Marshal(out)
	return toolOK(string(data)), nil
}

func execFrameworkInstall(args map[string]any) (any, *rpcError) {
	name := strArg(args, "name")
	if name == "" {
		return toolErr("name is required"), nil
	}
	version := strArg(args, "version")

	client := store.NewClient()

	// Auto-detect version from site path if not specified
	if version == "" {
		sitePath := defaultSitePath
		if sitePath != "" {
			if idx, err := client.FetchIndex(); err == nil {
				for _, entry := range idx.Frameworks {
					if entry.Name == name {
						version = store.ResolveVersion(sitePath, entry.Detect, entry.Versions, "")
						break
					}
				}
			}
		}
	}

	fw, err := client.FetchFramework(name, version)
	if err != nil {
		return toolErr(fmt.Sprintf("fetching framework: %v", err)), nil
	}

	if err := config.SaveStoreFramework(fw); err != nil {
		return toolErr(fmt.Sprintf("saving framework: %v", err)), nil
	}

	versionStr := fw.Version
	if versionStr == "" {
		versionStr = "latest"
	}
	filename := fw.Name + ".yaml"
	if fw.Version != "" {
		filename = fw.Name + "@" + fw.Version + ".yaml"
	}
	return toolOK(fmt.Sprintf("Installed %s@%s (%s). Saved to %s/%s", fw.Name, versionStr, fw.Label, config.StoreFrameworksDir(), filename)), nil
}

func execProjectNew(args map[string]any) (any, *rpcError) {
	projectPath := strArg(args, "path")
	if projectPath == "" {
		return toolErr("path is required — provide an absolute path for the new project directory"), nil
	}
	frameworkName := strArg(args, "framework")
	if frameworkName == "" {
		frameworkName = "laravel"
	}
	extraArgs := strSliceArg(args, "args")

	fw, ok := config.GetFramework(frameworkName)
	if !ok {
		return toolErr(fmt.Sprintf("unknown framework %q — use framework_list to see available frameworks", frameworkName)), nil
	}
	if fw.Create == "" {
		return toolErr(fmt.Sprintf("framework %q has no create command — add a 'create' field to its YAML definition", frameworkName)), nil
	}

	parts := strings.Fields(fw.Create)
	parts = append(parts, projectPath)
	parts = append(parts, extraArgs...)

	var out bytes.Buffer
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return toolErr(fmt.Sprintf("scaffold command failed (%v):\n%s", err, stripANSI(out.String()))), nil
	}
	return toolOK(fmt.Sprintf("Project created at %s\n\nNext steps:\n  site_link(path: %q)\n  env_setup(path: %q)\n\n%s",
		projectPath, projectPath, projectPath, stripANSI(strings.TrimSpace(out.String())))), nil
}

func execSitePHP(args map[string]any) (any, *rpcError) {
	siteName := strArg(args, "site")
	version := strArg(args, "version")
	if siteName == "" {
		return toolErr("site is required"), nil
	}
	if version == "" {
		return toolErr("version is required"), nil
	}

	site, err := config.FindSite(siteName)
	if err != nil {
		return toolErr(fmt.Sprintf("site %q not found — run sites to list registered sites", siteName)), nil
	}

	// Write .php-version pin file (keeps CLI php and other tools in sync).
	phpVersionFile := filepath.Join(site.Path, ".php-version")
	if err := os.WriteFile(phpVersionFile, []byte(version+"\n"), 0644); err != nil {
		return toolErr("writing .php-version: " + err.Error()), nil
	}
	_ = config.SetProjectPHPVersion(site.Path, version)

	// Ensure the FPM quadlet and xdebug ini exist for this version.
	if err := podman.WriteFPMQuadlet(version); err != nil {
		return toolErr("writing FPM quadlet: " + err.Error()), nil
	}
	_ = podman.WriteXdebugIni(version, false) // non-fatal if version not yet built

	// Update the site registry.
	site.PHPVersion = version
	if err := config.AddSite(*site); err != nil {
		return toolErr("updating site registry: " + err.Error()), nil
	}

	// Regenerate the nginx vhost (SSL or plain).
	if site.Secured {
		if err := certs.SecureSite(*site); err != nil {
			return toolErr("regenerating SSL vhost: " + err.Error()), nil
		}
	} else {
		if err := nginx.GenerateVhost(*site, version); err != nil {
			return toolErr("regenerating vhost: " + err.Error()), nil
		}
	}

	if err := nginx.Reload(); err != nil {
		return toolErr("reloading nginx: " + err.Error()), nil
	}

	return toolOK(fmt.Sprintf("PHP version for %s set to %s. The FPM container for PHP %s must be running — use service_start(name: \"php%s\") if it isn't.", siteName, version, version, version)), nil
}

func execSiteNode(args map[string]any) (any, *rpcError) {
	siteName := strArg(args, "site")
	version := strArg(args, "version")
	if siteName == "" {
		return toolErr("site is required"), nil
	}
	if version == "" {
		return toolErr("version is required"), nil
	}

	site, err := config.FindSite(siteName)
	if err != nil {
		return toolErr(fmt.Sprintf("site %q not found — run sites to list registered sites", siteName)), nil
	}

	// Write .node-version pin file in the project.
	nodeVersionFile := filepath.Join(site.Path, ".node-version")
	if err := os.WriteFile(nodeVersionFile, []byte(version+"\n"), 0644); err != nil {
		return toolErr("writing .node-version: " + err.Error()), nil
	}

	// Install the version via fnm (non-fatal if already installed or fnm unavailable).
	fnmPath := filepath.Join(config.BinDir(), "fnm")
	if _, statErr := os.Stat(fnmPath); statErr == nil {
		var out bytes.Buffer
		cmd := exec.Command(fnmPath, "install", version)
		cmd.Stdout = &out
		cmd.Stderr = &out
		_ = cmd.Run()
	}

	// Update the site registry.
	site.NodeVersion = version
	if err := config.AddSite(*site); err != nil {
		return toolErr("updating site registry: " + err.Error()), nil
	}

	return toolOK(fmt.Sprintf("Node.js version for %s set to %s. Run npm install inside the project if dependencies need rebuilding.", siteName, version)), nil
}

func execSitePause(args map[string]any) (any, *rpcError) {
	siteName := strArg(args, "site")
	if siteName == "" {
		return toolErr("site is required"), nil
	}
	return runLerdCmd("pause", siteName)
}

func execSiteUnpause(args map[string]any) (any, *rpcError) {
	siteName := strArg(args, "site")
	if siteName == "" {
		return toolErr("site is required"), nil
	}
	return runLerdCmd("unpause", siteName)
}

func execServicePin(args map[string]any) (any, *rpcError) {
	name := strArg(args, "name")
	if name == "" {
		return toolErr("name is required"), nil
	}
	return runLerdCmd("service", "pin", name)
}

func execServiceUnpin(args map[string]any) (any, *rpcError) {
	name := strArg(args, "name")
	if name == "" {
		return toolErr("name is required"), nil
	}
	return runLerdCmd("service", "unpin", name)
}

// runLerdCmd runs the lerd binary with the given arguments and returns its
// combined stdout+stderr output as a tool result.
func runLerdCmd(cmdArgs ...string) (any, *rpcError) {
	self, err := os.Executable()
	if err != nil {
		return toolErr("could not resolve lerd executable: " + err.Error()), nil
	}
	var out bytes.Buffer
	cmd := exec.Command(self, cmdArgs...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return toolErr(fmt.Sprintf("command failed (%v):\n%s", err, stripANSI(out.String()))), nil
	}
	return toolOK(stripANSI(strings.TrimSpace(out.String()))), nil
}

// ---- DB import / create ----

func execDBImport(args map[string]any) (any, *rpcError) {
	projectPath := resolvedPath(args)
	if projectPath == "" {
		return toolErr("path is required — pass a path argument or open Claude in the project directory"), nil
	}

	file := strArg(args, "file")
	if file == "" {
		return toolErr("file is required"), nil
	}

	env, err := readDBEnv(projectPath)
	if err != nil {
		return toolErr(err.Error()), nil
	}
	if db := strArg(args, "database"); db != "" {
		env.database = db
	}

	f, err := os.Open(file)
	if err != nil {
		return toolErr(fmt.Sprintf("opening %s: %v", file, err)), nil
	}
	defer f.Close()

	var cmd *exec.Cmd
	switch env.connection {
	case "mysql", "mariadb":
		cmd = podman.Cmd("exec", "-i", "lerd-mysql",
			"mysql", "-u"+env.username, "-p"+env.password, env.database)
	case "pgsql", "postgres":
		cmd = podman.Cmd("exec", "-i", "-e", "PGPASSWORD="+env.password,
			"lerd-postgres", "psql", "-U", env.username, env.database)
	default:
		return toolErr("unsupported DB_CONNECTION: " + env.connection), nil
	}

	var stderr bytes.Buffer
	cmd.Stdin = f
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return toolErr(fmt.Sprintf("import failed (%v):\n%s", err, stripANSI(stderr.String()))), nil
	}
	return toolOK(fmt.Sprintf("Imported %s into %s (%s)", file, env.database, env.connection)), nil
}

func execDBCreate(args map[string]any) (any, *rpcError) {
	projectPath := resolvedPath(args)
	if projectPath == "" {
		return toolErr("path is required — pass a path argument or open Claude in the project directory"), nil
	}

	env, _ := readDBEnvLenient(projectPath)

	dbName := strArg(args, "name")
	if dbName == "" {
		if env != nil && env.database != "" {
			dbName = env.database
		} else {
			base := filepath.Base(projectPath)
			dbName = strings.ToLower(strings.ReplaceAll(base, "-", "_"))
		}
	}

	conn := "mysql"
	if env != nil && env.connection != "" {
		conn = env.connection
	}

	svc := "mysql"
	switch strings.ToLower(conn) {
	case "pgsql", "postgres":
		svc = "postgres"
	}

	var results []string
	for _, name := range []string{dbName, dbName + "_testing"} {
		created, err := mcpCreateDatabase(svc, name)
		if err != nil {
			return toolErr(fmt.Sprintf("creating %q: %v", name, err)), nil
		}
		if created {
			results = append(results, fmt.Sprintf("Created database %q", name))
		} else {
			results = append(results, fmt.Sprintf("Database %q already exists", name))
		}
	}
	return toolOK(strings.Join(results, "\n")), nil
}

func mcpCreateDatabase(svc, name string) (bool, error) {
	switch svc {
	case "mysql":
		check := podman.Cmd("exec", "lerd-mysql", "mysql", "-uroot", "-plerd",
			"-sNe", fmt.Sprintf("SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name='%s';", name))
		out, err := check.Output()
		if err == nil && strings.TrimSpace(string(out)) != "0" {
			return false, nil
		}
		cmd := podman.Cmd("exec", "lerd-mysql", "mysql", "-uroot", "-plerd",
			"-e", fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`;", name))
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return false, fmt.Errorf("%v: %s", err, stderr.String())
		}
		return true, nil
	case "postgres":
		cmd := podman.Cmd("exec", "lerd-postgres", "psql", "-U", "postgres",
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

// readDBEnvLenient reads DB connection info from .env without requiring DB_DATABASE.
func readDBEnvLenient(projectPath string) (*mcpDBEnv, error) {
	envPath := filepath.Join(projectPath, ".env")
	data, err := os.ReadFile(envPath)
	if err != nil {
		return nil, fmt.Errorf("no .env found in %s", projectPath)
	}
	vals := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		k, v, _ := strings.Cut(line, "=")
		vals[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	return &mcpDBEnv{
		connection: vals["DB_CONNECTION"],
		database:   vals["DB_DATABASE"],
		username:   vals["DB_USERNAME"],
		password:   vals["DB_PASSWORD"],
	}, nil
}

// ---- PHP list / extensions ----

func execPHPList() (any, *rpcError) {
	versions, err := phpDet.ListInstalled()
	if err != nil {
		return toolErr("listing PHP versions: " + err.Error()), nil
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		return toolErr("loading config: " + err.Error()), nil
	}

	if len(versions) == 0 {
		return toolOK("No PHP versions installed. Run 'lerd install' to set up PHP."), nil
	}

	type entry struct {
		Version string `json:"version"`
		Default bool   `json:"default"`
	}
	result := make([]entry, 0, len(versions))
	for _, v := range versions {
		result = append(result, entry{Version: v, Default: v == cfg.PHP.DefaultVersion})
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	return toolOK(string(data)), nil
}

func execPHPExtList(args map[string]any) (any, *rpcError) {
	version, err := resolvePHPVersion(args)
	if err != nil {
		return toolErr(err.Error()), nil
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		return toolErr("loading config: " + err.Error()), nil
	}

	exts := cfg.GetExtensions(version)
	if len(exts) == 0 {
		return toolOK(fmt.Sprintf("No custom extensions configured for PHP %s.", version)), nil
	}

	data, _ := json.MarshalIndent(map[string]any{
		"version":    version,
		"extensions": exts,
	}, "", "  ")
	return toolOK(string(data)), nil
}

func execPHPExtAdd(args map[string]any) (any, *rpcError) {
	ext := strArg(args, "extension")
	if ext == "" {
		return toolErr("extension is required"), nil
	}

	version, err := resolvePHPVersion(args)
	if err != nil {
		return toolErr(err.Error()), nil
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		return toolErr("loading config: " + err.Error()), nil
	}

	cfg.AddExtension(version, ext)
	if err := config.SaveGlobal(cfg); err != nil {
		return toolErr("saving config: " + err.Error()), nil
	}

	var out bytes.Buffer
	if err := podman.RebuildFPMImageTo(version, false, &out); err != nil {
		return toolErr(fmt.Sprintf("rebuilding PHP %s image (%v):\n%s", version, err, out.String())), nil
	}

	short := strings.ReplaceAll(version, ".", "")
	unit := "lerd-php" + short + "-fpm"
	if err := podman.RestartUnit(unit); err != nil {
		return toolOK(fmt.Sprintf("Extension %q added to PHP %s.\n[WARN] FPM restart failed: %v\nRun: systemctl --user restart %s", ext, version, err, unit)), nil
	}
	return toolOK(fmt.Sprintf("Extension %q added to PHP %s. FPM container restarted.", ext, version)), nil
}

func execPHPExtRemove(args map[string]any) (any, *rpcError) {
	ext := strArg(args, "extension")
	if ext == "" {
		return toolErr("extension is required"), nil
	}

	version, err := resolvePHPVersion(args)
	if err != nil {
		return toolErr(err.Error()), nil
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		return toolErr("loading config: " + err.Error()), nil
	}

	cfg.RemoveExtension(version, ext)
	if err := config.SaveGlobal(cfg); err != nil {
		return toolErr("saving config: " + err.Error()), nil
	}

	var out bytes.Buffer
	if err := podman.RebuildFPMImageTo(version, false, &out); err != nil {
		return toolErr(fmt.Sprintf("rebuilding PHP %s image (%v):\n%s", version, err, out.String())), nil
	}

	short := strings.ReplaceAll(version, ".", "")
	unit := "lerd-php" + short + "-fpm"
	if err := podman.RestartUnit(unit); err != nil {
		return toolOK(fmt.Sprintf("Extension %q removed from PHP %s.\n[WARN] FPM restart failed: %v\nRun: systemctl --user restart %s", ext, version, err, unit)), nil
	}
	return toolOK(fmt.Sprintf("Extension %q removed from PHP %s. FPM container restarted.", ext, version)), nil
}

// resolvePHPVersion picks the PHP version from args["version"], the site .php-version file, or the global default.
func resolvePHPVersion(args map[string]any) (string, error) {
	if v := strArg(args, "version"); v != "" {
		if !phpVersionRe.MatchString(v) {
			return "", fmt.Errorf("invalid PHP version %q — expected format like \"8.4\"", v)
		}
		return v, nil
	}
	if defaultSitePath != "" {
		if v, err := phpDet.DetectVersion(defaultSitePath); err == nil {
			return v, nil
		}
	}
	cfg, err := config.LoadGlobal()
	if err != nil {
		return "", fmt.Errorf("loading config: %w", err)
	}
	return cfg.PHP.DefaultVersion, nil
}

// ---- Park / Unpark ----

func execPark(args map[string]any) (any, *rpcError) {
	path := resolvedPath(args)
	if path == "" {
		return toolErr("path is required — pass a path argument or open Claude in the project directory"), nil
	}
	return runLerdCmd("park", path)
}

func execUnpark(args map[string]any) (any, *rpcError) {
	path := strArg(args, "path")
	if path == "" {
		return toolErr("path is required"), nil
	}
	return runLerdCmd("unpark", path)
}
