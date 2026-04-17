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
(Claude Code, Cursor, JetBrains Junie, etc.) to manage lerd sites, run artisan
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

  .mcp.json                     MCP server config for Claude Code
  .claude/skills/lerd/SKILL.md  Claude Code skill (lerd tools reference)
  .cursor/mcp.json              MCP server config for Cursor
  .cursor/rules/lerd.mdc        Cursor rules file (lerd tools reference)
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

	// .cursor/mcp.json — Cursor
	cursorPath := filepath.Join(abs, ".cursor", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(cursorPath), 0755); err != nil {
		return fmt.Errorf("creating .cursor: %w", err)
	}
	if err := mergeMCPServersJSON(cursorPath, lerdEntry); err != nil {
		return err
	}
	fmt.Printf("  updated .cursor/mcp.json\n")

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

	// .cursor/rules/lerd.mdc — Cursor rules file (always overwrite, we own it)
	cursorRulesPath := filepath.Join(abs, ".cursor", "rules", "lerd.mdc")
	if err := os.MkdirAll(filepath.Dir(cursorRulesPath), 0755); err != nil {
		return fmt.Errorf("creating .cursor/rules: %w", err)
	}
	if err := os.WriteFile(cursorRulesPath, []byte(cursorRulesContent), 0644); err != nil {
		return fmt.Errorf("writing lerd.mdc: %w", err)
	}
	fmt.Printf("  wrote   .cursor/rules/lerd.mdc\n")

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
  ~/.cursor/mcp.json            Cursor global MCP config
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

	// Cursor global.
	cursorPath := filepath.Join(home, ".cursor", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(cursorPath), 0755); err != nil {
		return fmt.Errorf("creating ~/.cursor: %w", err)
	}
	if err := mergeMCPServersJSON(cursorPath, lerdEntry); err != nil {
		return err
	}
	fmt.Println("  updated ~/.cursor/mcp.json")

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
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
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
description: Manage the lerd local PHP development environment — run framework console commands (artisan, bin/console, etc.), manage services, start/stop queue workers, run composer, manage Node.js versions, and inspect site status via MCP tools.
---
# Lerd — Laravel Local Dev Environment

This project runs on **lerd**, a Podman-based Laravel development environment for Linux (similar to Laravel Herd). The ` + bt + `lerd` + bt + ` MCP server exposes tools to manage it directly from your AI assistant.

## Path resolution

Tools that accept a ` + bt + `path` + bt + ` argument (` + bt + `artisan` + bt + `, ` + bt + `composer` + bt + `, ` + bt + `env_setup` + bt + `, ` + bt + `env_check` + bt + `, ` + bt + `db_set` + bt + `, ` + bt + `site_link` + bt + `, ` + bt + `site_unlink` + bt + `, ` + bt + `site_domain_add` + bt + `, ` + bt + `site_domain_remove` + bt + `, ` + bt + `db_export` + bt + `, ` + bt + `db_import` + bt + `, ` + bt + `db_create` + bt + `, etc.) resolve it in this order:
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
- Worker commands are defined per-framework in YAML definitions; Laravel has built-in queue/schedule/reverb workers; custom frameworks can add any workers; both workers and setup commands support an optional ` + bt + `check` + bt + ` field (` + bt + `file` + bt + ` or ` + bt + `composer` + bt + `) to conditionally show them based on project dependencies
- Framework definitions can include ` + bt + `setup` + bt + ` commands (one-off bootstrap steps like migrations, storage links) shown in ` + bt + `lerd setup` + bt + `; Laravel has built-in storage:link/migrate/db:seed
- **Custom containers**: non-PHP sites (Node.js, Python, Go, etc.) can define a ` + bt + `Containerfile.lerd` + bt + ` and a ` + bt + `container:` + bt + ` section in ` + bt + `.lerd.yaml` + bt + ` with a port. Lerd builds a per-project image (` + bt + `lerd-custom-<sitename>:local` + bt + `), runs it as ` + bt + `lerd-custom-<sitename>` + bt + `, and nginx reverse-proxies to it. Workers exec into the custom container. Services are accessible by name (` + bt + `lerd-mysql` + bt + `, ` + bt + `lerd-redis` + bt + `, etc.) on the shared ` + bt + `lerd` + bt + ` Podman network.
- Git worktrees automatically get a ` + bt + `<branch>.<site>.test` + bt + ` subdomain; ` + bt + `vendor/` + bt + `, ` + bt + `node_modules/` + bt + `, and ` + bt + `.env` + bt + ` are symlinked/copied from the main checkout
- DNS resolves ` + bt + `*.test` + bt + ` to ` + bt + `127.0.0.1` + bt + `

## Available MCP Tools

### ` + bt + `sites` + bt + `
List all registered lerd sites with domains, paths, PHP versions, Node versions, and queue status. **Call this first** to find site names and paths needed by other tools.

### ` + bt + `runtime_versions` + bt + `
List all installed PHP and Node.js versions and the configured defaults. Call this to check what runtimes are available before running commands.

### ` + bt + `php_list` + bt + `
List all PHP versions installed by lerd as JSON, with each version's ` + bt + `default` + bt + ` flag. Use this to confirm which versions are available before calling ` + bt + `site_php` + bt + `, ` + bt + `php_ext_add` + bt + `, or ` + bt + `xdebug_on` + bt + `.

### ` + bt + `php_ext_list` + bt + ` / ` + bt + `php_ext_add` + bt + ` / ` + bt + `php_ext_remove` + bt + `
Manage custom PHP extensions for a PHP version. Extensions are added on top of the bundled lerd FPM image. Adding or removing an extension rebuilds the image and restarts the FPM container (may take a minute).

Optional ` + bt + `version` + bt + ` argument on all three — defaults to the project or global PHP version.

` + bt + `php_ext_add` + bt + ` and ` + bt + `php_ext_remove` + bt + ` take ` + bt + `extension` + bt + ` (required).

Examples:
` + "```" + `
php_ext_list()                              // list extensions for current project's PHP version
php_ext_list(version: "8.4")               // list extensions for 8.4
php_ext_add(extension: "imagick")          // add imagick to current project's PHP version
php_ext_add(extension: "redis", version: "8.3")
php_ext_remove(extension: "imagick")
` + "```" + `

### ` + bt + `artisan` + bt + ` (Laravel only)
Run ` + bt + `php artisan` + bt + ` inside the PHP-FPM container for the project. Only available when the site is detected as Laravel. Arguments:
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

### ` + bt + `console` + bt + ` (non-Laravel frameworks)
Run the framework's console command (e.g. ` + bt + `php bin/console` + bt + ` for Symfony) inside the PHP-FPM container. Only available for non-Laravel frameworks that define a ` + bt + `console` + bt + ` field in their YAML definition. Arguments:
- ` + bt + `path` + bt + ` (optional): absolute path to the project root — defaults to the current working directory (or ` + bt + `LERD_SITE_PATH` + bt + ` if set by ` + bt + `mcp:inject` + bt + `)
- ` + bt + `args` + bt + ` (required): console arguments as an array

Example — Symfony:
` + "```" + `
console(args: ["cache:clear"])
console(args: ["doctrine:migrations:migrate"])
console(args: ["messenger:consume", "async", "--time-limit=60"])
` + "```" + `

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

### ` + bt + `vendor_bins` + bt + ` / ` + bt + `vendor_run` + bt + `
Discover and execute composer-installed binaries from the project's ` + bt + `vendor/bin` + bt + ` directory inside the PHP-FPM container. Use ` + bt + `vendor_bins` + bt + ` first to see what tooling is available (pest, phpunit, pint, phpstan, rector, paratest, psalm, etc.), then ` + bt + `vendor_run` + bt + ` to invoke one. Both accept an optional ` + bt + `path` + bt + ` argument that defaults to the current site.

Arguments:
- ` + bt + `vendor_bins(path?)` + bt + ` — returns the sorted list of executables in ` + bt + `vendor/bin` + bt + `
- ` + bt + `vendor_run(path?, bin, args?)` + bt + ` — runs ` + bt + `php vendor/bin/<bin> [args]` + bt + ` inside the FPM container; ` + bt + `bin` + bt + ` must be a plain filename, not a path

