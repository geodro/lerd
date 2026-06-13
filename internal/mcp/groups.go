package mcp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/geodro/lerd/internal/activityping"
	"github.com/geodro/lerd/internal/config"
)

// recordToolActivity marks the site a tool call targets as active under
// idle-suspend. Global tool calls that name no site are a no-op.
func recordToolActivity(args map[string]any) {
	activityping.Site(siteForToolArgs(args))
}

// siteForToolArgs resolves the site a tool call targets, from the explicit
// `site` (by name) or `path` (by directory) argument. Returns "" when the call
// names no site.
func siteForToolArgs(args map[string]any) string {
	if name := strArg(args, "site"); name != "" {
		return name
	}
	if path := strArg(args, "path"); path != "" {
		if s, err := config.FindSiteByPath(path); err == nil && s != nil {
			return s.Name
		}
	}
	return ""
}

// This file consolidates what used to be ~80 flat MCP tools into eleven
// resource-grouped tools (site, service, db, env, runtime, worker, exec,
// framework, diag, logs, worktree), each selecting behaviour via an `action` enum.
// The grouped manifest is a fraction of the old tools/list payload (cheaper per
// session, sharper model focus) and is now STATIC — framework-specific actions
// like queue/horizon are gated inside their handlers rather than by toggling the
// tool set, so the manifest no longer changes shape mid-session (stable prompt
// cache). Every execXxx handler is reused unchanged; only routing moved here.

// handlerFn is the uniform signature every grouped action routes to. Handlers
// that take no args or extra flags are wrapped in thin closures in dispatch.
type handlerFn func(map[string]any) (any, *rpcError)

func toolList() []mcpTool {
	return []mcpTool{
		siteTool(),
		serviceTool(),
		dbTool(),
		envTool(),
		runtimeTool(),
		workerTool(),
		execTool(),
		frameworkTool(),
		diagTool(),
		logsTool(),
		worktreeTool(),
	}
}

