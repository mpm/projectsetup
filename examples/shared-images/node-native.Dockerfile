ARG NODE=projectsetup-reference-parent-not-selected:invalid
FROM ${NODE}
USER root
# Opt-in native npm modules/PostgreSQL clients; excluded from ordinary Node.
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential pkg-config libpq-dev \
    && rm -rf /var/lib/apt/lists/* /var/cache/apt/archives/*
USER vscode
