# Disk cleanup

Local PHP development on podman accumulates reclaimable image data. Every PHP image rebuild re-points the fixed `:local` tag and leaves the old image dangling, every Containerfile hash bump (from a lerd update) strands the previous base image, and every service upgrade leaves the old version's image behind. Left alone, a machine that only ever kicked the tyres can end up tens of gigabytes deep.

`lerd cleanup` reclaims that space. Unlike a blunt `podman system prune -a`, it knows which images are load-bearing and only ever removes lerd's own, so it can never eat a database or another tool's images.

## What it removes

**Safe tier** (the default, and what runs automatically):

- **Orphaned PHP build images** — the old `lerd-php<ver>-fpm:local` / `lerd-frankenphp<ver>:local` image a rebuild left dangling when it re-pointed the tag.
- **Orphaned base images** — a pre-built `lerd-php*-fpm-base` image nothing live is built on: an old Containerfile hash, or a PHP version you no longer have installed. Whether a base is still in use is decided by **layer ancestry** (is its top layer part of any live image?), so a base the current PHP image is built on is always kept, never untagged into a needless re-pull.

**Deep tier** (`--deep`, on demand only):

- **Unused service images** — a service image no installed service references any more, e.g. an old `mysql:8.0` after you upgraded to `8.4`. Each service's **current image and its one-back rollback target are kept**, so a rollback still works.

## What it never touches

- Any image not provably lerd's. An image qualifies only by a `dev.lerd.*` label or the `lerd-php*-fpm-base` repo name (service images are matched against lerd's own preset catalogue). Your own `mysql:8.0` pulled outside lerd, or any unrelated image, is left alone.
- Named data volumes — your databases are never in scope.
- Any image a running container uses, and any image still referenced by an installed service.

Removal is reference-count safe: layers an image still shares with a live image stay on disk, and an image that turns out to be in use is skipped rather than forced.

## Commands

```bash
lerd cleanup              # preview, confirm, then reclaim the safe tier
lerd cleanup --dry-run    # show what would be reclaimed and the size, remove nothing
lerd cleanup --deep       # also reclaim unused service images (keeps current + rollback)
lerd cleanup --yes        # skip the confirmation prompt (for scripts)
```

Reported sizes are the disk actually freed (an image's unique layers, not its full size), so the figure is honest even when images share base layers. `lerd doctor` shows the reclaimable total as a read-only line so you discover the bloat early.

The destructive command is CLI-only by design, consistent with keeping destructive operations out of the dashboard and TUI.

## Automatic cleanup

Cleanup is on by default and safe, so the disk doesn't grow on its own:

- **On rebuild / service change** — a PHP rebuild (`lerd use`, `lerd php:rebuild`, `lerd php:ext`/`php:pkg`, a `lerd update` that bumps the Containerfile) reclaims the image it just superseded immediately. A `lerd service update` or `lerd service remove` reclaims that service's now-unused versions, scoped to that one service.
- **Daily backstop** — the `lerd-watcher` runs a safe-tier sweep about once a day (throttled by a timestamp so a restarting watcher can't sweep more often), catching anything an interrupted operation left behind.

The automatic path only ever runs the **safe tier**, never `--deep`. Toggle it with `lerd cleanup auto on` / `lerd cleanup auto off` (or set `auto_cleanup` in [`~/.config/lerd/config.yaml`](../configuration.md)); `lerd cleanup auto status` shows the current state. When off, `lerd cleanup` stays available on demand.

```bash
lerd cleanup auto off       # disable the automatic sweep and event-driven reaping
lerd cleanup auto on        # re-enable (the default)
lerd cleanup auto status    # show whether automatic cleanup is on
```
