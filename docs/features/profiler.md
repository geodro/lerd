# Profiler

The profiler answers the question dumps and logs can't: where is a request actually spending its time. lerd bundles [SPX](https://github.com/NoiseByNorthwest/php-spx), a low-overhead PHP profiler, into every PHP-FPM image and surfaces its flame graphs through a single global **Profiler** entry in the dashboard.

The profiler is **off by default**. Turn it on with `lerd profile on`, the Profiler toggle in the dashboard's System health card, the Start profiling button in the Profiler view, or `profiler_toggle` via MCP. It is a single global switch: while it is on, every HTTP request to every PHP-FPM site is profiled. Turning it on or off costs nothing but an nginx reload, no FPM restart and no code changes.

## How it works

The SPX extension is always loaded in the FPM image, but it does nothing until a request opts in. Turning the profiler on flips that opt-in for every site:

1. `lerd profile on` sets a global flag in `~/.config/lerd/config.yaml` and regenerates every PHP-FPM site's nginx vhost.
2. The regenerated vhost injects an `SPX_ENABLED` cookie into the `HTTP_COOKIE` that nginx passes to PHP-FPM. The browser never sees this cookie, so there is nothing to install and nothing third-party-cookie rules can block.
3. SPX sees the cookie, profiles the request, and writes a report to `~/.local/share/lerd/spx/`.
4. The Profiler view embeds SPX's own report UI, so you browse flame graphs without leaving the dashboard.

A second nginx variable, `$spx_key`, carries the SPX auth key. It resolves to an empty string whenever an `X-Forwarded-Host` header is present, so the profiler and its UI stay unreachable through `lerd share` tunnels and LAN shares. The profiler is a local-only tool.

## Turning the profiler on

```bash
lerd profile on        # profile every PHP-FPM site
lerd profile status    # show the state and the SPX UI URL
lerd profile off       # stop profiling
lerd profile clear     # delete every captured report
```

In the dashboard, the System health card carries a Profiler toggle next to the dump bridge one: click it to flip profiling on or off, and a pulsing emerald dot marks it as live. The Sites column carries the same toggle above the site list. The flame icon in the left rail opens the **Profiler** view and shows the same green dot while profiling is on. Inside the Profiler view, the header has a **Start profiling** / **Stop profiling** button and a **Clear data** button. The button is emerald with a live pulsing dot while profiling is on, and muted grey while it is off.

While the profiler is on, every HTTP request to every PHP-FPM site is profiled. Reload the sites you care about a few times, then open the Profiler view to read the reports. The report list refreshes itself while profiling is on, so new captures appear without a manual reload. Turn it off when you are done so the profiler is not adding overhead to every request.

## Reading flame graphs

The Profiler view embeds the SPX report UI, served by a dedicated `profiler.localhost` nginx vhost so it does not depend on any one site. Its landing page is SPX's control panel, the list of captured reports. SPX's Configuration form above the list is collapsed by default so the list takes the whole view; the **Show configuration** button in the header brings it back. Each report lists wall time, CPU time, memory, and the call tree. SPX's time-line view is an interactive flame graph: wide frames are where the time went. Click a frame to zoom, and use the flat-profile table to find the most expensive functions.

All reports land in one shared directory, `~/.local/share/lerd/spx/`, regardless of which site or PHP version produced them. Each report is labelled with its request host and URI so they stay distinguishable.

## Profiling CLI commands

The cookie mechanism only covers HTTP requests. To profile an artisan command, a queue job, or a test run, use `lerd profile run`:

```bash
lerd profile run artisan queue:work --once
lerd profile run artisan app:heavy-report
```

The command runs as `php <command>` inside the project's container with SPX enabled, and the report lands in the same Profiler view alongside the HTTP reports. This works whether or not the global profiler is on.

Because the `php` shim runs PHP inside the container, you can also profile any shim'd PHP tool by setting `SPX_ENABLED=1` in front of it. lerd forwards every `SPX_*` variable from your shell into the container, so `SPX_ENABLED=1 composer update` or `SPX_ENABLED=1 php artisan migrate` is profiled too. When SPX is enabled and you have not set `SPX_REPORT`, lerd defaults it to `full` so the run shows up in the Profiler view rather than only printing a flat profile to the terminal. Other SPX knobs work the same way, for example `SPX_SAMPLING_PERIOD=100 SPX_ENABLED=1 composer update`.

## Open the SPX UI directly

```bash
lerd profile open      # open the SPX web UI in your browser
```

This opens `http://profiler.localhost/?SPX_UI_URI=/`, the same UI the Profiler view embeds.

## Scope and limits

- **PHP-FPM sites only.** FrankenPHP and custom-container sites run images that don't carry the SPX extension, so their requests are not profiled.
- **Sampling, not exact counts.** SPX is a tracing profiler that records wall and CPU time accurately. It is built for "where did the time go", not for exact call counts.
- **Local only.** The profiler is never reachable through tunnels or LAN shares.
- **Reports accumulate** in `~/.local/share/lerd/spx/`. Clear them all at once with the **Clear data** button in the Profiler view header, `lerd profile clear`, or `profiler_clear` via MCP; the SPX UI can also delete reports individually.

## MCP

AI assistants can drive the profiler through three tools:

- `profiler_toggle({ enable })` turns profiling on or off globally.
- `profiler_status()` reports whether profiling is on and the SPX web UI URL.
- `profiler_clear()` deletes every captured report and returns how many were removed.
