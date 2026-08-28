# projectsetup

`projectsetup` is an opinionated Go CLI that generates deterministic Dev Container configurations for existing Node, Rails, and Python projects, designed for use with `dworm`.

## Status and scope

The current implementation provides interactive and flag-driven `init`, static `check`, host diagnostics with `doctor`, and optional Dev Container build validation. It supports:

- Node with npm, pnpm, or Yarn
- Rails
- Python with pip, Poetry, or uv
- Optional PostgreSQL 17 sidecar
- OpenCode by default, optional Claude Code, and GitHub CLI

User templates, plugins, migration of hand-written configurations, databases other than PostgreSQL, Alpine/musl images, Windows containers, and application generation are out of scope.

## Prerequisites

- Go 1.23 or newer when installing from source
- Docker with Docker Compose for external configuration checks and build validation
- [Dev Container CLI](https://github.com/devcontainers/cli) (`devcontainer`) for external configuration checks and `check --build`
- `dworm` for the intended container workflow

Docker and `devcontainer` are not required merely to generate files. A normal `check` reports missing external tools as warnings; `check --build` requires `devcontainer`.

## Installation

Install the latest release for Linux or macOS (`amd64` or `arm64`) into `~/.local/bin`:

```bash
curl -fsSL https://raw.githubusercontent.com/mpm/projectsetup/main/install.sh | sh
```

Set `PROJECTSETUP_INSTALL_DIR` to choose another directory, or pass a release tag to install a specific version:

```bash
curl -fsSL https://raw.githubusercontent.com/mpm/projectsetup/main/install.sh | PROJECTSETUP_INSTALL_DIR=/usr/local/bin sh
curl -fsSL https://raw.githubusercontent.com/mpm/projectsetup/main/install.sh | sh -s v0.1.0
```

Release archives and SHA-256 checksums are also available on the [GitHub Releases page](https://github.com/mpm/projectsetup/releases).

Install from source into `GOBIN` (or `$(go env GOPATH)/bin`):

```bash
go install github.com/mpm/projectsetup/cmd/projectsetup@latest
```

Or build a local executable:

```bash
mkdir -p bin
go build -o bin/projectsetup ./cmd/projectsetup
```

## Initialize a project

Run commands from the project root. Interactive mode detects project files, asks only for unresolved choices, shows the normalized configuration, and requests confirmation before writing:

```bash
projectsetup init
```

For automation, add `--non-interactive`; unresolved or ambiguous required choices fail instead of prompting:

```bash
projectsetup init --non-interactive --preset node
projectsetup init --non-interactive --preset python --python-version 3.13 --package-manager uv
projectsetup init --non-interactive --preset rails --database postgres --ai opencode,claude
projectsetup init --non-interactive --preset node --port 3000 --port 5173 --system-package imagemagick
```

Available `init` flags:

```text
--preset node|rails|python
--name NAME
--database none|postgres
--ai opencode|opencode,claude|none
--node-version VERSION
--ruby-version VERSION
--python-version VERSION
--package-manager npm|pnpm|yarn|pip|poetry|uv
--port PORT                  repeatable
--system-package PACKAGE     repeatable apt package
--non-interactive
--force
```

Defaults are detected from version files, manifests, and lockfiles. Without a detected language version, the defaults are Node 24, Ruby 3.3, and Python 3.13. OpenCode is enabled by default; the database defaults to none.

The normalized project name also becomes the Compose project name, producing resource names such as `<project>-app-1` and, when selected, `<project>-postgres-1`. Use `--name` to select a different prefix. Docker names are host-global, so separate checkouts that need to run simultaneously must use different project names.

## Upgrade a generated project

Run this from a project with an older `projectsetup`-generated `.devcontainer`, including a legacy Dockerfile-only setup:

```bash
projectsetup upgrade
```

The command regenerates from `.devcontainer/projectsetup.json`, preserving its preset, versions, tools, ports, packages, and database selection. It only replaces recognized projectsetup output containing known generated files; hand-written configurations and unsupported manifest schemas are refused.

## Validate a configuration

```bash
projectsetup check
projectsetup check --build
```

`check` validates the manifest, generated files and script modes, users and workspace paths, Compose service configuration, AI mounts, ports, language versions, lockfiles, and PostgreSQL consistency. When installed, it also runs `docker compose config` and `devcontainer read-configuration`.

`check --build` runs the static and external checks first, then executes:

```bash
devcontainer build --workspace-folder <project-root>
```

It builds the configuration but does not start the Dev Container or application.

Maintainers can run the opt-in preset integration checks with `PROJECTSETUP_BUILD_TESTS=1 go test ./internal/generate -run TestBuildPresetFixtures` and the first-run lifecycle smoke tests with `PROJECTSETUP_SMOKE_TESTS=1 go test ./internal/generate -run TestSmokePresetFixtures`.

## Doctor

```bash
projectsetup doctor
```

`doctor` reports Docker, the Docker daemon, Dev Container CLI, `dworm`, host architecture, SSH and GPG agent sockets, and the selected AI tools' persistence directories. Informational results go to standard output; warnings and errors go to standard error. Errors produce a non-zero exit status.

## Generated files

`init` writes:

```text
.devcontainer/
├── Dockerfile
├── compose.yaml
├── devcontainer.json
├── projectsetup.json
└── scripts/
    ├── install-ai-tools.sh
    └── post-create.sh
```

Every setup uses Compose with an `app` service and a project-scoped network. Selecting PostgreSQL adds a `postgres` service and named data volume. `projectsetup.json` is the generated configuration manifest and source of truth for validation. The scripts are executable.

If `.devcontainer` already exists, generation stops. `--force` replaces it only when `projectsetup.json` identifies it as generated by `projectsetup` and it contains only the known generated paths; unrelated files are never overwritten. Generation is staged and validated before installation.

## `dworm` workflow and limitations

Run `dworm` from the same project root where `projectsetup init` created the configuration:

```bash
dworm up
dworm exec -- <command>
dworm shell
```

Keep the original `dworm up` process alive: SSH and Git credential forwarding last only for that process's session. Later `dworm exec` calls do not automatically receive `SSH_AUTH_SOCK`, even though the forwarded socket exists at `/tmp/dworm-ssh-agent.sock`.

`dworm` dynamically scans and forwards listening TCP ports from 1024 through 20000 in the primary container. Ports outside that range trigger a `projectsetup check` warning. Sidecar ports are not scanned, and `forwardPorts` is Dev Container/editor metadata rather than input to `dworm`.

No application server is started automatically. Start the server yourself with `dworm exec -- ...` or from `dworm shell` while `dworm up` remains active.

## AI persistence

`init` creates the selected tools' host bind-mount source directories. OpenCode uses:

```text
~/.config/opencode
~/.local/share/opencode
~/.opencode
~/.cache/opencode
```

Claude Code additionally uses:

```text
~/.claude
~/.local/share/claude
```

These directories preserve configuration, credentials, installations, and caches across container rebuilds. Host `~/.ssh` and `~/.gitconfig` are deliberately not mounted because `dworm` supplies credential forwarding.

## PostgreSQL

`--database postgres` switches generation to Docker Compose with an `app` service and a healthy `postgres:17-bookworm` sidecar backed by the `postgres-data` named volume. The generated development credentials are `projectsetup`/`projectsetup`; `DB_HOST` and `PGHOST` are `postgres`, and the database name is the normalized project name.

The PostgreSQL port is not published to the host. `dworm` scans only the primary `app` container, and adding `forwardPorts` metadata would not expose the sidecar. Application database configuration is not rewritten automatically.
