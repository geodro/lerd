# HTTPS / TLS

Lerd uses [mkcert](https://github.com/FiloSottile/mkcert) — a locally-trusted CA that your browser will accept without warnings.

```bash
cd ~/Lerd/my-app
lerd secure
# Issues a cert for my-app.test, regenerates the SSL vhost, reloads nginx
# Updates APP_URL=https://my-app.test in .env if it exists
# Updates secured: true in .lerd.yaml if it exists
# Visit https://my-app.test — no certificate warning

lerd unsecure
# Removes the cert, switches back to HTTP vhost
# Updates APP_URL=http://my-app.test in .env if it exists
# Updates secured: false in .lerd.yaml if it exists
```

HTTPS can also be enabled during `lerd init` or `lerd setup` — the wizard asks the question upfront and applies it as part of the configuration step.

Certificates are stored in `~/.local/share/lerd/certs/sites/`.

---

## From the Web UI

The Sites tab has an HTTPS toggle per site — clicking it runs `lerd secure` or `lerd unsecure` inline and updates the vhost without touching the terminal. If `.lerd.yaml` exists in the project, the `secured` field is updated there too so the state is preserved for future `lerd init` runs.

---

## Git worktrees

When a site has [git worktrees](git-worktrees.md), securing the parent automatically enables HTTPS for all its worktrees too. Lerd reuses the parent's wildcard certificate (`*.myapp.test`) — no extra `lerd secure` calls needed, and no per-worktree certificate is issued.

Unsecuring the parent switches all worktree vhosts back to HTTP and updates their `.env` files accordingly.

---

## Stripe listener

If a [Stripe webhook listener](../usage/stripe.md#stripelisten) is running for the site, toggling HTTPS automatically restarts it so `--forward-to` points at the correct `http://` or `https://` URL. No manual intervention required.

---

## How it works

1. `lerd install` generates a local CA with mkcert and installs it into the system trust store (NSS databases for Chrome/Firefox, and the system root store).
2. `lerd secure <site>` issues a certificate signed by that CA for `<site>.test` **and** `*.<site>.test` (wildcard), so all subdomain worktrees are covered by a single cert.
3. The nginx vhost is regenerated to listen on port 443 with the new cert, and port 80 redirects to HTTPS (302, not 301, so the redirect is not cached by browsers).
4. `APP_URL` in the project's `.env` (and any worktree `.env` files) is updated to `https://`.
5. If a `lerd stripe:listen` service is active for the site, it is restarted with the updated forwarding URL.
