# projectsetup

`projectsetup` is an opinionated Go CLI that generates deterministic Dev Container configurations for existing Node, Ruby, Rails, and Python projects, designed for use with `dworm`.

## Status and scope

The current implementation provides interactive and flag-driven `init`, static `check`, host diagnostics with `doctor`, and optional Dev Container build validation. It supports:

- Node with npm, pnpm, or Yarn
- Ruby with Bundler
- Rails
- Python with pip, Poetry, or uv
- Optional SQLite in the app container or PostgreSQL 17 sidecar
- OpenCode by default, optional Claude Code and Codex, and GitHub CLI

User templates, plugins, migration of hand-written configurations, databases other than SQLite and PostgreSQL, Alpine/musl images, Windows containers, and application generation are out of scope.

## Prerequisites

- Go 1.27.1 or newer when installing from source (prebuilt binaries do not require Go)
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

Show the running version and its build metadata with:

```bash
projectsetup --version
```

Release builds check GitHub for a newer release in the background while a command runs. If the check completes and an update is available, `projectsetup` suggests `projectsetup self-update` and prints the release URL on stderr. Checks are best-effort, quiet on failure, and skipped for development builds and the explicit self-update command.

## Update projectsetup

```bash
projectsetup self-update
```

This command updates the running application executable to the latest stable GitHub release for Linux or macOS (`amd64` or `arm64`). It works from any directory without Docker or a generated project. It makes a fresh release lookup, requires the archive's SHA-256 entry in `checksums.txt`, and verifies it before replacing the executable. Current or newer versions are left in place; prereleases are not selected.

Supported installations are the installer, manually extracted release archives, and versioned `go install` builds. Keep the actual executable named `projectsetup`. Symlinked commands update the resolved target and preserve the symlink; another executable on `PATH` is never selected. Unversioned local builds and dirty builds refuse self-replacement. Valid `go install` module version metadata is recognized even without release linker flags.

The executable's directory must be writable so the updater can stage and rename files. The command does not invoke `sudo`; if permissions prevent updating, fix the installation's ownership or reinstall into a user-writable directory such as `~/.local/bin`. The library creates the new executable with mode `0755` (subject to umask), attempts to restore the old executable if replacement fails, and reports rollback failures with a recovery path. Successful output includes the installed version and resolved destination. The next invocation uses the update; the command does not restart itself.

Discovery has a five-second deadline; downloads and checksum retrieval have a five-minute deadline. Ctrl-C cancels network operations. Missing or mismatched checksums fail without changing the existing executable.

**Older binaries, including v0.5.0, need the installer or manual installation once to gain this command.** `projectsetup upgrade` continues to regenerate a project's Dev Container configuration; use `self-update` to update the application itself.

## Initialize a project

Run commands from the project root. Interactive mode detects project files, asks only for unresolved choices, shows the normalized configuration, and requests confirmation before writing:

```bash
projectsetup init
```

For automation, add `--non-interactive`; unresolved or ambiguous required choices fail instead of prompting:

```bash
projectsetup init --non-interactive --preset node
projectsetup init --non-interactive --preset ruby --ruby-version 3.3
projectsetup init --non-interactive --preset python --python-version 3.13 --package-manager uv
projectsetup init --non-interactive --preset rails --database postgres --ai opencode,claude
projectsetup init --non-interactive --preset rails --database sqlite
projectsetup init --non-interactive --preset node --port 3000 --port 5173 --system-package imagemagick
```

Available `init` flags:

