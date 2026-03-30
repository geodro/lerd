package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/mcp"
	"github.com/spf13/cobra"
)

// NewMCPCmd returns the mcp command — starts the MCP server over stdio.
func NewMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Start the lerd MCP server (JSON-RPC 2.0 over stdio)",
		Long: `Starts a Model Context Protocol server that allows AI assistants
(Claude Code, JetBrains Junie, etc.) to manage lerd sites, run artisan
commands, and control services.

This command is normally invoked automatically by the AI assistant via
the MCP configuration injected by 'lerd mcp:inject'.`,
		Hidden: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return mcp.Serve()
		},
	}
}

// NewMCPInjectCmd returns the mcp:inject command.
func NewMCPInjectCmd() *cobra.Command {
	var targetPath string
	cmd := &cobra.Command{
		Use:   "mcp:inject",
		Short: "Inject lerd MCP config and AI skill files into a project",
		Long: `Writes the following files into the target project directory:

  .mcp.json                    MCP server config for Claude Code
  .claude/skills/lerd/SKILL.md  Claude Code skill (lerd tools reference)
  .junie/mcp/mcp.json           MCP server config for JetBrains Junie

Run this from a Laravel project root, or use --path to specify a directory.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runMCPInject(targetPath)
		},
	}
	cmd.Flags().StringVar(&targetPath, "path", "", "Target project directory (defaults to current directory)")
	return cmd
}

func runMCPInject(targetPath string) error {
	if targetPath == "" {
		var err error
		targetPath, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	abs, err := filepath.Abs(targetPath)
	if err != nil {
		return err
	}

	lerdEntry := map[string]any{
		"command": "lerd",
		"args":    []string{"mcp"},
		"env":     map[string]string{"LERD_SITE_PATH": abs},
	}

	fmt.Printf("Injecting lerd MCP config into: %s\n\n", abs)

	// .mcp.json — merge lerd into mcpServers
	if err := mergeMCPServersJSON(filepath.Join(abs, ".mcp.json"), lerdEntry); err != nil {
		return err
	}
	rel1 := ".mcp.json"
	fmt.Printf("  updated %s\n", rel1)

	// .ai/mcp/mcp.json — same mcpServers format (Windsurf and others)
	aiPath := filepath.Join(abs, ".ai", "mcp", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(aiPath), 0755); err != nil {
		return fmt.Errorf("creating .ai/mcp: %w", err)
	}
	if err := mergeMCPServersJSON(aiPath, lerdEntry); err != nil {
		return err
	}
	fmt.Printf("  updated .ai/mcp/mcp.json\n")

	// .junie/mcp/mcp.json — same mcpServers format
	juniePath := filepath.Join(abs, ".junie", "mcp", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(juniePath), 0755); err != nil {
		return fmt.Errorf("creating .junie/mcp: %w", err)
	}
	if err := mergeMCPServersJSON(juniePath, lerdEntry); err != nil {
		return err
	}
	fmt.Printf("  updated .junie/mcp/mcp.json\n")

	// .claude/skills/lerd/SKILL.md — always overwrite (we own this file)
	skillPath := filepath.Join(abs, ".claude", "skills", "lerd", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0755); err != nil {
		return fmt.Errorf("creating .claude/skills/lerd: %w", err)
	}
	if err := os.WriteFile(skillPath, []byte(claudeSkillContent), 0644); err != nil {
		return fmt.Errorf("writing SKILL.md: %w", err)
	}
	fmt.Printf("  wrote   .claude/skills/lerd/SKILL.md\n")

	// .junie/guidelines.md — merge our section (Junie's equivalent of Claude skills)
	guidelinesPath := filepath.Join(abs, ".junie", "guidelines.md")
	if err := mergeJunieGuidelines(guidelinesPath, junieGuidelinesSection); err != nil {
		return fmt.Errorf("writing .junie/guidelines.md: %w", err)
	}
	fmt.Printf("  updated .junie/guidelines.md\n")

	fmt.Println("\nDone! Restart your AI assistant to load the lerd MCP server.")
	return nil
}

// NewMCPEnableGlobalCmd returns the mcp:enable-global command.
func NewMCPEnableGlobalCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp:enable-global",
		Short: "Register lerd MCP globally for all AI assistant sessions",
		Long: `Registers the lerd MCP server at user scope so it is available
in every Claude Code session, regardless of the current project directory.

The server uses the directory Claude is opened in as the site context —
no LERD_SITE_PATH configuration needed.

This command updates:
  claude mcp add --scope user   Claude Code user-scope MCP registration
  ~/.ai/mcp/mcp.json            Windsurf global MCP config
  ~/.junie/mcp/mcp.json         JetBrains Junie global MCP config`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return RunMCPEnableGlobal()
		},
	}
}

// RunMCPEnableGlobal registers lerd MCP at user scope for all supported AI tools.
// It is exported so the install command can call it directly.
func RunMCPEnableGlobal() error {
	// Entry without LERD_SITE_PATH — server falls back to cwd at runtime.
	lerdEntry := map[string]any{
		"command": "lerd",
		"args":    []string{"mcp"},
	}

	fmt.Println("Registering lerd MCP globally...")

	// Claude Code — user scope via CLI.
	// Try remove first (idempotent re-registration), then add.
	_ = exec.Command("claude", "mcp", "remove", "--scope", "user", "lerd").Run()
	out, err := exec.Command("claude", "mcp", "add", "--scope", "user", "lerd", "--", "lerd", "mcp").CombinedOutput()
	if err != nil {
		fmt.Printf("  warning: could not register with Claude Code (%v): %s\n", err, strings.TrimSpace(string(out)))
		fmt.Printf("  Run manually: claude mcp add --scope user lerd -- lerd mcp\n")
	} else {
		fmt.Println("  registered in Claude Code (user scope)")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	// Windsurf global.
	aiPath := filepath.Join(home, ".ai", "mcp", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(aiPath), 0755); err != nil {
		return fmt.Errorf("creating ~/.ai/mcp: %w", err)
	}
	if err := mergeMCPServersJSON(aiPath, lerdEntry); err != nil {
		return err
	}
	fmt.Println("  updated ~/.ai/mcp/mcp.json")

	// JetBrains Junie global.
	juniePath := filepath.Join(home, ".junie", "mcp", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(juniePath), 0755); err != nil {
		return fmt.Errorf("creating ~/.junie/mcp: %w", err)
	}
	if err := mergeMCPServersJSON(juniePath, lerdEntry); err != nil {
		return err
	}
	fmt.Println("  updated ~/.junie/mcp/mcp.json")

	fmt.Println("\nDone! Restart your AI assistant for changes to take effect.")
	fmt.Println("lerd will use the directory you open Claude in as the site context.")
	return nil
}

// IsMCPGloballyRegistered reports whether lerd is already registered at user scope
// in Claude Code. Used by the install command to skip the prompt if already set up.
func IsMCPGloballyRegistered() bool {
	out, err := exec.Command("claude", "mcp", "list", "--scope", "user").CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "lerd")
}

// mergeJunieGuidelines upserts the lerd section inside .junie/guidelines.md.
// If the file does not exist it is created. If a lerd section already exists
// (delimited by the sentinel comments) it is replaced; otherwise the section
// is appended.
func mergeJunieGuidelines(path, section string) error {
	const begin = "<!-- lerd:begin -->"
	const end = "<!-- lerd:end -->"

	existing := ""
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return err
	}

	block := begin + "\n" + section + "\n" + end

	if strings.Contains(existing, begin) {
		// Replace the existing lerd block.
		startIdx := strings.Index(existing, begin)
		endIdx := strings.Index(existing, end)
		if endIdx == -1 {
			// Malformed — replace from begin to EOF.
			existing = strings.TrimRight(existing[:startIdx], "\n") + "\n\n" + block + "\n"
		} else {
			existing = existing[:startIdx] + block + existing[endIdx+len(end):]
		}
	} else {
		// Append, ensuring a blank line separator.
		if existing != "" {
			existing = strings.TrimRight(existing, "\n") + "\n\n"
		}
		existing += block + "\n"
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(existing), 0644)
}

// mergeMCPServersJSON reads an existing JSON file (if present), adds or updates
// the "lerd" key inside "mcpServers", and writes it back with indentation.
func mergeMCPServersJSON(path string, lerdEntry map[string]any) error {
	// Start with an empty config or read what's there.
	cfg := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		// Unmarshal preserving all existing keys.
		if err := json.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
	}

	// Ensure mcpServers map exists.
	servers, _ := cfg["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers["lerd"] = lerdEntry
	cfg["mcpServers"] = servers

	data, err := json.MarshalIndent(cfg, "", "    ")
	if err != nil {
		return fmt.Errorf("marshalling %s: %w", path, err)
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}

// bt is a backtick character for use inside raw string literals.
const bt = "`"

const claudeSkillContent = `---
name: lerd
description: Manage the lerd local Laravel development environment — run artisan commands, manage services, start/stop queue workers, run composer, manage Node.js versions, and inspect site status via MCP tools.
---
# Lerd — Laravel Local Dev Environment

This project runs on **lerd**, a Podman-based Laravel development environment for Linux (similar to Laravel Herd). The ` + bt + `lerd` + bt + ` MCP server exposes tools to manage it directly from your AI assistant.

## Path resolution

Tools that accept a ` + bt + `path` + bt + ` argument (` + bt + `artisan` + bt + `, ` + bt + `composer` + bt + `, ` + bt + `env_setup` + bt + `, ` + bt + `site_link` + bt + `, ` + bt + `db_export` + bt + `, etc.) resolve it in this order:
1. Explicit ` + bt + `path` + bt + ` argument
2. ` + bt + `LERD_SITE_PATH` + bt + ` env var (set when using project-scoped ` + bt + `mcp:inject` + bt + `)
3. **Current working directory** — the directory Claude was opened in

In practice, you can almost always omit ` + bt + `path` + bt + ` — just open Claude in the project directory.

## Architecture

- PHP runs inside Podman containers named ` + bt + `lerd-php<version>-fpm` + bt + ` (e.g. ` + bt + `lerd-php84-fpm` + bt + `)
- Each PHP-FPM container includes **composer** and **node/npm** so you can run all tooling without leaving the container
- Nginx routes ` + bt + `*.test` + bt + ` domains to the appropriate FPM container
- Services (MySQL, Redis, PostgreSQL, etc.) run as Podman containers via systemd quadlets
- Custom services (MongoDB, RabbitMQ, …) can be added with ` + bt + `service_add` + bt + ` and managed identically to built-in ones
- Node.js versions are managed by **fnm** (Fast Node Manager); pin per-project with a ` + bt + `.node-version` + bt + ` file
- Framework workers (queue, schedule, reverb, messenger, etc.) run as systemd user services named ` + bt + `lerd-<worker>-<sitename>` + bt + ` (e.g. ` + bt + `lerd-queue-myapp` + bt + `, ` + bt + `lerd-messenger-myapp` + bt + `)
- Worker commands are defined per-framework in YAML definitions; Laravel has built-in queue/schedule/reverb workers; custom frameworks can add any workers
- Git worktrees automatically get a ` + bt + `<branch>.<site>.test` + bt + ` subdomain; ` + bt + `vendor/` + bt + `, ` + bt + `node_modules/` + bt + `, and ` + bt + `.env` + bt + ` are symlinked/copied from the main checkout
- DNS resolves ` + bt + `*.test` + bt + ` to ` + bt + `127.0.0.1` + bt + `

## Available MCP Tools

### ` + bt + `sites` + bt + `
List all registered lerd sites with domains, paths, PHP versions, Node versions, and queue status. **Call this first** to find site names and paths needed by other tools.

### ` + bt + `runtime_versions` + bt + `
List all installed PHP and Node.js versions and the configured defaults. Call this to check what runtimes are available before running commands.

### ` + bt + `artisan` + bt + `
Run ` + bt + `php artisan` + bt + ` inside the PHP-FPM container for the project. Arguments:
- ` + bt + `path` + bt + ` (optional): absolute path to the Laravel project root — defaults to the current working directory (or ` + bt + `LERD_SITE_PATH` + bt + ` if set by ` + bt + `mcp:inject` + bt + `)
- ` + bt + `args` + bt + ` (required): artisan arguments as an array

Examples:
` + "```" + `
artisan(args: ["migrate"])
artisan(args: ["make:model", "Post", "-m"])
artisan(args: ["db:seed", "--class=UserSeeder"])
artisan(args: ["cache:clear"])
artisan(args: ["tinker", "--execute=echo App\\Models\\User::count();"])
` + "```" + `

> **Note:** ` + bt + `tinker` + bt + ` requires ` + bt + `--execute=<code>` + bt + ` for non-interactive use.

### ` + bt + `composer` + bt + `
Run ` + bt + `composer` + bt + ` inside the PHP-FPM container for the project. Arguments:
- ` + bt + `path` + bt + ` (optional): absolute path to the Laravel project root — defaults to the current working directory (or ` + bt + `LERD_SITE_PATH` + bt + ` if set by ` + bt + `mcp:inject` + bt + `)
- ` + bt + `args` + bt + ` (required): composer arguments as an array

Examples:
` + "```" + `
composer(args: ["install"])
composer(args: ["require", "laravel/sanctum"])
composer(args: ["dump-autoload"])
composer(args: ["update", "laravel/framework"])
` + "```" + `

### ` + bt + `node_install` + bt + ` / ` + bt + `node_uninstall` + bt + `
Install or uninstall a Node.js version via fnm. Accepts a version number or alias:
` + "```" + `
node_install(version: "20")
node_install(version: "20.11.0")
node_install(version: "lts")
node_uninstall(version: "18.20.0")
` + "```" + `

After installing a version you can pin it to a project by writing a ` + bt + `.node-version` + bt + ` file in the project root (or run ` + bt + `lerd isolate:node <version>` + bt + ` from a terminal).

### ` + bt + `service_start` + bt + ` / ` + bt + `service_stop` + bt + `
Start or stop any service — built-in or custom. ` + bt + `service_stop` + bt + ` marks the service as **paused** — ` + bt + `lerd start` + bt + ` and autostart on login will skip it until you explicitly start it again.

**Dependency cascade:** if a custom service has ` + bt + `depends_on` + bt + ` set, starting its dependency also starts it; stopping the dependency stops it first. Starting the custom service directly ensures its dependencies start first.

Built-in names: ` + bt + `mysql` + bt + `, ` + bt + `redis` + bt + `, ` + bt + `postgres` + bt + `, ` + bt + `meilisearch` + bt + `, ` + bt + `rustfs` + bt + `, ` + bt + `mailpit` + bt + `. Custom service names (registered with ` + bt + `service_add` + bt + `) are also accepted — just pass the same name used in ` + bt + `service_add` + bt + `.

**.env values for built-in lerd services:**

| Service | Host | Key vars |
|---------|------|----------|
| mysql | ` + bt + `lerd-mysql` + bt + ` | ` + bt + `DB_CONNECTION=mysql` + bt + `, ` + bt + `DB_PASSWORD=lerd` + bt + ` |
| postgres | ` + bt + `lerd-postgres` + bt + ` | ` + bt + `DB_CONNECTION=pgsql` + bt + `, ` + bt + `DB_PASSWORD=lerd` + bt + ` |
| redis | ` + bt + `lerd-redis` + bt + ` | ` + bt + `REDIS_PASSWORD=null` + bt + ` |
| mailpit | ` + bt + `lerd-mailpit:1025` + bt + ` | web UI: http://localhost:8025 |
| meilisearch | ` + bt + `lerd-meilisearch:7700` + bt + ` | |
| rustfs | ` + bt + `lerd-rustfs:9000` + bt + ` | ` + bt + `AWS_USE_PATH_STYLE_ENDPOINT=true` + bt + ` |

### ` + bt + `service_expose` + bt + `
Add or remove an extra published port on a built-in service. The mapping is persisted in ` + bt + `~/.config/lerd/config.yaml` + bt + ` and applied on every start. The service is restarted automatically if running.

Arguments:
- ` + bt + `name` + bt + ` (required): built-in service name (` + bt + `mysql` + bt + `, ` + bt + `redis` + bt + `, ` + bt + `postgres` + bt + `, ` + bt + `meilisearch` + bt + `, ` + bt + `rustfs` + bt + `, ` + bt + `mailpit` + bt + `)
- ` + bt + `port` + bt + ` (required): mapping as ` + bt + `"host:container"` + bt + `, e.g. ` + bt + `"13306:3306"` + bt + `
- ` + bt + `remove` + bt + ` (optional): set to ` + bt + `true` + bt + ` to remove the mapping instead of adding it

Examples:
` + "```" + `
service_expose(name: "mysql", port: "13306:3306")
service_expose(name: "mysql", port: "13306:3306", remove: true)
` + "```" + `

### ` + bt + `service_add` + bt + ` / ` + bt + `service_remove` + bt + `
Register or remove a custom OCI-based service. Arguments for ` + bt + `service_add` + bt + `:
- ` + bt + `name` + bt + ` (required): slug, e.g. ` + bt + `"mongodb"` + bt + `
- ` + bt + `image` + bt + ` (required): OCI image, e.g. ` + bt + `"docker.io/library/mongo:7"` + bt + `
- ` + bt + `ports` + bt + ` (optional): array of ` + bt + `"host:container"` + bt + ` mappings
- ` + bt + `environment` + bt + ` (optional): array of ` + bt + `"KEY=VALUE"` + bt + ` strings for the container
- ` + bt + `env_vars` + bt + ` (optional): array of ` + bt + `"KEY=VALUE"` + bt + ` strings shown in ` + bt + `lerd env` + bt + ` suggestions
- ` + bt + `data_dir` + bt + ` (optional): mount path inside container for persistent data
- ` + bt + `description` + bt + ` (optional): human-readable description
- ` + bt + `dashboard` + bt + ` (optional): URL for the service's web UI
- ` + bt + `depends_on` + bt + ` (optional): array of service names that must be running before this service starts, e.g. ` + bt + `["mysql"]` + bt + `

When ` + bt + `depends_on` + bt + ` is set:
- Starting this service automatically starts its dependencies first
- Starting a dependency automatically starts this service afterwards
- Stopping a dependency automatically stops this service first (cascade stop)

Example — add MongoDB:
` + "```" + `
service_add(
  name: "mongodb",
  image: "docker.io/library/mongo:7",
  ports: ["27017:27017"],
  data_dir: "/data/db",
  env_vars: ["MONGODB_URL=mongodb://lerd-mongodb:27017"]
)
service_start(name: "mongodb")
` + "```" + `

Example — add phpMyAdmin depending on MySQL:
` + "```" + `
service_add(
  name: "phpmyadmin",
  image: "docker.io/phpmyadmin:latest",
  ports: ["8080:80"],
  depends_on: ["mysql"],
  dashboard: "http://localhost:8080"
)
service_start(name: "phpmyadmin")   // starts mysql first, then phpmyadmin
` + "```" + `

` + bt + `service_remove` + bt + ` stops and deregisters a custom service. Persistent data is NOT deleted.

### ` + bt + `service_env` + bt + `
Return the recommended Laravel ` + bt + `.env` + bt + ` connection variables for a service — built-in or custom — as a key/value map. Use this when you need to inspect or manually apply connection settings without running ` + bt + `env_setup` + bt + `.

### ` + bt + `env_setup` + bt + `
Configure the project's ` + bt + `.env` + bt + ` for lerd in one call:
- Creates ` + bt + `.env` + bt + ` from ` + bt + `.env.example` + bt + ` if it doesn't exist
- Detects which services (MySQL, Redis, …) the project uses and sets lerd connection values
- Starts any referenced services that aren't running
- Creates the project database (and ` + bt + `<name>_testing` + bt + ` database)
- Generates ` + bt + `APP_KEY` + bt + ` if missing
- Sets ` + bt + `APP_URL` + bt + ` to the registered ` + bt + `.test` + bt + ` domain

Arguments:
- ` + bt + `path` + bt + ` (optional): absolute path to the Laravel project root — defaults to the current working directory (or ` + bt + `LERD_SITE_PATH` + bt + ` if set by ` + bt + `mcp:inject` + bt + `)

> Run this right after ` + bt + `site_link` + bt + ` when setting up a fresh project.

### ` + bt + `site_link` + bt + ` / ` + bt + `site_unlink` + bt + `
Register or unregister a directory as a lerd site. Arguments for ` + bt + `site_link` + bt + `:
- ` + bt + `path` + bt + ` (optional): absolute path to the project directory — defaults to ` + bt + `LERD_SITE_PATH` + bt + ` set by ` + bt + `mcp:inject` + bt + `
- ` + bt + `name` + bt + ` (optional): site name (defaults to directory name, cleaned up)
- ` + bt + `domain` + bt + ` (optional): custom domain (defaults to ` + bt + `<name>.test` + bt + `)

` + bt + `site_unlink` + bt + ` takes ` + bt + `site` + bt + ` (site name from ` + bt + `sites` + bt + ` tool). Project files are NOT deleted.

### ` + bt + `secure` + bt + ` / ` + bt + `unsecure` + bt + `
Enable or disable HTTPS for a site using a locally-trusted mkcert certificate. Both take ` + bt + `site` + bt + ` (site name). ` + bt + `APP_URL` + bt + ` in ` + bt + `.env` + bt + ` is updated automatically.

### ` + bt + `xdebug_on` + bt + ` / ` + bt + `xdebug_off` + bt + ` / ` + bt + `xdebug_status` + bt + `
Toggle Xdebug for a PHP version (restarts the FPM container). Optional ` + bt + `version` + bt + ` argument — defaults to the project or global PHP version. Xdebug listens on port ` + bt + `9003` + bt + ` at ` + bt + `host.containers.internal` + bt + `.

` + bt + `xdebug_status` + bt + ` returns the enabled/disabled state for all installed PHP versions.

### ` + bt + `queue_start` + bt + ` / ` + bt + `queue_stop` + bt + `
Start or stop a queue worker for a site. Available for any framework that defines a ` + bt + `queue` + bt + ` worker (Laravel has it built-in). Runs the framework-defined command in the FPM container as a systemd service.

> **Redis queues:** if the project's ` + bt + `.env` + bt + ` has ` + bt + `QUEUE_CONNECTION=redis` + bt + `, lerd will refuse to start the worker unless ` + bt + `lerd-redis` + bt + ` is running. Call ` + bt + `service_start(name: "redis")` + bt + ` first.

Arguments for ` + bt + `queue_start` + bt + `:
- ` + bt + `site` + bt + ` (required): site name from ` + bt + `sites` + bt + ` tool
- ` + bt + `queue` + bt + ` (optional): queue name, default ` + bt + `"default"` + bt + `
- ` + bt + `tries` + bt + ` (optional): max job attempts, default ` + bt + `3` + bt + `
- ` + bt + `timeout` + bt + ` (optional): job timeout in seconds, default ` + bt + `60` + bt + `

### ` + bt + `horizon_start` + bt + ` / ` + bt + `horizon_stop` + bt + `
Start or stop Laravel Horizon for a site. Horizon is a queue manager that replaces ` + bt + `queue:work` + bt + ` — use ` + bt + `horizon_start` + bt + ` instead of ` + bt + `queue_start` + bt + ` for projects that have ` + bt + `laravel/horizon` + bt + ` in ` + bt + `composer.json` + bt + `. Takes ` + bt + `site` + bt + ` (required, site name from ` + bt + `sites` + bt + ` tool). Returns an error if ` + bt + `laravel/horizon` + bt + ` is not installed.

> **Horizon vs queue worker:** The ` + bt + `sites` + bt + ` tool returns ` + bt + `has_horizon: true` + bt + ` when a site has Horizon installed. In that case prefer ` + bt + `horizon_start` + bt + ` over ` + bt + `queue_start` + bt + `.

### ` + bt + `reverb_start` + bt + ` / ` + bt + `reverb_stop` + bt + `
Start or stop the Reverb WebSocket server for a site. Available for any framework that defines a ` + bt + `reverb` + bt + ` worker. Takes ` + bt + `site` + bt + ` (required, site name from ` + bt + `sites` + bt + ` tool).

### ` + bt + `schedule_start` + bt + ` / ` + bt + `schedule_stop` + bt + `
Start or stop the task scheduler for a site. Available for any framework that defines a ` + bt + `schedule` + bt + ` worker. Takes ` + bt + `site` + bt + ` (required, site name from ` + bt + `sites` + bt + ` tool).

### ` + bt + `worker_start` + bt + ` / ` + bt + `worker_stop` + bt + `
Start or stop any named framework worker for a site. Use this for workers that don't have a dedicated shortcut (e.g. ` + bt + `messenger` + bt + ` for Symfony, ` + bt + `horizon` + bt + ` or ` + bt + `pulse` + bt + ` for Laravel). The worker command is taken from the framework definition.

Arguments:
- ` + bt + `site` + bt + ` (required): site name from ` + bt + `sites` + bt + ` tool
- ` + bt + `worker` + bt + ` (required): worker name as defined in the framework (e.g. ` + bt + `"messenger"` + bt + `, ` + bt + `"horizon"` + bt + `)

### ` + bt + `worker_list` + bt + `
List all workers defined for a site's framework, with their running status, command, unit name, and restart policy. Use this to discover available workers before calling ` + bt + `worker_start` + bt + `.

Arguments:
- ` + bt + `site` + bt + ` (required): site name from ` + bt + `sites` + bt + ` tool

### ` + bt + `project_new` + bt + `
Scaffold a new PHP project using a framework's create command. For Laravel, runs ` + bt + `composer create-project laravel/laravel <path>` + bt + `. Other frameworks must have a ` + bt + `create` + bt + ` field in their YAML definition.

Arguments:
- ` + bt + `path` + bt + ` (required): absolute path for the new project directory (e.g. ` + bt + `/home/user/code/myapp` + bt + `)
- ` + bt + `framework` + bt + ` (optional): framework name (default: ` + bt + `"laravel"` + bt + `)
- ` + bt + `args` + bt + ` (optional): extra arguments passed to the scaffold command

After creation, register and configure the project:
` + "```" + `
project_new(path: "/home/user/code/myapp")
site_link(path: "/home/user/code/myapp")
env_setup(path: "/home/user/code/myapp")
` + "```" + `

From the terminal you can also run:
` + "```" + `
lerd new myapp
cd myapp && lerd link && lerd setup
` + "```" + `

### ` + bt + `framework_list` + bt + `
List all available framework definitions (Laravel built-in plus any user-defined YAMLs at ` + bt + `~/.config/lerd/frameworks/` + bt + `), including their defined workers. Call this before ` + bt + `framework_add` + bt + ` to see what already exists.

### ` + bt + `framework_add` + bt + `
Create or update a framework definition. For ` + bt + `laravel` + bt + `, only the ` + bt + `workers` + bt + ` field is accepted (built-in settings are always preserved). For other frameworks, creates a full definition.

Arguments:
- ` + bt + `name` + bt + ` (required): framework slug (e.g. ` + bt + `"symfony"` + bt + `). Use ` + bt + `"laravel"` + bt + ` to add custom workers to the built-in Laravel definition (e.g. ` + bt + `horizon` + bt + `, ` + bt + `pulse` + bt + `)
- ` + bt + `label` + bt + ` (optional): display name, e.g. ` + bt + `"Symfony"` + bt + `
- ` + bt + `public_dir` + bt + ` (optional): document root relative to project (default: ` + bt + `"public"` + bt + `)
- ` + bt + `detect_files` + bt + ` (optional): array of filenames that signal this framework
- ` + bt + `detect_packages` + bt + ` (optional): array of Composer packages that signal this framework
- ` + bt + `env_file` + bt + ` (optional): primary env file path (default: ` + bt + `".env"` + bt + `)
- ` + bt + `env_format` + bt + ` (optional): ` + bt + `"dotenv"` + bt + ` or ` + bt + `"php-const"` + bt + `
- ` + bt + `workers` + bt + ` (optional): map of worker name → ` + bt + `{label, command, restart}` + bt + `

Example — add Horizon to Laravel:
` + "```" + `
framework_add(name: "laravel", workers: {
  "horizon": {"label": "Horizon", "command": "php artisan horizon", "restart": "always"}
})
` + "```" + `

Example — define a new framework:
` + "```" + `
framework_add(
  name: "wordpress",
  label: "WordPress",
  public_dir: ".",
  detect_files: ["wp-login.php"],
  workers: {
    "cron": {"label": "WP Cron", "command": "wp cron event run --due-now --allow-root", "restart": "always"}
  }
)
` + "```" + `

### ` + bt + `framework_remove` + bt + `
Delete a user-defined framework YAML. For ` + bt + `laravel` + bt + `, removes only the custom worker additions (built-in queue/schedule/reverb remain). Takes ` + bt + `name` + bt + ` (required).

### ` + bt + `site_php` + bt + ` / ` + bt + `site_node` + bt + `
Change the PHP or Node.js version for a registered site. Both take ` + bt + `site` + bt + ` (required) and ` + bt + `version` + bt + ` (required).

` + bt + `site_php` + bt + ` writes a ` + bt + `.php-version` + bt + ` pin file to the project root, updates the site registry, and regenerates the nginx vhost. The FPM container for the target PHP version must be running — start it with ` + bt + `service_start(name: "php<version>")` + bt + ` if needed.

` + bt + `site_node` + bt + ` writes a ` + bt + `.node-version` + bt + ` pin file and installs the version via fnm if it isn't already installed. Run ` + bt + `npm install` + bt + ` inside the project if dependencies need rebuilding against the new version.

### ` + bt + `site_pause` + bt + ` / ` + bt + `site_unpause` + bt + `
Pause or resume a site. Both take ` + bt + `site` + bt + ` (required, site name from ` + bt + `sites` + bt + ` tool).

` + bt + `site_pause` + bt + ` stops all running workers for the site (queue, schedule, reverb, stripe, custom) and replaces its nginx vhost with a landing page that includes a **Resume** button. Services no longer needed by any active site are auto-stopped. The paused state is persisted — the site stays paused across lerd restarts.

` + bt + `site_unpause` + bt + ` restores the nginx vhost, ensures any services referenced in the site's ` + bt + `.env` + bt + ` are running, and restarts any workers that were running when the site was paused.

Use this to free up resources for sites you're not actively working on without fully unlinking them.

### ` + bt + `service_pin` + bt + ` / ` + bt + `service_unpin` + bt + `
Pin or unpin a service. Both take ` + bt + `name` + bt + ` (required).

` + bt + `service_pin` + bt + ` marks a service so it is **never auto-stopped**, even when no active sites reference it in their ` + bt + `.env` + bt + `. Starts the service if it isn't already running. Use this for services you want always available regardless of which site is active (e.g. a shared Redis or MySQL).

` + bt + `service_unpin` + bt + ` removes the pin so the service can be auto-stopped when no sites use it.

### ` + bt + `stripe_listen` + bt + ` / ` + bt + `stripe_listen_stop` + bt + `
Start or stop a Stripe webhook listener for a site using the Stripe CLI container. Reads ` + bt + `STRIPE_SECRET` + bt + ` from the site's ` + bt + `.env` + bt + ` and forwards webhooks to ` + bt + `/stripe/webhook` + bt + ` by default.

Arguments for ` + bt + `stripe_listen` + bt + `:
- ` + bt + `site` + bt + ` (required): site name from ` + bt + `sites` + bt + ` tool
- ` + bt + `api_key` + bt + ` (optional): Stripe secret key (defaults to ` + bt + `STRIPE_SECRET` + bt + ` in the site's ` + bt + `.env` + bt + `)
- ` + bt + `webhook_path` + bt + ` (optional): webhook route path (default: ` + bt + `"/stripe/webhook"` + bt + `)

### ` + bt + `db_export` + bt + `
Export a database to a SQL dump file. Arguments:
- ` + bt + `path` + bt + ` (optional): absolute path to the Laravel project root — defaults to the current working directory (or ` + bt + `LERD_SITE_PATH` + bt + ` if set by ` + bt + `mcp:inject` + bt + `)
- ` + bt + `database` + bt + ` (optional): database name to export (defaults to ` + bt + `DB_DATABASE` + bt + ` from ` + bt + `.env` + bt + `)
- ` + bt + `output` + bt + ` (optional): output file path (defaults to ` + bt + `<database>.sql` + bt + ` in the project root)

### ` + bt + `logs` + bt + `
Fetch recent container logs. ` + bt + `target` + bt + ` is optional — when omitted, returns logs for the current site's PHP-FPM container (resolved from ` + bt + `LERD_SITE_PATH` + bt + `). Specify ` + bt + `target` + bt + ` only when you want a different container:
- ` + bt + `"nginx"` + bt + ` — nginx proxy logs
- Service name: ` + bt + `"mysql"` + bt + `, ` + bt + `"redis"` + bt + `, or any custom service name
- PHP version: ` + bt + `"8.4"` + bt + ` — logs for that PHP-FPM container
- Site name — logs for a different site's PHP-FPM container

Optional ` + bt + `lines` + bt + ` parameter (default: 50).

### ` + bt + `status` + bt + `
Return the health status of core lerd services as structured JSON: DNS resolution (ok + tld), nginx (running), PHP-FPM containers (running per version), and the file watcher (running). **Call this first when a site isn't loading** — it pinpoints which service is down before suggesting fixes.

### ` + bt + `doctor` + bt + `
Run a full environment diagnostic. Checks podman availability, systemd user session, linger, quadlet/data dir writability, config validity, DNS resolution, port 80/443 conflicts, PHP image presence, and available updates. Returns a text report with OK/FAIL/WARN per check and hints for each failure. **Use this when the user reports setup issues or unexpected behaviour.**

## Common Workflows

**Check installed runtimes before starting:**
` + "```" + `
runtime_versions()   // see PHP and Node.js versions available
` + "```" + `

**Create a new Laravel project from scratch (global session, empty directory):**
` + "```" + `
composer(args: ["create-project", "laravel/laravel", "."])
site_link()           // registers the cwd as a lerd site
env_setup()           // configures .env, starts services, creates DB, generates APP_KEY
artisan(args: ["migrate"])
` + "```" + `

**Set up a cloned project (full flow):**
` + "```" + `
site_link()                          // registers the cwd as a lerd site
env_setup()                          // auto-configures .env, starts services, creates DB
composer(args: ["install"])
artisan(args: ["migrate", "--seed"])
` + "```" + `

**Enable HTTPS for a site:**
` + "```" + `
secure(site: "myapp")
` + "```" + `

**Enable Xdebug for a debugging session:**
` + "```" + `
xdebug_status()              // check current state
xdebug_on(version: "8.4")   // enable and restart FPM
// ... debug ...
xdebug_off(version: "8.4")  // disable when done (Xdebug adds overhead)
` + "```" + `

**Run migrations after schema changes:**
` + "```" + `
artisan(args: ["migrate"])
` + "```" + `

**Install and configure a service:**
` + "```" + `
service_start(name: "mysql")
service_start(name: "redis")   // if needed
composer(args: ["install"])
artisan(args: ["key:generate"])
artisan(args: ["migrate", "--seed"])
` + "```" + `

**Install a new package:**
` + "```" + `
composer(args: ["require", "spatie/laravel-permission"])
artisan(args: ["vendor:publish", "--provider=Spatie\\Permission\\PermissionServiceProvider"])
artisan(args: ["migrate"])
` + "```" + `

**Install a Node.js version and pin it to the project:**
` + "```" + `
node_install(version: "20")
// Then in a terminal: lerd isolate:node 20
` + "```" + `

**Add a custom service (e.g. MongoDB):**
` + "```" + `
service_add(name: "mongodb", image: "docker.io/library/mongo:7", ports: ["27017:27017"], data_dir: "/data/db")
service_start(name: "mongodb")
` + "```" + `

**Back up the database before a risky migration:**
` + "```" + `
db_export(output: "/tmp/myapp-backup.sql")
artisan(args: ["migrate"])
` + "```" + `

**Diagnose PHP errors:**
` + "```" + `
logs()                  // current site's PHP-FPM errors (no target needed)
logs(target: "nginx")   // nginx errors
` + "```" + `

**Site isn't loading — check service health first:**
` + "```" + `
status()    // see which of DNS / nginx / PHP-FPM / watcher is down
` + "```" + `

**Free up resources — pause sites you're not using:**
` + "```" + `
sites()                          // see all sites
site_pause(site: "old-project")  // stop workers + replace vhost with landing page
// ... later ...
site_unpause(site: "old-project")  // restore and restart
` + "```" + `

**Keep a service always running regardless of active site:**
` + "```" + `
service_pin(name: "mysql")    // never auto-stopped
service_pin(name: "redis")
` + "```" + `

**User reports setup issues or something unexpected:**
` + "```" + `
doctor()    // full diagnostic: podman, systemd, DNS, ports, images, config
` + "```" + `

**Start a framework worker (Symfony Messenger, Laravel Horizon, etc.):**
` + "```" + `
worker_list(site: "myapp")            // see what workers are available and their status
worker_start(site: "myapp", worker: "messenger")  // start by name
worker_stop(site: "myapp", worker: "messenger")
` + "```" + `

**Add a custom worker to Laravel (e.g. Horizon):**
` + "```" + `
framework_add(name: "laravel", workers: {
  "horizon": {"label": "Horizon", "command": "php artisan horizon", "restart": "always"}
})
worker_start(site: "myapp", worker: "horizon")
` + "```" + `

**Work with failed queue jobs:**
` + "```" + `
artisan(args: ["queue:failed"])
artisan(args: ["queue:retry", "all"])
` + "```" + `

**Generate and run a new migration:**
` + "```" + `
artisan(args: ["make:migration", "add_status_to_orders"])
// ... edit the migration file ...
artisan(args: ["migrate"])
` + "```" + `
`

// junieGuidelinesSection is the lerd block written into .junie/guidelines.md.
// It is wrapped in sentinel comments by mergeJunieGuidelines so it can be
// cleanly updated on subsequent mcp:inject runs.
const junieGuidelinesSection = `## Lerd — Laravel Local Dev Environment

This project runs on **lerd**, a Podman-based Laravel development environment. The ` + bt + `lerd` + bt + ` MCP server is available — use it to manage the environment without leaving the chat.

### Architecture

- PHP runs in Podman containers named ` + bt + `lerd-php<version>-fpm` + bt + ` (e.g. ` + bt + `lerd-php84-fpm` + bt + `); each container includes composer and node/npm
- Nginx routes ` + bt + `*.test` + bt + ` domains to the correct PHP-FPM container
- Services (MySQL, Redis, PostgreSQL, etc.) and custom services run as Podman containers via systemd quadlets
- Node.js versions are managed by fnm; per-project version is set via a ` + bt + `.node-version` + bt + ` file
- Framework workers (queue, schedule, reverb, horizon, messenger, etc.) run as systemd user services named ` + bt + `lerd-<worker>-<sitename>` + bt + `; commands are defined per-framework in YAML definitions; Laravel Horizon is auto-detected from ` + bt + `composer.json` + bt + ` and replaces the queue toggle when installed
- Git worktrees automatically get a ` + bt + `<branch>.<site>.test` + bt + ` subdomain; ` + bt + `vendor/` + bt + `, ` + bt + `node_modules/` + bt + `, and ` + bt + `.env` + bt + ` are symlinked/copied from the main checkout

### Available MCP tools

| Tool | What it does |
|------|-------------|
| ` + bt + `sites` + bt + ` | List all registered sites with framework and worker status — call this first |
| ` + bt + `runtime_versions` + bt + ` | List installed PHP and Node.js versions with defaults |
| ` + bt + `artisan` + bt + ` | Run ` + bt + `php artisan` + bt + ` inside the PHP-FPM container |
| ` + bt + `composer` + bt + ` | Run ` + bt + `composer` + bt + ` inside the PHP-FPM container |
| ` + bt + `node_install` + bt + ` | Install a Node.js version via fnm (e.g. ` + bt + `"20"` + bt + `, ` + bt + `"lts"` + bt + `) |
| ` + bt + `node_uninstall` + bt + ` | Uninstall a Node.js version via fnm |
| ` + bt + `env_setup` + bt + ` | Configure ` + bt + `.env` + bt + ` for lerd: detects services, starts them, creates DB, generates APP_KEY |
| ` + bt + `site_link` + bt + ` | Register a directory as a lerd site (creates nginx vhost + ` + bt + `.test` + bt + ` domain) |
| ` + bt + `site_unlink` + bt + ` | Unregister a site and remove its nginx vhost |
| ` + bt + `secure` + bt + ` | Enable HTTPS for a site (mkcert) — updates APP_URL automatically |
| ` + bt + `unsecure` + bt + ` | Disable HTTPS for a site |
| ` + bt + `xdebug_on` + bt + ` | Enable Xdebug for a PHP version (port 9003) |
| ` + bt + `xdebug_off` + bt + ` | Disable Xdebug for a PHP version |
| ` + bt + `xdebug_status` + bt + ` | Show Xdebug state for all PHP versions |
| ` + bt + `service_start` + bt + ` | Start a built-in or custom service |
| ` + bt + `service_stop` + bt + ` | Stop a service |
| ` + bt + `service_add` + bt + ` | Register a new custom OCI service (MongoDB, RabbitMQ, …); supports ` + bt + `depends_on` + bt + ` for service dependencies |
| ` + bt + `service_remove` + bt + ` | Stop and deregister a custom service |
| ` + bt + `service_expose` + bt + ` | Add or remove an extra published port on a built-in service (persisted) |
| ` + bt + `service_env` + bt + ` | Return the recommended ` + bt + `.env` + bt + ` connection variables for a service |
| ` + bt + `db_export` + bt + ` | Export a database to a SQL dump file (defaults to site DB from ` + bt + `.env` + bt + `) |
| ` + bt + `queue_start` + bt + ` | Start the queue worker for a site (any framework with a queue worker) |
| ` + bt + `queue_stop` + bt + ` | Stop the queue worker |
| ` + bt + `horizon_start` + bt + ` | Start Laravel Horizon for a site (use instead of queue_start when laravel/horizon is installed) |
| ` + bt + `horizon_stop` + bt + ` | Stop Laravel Horizon |
| ` + bt + `reverb_start` + bt + ` | Start the Reverb WebSocket server for a site |
| ` + bt + `reverb_stop` + bt + ` | Stop the Reverb server |
| ` + bt + `schedule_start` + bt + ` | Start the task scheduler for a site |
| ` + bt + `schedule_stop` + bt + ` | Stop the task scheduler |
| ` + bt + `worker_start` + bt + ` | Start any named framework worker (e.g. messenger, pulse) |
| ` + bt + `worker_stop` + bt + ` | Stop a named framework worker |
| ` + bt + `worker_list` + bt + ` | List all workers defined for a site's framework with running status |
| ` + bt + `project_new` + bt + ` | Scaffold a new PHP project (runs the framework's create command); follow with ` + bt + `site_link` + bt + ` + ` + bt + `env_setup` + bt + ` |
| ` + bt + `framework_list` + bt + ` | List all framework definitions with their workers |
| ` + bt + `framework_add` + bt + ` | Add or update a framework definition; use ` + bt + `name: "laravel"` + bt + ` to add custom workers to Laravel |
| ` + bt + `framework_remove` + bt + ` | Remove a user-defined framework; for laravel removes only custom worker additions |
| ` + bt + `site_php` + bt + ` | Change PHP version for a site — writes ` + bt + `.php-version` + bt + `, updates registry, regenerates nginx vhost |
| ` + bt + `site_node` + bt + ` | Change Node.js version for a site — writes ` + bt + `.node-version` + bt + `, installs via fnm if needed |
| ` + bt + `site_pause` + bt + ` | Pause a site: stop all its workers and replace its vhost with a landing page |
| ` + bt + `site_unpause` + bt + ` | Resume a paused site: restore its vhost and restart previously running workers |
| ` + bt + `service_pin` + bt + ` | Pin a service so it is never auto-stopped even when no sites reference it |
| ` + bt + `service_unpin` + bt + ` | Unpin a service so it can be auto-stopped when unused |
| ` + bt + `stripe_listen` + bt + ` | Start a Stripe webhook listener for a site |
| ` + bt + `stripe_listen_stop` + bt + ` | Stop the Stripe webhook listener |
| ` + bt + `logs` + bt + ` | Fetch container logs — defaults to current site's FPM; optionally specify nginx, service name, PHP version, or site name |
| ` + bt + `status` + bt + ` | Health snapshot of DNS, nginx, PHP-FPM containers, and the file watcher |
| ` + bt + `doctor` + bt + ` | Full diagnostic: podman, systemd, DNS, ports, PHP images, config, updates |

### Key conventions

- ` + bt + `path` + bt + ` argument is optional on most tools — defaults to the directory the AI assistant was opened in (cwd), then ` + bt + `LERD_SITE_PATH` + bt + ` if set; you can almost always omit it
- ` + bt + `artisan` + bt + ` and ` + bt + `composer` + bt + ` take ` + bt + `path` + bt + ` (absolute project root) and ` + bt + `args` + bt + ` (array)
- ` + bt + `tinker` + bt + ` must use ` + bt + `--execute=<code>` + bt + ` for non-interactive use
- Built-in service hosts follow the pattern ` + bt + `lerd-<name>` + bt + ` (e.g. ` + bt + `lerd-mysql` + bt + `, ` + bt + `lerd-redis` + bt + `)
- Default DB credentials: username ` + bt + `root` + bt + `, password ` + bt + `lerd` + bt + `
- ` + bt + `service_stop` + bt + ` marks the service paused — ` + bt + `lerd start` + bt + ` skips it until explicitly started again
- ` + bt + `queue_start` + bt + ` requires Redis to be running when ` + bt + `QUEUE_CONNECTION=redis` + bt + `; call ` + bt + `service_start(name: "redis")` + bt + ` first
- If ` + bt + `sites` + bt + ` returns ` + bt + `has_horizon: true` + bt + ` for a site, use ` + bt + `horizon_start` + bt + ` / ` + bt + `horizon_stop` + bt + ` instead of ` + bt + `queue_start` + bt + ` / ` + bt + `queue_stop` + bt + ` — Horizon manages queues and they are mutually exclusive
- Use ` + bt + `worker_list` + bt + ` first to discover what workers are available for a site before calling ` + bt + `worker_start` + bt + `
- Worker unit names follow the pattern ` + bt + `lerd-<worker>-<site>` + bt + ` (e.g. ` + bt + `lerd-messenger-myapp` + bt + `, ` + bt + `lerd-horizon-myapp` + bt + `)
- ` + bt + `site_php` + bt + ` / ` + bt + `site_node` + bt + ` change the PHP/Node version for a site; the FPM container for the new PHP version must be running after calling ` + bt + `site_php` + bt + `
- ` + bt + `site_pause` + bt + ` / ` + bt + `site_unpause` + bt + ` free up resources for sites not in active use without unlinking them; paused state persists across restarts
- ` + bt + `service_pin` + bt + ` keeps a service always running regardless of which sites are active; use for shared services like MySQL or Redis
- ` + bt + `service_add` + bt + ` supports ` + bt + `depends_on` + bt + ` (array of service names): starting a dependency auto-starts the dependent service; stopping a dependency cascade-stops the dependent first; starting the dependent ensures dependencies start first
- ` + bt + `project_new` + bt + ` requires an absolute ` + bt + `path` + bt + ` and runs the framework's ` + bt + `create` + bt + ` command; follow it with ` + bt + `site_link` + bt + ` + ` + bt + `env_setup` + bt + ` to register and configure the new project
`
