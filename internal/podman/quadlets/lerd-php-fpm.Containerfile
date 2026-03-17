FROM php:{{.Version}}-fpm-alpine

RUN apk add --no-cache \
        autoconf \
        make \
        g++ \
        curl-dev \
        libzip-dev \
        libpng-dev \
        libjpeg-turbo-dev \
        freetype-dev \
        icu-dev \
        oniguruma-dev \
        libxml2-dev \
        postgresql-dev \
        linux-headers \
        imagemagick-dev \
        imagemagick \
    && docker-php-ext-configure gd --with-freetype --with-jpeg \
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
        opcache \
        pcntl \
        exif \
        sockets \
    && pecl install redis imagick \
    && docker-php-ext-enable redis imagick \
    && rm -rf /tmp/pear /var/cache/apk/*

# Override pool: run workers as root, log errors to stderr
RUN printf '[www]\nuser=root\ngroup=root\ncatch_workers_output=yes\nphp_flag[display_errors]=off\nphp_admin_value[error_log]=/proc/self/fd/2\nphp_admin_flag[log_errors]=on\n' > /usr/local/etc/php-fpm.d/zz-lerd.conf
