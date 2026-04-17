# Remote / LAN Development

Lerd supports two remote workflows: per-site LAN sharing (quick demos to someone on the same wifi) via [`lerd lan:share`](lan-sharing.md), and full LAN exposure where a laptop browses `https://myapp.test` against a remote lerd server. This page covers the full setup.

## Full LAN exposure (all sites, DNS-based)

Lerd is designed to run on the machine you're developing from: install it, link a project, browse `myapp.test`, done. But there's a common variant of that workflow worth documenting: **the lerd server is a different machine from the one you're typing on**. Typical case: you have a beefier Linux box (desktop, NAS, mini PC) running the containers, and a laptop you carry around for editing.

This page walks through the full setup so the laptop can browse `https://myapp.test` against a remote server with no per-site `/etc/hosts` maintenance and no certificate warnings.

## Architecture

```
┌──────────── Laptop ────────────┐         ┌──────────── Server ────────────┐
│                                │         │                                │
│  Editor (VSCode Remote / etc.) │ ──ssh── │  ~/Projects/myapp              │
│  Browser to http(s)://myapp.test│ HTTP→  │  nginx :80 / :443              │
│  Resolver forwards .test ────────────→   │  lerd-dns :5300 (LAN)          │
│  mkcert root CA (trusted) ──────────────→│  cert issued by mkcert         │
│                                │         │                                │
└────────────────────────────────┘         └────────────────────────────────┘
```

The server runs lerd normally: containers, watcher, dnsmasq, nginx, certs. The laptop forwards `.test` queries to the server's dnsmasq and trusts the server's mkcert root CA. After that, every site you create on the server is reachable from the laptop with no laptop-side action.

## Server-side setup

### 1. Install lerd

Same as any other lerd install:

```bash
curl -sSL https://geodro.github.io/lerd/install.sh | bash
```

### 2. Keep user services running after logout

By default systemd shuts down a user's services when they log out. On a headless server you want lerd's containers to keep running:

```bash
sudo loginctl enable-linger $(whoami)
```

This persists across reboots.

### 3. Open the firewall

The laptop needs to reach:

| Port | Protocol | Purpose |
|---|---|---|
| 80 | TCP | nginx (HTTP) |
| 443 | TCP | nginx (HTTPS) |
| 5300 | UDP + TCP | lerd dnsmasq |
| 7073 | TCP | lerd dashboard + remote-setup endpoint |

For `ufw`:

```bash
sudo ufw allow from 192.168.1.0/24 to any port 80,443,5300,7073 proto tcp
sudo ufw allow from 192.168.1.0/24 to any port 5300 proto udp
```

For `firewalld`:

```bash
sudo firewall-cmd --permanent --add-port=80/tcp
sudo firewall-cmd --permanent --add-port=443/tcp
sudo firewall-cmd --permanent --add-port=5300/tcp
sudo firewall-cmd --permanent --add-port=5300/udp
sudo firewall-cmd --permanent --add-port=7073/tcp
sudo firewall-cmd --reload
```

### 4. Expose lerd to the LAN

By default lerd binds nginx to `127.0.0.1`, so sites are invisible to the network, safe for untrusted wifi (cafés, conference networks, hotels). Flip that with the unified LAN exposure switch:

```bash
lerd lan:expose
```

This single command:

- Rewrites the `lerd-nginx` quadlet so its `PublishPort=` bindings drop the `127.0.0.1:` prefix (port 80 / 443 become reachable from other devices on the LAN). **Service containers stay on `127.0.0.1` in both modes**; Laravel apps reach them through the internal podman bridge using container DNS names (`DB_HOST=lerd-mysql`, etc.), so there's no reason to expose mysql/postgres/redis/meilisearch/rustfs/mailpit ports to the network. If you need TablePlus or another tool from a second machine, use SSH port forwarding instead.
- Restarts `lerd-nginx` so the new bind takes effect.
- Updates the dnsmasq config so `.test` queries return the server's auto-detected LAN IP instead of `127.0.0.1`, and starts the userspace `lerd-dns-forwarder.service` that bridges `LAN-IP:5300` to `127.0.0.1:5300` (rootless pasta cannot accept LAN-side traffic on its own).
- Persists `lan.exposed: true` in `~/.config/lerd/config.yaml` so reboots and reinstalls restore the exposed state.

