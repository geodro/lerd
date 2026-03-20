# Site Management

## Commands

| Command | Description |
|---|---|
| `lerd park [dir]` | Register all Laravel projects inside `dir` (defaults to cwd) |
| `lerd unpark [dir]` | Remove a parked directory and unlink all its sites |
| `lerd link [name]` | Register the current directory as a site |
| `lerd link [name] --domain foo.test` | Register with a custom domain |
| `lerd unlink [name]` | Stop serving the site |
| `lerd sites` | Table view of all registered sites |
| `lerd open [name]` | Open the site in the default browser |
| `lerd share [name]` | Expose the site publicly via ngrok or Expose (auto-detected) |
| `lerd secure [name]` | Issue a mkcert TLS cert and enable HTTPS — updates `APP_URL` in `.env` |
| `lerd unsecure [name]` | Remove TLS and switch back to HTTP — updates `APP_URL` in `.env` |
| `lerd env` | Configure `.env` for the current project with lerd service connection settings |

---

## Domain naming

Directories with real TLDs are automatically normalised — dots are replaced with dashes and the TLD is stripped before appending `.test`.

For example: `admin.astrolov.com` → `admin-astrolov.test`

---

## Name collision handling

When a directory is parked or linked and another site is already registered with the same name:

- **Same path** — treated as a re-link of the same site. The existing registration is updated and the TLS state is preserved.
- **Different path** — the new site is registered with a numeric suffix (`myapp-2`, `myapp-3`, …) so both sites can coexist.

---

## Unlink behaviour for parked sites

When you unlink a site that lives inside a parked directory, the vhost is removed but the registry entry is kept and marked as *ignored* — the watcher will not re-register it on its next scan. Running `lerd link` in that directory clears the ignored flag and restores the site.

---

## Sharing sites

`lerd share` exposes the current site via a public tunnel. Requires [ngrok](https://ngrok.com/download) or [Expose](https://expose.dev) to be installed.

| Command | Description |
|---|---|
| `lerd share` | Share the current site (auto-detects ngrok or Expose) |
| `lerd share <name>` | Share a named site |
| `lerd share --ngrok` | Force ngrok |
| `lerd share --expose` | Force Expose |

The tunnel forwards to nginx's local port with the site's domain as the `Host` header, so nginx routes the request to the right vhost even though the incoming request has the public tunnel URL as its host.