Examples:
` + "```" + `
vendor_bins()                                      // list available tools
vendor_run(bin: "pest")                            // run the full pest suite
vendor_run(bin: "pest", args: ["--filter", "UserTest"])
vendor_run(bin: "phpunit", args: ["--testsuite", "Feature"])
vendor_run(bin: "pint", args: ["--test"])          // dry-run pint
vendor_run(bin: "phpstan", args: ["analyse", "--memory-limit=2G"])
vendor_run(bin: "rector", args: ["process", "--dry-run"])
` + "```" + `

Prefer ` + bt + `vendor_run` + bt + ` over ` + bt + `composer(args: ["exec", ...])` + bt + ` — it's faster, doesn't go through composer's plugin pipeline, and the same shortcut is available on the CLI as ` + bt + `lerd <bin>` + bt + ` (e.g. ` + bt + `lerd pest` + bt + `, ` + bt + `lerd pint` + bt + `).

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

### ` + bt + `service_preset_list` + bt + ` / ` + bt + `service_preset_install` + bt + `
Lerd ships a small catalogue of opt-in **service presets** — bundled YAML definitions for common dev services that become normal custom services once installed. Use ` + bt + `service_preset_list` + bt + ` to see what's available and ` + bt + `service_preset_install` + bt + ` to install one. Prefer this over hand-rolling ` + bt + `service_add` + bt + ` for anything in the catalogue: presets ship sane defaults, dependency wiring, dashboard URLs, and (where relevant) rendered config files.

Current catalogue: ` + bt + `phpmyadmin` + bt + ` (depends on built-in mysql), ` + bt + `pgadmin` + bt + ` (depends on built-in postgres, ships a pre-loaded servers.json + pgpass), ` + bt + `mongo` + bt + `, ` + bt + `mongo-express` + bt + ` (depends on the ` + bt + `mongo` + bt + ` preset), ` + bt + `selenium` + bt + ` (Chromium for browser testing — Dusk, Panther, etc.), ` + bt + `stripe-mock` + bt + `. Some presets (e.g. ` + bt + `mysql` + bt + `, ` + bt + `mariadb` + bt + `) declare multiple versions in a single family — pass ` + bt + `version` + bt + ` to pick one, otherwise lerd installs the family default.

Arguments:
- ` + bt + `service_preset_list()` + bt + ` — returns each preset with its image, declared versions, dependencies, dashboard URL, and an ` + bt + `installed` + bt + ` flag
- ` + bt + `service_preset_install(name, version?)` + bt + ` — installs a preset by name; ` + bt + `version` + bt + ` is required only for multi-version families when you want a specific tag

Examples:
` + "```" + `
service_preset_list()
service_preset_install(name: "phpmyadmin")           // adds phpmyadmin, mysql is built-in
service_preset_install(name: "mongo")                // install mongo first…
service_preset_install(name: "mongo-express")        // …then mongo-express (gated otherwise)
service_preset_install(name: "mysql", version: "8.4")
service_start(name: "phpmyadmin")                    // mysql is started automatically
` + "```" + `

**Dependency gating:** installing a preset whose dependency is another *custom* service (e.g. ` + bt + `mongo-express` + bt + ` on ` + bt + `mongo` + bt + `) is rejected with a clear error until the dependency is installed first. Built-in deps (mysql, postgres) are auto-satisfied.

Once installed, presets are normal custom services — manage them with ` + bt + `service_start` + bt + `, ` + bt + `service_stop` + bt + `, ` + bt + `service_remove` + bt + `, ` + bt + `service_expose` + bt + `, ` + bt + `service_pin` + bt + `.

### ` + bt + `service_env` + bt + `
Return the recommended Laravel ` + bt + `.env` + bt + ` connection variables for a service — built-in or custom — as a key/value map. Use this when you need to inspect or manually apply connection settings without running ` + bt + `env_setup` + bt + `.

