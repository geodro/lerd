# Framework commands

Every PHP site in the lerd dashboard exposes a **Commands ▾** dropdown in the site controls band, alongside the version pickers and worker toggles. It surfaces a curated set of one-shot admin actions for the detected framework, plus anything the project adds in its `.lerd.yaml`. The same set is reachable from the command palette (`⌘K` / `/`) and from the terminal via `lerd run <name>`.

The feature exists for the actions you'd otherwise ssh in for: clearing caches, applying migrations, generating an admin login link, exporting the database before a risky change.

## Canonical commands per framework

| Framework | Commands |
|---|---|
| Laravel | `optimize:clear`, `migrate`, `migrate:fresh` |
| WordPress | `cache:flush`, `rewrite:flush`, `db:export` |
| Drupal | `cr`, `uli`, `updb`, `cex`, `cim` |
| Symfony | `cache:clear`, `doctrine:migrations:migrate`, `doctrine:fixtures:load` |
| CakePHP | `cache:clear`, `migrate`, `schema:cache:clear` |
| Statamic | `cache:clear`, `stache:warm`, `search:update` |

Destructive commands (`migrate:fresh`, `cim`, `doctrine:fixtures:load`) are gated by a confirmation modal before running.

Some commands include a `check:` rule and only surface when the relevant package is installed (`doctrine:migrations:migrate` requires `doctrine/doctrine-migrations-bundle`, CakePHP `migrate` requires `cakephp/migrations`).

## How a run works

Clicking a command (or pressing Enter on a palette entry, or running `lerd run <name>`) executes the shell command in the project's directory, with stdio routed depending on the command's `output:` value:

- **`silent`** (default) — runs, captures output, shows a brief success or failure indicator in the modal.
- **`text`** — same as silent but with the captured stdout rendered in a scrollable monospace block. Use for commands whose output you'd want to read (test runs, route lists, config diffs).
- **`url`** — captures stdout, scans it for the first `http(s)://...` URL, and surfaces it with Copy and Open buttons. The killer feature for `drush uli` and similar one-time-login generators.
- **`terminal`** — spawns the user's terminal emulator (kitty, foot, alacritty, wezterm, ghostty, ptyxis, konsole, gnome-terminal, xterm; on macOS iTerm or Terminal.app) with the command running inside. Use for interactive commands like `php artisan tinker`, `bin/cake bake`, `wp shell` that need a real TTY. The lerd-ui modal stays closed.

The dashboard modal streams output as it arrives via Server-Sent Events from `POST /api/sites/:domain/commands/:name/run`; the CLI streams straight to your terminal (`lerd run` is stdio-passthrough).

## Project commands

Any `.lerd.yaml` can add or override commands via a `commands:` block:

```yaml
# .lerd.yaml
commands:
  - name: deploy
    label: Deploy to staging
    command: ./bin/deploy staging
    description: Push the current branch to the staging environment
    output: text
    icon: arrow-up

  - name: search:replace
    label: Replace URL across the database
    command: wp search-replace https://old.example https://new.example --all-tables
    output: text
    confirm: true
    icon: edit

  - name: migrate:fresh
    disabled: true                 # suppress the Laravel default
```

The merge rules are:

- A project entry with the same `name` as a framework entry **fully replaces** it. Use this to point `test` at Pest, swap the migration command for a custom wrapper, etc.
- `disabled: true` on a name that matches a framework entry **suppresses** the framework default without contributing a replacement.
- A project entry with a new `name` is **appended** after the framework set.
- Framework entries whose `check:` rule fails are dropped before the merge.

Validation runs as part of `lerd check`. Invalid `output:` values, unknown icons, duplicate names, and missing commands all surface there.

Because a `.lerd.yaml` `commands:` entry comes from the project (an untrusted cloned repo), lerd asks before running one on your host: the first run via `lerd run` or the dashboard shows the exact command and prompts, and the approval is remembered per site so later runs don't re-prompt. `lerd run --yes` bypasses the prompt, and `host_commands.skip_confirmation: true` (or `host_commands.disabled: true` to refuse them) in the global config changes the default. Framework-provided commands (store, built-in, user overlay) run without this prompt.

## Schema

```yaml
commands:
  - name: optimize:clear         # stable id; also the `lerd run` argument and override key
    label: Clear all caches       # UI label
    command: php artisan optimize:clear   # shell, passed to `sh -c`
    description: Clear config, route, view, event, and compiled caches
    output: silent                # silent | text | url | terminal
    confirm: false                # ask before running
    icon: broom                   # from the known icon set
    cwd: .                        # optional, relative to project root
    check:                        # optional; hide when this rule fails
      composer: doctrine/doctrine-migrations-bundle
    # `disabled: true` is only meaningful in .lerd.yaml; ignored in framework yamls
```

**Known icons**: `broom`, `database`, `refresh`, `link`, `check`, `list`, `key`, `edit`, `arrow-down`, `arrow-up`, `play`, `terminal`. An unknown icon falls back to a generic glyph; `lerd check` warns.

**Output values:** invalid values fail `lerd check`. Defaults to `silent`.

**Check rules**: reuse `FrameworkRule`. The two common forms are `composer: <package>` (the package must be in `composer.json`) and `file: <path>` (the file must exist relative to the project root).

## Agents (MCP)

When the lerd MCP server is registered, an AI assistant can:

- `commands_list(site)` — see what's available for a site
- `commands_run(site, name, force?)` — execute one (with `force: true` to bypass `confirm`)
- `command_add(site, name, command, ...)` — write a new entry into `.lerd.yaml`'s `commands:` block. Same `name` as a framework default replaces it. Use `disabled: true` to suppress a framework default
- `command_remove(site, name)` — delete a project entry

Agents should prefer `commands_run` over invoking `php artisan` / `drush` / `wp` directly so per-project overrides are honored, and `command_add` over hand-editing yaml so the entry passes the same validation `lerd check` runs.

## CLI

```
$ lerd run                            # list available commands for the current project
    optimize:clear   Clear all caches
    migrate          Run migrations
  * migrate:fresh    Drop and re-migrate

  * = asks for confirmation. Use --yes to skip.

$ lerd run optimize:clear             # execute, stream stdout to your terminal
$ lerd run migrate:fresh --yes        # bypass the confirm prompt
```

`lerd run` walks up from the current directory to find the nearest `.lerd.yaml`, so it works from any subdirectory of a site (including inside a git worktree). The exit code propagates from the underlying shell.

Shell completion populates command names: `lerd run <TAB>` lists what's available in the current project.

## Concurrency & security

Two commands cannot run on the same site at the same time; the API returns `409 Conflict` if a second run is attempted while one is in flight. This protects against accidentally running `migrate:fresh` twice from two tabs.

The run endpoint is loopback-only — LAN clients (when the access mode allows remote viewing) can see the list of commands but cannot execute them. The list endpoint is read-only and exposed everywhere lerd-ui is reachable.