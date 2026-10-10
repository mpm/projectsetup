# Verified linux/amd64 Debian 12; update this digest deliberately.
FROM debian:bookworm-slim@sha256:a4672c0cb26fbdde88e38fa2dfb6c681942306680e41e4378b28770b6e79ee91
ARG VSCODE_UID=1000
ARG VSCODE_GID=1000
# Establish IDs before installing toolchains. Never delete/renumber other accounts.
RUN set -eu; \
    if getent passwd "$VSCODE_UID"; then echo "requested UID already belongs to an account; choose another base or ID" >&2; exit 1; fi; \
    if getent group "$VSCODE_GID"; then echo "requested GID already belongs to a group; choose another base or ID" >&2; exit 1; fi; \
    groupadd --gid "$VSCODE_GID" vscode; \
    useradd --uid "$VSCODE_UID" --gid "$VSCODE_GID" --create-home --shell /bin/bash vscode
RUN apt-get update && apt-get install -y --no-install-recommends \
    bash ca-certificates curl git gnupg sudo xz-utils \
    && rm -rf /var/lib/apt/lists/* /var/cache/apt/archives/* \
    && printf 'vscode ALL=(ALL) NOPASSWD:ALL\n' > /etc/sudoers.d/vscode \
    && chmod 0440 /etc/sudoers.d/vscode
# Explicit upstream archive/checksum, no moving installer or Dev Container features.
RUN set -eu; \
    curl -fsSL https://github.com/cli/cli/releases/download/v2.67.0/gh_2.67.0_linux_amd64.tar.gz -o /tmp/gh.tar.gz; \
    echo 'd77623479bec017ef8eebadfefc785bafd4658343b3eb6d3f3e26fd5e11368d5  /tmp/gh.tar.gz' | sha256sum -c -; \
    tar -xzf /tmp/gh.tar.gz -C /tmp; \
    install -m 0755 /tmp/gh_2.67.0_linux_amd64/bin/gh /usr/local/bin/gh; \
    rm -rf /tmp/gh.tar.gz /tmp/gh_2.67.0_linux_amd64
ENV HOME=/home/vscode
USER vscode
