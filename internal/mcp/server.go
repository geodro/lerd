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
	"github.com/geodro/lerd/internal/xdebugops"
)

const protocolVersion = "2024-11-05"

// knownServices returns the default-preset names. Wrapper so the existing
// MCP call sites (`for _, s := range knownServices`) keep compiling.
func knownServices() []string { return config.DefaultPresetNames() }

// builtinServiceEnv returns the recommended Laravel .env KEY=VALUE pairs for
// a default-preset service, or nil for non-defaults. Reads from the preset
// YAML so it stays in sync with the env writer (lerd env).
func builtinServiceEnv(name string) []string { return config.DefaultPresetEnvVars(name) }

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
	Description string   `json:"description,omitempty"`
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
			Description: "List registered sites (domain, path, PHP/Node version, TLS, workers). Call first to discover site names.",
			InputSchema: mcpSchema{
				Type:       "object",
				Properties: map[string]mcpProp{},
			},
		},
		{
			Name:        "service_control",
			Description: "Lifecycle. update=pull. migrate=dump+restore across data-breaking versions. rollback=revert. remove=delete custom.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"action": {Type: "string", Enum: []string{"start", "stop", "restart", "pin", "unpin", "update", "rollback", "migrate", "remove"}},
					"name":   {Type: "string"},
					"tag":    {Type: "string", Description: "For update/migrate."},
				},
				Required: []string{"action", "name"},
			},
		},
		{
			Name:        "logs",
			Description: "Fetch recent container logs. target: nginx, service, PHP version (8.4), or site name. Defaults to current site's FPM.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"target": {Type: "string", Description: "nginx, service, PHP version, or site name. Defaults to current FPM."},
					"lines":  {Type: "integer", Description: "Tail count (default 50)."},
				},
			},
		},
		{
			Name:        "composer",
			Description: "Run composer in the PHP-FPM container.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {Type: "string", Description: "Project root. Defaults to cwd."},
					"args": {Type: "array", Description: `e.g. ["install"] or ["require", "laravel/sanctum"].`},
				},
				Required: []string{"args"},
			},
		},
		{
			Name:        "vendor_bins",
			Description: "List composer-installed binaries in vendor/bin (pest, phpunit, pint, etc.). Call before vendor_run.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {Type: "string", Description: "Project root. Defaults to cwd."},
				},
			},
		},
		{
			Name:        "vendor_run",
			Description: "Run a vendor/bin binary in the PHP-FPM container. Use vendor_bins first.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {Type: "string", Description: "Project root. Defaults to cwd."},
					"bin":  {Type: "string", Description: "Binary name (pest, phpunit, pint, …)."},
					"args": {Type: "array", Description: "Arguments to pass."},
				},
				Required: []string{"bin"},
			},
		},
		{
			Name:        "node",
			Description: "Install or uninstall a Node.js version via fnm.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"action":  {Type: "string", Enum: []string{"install", "uninstall"}},
					"version": {Type: "string", Description: "Version or alias (e.g. 20, 20.11.0, lts)."},
				},
				Required: []string{"action", "version"},
			},
		},
		{
			Name:        "runtime_versions",
			Description: "List installed PHP and Node.js versions (plus defaults).",
			InputSchema: mcpSchema{
				Type:       "object",
				Properties: map[string]mcpProp{},
			},
		},
		{
			Name:        "status",
			Description: "Health status of DNS, nginx, PHP-FPM containers, and the file watcher. Call when a site is unreachable.",
			InputSchema: mcpSchema{
				Type:       "object",
				Properties: map[string]mcpProp{},
			},
		},
		{
			Name:        "doctor",
			Description: "Full environment diagnostic (podman, systemd, DNS, ports, images, config). Call on setup issues.",
			InputSchema: mcpSchema{
				Type:       "object",
				Properties: map[string]mcpProp{},
			},
		},
		{
			Name:        "service_add",
			Description: "Register a custom OCI service (writes a systemd quadlet). Use service_preset_install for bundled presets.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"name":        {Type: "string", Description: "Service slug (lowercase, hyphens)."},
					"image":       {Type: "string", Description: "OCI image reference."},
					"ports":       {Type: "array", Description: `host:container mappings, e.g. ["27017:27017"].`},
					"environment": {Type: "array", Description: `Container env ["KEY=VALUE", ...].`},
					"env_vars":    {Type: "array", Description: `Project .env keys to inject ["KEY=VALUE", ...].`},
					"data_dir":    {Type: "string", Description: "Container path for persistent data."},
					"description": {Type: "string", Description: "Human-readable description."},
					"dashboard":   {Type: "string", Description: "Web dashboard URL."},
					"depends_on":  {Type: "array", Description: `Services that must start first, e.g. ["mysql"].`},
				},
				Required: []string{"name", "image"},
			},
		},
		{
			Name:        "service_expose",
			Description: "Add or remove a published port on a built-in service.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"name":   {Type: "string", Description: "mysql, redis, postgres, meilisearch, rustfs, mailpit."},
					"port":   {Type: "string", Description: `"host:container" (e.g. "13306:3306").`},
					"remove": {Type: "boolean", Description: "Set true to remove the mapping."},
				},
				Required: []string{"name", "port"},
			},
		},
		{
			Name:        "service_env",
			Description: "Recommended Laravel .env connection keys for a service.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"name": {
						Type:        "string",
						Description: "Service name (e.g. mysql, redis, mongodb).",
					},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:        "service_preset_list",
			Description: "List bundled service presets (name, description, versions, installed state). Call before service_preset_install.",
			InputSchema: mcpSchema{
				Type:       "object",
				Properties: map[string]mcpProp{},
			},
		},
		{
			Name:        "service_preset_install",
			Description: "Install a bundled preset (call service_preset_list first). Multi-version presets need version. Install any preset-dependency first.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"name":    {Type: "string", Description: "Preset name (from service_preset_list)."},
					"version": {Type: "string", Description: "Required for multi-version presets (mysql, mariadb)."},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:        "service_check_updates",
			Description: "Check registry for newer images. latest_tag=safe update; upgrade_tag=cross-strategy (may need migration). Omit name to scan all active.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"name": {Type: "string", Description: "Service name. Omit to scan all."},
				},
			},
		},
		{
			Name:        "env_setup",
			Description: "Configure .env (services, DBs, APP_KEY, APP_URL). Call after site_link, then ALWAYS follow with setup to run migrations. For sqlite DB_CONNECTION, pick db_set first if you want mysql/postgres instead.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {Type: "string", Description: "Project root. Defaults to cwd."},
				},
			},
		},
		{
			Name:        "setup",
			Description: "Run the framework's post-install steps (migrations, storage:link, etc.). MANDATORY after env_setup on new or cloned projects — otherwise migrations never run. Idempotent.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {Type: "string", Description: "Project root. Defaults to cwd."},
				},
			},
		},
		{
			Name:        "db_set",
			Description: "Pick the project database. Persists to .lerd.yaml, rewrites DB_ keys in .env, starts service, creates DB + _testing. Call before env_setup on fresh clones.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path":     {Type: "string", Description: "Project root."},
					"database": {Type: "string", Enum: []string{"sqlite", "mysql", "postgres"}},
				},
				Required: []string{"database"},
			},
		},
		{
			Name:        "env_check",
			Description: "Compare .env against .env.example; flag missing/extra keys.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {Type: "string", Description: "Project root. Defaults to cwd."},
				},
			},
		},
		{
			Name:        "site_link",
			Description: "Register a directory as a lerd site. Non-PHP sites need .lerd.yaml container.port + Containerfile first.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {Type: "string", Description: "Project directory. Defaults to cwd."},
					"name": {Type: "string", Description: "Without .test TLD. Defaults to dir name."},
				},
			},
		},
		{
			Name:        "site_unlink",
			Description: "Unregister a site and remove its nginx vhost. Project files are kept.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {Type: "string", Description: "Project directory. Defaults to cwd."},
				},
			},
		},
		{
			Name:        "site_domain",
			Description: "Add or remove a site domain (no .test TLD). Can't remove the last one.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"action": {Type: "string", Enum: []string{"add", "remove"}},
					"path":   {Type: "string", Description: "Project directory."},
					"domain": {Type: "string", Description: "Without .test TLD."},
				},
				Required: []string{"action", "domain"},
			},
		},
		{
			Name:        "site_tls",
			Description: "Toggle HTTPS for a site (mkcert). Updates APP_URL in .env.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"action": {Type: "string", Enum: []string{"enable", "disable"}},
					"site":   {Type: "string"},
				},
				Required: []string{"action", "site"},
			},
		},
		{
			Name:        "xdebug",
			Description: "Xdebug control on port 9003. on/off restarts FPM; status reports all versions.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"action":  {Type: "string", Enum: []string{"on", "off", "status"}},
					"version": {Type: "string", Description: "PHP version. Ignored for status."},
					"mode":    {Type: "string", Description: "debug (default) | coverage | develop | profile | trace | gcstats. Combinable."},
				},
				Required: []string{"action"},
			},
		},
		{
			Name:        "db_export",
			Description: "Export the project database to a SQL dump.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path":     {Type: "string", Description: "Project root."},
					"database": {Type: "string", Description: "Defaults to DB_DATABASE."},
					"output":   {Type: "string", Description: "Defaults to <database>.sql in project root."},
				},
			},
		},
		{
			Name:        "db_import",
			Description: "Import a SQL dump into the project database.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path":     {Type: "string", Description: "Project root."},
					"file":     {Type: "string", Description: "SQL dump file path."},
					"database": {Type: "string", Description: "Defaults to DB_DATABASE."},
				},
				Required: []string{"file"},
			},
		},
		{
			Name:        "db_create",
			Description: "Create the project database (and a _testing variant). Starts service if needed.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {Type: "string", Description: "Project root. Defaults to cwd."},
					"name": {
						Type:        "string",
						Description: "Database name (defaults to DB_DATABASE, then project dir name).",
					},
				},
			},
		},
		{
			Name:        "php_list",
			Description: "List installed PHP versions (global default marked).",
			InputSchema: mcpSchema{
				Type:       "object",
				Properties: map[string]mcpProp{},
			},
		},
		{
			Name:        "php_ext",
			Description: "Manage custom PHP extensions. add/remove rebuild FPM (slow).",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"action":    {Type: "string", Enum: []string{"list", "add", "remove"}},
					"extension": {Type: "string", Description: "Required for add/remove (e.g. imagick, redis, swoole)."},
					"version":   {Type: "string", Description: "PHP version. Defaults to project/global."},
				},
				Required: []string{"action"},
			},
		},
		{
			Name:        "park",
			Description: "Register a parent directory as a park (auto-registers all PHP projects under it).",
			InputSchema: mcpSchema{
				Type:       "object",
				Properties: map[string]mcpProp{"path": {Type: "string", Description: "Parent directory."}},
			},
		},
		{
			Name:        "unpark",
			Description: "Remove a parked directory and unlink sites under it. Project files are kept.",
			InputSchema: mcpSchema{
				Type:       "object",
				Properties: map[string]mcpProp{"path": {Type: "string", Description: "Parked directory path."}},
				Required:   []string{"path"},
			},
		},
		{
			Name:        "which",
			Description: "Show resolved PHP/Node versions, docroot, and nginx config path for a site.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {Type: "string", Description: "Project root. Defaults to cwd."},
				},
			},
		},
		{
			Name:        "check",
			Description: "Validate .lerd.yaml (syntax, PHP, framework, services, workers, container, db). Reports OK/WARN/FAIL per field.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {Type: "string", Description: "Project root. Defaults to cwd."},
				},
			},
		},
	}

	if siteHasConsole() {
		tools = append(tools,
			mcpTool{
				Name:        "artisan",
				Description: "Run `php artisan` in the project's PHP-FPM container.",
				InputSchema: mcpSchema{
					Type: "object",
					Properties: map[string]mcpProp{
						"path": {
							Type:        "string",
							Description: "Project root (defaults to LERD_SITE_PATH or cwd).",
						},
						"args": {
							Type:        "array",
							Description: `Artisan arguments, e.g. ["migrate"], ["make:model", "Post", "-m"].`,
						},
					},
					Required: []string{"args"},
				},
			},
			mcpTool{
				Name:        "queue",
				Description: "Start or stop a Laravel queue:work worker (systemd user service) for a site.",
				InputSchema: mcpSchema{
					Type: "object",
					Properties: map[string]mcpProp{
						"action": {Type: "string", Enum: []string{"start", "stop"}},
						"site":   {Type: "string", Description: "Site name (from sites)."},
						"queue": {
							Type:        "string",
							Description: `Queue name for action=start (default "default").`,
						},
						"tries": {
							Type:        "integer",
							Description: "Max attempts for action=start (default 3).",
						},
						"timeout": {
							Type:        "integer",
							Description: "Job timeout seconds for action=start (default 60).",
						},
					},
					Required: []string{"action", "site"},
				},
			},
			mcpTool{
				Name:        "reverb",
				Description: "Start or stop Laravel Reverb (WebSocket server) for a site.",
				InputSchema: mcpSchema{
					Type: "object",
					Properties: map[string]mcpProp{
						"action": {Type: "string", Enum: []string{"start", "stop"}},
						"site":   {Type: "string", Description: "Site name (from sites)."},
					},
					Required: []string{"action", "site"},
				},
			},
			mcpTool{
				Name:        "horizon",
				Description: "Start or stop Laravel Horizon for a site (requires laravel/horizon; replaces queue:work).",
				InputSchema: mcpSchema{
					Type: "object",
					Properties: map[string]mcpProp{
						"action": {Type: "string", Enum: []string{"start", "stop"}},
						"site":   {Type: "string", Description: "Site name (from sites)."},
					},
					Required: []string{"action", "site"},
				},
			},
			mcpTool{
				Name:        "schedule",
				Description: "Start or stop the Laravel scheduler (schedule:work) for a site.",
				InputSchema: mcpSchema{
					Type: "object",
					Properties: map[string]mcpProp{
						"action": {Type: "string", Enum: []string{"start", "stop"}},
						"site":   {Type: "string", Description: "Site name (from sites)."},
					},
					Required: []string{"action", "site"},
				},
			},
			mcpTool{
				Name:        "stripe",
				Description: "Start or stop a Stripe webhook listener. Reads STRIPE_SECRET from .env.",
				InputSchema: mcpSchema{
					Type: "object",
					Properties: map[string]mcpProp{
						"action":       {Type: "string", Enum: []string{"start", "stop"}},
						"site":         {Type: "string"},
						"api_key":      {Type: "string", Description: "Defaults to STRIPE_SECRET."},
						"webhook_path": {Type: "string", Description: "Default /stripe/webhook."},
					},
					Required: []string{"action", "site"},
				},
			},
		)
	}

	if fw, ok := siteFramework(); ok && fw.Console != "" && fw.Console != "artisan" {
		tools = append(tools, mcpTool{
			Name:        "console",
			Description: fmt.Sprintf("Run `php %s` in the project's PHP-FPM container.", fw.Console),
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {Type: "string", Description: "Project root. Defaults to cwd."},
					"args": {
						Type:        "array",
						Description: fmt.Sprintf(`Console arguments, e.g. ["%s", "cache:clear"].`, fw.Console),
					},
				},
				Required: []string{"args"},
			},
		})
	}

	tools = append(tools,
		mcpTool{
			Name:        "worker",
			Description: "Start or stop a framework-defined worker. Call worker_list first.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"action": {Type: "string", Enum: []string{"start", "stop"}},
					"site":   {Type: "string"},
					"worker": {Type: "string", Description: "e.g. messenger, horizon, pulse."},
				},
				Required: []string{"action", "site", "worker"},
			},
		},
		mcpTool{
			Name:        "worker_list",
			Description: "List workers defined for a site's framework, including running status.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"site": {Type: "string", Description: "Site name (from sites)."},
				},
				Required: []string{"site"},
			},
		},
		mcpTool{
			Name:        "worker_add",
			Description: "Add or update a custom worker. Saves to .lerd.yaml (global=true → user overlay). Start via worker.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"site":               {Type: "string", Description: "Site name (from sites)."},
					"name":               {Type: "string", Description: "Worker slug."},
					"command":            {Type: "string", Description: "Command run in PHP-FPM container."},
					"label":              {Type: "string", Description: "Human-readable label."},
					"restart":            {Type: "string", Description: "always (default) | on-failure."},
					"check_file":         {Type: "string", Description: "Show only if this file exists."},
					"check_composer":     {Type: "string", Description: "Show only if this Composer package is installed."},
					"conflicts_with":     {Type: "array", Description: "Workers to stop before starting this one."},
					"proxy_path":         {Type: "string", Description: "URL path to proxy (e.g. /app)."},
					"proxy_port_env_key": {Type: "string", Description: "Env key holding the worker port."},
					"proxy_default_port": {Type: "number", Description: "Fallback port when env key is unset."},
					"global":             {Type: "boolean", Description: "Save to user overlay."},
				},
				Required: []string{"site", "name", "command"},
			},
		},
		mcpTool{
			Name:        "worker_remove",
			Description: "Remove a custom worker. Stops if running.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"site":   {Type: "string"},
					"name":   {Type: "string", Description: "Worker name."},
					"global": {Type: "boolean", Description: "Target user overlay."},
				},
				Required: []string{"site", "name"},
			},
		},
		mcpTool{
			Name:        "framework_list",
			Description: "List framework definitions (built-in + user YAMLs), with their workers and setup commands.",
			InputSchema: mcpSchema{Type: "object", Properties: map[string]mcpProp{}},
		},
		mcpTool{
			Name:        "framework_add",
			Description: "Create or update a framework definition. name=laravel merges workers/setup into the built-in.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"name":                {Type: "string", Description: `Framework slug (e.g. "laravel", "symfony", "wordpress").`},
					"label":               {Type: "string", Description: "Human-readable name."},
					"public_dir":          {Type: "string", Description: "Document root."},
					"detect_files":        {Type: "array", Description: "Filenames that signal this framework."},
					"detect_packages":     {Type: "array", Description: "Composer packages that signal this framework."},
					"env_file":            {Type: "string", Description: `Primary env file (default ".env").`},
					"env_format":          {Type: "string", Description: "dotenv (default) or php-const."},
					"env_fallback_file":   {Type: "string", Description: `Secondary env file (e.g. "wp-config.php").`},
					"env_fallback_format": {Type: "string", Description: "Format for the fallback file."},
					"workers":             {Type: "object", Description: "Map of name → {label, command, restart, check?}."},
					"setup":               {Type: "array", Description: "{label, command, default?, check?} entries."},
					"logs":                {Type: "array", Description: `{path, format?: "monolog"|"raw"} entries.`},
				},
				Required: []string{"name"},
			},
		},
		mcpTool{
			Name:        "framework_remove",
			Description: "Delete a framework. For laravel, removes only custom additions.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"name":    {Type: "string", Description: "Framework slug."},
					"version": {Type: "string", Description: "Optional. Omit to remove all versions."},
				},
				Required: []string{"name"},
			},
		},
		mcpTool{
			Name:        "framework_search",
			Description: "Search the community framework store.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"query": {
						Type:        "string",
						Description: "Query (matches name or label, case-insensitive).",
					},
				},
				Required: []string{"query"},
			},
		},
		mcpTool{
			Name:        "framework_install",
			Description: "Install a framework from the community store. Auto-detects version from composer.lock if omitted.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"name": {
						Type:        "string",
						Description: "Framework name (e.g. symfony, wordpress).",
					},
					"version": {
						Type:        "string",
						Description: "Major version (e.g. 11, 7). Omit to auto-detect.",
					},
				},
				Required: []string{"name"},
			},
		},
		mcpTool{
			Name:        "project_new",
			Description: "Scaffold a new PHP project via framework create command (default laravel). Follow with site_link.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path":      {Type: "string", Description: "New project directory."},
					"framework": {Type: "string", Description: `Defaults to "laravel".`},
					"args":      {Type: "array", Description: "Extra args for the scaffold command."},
				},
				Required: []string{"path"},
			},
		},
		mcpTool{
			Name:        "site_php",
			Description: "Change a site's PHP version. Writes .php-version and regenerates the nginx vhost.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"site": {Type: "string", Description: "Site name (from sites)."},
					"version": {
						Type:        "string",
						Description: "PHP version (e.g. 8.4, 8.3).",
					},
				},
				Required: []string{"site", "version"},
			},
		},
		mcpTool{
			Name:        "site_node",
			Description: "Change a site's Node.js version. Writes .node-version; installs via fnm if needed.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"site": {Type: "string", Description: "Site name (from sites)."},
					"version": {
						Type:        "string",
						Description: "Node.js version (e.g. 22, 20, lts).",
					},
				},
				Required: []string{"site", "version"},
			},
		},
		mcpTool{
			Name:        "site_control",
			Description: "pause (stop workers + landing vhost), unpause, restart (no rebuild), rebuild (image rebuild + restart; custom containers only).",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"action": {Type: "string", Enum: []string{"pause", "unpause", "restart", "rebuild"}},
					"site":   {Type: "string"},
				},
				Required: []string{"action", "site"},
			},
		},
		mcpTool{
			Name:        "site_runtime",
			Description: "Switch between fpm (shared container) and frankenphp (per-site, keeps PHP resident). worker=true enables framework-aware worker mode.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"site":    {Type: "string"},
					"runtime": {Type: "string", Enum: []string{"fpm", "frankenphp"}},
					"worker":  {Type: "boolean", Description: "frankenphp worker mode. Ignored for fpm."},
				},
				Required: []string{"site", "runtime"},
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

	action := strArg(args, "action")
	unknownAction := func(tool string) (any, *rpcError) {
		return toolErr(fmt.Sprintf("unknown action %q for tool %q", action, tool)), nil
	}

	switch p.Name {
	case "artisan":
		return execArtisan(args)
	case "console":
		return execArtisan(args)
	case "sites":
		return execSites()

	case "service_control":
		switch action {
		case "start":
			return execServiceStart(args)
		case "stop":
			return execServiceStop(args)
		case "restart":
			return execServiceRestart(args)
		case "pin":
			return execServicePin(args)
		case "unpin":
			return execServiceUnpin(args)
		case "update":
			return execServiceUpdate(args)
		case "rollback":
			return execServiceRollback(args)
		case "migrate":
			return execServiceMigrate(args)
		case "remove":
			return execServiceRemove(args)
		default:
			return unknownAction("service_control")
		}

	case "queue":
		switch action {
		case "start":
			return execQueueStart(args)
		case "stop":
			return execQueueStop(args)
		default:
			return unknownAction("queue")
		}

	case "reverb":
		switch action {
		case "start":
			return execReverbStart(args)
		case "stop":
			return execReverbStop(args)
		default:
			return unknownAction("reverb")
		}

	case "horizon":
		switch action {
		case "start":
			return execHorizonStart(args)
		case "stop":
			return execHorizonStop(args)
		default:
			return unknownAction("horizon")
		}

	case "schedule":
		switch action {
		case "start":
			return execScheduleStart(args)
		case "stop":
			return execScheduleStop(args)
		default:
			return unknownAction("schedule")
		}

	case "stripe":
		switch action {
		case "start":
			return execStripeListen(args)
		case "stop":
			return execStripeListenStop(args)
		default:
			return unknownAction("stripe")
		}

	case "worker":
		switch action {
		case "start":
			return execWorkerStart(args)
		case "stop":
			return execWorkerStop(args)
		default:
			return unknownAction("worker")
		}
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

	case "node":
		switch action {
		case "install":
			return execNodeInstall(args)
		case "uninstall":
			return execNodeUninstall(args)
		default:
			return unknownAction("node")
		}

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
	case "service_expose":
		return execServiceExpose(args)
	case "service_preset_list":
		return execServicePresetList(args)
	case "service_preset_install":
		return execServicePresetInstall(args)
	case "service_check_updates":
		return execServiceCheckUpdates(args)
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

	case "site_domain":
		switch action {
		case "add":
			return execSiteDomainAdd(args)
		case "remove":
			return execSiteDomainRemove(args)
		default:
			return unknownAction("site_domain")
		}

	case "site_tls":
		switch action {
		case "enable":
			return execSecure(args)
		case "disable":
			return execUnsecure(args)
		default:
			return unknownAction("site_tls")
		}

	case "xdebug":
		switch action {
		case "on":
			return execXdebugToggle(args, true)
		case "off":
			return execXdebugToggle(args, false)
		case "status":
			return execXdebugStatus()
		default:
			return unknownAction("xdebug")
		}

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
	case "setup":
		return execSetup(args)
	case "site_php":
		return execSitePHP(args)
	case "site_node":
		return execSiteNode(args)

	case "site_control":
		switch action {
		case "pause":
			return execSitePause(args)
		case "unpause":
			return execSiteUnpause(args)
		case "restart":
			return execSiteRestart(args)
		case "rebuild":
			return execSiteRebuild(args)
		default:
			return unknownAction("site_control")
		}

	case "site_runtime":
		return execSiteRuntime(args)

	case "db_import":
		return execDBImport(args)
	case "db_create":
		return execDBCreate(args)
	case "php_list":
		return execPHPList()

	case "php_ext":
		switch action {
		case "list":
			return execPHPExtList(args)
		case "add":
			return execPHPExtAdd(args)
		case "remove":
			return execPHPExtRemove(args)
		default:
			return unknownAction("php_ext")
		}

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

func isKnownService(name string) bool { return config.IsDefaultPreset(name) }

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
		Name            string         `json:"name"`
		Domain          string         `json:"domain"`
		Domains         []string       `json:"domains"`
		Path            string         `json:"path"`
		PHPVersion      string         `json:"php_version"`
		NodeVersion     string         `json:"node_version"`
		TLS             bool           `json:"tls"`
		Framework       string         `json:"framework,omitempty"`
		CustomContainer bool           `json:"custom_container,omitempty"`
		ContainerPort   int            `json:"container_port,omitempty"`
		ContainerSSL    bool           `json:"container_ssl,omitempty"`
		Workers         []workerStatus `json:"workers,omitempty"`
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
			Name:            e.Name,
			Domain:          e.PrimaryDomain(),
			Domains:         e.Domains,
			Path:            e.Path,
			PHPVersion:      e.PHPVersion,
			NodeVersion:     e.NodeVersion,
			TLS:             e.Secured,
			Framework:       e.FrameworkName,
			CustomContainer: e.ContainerPort > 0,
			ContainerPort:   e.ContainerPort,
			ContainerSSL:    e.ContainerSSL,
			Workers:         workers,
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
		if err := serviceops.EnsureDefaultPresetQuadlet(name); err != nil {
			return toolErr("ensuring default preset quadlet: " + err.Error()), nil
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

func execServiceRestart(args map[string]any) (any, *rpcError) {
	name := strArg(args, "name")
	if name == "" {
		return toolErr("name is required"), nil
	}
	if isKnownService(name) {
		if err := serviceops.EnsureDefaultPresetQuadlet(name); err != nil {
			return toolErr("ensuring quadlet: " + err.Error()), nil
		}
	} else if svc, err := config.LoadCustomService(name); err == nil {
		if err := serviceops.EnsureCustomServiceQuadlet(svc); err != nil {
			return toolErr("ensuring quadlet: " + err.Error()), nil
		}
	}
	if err := podman.RestartUnit("lerd-" + name); err != nil {
		return toolErr("restarting " + name + ": " + err.Error()), nil
	}
	return toolOK(name + " restarted"), nil
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
		if cfg.Container != nil {
			// Custom container site: workers must be defined in custom_workers.
			for _, w := range cfg.Workers {
				if _, ok := cfg.CustomWorkers[w]; ok {
					add("worker_"+w, "ok", "")
				} else {
					add("worker_"+w, "fail", "not defined in custom_workers")
				}
			}
		} else {
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
		if svc.Preset != "" {
			if _, err := config.LoadPreset(svc.Preset); err != nil {
				add("service_"+svc.Name, "fail", fmt.Sprintf("unknown preset %q", svc.Preset))
			} else if _, err := config.LoadCustomService(svc.Name); err != nil {
				add("service_"+svc.Name, "warn", fmt.Sprintf("preset %q not installed — run: lerd service preset install %s", svc.Preset, svc.Preset))
			} else {
				add("service_"+svc.Name, "ok", "preset: "+svc.Preset)
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

	// Container
	if cfg.Container != nil {
		if cfg.Container.Port <= 0 || cfg.Container.Port > 65535 {
			add("container.port", "fail", "required and must be 1–65535")
		} else {
			add("container.port", "ok", fmt.Sprintf("%d", cfg.Container.Port))
		}
		cfPath := cfg.Container.Containerfile
		if cfPath == "" {
			cfPath = "Containerfile.lerd"
		}
		if _, err := os.Stat(filepath.Join(projectPath, cfPath)); os.IsNotExist(err) {
			add("container.containerfile", "warn", cfPath+" not found — lerd link will fail")
		} else {
			add("container.containerfile", "ok", cfPath)
		}
		if cfg.Container.BuildContext != "" {
			if _, err := os.Stat(filepath.Join(projectPath, cfg.Container.BuildContext)); os.IsNotExist(err) {
				add("container.build_context", "warn", cfg.Container.BuildContext+" not found")
			} else {
				add("container.build_context", "ok", cfg.Container.BuildContext)
			}
		}
		if cfg.Container.SSL {
			add("container.ssl", "ok", "nginx will proxy_pass via HTTPS with ssl_verify off")
		}
	}

	// custom_workers
	for name, w := range cfg.CustomWorkers {
		if w.Command == "" {
			add("custom_worker."+name, "fail", "command is required")
		} else {
			add("custom_worker."+name, "ok", "")
		}
	}

	// db
	if cfg.DB.Service != "" {
		if isKnownService(cfg.DB.Service) {
			add("db.service", "ok", cfg.DB.Service)
		} else if _, err := config.LoadCustomService(cfg.DB.Service); err == nil {
			add("db.service", "ok", cfg.DB.Service+" (custom)")
		} else {
			add("db.service", "fail", cfg.DB.Service+" is not a known service")
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

func execServiceCheckUpdates(args map[string]any) (any, *rpcError) {
	name := strArg(args, "name")
	var names []string
	if name != "" {
		names = []string{name}
	} else {
		for _, s := range knownServices() {
			unit := "lerd-" + s
			if status, _ := podman.UnitStatus(unit); status == "active" {
				names = append(names, s)
			}
		}
	}
	results := make([]map[string]any, 0, len(names))
	for _, n := range names {
		avail, err := serviceops.CheckUpdateAvailable(n)
		if err != nil || avail == nil {
			continue
		}
		entry := map[string]any{
			"service":       n,
			"current_image": avail.CurrentImage,
			"current_tag":   avail.CurrentTag,
			"strategy":      avail.Strategy,
			"available":     avail.Available,
		}
		if avail.LatestTag != "" {
			entry["latest_tag"] = avail.LatestTag
		}
		if avail.UpgradeTag != "" {
			entry["upgrade_tag"] = avail.UpgradeTag
		}
		results = append(results, entry)
	}
	return map[string]any{"services": results}, nil
}

func execServiceUpdate(args map[string]any) (any, *rpcError) {
	name := strArg(args, "name")
	if name == "" {
		return toolErr("name is required"), nil
	}
	tag := strArg(args, "tag")
	targetImage := ""
	if tag != "" {
		avail, err := serviceops.CheckUpdateAvailable(name)
		if err != nil || avail == nil || avail.CurrentImage == "" {
			return toolErr("could not resolve current image for " + name), nil
		}
		if at := strings.LastIndex(avail.CurrentImage, ":"); at > 0 {
			targetImage = avail.CurrentImage[:at] + ":" + tag
		} else {
			targetImage = avail.CurrentImage + ":" + tag
		}
	}
	var lastImage string
	emit := func(ev serviceops.PhaseEvent) {
		if ev.Image != "" {
			lastImage = ev.Image
		}
	}
	if err := serviceops.UpdateServiceStreaming(name, targetImage, emit); err != nil {
		return toolErr(err.Error()), nil
	}
	if lastImage == "" {
		return toolOK("already up to date"), nil
	}
	return toolOK("Updated " + name + " to " + lastImage), nil
}

func execServiceMigrate(args map[string]any) (any, *rpcError) {
	name := strArg(args, "name")
	tag := strArg(args, "tag")
	if name == "" || tag == "" {
		return toolErr("name and tag are required"), nil
	}
	avail, err := serviceops.CheckUpdateAvailable(name)
	if err != nil || avail.CurrentImage == "" {
		return toolErr("could not resolve current image for " + name), nil
	}
	target := avail.CurrentImage
	if at := strings.LastIndex(target, ":"); at > 0 {
		target = target[:at] + ":" + tag
	}
	var lastImage string
	emit := func(ev serviceops.PhaseEvent) {
		if ev.Image != "" {
			lastImage = ev.Image
		}
	}
	if err := serviceops.MigrateService(name, target, emit); err != nil {
		return toolErr(err.Error()), nil
	}
	return toolOK("Migrated " + name + " to " + lastImage + ". Backup preserved in ~/.local/share/lerd/backups."), nil
}

func execServiceRollback(args map[string]any) (any, *rpcError) {
	name := strArg(args, "name")
	if name == "" {
		return toolErr("name is required"), nil
	}
	var lastImage string
	emit := func(ev serviceops.PhaseEvent) {
		if ev.Image != "" {
			lastImage = ev.Image
		}
	}
	if err := serviceops.RollbackService(name, emit); err != nil {
		return toolErr(err.Error()), nil
	}
	return toolOK("Rolled back " + name + " to " + lastImage), nil
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
	if err := serviceops.EnsureDefaultPresetQuadlet(name); err != nil {
		return toolErr("ensuring default preset quadlet: " + err.Error()), nil
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

	// Check built-in (default-preset) services first.
	if pairs := builtinServiceEnv(name); pairs != nil {
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

	proj, _ := config.LoadProjectConfig(projectPath)

	rawName := strArg(args, "name")
	if rawName == "" {
		rawName = filepath.Base(projectPath)
	}
	name, _ := siteops.SiteNameAndDomain(rawName, cfg.DNS.TLD)

	// Build domains: prefer .lerd.yaml domains, fall back to auto-generated.
	var domains []string
	if proj != nil && len(proj.Domains) > 0 {
		for _, d := range proj.Domains {
			domains = append(domains, strings.ToLower(d)+"."+cfg.DNS.TLD)
		}
	} else {
		_, domain := siteops.SiteNameAndDomain(rawName, cfg.DNS.TLD)
		domains = []string{domain}
	}

	// Validate domains are not used by other sites.
	for _, d := range domains {
		if existing, err := config.IsDomainUsed(d); err == nil && existing != nil && existing.Path != projectPath {
			return toolErr(fmt.Sprintf("domain %q is already used by site %q", d, existing.Name)), nil
		}
	}

	// Custom container path: .lerd.yaml has a container section with a port.
	if proj != nil && proj.Container != nil && proj.Container.Port > 0 {
		secured := siteops.CleanupRelink(projectPath, name) || (proj != nil && proj.Secured)
		site := config.Site{
			Name:          name,
			Domains:       domains,
			Path:          projectPath,
			Secured:       secured,
			ContainerPort: proj.Container.Port,
			ContainerSSL:  proj.Container.SSL,
		}
		if err := config.AddSite(site); err != nil {
			return toolErr("registering site: " + err.Error()), nil
		}
		_ = config.SyncProjectDomains(projectPath, site.Domains, cfg.DNS.TLD)
		if err := siteops.FinishCustomLink(site, proj.Container); err != nil {
			return toolErr(err.Error()), nil
		}
		return toolOK(fmt.Sprintf("Linked %s -> %s (custom container, port %d)", name, strings.Join(domains, ", "), proj.Container.Port)), nil
	}

	// PHP / framework path.
	framework := ""
	if fname, ok := config.DetectFrameworkForDir(projectPath); ok {
		framework = fname
	}
	versions := siteops.DetectSiteVersions(projectPath, framework, cfg.PHP.DefaultVersion, cfg.Node.DefaultVersion)
	phpVersion, nodeVersion := versions.PHP, versions.Node
	if proj != nil && proj.PHPVersion != "" {
		phpVersion = proj.PHPVersion
	}

	secured := siteops.CleanupRelink(projectPath, name) || (proj != nil && proj.Secured)
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
	_ = config.SyncProjectDomains(projectPath, site.Domains, cfg.DNS.TLD)

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

	applyMode := ""
	if enable {
		applyMode = strArg(args, "mode")
		if applyMode == "" {
			applyMode = "debug"
		}
	}

	res, err := xdebugops.Apply(version, applyMode)
	if err != nil {
		return toolErr(err.Error()), nil
	}

	if res.NoChange {
		if res.Enabled {
			return toolOK(fmt.Sprintf("Xdebug is already enabled (mode=%s) for PHP %s", res.Mode, version)), nil
		}
		return toolOK(fmt.Sprintf("Xdebug is already disabled for PHP %s", version)), nil
	}

	summary := fmt.Sprintf("Xdebug disabled for PHP %s", version)
	if res.Enabled {
		summary = fmt.Sprintf("Xdebug enabled for PHP %s (mode=%s, port 9003, host.containers.internal)", version, res.Mode)
	}
	if res.RestartErr != nil {
		unit := xdebugops.FPMUnit(version)
		return toolOK(fmt.Sprintf("%s\n[WARN] FPM restart failed: %v\nRun: systemctl --user restart %s", summary, res.RestartErr, unit)), nil
	}
	return toolOK(summary), nil
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
		Mode    string `json:"mode,omitempty"`
	}
	result := make([]entry, 0, len(versions))
	for _, v := range versions {
		mode := cfg.GetXdebugMode(v)
		result = append(result, entry{Version: v, Enabled: mode != "", Mode: mode})
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

	// Framework create commands use --no-install --no-plugins --no-scripts so
	// the scaffolder doesn't race with lerd's post-link setup. Chase with
	// `composer install` in the FPM container so project_new returns a
	// ready-to-work vendor/ directory and any post-install scripts fire.
	if composerErr := runComposerInstallIfNeeded(projectPath, &out); composerErr != nil {
		return toolErr(fmt.Sprintf("scaffold succeeded but composer install failed: %v\n%s", composerErr, stripANSI(out.String()))), nil
	}

	return toolOK(fmt.Sprintf("Project created at %s\n\nNext steps:\n  site_link(path: %q)\n  env_setup(path: %q)\n\n%s",
		projectPath, projectPath, projectPath, stripANSI(strings.TrimSpace(out.String())))), nil
}

// runComposerInstallIfNeeded runs `composer install` inside the FPM container
// matching projectPath's PHP version when composer.json exists but vendor/
// does not. Output is appended to the provided buffer.
func runComposerInstallIfNeeded(projectPath string, out *bytes.Buffer) error {
	if _, err := os.Stat(filepath.Join(projectPath, "composer.json")); err != nil {
		return nil
	}
	if _, err := os.Stat(filepath.Join(projectPath, "vendor")); err == nil {
		return nil
	}

	phpVersion, err := phpDet.DetectVersion(projectPath)
	if err != nil || phpVersion == "" {
		cfg, cfgErr := config.LoadGlobal()
		if cfgErr != nil || cfg == nil {
			return fmt.Errorf("could not determine PHP version: %w", err)
		}
		phpVersion = cfg.PHP.DefaultVersion
	}
	container := "lerd-php" + strings.ReplaceAll(phpVersion, ".", "") + "-fpm"

	out.WriteString("\n\n--- composer install ---\n")
	cmd := podman.Cmd("exec", "-w", projectPath, container, "composer", "install", "--no-interaction")
	cmd.Stdout = out
	cmd.Stderr = out
	return cmd.Run()
}

// execSetup runs every Default: true entry in the site framework's Setup list
// whose Check rule passes, mirroring what the `lerd setup` CLI does when the
// user keeps the default selections. Commands run in the site's PHP-FPM
// container via `podman exec`. A single step failure is reported but doesn't
// abort the rest — these commands are idempotent by convention.
func execSetup(args map[string]any) (any, *rpcError) {
	projectPath := resolvedPath(args)
	if projectPath == "" {
		return toolErr("path is required — pass a path argument or open Claude in the project directory"), nil
	}
	site, err := config.FindSiteByPath(projectPath)
	if err != nil || site == nil {
		return toolErr("no site registered at " + projectPath + " — run site_link first"), nil
	}
	fwName := site.Framework
	if fwName == "" {
		fwName, _ = config.DetectFrameworkForDir(projectPath)
	}
	if fwName == "" {
		return toolErr("no framework detected — nothing to set up"), nil
	}
	fw, ok := config.GetFramework(fwName)
	if !ok {
		return toolErr(fmt.Sprintf("framework %q is not defined", fwName)), nil
	}

	phpVersion, phpErr := phpDet.DetectVersion(projectPath)
	if phpErr != nil || phpVersion == "" {
		cfg, cfgErr := config.LoadGlobal()
		if cfgErr != nil || cfg == nil {
			return toolErr("could not determine PHP version"), nil
		}
		phpVersion = cfg.PHP.DefaultVersion
	}
	container := "lerd-php" + strings.ReplaceAll(phpVersion, ".", "") + "-fpm"

	var out bytes.Buffer
	ran, skipped, failed := 0, 0, 0
	for _, step := range fw.Setup {
		if !step.Default {
			skipped++
			continue
		}
		if step.Check != nil && !config.MatchesRule(projectPath, *step.Check) {
			skipped++
			continue
		}
		parts := strings.Fields(step.Command)
		if len(parts) == 0 {
			continue
		}
		fmt.Fprintf(&out, "\n--- %s ---\n", step.Label)
		cmdArgs := append([]string{"exec", "-i", "-w", projectPath, container}, parts...)
		cmd := podman.Cmd(cmdArgs...)
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(&out, "[WARN] %s failed: %v\n", step.Label, err)
			failed++
			continue
		}
		ran++
	}

	if ran == 0 && failed == 0 {
		return toolOK(fmt.Sprintf("No default setup steps to run for %s.", fw.Label)), nil
	}
	summary := fmt.Sprintf("%s setup: %d ran, %d skipped, %d failed.", fw.Label, ran, skipped, failed)
	return toolOK(summary + "\n" + stripANSI(strings.TrimSpace(out.String()))), nil
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
	if site.IsCustomContainer() {
		return toolErr("custom container sites do not use PHP versions — the container defines its own runtime"), nil
	}

	// Write .php-version pin file (keeps CLI php and other tools in sync).
	phpVersionFile := filepath.Join(site.Path, ".php-version")
	if err := os.WriteFile(phpVersionFile, []byte(version+"\n"), 0644); err != nil {
		return toolErr("writing .php-version: " + err.Error()), nil
	}
	_ = config.SetProjectPHPVersion(site.Path, version)

	// Update the site registry so later steps see the new version.
	site.PHPVersion = version
	if err := config.AddSite(*site); err != nil {
		return toolErr("updating site registry: " + err.Error()), nil
	}

	// FrankenPHP sites get a different image per PHP version; rewrite the
	// per-site quadlet (with restart-on-change) via the shared link helper
	// instead of touching FPM state or the FPM vhost.
	if site.IsFrankenPHP() {
		if err := siteops.FinishFrankenPHPLink(*site); err != nil {
			return toolErr("re-linking FrankenPHP site: " + err.Error()), nil
		}
		return toolOK(fmt.Sprintf("PHP version for %s set to %s (FrankenPHP image updated).", siteName, version)), nil
	}

	// Ensure the FPM quadlet and xdebug ini exist for this version.
	if err := podman.WriteFPMQuadlet(version); err != nil {
		return toolErr("writing FPM quadlet: " + err.Error()), nil
	}
	_ = podman.EnsureXdebugIni(version) // non-fatal if version not yet built

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

func execSiteRestart(args map[string]any) (any, *rpcError) {
	siteName := strArg(args, "site")
	if siteName == "" {
		return toolErr("site is required"), nil
	}
	return runLerdCmd("restart", siteName)
}

func execSiteRebuild(args map[string]any) (any, *rpcError) {
	siteName := strArg(args, "site")
	if siteName == "" {
		return toolErr("site is required"), nil
	}
	return runLerdCmd("rebuild", siteName)
}

func execSiteRuntime(args map[string]any) (any, *rpcError) {
	siteName := strArg(args, "site")
	if siteName == "" {
		return toolErr("site is required"), nil
	}
	runtime := strArg(args, "runtime")
	if runtime != "fpm" && runtime != "frankenphp" {
		return toolErr("runtime must be 'fpm' or 'frankenphp'"), nil
	}
	worker := boolArg(args, "worker")

	site, err := config.FindSite(siteName)
	if err != nil {
		return toolErr(fmt.Sprintf("site %q not found", siteName)), nil
	}
	if site.IsCustomContainer() {
		return toolErr("site uses a custom Containerfile; runtime is defined by Containerfile.lerd"), nil
	}

	if runtime == "fpm" {
		if !site.IsFrankenPHP() {
			return toolOK(fmt.Sprintf("%s already on fpm runtime", siteName)), nil
		}
		_ = podman.StopUnit(podman.FrankenPHPContainerName(site.Name))
		_ = podman.RemoveFrankenPHPQuadlet(site.Name)
		_ = podman.DaemonReloadFn()
		site.Runtime = ""
		site.RuntimeWorker = false
		if err := config.AddSite(*site); err != nil {
			return toolErr("updating site: " + err.Error()), nil
		}
		_ = config.SetProjectRuntime(site.Path, "", false)
		if site.Secured {
			if err := nginx.GenerateSSLVhost(*site, site.PHPVersion); err != nil {
				return toolErr("regenerating SSL vhost: " + err.Error()), nil
			}
		} else if err := nginx.GenerateVhost(*site, site.PHPVersion); err != nil {
			return toolErr("regenerating vhost: " + err.Error()), nil
		}
		_ = nginx.Reload()
		return toolOK(fmt.Sprintf("%s: runtime set to fpm", siteName)), nil
	}

	site.Runtime = "frankenphp"
	site.RuntimeWorker = worker
	if err := config.AddSite(*site); err != nil {
		return toolErr("updating site: " + err.Error()), nil
	}
	_ = config.SetProjectRuntime(site.Path, "frankenphp", worker)
	if err := siteops.FinishFrankenPHPLink(*site); err != nil {
		return toolErr("linking FrankenPHP site: " + err.Error()), nil
	}
	label := "frankenphp"
	if worker {
		label = "frankenphp (worker mode)"
	}
	return toolOK(fmt.Sprintf("%s: runtime set to %s", siteName, label)), nil
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