### ` + bt + `env_setup` + bt + `
Configure the project's ` + bt + `.env` + bt + ` for lerd in one call:
- Creates ` + bt + `.env` + bt + ` from ` + bt + `.env.example` + bt + ` if it doesn't exist
- Detects which services (MySQL, Redis, …) the project uses and sets lerd connection values
- Starts any referenced services that aren't running
- Creates the project database (and ` + bt + `<name>_testing` + bt + ` database)
- Generates ` + bt + `APP_KEY` + bt + ` if missing
- Sets ` + bt + `APP_URL` + bt + ` (or the framework's URL key) using the precedence chain: ` + bt + `.lerd.yaml` + bt + ` ` + bt + `app_url` + bt + ` → ` + bt + `sites.yaml` + bt + ` ` + bt + `app_url` + bt + ` → default ` + bt + `<scheme>://<primary-domain>` + bt + ` — see "Custom APP_URL" below

Arguments:
- ` + bt + `path` + bt + ` (optional): absolute path to the Laravel project root — defaults to the current working directory (or ` + bt + `LERD_SITE_PATH` + bt + ` if set by ` + bt + `mcp:inject` + bt + `)

> Run this right after ` + bt + `site_link` + bt + ` when setting up a fresh project.
>
> **Database default:** on a fresh Laravel clone where ` + bt + `.env` + bt + ` still says ` + bt + `DB_CONNECTION=sqlite` + bt + `, ` + bt + `env_setup` + bt + ` leaves the database choice alone. Call ` + bt + `db_set` + bt + ` first to pick ` + bt + `mysql` + bt + ` / ` + bt + `postgres` + bt + ` / ` + bt + `sqlite` + bt + ` deliberately, then ` + bt + `env_setup` + bt + ` (or just ` + bt + `db_set` + bt + ` alone — it already runs the env step).

### ` + bt + `db_set` + bt + `
Pick the database for a Laravel project. Persists the choice to ` + bt + `.lerd.yaml` + bt + ` (replacing any prior database entry — the choice is exclusive), rewrites the relevant ` + bt + `DB_` + bt + ` keys in ` + bt + `.env` + bt + `, and provisions the backing storage:
- ` + bt + `sqlite` + bt + ` — sets ` + bt + `DB_CONNECTION=sqlite` + bt + ` and ` + bt + `DB_DATABASE=database/database.sqlite` + bt + `, creates the file if missing. No service is started.
- ` + bt + `mysql` + bt + ` — sets ` + bt + `DB_CONNECTION=mysql` + bt + ` and the ` + bt + `lerd-mysql` + bt + ` connection vars, starts ` + bt + `lerd-mysql` + bt + ` if needed, creates ` + bt + `<project>` + bt + ` and ` + bt + `<project>_testing` + bt + ` databases.
- ` + bt + `postgres` + bt + ` — sets ` + bt + `DB_CONNECTION=pgsql` + bt + ` and the ` + bt + `lerd-postgres` + bt + ` connection vars, starts ` + bt + `lerd-postgres` + bt + ` if needed, creates the project databases.

Arguments:
- ` + bt + `path` + bt + ` (optional): absolute path to the Laravel project root — defaults to ` + bt + `LERD_SITE_PATH` + bt + ` / cwd
- ` + bt + `database` + bt + ` (required): one of ` + bt + `"sqlite"` + bt + `, ` + bt + `"mysql"` + bt + `, ` + bt + `"postgres"` + bt + `

Examples:
` + "```" + `
db_set(database: "mysql")        // fresh Laravel clone, switch to MySQL
db_set(database: "postgres")     // switch from MySQL → PostgreSQL (removes mysql)
db_set(database: "sqlite")       // explicitly keep SQLite (and create the file)
` + "```" + `

> Use this **before** ` + bt + `env_setup` + bt + ` on a fresh Laravel project so the database lands in ` + bt + `.env` + bt + ` deliberately. Switching databases later via ` + bt + `db_set` + bt + ` removes the previous database entry from ` + bt + `.lerd.yaml` + bt + ` automatically.

### Custom ` + bt + `APP_URL` + bt + `
By default ` + bt + `env_setup` + bt + ` writes ` + bt + `APP_URL=<scheme>://<primary-domain>` + bt + ` (e.g. ` + bt + `http://myapp.test` + bt + `) on every run. Three-tier override chain when you need a different value:

1. ` + bt + `.lerd.yaml` + bt + ` ` + bt + `app_url` + bt + ` field — committed to the repo, applies to every machine. Use for path prefixes, ports, or unrelated hostnames the whole team should share.
2. ` + bt + `~/.local/share/lerd/sites.yaml` + bt + ` ` + bt + `app_url` + bt + ` field on the site entry — per-machine override, not committed.
3. The default ` + bt + `<scheme>://<primary-domain>` + bt + ` generator — used when neither override is set.

There is no MCP tool to set ` + bt + `app_url` + bt + ` programmatically; the user (or you) edit ` + bt + `.lerd.yaml` + bt + ` directly and re-run ` + bt + `env_setup` + bt + ` (or any command that runs ` + bt + `lerd env` + bt + ` internally) to apply it.

Example ` + bt + `.lerd.yaml` + bt + `:
` + "```" + `yaml
domains:
  - myapp
app_url: http://myapp.test/api
` + "```" + `

If the configured ` + bt + `app_url` + bt + ` happens to point at a domain that the conflict filter dropped, lerd silently falls through to the next precedence level so ` + bt + `.env` + bt + ` doesn't end up writing a hostname owned by another site.

### ` + bt + `env_check` + bt + `
Compare all ` + bt + `.env` + bt + ` files (` + bt + `.env` + bt + `, ` + bt + `.env.testing` + bt + `, ` + bt + `.env.local` + bt + `, …) against ` + bt + `.env.example` + bt + ` and return structured JSON with missing or extra keys. Useful for catching "works on my machine" bugs caused by env drift after pulling new code.

Returns: ` + bt + `{"in_sync": bool, "keys": [{key, in_example, files: {filename: bool}}], "out_of_sync_count": N}` + bt + `

Arguments:
- ` + bt + `path` + bt + ` (optional): absolute path to the project root — defaults to the current working directory (or ` + bt + `LERD_SITE_PATH` + bt + ` if set by ` + bt + `mcp:inject` + bt + `)

### ` + bt + `site_link` + bt + ` / ` + bt + `site_unlink` + bt + `
Register or unregister a directory as a lerd site. Arguments for ` + bt + `site_link` + bt + `:
- ` + bt + `path` + bt + ` (optional): absolute path to the project directory — defaults to ` + bt + `LERD_SITE_PATH` + bt + ` set by ` + bt + `mcp:inject` + bt + `
- ` + bt + `name` + bt + ` (optional): domain name without TLD (e.g. ` + bt + `"myapp"` + bt + ` becomes ` + bt + `myapp.test` + bt + `; defaults to directory name, cleaned up)

> **Non-PHP projects (Node.js, Python, Go, etc.):** a Containerfile and ` + bt + `.lerd.yaml` + bt + ` with a ` + bt + `container: {port: <N>}` + bt + ` section must exist **before** calling ` + bt + `site_link` + bt + `. The Containerfile can be named anything (` + bt + `Containerfile.lerd` + bt + ` is the default; set ` + bt + `container.containerfile` + bt + ` to point at a different name like ` + bt + `Dockerfile` + bt + `). Write ` + bt + `.lerd.yaml` + bt + ` directly (there is no MCP tool for this — see the custom container setup workflow in the Workflows section below), or ask the user to run ` + bt + `lerd init` + bt + ` which runs an interactive wizard and writes the file. Calling ` + bt + `site_link` + bt + ` without this config registers the site as a PHP-FPM site, which is wrong. If that happened, call ` + bt + `site_unlink` + bt + ` first, set up the files, then ` + bt + `site_link` + bt + ` again.

` + bt + `site_unlink` + bt + ` takes ` + bt + `path` + bt + ` (optional, same resolution as ` + bt + `site_link` + bt + `). Removes the site and all its domains. Project files are NOT deleted.

### ` + bt + `site_domain_add` + bt + ` / ` + bt + `site_domain_remove` + bt + `
Add or remove additional domains for a site. Each site can have multiple domains (all served by the same nginx vhost).
- ` + bt + `path` + bt + ` (optional): project directory
- ` + bt + `domain` + bt + ` (required): domain name without TLD (e.g. ` + bt + `"api"` + bt + ` becomes ` + bt + `api.test` + bt + `)

Cannot remove the last domain. When a site is secured, the TLS certificate is automatically reissued to cover all domains.

### ` + bt + `park` + bt + ` / ` + bt + `unpark` + bt + `
` + bt + `park` + bt + ` registers a parent directory: it scans every immediate subdirectory and auto-registers any PHP projects found as lerd sites. Use this when you keep many projects under one folder.

` + bt + `unpark` + bt + ` removes the registration and unlinks all sites whose paths are under that directory. Project files are NOT deleted.

Both take ` + bt + `path` + bt + ` (optional, defaults to LERD_SITE_PATH or cwd).

### ` + bt + `secure` + bt + ` / ` + bt + `unsecure` + bt + `
Enable or disable HTTPS for a site using a locally-trusted mkcert certificate. Both take ` + bt + `site` + bt + ` (site name). ` + bt + `APP_URL` + bt + ` in ` + bt + `.env` + bt + ` is updated automatically.

### ` + bt + `xdebug_on` + bt + ` / ` + bt + `xdebug_off` + bt + ` / ` + bt + `xdebug_status` + bt + `
Toggle Xdebug for a PHP version (restarts the FPM container). Optional ` + bt + `version` + bt + ` argument — defaults to the project or global PHP version. Xdebug listens on port ` + bt + `9003` + bt + ` at ` + bt + `host.containers.internal` + bt + `.

` + bt + `xdebug_on` + bt + ` accepts an optional ` + bt + `mode` + bt + ` argument (default ` + bt + `debug` + bt + `). Valid values: ` + bt + `debug` + bt + `, ` + bt + `coverage` + bt + `, ` + bt + `develop` + bt + `, ` + bt + `profile` + bt + `, ` + bt + `trace` + bt + `, ` + bt + `gcstats` + bt + `, or a comma-separated combo such as ` + bt + `debug,coverage` + bt + `. Use ` + bt + `coverage` + bt + ` for ` + bt + `phpunit --coverage` + bt + ` / ` + bt + `pest --coverage` + bt + ` when PCOV isn't available or is disabled. Calling ` + bt + `xdebug_on` + bt + ` with a different mode on an already-enabled version swaps modes without needing ` + bt + `xdebug_off` + bt + ` first.

` + bt + `xdebug_status` + bt + ` returns the enabled/disabled state and the active ` + bt + `mode` + bt + ` for all installed PHP versions.

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

### ` + bt + `worker_add` + bt + `
Add or update a custom worker for a project. Saves to ` + bt + `.lerd.yaml` + bt + ` ` + bt + `custom_workers` + bt + ` by default, or to the global framework overlay (` + bt + `~/.config/lerd/frameworks/` + bt + `) with ` + bt + `global: true` + bt + `. Does not auto-start — use ` + bt + `worker_start` + bt + ` afterwards.

Arguments:
- ` + bt + `site` + bt + ` (required): site name from ` + bt + `sites` + bt + ` tool
- ` + bt + `name` + bt + ` (required): worker name (slug, e.g. ` + bt + `"pdf-generator"` + bt + `)
- ` + bt + `command` + bt + ` (required): command to run inside the PHP-FPM container
- ` + bt + `label` + bt + `: human-readable label
- ` + bt + `restart` + bt + `: ` + bt + `"always"` + bt + ` or ` + bt + `"on-failure"` + bt + ` (default: always)
- ` + bt + `check_file` + bt + `: only show worker when this file exists
- ` + bt + `check_composer` + bt + `: only show worker when this Composer package is installed
- ` + bt + `conflicts_with` + bt + `: array of workers to stop before starting this one
- ` + bt + `global` + bt + `: save to global framework overlay instead of ` + bt + `.lerd.yaml` + bt + `

### ` + bt + `worker_remove` + bt + `
Remove a custom worker from a project's ` + bt + `.lerd.yaml` + bt + ` or global framework overlay. Stops the worker if running.

Arguments:
- ` + bt + `site` + bt + ` (required): site name from ` + bt + `sites` + bt + ` tool
- ` + bt + `name` + bt + ` (required): worker name to remove
- ` + bt + `global` + bt + `: remove from global framework overlay instead of ` + bt + `.lerd.yaml` + bt + `

### ` + bt + `project_new` + bt + `
Scaffold a new PHP project using a framework's create command. For Laravel, runs ` + bt + `composer create-project --no-install --no-plugins --no-scripts laravel/laravel <path>` + bt + `. Other frameworks must have a ` + bt + `create` + bt + ` field in their YAML definition.

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
List all available framework definitions (Laravel built-in plus any user-defined YAMLs at ` + bt + `~/.config/lerd/frameworks/` + bt + `), including their defined workers and setup commands. Call this before ` + bt + `framework_add` + bt + ` to see what already exists.

### ` + bt + `framework_add` + bt + `
Create or update a framework definition. For ` + bt + `laravel` + bt + `, only the ` + bt + `workers` + bt + ` and ` + bt + `setup` + bt + ` fields are accepted (built-in settings are always preserved). For other frameworks, creates a full definition.

Arguments:
- ` + bt + `name` + bt + ` (required): framework slug (e.g. ` + bt + `"symfony"` + bt + `). Use ` + bt + `"laravel"` + bt + ` to add custom workers to the built-in Laravel definition (e.g. ` + bt + `horizon` + bt + `, ` + bt + `pulse` + bt + `)
- ` + bt + `label` + bt + ` (optional): display name, e.g. ` + bt + `"Symfony"` + bt + `
- ` + bt + `public_dir` + bt + ` (optional): document root relative to project (default: ` + bt + `"public"` + bt + `)
- ` + bt + `detect_files` + bt + ` (optional): array of filenames that signal this framework
- ` + bt + `detect_packages` + bt + ` (optional): array of Composer packages that signal this framework
- ` + bt + `env_file` + bt + ` (optional): primary env file path (default: ` + bt + `".env"` + bt + `)
- ` + bt + `env_format` + bt + ` (optional): ` + bt + `"dotenv"` + bt + ` or ` + bt + `"php-const"` + bt + `
- ` + bt + `workers` + bt + ` (optional): map of worker name → ` + bt + `{label, command, restart, check}` + bt + ` — ` + bt + `check` + bt + ` is optional (` + bt + `{file}` + bt + ` or ` + bt + `{composer}` + bt + `), worker only shown when check passes
- ` + bt + `setup` + bt + ` (optional): array of one-off setup commands shown in ` + bt + `lerd setup` + bt + ` wizard, each with ` + bt + `{label, command, default, check}` + bt + ` — ` + bt + `check` + bt + ` is optional, same format as workers

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
Delete a user-defined framework YAML. For ` + bt + `laravel` + bt + `, removes only custom worker and setup command additions (built-in queue/schedule/reverb workers and storage:link/migrate/db:seed setup remain). Takes ` + bt + `name` + bt + ` (required).

### ` + bt + `site_php` + bt + ` / ` + bt + `site_node` + bt + `
Change the PHP or Node.js version for a registered site. Both take ` + bt + `site` + bt + ` (required) and ` + bt + `version` + bt + ` (required).

` + bt + `site_php` + bt + ` writes a ` + bt + `.php-version` + bt + ` pin file to the project root, updates the site registry, and regenerates the nginx vhost. The FPM container for the target PHP version must be running — start it with ` + bt + `service_start(name: "php<version>")` + bt + ` if needed.

` + bt + `site_node` + bt + ` writes a ` + bt + `.node-version` + bt + ` pin file and installs the version via fnm if it isn't already installed. Run ` + bt + `npm install` + bt + ` inside the project if dependencies need rebuilding against the new version.

### ` + bt + `site_pause` + bt + ` / ` + bt + `site_unpause` + bt + `
Pause or resume a site. Both take ` + bt + `site` + bt + ` (required, site name from ` + bt + `sites` + bt + ` tool).

` + bt + `site_pause` + bt + ` stops all running workers for the site, stops the custom container (for custom container sites), and replaces its nginx vhost with a landing page that includes a **Resume** button. Services no longer needed by any active site are auto-stopped. The paused state is persisted.

` + bt + `site_unpause` + bt + ` starts the custom container (if applicable), restores the nginx vhost, ensures required services are running, and restarts any workers that were running when the site was paused.

Use this to free up resources for sites you're not actively working on without fully unlinking them.

### ` + bt + `site_restart` + bt + `
Restart the container for a site without rebuilding the image. Takes ` + bt + `site` + bt + ` (required). For custom container sites this restarts the dedicated container; for PHP sites it restarts the shared FPM container.

### ` + bt + `site_rebuild` + bt + `
Rebuild the custom container image from the Containerfile and restart the container. Takes ` + bt + `site` + bt + ` (required). Use after changing the Containerfile. ` + bt + `site_link` + bt + ` reuses the cached image; ` + bt + `site_rebuild` + bt + ` forces a fresh build. Only works for custom container sites.

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
Export a database to a SQL dump file. Works with any project type — service and database are auto-detected. Arguments:
- ` + bt + `path` + bt + ` (optional): absolute path to the project root — defaults to the current working directory (or ` + bt + `LERD_SITE_PATH` + bt + ` if set by ` + bt + `mcp:inject` + bt + `)
- ` + bt + `service` + bt + ` (optional): lerd service name to target (e.g. ` + bt + `mysql` + bt + `, ` + bt + `postgres` + bt + `) — overrides auto-detection
- ` + bt + `database` + bt + ` (optional): database name to export — overrides auto-detection
- ` + bt + `output` + bt + ` (optional): output file path (defaults to ` + bt + `<database>.sql` + bt + ` in the project root)

### ` + bt + `db_import` + bt + `
Import a SQL dump file into the project database. Service and database are auto-detected; the service is started if not already running. Arguments:
- ` + bt + `file` + bt + ` (required): absolute path to the SQL file to import
- ` + bt + `path` + bt + ` (optional): absolute path to the project root — defaults to the current working directory
- ` + bt + `service` + bt + ` (optional): lerd service name to target — overrides auto-detection
- ` + bt + `database` + bt + ` (optional): database name to import into — overrides auto-detection

### ` + bt + `db_create` + bt + `
Create a database and a ` + bt + `<name>_testing` + bt + ` variant for the project. Service and database name are auto-detected; the service is started if not already running. Arguments:
- ` + bt + `path` + bt + ` (optional): absolute path to the project root
- ` + bt + `service` + bt + ` (optional): lerd service name to target — overrides auto-detection
- ` + bt + `name` + bt + ` (optional): database name — overrides auto-detection

### ` + bt + `logs` + bt + `
Fetch recent container logs. ` + bt + `target` + bt + ` is optional — when omitted, returns logs for the current site's PHP-FPM container (resolved from ` + bt + `LERD_SITE_PATH` + bt + `). Specify ` + bt + `target` + bt + ` only when you want a different container:
- ` + bt + `"nginx"` + bt + ` — nginx proxy logs
- Service name: ` + bt + `"mysql"` + bt + `, ` + bt + `"redis"` + bt + `, or any custom service name
- PHP version: ` + bt + `"8.4"` + bt + ` — logs for that PHP-FPM container
- Site name — logs for a different site's PHP-FPM container

Optional ` + bt + `lines` + bt + ` parameter (default: 50).

### ` + bt + `status` + bt + `
Return the health status of core lerd services as structured JSON: DNS resolution (ok + tld), nginx (running), PHP-FPM containers (running per version), and the file watcher (running). **Call this first when a site isn't loading** — it pinpoints which service is down before suggesting fixes.

### ` + bt + `which` + bt + `
Show the resolved PHP version, Node version, document root, and nginx config path for the current site. Call this to confirm which runtime versions a project will use before running commands.

Arguments:
- ` + bt + `path` + bt + ` (optional): absolute path to the project root — defaults to the current working directory (or ` + bt + `LERD_SITE_PATH` + bt + ` if set by ` + bt + `mcp:inject` + bt + `)

### ` + bt + `check` + bt + `
Validate a project's ` + bt + `.lerd.yaml` + bt + ` file. Returns structured JSON with per-field status (ok/warn/fail). Checks PHP version format and installation, service definitions (built-in, custom, inline), framework references, and worker configuration.

Returns: ` + bt + `{"valid": bool, "errors": N, "warnings": N, "items": [{name, status, detail}]}` + bt + `

Arguments:
- ` + bt + `path` + bt + ` (optional): absolute path to the project root containing ` + bt + `.lerd.yaml` + bt + ` — defaults to the current working directory (or ` + bt + `LERD_SITE_PATH` + bt + ` if set by ` + bt + `mcp:inject` + bt + `)

> **Use this before** ` + bt + `env_setup` + bt + ` or ` + bt + `site_link` + bt + ` to catch configuration errors early.

### ` + bt + `doctor` + bt + `
Run a full environment diagnostic. Returns structured JSON with per-check status (ok/warn/fail): podman, systemd, linger, dir writability, config validity, DNS resolution, nginx, PHP images, and update availability.

Returns: ` + bt + `{"version": "...", "checks": [{name, status, detail}], "failures": N, "warnings": N, "php_installed": [...], "php_default": "...", "node_default": "..."}` + bt + `

**Use this when the user reports setup issues or unexpected behaviour.**

## Common Workflows

**Check installed runtimes before starting:**
` + "```" + `
runtime_versions()   // see PHP and Node.js versions available
` + "```" + `

**Create a new Laravel project from scratch (global session, empty directory):**
` + "```" + `
composer(args: ["create-project", "laravel/laravel", "."])
site_link()           // registers the cwd as a lerd site
env_setup()           // configures .env, starts services, creates DB, generates APP_KEY (even before composer install)
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
xdebug_status()                                 // check current state and mode
xdebug_on(version: "8.4")                       // default mode=debug, restarts FPM
// ... debug ...
xdebug_off(version: "8.4")                      // disable when done (Xdebug adds overhead)
` + "```" + `

**Enable Xdebug coverage mode for phpunit/pest:**
` + "```" + `
xdebug_on(version: "8.4", mode: "coverage")     // swap mode without xdebug_off first
vendor_run(name: "pest", args: ["--coverage"])
xdebug_off(version: "8.4")
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

**Restore a database from a dump:**
` + "```" + `
db_import(file: "/tmp/myapp-backup.sql")
` + "```" + `

**Create databases for a new project manually:**
` + "```" + `
db_create()   // creates myapp + myapp_testing based on .env DB_DATABASE
` + "```" + `

**Check and manage PHP extensions:**
` + "```" + `
php_list()                           // see installed PHP versions
php_ext_list()                       // see custom extensions for current project's PHP version
php_ext_add(extension: "imagick")    // install imagick (rebuilds FPM image)
` + "```" + `

**Park a directory of projects:**
` + "```" + `
park(path: "/home/user/code")   // registers all PHP projects under ~/code as sites
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

**Restart a site's container (e.g. after changing Containerfile):**
` + "```" + `
site_restart(site: "nestjs-app")  // restarts container (no rebuild)
site_rebuild(site: "nestjs-app")  // rebuilds image from Containerfile + restarts
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

**Check which PHP and Node versions a site will use:**
` + "```" + `
which()   // shows resolved PHP, Node, document root, nginx config
` + "```" + `

**Validate project config before setup:**
` + "```" + `
check()   // validates .lerd.yaml syntax, services, PHP version
` + "```" + `

**Set up a custom container site (Node.js, Python, Go, etc.):**

1. Create a ` + bt + `Containerfile.lerd` + bt + ` in the project root (do NOT add WORKDIR or COPY — lerd volume-mounts the project directory at its host path and sets --workdir automatically):
` + "```dockerfile" + `
FROM node:20-alpine
RUN npm install -g nodemon
CMD ["npm", "run", "start:dev"]
` + "```" + `

   > **Hot-reload on macOS**: inotify events do not fire across Podman Machine's virtiofs mount. Use polling: nodemon needs ` + bt + `--legacy-watch` + bt + `, Vite needs ` + bt + `server.watch.usePolling: true` + bt + `, webpack needs ` + bt + `watchOptions: { poll: 1000 }` + bt + `. Example ` + bt + `package.json` + bt + `: ` + "`" + `"start:dev": "nodemon --legacy-watch src/main.js"` + "`" + `.

2. Write ` + bt + `.lerd.yaml` + bt + ` with the container section (there is no MCP tool for this — write the file directly, or ask the user to run ` + bt + `lerd init` + bt + ` which runs an interactive wizard and writes it):
` + "```yaml" + `
domains:
  - myapp
container:
  port: 3000
services:
  - mysql
  - redis
custom_workers:
  queue:
    label: Queue Worker
    command: node dist/queue.js
    restart: always
` + "```" + `

3. **Configure environment variables BEFORE linking.** The container starts immediately on ` + bt + `site_link` + bt + `, so the app's ` + bt + `.env` + bt + ` (or equivalent config) must already have the correct service connection strings. Lerd services are reachable by container name on the ` + bt + `lerd` + bt + ` network:
` + "```" + `
DB_HOST=lerd-mysql          # or lerd-postgres
DB_PORT=3306                # 5432 for postgres
DB_USERNAME=root            # postgres for postgres
DB_PASSWORD=lerd
REDIS_HOST=lerd-redis
REDIS_PORT=6379
` + "```" + `
   Start the services first if they're not running:
` + "```" + `
service_start(name: "mysql")
service_start(name: "redis")
` + "```" + `

4. Link and verify:
` + "```" + `
site_link()            // builds image, creates container, generates nginx vhost
sites()                // verify the site is listed with custom_container: true
` + "```" + `

The ` + bt + `container.port` + bt + ` field is required — it's the port the app listens on inside the container. ` + bt + `container.containerfile` + bt + ` defaults to ` + bt + `Containerfile.lerd` + bt + `. Workers defined in ` + bt + `custom_workers` + bt + ` exec into the custom container.

## .lerd.yaml Reference

` + bt + `.lerd.yaml` + bt + ` is the per-project config file, committed to the repo. ` + bt + `lerd link` + bt + ` and ` + bt + `lerd init` + bt + ` apply it automatically.

### PHP site fields

| Field | Description |
|-------|-------------|
| ` + bt + `domains` + bt + ` | Site hostnames without TLD (e.g. ` + bt + `[myapp, api]` + bt + `). First is primary. |
| ` + bt + `php_version` + bt + ` | PHP version for this project (e.g. ` + bt + `"8.4"` + bt + `) |
| ` + bt + `node_version` + bt + ` | Node version (e.g. ` + bt + `"22"` + bt + `) |
| ` + bt + `framework` + bt + ` | Framework name (e.g. ` + bt + `laravel` + bt + `, ` + bt + `symfony` + bt + `, ` + bt + `wordpress` + bt + `) |
| ` + bt + `secured` + bt + ` | ` + bt + `true` + bt + ` to enable HTTPS |
| ` + bt + `services` + bt + ` | Services to start (e.g. ` + bt + `[mysql, redis]` + bt + `) |
| ` + bt + `workers` + bt + ` | Active worker names (e.g. ` + bt + `[queue, schedule]` + bt + `) — auto-synced by start/stop |
| ` + bt + `app_url` + bt + ` | Override for APP_URL in ` + bt + `.env` + bt + ` |

### Custom container fields

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| ` + bt + `container.port` + bt + ` | yes | | Port the app listens on inside the container |
| ` + bt + `container.containerfile` + bt + ` | no | ` + bt + `Containerfile.lerd` + bt + ` | Path to the Containerfile (relative to project root) |
| ` + bt + `container.build_context` + bt + ` | no | ` + bt + `.` + bt + ` | Build context directory |
| ` + bt + `custom_workers` + bt + ` | no | | Worker definitions — see below |
| ` + bt + `domains` + bt + ` | no | | Same as PHP sites |
| ` + bt + `secured` + bt + ` | no | | Same as PHP sites |
| ` + bt + `services` + bt + ` | no | | Same as PHP sites |

When ` + bt + `container` + bt + ` is present, ` + bt + `php_version` + bt + `, ` + bt + `framework` + bt + `, and ` + bt + `node_version` + bt + ` are ignored — the container defines its own runtime.

### custom_workers fields

Each entry under ` + bt + `custom_workers` + bt + ` is a name-to-config map. Works for both PHP and custom container sites.

` + "```yaml" + `
custom_workers:
  queue:
    label: Queue Worker
    command: node dist/queue.js
    restart: always
  cron:
    label: Cron
    command: node dist/cron.js
    restart: on-failure
` + "```" + `

| Field | Required | Description |
|-------|----------|-------------|
| ` + bt + `label` + bt + ` | no | Display name in the UI |
| ` + bt + `command` + bt + ` | yes | Shell command to run inside the container |
| ` + bt + `restart` + bt + ` | no | ` + bt + `always` + bt + ` (default) or ` + bt + `on-failure` + bt + ` |
| ` + bt + `schedule` + bt + ` | no | systemd OnCalendar expression for cron-style workers (e.g. ` + bt + `minutely` + bt + `) |
| ` + bt + `conflicts_with` + bt + ` | no | List of worker names to stop before starting this one |
`

// junieGuidelinesSection is the lerd block written into .junie/guidelines.md.
// It is wrapped in sentinel comments by mergeJunieGuidelines so it can be
// cleanly updated on subsequent mcp:inject runs.
const junieGuidelinesSection = `## Lerd — Laravel Local Dev Environment

This project runs on **lerd**, a Podman-based Laravel development environment. The ` + bt + `lerd` + bt + ` MCP server is available — use it to manage the environment without leaving the chat.

### Architecture

- PHP runs in Podman containers named ` + bt + `lerd-php<version>-fpm` + bt + ` (e.g. ` + bt + `lerd-php84-fpm` + bt + `); each container includes composer and node/npm; the PHP version is resolved from ` + bt + `.lerd.yaml` + bt + ` → ` + bt + `.php-version` + bt + ` → ` + bt + `composer.json` + bt + ` ` + bt + `require.php` + bt + ` constraint (matched against installed versions) → global default
- Nginx routes ` + bt + `*.test` + bt + ` domains to the correct PHP-FPM container
- Services (MySQL, Redis, PostgreSQL, etc.) and custom services run as Podman containers via systemd quadlets
- Node.js versions are managed by fnm; per-project version is set via a ` + bt + `.node-version` + bt + ` file
- Framework workers (queue, schedule, reverb, horizon, messenger, etc.) run as systemd user services named ` + bt + `lerd-<worker>-<sitename>` + bt + `; commands are defined per-framework in YAML definitions; Laravel Horizon is auto-detected from ` + bt + `composer.json` + bt + ` and replaces the queue toggle when installed; both workers and setup commands support an optional ` + bt + `check` + bt + ` field (` + bt + `file` + bt + ` or ` + bt + `composer` + bt + `) to conditionally show them based on project dependencies; workers with ` + bt + `conflicts_with` + bt + ` auto-stop conflicting workers on start and hide them from the UI
- Custom workers can be added per-project (` + bt + `.lerd.yaml` + bt + ` ` + bt + `custom_workers` + bt + `) or globally (` + bt + `~/.config/lerd/frameworks/<name>.yaml` + bt + `); use ` + bt + `worker_add` + bt + ` / ` + bt + `worker_remove` + bt + ` — both survive framework store updates
- Framework setup commands (one-off bootstrap steps like migrations, storage links) are defined in the framework YAML and shown in ` + bt + `lerd setup` + bt + `; Laravel has built-in storage:link/migrate/db:seed; custom frameworks can define their own
- Service version placeholders (` + bt + `{{mysql_version}}` + bt + `, ` + bt + `{{postgres_version}}` + bt + `, ` + bt + `{{redis_version}}` + bt + `, ` + bt + `{{meilisearch_version}}` + bt + `) are available in framework env vars and are resolved from the service image tag at ` + bt + `lerd env` + bt + ` time
- **Custom containers**: non-PHP sites (Node.js, Python, Go, etc.) can define a ` + bt + `Containerfile.lerd` + bt + ` and a ` + bt + `container:` + bt + ` section in ` + bt + `.lerd.yaml` + bt + ` with a port; lerd builds a per-project image, runs it as ` + bt + `lerd-custom-<sitename>` + bt + `, and nginx reverse-proxies to it; the project directory is volume-mounted at its host path with ` + bt + `--workdir` + bt + ` set automatically — do NOT add ` + bt + `WORKDIR` + bt + ` or ` + bt + `COPY` + bt + ` to the Containerfile; workers exec into the custom container; services are accessible by name on the shared ` + bt + `lerd` + bt + ` Podman network; **hot-reload file watchers must use polling on macOS** (inotify does not fire across Podman Machine's virtiofs mount) — nodemon: ` + bt + `--legacy-watch` + bt + `, Vite: ` + bt + `server.watch.usePolling: true` + bt + `, webpack: ` + bt + `watchOptions: { poll: 1000 }` + bt + `
- Git worktrees automatically get a ` + bt + `<branch>.<site>.test` + bt + ` subdomain; ` + bt + `vendor/` + bt + `, ` + bt + `node_modules/` + bt + `, and ` + bt + `.env` + bt + ` are symlinked/copied from the main checkout

### Available MCP tools

| Tool | What it does |
|------|-------------|
| ` + bt + `sites` + bt + ` | List all registered sites with framework and worker status — call this first |
| ` + bt + `runtime_versions` + bt + ` | List installed PHP and Node.js versions with defaults |
| ` + bt + `php_list` + bt + ` | List installed PHP versions, marking the global default |
| ` + bt + `php_ext_list` + bt + ` | List custom PHP extensions for a PHP version |
| ` + bt + `php_ext_add` + bt + ` | Add a custom PHP extension — rebuilds FPM image and restarts container |
| ` + bt + `php_ext_remove` + bt + ` | Remove a custom PHP extension — rebuilds FPM image and restarts container |
| ` + bt + `artisan` + bt + ` | Run ` + bt + `php artisan` + bt + ` inside the PHP-FPM container (Laravel only) |
| ` + bt + `console` + bt + ` | Run the framework's console command (e.g. ` + bt + `php bin/console` + bt + ` for Symfony) — non-Laravel frameworks with a ` + bt + `console` + bt + ` field |
| ` + bt + `composer` + bt + ` | Run ` + bt + `composer` + bt + ` inside the PHP-FPM container |
| ` + bt + `vendor_bins` + bt + ` | List composer-installed binaries available in the project's ` + bt + `vendor/bin` + bt + ` directory |
| ` + bt + `vendor_run` + bt + ` | Run a binary from ` + bt + `vendor/bin` + bt + ` (pest, phpunit, pint, phpstan, rector, …) inside the PHP-FPM container |
| ` + bt + `node_install` + bt + ` | Install a Node.js version via fnm (e.g. ` + bt + `"20"` + bt + `, ` + bt + `"lts"` + bt + `) |
| ` + bt + `node_uninstall` + bt + ` | Uninstall a Node.js version via fnm |
| ` + bt + `env_setup` + bt + ` | Configure ` + bt + `.env` + bt + ` for lerd: detects services, starts them, creates DB, generates APP_KEY (leaves ` + bt + `DB_CONNECTION=sqlite` + bt + ` alone — call ` + bt + `db_set` + bt + ` first); ` + bt + `APP_URL` + bt + ` follows ` + bt + `.lerd.yaml app_url` + bt + ` → ` + bt + `sites.yaml app_url` + bt + ` → default chain |
| ` + bt + `db_set` + bt + ` | Pick the database for a Laravel project: ` + bt + `sqlite` + bt + ` / ` + bt + `mysql` + bt + ` / ` + bt + `postgres` + bt + `; persists to ` + bt + `.lerd.yaml` + bt + `, rewrites ` + bt + `DB_` + bt + ` keys in ` + bt + `.env` + bt + `, starts the service, creates the database |
| ` + bt + `env_check` + bt + ` | Compare all ` + bt + `.env` + bt + ` files against ` + bt + `.env.example` + bt + ` — returns structured JSON with per-key sync status |
| ` + bt + `site_link` + bt + ` | Register a directory as a lerd site — **non-PHP projects** must have a Containerfile (default name ` + bt + `Containerfile.lerd` + bt + `; set ` + bt + `container.containerfile` + bt + ` for a different path, e.g. ` + bt + `Dockerfile` + bt + `) + ` + bt + `.lerd.yaml` + bt + ` with ` + bt + `container: {port: N}` + bt + ` written first, otherwise the site registers as PHP (wrong) |
| ` + bt + `site_unlink` + bt + ` | Unregister a site and remove its nginx vhost (all domains) |
| ` + bt + `site_domain_add` + bt + ` | Add an additional domain to a site (without TLD) |
| ` + bt + `site_domain_remove` + bt + ` | Remove a domain from a site (cannot remove last) |
| ` + bt + `park` + bt + ` | Register a parent directory — auto-registers all PHP projects as sites |
| ` + bt + `unpark` + bt + ` | Remove a parked directory and unlink all its sites |
| ` + bt + `secure` + bt + ` | Enable HTTPS for a site (mkcert) — updates APP_URL automatically |
| ` + bt + `unsecure` + bt + ` | Disable HTTPS for a site |
| ` + bt + `xdebug_on` + bt + ` | Enable Xdebug for a PHP version (port 9003); optional ` + bt + `mode` + bt + ` (default ` + bt + `debug` + bt + `; also ` + bt + `coverage` + bt + `, ` + bt + `develop` + bt + `, ` + bt + `profile` + bt + `, ` + bt + `trace` + bt + `, ` + bt + `gcstats` + bt + `, or comma combos) |
| ` + bt + `xdebug_off` + bt + ` | Disable Xdebug for a PHP version |
| ` + bt + `xdebug_status` + bt + ` | Show Xdebug state and active mode for all PHP versions |
| ` + bt + `service_start` + bt + ` | Start a built-in or custom service |
| ` + bt + `service_stop` + bt + ` | Stop a service |
| ` + bt + `service_add` + bt + ` | Register a new custom OCI service (MongoDB, RabbitMQ, …); supports ` + bt + `depends_on` + bt + ` for service dependencies |
| ` + bt + `service_preset_list` + bt + ` | List bundled service presets (phpmyadmin, pgadmin, mongo, mongo-express, selenium, stripe-mock, …) with versions and install state |
| ` + bt + `service_preset_install` + bt + ` | Install a bundled preset by name (` + bt + `version` + bt + ` for multi-version families); becomes a normal custom service |
| ` + bt + `service_remove` + bt + ` | Stop and deregister a custom service |
| ` + bt + `service_expose` + bt + ` | Add or remove an extra published port on a built-in service (persisted) |
| ` + bt + `service_env` + bt + ` | Return the recommended ` + bt + `.env` + bt + ` connection variables for a service |
| ` + bt + `db_export` + bt + ` | Export a database to a SQL dump file — auto-detects service and database; accepts optional ` + bt + `service` + bt + ` override |
| ` + bt + `db_import` + bt + ` | Import a SQL dump file into the project database — auto-detects service and database; starts the service if needed |
| ` + bt + `db_create` + bt + ` | Create a database and ` + bt + `_testing` + bt + ` variant — auto-detects service and name; starts the service if needed |
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
| ` + bt + `worker_add` + bt + ` | Add a custom worker to a project or global framework overlay |
| ` + bt + `worker_remove` + bt + ` | Remove a custom worker; stops it if running |
| ` + bt + `project_new` + bt + ` | Scaffold a new PHP project (runs the framework's create command); follow with ` + bt + `site_link` + bt + ` + ` + bt + `env_setup` + bt + ` |
| ` + bt + `framework_list` + bt + ` | List all framework definitions with their workers and setup commands |
| ` + bt + `framework_add` + bt + ` | Add or update a framework definition; use ` + bt + `name: "laravel"` + bt + ` to add custom workers or setup commands to Laravel |
| ` + bt + `framework_remove` + bt + ` | Remove a user-defined framework; for laravel removes only custom worker and setup additions |
| ` + bt + `site_php` + bt + ` | Change PHP version for a site — writes ` + bt + `.php-version` + bt + `, updates registry, regenerates nginx vhost |
| ` + bt + `site_node` + bt + ` | Change Node.js version for a site — writes ` + bt + `.node-version` + bt + `, installs via fnm if needed |
| ` + bt + `site_pause` + bt + ` | Pause a site: stop workers and custom container, replace vhost with landing page |
| ` + bt + `site_unpause` + bt + ` | Resume a paused site: start container, restore vhost, restart workers |
| ` + bt + `site_restart` + bt + ` | Restart a site's container (custom container or PHP-FPM) |
| ` + bt + `site_rebuild` + bt + ` | Rebuild custom container image from Containerfile and restart |
| ` + bt + `service_pin` + bt + ` | Pin a service so it is never auto-stopped even when no sites reference it |
| ` + bt + `service_unpin` + bt + ` | Unpin a service so it can be auto-stopped when unused |
| ` + bt + `stripe_listen` + bt + ` | Start a Stripe webhook listener for a site |
| ` + bt + `stripe_listen_stop` + bt + ` | Stop the Stripe webhook listener |
| ` + bt + `logs` + bt + ` | Fetch container logs — defaults to current site's FPM; optionally specify nginx, service name, PHP version, or site name |
| ` + bt + `status` + bt + ` | Health snapshot of DNS, nginx, PHP-FPM containers, and the file watcher |
| ` + bt + `doctor` + bt + ` | Full diagnostic as structured JSON: podman, systemd, DNS, ports, PHP images, config, updates |
| ` + bt + `which` + bt + ` | Show resolved PHP version, Node version, document root, and nginx config for the current site |
| ` + bt + `check` + bt + ` | Validate ` + bt + `.lerd.yaml` + bt + ` as structured JSON — PHP version, services, framework references with per-field ok/warn/fail |

### Key conventions

- ` + bt + `path` + bt + ` argument is optional on most tools — defaults to the directory the AI assistant was opened in (cwd), then ` + bt + `LERD_SITE_PATH` + bt + ` if set; you can almost always omit it
- ` + bt + `artisan` + bt + ` is Laravel-only; ` + bt + `console` + bt + ` is the equivalent for non-Laravel frameworks — both take ` + bt + `path` + bt + ` (absolute project root) and ` + bt + `args` + bt + ` (array)
- ` + bt + `vendor_run` + bt + ` is the right way to invoke project tooling like pest, phpunit, pint, phpstan, rector — call ` + bt + `vendor_bins` + bt + ` first to discover what's installed, then ` + bt + `vendor_run(bin: "<name>", args: [...])` + bt + `; prefer it over ` + bt + `composer(args: ["exec", ...])` + bt + `
- On a **fresh Laravel clone** (DB_CONNECTION=sqlite in ` + bt + `.env` + bt + `), call ` + bt + `db_set(database: "mysql"|"postgres"|"sqlite")` + bt + ` before ` + bt + `env_setup` + bt + ` to pick a database deliberately. ` + bt + `env_setup` + bt + ` on its own won't switch the database away from sqlite.
- **Domain conflicts on link**: when ` + bt + `lerd link` + bt + ` (or the parked-directory watcher) tries to register a ` + bt + `.lerd.yaml` + bt + ` domain that another site already owns, the conflicting domain is filtered out and a ` + bt + `[WARN] domain "X" already used by site "Y" — skipped` + bt + ` line is printed. The site still gets registered with surviving domains, falling back to ` + bt + `<dirname>.<tld>` + bt + ` if everything was filtered. ` + bt + `.lerd.yaml` + bt + ` is not modified on disk so the conflict is visible in the UI and self-heals on the next link if the owning site is removed. The ` + bt + `site_link` + bt + ` and ` + bt + `site_domain_add` + bt + ` MCP tools, by contrast, hard-error on conflicts so you can react explicitly — read the error message for the owning site name.
- **Custom APP_URL**: ` + bt + `env_setup` + bt + ` writes ` + bt + `<scheme>://<primary-domain>` + bt + ` by default. Override by setting ` + bt + `app_url` + bt + ` in ` + bt + `.lerd.yaml` + bt + ` (committed) or in the per-machine ` + bt + `sites.yaml` + bt + ` site entry. No MCP tool sets it — edit the YAML and re-run ` + bt + `env_setup` + bt + `.
- ` + bt + `tinker` + bt + ` must use ` + bt + `--execute=<code>` + bt + ` for non-interactive use
- Built-in service hosts follow the pattern ` + bt + `lerd-<name>` + bt + ` (e.g. ` + bt + `lerd-mysql` + bt + `, ` + bt + `lerd-redis` + bt + `)
- Default DB credentials: username ` + bt + `root` + bt + `, password ` + bt + `lerd` + bt + `
- ` + bt + `service_stop` + bt + ` marks the service paused — ` + bt + `lerd start` + bt + ` skips it until explicitly started again
- ` + bt + `queue_start` + bt + ` requires Redis to be running when ` + bt + `QUEUE_CONNECTION=redis` + bt + `; call ` + bt + `service_start(name: "redis")` + bt + ` first
- If ` + bt + `sites` + bt + ` returns ` + bt + `has_horizon: true` + bt + ` for a site, use ` + bt + `horizon_start` + bt + ` / ` + bt + `horizon_stop` + bt + ` instead of ` + bt + `queue_start` + bt + ` / ` + bt + `queue_stop` + bt + ` — Horizon manages queues and they are mutually exclusive
- Use ` + bt + `worker_list` + bt + ` first to discover what workers are available for a site before calling ` + bt + `worker_start` + bt + `
- ` + bt + `worker_add` + bt + ` saves custom workers to ` + bt + `.lerd.yaml` + bt + ` by default (project-level, committed to git); use ` + bt + `global: true` + bt + ` to save to the user framework overlay (` + bt + `~/.config/lerd/frameworks/` + bt + `) for all projects of that framework; does not auto-start — call ` + bt + `worker_start` + bt + ` afterwards
- ` + bt + `worker_remove` + bt + ` stops a running worker before removing it from config; use ` + bt + `global: true` + bt + ` to target the framework overlay
- Workers with ` + bt + `conflicts_with` + bt + ` automatically stop conflicting workers when started (e.g. a custom queue processor that conflicts with the default queue worker); conflicted workers are hidden from the UI while the conflicting worker runs
- Worker unit names follow the pattern ` + bt + `lerd-<worker>-<site>` + bt + ` (e.g. ` + bt + `lerd-messenger-myapp` + bt + `, ` + bt + `lerd-horizon-myapp` + bt + `)
- ` + bt + `site_php` + bt + ` / ` + bt + `site_node` + bt + ` change the PHP/Node version for a site; the FPM container for the new PHP version must be running after calling ` + bt + `site_php` + bt + `
- ` + bt + `site_pause` + bt + ` / ` + bt + `site_unpause` + bt + ` free up resources for sites not in active use without unlinking them; paused state persists across restarts
- **Custom container sites** (Node.js, Python, Go, etc.) — mandatory sequence: **(1)** write a Containerfile in the project root (default name ` + bt + `Containerfile.lerd` + bt + `; any name works if you set ` + bt + `container.containerfile` + bt + `); **(2)** write ` + bt + `.lerd.yaml` + bt + ` with ` + bt + `container: {port: <N>}` + bt + ` (plus optional ` + bt + `domains` + bt + `, ` + bt + `services` + bt + `, ` + bt + `secured` + bt + `) — there is no MCP tool for this; write the file directly or ask the user to run ` + bt + `lerd init` + bt + `; **(3)** configure the project's ` + bt + `.env` + bt + ` (or equivalent config) with service connection strings BEFORE linking — use ` + bt + `lerd-mysql` + bt + `, ` + bt + `lerd-redis` + bt + `, ` + bt + `lerd-postgres` + bt + ` as hostnames and start needed services with ` + bt + `service_start` + bt + `; **(4)** call ` + bt + `site_link` + bt + ` — the container starts immediately, so the env must already be correct. **Never call ` + bt + `site_link` + bt + ` before steps 1–3**: without ` + bt + `container:` + bt + ` config the site registers as PHP-FPM (wrong); if that happened, ` + bt + `site_unlink` + bt + ` first, write the files, then link again. Workers in ` + bt + `custom_workers` + bt + ` exec into the container. ` + bt + `site_restart` + bt + ` restarts without rebuilding. When ` + bt + `container` + bt + ` is set, ` + bt + `php_version` + bt + ` and ` + bt + `framework` + bt + ` are ignored.
- ` + bt + `service_pin` + bt + ` keeps a service always running regardless of which sites are active; use for shared services like MySQL or Redis
- ` + bt + `service_add` + bt + ` supports ` + bt + `depends_on` + bt + ` (array of service names): starting a dependency auto-starts the dependent service; stopping a dependency cascade-stops the dependent first; starting the dependent ensures dependencies start first
- Prefer ` + bt + `service_preset_install` + bt + ` over hand-rolling ` + bt + `service_add` + bt + ` for anything in the bundled catalogue (` + bt + `phpmyadmin` + bt + `, ` + bt + `pgadmin` + bt + `, ` + bt + `mongo` + bt + `, ` + bt + `mongo-express` + bt + `, ` + bt + `selenium` + bt + `, ` + bt + `stripe-mock` + bt + `, ` + bt + `mysql` + bt + `, ` + bt + `mariadb` + bt + `, …) — presets ship sane defaults, dependency wiring, dashboards, and rendered config files; call ` + bt + `service_preset_list` + bt + ` first to see what's available; multi-version families take a ` + bt + `version` + bt + ` argument; presets whose dependency is another custom service (e.g. ` + bt + `mongo-express` + bt + ` on ` + bt + `mongo` + bt + `) require the dep installed first
- ` + bt + `project_new` + bt + ` requires an absolute ` + bt + `path` + bt + ` and runs the framework's ` + bt + `create` + bt + ` command; follow it with ` + bt + `site_link` + bt + ` + ` + bt + `env_setup` + bt + ` to register and configure the new project
- ` + bt + `framework_add` + bt + ` accepts ` + bt + `workers` + bt + ` (map) and ` + bt + `setup` + bt + ` (array) — both support an optional ` + bt + `check` + bt + ` field (` + bt + `{file}` + bt + ` or ` + bt + `{composer}` + bt + `) to conditionally show based on project deps; for Laravel, custom setup commands replace built-in storage:link/migrate/db:seed
- Framework env vars support service version placeholders: ` + bt + `{{mysql_version}}` + bt + `, ` + bt + `{{postgres_version}}` + bt + `, ` + bt + `{{redis_version}}` + bt + `, ` + bt + `{{meilisearch_version}}` + bt + ` — resolved from the running service image tag
- ` + bt + `php_ext_add` + bt + ` / ` + bt + `php_ext_remove` + bt + ` rebuild the FPM image and restart the container — may take a minute; ` + bt + `version` + bt + ` defaults to the project or global PHP version
- ` + bt + `db_import` + bt + ` / ` + bt + `db_export` + bt + ` / ` + bt + `db_create` + bt + ` auto-detect service and database via: ` + bt + `service` + bt + ` arg → framework definition detect rules → ` + bt + `DB_CONNECTION` + bt + ` / ` + bt + `DB_TYPE` + bt + ` / ` + bt + `TYPEORM_CONNECTION` + bt + ` / ` + bt + `DATABASE_URL` + bt + ` / ` + bt + `DB_PORT` + bt + `; pass ` + bt + `service` + bt + ` explicitly for projects with no env config
- ` + bt + `db_create` + bt + ` always creates both ` + bt + `<name>` + bt + ` and ` + bt + `<name>_testing` + bt + ` databases; safe to call if they already exist; starts the service automatically if not running
- ` + bt + `park` + bt + ` auto-registers all PHP subdirectories as sites in one call; ` + bt + `unpark` + bt + ` removes them all — project files are NOT deleted
`

// cursorRulesContent is the Cursor rules file written to .cursor/rules/lerd.mdc.
const cursorRulesContent = `---
description: Lerd local PHP development environment — use the lerd MCP tools to manage sites, services, workers, and PHP/Node runtimes.
globs:
alwaysApply: true
---
` + junieGuidelinesSection
