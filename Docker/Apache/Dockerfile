FROM php:8.2-apache

RUN docker-php-ext-install ftp

COPY ./html/ /var/www/html/

RUN mkdir -p /tmp/uploads && chown www-data:www-data /tmp/uploads

EXPOSE 80