// dispatch maps group name → action → handler. Built once at init from the
// existing execXxx functions. worktree is not here; it keeps its own internal
// action dispatcher (dispatchWorktree) and is routed directly in handleToolCall.
var groupDispatch = map[string]map[string]handlerFn{
	"site": {
		"list":           func(a map[string]any) (any, *rpcError) { return execSites() },
		"link":           execSiteLink,
		"unlink":         execSiteUnlink,
		"domain_add":     execSiteDomainAdd,
		"domain_remove":  execSiteDomainRemove,
		"group_assign":   execSiteGroupAssign,
		"group_unassign": execSiteGroupUnassign,
		"group_label":    execSiteGroupLabel,
		"group_db":       execSiteGroupDB,
		"group_list":     execSiteGroupList,
		"tls_enable":     execSecure,
		"tls_disable":    execUnsecure,
		"php":            execSitePHP,
		"node":           execSiteNode,
		"pause":          execSitePause,
		"unpause":        execSiteUnpause,
		"restart":        execSiteRestart,
		"rebuild":        execSiteRebuild,
		"runtime":        execSiteRuntime,
		"nginx_read":     execSiteNginxRead,
		"nginx_write":    execSiteNginxWrite,
		"nginx_reset":    execSiteNginxReset,
		"park":           execPark,
		"unpark":         execUnpark,
	},
	"service": {
		"start":               execServiceStart,
		"stop":                execServiceStop,
		"restart":             execServiceRestart,
		"pin":                 execServicePin,
		"unpin":               execServiceUnpin,
		"update":              execServiceUpdate,
		"rollback":            execServiceRollback,
		"migrate":             execServiceMigrate,
		"remove":              execServiceRemove,
		"reinstall":           execServiceReinstall,
		"add":                 execServiceAdd,
		"expose":              execServiceExpose,
		"env":                 execServiceEnv,
		"config_read":         execServiceConfigRead,
		"config_write":        execServiceConfigWrite,
		"config_restore":      execServiceConfigRestore,
		"config_reset":        execServiceConfigReset,
		"config_list_backups": execServiceConfigListBackups,
		"preset_list":         func(a map[string]any) (any, *rpcError) { return execServicePresetList(a) },
		"preset_install":      execServicePresetInstall,
		"check_updates":       execServiceCheckUpdates,
	},
	"db": {
		"set":             execDbSet,
		"move":            execDbMove,
		"create":          execDBCreate,
		"export":          execDBExport,
		"import":          execDBImport,
		"snapshot":        execDBSnapshot,
		"snapshots":       execDBSnapshots,
		"restore":         execDBRestore,
		"snapshot_delete": execDBSnapshotDelete,
	},
	"env": {
		"setup":    execEnvSetup,
		"check":    execEnvCheck,
		"override": execEnvOverride,
	},
	"runtime": {
		"versions":       func(a map[string]any) (any, *rpcError) { return execRuntimeVersions() },
		"node_install":   execNodeInstall,
		"node_uninstall": execNodeUninstall,
		"php_list":       func(a map[string]any) (any, *rpcError) { return execPHPList() },
		"ext_list":       execPHPExtList,
		"ext_add":        execPHPExtAdd,
		"ext_remove":     execPHPExtRemove,
	},
	"worker": {
		"start":  execWorkerStart,
		"stop":   execWorkerStop,
		"add":    execWorkerAdd,
		"remove": execWorkerRemove,
		"list":   execWorkerList,
		"health": func(a map[string]any) (any, *rpcError) { return execWorkersHealth() },
		"heal":   execWorkersHeal,
		// execWorkersMode switches on args["action"] (get/set) itself, so translate
		// the group action into the selector it expects.
		"mode_get":       func(a map[string]any) (any, *rpcError) { a["action"] = "get"; return execWorkersMode(a) },
		"mode_set":       func(a map[string]any) (any, *rpcError) { a["action"] = "set"; return execWorkersMode(a) },
		"queue_start":    execQueueStart,
		"queue_stop":     execQueueStop,
		"horizon_start":  execHorizonStart,
		"horizon_stop":   execHorizonStop,
		"reverb_start":   execReverbStart,
		"reverb_stop":    execReverbStop,
		"schedule_start": execScheduleStart,
		"schedule_stop":  execScheduleStop,
		"stripe_start":   execStripeListen,
		"stripe_stop":    execStripeListenStop,
		"stripe_config":  execStripeConfig,
	},
	"exec": {
		"artisan":        execArtisan,
		"console":        execArtisan,
		"composer":       execComposer,
		"vendor_bins":    execVendorBins,
		"vendor_run":     execVendorRun,
		"commands_list":  execCommandsList,
		"commands_run":   execCommandsRun,
		"command_add":    execCommandAdd,
		"command_remove": execCommandRemove,
	},
	"framework": {
		"list":        func(a map[string]any) (any, *rpcError) { return execFrameworkList() },
		"add":         execFrameworkAdd,
		"remove":      execFrameworkRemove,
		"search":      execFrameworkSearch,
		"install":     execFrameworkInstall,
		"project_new": execProjectNew,
		"setup":       execSetup,
	},
	"diag": {
		"status":          func(a map[string]any) (any, *rpcError) { return execStatus() },
		"doctor":          func(a map[string]any) (any, *rpcError) { return execDoctor() },
		"which":           execWhich,
		"check":           execCheck,
		"dns_diagnose":    execDNSDiagnose,
		"bug_report":      execBugReport,
		"analyze_queries": execAnalyzeQueries,
		"dumps_recent":    execDumpsRecent,
		"dumps_status":    execDumpsStatus,
		"dumps_clear":     execDumpsClear,
		"dumps_toggle":    execDumpsToggle,
		"profiler_toggle": execProfilerToggle,
		"profiler_status": execProfilerStatus,
		"profiler_clear":  execProfilerClear,
		"xdebug_on":       func(a map[string]any) (any, *rpcError) { return execXdebugToggle(a, true) },
		"xdebug_off":      func(a map[string]any) (any, *rpcError) { return execXdebugToggle(a, false) },
		"xdebug_status":   func(a map[string]any) (any, *rpcError) { return execXdebugStatus() },
	},
	"logs": {
		"sources": execLogsSources,
		"fetch":   execLogsFetch,
	},
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

	// Working on a site through MCP counts as activity, so idle-suspend keeps it
	// awake (and wakes it) the same as an HTTP request or a CLI command would.
	recordToolActivity(args)

	// worktree keeps its own internal action dispatcher.
	if p.Name == "worktree" {
		return dispatchWorktree(args)
	}

	actions, ok := groupDispatch[p.Name]
	if !ok {
		return toolErr("unknown tool: " + p.Name), nil
	}
	action := strArg(args, "action")
	h, ok := actions[action]
	if !ok {
		return toolErr(fmt.Sprintf("unknown action %q for tool %q; valid actions: %s",
			action, p.Name, strings.Join(sortedActions(actions), ", "))), nil
	}
	return h(args)
}

