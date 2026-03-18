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

	"github.com/geodro/lerd/internal/config"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	lerdSystemd "github.com/geodro/lerd/internal/systemd"
)

const protocolVersion = "2024-11-05"

var knownServices = []string{"mysql", "redis", "postgres", "meilisearch", "minio", "mailpit", "soketi"}

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
						Description: "Absolute path to the Laravel project root (e.g. /home/user/code/myapp)",
					},
					"args": {
						Type:        "array",
						Description: `Artisan arguments as an array, e.g. ["migrate"] or ["make:model", "Post", "-m"] or ["tinker", "--execute=App\\Models\\User::count()"]`,
					},
				},
				Required: []string{"path", "args"},
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
			Description: "Start a lerd infrastructure service. Ensures the quadlet is written and the systemd unit is running.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"name": {
						Type:        "string",
						Description: "Service to start",
						Enum:        knownServices,
					},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:        "service_stop",
			Description: "Stop a running lerd infrastructure service.",
			InputSchema: mcpSchema{
				Type: "object",
				Properties: map[string]mcpProp{
					"name": {
						Type:        "string",
						Description: "Service to stop",
						Enum:        knownServices,
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
	case "logs":
		return execLogs(args)
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
	projectPath := strArg(args, "path")
	if projectPath == "" {
		return toolErr("path is required"), nil
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
	if !isKnownService(name) {
		return toolErr("unknown service: " + name + ". Valid: " + strings.Join(knownServices, ", ")), nil
	}

	unitName := "lerd-" + name
	content, err := podman.GetQuadletTemplate(unitName + ".container")
	if err != nil {
		return toolErr("no quadlet template for " + name + ": " + err.Error()), nil
	}
	if err := podman.WriteQuadlet(unitName, content); err != nil {
		return toolErr("writing quadlet: " + err.Error()), nil
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
	if !isKnownService(name) {
		return toolErr("unknown service: " + name + ". Valid: " + strings.Join(knownServices, ", ")), nil
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
