# Healing failed workers

Framework workers (queue, schedule, horizon, reverb, custom) run as systemd user units on Linux and as launchd jobs wrapping `podman exec` on macOS. Both supervisors restart a worker that crashes, but both also stop trying after enough rapid failures and mark the unit `failed`. Once a unit is in `failed` state, neither systemd nor launchd will lift it out without an explicit reset.

`lerd worker heal` is the recovery primitive: it reset-clears the failed state and starts the unit again, on every surface (CLI, dashboard, TUI, MCP).

## When workers go to `failed`

Two common paths:

1. **Restart-rate-limit cascade.** If something stops the unit's parent FPM container repeatedly (a quadlet rewrite cascade, a podman-machine bridge wedge on macOS, an external process), the worker's `BindsTo=` directive stops it as collateral. systemd then tries to restart it, hits `StartLimitBurst`, and parks the unit in `failed`.
2. **Application crash loop.** A worker that throws on startup (missing env var, bad migration, dependency removed) exits faster than `RestartSec=`, exhausts the burst budget, and lands in `failed` with the same message.

The second case requires fixing the underlying error before heal will stick. Heal will start the unit, but if it crashes again it'll burn through the rate limit and re-enter `failed`. The dashboard banner will reappear; check the worker logs (`lerd worker logs <name>` or the dashboard logs pane) for the real cause.

## CLI

```sh
lerd worker heal
```

Scans every registered, non-paused site and resets-and-starts every worker unit currently in `failed`. Prints one line per unit.

```sh
lerd worker heal queue
```

Heals one worker for the site at the current working directory. Equivalent to `systemctl --user reset-failed lerd-queue-<site> && systemctl --user start lerd-queue-<site>` but runs through the same code path the dashboard and MCP use.

## Dashboard

When the detector finds any failed worker, an amber banner appears at the top of the Sites tab listing the affected workers. Clicking **Heal** runs the same reset-and-start sequence and streams per-unit progress (`Starting lerd-queue-myapp…`) until done. The banner disappears when the count returns to zero.

The banner refresh is event-driven: the dashboard reloads health on every `sites` WebSocket push (debounced 500ms), so heal results show up without polling.

## TUI

Press `H` from any pane to heal every failed worker on the box. The status line tracks progress; the snapshot reloads automatically once heal returns. Failing workers also show as red glyphs (`q`, `s`, `v`, `h`, `•`) in the site list — the same surface the dashboard banner reflects.

## MCP

Two tools:

- **`workers_health`** — read-only. Returns the JSON list of unhealthy workers (site, worker name, full unit name, state). Call before deciding whether to heal.
- **`workers_heal`** — heals every failed worker, or one named unit if `unit` is passed. Returns `{summary, healed, failed}` so the agent can report what was fixed without re-querying.

## What heal will not do

Heal is intentionally narrow. It is a runtime recovery, not an enable/disable knob:

- **It does not write `.lerd.yaml`.** A failed worker is a transient runtime condition, not a change of user intent. Adding or removing workers from a site's enabled list still belongs to `lerd worker add` / `lerd worker remove` (or the equivalent dashboard / TUI / MCP actions).
- **It does not rewrite the unit file.** If the unit drifted (e.g. you renamed the site directory and the `WorkingDirectory=` is stale), heal won't fix it. Run `lerd install` to regenerate the unit file from the framework definition first.
- **It does not touch paused sites.** A worker that's failed because its site is paused stays as it is.
- **It does not loop.** Heal makes one attempt per unit. If the underlying cause is still present, the unit will go back to `failed` shortly after; the banner is the signal to look at logs, not a button to mash.
