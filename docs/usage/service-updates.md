# Service updates

Lerd watches Docker Hub / GHCR for newer images of every running default service and surfaces three escalation tiers depending on what kind of jump is needed:

| Tier | When it fires | What it does | Risk |
|---|---|---|---|
| **Update** (green) | A newer image exists in the same major / minor line as configured by the preset's `update_strategy` (`patch` / `minor`) | Pull, rewrite quadlet, restart | None — same data format |
| **Upgrade** (amber) | A newer same-major tag exists outside the safe-update strategy (e.g. mailpit, rustfs rolling re-pulls) | Same as Update, but the user's accepting potential format changes | Up to the user — no migration |
| **Migrate** (violet) | The service family has a registered SQL-based migrator (mysql, mariadb, postgres) and a newer same-major tag exists | Dump current data, swap data dir, start new image, restore the dump | Safest path for cross-version moves; pre-migrate dump and data dir are preserved |

Default services declare their policy in their preset YAML. The most common combination — `update_strategy: minor`, `allow_major_upgrade: false`, `track_latest: true` — applies to mysql, postgres, redis, meilisearch.

## Update — same-major patches

Click the green **Update → \<tag\>** button in the dashboard, run `lerd service update <name>`, or call MCP `service_control(action: "update", name)`. Lerd queries the registry for the newest tag matching the preset's `update_strategy`, applies the same digest comparison the rolling-tag flow uses (suppresses no-op updates), pulls, persists the chosen image, and restarts the unit. The previous image is recorded so a rollback button appears next to the version label.

For services on rolling tags (`mailpit:latest`, `rustfs:latest`), Update only fires when the local manifest digest differs from the registry's — re-pulling the same `:latest` digest doesn't surface a phantom badge.

## Upgrade — same-major across minor lines

Some services have a "patch" `update_strategy` (Meilisearch is the canonical example because every minor breaks data-dir compatibility). For those, `MaybeNewerTag` only suggests within the current minor; cross-minor jumps surface as the cross-strategy *Upgrade* offer.

The Upgrade button is amber to flag that the user takes responsibility for any format change. **It's only shown when a SQL migrator is NOT registered for the family** — otherwise the safer Migrate button replaces it.

Cross-major boundaries (e.g. mysql 8 → 9, postgres 16 → 17) are not surfaced as upgrades by default. To opt in for a specific preset, set `allow_major_upgrade: true` in the YAML — but consider the alternates picker first (one canonical service, several side-by-side alternates with their own data dirs), which is the safer way to try a major upgrade without touching the running data.

## Migrate — automated dump + restore

For mysql, mariadb, and postgres, the violet **Migrate → \<tag\>** button runs the full SQL migration:

```
1. Dump current data via the running container's native tool
   (mysqldump --all-databases / pg_dumpall) → ~/.local/share/lerd/backups/<svc>-<ts>.sql
2. Stop the unit
3. Move the on-disk data dir aside as <name>.pre-migrate-<ts> (preserved, NOT deleted)
4. Pull the target image
5. Persist the new image and regenerate the quadlet
6. Start the new container, wait for the service-specific readiness probe
7. Pipe the dump into the new container to restore
```

If anything fails before step 5, the data dir is restored and the unit goes back on the old image. If a step after that fails, both the dump file and the old data dir stay on disk so the user can recover by hand.

```bash
# Migrate mysql to a specific 9.x tag (one major above the running 8.x)
lerd service migrate mysql 9.0
```

```jsonc
// MCP
{"tool": "service_control", "args": {"action": "migrate", "name": "mysql", "tag": "9.0"}}
```

The Migrate flow is **only registered for SQL databases** because their dumps are stable text formats — every newer mysql/postgres/mariadb can replay any older version's `mysqldump` / `pg_dumpall` output. Engines whose dumps are version-specific binaries (Meilisearch, Elasticsearch, MongoDB) are intentionally **not** auto-migrated; lerd surfaces the Migrate button only when it has a tested handler. For those, follow the upstream migration guide manually.

### What the dump contains

