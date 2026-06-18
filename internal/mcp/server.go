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
	"github.com/geodro/lerd/internal/composer"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dns"
	"github.com/geodro/lerd/internal/envfile"
	gitpkg "github.com/geodro/lerd/internal/git"
	"github.com/geodro/lerd/internal/logsource"
	"github.com/geodro/lerd/internal/nginx"
	lerdNode "github.com/geodro/lerd/internal/node"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/serviceops"
	"github.com/geodro/lerd/internal/siteinfo"
	"github.com/geodro/lerd/internal/siteops"
	"github.com/geodro/lerd/internal/store"
	lerdSystemd "github.com/geodro/lerd/internal/systemd"
	lerdUpdate "github.com/geodro/lerd/internal/update"
	"github.com/geodro/lerd/internal/version"
	"github.com/geodro/lerd/internal/workerheal"
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

// ToolNames returns the names of every registered MCP tool. Exported so
// documentation drift tests in other packages can assert each tool is covered
// by the injected skill and guidelines files.
func ToolNames() []string {
	tools := toolList()
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	return names
}

// ---- Tool dispatch ----

type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
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

// toolJSON renders a structured value as indented JSON inside a text content
// block. Tool results must carry a "content" array for the host to display
// anything; returning a bare map produces a silent "no output" response.
func toolJSON(v any) map[string]any {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return toolErr("marshal result: " + err.Error())
	}
	return toolOK(string(data))
}

