FROM ubuntu
RUN apt-get update && apt-get install -y \
    clang \
    gcc \
    vsftpd \
    valgrind \
    bsdmainutils \
    && apt-get clean

RUN useradd -m -d /home/ftpuser -s /usr/sbin/nologin ftpuser && \
    echo "ftpuser:motdepasse123" | chpasswd

RUN mkdir -p /home/ftpuser/ftp/upload && \
    chown -R ftpuser:ftpuser /home/ftpuser/ftp && \
    chmod -R 755 /home/ftpuser/ftp

COPY ./conf/vsftpd.conf /etc/vsftpd.conf

RUN mkdir -p /var/run/vsftpd/empty

EXPOSE 21 21100-21110

CMD ["vsftpd", "/etc/vsftpd.conf"]
