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
        icu-data-full \
        oniguruma-dev \
        libxml2-dev \
        postgresql-dev \
        imagemagick-dev \
        gmp-dev \
        bzip2-dev \
        openldap-dev \
        sqlite-dev \
        libxslt-dev \
    && docker-php-ext-configure gd --with-freetype --with-jpeg --with-webp \
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
    && { (yes '' | pecl install redis && docker-php-ext-enable redis) \
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
    && rm -rf /tmp/pear /var/cache/apk/*

# Xdebug compiled in the builder too. Legacy PHP needs older xdebug majors.
RUN PHPVER="$(php -r 'echo PHP_MAJOR_VERSION,".",PHP_MINOR_VERSION;')" \
    && case "$PHPVER" in \
        7.4) XDEBUG_PKG="xdebug-3.1.6" ;; \
        8.0) XDEBUG_PKG="xdebug-3.3.2" ;; \
        *)   XDEBUG_PKG="xdebug" ;; \
    esac \
    && yes '' | pecl install "$XDEBUG_PKG" && docker-php-ext-enable xdebug \
    && rm -rf /tmp/pear /var/cache/apk/*

# Project-defined custom extensions compile here while the toolchain
# is available. Their .so files travel through the COPY below.
{{.CustomExtensions}}

# ── Runtime stage ───────────────────────────────────────────────────────────
FROM docker.io/library/php:{{.Version}}-fpm-alpine

# Runtime libraries only (no -dev headers, no toolchain). PHP's
# compiled extensions dlopen these. icu-data-full is bulky but
# i18n needs it.
RUN apk update && apk add --no-cache \
        ghostscript \
        imagemagick \
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
        icu-data-full \
        oniguruma \
        libxml2 \
        libpq \
        gmp \
        bzip2 \
        libldap \
        sqlite-libs \
        libxslt \
    && rm -rf /var/cache/apk/*

# Runtime system libs for user-configured custom extensions (e.g.
# imap needs c-client.so). Empty when no custom exts have apk deps.
{{.CustomExtensionsRuntime}}

# Compiled extensions + config from the builder stage; ~25 extensions
# plus xdebug + pecl modules without dragging autoconf/make/g++ across.
COPY --from=builder /usr/local/lib/php/extensions/ /usr/local/lib/php/extensions/
COPY --from=builder /usr/local/etc/php/conf.d/ /usr/local/etc/php/conf.d/

# MariaDB client (mysql-client) connecting to lerd MySQL uses self-signed
# certs; disable SSL verification so CLI tools (mysqldump, schema loading)
# work out of the box.
RUN mkdir -p /etc/my.cnf.d && printf '[client]\nssl=0\n' > /etc/my.cnf.d/lerd-no-ssl.cnf

# Composer from the official image.
COPY --from=composer-bin /usr/bin/composer /usr/local/bin/composer

# Interactive shell for lerd shell. zsh/fzf/bat exist on every alpine;
# starship/eza/zoxide need alpine 3.18+, so legacy php 7.4/8.0 (alpine
# 3.16) get them via || true and the zshrc inits starship conditionally.
RUN apk add --no-cache zsh fzf bat \
    && { apk add --no-cache starship eza zoxide 2>/dev/null || true; } \
    && mkdir -p /etc/zsh /root/.zsh_state \
    && printf 'export EDITOR=vi\nexport PAGER=less\nexport HISTFILE=/root/.zsh_state/history\nexport HISTSIZE=10000\nexport SAVEHIST=10000\nsetopt INC_APPEND_HISTORY SHARE_HISTORY\nautoload -Uz compinit && compinit -u\nif command -v starship >/dev/null 2>&1; then\n  eval "$(starship init zsh)"\nfi\n' \
        > /etc/zsh/zshrc

# Override pool: run workers as root, log errors to stderr
RUN printf '[www]\nuser=root\ngroup=root\ncatch_workers_output=yes\nphp_flag[display_errors]=off\nphp_admin_value[error_log]=/proc/self/fd/2\nphp_admin_flag[log_errors]=on\n' > /usr/local/etc/php-fpm.d/zz-lerd.conf

{{.MkcertCA}}