func stripANSI(s string) string {
	return logsource.StripANSI(s)
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

// resolveNginxDomain turns the site/branch args into the domain whose custom
// nginx override to operate on. site defaults to the current context;
// branch resolves to that worktree's subdomain like the daemon does.
func resolveNginxDomain(args map[string]any) (string, error) {
	siteName := strArg(args, "site")
	var site *config.Site
	var err error
	if siteName != "" {
		if site, err = config.FindSite(siteName); err != nil {
			return "", fmt.Errorf("site not found: %s", siteName)
		}
	} else if defaultSitePath != "" {
		if site, err = config.FindSiteByPath(defaultSitePath); err != nil {
			return "", fmt.Errorf("no site for the current path; pass site=")
		}
	} else {
		return "", fmt.Errorf("site is required")
	}
	return siteops.WorktreeDomain(site, strArg(args, "branch"))
}

func execSiteNginxRead(args map[string]any) (any, *rpcError) {
	domain, err := resolveNginxDomain(args)
	if err != nil {
		return toolErr(err.Error()), nil
	}
	got, err := siteops.ReadCustomNginx(domain)
	if err != nil {
		return toolErr(err.Error()), nil
	}
	state := "saved override"
	if !got.Exists {
		state = "no override yet — showing the template"
	}
	return toolOK(fmt.Sprintf("# %s (%s)\n%s", domain, state, got.Body)), nil
}

func execSiteNginxWrite(args map[string]any) (any, *rpcError) {
	domain, err := resolveNginxDomain(args)
	if err != nil {
		return toolErr(err.Error()), nil
	}
	res, err := siteops.SaveCustomNginx(domain, strArg(args, "content"), true)
	if err != nil {
		return toolErr(err.Error()), nil
	}
	if !res.OK {
		msg := res.Error
		if res.ValidationOutput != "" {
			msg += "\n" + res.ValidationOutput
		}
		return toolErr(msg), nil
	}
	return toolOK(fmt.Sprintf("Saved nginx override for %s and reloaded nginx.", domain)), nil
}

func execSiteNginxReset(args map[string]any) (any, *rpcError) {
	domain, err := resolveNginxDomain(args)
	if err != nil {
		return toolErr(err.Error()), nil
	}
	if err := siteops.ResetCustomNginx(domain); err != nil {
		return toolErr(err.Error()), nil
	}
	return toolOK(fmt.Sprintf("Reset %s to the bundled nginx defaults.", domain)), nil
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
	// The queue name is interpolated into the worker unit's ExecStart line;
	// whitespace would add stray artisan arguments and a newline would inject a
	// systemd directive, so reject both.
	if strings.ContainsAny(queue, " \t\r\n") {
		return toolErr("invalid queue name: must not contain whitespace"), nil
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
	if apiKey != "" && strings.ContainsAny(apiKey, " \t\r\n") {
		// Interpolated into the listener unit's ExecStart line alongside
		// --forward-to; whitespace/newline would inject a stripe-cli argument
		// or a systemd directive, the same vector the webhook path guards.
		return toolErr("invalid api_key: must not contain whitespace"), nil
	}
	if apiKey == "" {
		_, apiKey = config.ResolveStripeSecret(site.Path)
	}
	if apiKey == "" {
		return toolErr("Stripe API key required: pass api_key or set one of " +
			strings.Join(config.StripeSecretEnvCandidates, ", ") + " in the site's .env"), nil
	}
	webhookPath := strArg(args, "webhook_path")
	if webhookPath == "" {
		webhookPath = config.StripeWebhookPath(site.Path)
	} else {
		// Validate as the CLI does: an unchecked path interpolates straight
		// into the unit's ExecStart, where a space adds a stripe-cli argument
		// and a newline ends the systemd directive.
		validated, vErr := config.ValidateStripeWebhookPath(webhookPath)
		if vErr != nil {
			return toolErr(vErr.Error()), nil
		}
		webhookPath = validated
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

func execStripeConfig(args map[string]any) (any, *rpcError) {
	siteName := strArg(args, "site")
	if siteName == "" {
		return toolErr("site is required"), nil
	}
	site, err := config.FindSite(siteName)
	if err != nil {
		return toolErr("site not found: " + siteName), nil
	}
	webhookPath := strArg(args, "webhook_path")
	secretEnvKey := strArg(args, "secret_env_key")

	// No fields: report current settings instead of writing.
	if webhookPath == "" && secretEnvKey == "" {
		key, _ := config.ResolveStripeSecret(site.Path)
		if key == "" {
			key = "(none found; looked for " + strings.Join(config.StripeSecretEnvCandidates, ", ") + ")"
		}
		return toolOK(fmt.Sprintf("Stripe config for %s\nWebhook path: %s\nSecret env key: %s",
			siteName, config.StripeWebhookPath(site.Path), key)), nil
	}

	if err := config.SetProjectStripe(site.Path, webhookPath, secretEnvKey); err != nil {
		return toolErr("saving stripe config: " + err.Error()), nil
	}
	// Re-forward a running listener to the new route by rewriting + restarting
	// its unit; the start path reads the freshly saved path from .lerd.yaml.
	if lerdSystemd.IsServiceActive("lerd-stripe-" + siteName) {
		return execStripeListen(args)
	}
	return toolOK(fmt.Sprintf("Updated Stripe config for %s\nWebhook path: %s",
		siteName, config.StripeWebhookPath(site.Path))), nil
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

	cmdArgs := []string{"exec", "-w", projectPath, "--env", composer.ProcessTimeoutEnv(), container, "composer"}
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

	// Node.js versions via fnm. Goes through the shared internal/node
	// helper so the MCP and the web UI (/api/node-versions) return the
	// same shape: major-only deduped majors like "20", "18".
	defaultNode := ""
	if cfg != nil {
		defaultNode = cfg.Node.DefaultVersion
	}
	nodeVersions := lerdNode.ListInstalled()

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

	// Request timeout
	if cfg.RequestTimeout != 0 {
		if cfg.RequestTimeout < 0 {
			add("request_timeout", "fail", fmt.Sprintf("%d — must be a positive number of seconds", cfg.RequestTimeout))
		} else {
			add("request_timeout", "ok", fmt.Sprintf("%ds", cfg.RequestTimeout))
		}
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
			} else if !serviceops.ServiceInstalled(svc.Name) {
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
		if serviceops.ServiceInstalled(svc.Name) {
			add("service_"+svc.Name, "ok", "custom")
		} else {
			add("service_"+svc.Name, "fail", fmt.Sprintf("not installed — run `lerd service preset install %s` (if it's a bundled preset) or `lerd service add --name %s ...`", svc.Name, svc.Name))
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
		} else if serviceops.ServiceInstalled(cfg.DB.Service) {
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
	if serviceops.ServiceInstalled(name) {
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
		Init:        boolArg(args, "init"),
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
	removeData := boolArg(args, "remove_data")

	if err := serviceops.RemoveService(name, serviceops.RemoveOptions{RemoveData: removeData}, nil); err != nil {
		return toolErr(err.Error()), nil
	}

	if removeData {
		return toolOK(fmt.Sprintf("Service %q removed. Data renamed aside as %s.pre-remove-<ts>.", name, config.DataSubDir(name))), nil
	}
	return toolOK(fmt.Sprintf("Service %q removed. Persistent data was NOT deleted.", name)), nil
}

func execServiceReinstall(args map[string]any) (any, *rpcError) {
	name := strArg(args, "name")
	if name == "" {
		return toolErr("name is required"), nil
	}
	resetData := boolArg(args, "reset_data")
	if err := serviceops.ReinstallService(name, resetData, nil); err != nil {
		return toolErr(err.Error()), nil
	}
	if resetData {
		return toolOK(fmt.Sprintf("Service %q reinstalled with fresh data; linked sites' DBs/buckets were recreated.", name)), nil
	}
	return toolOK(fmt.Sprintf("Service %q reinstalled (data preserved).", name)), nil
}

// resolveTunableService returns the resolved service definition + the
// in-container mount target, or a typed MCP error mapping the sentinel
// errors from serviceops. Used as the shared preamble of every
// service_config action so install / family checks stay in sync with
// the HTTP handlers.
func resolveTunableService(name string) (*config.CustomService, string, map[string]any) {
	if name == "" {
		return nil, "", toolErr("name is required")
	}
	if !serviceops.ServiceInstalled(name) {
		return nil, "", toolErr(fmt.Sprintf("service %q is not installed; run `lerd service preset install %s` first", name, name))
	}
	svc, err := config.ResolveServiceForTuning(name)
	if err != nil {
		return nil, "", toolErr(fmt.Sprintf("service %q: %v", name, err))
	}
	target, ok := config.ServiceTuningMount(svc)
	if !ok {
		return nil, "", toolErr(fmt.Sprintf("service %q does not support tuning (family %q). Built-in tunable families: %s. Custom services can opt in via a `tuning:` block in the service YAML.", name, config.FamilyOf(svc), strings.Join(config.TuningFamilies(), ", ")))
	}
	return svc, target, nil
}

func execServiceConfigRead(args map[string]any) (any, *rpcError) {
	name := strArg(args, "name")
	svc, target, errResp := resolveTunableService(name)
	if errResp != nil {
		return errResp, nil
	}
	if err := config.MaterializeServiceTuning(svc); err != nil {
		return toolErr("creating tuning file: " + err.Error()), nil
	}
	body, err := os.ReadFile(config.ServiceTuningFile(name))
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return toolErr("reading tuning file: " + err.Error()), nil
	}
	backups, _ := serviceops.ListTuningBackups(name)
	backupNames := make([]string, 0, len(backups))
	for _, b := range backups {
		backupNames = append(backupNames, b.Name)
	}
	payload, _ := json.MarshalIndent(map[string]any{
		"name":    name,
		"target":  target,
		"path":    config.ServiceTuningFile(name),
		"exists":  exists,
		"content": string(body),
		"backups": backupNames,
	}, "", "  ")
	return toolOK(string(payload)), nil
}

func execServiceConfigWrite(args map[string]any) (any, *rpcError) {
	name := strArg(args, "name")
	content := strArg(args, "content")
	backup := boolArg(args, "backup")
	if _, _, errResp := resolveTunableService(name); errResp != nil {
		return errResp, nil
	}
	res, err := serviceops.SaveTuningOverride(name, content, backup)
	if err != nil {
		// The save may have auto-rolled back; surface that distinctly so
		// callers know the running service is back on its prior bytes
		// rather than wedged on the broken save.
		msg := err.Error()
		if res.RolledBack {
			msg = "save reverted (prior config restored): " + msg
		}
		return toolErr(msg), nil
	}
	out := fmt.Sprintf("Saved tuning override for %q. ", name)
	if res.BackupName != "" {
		out += "Backup staged as " + res.BackupName + ". "
	}
	out += "Service restarted and ready."
	return toolOK(out), nil
}

func execServiceConfigListBackups(args map[string]any) (any, *rpcError) {
	name := strArg(args, "name")
	if _, _, errResp := resolveTunableService(name); errResp != nil {
		return errResp, nil
	}
	backups, err := serviceops.ListTuningBackups(name)
	if err != nil {
		return toolErr("listing backups: " + err.Error()), nil
	}
	if backups == nil {
		backups = []serviceops.TuningBackup{}
	}
	payload, _ := json.MarshalIndent(map[string]any{
		"name":    name,
		"backups": backups,
	}, "", "  ")
	return toolOK(string(payload)), nil
}

func execServiceConfigRestore(args map[string]any) (any, *rpcError) {
	name := strArg(args, "name")
	backupName := strArg(args, "backup_name")
	if _, _, errResp := resolveTunableService(name); errResp != nil {
		return errResp, nil
	}
	// Resolve "newest" server-side so callers that omit backup_name still
	// hit the same code path as the HTTP handler, and the response can
	// report exactly which backup was consumed.
	if backupName == "" {
		list, err := serviceops.ListTuningBackups(name)
		if err != nil {
			return toolErr("listing backups: " + err.Error()), nil
		}
		if len(list) == 0 {
			return toolErr("no backup available for " + name), nil
		}
		backupName = list[0].Name
	}
	res, err := serviceops.RestoreTuningFromBackup(name, backupName)
	if err != nil {
		msg := err.Error()
		if res.RolledBack {
			msg = "restore reverted (prior config restored): " + msg
		}
		return toolErr(msg), nil
	}
	return toolOK(fmt.Sprintf("Restored %q from %s. Service restarted and ready.", name, backupName)), nil
}

func execServiceConfigReset(args map[string]any) (any, *rpcError) {
	name := strArg(args, "name")
	if _, _, errResp := resolveTunableService(name); errResp != nil {
		return errResp, nil
	}
	res, err := serviceops.ResetTuningOverride(name)
	if err != nil {
		msg := err.Error()
		if res.RolledBack {
			msg = "reset reverted (prior config restored): " + msg
		}
		return toolErr(msg), nil
	}
	out := fmt.Sprintf("Reset %q to the bundled template; service restarted.", name)
	if res.AutoBackupName != "" {
		out += " Prior config staged as " + res.AutoBackupName + " (use action=restore to recover)."
	}
	return toolOK(out), nil
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
			if serviceops.ServiceInstalled(p.Name) {
				e.Installed = true
			}
		} else {
			anyInstalled := false
			for _, v := range p.Versions {
				vi := versionEntry{
					Tag:       v.Tag,
					Label:     v.Label,
					Image:     v.Image,
					Installed: serviceops.ServiceInstalled(config.PresetVersionServiceName(p.Name, v)),
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
	return toolJSON(map[string]any{"services": results}), nil
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
	target, err := serviceops.ResolveMigrateTarget(name, avail.CurrentImage, tag)
	if err != nil {
		return toolErr(err.Error()), nil
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
		return toolJSON(map[string]any{"service": name, "vars": vars}), nil
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
	return toolJSON(map[string]any{"service": name, "vars": vars}), nil
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

// execEnvOverride scaffolds/seeds the personal .env.lerd_override file by
// shelling out to `lerd env:override` (same path as the CLI), then returns the
// resulting file contents so the agent can see the current overrides.
func execEnvOverride(args map[string]any) (any, *rpcError) {
	projectPath := resolvedPath(args)
	if projectPath == "" {
		return toolErr("path is required — pass a path argument or open Claude in the project directory"), nil
	}

	self, err := os.Executable()
	if err != nil {
		return toolErr("could not resolve lerd executable: " + err.Error()), nil
	}

	cmdArgs := append([]string{"env:override"}, strSliceArg(args, "set")...)
	var out bytes.Buffer
	cmd := exec.Command(self, cmdArgs...)
	cmd.Dir = projectPath
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return toolErr(fmt.Sprintf("env:override failed (%v):\n%s", err, stripANSI(out.String()))), nil
	}

	body, _ := os.ReadFile(filepath.Join(projectPath, ".env.lerd_override"))
	msg := stripANSI(strings.TrimSpace(out.String()))
	if len(body) > 0 {
		msg += "\n\n--- .env.lerd_override ---\n" + strings.TrimSpace(string(body))
	}
	return toolOK(msg), nil
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
	if choice == "" {
		return toolErr("database is required — pass sqlite, a built-in (mysql/postgres), or an installed family alternate (e.g. mariadb, postgres-pgvector, mysql-5-7)"), nil
	}
	if !config.IsDBServiceName(choice) {
		return toolErr(fmt.Sprintf("invalid database %q — must be sqlite or a service in the mysql/mariadb/postgres/mongo families (install the preset with `lerd service preset %s` first if needed)", choice, choice)), nil
	}

	// Check existing DB for the summary message.
	previous := ""
	if proj, _ := config.LoadProjectConfig(projectPath); proj != nil {
		for _, svc := range proj.Services {
			if config.IsDBServiceName(svc.Name) {
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

func execDbMove(args map[string]any) (any, *rpcError) {
	from := strings.TrimSpace(strArg(args, "from"))
	to := strings.TrimSpace(strArg(args, "to"))
	if from == "" || to == "" {
		return toolErr("from and to are required (e.g. from=postgres, to=postgres-18)"), nil
	}
	sites := strSliceArg(args, "sites")
	all := boolArg(args, "all")
	if !all && len(sites) == 0 {
		return toolErr("pass sites (a list of site names) or all=true"), nil
	}

	// Re-exec the CLI so the move runs through the same code path as
	// `lerd db:move`. --force skips the prompt; output is captured rather than
	// written to the MCP stdio channel.
	self, err := os.Executable()
	if err != nil {
		return toolErr("could not resolve lerd executable: " + err.Error()), nil
	}
	cmdArgs := []string{"db:move", "--from", from, "--to", to, "--force"}
	if all {
		cmdArgs = append(cmdArgs, "--all")
	}
	for _, s := range sites {
		cmdArgs = append(cmdArgs, "--site", s)
	}
	var out bytes.Buffer
	cmd := exec.Command(self, cmdArgs...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return toolErr(fmt.Sprintf("db_move failed (%v):\n%s", err, strings.TrimSpace(out.String()))), nil
	}
	return toolOK(strings.TrimSpace(out.String())), nil
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
		secured := siteops.ResolveSecured(siteops.CleanupRelink(projectPath, name), proj, cfg)
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

	secured := siteops.ResolveSecured(siteops.CleanupRelink(projectPath, name), proj, cfg)
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
		_ = certs.ReissueCertForWorktree(*site)
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

	_ = config.ReplaceProjectDomain(site.Path, site.Domains, fullDomain, cfg.DNS.TLD)

	if err := siteops.RegenerateSiteVhost(site, oldPrimary); err != nil {
		return toolErr("regenerating vhost: " + err.Error()), nil
	}

	if site.Secured {
		_ = certs.ReissueCertForWorktree(*site)
	}

	_ = podman.WriteContainerHosts()
	_ = nginx.Reload()

	if site.PrimaryDomain() != oldPrimary {
		_ = envfile.SyncPrimaryDomain(site.Path, site.PrimaryDomain(), site.Secured)
	}

	return toolOK(fmt.Sprintf("Removed domain %s from site %s", fullDomain, site.Name)), nil
}

func execSecure(args map[string]any) (any, *rpcError) {
	return execToggleSecure(args, true)
}

func execUnsecure(args map[string]any) (any, *rpcError) {
	return execToggleSecure(args, false)
}

// execToggleSecure is the MCP entry-point shared by site_secure / site_unsecure.
// It funnels through siteops.SetSecured, the single source of truth shared
// with CLI and UI. All post-toggle work (Stripe restart, LAN share refresh)
// lives inside SetSecured so MCP, CLI, and UI all do exactly the same thing.
func execToggleSecure(args map[string]any, secured bool) (any, *rpcError) {
	siteName := strArg(args, "site")
	if siteName == "" {
		return toolErr("site is required"), nil
	}
	site, err := config.FindSite(siteName)
	if err != nil {
		return toolErr(fmt.Sprintf("site %q not found", siteName)), nil
	}
	if err := siteops.SetSecured(site, secured); err != nil {
		return toolErr(err.Error()), nil
	}
	scheme := "http"
	state := "Unsecured"
	if secured {
		scheme = "https"
		state = "Secured"
	}
	return toolOK(fmt.Sprintf("%s: %s://%s", state, scheme, site.PrimaryDomain())), nil
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

// resolveWorkerCwd picks the cwd to shell `lerd worker` into: site.Path for
// parent, worktree path when branch is set. The CLI's workerNames helper
// keys off cwd to pick parent vs per-worktree unit names.
func resolveWorkerCwd(site *config.Site, branch string) (string, map[string]any) {
	if branch == "" {
		return site.Path, nil
	}
	sanitized := gitpkg.SanitizeBranch(branch)
	wtPath := worktreePathFor(site, sanitized)
	if wtPath == "" {
		return "", toolErr(fmt.Sprintf("worktree branch %q not found on site %q", branch, site.Name))
	}
	return wtPath, nil
}

func execCommandsList(args map[string]any) (any, *rpcError) {
	siteName := strArg(args, "site")
	if siteName == "" {
		return toolErr("site is required"), nil
	}
	site, err := config.FindSite(siteName)
	if err != nil {
		return toolErr("site not found: " + siteName), nil
	}
	proj, _ := config.LoadProjectConfig(site.Path)
	var fw *config.Framework
	if site.Framework != "" {
		fw, _ = config.GetFrameworkForDir(site.Framework, site.Path)
	}
	cmds := config.ResolveCommands(fw, proj, site.Path)
	if len(cmds) == 0 {
		return toolOK("(no commands defined for this site)"), nil
	}
	var b strings.Builder
	for _, c := range cmds {
		marker := " "
		if c.Confirm {
			marker = "*"
		}
		desc := c.Description
		if desc == "" {
			desc = c.Label
		}
		fmt.Fprintf(&b, "  %s %-30s  %s\n", marker, c.Name, desc)
	}
	b.WriteString("\n  * = asks for confirmation. Pass force: true to commands_run.\n")
	return toolOK(b.String()), nil
}

func execCommandsRun(args map[string]any) (any, *rpcError) {
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
	proj, _ := config.LoadProjectConfig(site.Path)
	var fw *config.Framework
	if site.Framework != "" {
		fw, _ = config.GetFrameworkForDir(site.Framework, site.Path)
	}
	cmds := config.ResolveCommands(fw, proj, site.Path)
	var target *config.FrameworkCommand
	for i := range cmds {
		if cmds[i].Name == name {
			target = &cmds[i]
			break
		}
	}
	if target == nil {
		return toolErr(fmt.Sprintf("command %q not found. Run commands_list(site: %q) to see available names", name, siteName)), nil
	}
	if target.Command == "" {
		return toolErr(fmt.Sprintf("command %q has no shell invocation", name)), nil
	}
	if target.Confirm {
		force, _ := args["force"].(bool)
		if !force {
			return toolErr(fmt.Sprintf("command %q is destructive (confirm: true). Re-run with force: true to confirm. Will execute: %s", name, target.Command)), nil
		}
	}
	if target.Output == config.CommandOutputTerminal {
		return toolErr(fmt.Sprintf("command %q has output: terminal — only runnable from the dashboard or `lerd run`, not via MCP", name)), nil
	}
	cwd := site.Path
	if target.CWD != "" && target.CWD != "." {
		cwd = filepath.Join(site.Path, target.CWD)
	}
	cmd := exec.Command("sh", "-c", target.Command)
	cmd.Dir = cwd
	out, runErr := cmd.CombinedOutput()
	body := string(out)
	if runErr != nil {
		if ee, ok := runErr.(*exec.ExitError); ok {
			return toolErr(fmt.Sprintf("exit %d:\n%s", ee.ExitCode(), body)), nil
		}
		return toolErr(fmt.Sprintf("run failed: %v\n%s", runErr, body)), nil
	}
	return toolOK(body), nil
}

func execCommandAdd(args map[string]any) (any, *rpcError) {
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

	disabled := boolArg(args, "disabled")
	command := strArg(args, "command")
	if !disabled && command == "" {
		return toolErr("command is required (or set disabled: true to suppress a framework default)"), nil
	}

	cmd := config.FrameworkCommand{
		Name:        name,
		Label:       strArg(args, "label"),
		Command:     command,
		Description: strArg(args, "description"),
		Output:      strArg(args, "output"),
		Confirm:     boolArg(args, "confirm"),
		Icon:        strArg(args, "icon"),
		CWD:         strArg(args, "cwd"),
		Disabled:    disabled,
	}
	if checkFile, checkComposer := strArg(args, "check_file"), strArg(args, "check_composer"); checkFile != "" || checkComposer != "" {
		cmd.Check = &config.FrameworkRule{File: checkFile, Composer: checkComposer}
	}

	// Detect whether this name overwrites an existing project entry vs replacing
	// a framework default vs being net-new, to give the agent a useful response.
	proj, _ := config.LoadProjectConfig(site.Path)
	action := "added"
	for _, c := range proj.Commands {
		if c.Name == name {
			action = "updated"
			break
		}
	}

	if err := config.SetProjectCommand(site.Path, cmd); err != nil {
		return toolErr("saving .lerd.yaml: " + err.Error()), nil
	}
	hint := ""
	if disabled {
		hint = " (suppresses framework default)"
	}
	return toolOK(fmt.Sprintf("Command %q %s in .lerd.yaml%s. Run it via commands_run(site: %q, name: %q) or `lerd run %s`.", name, action, hint, siteName, name, name)), nil
}

func execCommandRemove(args map[string]any) (any, *rpcError) {
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
	if err := config.RemoveProjectCommand(site.Path, name); err != nil {
		if _, ok := err.(*config.CommandNotFoundError); ok {
			return toolErr(fmt.Sprintf("command %q not found in .lerd.yaml for site %q", name, siteName)), nil
		}
		return toolErr("saving .lerd.yaml: " + err.Error()), nil
	}
	return toolOK(fmt.Sprintf("Command %q removed from .lerd.yaml.", name)), nil
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
	cwd, errResp := resolveWorkerCwd(site, strArg(args, "branch"))
	if errResp != nil {
		return errResp, nil
	}
	out, err := runIn(cwd, "lerd", "worker", "start", workerName)
	if err != nil {
		msg := strings.TrimSpace(out)
		if msg == "" {
			msg = err.Error()
		}
		return toolErr(fmt.Sprintf("starting worker %q: %s", workerName, msg)), nil
	}
	return toolOK(out), nil
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
	site, err := config.FindSite(siteName)
	if err != nil {
		return toolErr("site not found: " + siteName), nil
	}
	cwd, errResp := resolveWorkerCwd(site, strArg(args, "branch"))
	if errResp != nil {
		return errResp, nil
	}
	out, err := runIn(cwd, "lerd", "worker", "stop", workerName)
	if err != nil {
		msg := strings.TrimSpace(out)
		if msg == "" {
			msg = err.Error()
		}
		return toolErr(fmt.Sprintf("stopping worker %q: %s", workerName, msg)), nil
	}
	return toolOK(out), nil
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

	branchArg := strArg(args, "branch")
	var unitSuffix string
	if branchArg != "" {
		sanitized := gitpkg.SanitizeBranch(branchArg)
		if worktreePathFor(site, sanitized) == "" {
			return toolErr(fmt.Sprintf("worktree branch %q not found on site %q", branchArg, siteName)), nil
		}
		unitSuffix = "-" + sanitized
	}

	type workerInfo struct {
		Name          string `json:"name"`
		Label         string `json:"label"`
		Command       string `json:"command"`
		Restart       string `json:"restart"`
		Running       bool   `json:"running"`
		Unit          string `json:"unit"`
		Branch        string `json:"branch,omitempty"`
		Host          bool   `json:"host,omitempty"`
		PerWorktree   bool   `json:"per_worktree,omitempty"`
		ReplacesBuild bool   `json:"replaces_build,omitempty"`
		Orphaned      bool   `json:"orphaned,omitempty"`
	}

	known := make(map[string]bool, len(fw.Workers))
	var result []workerInfo
	for wname, w := range fw.Workers {
		known[wname] = true
		unitName := "lerd-" + wname + "-" + siteName + unitSuffix
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
			Name:          wname,
			Label:         label,
			Command:       w.Command,
			Restart:       restart,
			Running:       status == "active",
			Unit:          unitName,
			Branch:        branchArg,
			Host:          w.Host,
			PerWorktree:   w.IsPerWorktree(),
			ReplacesBuild: w.ReplacesBuild,
		})
	}

	// Skip orphan detection when scoped to a worktree — FindOrphanedWorkers
	// walks the parent site only.
	if branchArg == "" {
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
	}

	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })

	data, _ := json.MarshalIndent(result, "", "  ")
	return toolOK(string(data)), nil
}

// execWorkersHealth returns every worker unit currently in the systemd
// "failed" state, grouped per site. Read-only counterpart to workers_heal.
func execWorkersHealth() (any, *rpcError) {
	unhealthy, err := workerheal.Detect()
	if err != nil {
		return toolErr("detecting workers: " + err.Error()), nil
	}
	if unhealthy == nil {
		unhealthy = []workerheal.UnhealthyWorker{}
	}
	data, _ := json.MarshalIndent(unhealthy, "", "  ")
	return toolOK(string(data)), nil
}

// execWorkersHeal heals every failed worker (or the named one if `unit` is
// passed). Returns a per-unit summary so the agent can report what was
// fixed without re-querying. Mirrors `lerd worker heal` exactly: no
// .lerd.yaml writes, no unit-file rewrites.
func execWorkersHeal(args map[string]any) (any, *rpcError) {
	if unit := strArg(args, "unit"); unit != "" {
		if err := workerheal.HealUnit(unit); err != nil {
			return toolErr("heal " + unit + ": " + err.Error()), nil
		}
		return toolOK("Healed " + unit + "."), nil
	}
	report, err := workerheal.HealAll(nil)
	if err != nil {
		return toolErr("heal: " + err.Error()), nil
	}
	out := map[string]any{
		"summary": workerheal.Summary(report),
		"healed":  report.Healed,
		"failed":  report.Failed,
	}
	data, _ := json.MarshalIndent(out, "", "  ")
	return toolOK(string(data)), nil
}

// execWorkersMode shells out to `lerd workers mode` so the same migration
// path that restarts active workers in their new shape on macOS runs from
// MCP too. Linux is a no-op at the CLI layer.
func execWorkersMode(args map[string]any) (any, *rpcError) {
	action := strArg(args, "action")
	switch action {
	case "get":
		out, err := runIn("", "lerd", "workers", "mode")
		if err != nil {
			msg := strings.TrimSpace(out)
			if msg == "" {
				msg = err.Error()
			}
			return toolErr("workers mode: " + msg), nil
		}
		return toolOK(out), nil
	case "set":
		mode := strArg(args, "mode")
		if mode != "exec" && mode != "container" {
			return toolErr("mode must be exec or container"), nil
		}
		out, err := runIn("", "lerd", "workers", "mode", mode)
		if err != nil {
			msg := strings.TrimSpace(out)
			if msg == "" {
				msg = err.Error()
			}
			return toolErr("workers mode " + mode + ": " + msg), nil
		}
		return toolOK(out), nil
	default:
		return toolErr("action must be get or set"), nil
	}
}

// execBugReport shells out to `lerd bug-report` rather than re-implementing
// the doctor-output / config-collection logic. Returns the file path so the
// agent can read or upload it; flags map 1:1 onto the CLI.
func execBugReport(args map[string]any) (any, *rpcError) {
	cmd := []string{"bug-report"}
	if out := strArg(args, "output"); out != "" {
		cmd = append(cmd, "--output", out)
	}
	if v, ok := args["log_lines"]; ok {
		switch n := v.(type) {
		case float64:
			cmd = append(cmd, "--log-lines", fmt.Sprintf("%d", int(n)))
		case int:
			cmd = append(cmd, "--log-lines", fmt.Sprintf("%d", n))
		}
	}
	if v, ok := args["show_real_names"].(bool); ok && v {
		cmd = append(cmd, "--show-real-names")
	}
	output, err := runIn("", "lerd", cmd...)
	if err != nil {
		msg := strings.TrimSpace(output)
		if msg == "" {
			msg = err.Error()
		}
		return toolErr("bug-report: " + msg), nil
	}
	return toolOK(output), nil
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
	cmd := podman.Cmd("exec", "-w", projectPath, "--env", composer.ProcessTimeoutEnv(), container, "composer", "install", "--no-interaction")
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
	if !config.IsSupportedPHPVersion(version) {
		return toolErr(fmt.Sprintf("unsupported PHP version %q — supported: %s", version, strings.Join(config.SupportedPHPVersions, ", "))), nil
	}

	site, err := config.FindSite(siteName)
	if err != nil {
		return toolErr(fmt.Sprintf("site %q not found — run sites to list registered sites", siteName)), nil
	}
	if site.IsCustomContainer() {
		return toolErr("custom container sites do not use PHP versions — the container defines its own runtime"), nil
	}
	if site.IsHostProxy() {
		return toolErr("host-proxy sites do not use PHP versions — they run your dev command on the host"), nil
	}

	if branch := strArg(args, "branch"); branch != "" {
		cwd, errResp := resolveWorkerCwd(site, branch)
		if errResp != nil {
			return errResp, nil
		}
		out, runErr := runIn(cwd, "lerd", "isolate", version)
		if runErr != nil {
			msg := strings.TrimSpace(out)
			if msg == "" {
				msg = runErr.Error()
			}
			return toolErr(fmt.Sprintf("isolate PHP %s on %s: %s", version, branch, msg)), nil
		}
		return toolOK(out), nil
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
		// FrankenPHP only publishes images for PHP >= 8.2; building below that
		// normalizes the version up and silently runs a different PHP than the
		// site reports. Match the CLI and fall back to FPM rather than upgrade.
		if !config.IsFrankenPHPVersion(version) {
			if err := siteops.DemoteFrankenPHPToFPM(site); err != nil {
				return toolErr(err.Error()), nil
			}
			return toolOK(fmt.Sprintf("PHP version for %s set to %s; FrankenPHP has no image for it, so the site was switched to FPM.", siteName, version)), nil
		}
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

	if branch := strArg(args, "branch"); branch != "" {
		cwd, errResp := resolveWorkerCwd(site, branch)
		if errResp != nil {
			return errResp, nil
		}
		out, runErr := runIn(cwd, "lerd", "isolate:node", version)
		if runErr != nil {
			msg := strings.TrimSpace(out)
			if msg == "" {
				msg = runErr.Error()
			}
			return toolErr(fmt.Sprintf("isolate Node %s on %s: %s", version, branch, msg)), nil
		}
		return toolOK(out), nil
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
	if site.IsHostProxy() {
		return toolErr("host-proxy sites run your dev command on the host, not a PHP runtime"), nil
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

	// FrankenPHP only publishes images for PHP >= 8.2; without this guard the
	// build normalizes the version up (e.g. 8.1 -> 8.5) and silently runs a
	// different PHP than the site reports. Mirror the `lerd runtime` guard.
	if !config.IsFrankenPHPVersion(site.PHPVersion) {
		return toolErr(fmt.Sprintf("FrankenPHP requires PHP %s or newer; this site is on PHP %s — bump it first.",
			config.FrankenPHPMinVersion, site.PHPVersion)), nil
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

// mcpSnapshotTarget resolves a snapshot target from MCP args. It honours an
// explicit service override, then the project's .lerd.yaml db block (what
// db_set persists), then falls back to .env — the same priority the CLI uses.
func mcpSnapshotTarget(args map[string]any) (serviceops.SnapshotTarget, error) {
	all := boolArg(args, "all_databases")
	dbOverride := strArg(args, "database")
	build := func(service, database string) serviceops.SnapshotTarget {
		family := config.FamilyOfName(service)
		if family == "" {
			family = service
		}
		if dbOverride != "" {
			database = dbOverride
		}
		return serviceops.SnapshotTarget{Service: service, Family: family, Database: database, AllDatabases: all}
	}

	if svc := strArg(args, "service"); svc != "" {
		return build(svc, ""), nil
	}
	projectPath := resolvedPath(args)
	if projectPath == "" {
		return serviceops.SnapshotTarget{}, fmt.Errorf("pass a path argument (project root) or a service argument")
	}
	if pc, err := config.LoadProjectConfig(projectPath); err == nil && pc.DB.Service != "" {
		return build(pc.DB.Service, pc.DB.Database), nil
	}
	env, err := readDBEnvLenient(projectPath)
	if err != nil || env == nil {
		return serviceops.SnapshotTarget{}, fmt.Errorf("no .lerd.yaml db block or .env found in %s — pass a service argument", projectPath)
	}
	service := "mysql"
	switch strings.ToLower(env.connection) {
	case "pgsql", "postgres", "postgresql":
		service = "postgres"
	}
	return build(service, env.database), nil
}

func execDBSnapshot(args map[string]any) (any, *rpcError) {
	target, err := mcpSnapshotTarget(args)
	if err != nil {
		return toolErr(err.Error()), nil
	}
	if !serviceops.SnapshotFamilySupported(target.Family) {
		return toolErr("snapshots support only MySQL, MariaDB and PostgreSQL"), nil
	}
	if !target.AllDatabases && target.Database == "" {
		return toolErr("database is required — pass database, or all_databases:true"), nil
	}
	snap, err := serviceops.CreateSnapshot(target, strArg(args, "name"), serviceops.SnapshotMeta{}, nil)
	if err != nil {
		return toolErr(err.Error()), nil
	}
	scope := snap.Database
	if snap.AllDatabases {
		scope = "all databases"
	}
	return toolOK(fmt.Sprintf("Created snapshot %q for %s (%d bytes) on %s", snap.Name, scope, snap.SizeBytes, snap.Service)), nil
}

func execDBSnapshots(args map[string]any) (any, *rpcError) {
	target, err := mcpSnapshotTarget(args)
	if err != nil {
		return toolErr(err.Error()), nil
	}
	database := target.Database
	if boolArg(args, "all") {
		database = ""
	}
	snaps, err := serviceops.ListSnapshots(target.Service, database, true)
	if err != nil {
		return toolErr(err.Error()), nil
	}
	data, _ := json.MarshalIndent(snaps, "", "  ")
	return toolOK(string(data)), nil
}

func execDBRestore(args map[string]any) (any, *rpcError) {
	name := strArg(args, "name")
	if name == "" {
		return toolErr("name is required"), nil
	}
	target, err := mcpSnapshotTarget(args)
	if err != nil {
		return toolErr(err.Error()), nil
	}
	if !serviceops.SnapshotFamilySupported(target.Family) {
		return toolErr("snapshots support only MySQL, MariaDB and PostgreSQL"), nil
	}
	if !target.AllDatabases && target.Database == "" {
		return toolErr("database is required — pass database, or all_databases:true"), nil
	}
	if err := serviceops.RestoreSnapshot(target, name, nil); err != nil {
		return toolErr(err.Error()), nil
	}
	return toolOK(fmt.Sprintf("Restored snapshot %q", name)), nil
}

func execDBSnapshotDelete(args map[string]any) (any, *rpcError) {
	name := strArg(args, "name")
	if name == "" {
		return toolErr("name is required"), nil
	}
	target, err := mcpSnapshotTarget(args)
	if err != nil {
		return toolErr(err.Error()), nil
	}
	if err := serviceops.DeleteSnapshot(target.Service, target.Database, name, target.AllDatabases); err != nil {
		return toolErr(err.Error()), nil
	}
	return toolOK(fmt.Sprintf("Deleted snapshot %q", name)), nil
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
			dbName = config.SiteSlug(base)
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

	deps, err := podman.ParseApkDeps(strArg(args, "apk_deps"))
	if err != nil {
		return toolErr(err.Error()), nil
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		return toolErr("loading config: " + err.Error()), nil
	}

	cfg.AddExtension(version, ext)
	if len(deps) > 0 {
		cfg.SetExtApkDeps(ext, deps)
	}
	if err := config.SaveGlobal(cfg); err != nil {
		return toolErr("saving config: " + err.Error()), nil
	}

	var out bytes.Buffer
	if err := podman.RebuildFPMImageTo(version, false, &out); err != nil {
		return toolErr(fmt.Sprintf("rebuilding PHP %s image (%v):\n%s", version, err, out.String())), nil
	}

	if err := podman.VerifyExtensionLoaded(version, ext); err != nil {
		cfg.RemoveExtension(version, ext)
		_ = config.SaveGlobal(cfg)
		return toolErr(fmt.Sprintf("extension %q was not installed for PHP %s (config reverted): %v", ext, version, err)), nil
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
