package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/certs"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
	"github.com/geodro/lerd/internal/nginx"
	nodeDet "github.com/geodro/lerd/internal/node"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	lerdSystemd "github.com/geodro/lerd/internal/systemd"
)

const protocolVersion = "2024-11-05"

var knownServices = []string{"mysql", "redis", "postgres", "meilisearch", "minio", "mailpit"}

// defaultSitePath is set at startup from LERD_SITE_PATH, injected by mcp:inject.
// Tools that accept a "path" argument use this as a fallback when none is provided.
var defaultSitePath = os.Getenv("LERD_SITE_PATH")

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

func toolList() []mcpTool {
	return []mcpTool{
		{
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
						Description: "Service to start (built-in: mysql, redis, postgres, meilisearch, minio, mailpit — or any custom service name registered with service_add)",
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
						Description: "Service to stop (built-in: mysql, redis, postgres, meilisearch, minio, mailpit — or any custom service name registered with service_add)",
					},
				},
				Required: []string{"name"},
			},
		},
		{
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
		{
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
		{
			Name:        "logs",
			Description: `Fetch recent container logs for a lerd service or PHP-FPM container. Valid targets: "nginx", a service name (mysql, redis, etc.), a PHP version (8.4, 8.5), or a site name for its FPM logs.`,
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"target": {
						Type:        "string",
						Description: `Container target: "nginx", service name like "mysql", PHP version like "8.4", or site name`,
					},
					"lines": {
						Type:        "integer",
						Description: "Number of lines to return from the tail (default: 50)",
					},
				},
				Required: []string{"target"},
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
			Name:        "service_add",
			Description: "Register a new custom OCI-based service with lerd (e.g. MongoDB, RabbitMQ, Cassandra). Writes a systemd quadlet so the service can be started/stopped like built-in services.",
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
			Name:        "env_setup",
			Description: "Configure the project's .env for lerd: creates .env from .env.example if missing, detects services (mysql, redis, etc.), starts them, creates databases, generates APP_KEY, and sets APP_URL. Run this once after cloning a project.",
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
						Description: "Site name (defaults to the directory name, cleaned up)",
					},
					"domain": {
						Type:        "string",
						Description: "Custom domain (defaults to <name>.test)",
					},
				},
			},
		},
		{
			Name:        "site_unlink",
			Description: "Unregister a lerd site and remove its nginx vhost. The project files are NOT deleted.",
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
			Description: "Export the project's database to a SQL dump file. Reads connection details from the project .env.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"path": {
						Type:        "string",
						Description: "Absolute path to the Laravel project root. Defaults to LERD_SITE_PATH when omitted.",
					},
					"output": {
						Type:        "string",
						Description: "Output file path (defaults to <database>.sql in the project root)",
					},
				},
			},
		},
		{
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
		{
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
		{
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
		{
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
		{
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
		{
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
	}
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
	case "schedule_start":
		return execScheduleStart(args)
	case "schedule_stop":
		return execScheduleStop(args)
	case "stripe_listen":
		return execStripeListen(args)
	case "stripe_listen_stop":
		return execStripeListenStop(args)
	case "logs":
		return execLogs(args)
	case "composer":
		return execComposer(args)
	case "node_install":
		return execNodeInstall(args)
	case "node_uninstall":
		return execNodeUninstall(args)
	case "runtime_versions":
		return execRuntimeVersions()
	case "service_add":
		return execServiceAdd(args)
	case "service_remove":
		return execServiceRemove(args)
	case "env_setup":
		return execEnvSetup(args)
	case "site_link":
		return execSiteLink(args)
	case "site_unlink":
		return execSiteUnlink(args)
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
		return toolErr("path is required (or set LERD_SITE_PATH via mcp:inject)"), nil
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

	// No -it flags — non-interactive, output captured to buffer.
	cmdArgs := []string{"exec", "-w", projectPath, container, "php", "artisan"}
	cmdArgs = append(cmdArgs, artisanArgs...)

	var out bytes.Buffer
	cmd := exec.Command("podman", cmdArgs...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return toolErr(fmt.Sprintf("artisan failed (%v):\n%s", err, out.String())), nil
	}
	return toolOK(strings.TrimSpace(out.String())), nil
}

func execSites() (any, *rpcError) {
	reg, err := config.LoadSites()
	if err != nil {
		return toolErr("failed to load sites: " + err.Error()), nil
	}

	type siteInfo struct {
		Name         string `json:"name"`
		Domain       string `json:"domain"`
		Path         string `json:"path"`
		PHPVersion   string `json:"php_version"`
		NodeVersion  string `json:"node_version"`
		TLS          bool   `json:"tls"`
		QueueRunning bool   `json:"queue_running"`
	}

	var out []siteInfo
	for _, s := range reg.Sites {
		if s.Ignored {
			continue
		}
		queueStatus, _ := podman.UnitStatus("lerd-queue-" + s.Name)
		out = append(out, siteInfo{
			Name:         s.Name,
			Domain:       s.Domain,
			Path:         s.Path,
			PHPVersion:   s.PHPVersion,
			NodeVersion:  s.NodeVersion,
			TLS:          s.Secured,
			QueueRunning: queueStatus == "active",
		})
	}
	if out == nil {
		out = []siteInfo{}
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
		if err := podman.WriteQuadlet(unitName, content); err != nil {
			return toolErr("writing quadlet: " + err.Error()), nil
		}
	} else {
		svc, err := config.LoadCustomService(name)
		if err != nil {
			return toolErr("unknown service: " + name + ". Use service_add to register a custom service first."), nil
		}
		content := podman.GenerateCustomQuadlet(svc)
		if err := podman.WriteQuadlet(unitName, content); err != nil {
			return toolErr("writing quadlet: " + err.Error()), nil
		}
	}

	if err := podman.DaemonReload(); err != nil {
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
ExecStart=podman exec -w %s %s php artisan %s

[Install]
WantedBy=default.target
`, siteName, fpmUnit, fpmUnit, site.Path, container, artisanArgs)

	if err := lerdSystemd.WriteService(unitName, unit); err != nil {
		return toolErr("writing service unit: " + err.Error()), nil
	}
	if err := podman.DaemonReload(); err != nil {
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
	_ = podman.DaemonReload()
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
ExecStart=podman exec -w %s %s php artisan reverb:start

[Install]
WantedBy=default.target
`, siteName, fpmUnit, fpmUnit, site.Path, container)

	if err := lerdSystemd.WriteService(unitName, unit); err != nil {
		return toolErr("writing service unit: " + err.Error()), nil
	}
	if err := podman.DaemonReload(); err != nil {
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
	_ = podman.DaemonReload()
	return toolOK("Reverb stopped for " + siteName), nil
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
ExecStart=podman exec -w %s %s php artisan schedule:work

[Install]
WantedBy=default.target
`, siteName, fpmUnit, fpmUnit, site.Path, container)

	if err := lerdSystemd.WriteService(unitName, unit); err != nil {
		return toolErr("writing service unit: " + err.Error()), nil
	}
	if err := podman.DaemonReload(); err != nil {
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
	_ = podman.DaemonReload()
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
	forwardTo := scheme + "://" + site.Domain + webhookPath
	unitName := "lerd-stripe-" + siteName
	containerName := unitName

	unit := fmt.Sprintf(`[Unit]
Description=Lerd Stripe Listener (%s)
After=network.target

[Service]
Type=simple
Restart=on-failure
RestartSec=5
ExecStart=podman run --rm --replace --name %s --network host docker.io/stripe/stripe-cli:latest listen --api-key %s --forward-to %s --skip-verify

[Install]
WantedBy=default.target
`, siteName, containerName, apiKey, forwardTo)

	if err := lerdSystemd.WriteService(unitName, unit); err != nil {
		return toolErr("writing service unit: " + err.Error()), nil
	}
	if err := podman.DaemonReload(); err != nil {
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
	_ = podman.DaemonReload()
	return toolOK("Stripe listener stopped for " + siteName), nil
}

func execLogs(args map[string]any) (any, *rpcError) {
	target := strArg(args, "target")
	if target == "" {
		return toolErr("target is required"), nil
	}
	lines := intArg(args, "lines", 50)

	container, err := resolveLogsContainer(target)
	if err != nil {
		return toolErr(err.Error()), nil
	}

	var out bytes.Buffer
	cmd := exec.Command("podman", "logs", "--tail", fmt.Sprintf("%d", lines), container)
	cmd.Stdout = &out
	cmd.Stderr = &out
	_ = cmd.Run() // non-zero exit if container not running is fine — we return what we have

	return toolOK(strings.TrimSpace(out.String())), nil
}

func resolveLogsContainer(target string) (string, error) {
	if target == "nginx" {
		return "lerd-nginx", nil
	}
	if isKnownService(target) {
		return "lerd-" + target, nil
	}
	// PHP version like "8.4"
	if strings.Contains(target, ".") {
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
		return toolErr("path is required (or set LERD_SITE_PATH via mcp:inject)"), nil
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
	cmd := exec.Command("podman", cmdArgs...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return toolErr(fmt.Sprintf("composer failed (%v):\n%s", err, out.String())), nil
	}
	return toolOK(strings.TrimSpace(out.String())), nil
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
		return toolErr(fmt.Sprintf("fnm install %s failed (%v):\n%s", version, err, out.String())), nil
	}
	return toolOK(strings.TrimSpace(out.String())), nil
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
		return toolErr(fmt.Sprintf("fnm uninstall %s failed (%v):\n%s", version, err, out.String())), nil
	}
	return toolOK(strings.TrimSpace(out.String())), nil
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

	if svc.DataDir != "" {
		if err := os.MkdirAll(config.DataSubDir(svc.Name), 0755); err != nil {
			return toolErr("creating data directory: " + err.Error()), nil
		}
	}

	content := podman.GenerateCustomQuadlet(svc)
	unitName := "lerd-" + name
	if err := podman.WriteQuadlet(unitName, content); err != nil {
		return toolErr("writing quadlet: " + err.Error()), nil
	}
	if err := podman.DaemonReload(); err != nil {
		return toolErr("daemon-reload: " + err.Error()), nil
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
	if err := podman.RemoveQuadlet(unit); err != nil {
		return toolErr("removing quadlet: " + err.Error()), nil
	}
	_ = podman.DaemonReload()
	if err := config.RemoveCustomService(name); err != nil {
		return toolErr("removing service config: " + err.Error()), nil
	}

	return toolOK(fmt.Sprintf("Service %q removed. Persistent data was NOT deleted.", name)), nil
}

func execEnvSetup(args map[string]any) (any, *rpcError) {
	projectPath := resolvedPath(args)
	if projectPath == "" {
		return toolErr("path is required (or set LERD_SITE_PATH via mcp:inject)"), nil
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
		return toolErr(fmt.Sprintf("env setup failed (%v):\n%s", err, out.String())), nil
	}
	return toolOK(strings.TrimSpace(out.String())), nil
}

func execSiteLink(args map[string]any) (any, *rpcError) {
	projectPath := resolvedPath(args)
	if projectPath == "" {
		return toolErr("path is required (or set LERD_SITE_PATH via mcp:inject)"), nil
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		return toolErr("loading config: " + err.Error()), nil
	}

	rawName := strArg(args, "name")
	if rawName == "" {
		rawName = filepath.Base(projectPath)
	}
	name, domain := siteLinkNameAndDomain(rawName, cfg.DNS.TLD)
	if custom := strArg(args, "domain"); custom != "" {
		domain = strings.ToLower(custom)
	}

	phpVersion, err := phpDet.DetectVersion(projectPath)
	if err != nil {
		phpVersion = cfg.PHP.DefaultVersion
	}

	nodeVersion, err := nodeDet.DetectVersion(projectPath)
	if err != nil {
		nodeVersion = cfg.Node.DefaultVersion
	}

	secured := false
	if existing, err := config.FindSite(name); err == nil {
		secured = existing.Secured
	}

	site := config.Site{
		Name:        name,
		Domain:      domain,
		Path:        projectPath,
		PHPVersion:  phpVersion,
		NodeVersion: nodeVersion,
		Secured:     secured,
	}

	if err := config.AddSite(site); err != nil {
		return toolErr("registering site: " + err.Error()), nil
	}

	if err := nginx.GenerateVhost(site, phpVersion); err != nil {
		return toolErr("generating vhost: " + err.Error()), nil
	}

	// Write quadlet and xdebug ini for this PHP version (non-blocking — image must already exist).
	short := strings.ReplaceAll(phpVersion, ".", "")
	_ = podman.WriteXdebugIni(phpVersion, false)
	if err := podman.WriteFPMQuadlet(phpVersion); err != nil {
		// Non-fatal: container may already be running.
		_ = err
	} else {
		_ = podman.DaemonReload()
		_ = podman.StartUnit("lerd-php" + short + "-fpm")
	}

	if err := nginx.Reload(); err != nil {
		return toolOK(fmt.Sprintf("Linked %s -> %s (PHP %s, Node %s)\n[WARN] nginx reload: %v", name, domain, phpVersion, nodeVersion, err)), nil
	}

	return toolOK(fmt.Sprintf("Linked %s -> %s (PHP %s, Node %s)", name, domain, phpVersion, nodeVersion)), nil
}

func execSiteUnlink(args map[string]any) (any, *rpcError) {
	siteName := strArg(args, "site")
	if siteName == "" {
		return toolErr("site is required"), nil
	}

	site, err := config.FindSite(siteName)
	if err != nil {
		return toolErr(fmt.Sprintf("site %q not found", siteName)), nil
	}

	if err := nginx.RemoveVhost(site.Domain); err != nil {
		// Non-fatal — vhost may already be gone.
		_ = err
	}

	cfg, _ := config.LoadGlobal()
	isParked := false
	if cfg != nil {
		for _, dir := range cfg.ParkedDirectories {
			if filepath.Dir(site.Path) == dir {
				isParked = true
				break
			}
		}
	}

	if isParked {
		if err := config.IgnoreSite(siteName); err != nil {
			return toolErr("ignoring site: " + err.Error()), nil
		}
	} else {
		if err := config.RemoveSite(siteName); err != nil {
			return toolErr("removing site: " + err.Error()), nil
		}
	}

	_ = nginx.Reload()
	return toolOK(fmt.Sprintf("Unlinked %s (%s)", siteName, site.Domain)), nil
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
		"APP_URL": "https://" + site.Domain,
	}); err != nil {
		// Non-fatal — .env may not exist.
		_ = err
	}

	if err := nginx.Reload(); err != nil {
		return toolErr("reloading nginx: " + err.Error()), nil
	}

	return toolOK(fmt.Sprintf("Secured: https://%s", site.Domain)), nil
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
		"APP_URL": "http://" + site.Domain,
	}); err != nil {
		_ = err
	}

	if err := nginx.Reload(); err != nil {
		return toolErr("reloading nginx: " + err.Error()), nil
	}

	return toolOK(fmt.Sprintf("Unsecured: http://%s", site.Domain)), nil
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
		return toolErr("path is required (or set LERD_SITE_PATH via mcp:inject)"), nil
	}

	env, err := readDBEnv(projectPath)
	if err != nil {
		return toolErr(err.Error()), nil
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
		cmd = exec.Command("podman", "exec", "-i", "lerd-mysql",
			"mysqldump", "-u"+env.username, "-p"+env.password, env.database)
	case "pgsql", "postgres":
		cmd = exec.Command("podman", "exec", "-i", "-e", "PGPASSWORD="+env.password,
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
		return toolErr(fmt.Sprintf("export failed (%v):\n%s", err, stderr.String())), nil
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

// siteLinkNameAndDomain derives a clean site name and domain from a directory name.
// Mirrors the logic in internal/cli/park.go — kept in sync manually.
func siteLinkNameAndDomain(dirName, tld string) (string, string) {
	knownTLDs := []string{
		".com", ".net", ".org", ".io", ".co", ".ltd", ".dev", ".app", ".me",
		".info", ".biz", ".uk", ".us", ".eu", ".de", ".fr", ".ca", ".au",
	}
	name := strings.ToLower(dirName)
	for _, ext := range knownTLDs {
		if strings.HasSuffix(name, ext) {
			name = name[:len(name)-len(ext)]
			break
		}
	}
	name = strings.ReplaceAll(name, ".", "-")
	return name, name + "." + tld
}
