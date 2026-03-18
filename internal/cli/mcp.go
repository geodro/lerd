package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

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
	}

	fmt.Printf("Injecting lerd MCP config into: %s\n\n", abs)

	// .mcp.json — merge lerd into mcpServers
	if err := mergeMCPServersJSON(filepath.Join(abs, ".mcp.json"), lerdEntry); err != nil {
		return err
	}
	rel1 := ".mcp.json"
	fmt.Printf("  updated %s\n", rel1)

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

	fmt.Println("\nDone! Restart your AI assistant to load the lerd MCP server.")
	return nil
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
description: Manage the lerd local Laravel development environment — run artisan commands, manage services, start/stop queue workers, and inspect site status via MCP tools.
---
# Lerd — Laravel Local Dev Environment

This project runs on **lerd**, a Podman-based Laravel development environment for Linux (similar to Laravel Herd). The ` + bt + `lerd` + bt + ` MCP server exposes tools to manage it directly from your AI assistant.

## Architecture

- PHP runs inside Podman containers named ` + bt + `lerd-php<version>-fpm` + bt + ` (e.g. ` + bt + `lerd-php84-fpm` + bt + `)
- Nginx routes ` + bt + `*.test` + bt + ` domains to the appropriate FPM container
- Services (MySQL, Redis, PostgreSQL, etc.) run as Podman containers via systemd quadlets
- Queue workers run as systemd user services named ` + bt + `lerd-queue-<sitename>` + bt + `
- DNS resolves ` + bt + `*.test` + bt + ` to ` + bt + `127.0.0.1` + bt + `

## Available MCP Tools

### ` + bt + `sites` + bt + `
List all registered lerd sites with domains, paths, PHP versions, Node versions, and queue status. **Call this first** to find site names and paths needed by other tools.

### ` + bt + `artisan` + bt + `
Run ` + bt + `php artisan` + bt + ` inside the PHP-FPM container for the project. Arguments:
- ` + bt + `path` + bt + ` (required): absolute path to the Laravel project root
- ` + bt + `args` + bt + ` (required): artisan arguments as an array

Examples:
` + "```" + `
artisan(path: "/home/user/code/myapp", args: ["migrate"])
artisan(path: "/home/user/code/myapp", args: ["make:model", "Post", "-m"])
artisan(path: "/home/user/code/myapp", args: ["db:seed", "--class=UserSeeder"])
artisan(path: "/home/user/code/myapp", args: ["cache:clear"])
artisan(path: "/home/user/code/myapp", args: ["tinker", "--execute=echo App\\Models\\User::count();"])
` + "```" + `

> **Note:** ` + bt + `tinker` + bt + ` requires ` + bt + `--execute=<code>` + bt + ` for non-interactive use.

### ` + bt + `service_start` + bt + ` / ` + bt + `service_stop` + bt + `
Start or stop a service. Valid names: ` + bt + `mysql` + bt + `, ` + bt + `redis` + bt + `, ` + bt + `postgres` + bt + `, ` + bt + `meilisearch` + bt + `, ` + bt + `minio` + bt + `, ` + bt + `mailpit` + bt + `, ` + bt + `soketi` + bt + `

**.env values for lerd services:**

| Service | Host | Key vars |
|---------|------|----------|
| mysql | ` + bt + `lerd-mysql` + bt + ` | ` + bt + `DB_CONNECTION=mysql` + bt + `, ` + bt + `DB_PASSWORD=lerd` + bt + ` |
| postgres | ` + bt + `lerd-postgres` + bt + ` | ` + bt + `DB_CONNECTION=pgsql` + bt + `, ` + bt + `DB_PASSWORD=lerd` + bt + ` |
| redis | ` + bt + `lerd-redis` + bt + ` | ` + bt + `REDIS_PASSWORD=null` + bt + ` |
| mailpit | ` + bt + `lerd-mailpit:1025` + bt + ` | web UI: http://localhost:8025 |
| meilisearch | ` + bt + `lerd-meilisearch:7700` + bt + ` | |
| minio | ` + bt + `lerd-minio:9000` + bt + ` | ` + bt + `AWS_USE_PATH_STYLE_ENDPOINT=true` + bt + ` |

### ` + bt + `queue_start` + bt + ` / ` + bt + `queue_stop` + bt + `
Start or stop a Laravel queue worker for a site. The worker runs ` + bt + `php artisan queue:work` + bt + ` in the FPM container as a systemd service. Arguments for ` + bt + `queue_start` + bt + `:
- ` + bt + `site` + bt + ` (required): site name from ` + bt + `sites` + bt + ` tool
- ` + bt + `queue` + bt + ` (optional): queue name, default ` + bt + `"default"` + bt + `
- ` + bt + `tries` + bt + ` (optional): max job attempts, default ` + bt + `3` + bt + `
- ` + bt + `timeout` + bt + ` (optional): job timeout in seconds, default ` + bt + `60` + bt + `

### ` + bt + `logs` + bt + `
Fetch recent container logs. Target can be:
- ` + bt + `"nginx"` + bt + ` — nginx proxy logs
- Service name: ` + bt + `"mysql"` + bt + `, ` + bt + `"redis"` + bt + `, etc.
- PHP version: ` + bt + `"8.4"` + bt + ` — logs for that PHP-FPM container
- Site name — logs for the site's PHP-FPM container

Optional ` + bt + `lines` + bt + ` parameter (default: 50).

## Common Workflows

**Run migrations after schema changes:**
` + "```" + `
artisan(path, args: ["migrate"])
` + "```" + `

**Set up a fresh project:**
` + "```" + `
service_start(name: "mysql")
service_start(name: "redis")   // if needed
artisan(path, args: ["key:generate"])
artisan(path, args: ["migrate", "--seed"])
` + "```" + `

**Diagnose PHP errors:**
` + "```" + `
logs(target: "8.4")     // PHP-FPM errors
logs(target: "nginx")   // nginx errors
` + "```" + `

**Work with failed queue jobs:**
` + "```" + `
artisan(path, args: ["queue:failed"])
artisan(path, args: ["queue:retry", "all"])
` + "```" + `

**Generate and run a new migration:**
` + "```" + `
artisan(path, args: ["make:migration", "add_status_to_orders"])
// ... edit the migration file ...
artisan(path, args: ["migrate"])
` + "```" + `
`
