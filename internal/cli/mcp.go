package cli

import (
	"encoding/json"
	"fmt"
	"os"
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

## Architecture

- PHP runs inside Podman containers named ` + bt + `lerd-php<version>-fpm` + bt + ` (e.g. ` + bt + `lerd-php84-fpm` + bt + `)
- Each PHP-FPM container includes **composer** and **node/npm** so you can run all tooling without leaving the container
- Nginx routes ` + bt + `*.test` + bt + ` domains to the appropriate FPM container
- Services (MySQL, Redis, PostgreSQL, etc.) run as Podman containers via systemd quadlets
- Custom services (MongoDB, RabbitMQ, …) can be added with ` + bt + `service_add` + bt + ` and managed identically to built-in ones
- Node.js versions are managed by **fnm** (Fast Node Manager); pin per-project with a ` + bt + `.node-version` + bt + ` file
- Queue workers run as systemd user services named ` + bt + `lerd-queue-<sitename>` + bt + `
- Git worktrees automatically get a ` + bt + `<branch>.<site>.test` + bt + ` subdomain; ` + bt + `vendor/` + bt + `, ` + bt + `node_modules/` + bt + `, and ` + bt + `.env` + bt + ` are symlinked/copied from the main checkout
- DNS resolves ` + bt + `*.test` + bt + ` to ` + bt + `127.0.0.1` + bt + `

## Available MCP Tools

### ` + bt + `sites` + bt + `
List all registered lerd sites with domains, paths, PHP versions, Node versions, and queue status. **Call this first** to find site names and paths needed by other tools.

### ` + bt + `runtime_versions` + bt + `
List all installed PHP and Node.js versions and the configured defaults. Call this to check what runtimes are available before running commands.

### ` + bt + `artisan` + bt + `
Run ` + bt + `php artisan` + bt + ` inside the PHP-FPM container for the project. Arguments:
- ` + bt + `path` + bt + ` (optional): absolute path to the Laravel project root — defaults to ` + bt + `LERD_SITE_PATH` + bt + ` set by ` + bt + `mcp:inject` + bt + `
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
- ` + bt + `path` + bt + ` (optional): absolute path to the Laravel project root — defaults to ` + bt + `LERD_SITE_PATH` + bt + ` set by ` + bt + `mcp:inject` + bt + `
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

Built-in names: ` + bt + `mysql` + bt + `, ` + bt + `redis` + bt + `, ` + bt + `postgres` + bt + `, ` + bt + `meilisearch` + bt + `, ` + bt + `minio` + bt + `, ` + bt + `mailpit` + bt + `. Custom service names (registered with ` + bt + `service_add` + bt + `) are also accepted — just pass the same name used in ` + bt + `service_add` + bt + `.

**.env values for built-in lerd services:**

| Service | Host | Key vars |
|---------|------|----------|
| mysql | ` + bt + `lerd-mysql` + bt + ` | ` + bt + `DB_CONNECTION=mysql` + bt + `, ` + bt + `DB_PASSWORD=lerd` + bt + ` |
| postgres | ` + bt + `lerd-postgres` + bt + ` | ` + bt + `DB_CONNECTION=pgsql` + bt + `, ` + bt + `DB_PASSWORD=lerd` + bt + ` |
| redis | ` + bt + `lerd-redis` + bt + ` | ` + bt + `REDIS_PASSWORD=null` + bt + ` |
| mailpit | ` + bt + `lerd-mailpit:1025` + bt + ` | web UI: http://localhost:8025 |
| meilisearch | ` + bt + `lerd-meilisearch:7700` + bt + ` | |
| minio | ` + bt + `lerd-minio:9000` + bt + ` | ` + bt + `AWS_USE_PATH_STYLE_ENDPOINT=true` + bt + ` |

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

` + bt + `service_remove` + bt + ` stops and deregisters a custom service. Persistent data is NOT deleted.

### ` + bt + `env_setup` + bt + `
Configure the project's ` + bt + `.env` + bt + ` for lerd in one call:
- Creates ` + bt + `.env` + bt + ` from ` + bt + `.env.example` + bt + ` if it doesn't exist
- Detects which services (MySQL, Redis, …) the project uses and sets lerd connection values
- Starts any referenced services that aren't running
- Creates the project database (and ` + bt + `<name>_testing` + bt + ` database)
- Generates ` + bt + `APP_KEY` + bt + ` if missing
- Sets ` + bt + `APP_URL` + bt + ` to the registered ` + bt + `.test` + bt + ` domain

Arguments:
- ` + bt + `path` + bt + ` (optional): absolute path to the Laravel project root — defaults to ` + bt + `LERD_SITE_PATH` + bt + ` set by ` + bt + `mcp:inject` + bt + `

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
Start or stop a Laravel queue worker for a site. The worker runs ` + bt + `php artisan queue:work` + bt + ` in the FPM container as a systemd service.

> **Redis queues:** if the project's ` + bt + `.env` + bt + ` has ` + bt + `QUEUE_CONNECTION=redis` + bt + `, lerd will refuse to start the worker unless ` + bt + `lerd-redis` + bt + ` is running. Call ` + bt + `service_start(name: "redis")` + bt + ` first.

Arguments for ` + bt + `queue_start` + bt + `:
- ` + bt + `site` + bt + ` (required): site name from ` + bt + `sites` + bt + ` tool
- ` + bt + `queue` + bt + ` (optional): queue name, default ` + bt + `"default"` + bt + `
- ` + bt + `tries` + bt + ` (optional): max job attempts, default ` + bt + `3` + bt + `
- ` + bt + `timeout` + bt + ` (optional): job timeout in seconds, default ` + bt + `60` + bt + `

### ` + bt + `reverb_start` + bt + ` / ` + bt + `reverb_stop` + bt + `
Start or stop the Laravel Reverb WebSocket server for a site. The server runs ` + bt + `php artisan reverb:start` + bt + ` inside the FPM container as a systemd service. Takes ` + bt + `site` + bt + ` (required, site name from ` + bt + `sites` + bt + ` tool).

### ` + bt + `schedule_start` + bt + ` / ` + bt + `schedule_stop` + bt + `
Start or stop the Laravel task scheduler for a site. Runs ` + bt + `php artisan schedule:work` + bt + ` inside the FPM container as a systemd service. Takes ` + bt + `site` + bt + ` (required, site name from ` + bt + `sites` + bt + ` tool).

### ` + bt + `stripe_listen` + bt + ` / ` + bt + `stripe_listen_stop` + bt + `
Start or stop a Stripe webhook listener for a site using the Stripe CLI container. Reads ` + bt + `STRIPE_SECRET` + bt + ` from the site's ` + bt + `.env` + bt + ` and forwards webhooks to ` + bt + `/stripe/webhook` + bt + ` by default.

Arguments for ` + bt + `stripe_listen` + bt + `:
- ` + bt + `site` + bt + ` (required): site name from ` + bt + `sites` + bt + ` tool
- ` + bt + `api_key` + bt + ` (optional): Stripe secret key (defaults to ` + bt + `STRIPE_SECRET` + bt + ` in the site's ` + bt + `.env` + bt + `)
- ` + bt + `webhook_path` + bt + ` (optional): webhook route path (default: ` + bt + `"/stripe/webhook"` + bt + `)

### ` + bt + `db_export` + bt + `
Export the project's database to a SQL dump file. Arguments:
- ` + bt + `path` + bt + ` (optional): absolute path to the Laravel project root — defaults to ` + bt + `LERD_SITE_PATH` + bt + ` set by ` + bt + `mcp:inject` + bt + `
- ` + bt + `output` + bt + ` (optional): output file path (defaults to ` + bt + `<database>.sql` + bt + ` in the project root)

### ` + bt + `logs` + bt + `
Fetch recent container logs. Target can be:
- ` + bt + `"nginx"` + bt + ` — nginx proxy logs
- Service name: ` + bt + `"mysql"` + bt + `, ` + bt + `"redis"` + bt + `, or any custom service name
- PHP version: ` + bt + `"8.4"` + bt + ` — logs for that PHP-FPM container
- Site name — logs for the site's PHP-FPM container

Optional ` + bt + `lines` + bt + ` parameter (default: 50).

## Common Workflows

**Check installed runtimes before starting:**
` + "```" + `
runtime_versions()   // see PHP and Node.js versions available
` + "```" + `

**Set up a brand-new cloned project (full flow):**
` + "```" + `
site_link()                          // registers LERD_SITE_PATH as a lerd site
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
logs(target: "8.4")     // PHP-FPM errors
logs(target: "nginx")   // nginx errors
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
- Queue workers run as systemd user services named ` + bt + `lerd-queue-<sitename>` + bt + `
- Git worktrees automatically get a ` + bt + `<branch>.<site>.test` + bt + ` subdomain; ` + bt + `vendor/` + bt + `, ` + bt + `node_modules/` + bt + `, and ` + bt + `.env` + bt + ` are symlinked/copied from the main checkout

### Available MCP tools

| Tool | What it does |
|------|-------------|
| ` + bt + `sites` + bt + ` | List all registered sites — call this first to find paths and site names |
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
| ` + bt + `service_add` + bt + ` | Register a new custom OCI service (MongoDB, RabbitMQ, …) |
| ` + bt + `service_remove` + bt + ` | Stop and deregister a custom service |
| ` + bt + `db_export` + bt + ` | Export the project database to a SQL dump file |
| ` + bt + `queue_start` + bt + ` | Start a queue worker for a site |
| ` + bt + `queue_stop` + bt + ` | Stop a queue worker |
| ` + bt + `reverb_start` + bt + ` | Start the Laravel Reverb WebSocket server for a site |
| ` + bt + `reverb_stop` + bt + ` | Stop the Reverb server |
| ` + bt + `schedule_start` + bt + ` | Start the Laravel task scheduler for a site |
| ` + bt + `schedule_stop` + bt + ` | Stop the task scheduler |
| ` + bt + `stripe_listen` + bt + ` | Start a Stripe webhook listener for a site |
| ` + bt + `stripe_listen_stop` + bt + ` | Stop the Stripe webhook listener |
| ` + bt + `logs` + bt + ` | Fetch container logs (nginx, service name, PHP version, or site name) |

### Key conventions

- ` + bt + `artisan` + bt + ` and ` + bt + `composer` + bt + ` take ` + bt + `path` + bt + ` (absolute project root) and ` + bt + `args` + bt + ` (array)
- ` + bt + `tinker` + bt + ` must use ` + bt + `--execute=<code>` + bt + ` for non-interactive use
- Built-in service hosts follow the pattern ` + bt + `lerd-<name>` + bt + ` (e.g. ` + bt + `lerd-mysql` + bt + `, ` + bt + `lerd-redis` + bt + `)
- Default DB credentials: username ` + bt + `root` + bt + `, password ` + bt + `lerd` + bt + `
- ` + bt + `service_stop` + bt + ` marks the service paused — ` + bt + `lerd start` + bt + ` skips it until explicitly started again
- ` + bt + `queue_start` + bt + ` requires Redis to be running when ` + bt + `QUEUE_CONNECTION=redis` + bt + `; call ` + bt + `service_start(name: "redis")` + bt + ` first
`
