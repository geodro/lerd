FROM php:{{.Version}}-fpm-alpine

RUN apk update && apk add --no-cache \
        autoconf \
        make \
        g++ \
        git \
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
        pcntl \
        exif \
        sockets \
    && (docker-php-ext-enable opcache || true) \
    && { (pecl install redis && docker-php-ext-enable redis) \
         || (git clone --depth 1 https://github.com/phpredis/phpredis /tmp/phpredis \
             && cd /tmp/phpredis && phpize && ./configure && make -j$(nproc) && make install \
             && docker-php-ext-enable redis \
             && rm -rf /tmp/phpredis) \
         || true; } \
    && { (pecl install imagick && docker-php-ext-enable imagick) \
         || (git clone --depth 1 https://github.com/Imagick/imagick /tmp/imagick \
             && cd /tmp/imagick && phpize && ./configure && make -j$(nproc) && make install \
             && docker-php-ext-enable imagick \
             && rm -rf /tmp/imagick) \
         || true; } \
    && rm -rf /tmp/pear /var/cache/apk/*

# Override pool: run workers as root, log errors to stderr
RUN printf '[www]\nuser=root\ngroup=root\ncatch_workers_output=yes\nphp_flag[display_errors]=off\nphp_admin_value[error_log]=/proc/self/fd/2\nphp_admin_flag[log_errors]=on\n' > /usr/local/etc/php-fpm.d/zz-lerd.conf
