FROM php:8.2-apache

RUN docker-php-ext-install ftp

COPY ./html/ /var/www/html/

RUN mkdir -p /tmp/uploads && chown www-data:www-data /tmp/uploads

RUN { \
    echo 'upload_max_filesize = 100M'; \
    echo 'post_max_size = 100M'; \
    echo 'max_file_uploads = 20'; \
} > /usr/local/etc/php/conf.d/uploads.ini

EXPOSE 80