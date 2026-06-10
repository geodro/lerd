FROM docker.io/library/composer:latest AS composer-bin

# Builder stage: compile every PHP extension so the build toolchain
# never lands in the final image. .so files and configs travel into
# the runtime stage via COPY at the bottom.
FROM docker.io/library/php:{{.Version}}-fpm-alpine AS builder

RUN apk update && apk add --no-cache \
        autoconf \
        make \
        g++ \
        git \
        linux-headers \
        curl-dev \
        libzip-dev \
        libpng-dev \
        libjpeg-turbo-dev \
        freetype-dev \
        libwebp-dev \
        icu-dev \
        oniguruma-dev \
        libxml2-dev \
        postgresql-dev \
        imagemagick-dev \
        gmp-dev \
        bzip2-dev \
        openldap-dev \
        sqlite-dev \
        libxslt-dev \
        zlib-dev \
    && PHP_ID="$(php -r 'echo PHP_VERSION_ID;')" \
    && if [ "$PHP_ID" -lt 70400 ]; then \
           docker-php-ext-configure gd --with-freetype-dir=/usr --with-jpeg-dir=/usr --with-png-dir=/usr --with-webp-dir=/usr; \
       else \
           docker-php-ext-configure gd --with-freetype --with-jpeg --with-webp; \
       fi \
    && docker-php-ext-install -j$(nproc) \
        curl \
        pdo_mysql \
        pdo_pgsql \
        bcmath \
        mbstring \
        xml \
        zip \
        gd \
        intl \
        pcntl \
        exif \
        sockets \
        gmp \
        bz2 \
        calendar \
        dba \
        ldap \
        mysqli \
        soap \
        shmop \
        sysvmsg \
        sysvsem \
        sysvshm \
        xsl \
    && (docker-php-ext-enable opcache || true) \
    && if [ "$PHP_ID" -lt 70400 ]; then REDIS_PKG=redis-5.3.7; else REDIS_PKG=redis; fi \
    && { (yes '' | pecl install "$REDIS_PKG" && docker-php-ext-enable redis) \
         || (git clone --depth 1 https://github.com/phpredis/phpredis /tmp/phpredis \
             && cd /tmp/phpredis && phpize && ./configure && make -j$(nproc) && make install \
             && docker-php-ext-enable redis \
             && rm -rf /tmp/phpredis) \
         || true; } \
    && { (yes '' | pecl install imagick && docker-php-ext-enable imagick) \
         || (git clone --depth 1 https://github.com/Imagick/imagick /tmp/imagick \
             && cd /tmp/imagick && phpize && ./configure && make -j$(nproc) && make install \
             && docker-php-ext-enable imagick \
             && rm -rf /tmp/imagick) \
         || true; } \
    && { (yes '' | pecl install igbinary && docker-php-ext-enable igbinary) || true; } \
    && { (yes '' | pecl install mongodb && docker-php-ext-enable mongodb) || true; } \
    && { (yes '' | pecl install pcov && docker-php-ext-enable pcov) || true; } \
    && { (git clone --depth 1 --branch release/latest https://github.com/NoiseByNorthwest/php-spx /tmp/php-spx \
          && cd /tmp/php-spx && phpize && ./configure && make -j$(nproc) && make install \
          && docker-php-ext-enable spx) || true; } \
    && mkdir -p /usr/local/share/misc/php-spx/assets/web-ui \
    && rm -rf /tmp/php-spx /tmp/pear /var/cache/apk/*

# Xdebug compiled in the builder too. Legacy PHP needs older xdebug majors.
RUN PHPVER="$(php -r 'echo PHP_MAJOR_VERSION,".",PHP_MINOR_VERSION;')" \
    && case "$PHPVER" in \
        7.2) XDEBUG_PKG="xdebug-3.1.6" ;; \
        7.4) XDEBUG_PKG="xdebug-3.1.6" ;; \
        8.0) XDEBUG_PKG="xdebug-3.3.2" ;; \
        *)   XDEBUG_PKG="xdebug" ;; \
    esac \
    && yes '' | pecl install "$XDEBUG_PKG" && docker-php-ext-enable xdebug \
    && rm -rf /tmp/pear /var/cache/apk/*

# lerd_devtools: lerd's engine-level Debug-window capture (queries, mail, views,
# events, jobs, http). Compiled in the builder so its .so and the
# docker-php-ext-enable conf.d travel into the runtime stage via the
# COPY --from=builder below, like every other extension, so users pull it
# ready-built instead of compiling C on their own machine. The marker line
# hashes the extension source so any change to it drifts the image hash and
# rebuilds the base; TestDevtoolsSourceMarkerInSync keeps the marker honest.
# No-op at runtime on PHP < 8.0 (no zend_observer); the || true degrades a
# compile failure to "Debug window unavailable" rather than bricking the image.
# lerd_devtools-src-sha256: 0afa730e05b7
COPY internal/podman/devtools /tmp/lerd-devtools
RUN { cd /tmp/lerd-devtools && phpize && ./configure --enable-lerd-devtools && make -j$(nproc) && make install && docker-php-ext-enable lerd_devtools; } || true; \
    rm -rf /tmp/lerd-devtools /var/cache/apk/*

# Project-defined custom extensions compile here while the toolchain
# is available. Their .so files travel through the COPY below.
{{.CustomExtensions}}

# ── Runtime stage ───────────────────────────────────────────────────────────
FROM docker.io/library/php:{{.Version}}-fpm-alpine

# Runtime libraries only (no -dev headers, no toolchain). PHP's
# compiled extensions dlopen these.
RUN apk update && apk add --no-cache \
        ghostscript \
        imagemagick \
        libgomp \
        ffmpeg \
        git \
        mysql-client \
        nodejs \
        npm \
        libzip \
        libpng \
        libjpeg-turbo \
        freetype \
        libwebp \
        icu-libs \
        oniguruma \
        libxml2 \
        libpq \
        gmp \
        bzip2 \
        libldap \
        sqlite-libs \
        libxslt \
    && rm -rf /var/cache/apk/*

# icu-data-full carries the full CLDR locale set for ext-intl (#332). Alpine
# 3.16+ ships it as a separate package; older bases fold the full data into
# icu-libs, so the package is absent there and the install is skipped.
RUN apk add --no-cache icu-data-full 2>/dev/null || true

# Runtime system libs for user-configured custom extensions (e.g.
# imap needs c-client.so). Empty when no custom exts have apk deps.
{{.CustomExtensionsRuntime}}

# User-requested extra Alpine packages (lerd php:pkg). Empty until opted in.
{{.CustomPackages}}

# Compiled extensions + config from the builder stage; ~25 extensions
# plus xdebug + pecl modules without dragging autoconf/make/g++ across.
COPY --from=builder /usr/local/lib/php/extensions/ /usr/local/lib/php/extensions/
COPY --from=builder /usr/local/etc/php/conf.d/ /usr/local/etc/php/conf.d/

# SPX profiler web UI assets (shipped as files, not embedded in the .so). The
# builder's mkdir -p guarantees this path exists even if the SPX build failed.
COPY --from=builder /usr/local/share/misc/php-spx/ /usr/local/share/misc/php-spx/

# MariaDB client (mysql-client) connecting to lerd MySQL uses self-signed
# certs; disable SSL verification so CLI tools (mysqldump, schema loading)
# work out of the box.
RUN mkdir -p /etc/my.cnf.d && printf '[client]\nssl=0\n' > /etc/my.cnf.d/lerd-no-ssl.cnf

# Composer from the official image.
COPY --from=composer-bin /usr/bin/composer /usr/local/bin/composer

# Interactive shell for lerd shell. zsh/fzf exist on every alpine base;
# bat lands on 3.16+ and starship/eza/zoxide on 3.18+, so the optional
# tools install tolerantly and zshrc inits starship only when present.
RUN apk add --no-cache zsh fzf \
    && { apk add --no-cache bat 2>/dev/null || true; } \
    && { apk add --no-cache starship eza zoxide 2>/dev/null || true; } \
    && mkdir -p /etc/zsh /root/.zsh_state \
    && printf 'export EDITOR=vi\nexport PAGER=less\nexport HISTFILE=/root/.zsh_state/history\nexport HISTSIZE=10000\nexport SAVEHIST=10000\nsetopt INC_APPEND_HISTORY SHARE_HISTORY\nautoload -Uz compinit && compinit -u\nif command -v starship >/dev/null 2>&1; then\n  eval "$(starship init zsh)"\nfi\n' \
        > /etc/zsh/zshrc

# Override pool: run workers as root, log errors to stderr
RUN printf '[www]\nuser=root\ngroup=root\ncatch_workers_output=yes\nphp_flag[display_errors]=off\nphp_admin_value[error_log]=/proc/self/fd/2\nphp_admin_flag[log_errors]=on\n' > /usr/local/etc/php-fpm.d/zz-lerd.conf

{{.MkcertCA}}
