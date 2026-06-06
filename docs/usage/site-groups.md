---
title: Site Groups
description: Group related sites so one main site owns a base domain and secondaries live on subdomains of it
---

# Site Groups

Site groups let related sites share one base domain. One site is the **main** and owns the base domain (for example `astrolov.test`); each **secondary** occupies a subdomain of it (`admin.astrolov.test`). This mirrors production, where the app lives on the apex domain and the admin lives on an `admin.` subdomain, without having to run the admin on an unrelated `.test` domain locally.

A secondary stays a completely independent site: its own project path, PHP version, workers, env and certificate. Grouping only changes the domain it answers on. Under the hood lerd gives the secondary an exact-match nginx vhost (`admin.astrolov.test`), and nginx prefers that exact host over the main's `*.astrolov.test` wildcard, so the subdomain routes to the secondary while everything else still hits the main. This is the same mechanism [git worktree subdomains](../features/git-worktrees.md) already use.

## Grouping sites in the web UI

Open a site in the [web UI](../features/web-ui.md) and click the group icon next to the domain. For a site that isn't grouped yet you first choose its role:

- **This is the main** — pick an ungrouped site to add as a secondary under it. The subdomain label is pre-filled from that site's name (`admin-astrolov` becomes `admin`) and is editable before you confirm, and a "Share the main's database" checkbox lets the secondary use the main's database instead of its own.
- **Secondary of…** — pick which site this one should sit under, set its subdomain label, and optionally share that main's database. Confirming makes the current site a secondary.

So you can build a group from either end: add secondaries from the main, or attach the current site under another main. Once a group exists you can also:

- Edit a secondary's subdomain label.
- Toggle whether each secondary shares the main's database or keeps its own.
- Remove a secondary, which restores its standalone domain.
- Dissolve the whole group.

In the sites list a secondary appears directly under its main, marked with a group icon. Opening a secondary shows which main it belongs to and lets you change its label, toggle the shared database, or ungroup it.

When you group an existing site, its old standalone domain is replaced by the subdomain, matching production. For example `admin-astrolov.test` becomes `admin.astrolov.test`.

## Sharing the main's database

By default a secondary keeps its own database; grouping never touches its `DB_DATABASE`. If the secondary is really part of the same application as the main (a separate admin frontend over the same data, for example), turn on **Share the main's database**. Lerd then points the secondary's `DB_DATABASE` at the main's database and keeps it there, so running the env wizard on the secondary won't reset it back to its own name. Turning sharing back off, or ungrouping the site, restores its own database name.

Sharing assumes both sites use the same lerd-managed database service (the usual case for a related main and admin). It changes only the database name, not the connection host or credentials, which are already identical across sites on the same service.

## Grouping from the CLI

Run the commands from the secondary site's directory:

```bash
cd ~/Projects/admin-astrolov
lerd group add astrolov admin     # admin-astrolov.test -> admin.astrolov.test
lerd group add astrolov admin --share-db   # ...and share astrolov's database
lerd group label backoffice       # change the subdomain to backoffice.astrolov.test
lerd group db share               # share the main's database
lerd group db separate            # go back to a separate database
lerd group remove                 # restore the standalone domain
lerd group list                   # show all groups and their members
```

`lerd group add` takes the main site (by name or domain) and the subdomain label.

`lerd sites`, the `lerd tui` dashboard, and `lerd group list` all show the grouping: a secondary is listed directly under its main, marked with a `↳` and a `group` label. The TUI detail pane also notes whether a site is a group main (with a secondary count) or a secondary of another site, and whether it shares the main's database.

## How it interacts with other features

**Git worktrees.** A worktree of the main repo whose branch sanitises to the same label as a secondary (a branch named `admin` when `admin.astrolov.test` is a secondary) would collide on the same host. Lerd reserves group subdomains: it refuses to assign a label a current main-repo worktree already uses, and it never generates a worktree vhost for a host a secondary already owns. The worktree checkout still exists, it just isn't served on that reserved subdomain.

**Multi-tenant subdomains.** If the main uses wildcard tenant subdomains (via `env_overrides` in `.lerd.yaml`), a grouped subdomain is carved out of that wildcard space: `admin.astrolov.test` is served by the secondary instead of being treated as a tenant of the main. The UI shows a warning when you group a secondary under such a main.

**Renaming the base domain.** Changing the main's domain cascades to every secondary automatically: each one's subdomain is recomputed against the new base domain and its vhost, certificate and `.env` are regenerated.

## Limitations

Groups are one level deep: a secondary cannot itself have secondaries, and a site that is already a secondary cannot be a group main. To change which site is the main, dissolve the group and recreate it.
