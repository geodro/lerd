# Node

## Commands

| Command | Description |
|---|---|
| `lerd node:install <version>` | Install a Node.js version globally via fnm |
| `lerd node:uninstall <version>` | Uninstall a Node.js version via fnm |
| `lerd node:use <version>` | Set the global default Node.js version |
| `lerd isolate:node <version>` | Pin Node version for cwd: writes `.node-version`, runs `fnm install` |
| `lerd node:manage` | Opt into lerd-managed Node: install the fnm shims and a default version |
| `lerd node:unmanage` | Stop managing Node: remove lerd's shims and fnm-installed versions for a clean system |
| `lerd js:runtime [bun\|node\|auto]` | Pin the current site's JS runtime (or show it with no argument) |
| `lerd php:bun install [version]` | Install a musl bun inside the PHP-FPM container |
| `lerd php:bun remove` | Remove the in-container bun and clear its shared persistent volume |
| `lerd php:bun update [version]` | Update the container's bun in place (`bun upgrade`) |
| `lerd php:bun version` | Show the bun version installed in the PHP-FPM container |

---

## Usage

`lerd install` places shims for `node`, `npm`, and `npx` in `~/.local/share/lerd/bin/`, which is added to your `PATH`. You use them exactly as you normally would, lerd picks the right version automatically:

```bash
node --version
npm install
npx tsc --init
```

---

## Version resolution

1. `.lerd.yaml`: `node_version` field (explicit lerd override, highest priority)
2. `.nvmrc` in the project root
3. `.node-version` in the project root
4. `package.json`: `engines.node` field
5. Global default in `~/.config/lerd/config.yaml`

To pin a project to a specific version:

```bash
cd ~/Lerd/my-app
lerd isolate:node 20
# writes .node-version and installs Node 20 via fnm
```

To install a version without pinning a project:

```bash
lerd node:install 22
```

---

## Default version

`lerd node:use <version>` sets the global default and stores it in `~/.config/lerd/config.yaml`. Sites without a pinned version use this default.

```bash
lerd node:use 22
```

Version numbers are normalised to the major only, so `22.11.0` and `22.14.1` are both treated as `22`, and only one entry per major appears in the UI and CLI.

---

## fnm

