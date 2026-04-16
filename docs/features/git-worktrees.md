# Git Worktrees

Lerd automatically detects [git worktrees](https://git-scm.com/docs/git-worktree) and gives each checkout its own subdomain — no configuration needed.

```bash
cd ~/Lerd/myapp

# Add a worktree for a feature branch
git worktree add ../myapp-feature feature/auth

# Lerd immediately creates:
#   http://feature-auth.myapp.test  →  the worktree's document root
```

Branch names are sanitised to be subdomain-safe: `/`, `_`, and `.` are replaced with `-`, and non-alphanumeric characters are stripped.

---

## How it works

When the Lerd watcher daemon is running it watches each registered site's `.git/` directory. As soon as `git worktree add` writes its metadata under `.git/worktrees/`, Lerd:

1. Reads the branch name and checkout path from the worktree metadata.
2. Generates an nginx vhost for `<branch>.<site>.test` pointing at the worktree's document root, using the same framework/public-dir rules as the parent site.
3. Reloads nginx so the subdomain starts serving immediately.

When `git worktree remove` is run the vhost is removed and nginx is reloaded.

Existing worktrees are also picked up on watcher startup, so nothing is lost after a reboot.

---

## Dependency setup

When a worktree vhost is first created, Lerd sets up three things in the checkout directory automatically:

| Resource | Behaviour |
|---|---|
| `vendor/` | Symlinked from the main repo (shares Composer packages) |
| `node_modules/` | Symlinked from the main repo (shares npm packages) |
| `.env` | Copied from the main repo with `APP_URL` rewritten to `http://<branch>.<site>.test` |

If any of these already exist in the worktree they are left untouched.

---

## HTTPS

If the parent site is secured with `lerd secure`, worktree subdomains inherit HTTPS automatically. Lerd reuses the parent site's wildcard mkcert certificate (`*.myapp.test`), so no additional certificate is needed.

```bash
lerd secure myapp
# myapp.test         → https
# feature-auth.myapp.test → https  (automatic)
```

`APP_URL` in each worktree's `.env` is also updated to `https://` when you secure or unsecure the parent.

---

## `lerd sites` output

Worktrees are shown indented under their parent site:

```
NAME            DOMAIN                   PHP    NODE   TLS   PATH
myapp           myapp.test               8.4    22     ✓     ~/Lerd/myapp
↳ feature-auth  feature-auth.myapp.test  8.4    —      —     ~/Lerd/myapp-feature
```

---

## Web UI

In the Sites tab, any site that has active worktrees shows a branch icon in the sidebar. Clicking the site opens its detail panel which lists the worktrees as a tree. The **main checkout's current branch** is shown at the top of the tree with a link to the main site's domain, followed by each worktree branch below it.
