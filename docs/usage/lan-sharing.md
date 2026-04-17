# LAN sharing

The quickest way to let another device on the same network reach one of your sites, no DNS setup, no external tools, no internet access required:

```bash
cd ~/Projects/myapp
lerd lan:share
```

```
Sharing myapp at http://192.168.1.42:9100
Other devices on the network can use that URL directly, no DNS setup needed.

█████████████████████████████████
█ ▄▄▄▄▄ █▀▄ █▄█ ▀▀ ▄█ ▄▄▄▄▄ █
...
Run 'lerd lan:unshare' to stop.
```

What it does:

- Assigns a stable port to the site (starting at 9100, incremented to avoid conflicts) and saves it in `sites.yaml`.
- Starts a host-level reverse proxy inside the lerd daemon (`lerd-ui`) listening on `0.0.0.0:<port>`.
- Rewrites the `Host` header on every request so nginx routes to the correct vhost.
- Rewrites absolute URLs (from `https://myapp.test/...` to `http://192.168.1.42:9100/...`) in HTML, CSS, and JS response bodies so assets and redirects work from the client device without a `.test` DNS resolver.
- Prints a QR code you can scan to open the site on a phone.

The port is reused across restarts. Stop sharing with `lerd lan:unshare`.

The dashboard shows the LAN URL next to the HTTPS toggle for each site. Hovering the URL shows a QR code inline.

## When to use LAN sharing vs full LAN exposure

| | `lerd lan:share` | `lerd lan:expose` |
|---|---|---|
| Scope | One site at a time | All sites at once |
| Client DNS setup | Not required, plain `IP:port` | Required (forward `.test` to lerd dnsmasq) |
| Client cert trust | Not required | Required for HTTPS sites |
| External tools | None | None |
| Persists across restarts | Yes (port saved in `sites.yaml`) | Yes (`lan.exposed` in `config.yaml`) |
| Use case | Quick demo to someone on the same wifi | [full remote development setup](remote-development.md) (laptop + server) |