- **mysql / mariadb**: `mysqldump -uroot -plerd --all-databases --single-transaction --routines --triggers --events --quick`. All databases, stored procedures, triggers, and event scheduler entries. Excludes the `information_schema` and `performance_schema` (they're rebuilt by mysql at first start).
- **postgres**: `pg_dumpall --clean --if-exists -U postgres`. All databases, roles, and tablespaces. The `--clean --if-exists` flags make the dump replayable against a fresh data dir without errors on objects that don't yet exist.

### Recovery if a migration fails

The pre-migrate data dir lives at `~/.local/share/lerd/data/<svc>.pre-migrate-<ts>/`, the dump file at `~/.local/share/lerd/backups/<svc>-<ts>.sql`. To go back:

```bash
lerd service stop <svc>
mv ~/.local/share/lerd/data/<svc> ~/.local/share/lerd/data/<svc>.failed-migrate
mv ~/.local/share/lerd/data/<svc>.pre-migrate-<ts> ~/.local/share/lerd/data/<svc>
# edit ~/.config/lerd/config.yaml: set the image back to the old version
lerd service start <svc>
```

## Rollback — undo the last update

Every successful Update / Upgrade / Migrate records the previous image in `~/.config/lerd/config.yaml` (or the custom service YAML). The grey **Rollback → \<tag\>** button swaps back to it, pulling first if the image isn't local anymore. A second rollback returns to the post-update image — the swap is symmetric, so you can flip-flop while debugging.

```bash
lerd service rollback redis
```

Rollback uses no dump-and-restore: it's a pure image swap. Cross-data-format rollbacks (mysql 8.4 → 8.0 after a minor migration) will fail at startup because the older binary refuses the upgraded data dir. The confirm dialog warns about this. Use the migration recovery procedure above for that case.

## Configuration knobs

Per-preset YAML flags that shape the update flow:

```yaml
name: mysql
default: true
update_strategy: minor          # patch | minor | rolling | none
track_latest: true              # fresh installs resolve newest current upstream
allow_major_upgrade: false      # NewestStable stays in current major when false
```

- `update_strategy: patch` — same major.minor only (1.7 → 1.7.6 OK; 1.7 → 1.8 rejected). Use for engines where minor bumps break data.
- `update_strategy: minor` — same major (8.0 → 8.4 OK; 8.x → 9.x rejected). Default for SQL databases.
- `update_strategy: rolling` — no version comparison; surfaces an Update only when the local image digest differs from the remote tag's digest.
- `update_strategy: none` — disables Update suggestions entirely. Useful for hand-rolled custom services where lerd shouldn't second-guess the user.
- `track_latest: true` — fresh installs (no saved image pin) resolve the actual newest tag from the registry instead of using the YAML's `image:` baseline. Existing users with a saved pin are untouched. Falls back to the YAML image if the registry is unreachable.
- `allow_major_upgrade: true` — `NewestStable` is allowed to cross the numeric major boundary (so postgres 16 → 17 would surface as an Upgrade). Off by default; major upgrades are usually a maintainer judgment call, not a user-time impulse.

## CLI reference

```bash
lerd service list                            # Update column shows badges
lerd service update <name>                   # safe in-strategy patch
lerd service update <name> <tag>             # explicit upgrade target
lerd service migrate <name> <target-tag>     # SQL dump + restore
lerd service rollback <name>                 # toggle back to previous image
```

## MCP

The `service_control` tool covers everything except the read-only update check, which is its own tool:

```jsonc
{"tool": "service_check_updates"}                   // scan all active defaults
{"tool": "service_check_updates", "args": {"name": "mysql"}}

{"tool": "service_control", "args": {"action": "update",   "name": "mysql"}}
{"tool": "service_control", "args": {"action": "update",   "name": "mysql", "tag": "8.4.9"}}
{"tool": "service_control", "args": {"action": "migrate",  "name": "mysql", "tag": "9.0"}}
{"tool": "service_control", "args": {"action": "rollback", "name": "mysql"}}
```

## Backups directory

Every migration writes a dump file under `~/.local/share/lerd/backups/`, named `<service>-<ts>.{sql,dump}`. Lerd never auto-deletes these — you remove them manually after verifying the new container is correct:

```bash
ls -la ~/.local/share/lerd/backups/
rm ~/.local/share/lerd/backups/<old-dump>
```

The pre-migrate data dirs live alongside the active data dirs at `~/.local/share/lerd/data/<svc>.pre-migrate-<ts>/`. Same convention — clean up by hand once verified.