```text
--preset node|ruby|rails|python
--name NAME
--database none|postgres|sqlite
--ai TOOL[,TOOL...]          opencode, claude, codex; or none
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

Ruby projects are detected from a root-level `Gemfile`, `Gemfile.lock`, `.ruby-version`, or `*.gemspec`. The Ruby version comes from `.ruby-version` when present, then from a literal `ruby "VERSION"` declaration in the Gemfile. Rails-specific signals take precedence over generic Ruby detection. The Ruby preset installs the selected Ruby version and runs `bundle install` when a `Gemfile` exists; it does not add Node, Active Storage, Rails setup, or a default port.

Project names must match `^[a-z0-9][a-z0-9_-]*$`: lowercase letters, digits, `-`, and `_`, starting with a letter or digit. The one project name is used unchanged as the Compose project name, the `devcontainer.json` name, the manifest `projectName`, the workspace folder `/workspaces/<project>`, and the PostgreSQL database name. Compose resource names such as `<project>-app-1` and, when selected, `<project>-postgres-1` are derived from it.

Use `--name` to select a different prefix. An explicit `--name` is never rewritten. With `--non-interactive`, an invalid value fails with an error that names the value and the allowed pattern and suggests a normalized alternative when one exists (for example `--name a.b` suggests `a-b`). In interactive mode, `projectsetup` reports the invalid value and asks for a project name, offering the normalized alternative as the default. It asks again until the name is valid. Without `--name`, the name is derived from the directory name by lowercasing and replacing invalid characters with `-` (for example `My.App` becomes `my-app`). Interactive mode asks you to confirm or change a derived name when it had to be changed. If nothing usable remains, pass `--name`. Docker names are host-global, so separate checkouts that need to run simultaneously must use different project names.

Configurations generated by v0.6.0 and earlier with a dotted `--name` such as `a.b` recorded that name in the manifest, `devcontainer.json`, and workspace folder, but used `a-b` for Compose. `check` reports these manifests as invalid, and `upgrade` refuses them because it does not rename projects. To regenerate one, run `projectsetup init --force --name a-b` with the original options.

## Upgrade a generated project

Run this from a project with an older `projectsetup`-generated `.devcontainer`, including a legacy Dockerfile-only setup:

```bash
projectsetup upgrade
```

The command regenerates from `.devcontainer/projectsetup.json`, preserving its preset, versions, tools, ports, packages, and database selection. It only replaces recognized projectsetup output containing known generated files; hand-written configurations and unsupported manifest schemas are refused.

To change only the selected agents while upgrading:

```bash
projectsetup upgrade --ai opencode,codex
```

The list replaces the previous selection; use `--ai none` to disable all agents. Plain `upgrade` preserves the selection. Agent lists accept any combination and are deduplicated and sorted. Recreate the container after adding or removing mounts or changing its environment; regenerating files alone does not change an existing container. Upgrading configuration does not update working agent binaries.

## Validate a configuration

```bash
projectsetup check
projectsetup check --build
```

`check` validates the manifest, generated files and script modes, users and workspace paths, Compose service configuration, AI mounts, ports, language versions, lockfiles, SQLite packages, and PostgreSQL consistency. When installed, it also runs `docker compose config` and `devcontainer read-configuration`.

`check --build` runs the static and external checks first, then executes:

```bash
devcontainer build --workspace-folder <project-root>
```

It builds the configuration but does not start the Dev Container or application.

Maintainers can run the opt-in preset integration checks with `PROJECTSETUP_BUILD_TESTS=1 go test ./internal/generate -run TestBuildPresetFixtures` and the first-run lifecycle smoke tests with `PROJECTSETUP_SMOKE_TESTS=1 go test ./internal/generate -run TestSmokePresetFixtures`.

The Codex-specific smoke test uses isolated temporary directories to verify official installation, explicit update, custom state sharing, and host execution of the container-installed binary:

```bash
PROJECTSETUP_SMOKE_TESTS=1 go test ./internal/generate -run '^TestSmokeCodexSharedInstallation$' -v -timeout 12m
```

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

Every setup uses Compose with an `app` service and a project-scoped network. Selecting SQLite installs it directly in the app image; selecting PostgreSQL adds a `postgres` service and named data volume. `projectsetup.json` is the generated configuration manifest and source of truth for validation. The scripts are executable.

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

Codex uses two separate writable mounts:

| Host directory | Container directory | Purpose |
| --- | --- | --- |
| `CODEX_HOME`, default `~/.codex` | `/home/vscode/.codex` | Configuration, authorization, history, and session state |
| `~/.local/share/codex` | `/home/vscode/.local/share/codex` | Shared standalone installation |

Codex state is mounted through Compose, which supports the host `CODEX_HOME` fallback. Export a custom `CODEX_HOME` as an absolute path consistently when running `projectsetup` and `dworm up`. Compose's `.env` can also affect interpolation; keep it consistent with the exported host setting. Runtime `CODEX_HOME=/home/vscode/.codex` is set in `containerEnv` for direct `dworm exec` access.

The canonical installer installs Codex into the shared installation directory when absent. It keeps links relative so the same files work under different host and container home paths. An existing incompatible or broken Codex binary produces an actionable error instead of being overwritten. Sharing executables requires compatible Linux/CPU architectures.

To use this same installation on the host after the first container setup, put its command first in your host PATH:

```bash
export PATH="$HOME/.local/share/codex/bin:$PATH"
codex --version
```

An existing npm, Homebrew, or default standalone host installation is not moved or replaced automatically. Selecting the shared command on PATH makes subsequent host and container invocations use the same installation.

For shared authorization, set this top-level value in the host's `CODEX_HOME/config.toml` (default `~/.codex/config.toml`), then run `codex login` on the host if no file-based login exists:

```toml
cli_auth_credentials_store = "file"
```

Host OS keyring credentials cannot be shared by a directory mount. `doctor` reports the file-based cache's presence and warns about keyring/auto configuration; it does not inspect token contents or alter host configuration. File-based credentials and history remain on the host and are writable from the container. [Codex authentication documentation](https://learn.chatgpt.com/docs/auth)

Sharing the full state directory preserves local history and sessions when persistence is enabled. Host-specific paths in configuration, plugins, and prior sessions may still need adjustment. When the resume picker filters out sessions from a different workspace path, use `codex resume --all`. Avoid resuming the same session simultaneously in two processes. [Codex configuration documentation](https://learn.chatgpt.com/docs/config-file/config-advanced), [session commands](https://learn.chatgpt.com/docs/developer-commands?surface=cli#codex-resume)

These directories preserve configuration, credentials, installations, and caches across container rebuilds. Host `~/.ssh` and `~/.gitconfig` are deliberately not mounted because `dworm` supplies credential forwarding.

## Update agents in an existing container

After `projectsetup upgrade` has refreshed the generated scripts, run from the project root:

```bash
dworm exec -- .devcontainer/scripts/install-ai-tools.sh --update opencode
dworm exec -- .devcontainer/scripts/install-ai-tools.sh --update codex
dworm exec -- .devcontainer/scripts/install-ai-tools.sh --update opencode codex
```

Use only agents selected for that container, so their installation and state mounts are available. The shared installer also accepts `--update claude`. It reports versions and validates the resulting executable, propagating download or update failures. Normal post-create setup installs missing tools and leaves working versions alone.

For an older container without the updated script, OpenCode can already update itself:

```bash
dworm exec -- opencode upgrade --method curl
```

Updates persist in shared host installation directories and affect every container using them; restart running agent processes to use the updated version. No container rebuild is needed for a binary update. For Codex, use the generated update script on the Linux host or in the container so installation placement and relative links are preserved.

## PostgreSQL

`--database postgres` switches generation to Docker Compose with an `app` service and a healthy `postgres:17-bookworm` sidecar backed by the `postgres-data` named volume. The generated development credentials are `projectsetup`/`projectsetup`; `DB_HOST` and `PGHOST` are `postgres`, and the database name is the normalized project name.

The PostgreSQL port is not published to the host. `dworm` scans only the primary `app` container, and adding `forwardPorts` metadata would not expose the sidecar. Application database configuration is not rewritten automatically.

## SQLite

`--database sqlite` installs the `sqlite3` command and `libsqlite3-dev` development files in the primary app image. SQLite does not add a Compose service, dependency, volume, or database environment variables; the application stores its database in the workspace according to its own configuration.