// sortedActions returns the action keys of a group, sorted, for error messages.
func sortedActions(m map[string]handlerFn) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ---- Grouped tool schemas ----

func siteTool() mcpTool {
	return mcpTool{
		Name:        "site",
		Description: "Manage sites. action: list (discover sites — CALL FIRST), link, unlink, domain_add, domain_remove, group_assign, group_unassign, group_label, group_db, group_list, tls_enable, tls_disable, php, node, pause, unpause, restart, rebuild, runtime, nginx_read, nginx_write, nginx_reset, park, unpark.",
		InputSchema: mcpSchema{
			Type: "object",
			Properties: map[string]mcpProp{
				"action":   {Type: "string", Enum: []string{"list", "link", "unlink", "domain_add", "domain_remove", "group_assign", "group_unassign", "group_label", "group_db", "group_list", "tls_enable", "tls_disable", "php", "node", "pause", "unpause", "restart", "rebuild", "runtime", "nginx_read", "nginx_write", "nginx_reset", "park", "unpark"}},
				"path":     {Type: "string", Description: "Targets the site by directory (link, unlink, domain_*, group_* [the secondary], park, unpark). Defaults to cwd."},
				"site":     {Type: "string", Description: "Targets the site by name from action=list (php, node, tls_*, pause, unpause, restart, rebuild, runtime, nginx_*). group_* use path, not site."},
				"name":     {Type: "string", Description: "link: site name without .test TLD."},
				"domain":   {Type: "string", Description: "domain_add/remove: domain without .test TLD."},
				"main":     {Type: "string", Description: "group_assign: main site name/domain."},
				"label":    {Type: "string", Description: "group_assign/group_label: subdomain label."},
				"share_db": {Type: "boolean", Description: "group_assign: share the main's DB."},
				"db":       {Type: "string", Enum: []string{"share", "separate"}, Description: "group_db."},
				"version":  {Type: "string", Description: "php/node: target version."},
				"branch":   {Type: "string", Description: "Optional worktree branch (php/node/nginx_*)."},
				"content":  {Type: "string", Description: "nginx_write: full nginx config."},
				"runtime":  {Type: "string", Enum: []string{"fpm", "frankenphp"}, Description: "runtime: target runtime."},
				"worker":   {Type: "boolean", Description: "runtime: frankenphp worker mode."},
			},
			Required: []string{"action"},
		},
	}
}

func serviceTool() mcpTool {
	return mcpTool{
		Name:        "service",
		Description: "Manage services (built-in + custom). action: start, stop, restart, pin, unpin, update, rollback, migrate, remove, reinstall, add, expose, env, config_read, config_write, config_restore, config_reset, config_list_backups, preset_list, preset_install, check_updates. update=pull; migrate=dump+restore; reinstall reset_data wipes data; remove remove_data renames data aside.",
		InputSchema: mcpSchema{
			Type: "object",
			Properties: map[string]mcpProp{
				"action":      {Type: "string", Enum: []string{"start", "stop", "restart", "pin", "unpin", "update", "rollback", "migrate", "remove", "reinstall", "add", "expose", "env", "config_read", "config_write", "config_restore", "config_reset", "config_list_backups", "preset_list", "preset_install", "check_updates"}},
				"name":        {Type: "string", Description: "Service name/slug."},
				"tag":         {Type: "string", Description: "update/migrate: image tag."},
				"remove_data": {Type: "boolean", Description: "remove: rename data dir aside."},
				"reset_data":  {Type: "boolean", Description: "reinstall: wipe data, reprovision."},
				"image":       {Type: "string", Description: "add: OCI image reference."},
				"ports":       {Type: "array", Description: `add: host:container mappings, e.g. ["27017:27017"].`},
				"environment": {Type: "array", Description: `add: container env ["KEY=VALUE", ...].`},
				"env_vars":    {Type: "array", Description: `add: project .env keys ["KEY=VALUE", ...].`},
				"data_dir":    {Type: "string", Description: "add: container data path."},
				"description": {Type: "string", Description: "add: human-readable description."},
				"dashboard":   {Type: "string", Description: "add: web dashboard URL."},
				"depends_on":  {Type: "array", Description: `add: services to start first, e.g. ["mysql"].`},
				"init":        {Type: "boolean", Description: "add: pass --init (mysql, mariadb)."},
				"port":        {Type: "string", Description: `expose: "host:container".`},
				"remove":      {Type: "boolean", Description: "expose: remove the mapping."},
				"version":     {Type: "string", Description: "preset_install: required for multi-version presets."},
				"content":     {Type: "string", Description: "config_write: new file contents."},
				"backup":      {Type: "boolean", Description: "config_write: stage a backup first."},
				"backup_name": {Type: "string", Description: "config_restore: backup to restore (newest if omitted)."},
			},
			Required: []string{"action"},
		},
	}
}