Reverse with `lerd lan:unexpose` (also revokes any outstanding remote-setup code). Inspect the current state with `lerd lan:status`.

You can do the same thing from the dashboard: in **Lerd settings > LAN exposure**, click **Expose to LAN** and watch the per-step progress stream live.

The dashboard at port 7073 is gated independently. By default it returns 403 to LAN clients even when `lan:expose` is on; set HTTP Basic auth credentials with `lerd remote-control on` (or via the **Remote dashboard access** card in the dashboard) to grant LAN access. The two switches are independent: you can have sites LAN-reachable without exposing the dashboard, or vice versa.

### 5. Generate a one-time setup code

```bash
lerd remote-setup
```

This auto-enables `lerd lan:expose` when it isn't already active, then generates a one-time code (15-minute TTL by default).

Outputs something like:

```
Ensuring lerd is exposed on the LAN...
  • Saving LAN exposure flag
  • Rewriting container quadlets
  • Restarting lerd-nginx
  • Detecting primary LAN IP
  • Updating dnsmasq config (.test to 192.168.1.42)
  • Restarting lerd-dns
  • Installing lerd-dns-forwarder.service
  • Starting lerd-dns-forwarder
  • Done, lerd is reachable on 192.168.1.42

  Code: aB3xY9zQ
  Expires in: 15m0s

On the machine, run:

  curl -sSL 'http://192.168.1.42:7073/api/remote-setup?code=aB3xY9zQ' | bash

The endpoint is restricted to RFC 1918 private source IPs and the code
is single-use. Re-run this command to generate a new one if it expires.
```

The code is single-use (consumed on the first successful call) and expires after 15 minutes by default. Customize with `--ttl 1h`. Revoke an active code without generating a new one with `lerd remote-setup --revoke`.

## Laptop-side setup

Run the curl one-liner from the previous step on the laptop:

```bash
curl -sSL 'http://192.168.1.42:7073/api/remote-setup?code=aB3xY9zQ' | bash
```

The endpoint generates a self-contained bash script tailored to the calling client and pipes it into `bash`. The script:

1. Installs `mkcert` if missing (apt / dnf / pacman / brew)
2. Decodes the embedded lerd root CA (public only, the private key never leaves the server) into `$(mkcert -CAROOT)/rootCA.pem`
3. Runs `mkcert -install` to register the CA in the system trust store
4. Detects your local resolver and writes the appropriate dnsmasq dropin:
   - **NetworkManager dnsmasq plugin** (most desktop Linux distros): `/etc/NetworkManager/dnsmasq.d/lerd.conf`
   - **Standalone dnsmasq**: `/etc/dnsmasq.d/lerd.conf`
   - **macOS**: `/etc/resolver/test` (per-domain resolver, native macOS feature)
   - **systemd-resolved alone**: unsupported (it can't forward to a non-standard port); the script tells you to install dnsmasq locally
5. Restarts the resolver and prints a verification block

The script will prompt for `sudo` when it needs to write system files. Re-running is idempotent.

### Security model

The `/api/remote-setup` endpoint is **opt-in** and gated three ways:

- **Disabled until a code is generated**: the endpoint returns HTTP 404 (indistinguishable from "no such route") whenever no token is active. A network scanner probing the dashboard can't tell the endpoint exists until you run `lerd remote-setup`. After successful use the code is revoked and the endpoint disappears again.
- **Source IP**: with an active token, only RFC 1918 private addresses (10/8, 172.16/12, 192.168/16) and loopback are accepted. Public-internet requests are rejected with HTTP 403 so the legitimate user can diagnose a misconfigured VPN.
- **One-time code**: the code from `lerd remote-setup` must match exactly and not be expired. Successful calls revoke the code immediately so it can't be replayed.

The endpoint serves the script over plain HTTP (the dashboard runs on HTTP). Use only on trusted LANs.

## Verifying the setup

From the laptop:

```bash
# DNS resolves
dig myapp.test

# HTTPS works without cert warnings
curl -v https://myapp.test 2>&1 | grep -E "subject|SSL connection"

# Open in the browser
open https://myapp.test
```

The lerd dashboard is now also reachable at `http://<server-lan-ip>:7073` from the laptop.

## Troubleshooting

### Cert errors despite the script reporting success

- Make sure you ran the curl command with `| bash`, not just downloaded it. The trust store install runs as part of the script.
- Firefox uses its own cert store. Restart Firefox after running the script. If Firefox was running during install, mkcert may not have detected its profile.
- Chrome on Linux uses the NSS store. Some Chromium flavors (snap, flatpak) sandbox the trust store and won't see system additions. Run Chrome from the regular package or install the cert into the snap/flatpak's namespace manually.

### `.test` resolves but the browser hits localhost

You skipped step 4 (`lerd lan:expose`) on the server, so the dnsmasq config is still answering `127.0.0.1`. Re-run `lerd lan:expose`, then re-run `lerd remote-setup` and the curl one-liner.

### `.test` stopped resolving after the server moved networks

The bootstrap script writes the lerd server's LAN IP into the resolver dropin (NetworkManager dnsmasq config, systemd-resolved DNS=, dnsmasq.d, or `/etc/resolver/test`). If the server's LAN IP later changes (DHCP renew, new wifi, dock change, reboot on a different network) that hardcoded IP becomes stale and lookups fail or time out.

There's no automatic recovery (the server doesn't push updates and the remote machine doesn't poll). To fix it, **re-bootstrap**:

1. On the server, run `lerd remote-setup` again. It auto-detects the new LAN IP and prints a fresh curl one-liner.
2. On the remote machine, run the new curl one-liner. It overwrites the resolver dropin in place, no need to revert anything first.

If you move between networks regularly, consider giving the server a stable hostname via Tailscale, a router DHCP reservation, or a local DNS entry, and using that instead of an IP. That's outside lerd's scope but eliminates the re-bootstrap step.

### "remote-setup is only available from private LAN addresses"

The endpoint refuses requests from non-RFC-1918 source IPs as a defense-in-depth measure. If your laptop is on a non-private network (corporate VPN with public-range internal IPs, etc.) the check will reject. Workaround: SSH into the server, copy the mkcert root CA manually (`cat $(mkcert -CAROOT)/rootCA.pem`), and configure the laptop by hand following the manual steps below.

### Token expired before I could use it

Generate a new one: `lerd remote-setup --ttl 1h` for a longer window.

### `loginctl enable-linger` didn't help, services still die at logout

```bash
loginctl show-user $(whoami) | grep Linger
# Should print: Linger=yes
```

If linger is on but services still exit, make sure you're starting them under the user's systemd unit manager (`systemctl --user`), not the system one.

### Vite / HMR doesn't work from the laptop

Vite and similar dev servers default to binding `localhost` only. Add `--host 0.0.0.0` to your dev script or set `server.host = '0.0.0.0'` in `vite.config.js`. The HMR websocket also needs to point at the right hostname; set `server.hmr.host = 'myapp.test'` so the laptop reaches the right backend.

## Manual setup (no API)

If you can't or don't want to use the `/api/remote-setup` endpoint, every step is reproducible by hand:

### Install mkcert and trust the server's CA

On the server:

```bash
cat $(mkcert -CAROOT)/rootCA.pem | base64
# scp the file or paste the base64 into the laptop's terminal
```

On the laptop:

```bash
# Linux
sudo apt install mkcert libnss3-tools    # or dnf / pacman equivalents

# macOS
brew install mkcert nss

# Drop the CA into mkcert's store and install into the system trust
echo "<base64 paste>" | base64 --decode > "$(mkcert -CAROOT)/rootCA.pem"
mkcert -install
```

### Configure DNS forwarding

**Linux with NetworkManager + dnsmasq plugin** (Ubuntu / Fedora desktop / etc.):

```bash
echo 'server=/test/192.168.1.42#5300' \
  | sudo tee /etc/NetworkManager/dnsmasq.d/lerd.conf
sudo systemctl restart NetworkManager
```

**Linux with standalone dnsmasq** (Arch / Alpine / etc.):

```bash
echo 'server=/test/192.168.1.42#5300' \
  | sudo tee /etc/dnsmasq.d/lerd.conf
sudo systemctl restart dnsmasq
```

**Linux with systemd-resolved only**: not directly supported. `[Resolve] DNS=` doesn't accept a port. Install `dnsmasq` locally and forward to it from systemd-resolved.

**macOS**:

```bash
sudo mkdir -p /etc/resolver
sudo tee /etc/resolver/test <<EOF
nameserver 192.168.1.42
port 5300
EOF
```

The macOS resolver picks `/etc/resolver/<tld>` files up automatically, no service restart needed.

## Dashboard remote access

The dashboard at port 7073 is gated by **two independent flags**:

| `cfg.LAN.Exposed` | `cfg.UI.PasswordHash` | LAN client to :7073 |
|---|---|---|
| off (default)    | empty (default) | 403, LAN exposure off                                       |
| off              | set             | 403, LAN exposure off (credentials are inert)              |
| on               | empty           | 403, no credentials configured                              |
| on               | set             | HTTP Basic auth required                                     |

Loopback (`127.0.0.1`, `::1`) always bypasses both checks, you can never lock yourself out of your own machine. The `/api/remote-setup` endpoint is independent of the gate (it has its own token + IP + brute-force protection) so laptop bootstrap still works before you set credentials.

`cfg.LAN.Exposed` is the **top-level** flag: even with valid credentials, LAN clients get 403 if `lan:expose` is off. This makes `lan:unexpose` a complete kill switch: stale credentials from a prior expose session can't survive it.

To open the dashboard up to LAN clients you need both flags on:

```bash
lerd lan:expose          # 1. flip nginx to LAN, set up dnsmasq forwarder
lerd remote-control on   # 2. set the Basic auth credentials
# Username: george         (defaults to $USER, override with --user)
# Password: ********
# Confirm:  ********
# Remote dashboard access enabled.
```

The password is bcrypt-hashed (default cost) and stored in `~/.config/lerd/config.yaml`. From this point on, loopback bypasses everything; LAN requests must present HTTP Basic auth. Re-running `lerd remote-control on` rotates the password.

Disable either flag at any time:

```bash
lerd remote-control off  # clear credentials, LAN clients get 403
lerd lan:unexpose        # flip nginx back to 127.0.0.1, stop forwarder
```

Check the current state with `lerd remote-control status` and `lerd lan:status`. Both can be run from a loopback shell at any time, even if you've forgotten the password.

The browser handles the Basic auth prompt natively the first time the user visits `http://<server-ip>:7073`; they're prompted once and the credentials are cached for the session. There is no separate UI login screen.

`lerd remote-control on` refuses to run when `lan:expose` is off, since dashboard credentials are meaningless while the dashboard is loopback-only.

## Security caveats

- **Coffee shop wifi: leave `lan:expose` off.** That's the default and it binds nginx to `127.0.0.1` only, so sites are invisible to other devices on the network. Service containers (mysql, postgres, redis, mailpit, etc.) are *always* loopback-only regardless of `lan:expose`, so even with the LAN flag on, your dev databases are not network-reachable. Only run `lerd lan:expose` on networks you trust.
- **`lerd lan:expose` makes your dnsmasq an open recursive resolver for anyone on the LAN.** Lock down with firewall rules to your subnet, not 0.0.0.0/0.
- **The mkcert root CA has authority over any HTTPS site on the trusting machine.** Only install the CA on devices you own. Treat the private key (which never leaves the server) as a high-value secret.
- **The `/api/remote-setup` endpoint hands out the public CA to anyone who can pass the source-IP and code checks.** Don't share active codes.
- **`lerd remote-control on` uses HTTP, not HTTPS.** The Basic auth credentials travel in plaintext on the LAN. Use only on networks you trust. The dashboard does not currently support HTTPS for itself; if you need TLS, run lerd behind a reverse proxy that terminates HTTPS.

## Reverting

To roll back the laptop side:

```bash
# Remove the resolver dropin (Linux)
sudo rm /etc/NetworkManager/dnsmasq.d/lerd.conf       # NetworkManager
sudo rm /etc/dnsmasq.d/lerd.conf                      # standalone dnsmasq
sudo systemctl restart NetworkManager                 # or dnsmasq

# Remove the resolver dropin (macOS)
sudo rm /etc/resolver/test

# Untrust the lerd root CA
mkcert -uninstall
rm "$(mkcert -CAROOT)/rootCA.pem"
```

To roll back the server side:

```bash
lerd remote-control off          # clear dashboard credentials
lerd lan:unexpose                # bind nginx back to 127.0.0.1, stop dns-forwarder
sudo ufw delete allow ...        # close the firewall ports
```

Both commands are idempotent and revoke any outstanding remote-setup tokens as part of the unexpose path.