Node version management is handled by [fnm](https://github.com/Schniz/fnm), which is bundled and installed automatically. The `node`, `npm`, and `npx` shims in `~/.local/share/lerd/bin/` invoke the correct version via fnm for each project.

---

## Global npm packages

`npm install -g <pkg>` works through the lerd shim. The package goes to a lerd managed prefix at `~/.local/share/lerd/node-global/`, and lerd writes a small wrapper script for every binary into `~/.local/share/lerd/bin/`, which is already on your `PATH` because `lerd install` adds it. After `npm install -g pm2` you can call `pm2` from any shell directly, no extra setup, on both Linux and macOS regardless of whether lerd itself was installed via Homebrew or curl-pipe.

The wrapper exec's the real binary through `fnm exec --using=default`, so globally installed tools always run on the fnm default node version regardless of the project you are inside when you call them. If you need a specific version for a global tool, change the default with `lerd node:use <version>` before installing it.

`npm uninstall -g <pkg>` removes the wrapper as well. Files in `~/.local/share/lerd/bin/` that lerd did not create with its own marker comment are never touched, so the existing `node`, `npm`, `npx`, `php`, `composer`, and `laravel` shims in the same directory stay safe.

The same mechanism applies to `composer global require`. Composer's global vendor/bin (`~/.config/composer/vendor/bin/` by default, respecting `COMPOSER_HOME` and `XDG_CONFIG_HOME`) is mirrored into `~/.local/share/lerd/bin/` after every `composer` run, with wrappers that exec the real bin through `lerd php` so `#!/usr/bin/env php` shebangs resolve against the FPM container. After `composer global require psy/psysh` you can call `psysh` from any shell directly. `composer global remove` cleans the wrapper too.

---

## System-managed vs lerd-managed Node

If `lerd install` detects an existing `node`, `npm`, or `npx` on your `PATH` or under a known version-manager directory (nvm, volta, mise, asdf, fnm), it asks **"Let lerd manage Node.js?"** before writing any shims.

- **Answer yes**: lerd installs fnm, picks the current LTS, sets it as the fnm default, and writes the `node` / `npm` / `npx` shims into `~/.local/share/lerd/bin/`. Per-project version pinning works as described above.
- **Answer no**: lerd writes nothing into `~/.local/share/lerd/bin/`, removes any stale shims from a previous opt-in, and stays out of your `PATH`. Sites use whatever `node` your shell resolves; per-project pinning is your version manager's job. The dashboard's Node tab disables the install controls and points back at `lerd install` if you change your mind.

`lerd node:install` / `node:use` / `node:uninstall` warn and require confirmation if you run them on a host where lerd isn't currently managing Node, and write fresh shims on accept so CLI opt-in matches the install flow.

You can flip the choice at any time without re-running the whole installer:

- `lerd node:manage` writes the fnm shims and installs a default version (the same thing accepting the install prompt does).
- `lerd node:unmanage` removes the node/npm/npx shims and uninstalls the fnm Node versions, leaving a clean system so your own Node (or bun) is used directly.

Both also regenerate any host worker units (Vite and other `host: true` workers) so they switch between fnm, your system Node, and bun to match the new state. The dashboard and Settings exposes the same toggle: the Node page shows a **Let lerd manage Node** / **Stop managing** button.

---

## bun

lerd works with [bun](https://bun.sh) as a drop-in alternative to the Node + npm toolchain. lerd never installs or version-manages bun: you install it yourself (`curl -fsSL https://bun.sh/install | bash`) and update it with `bun upgrade`. lerd only detects it and routes work through it.

### When lerd uses bun

On the host, lerd runs install, dev (Vite), and build through bun instead of npm when either of these is true:

1. **The project uses bun**, detected from a `bun.lockb` / `bun.lock` / `bunfig.toml` file or a `packageManager: bun` field in `package.json`. The Vite host worker runs `bun run dev`, installs run `bun install`, and builds run `bun run <script>`.
2. **There is no Node available** (you ran `lerd node:unmanage` and have no system Node) but bun is installed. bun then becomes the fallback JS runtime for every project, since it can run the same `package.json` scripts.

If a project looks like a bun project but bun isn't installed, lerd falls back to npm and prints a one-line install hint. Node-managed projects keep using Node unless they opt into bun via a lockfile.

### Pinning the runtime per project

bun is not a perfect Node drop-in: apps with native N-API addons (NestJS with some dependencies, and similar) can crash on bun because its libuv coverage is incomplete. Pin the runtime in `.lerd.yaml` to override detection:

```yaml
js_runtime: node   # or "npm": always use Node/npm, never bun (opts out of the no-Node fallback too)
# js_runtime: bun  # always use bun, even with Node managed and no bun.lockb
```

Use `js_runtime: node` for a site that must run on Node (then install Node on your machine or let lerd manage it), while other sites still use bun. Leave it unset to auto-detect.

You don't have to edit the file by hand. From the site's directory, `lerd js:runtime bun` and `lerd js:runtime node` write the same `js_runtime` field for you, and `lerd js:runtime auto` clears it back to auto-detect. Each one re-syncs the site's host workers so a running Vite/dev worker switches runtime straight away, exactly like the dashboard's bun/Node toggle. Run `lerd js:runtime` with no argument to see the current setting and what it resolves to.

### Lifecycle

Detection is live for display (the dashboard and Settings show a `🥟 bun <version>` chip and switch the runtime label to **JS Runtime** when bun is active) and for any worker generated after bun appears. Existing host worker units are static, so they keep their old command until regenerated. Regeneration happens on:

- `lerd link` / `lerd setup` for that site,
- `lerd node:manage` / `lerd node:unmanage` (rewrites every host worker),
- `lerd update`, which re-syncs host workers to the current runtime when bun is installed (only workers whose command actually changes are restarted).

So if you install bun after a site is already running, the UI reflects it immediately, and a `lerd update` (or re-link) switches the running Vite worker onto bun.

### bun inside the PHP-FPM container

The host bun can't run inside the container (it's built for your host's libc, the container is Alpine/musl), so `lerd shell` gets its own bun:

```bash
lerd php:bun install        # installs a musl bun into the container, via the bundled npm
lerd php:bun version        # shows what's installed
lerd php:bun remove         # deletes it and clears the volume
```

bun is installed into a persistent volume (`~/.local/share/lerd/bun` mounted at `/root/.bun`), shared across every PHP version and **kept across image rebuilds and pulls** (it lives in the volume, not the image, so a new base image never reinstalls it). `lerd shell` puts it on `PATH`. Update it in place with `lerd php:bun update` (or `bun upgrade` from inside `lerd shell`). When bun is installed on the host, `lerd link` / `lerd setup` also installs it into the container automatically. `lerd php:bun remove` clears the volume so the next install starts clean; because the volume is shared it removes bun for every PHP version at once, and the container need not be running.