func dbTool() mcpTool {
	return mcpTool{
		Name:        "db",
		Description: "Database operations. action: set (pick sqlite/mysql/postgres/family alternate), move (between same-family services), create, export, import, snapshot, snapshots, restore (destructive), snapshot_delete.",
		InputSchema: mcpSchema{
			Type: "object",
			Properties: map[string]mcpProp{
				"action":        {Type: "string", Enum: []string{"set", "move", "create", "export", "import", "snapshot", "snapshots", "restore", "snapshot_delete"}},
				"path":          {Type: "string", Description: "Project root. Defaults to cwd."},
				"database":      {Type: "string", Description: "set: target db engine. Others: db name (defaults DB_DATABASE)."},
				"from":          {Type: "string", Description: "move: source service."},
				"to":            {Type: "string", Description: "move: target service (same family)."},
				"sites":         {Type: "array", Description: `move: site names, e.g. ["shop"]. Omit with all=true.`},
				"all":           {Type: "boolean", Description: "move/snapshots: cover every site / database."},
				"file":          {Type: "string", Description: "import: SQL dump path."},
				"output":        {Type: "string", Description: "export: output file (default <database>.sql)."},
				"name":          {Type: "string", Description: "snapshot/restore/snapshot_delete: snapshot name. create: db name."},
				"service":       {Type: "string", Description: "snapshot ops: DB service override."},
				"all_databases": {Type: "boolean", Description: "snapshot ops: cover whole service."},
			},
			Required: []string{"action"},
		},
	}
}

func envTool() mcpTool {
	return mcpTool{
		Name:        "env",
		Description: "Manage .env. action: setup (configure services/DBs/APP_KEY/APP_URL — follow with framework setup), check (vs .env.example), override (personal .env.lerd_override).",
		InputSchema: mcpSchema{
			Type: "object",
			Properties: map[string]mcpProp{
				"action": {Type: "string", Enum: []string{"setup", "check", "override"}},
				"path":   {Type: "string", Description: "Project root. Defaults to cwd."},
				"set":    {Type: "array", Description: "override: KEY=VALUE entries (e.g. LERD_EXTERNAL_SERVICES=postgres). Omit to show."},
			},
			Required: []string{"action"},
		},
	}
}

func runtimeTool() mcpTool {
	return mcpTool{
		Name:        "runtime",
		Description: "PHP/Node runtimes. action: versions (installed PHP+Node), node_install, node_uninstall, php_list, ext_list, ext_add, ext_remove. ext_add/ext_remove rebuild the FPM image (slow).",
		InputSchema: mcpSchema{
			Type: "object",
			Properties: map[string]mcpProp{
				"action":    {Type: "string", Enum: []string{"versions", "node_install", "node_uninstall", "php_list", "ext_list", "ext_add", "ext_remove"}},
				"version":   {Type: "string", Description: "node_*: version/alias (20, lts). ext_*: PHP version (defaults project/global)."},
				"extension": {Type: "string", Description: "ext_add/ext_remove: extension name (imagick, redis, swoole)."},
				"apk_deps":  {Type: "string", Description: "ext_add: extra Alpine build packages, space-separated."},
			},
			Required: []string{"action"},
		},
	}
}

