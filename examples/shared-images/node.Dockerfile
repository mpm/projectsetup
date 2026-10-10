ARG FOUNDATION=projectsetup-reference-parent-not-selected:invalid
FROM ${FOUNDATION}
USER root
RUN set -eu; \
    curl -fsSL https://nodejs.org/download/release/v22.14.0/node-v22.14.0-linux-x64.tar.xz -o /tmp/node.tar.xz; \
    echo '69b09dba5c8dcb05c4e4273a4340db1005abeafe3927efda2bc5b249e80437ec  /tmp/node.tar.xz' | sha256sum -c -; \
    mkdir -p /opt/node; tar --no-same-owner -xJf /tmp/node.tar.xz -C /opt/node --strip-components=1; \
    rm /tmp/node.tar.xz
ENV PATH=/opt/node/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
USER vscode
