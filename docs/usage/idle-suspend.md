# Idle-Suspend

Idle-suspend reclaims the resources a site's background workers hold while you're not using it. When a site sees no activity for the timeout, lerd gracefully stops **all** of its workers and resumes them on the next activity. It's off by default.

On a typical dev box your sites share one PHP-FPM container, so the per-site resource cost isn't the web tier — it's the workers (queue, Horizon, scheduler, Reverb, the Stripe listener, Vite), which run around the clock whether or not you're working. Idle-suspend stops them while a site is quiet and brings them back the moment the site is used again. Workers are asynchronous, so the request that wakes them is served immediately while they boot in the background.

It is a single global policy: one on/off switch and one timeout for every site. There is no per-site enable or per-site timeout — to keep one site always-warm, [pin](#pinning-a-site) it.

## Commands

```bash
lerd idle status        # global policy, plus each site and worktree's last-active state
lerd idle on            # enable
lerd idle off           # disable (resumes everything immediately)
lerd idle timeout 30m   # set the timeout (e.g. 5m, 30m, 2h)
lerd idle pin <site>    # keep a site always-warm (never suspended)
lerd idle unpin <site>  # let a pinned site sleep again
```

You can also toggle it and set the timeout from the dashboard's **System → lerd** page.

`lerd idle status` shows the global policy and the last-active state of every site, with each git worktree listed under its site:

```
Idle-suspend: enabled, timeout 30m

  myapp                 active 2m ago
    myapp/feature       active 8m ago
  shop                  idle 41m
  blog                  pinned
  legacy                paused
```

## Which workers are suspended

All of them. When a site goes idle, lerd stops every worker it runs — queue, scheduler, Horizon, Reverb, the Stripe listener, and Vite — and persists which it stopped so it can restart exactly those on the next activity. An idle site should do no background work at all.

Suspension is graceful: workers receive `SIGTERM` and finish their current job before exiting, and Laravel's job reservation/retry covers anything in flight, so no jobs are lost.

**Vite is a special case.** Stopping the Vite dev server makes Laravel's `@vite` directive fall back to the built asset manifest, so before suspending Vite lerd runs `npm run build` (once, if no usable build exists) and clears `public/hot`. A sleeping site then serves built assets instead of a broken page. If a build can't be produced, Vite is left running for that site.

## Pinning a site

A pinned site is excluded from idle-suspend: its workers stay running even while the feature is on. Pin from the CLI (`lerd idle pin <site>`) or from a site's overflow menu in the dashboard (the Pin item only appears while idle-suspend is enabled). Pinning a site that's currently asleep wakes it immediately.

## Worktrees

Each git worktree idles on its **own** timer, independent of the main checkout and of the other worktrees. Working on `feature.myapp.test` keeps that worktree's workers alive while the main site and other worktrees can sleep on their own schedules. Only workers a framework marks `per_worktree: true` run per worktree — for Laravel that's Vite — so suspending a worktree stops its `lerd-vite-<site>-<branch>` unit (with the same build-on-suspend handling) and resumes it on the worktree's own traffic.

## How activity is detected

Four signals count as activity, none of which poll or burn CPU:

- **HTTP requests** — nginx logs each request's host to a unix datagram socket lerd-ui owns; lerd-ui maps the host (including worktree subdomains) to its site or worktree and stamps it active.
- **CLI commands** — running `php`, `artisan`, `composer`, `npm`, or `node` through lerd's shims inside a project directory keeps that site awake, so working in the terminal doesn't let it sleep.
- **MCP tools** — a lerd MCP tool call that targets a site (by `site` name or `path`) marks it active too, so an agent working on a site keeps it warm.
- **Source-file saves** — `lerd-watcher` watches each site's (and each worktree's) source tree and treats a save as activity, so editing keeps the site awake even when nothing reaches nginx — a Vite HMR session, for instance, where the browser talks to the dev server directly and no request is logged. The watched directories default to the common source roots (`resources`, `src`, `app`, `routes`, ...) and never include `node_modules`/`vendor`; a framework can override them with `source_dirs` in its definition. This is the primary idle signal on macOS, where the nginx access feed isn't reachable from the host.

A site is idle once it has gone `timeout` without any of these. Last-active times are persisted, so a lerd-ui restart or a redeploy restores each countdown instead of resetting it. Resume is immediate: activity on a suspended site wakes its workers right away, not on the next evaluation tick (which only bounds how long an idle site waits past its timeout before suspending). Turning the feature off brings every suspended worker back at once.

## Notes

- Off by default — a quiet dev box only reclaims worker memory once you opt in.
- Workers stopped by idle-suspend are not reported as failed by the health watcher; they're asleep on purpose.