func workerTool() mcpTool {
	return mcpTool{
		Name:        "worker",
		Description: "Manage workers. action: list (CALL FIRST), start, stop, add, remove, health, heal, mode_get, mode_set. Framework workers: queue_start, queue_stop, horizon_start, horizon_stop, reverb_start, reverb_stop, schedule_start, schedule_stop, stripe_start, stripe_stop, stripe_config. Use horizon_* instead of queue_* when laravel/horizon is installed.",
		InputSchema: mcpSchema{
			Type: "object",
			Properties: map[string]mcpProp{
				"action":             {Type: "string", Enum: []string{"list", "start", "stop", "add", "remove", "health", "heal", "mode_get", "mode_set", "queue_start", "queue_stop", "horizon_start", "horizon_stop", "reverb_start", "reverb_stop", "schedule_start", "schedule_stop", "stripe_start", "stripe_stop", "stripe_config"}},
				"site":               {Type: "string", Description: "Site name (from site list)."},
				"worker":             {Type: "string", Description: "start/stop: worker name (e.g. messenger, vite)."},
				"branch":             {Type: "string", Description: "Optional worktree branch (per_worktree workers)."},
				"name":               {Type: "string", Description: "add/remove: worker slug."},
				"command":            {Type: "string", Description: "add: command run in the FPM container."},
				"label":              {Type: "string", Description: "add: human-readable label."},
				"restart":            {Type: "string", Description: "add: always (default) | on-failure."},
				"check_file":         {Type: "string", Description: "add: show only if this file exists."},
				"check_composer":     {Type: "string", Description: "add: show only if this package is installed."},
				"conflicts_with":     {Type: "array", Description: "add: workers to stop before starting this one."},
				"proxy_path":         {Type: "string", Description: "add: URL path to proxy."},
				"proxy_port_env_key": {Type: "string", Description: "add: env key holding the worker port."},
				"proxy_default_port": {Type: "number", Description: "add: fallback port."},
				"global":             {Type: "boolean", Description: "add/remove: target the user overlay."},
				"unit":               {Type: "string", Description: "heal: full unit name. Omit to heal all."},
				"mode":               {Type: "string", Enum: []string{"exec", "container"}, Description: "mode_set: macOS worker runtime."},
				"queue":              {Type: "string", Description: `queue_start: queue name (default "default").`},
				"tries":              {Type: "integer", Description: "queue_start: max attempts (default 3)."},
				"timeout":            {Type: "integer", Description: "queue_start: job timeout seconds (default 60)."},
				"api_key":            {Type: "string", Description: "stripe_start: defaults to the .env secret."},
				"webhook_path":       {Type: "string", Description: "stripe_start/stripe_config: forward path (default /stripe/webhook)."},
				"secret_env_key":     {Type: "string", Description: "stripe_config: which .env key holds the secret."},
			},
			Required: []string{"action"},
		},
	}
}

func execTool() mcpTool {
	return mcpTool{
		Name:        "exec",
		Description: "Run tooling in the PHP-FPM container. action: artisan (Laravel), console (other frameworks), composer, vendor_bins (list vendor/bin), vendor_run (pest, phpunit, pint…), commands_list, commands_run, command_add, command_remove.",
		InputSchema: mcpSchema{
			Type: "object",
			Properties: map[string]mcpProp{
				"action":         {Type: "string", Enum: []string{"artisan", "console", "composer", "vendor_bins", "vendor_run", "commands_list", "commands_run", "command_add", "command_remove"}},
				"path":           {Type: "string", Description: "Project root. Defaults to cwd."},
				"args":           {Type: "array", Description: `artisan/console/composer: argv, e.g. ["migrate"].`},
				"bin":            {Type: "string", Description: "vendor_run: binary name (pest, phpunit, pint)."},
				"site":           {Type: "string", Description: "commands_*/command_*: site name."},
				"name":           {Type: "string", Description: "commands_run/command_*: command name."},
				"command":        {Type: "string", Description: "command_add: shell command (sh -c)."},
				"force":          {Type: "boolean", Description: "commands_run: required for confirm-gated commands."},
				"label":          {Type: "string", Description: "command_add: dashboard label."},
				"description":    {Type: "string", Description: "command_add: tooltip."},
				"output":         {Type: "string", Enum: []string{"silent", "text", "url", "terminal"}, Description: "command_add: output mode."},
				"confirm":        {Type: "boolean", Description: "command_add: gate behind a confirm modal."},
				"icon":           {Type: "string", Description: "command_add: icon name."},
				"cwd":            {Type: "string", Description: "command_add: working dir relative to root."},
				"check_file":     {Type: "string", Description: "command_add: hide unless file exists."},
				"check_composer": {Type: "string", Description: "command_add: hide unless package installed."},
				"disabled":       {Type: "boolean", Description: "command_add: suppress a framework default."},
			},
			Required: []string{"action"},
		},
	}
}

