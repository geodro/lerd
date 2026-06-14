# Lerd FrankenPHP image: the upstream dunglas/frankenphp base plus the same
# runtime PHP extensions the lerd FPM image ships, so a FrankenPHP (Octane) site
# has redis/gd/pdo/intl/... plus Xdebug and the lerd_devtools / dump bridge
# tooling. {{.Version}} is the PHP minor (e.g. 8.4); the extension list, extra
# packages, and mkcert CA are injected by the builder.
#
# SPX and pcov are intentionally excluded: SPX is a per-request profiler that
# doesn't hook Octane's resident-worker request loop (its /_spx UI 404s under
# Octane and lerd's profiler injection is fastcgi-only), and pcov coverage runs
# only via CLI, which execs into the shared FPM container. Both stay FPM-only.
FROM docker.io/dunglas/frankenphp:php{{.Version}}-alpine

# Runtime extensions + Xdebug. install-php-extensions (shipped in the base) builds
# each for the ZTS runtime, pulls its runtime libs, and trims the build toolchain.
# Xdebug loads but stays inert until its bind-mounted 99-xdebug.ini arms it,
# exactly like the FPM image.
RUN install-php-extensions {{.Extensions}} xdebug \
    && rm -rf /tmp/* /var/cache/apk/*

# nodejs+npm so the Octane file-watcher (lerd octane:reload on) and JS tooling
# work without an apk add at container start, matching the FPM image.
RUN apk add --no-cache nodejs npm && rm -rf /var/cache/apk/*

# lerd_devtools: lerd's engine-level Debug-window capture (queries, mail, views,
# events, jobs, http), compiled here for the ZTS base since install-php-extensions
# can't build it. The marker hashes the extension source so any change rebuilds
# the image; the || true degrades a compile failure to "Debug window unavailable"
# rather than bricking the image.
# lerd_devtools-src-sha256: {{.DevtoolsHash}}
COPY internal/podman/devtools /tmp/lerd-devtools
RUN apk add --no-cache --virtual .lerd-devtools-build autoconf make g++ \
    && { cd /tmp/lerd-devtools && phpize && ./configure --enable-lerd-devtools \
         && make -j"$(nproc)" && make install && docker-php-ext-enable lerd_devtools; } || true; \
    apk del .lerd-devtools-build || true; \
    rm -rf /tmp/lerd-devtools /var/cache/apk/*

# User-requested extra Alpine packages (lerd php:pkg). Empty until opted in.
{{.CustomPackages}}

# Lerd mkcert CA so the app trusts local .test HTTPS from inside the container.
{{.MkcertCA}}