func frameworkTool() mcpTool {
	return mcpTool{
		Name:        "framework",
		Description: "Framework definitions and scaffolding. action: list, add (name=laravel merges into built-in), remove, search (community store), install, project_new (scaffold), setup (run post-install steps — MANDATORY after env setup).",
		InputSchema: mcpSchema{
			Type: "object",
			Properties: map[string]mcpProp{
				"action":              {Type: "string", Enum: []string{"list", "add", "remove", "search", "install", "project_new", "setup"}},
				"name":                {Type: "string", Description: "add/remove/install: framework slug."},
				"label":               {Type: "string", Description: "add: human-readable name."},
				"public_dir":          {Type: "string", Description: "add: document root."},
				"detect_files":        {Type: "array", Description: "add: filenames that signal this framework."},
				"detect_packages":     {Type: "array", Description: "add: composer packages that signal it."},
				"env_file":            {Type: "string", Description: `add: primary env file (default ".env").`},
				"env_format":          {Type: "string", Description: "add: dotenv (default) or php-const."},
				"env_fallback_file":   {Type: "string", Description: "add: secondary env file."},
				"env_fallback_format": {Type: "string", Description: "add: format for the fallback file."},
				"workers":             {Type: "object", Description: "add: map name → {label, command, restart, check?}."},
				"setup":               {Type: "array", Description: "add: {label, command, default?, check?} entries."},
				"logs":                {Type: "array", Description: `add: {path, format?} entries.`},
				"version":             {Type: "string", Description: "remove/install: version (omit to auto)."},
				"query":               {Type: "string", Description: "search: name/label query."},
				"path":                {Type: "string", Description: "project_new: new project dir. setup: project root."},
				"framework":           {Type: "string", Description: `project_new: framework (default "laravel").`},
				"args":                {Type: "array", Description: "project_new: extra scaffold args."},
			},
			Required: []string{"action"},
		},
	}
}

func diagTool() mcpTool {
	return mcpTool{
		Name:        "diag",
		Description: "Diagnostics & observability. action: status, doctor, which, check, dns_diagnose, bug_report, analyze_queries (N+1/slow queries), dumps_recent, dumps_status, dumps_clear, dumps_toggle, profiler_toggle, profiler_status, profiler_clear, xdebug_on, xdebug_off, xdebug_status. (Reading logs moved to the `logs` tool.)",
		InputSchema: mcpSchema{
			Type: "object",
			Properties: map[string]mcpProp{
				"action":          {Type: "string", Enum: []string{"status", "doctor", "which", "check", "dns_diagnose", "bug_report", "analyze_queries", "dumps_recent", "dumps_status", "dumps_clear", "dumps_toggle", "profiler_toggle", "profiler_status", "profiler_clear", "xdebug_on", "xdebug_off", "xdebug_status"}},
				"path":            {Type: "string", Description: "Project root (which/check). Defaults to cwd."},
				"site":            {Type: "string", Description: "dumps/analyze_queries: site filter."},
				"branch":          {Type: "string", Description: "dumps_recent: worktree branch filter."},
				"ctx":             {Type: "string", Enum: []string{"fpm", "cli"}, Description: "dumps_recent: context filter."},
				"kind":            {Type: "string", Enum: []string{"dump", "query", "job", "view", "mail", "cache", "event", "http"}, Description: "dumps_recent: event kind."},
				"since":           {Type: "string", Description: "dumps_recent: time filter."},
				"limit":           {Type: "integer", Description: "dumps_recent: max events."},
				"min_repeat":      {Type: "integer", Description: "analyze_queries: N+1 repeat threshold."},
				"slow_ms":         {Type: "number", Description: "analyze_queries: slow-query threshold."},
				"enable":          {Type: "boolean", Description: "dumps_toggle/profiler_toggle: on/off."},
				"output":          {Type: "string", Description: "bug_report: output file path."},
				"log_lines":       {Type: "integer", Description: "bug_report: lines per log (default 200)."},
				"show_real_names": {Type: "boolean", Description: "bug_report: skip anonymisation."},
				"version":         {Type: "string", Description: "xdebug_on/off: PHP version."},
				"mode":            {Type: "string", Description: "xdebug_on: debug (default) | coverage | develop | profile | trace | gcstats."},
				"tld":             {Type: "string", Description: "dns_diagnose: TLD override (defaults to configured)."},
			},
			Required: []string{"action"},
		},
	}
}
