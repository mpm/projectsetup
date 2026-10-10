# projectsetup

`projectsetup` is an opinionated Go CLI that generates deterministic Dev Container configurations for existing Node, Ruby, Rails, and Python projects, designed for use with `dworm`.

## Status and scope

The current implementation provides interactive and flag-driven `init`, static `check`, host diagnostics with `doctor`, optional Dev Container build validation, and management of preset and add-on definitions. The built-in definitions support:

- Presets: Node with npm, pnpm, or Yarn; Ruby with Bundler; Rails; Python with pip, Poetry, or uv
- Add-ons: SQLite in the app container, a PostgreSQL sidecar (18 by default), a Redis sidecar (8 by default), and Go (1.27 by default) and Rust (1.99 by default) toolchains in the app image
- OpenCode by default, optional Claude Code and Codex, and GitHub CLI

Presets and add-ons are TOML definition files. You can copy and edit a built-in, write your own, or install definitions from a URL; see [Presets and add-ons](#presets-and-add-ons). Definitions use a constrained schema and cannot change the parts `dworm` relies on.

Arbitrary Compose or `devcontainer.json` fragments, migration of hand-written configurations, Alpine/musl images, Windows containers, and application generation are out of scope.

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
projectsetup init --non-interactive --preset ruby --set version=4.0
projectsetup init --non-interactive --preset python --set version=3.14 --set package_manager=uv
projectsetup init --non-interactive --preset rails --addon postgres --ai opencode,claude
projectsetup init --non-interactive --preset ruby --addon go --addon redis --set go.version=1.26
projectsetup init --non-interactive --preset node --port 3000 --port 5173 --system-package imagemagick
```

Available `init` flags:

```text
--preset NAME                preset definition (projectsetup preset list)
--addon NAME                 add-on definition (repeatable)
--set [DEF.]OPTION=VALUE     option value (repeatable); DEF defaults to the preset
--name NAME
--ai TOOL[,TOOL...]          opencode, claude, codex; or none
--port PORT                  repeatable
--system-package PACKAGE     repeatable apt package
--non-interactive
--force
--list-options               print accepted values and exit
--json                       with --list-options, print JSON
```

These older flags remain as aliases:

```text
--node-version VERSION       --set node.version=VERSION
--ruby-version VERSION       --set ruby.version=VERSION (or rails.version)
--python-version VERSION     --set python.version=VERSION
--package-manager NAME       --set package_manager=NAME
--database none|postgres|sqlite    --addon postgres, --addon sqlite, or neither
--postgres-version MAJOR     --set postgres.version=MAJOR
```

`--set` can only name the preset and selected add-ons. `projectsetup init --list-options` lists every option with its default and accepted values. Option values are detected from version files, manifests, and lockfiles. Without a detected language version, the defaults are Node 26, Ruby 4.0, and Python 3.14. OpenCode is enabled by default, and no add-on is selected by default. Interactive mode offers detected add-on suggestions, such as PostgreSQL for a Rails project whose `config/database.yml` uses the PostgreSQL adapter, as the default.

Ruby projects are detected from a root-level `Gemfile`, `Gemfile.lock`, `.ruby-version`, or `*.gemspec`. The Ruby version comes from `.ruby-version` when present, then from a literal `ruby "VERSION"` declaration in the Gemfile. Rails-specific signals take precedence over generic Ruby detection. The Ruby preset installs the selected Ruby version and runs `bundle install` when a `Gemfile` exists; it does not add Node, Active Storage, Rails setup, or a default port.

Project names must match `^[a-z0-9][a-z0-9_-]*$`: lowercase letters, digits, `-`, and `_`, starting with a letter or digit. The one project name is used unchanged as the Compose project name, the `devcontainer.json` name, the manifest `projectName`, the workspace folder `/workspaces/<project>`, and the PostgreSQL database name. Compose resource names such as `<project>-app-1` and, when selected, `<project>-postgres-1` are derived from it.

Use `--name` to select a different prefix. An explicit `--name` is never rewritten. With `--non-interactive`, an invalid value fails with an error that names the value and the allowed pattern and suggests a normalized alternative when one exists (for example `--name a.b` suggests `a-b`). In interactive mode, `projectsetup` reports the invalid value and asks for a project name, offering the normalized alternative as the default. It asks again until the name is valid. Without `--name`, the name is derived from the directory name by lowercasing and replacing invalid characters with `-` (for example `My.App` becomes `my-app`). Interactive mode asks you to confirm or change a derived name when it had to be changed. If nothing usable remains, pass `--name`. Docker names are host-global, so separate checkouts that need to run simultaneously must use different project names.

Configurations generated by v0.6.0 and earlier with a dotted `--name` such as `a.b` recorded that name in the manifest, `devcontainer.json`, and workspace folder, but used `a-b` for Compose. `check` reports these manifests as invalid, and `upgrade` refuses them because it does not rename projects. To regenerate one, run `projectsetup init --force --name a-b` with the original options.

### Listing accepted values

Tools that drive `init --non-interactive`, such as project wizards, can read the accepted values from the installed version instead of hard-coding them:

```bash
projectsetup init --list-options --json
projectsetup init --list-options
```

The listing is built from the same registry of built-in and user definitions that validates `init` flags. It does not read or write the project directory, run detection, or prompt. Without `--json`, it prints a short human-readable summary. `--list-options` may be combined only with `--json` and `--non-interactive`; any other `init` flag is rejected, and `--json` without `--list-options` is an error.

The JSON output is indented and ends with a newline. Shortened to one preset and one add-on, it has this shape:

```json
{
  "schemaVersion": 2,
  "presets": ["node", "python", "rails", "ruby"],
  "addons": ["go", "postgres", "redis", "rust", "sqlite"],
  "definitions": {
    "node": {
      "name": "node",
      "kind": "preset",
      "version": "1.0.0",
      "source": "builtin",
      "description": "Node.js with npm, pnpm, or Yarn",
      "options": {
        "package_manager": {"description": "Node.js package manager", "default": "npm", "choices": ["npm", "pnpm", "yarn"]},
        "version": {"description": "Node.js version", "default": "26", "pattern": "[0-9]+(\\.[0-9]+){0,2}([-+][a-zA-Z0-9.-]+)?"}
      }
    },
    "postgres": {
      "name": "postgres",
      "kind": "addon",
      "version": "1.0.0",
      "source": "builtin",
      "description": "PostgreSQL sidecar with development-only credentials",
      "options": {
        "version": {"description": "PostgreSQL major version", "default": "18", "pattern": "[1-9][0-9]*"}
      }
    }
  },
  "aiTools": ["opencode", "claude", "codex"],
  "defaultAITools": ["opencode"],
  "projectNamePattern": "^[a-z0-9][a-z0-9_-]*$"
}
```

- `schemaVersion` identifies this listing format, independent of the manifest schema. New fields may be added without changing it. Version 2 replaced the `packageManagers`, `databases`, and `defaults` fields of version 1 with `addons`, `definitions`, and `defaultAITools`.
- `presets` and `addons` are sorted by name; `aiTools` is in display order. Object keys are sorted.
- `definitions` has an entry for every preset and add-on, including user definitions. `source` is `builtin`, `user`, or the URL of an installed remote definition.
- Each option has a `default` and either `choices` or a `pattern`. A pattern must match the whole value. Pass a value with `--set DEFINITION.OPTION=VALUE`. A definition without options has an empty `options` object.
- Detected version files and lockfiles take precedence over option defaults.
- `aiTools` values may be combined in a comma-separated `--ai` list; pass `--ai none` to select no tools. `defaultAITools` is the selection used when `--ai` is omitted.
- `projectNamePattern` is the regular expression an explicit `--name` must match.

## Upgrade a generated project

Run this from a project with an older `projectsetup`-generated `.devcontainer`, including a legacy Dockerfile-only setup:

```bash
projectsetup upgrade
```

The command regenerates from `.devcontainer/projectsetup.json`, preserving its preset, add-ons, option values, tools, ports, and packages. It only replaces recognized projectsetup output containing known generated files; hand-written configurations and unsupported manifest schemas are refused. This includes `devcontainer-lock.json`, which the Dev Container CLI writes when `dworm up` starts the container; delete it before upgrading.

`upgrade` uses the definition copies in `.devcontainer/presets/`, so the result does not depend on the definitions installed on this machine. To pick up newer versions of the built-in, user, or installed remote definitions, add `--refresh-presets`. It keeps the recorded option values and fills in defaults only for options the newer definitions add:

```bash
projectsetup upgrade --refresh-presets
```

Manifests written by v0.8.0 and earlier use schema 1. `upgrade` maps them to the built-in definitions, which produce the same setup: `database` becomes the `postgres` or `sqlite` add-on, `languageVersion` and `packageManager` become preset options, and a missing PostgreSQL version means 17. The upgraded manifest uses schema 2, and the definition copies are added to `.devcontainer/presets/`.

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

`check` validates the manifest, generated files and script modes, users and workspace paths, Compose service configuration, AI mounts, ports, option values against detected version files and lockfiles, and the features, environment, PATH entries, apt packages, and sidecar services that the selected definitions contribute. It reads the definitions from `.devcontainer/presets/` and fails when a copy does not match the hash recorded in the manifest. When installed, it also runs `docker compose config` and `devcontainer read-configuration`. Configurations with schema 1 manifests from earlier releases still pass.

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
├── presets/
│   ├── node.toml
│   └── postgres.toml
└── scripts/
    ├── install-ai-tools.sh
    └── post-create.sh
```

Every setup uses Compose with an `app` service and a project-scoped network. Add-ons with sidecars, such as PostgreSQL and Redis, add their services and named data volumes. `presets/` holds a copy of each selected definition. The scripts are executable.

`projectsetup.json` is the generated configuration manifest and source of truth for validation. It uses schema 2:

```json
{
  "schemaVersion": 2,
  "projectName": "example",
  "preset": {"name": "node", "version": "1.0.0", "source": "builtin", "sha256": "..."},
  "addons": [{"name": "postgres", "version": "1.0.0", "source": "builtin", "sha256": "..."}],
  "options": {"node": {"package_manager": "npm", "version": "26"}, "postgres": {"version": "18"}},
  "aiTools": ["opencode"],
  "ports": [],
  "systemPackages": [],
  "generatedBy": "projectsetup"
}
```

`options` records every option of every selected definition, including defaults; definitions without options have no entry.

If `.devcontainer` already exists, generation stops. `--force` replaces it only when `projectsetup.json` identifies it as generated by `projectsetup` and it contains only the known generated paths; unrelated files are never overwritten. `--force` keeps the recorded option values of add-ons that remain selected unless you set them explicitly, so a PostgreSQL data volume stays readable. Generation is staged and validated before installation.

## Presets and add-ons

A project uses exactly one preset and any number of add-ons. Presets set the base image and usually install the language runtime, package managers, and dependency setup. Add-ons add tools, packages, environment, setup steps, or sidecar services. Both are TOML definition files, from three sources:

| Source | Location | How it gets there |
| --- | --- | --- |
| `builtin` | embedded in the executable | ships with `projectsetup` |
| `user` | `~/.config/projectsetup/presets/NAME.toml` | written by you or by `preset eject` |
| a URL | `~/.config/projectsetup/presets/NAME.toml`, pinned in `~/.config/projectsetup/sources.toml` | installed by `preset add` |

On macOS, the configuration directory is `~/Library/Application Support/projectsetup`. Set `PROJECTSETUP_CONFIG_DIR` to use another directory; definitions are then read from `$PROJECTSETUP_CONFIG_DIR/presets`. Only files named `NAME.toml` are read, and a user definition cannot reuse a built-in name.

```bash
projectsetup preset list [--json]          # every definition with kind, version, and source
projectsetup preset show NAME              # print a definition file
projectsetup preset validate FILE          # check a definition before using it
projectsetup preset eject NAME --as NEW    # copy a definition into the user directory as NEW
projectsetup preset add [--yes] URL|github:owner/repo[/path][@ref]
projectsetup preset update [--yes] [NAME]
projectsetup preset remove NAME
```

Every generated project gets a copy of the exact definition files it was generated from in `.devcontainer/presets/`. The manifest records each definition's name, version, source, and SHA-256 hash. `check` and `upgrade` use these copies, so a project keeps working on a machine that does not have its user or remote definitions, and changing a definition does not affect existing projects until you run `projectsetup upgrade --refresh-presets` there. Do not edit the copies: `check` reports a copy whose hash differs from the manifest.

### Writing a definition

The easiest start is a copy of a similar built-in:

```bash
projectsetup preset eject go --as go-tip
$EDITOR ~/.config/projectsetup/presets/go-tip.toml
projectsetup preset validate ~/.config/projectsetup/presets/go-tip.toml
projectsetup init --addon go-tip
```

`eject` changes only the `name` line, so variant conditions and `supersedes` in the copy still name the original definitions. This add-on installs a Java JDK and optionally Maven or Gradle:

```toml
#:schema https://raw.githubusercontent.com/mpm/projectsetup/main/schema/preset.schema.json
schema = 1
kind = "addon"
name = "java"
version = "1.0.0"
description = "Java JDK with optional Maven or Gradle"

[options.version]
description = "Java major version"
default = "21"
pattern = '[1-9][0-9]*'

[options.build_tool]
description = "Java build tool"
default = "none"
choices = ["none", "maven", "gradle"]

[features."ghcr.io/devcontainers/features/java:1"]
version = "${option:version}"

# The generated containerEnv.PATH replaces the feature's PATH changes.
[container]
path = ["/usr/local/sdkman/bin", "/usr/local/sdkman/candidates/java/current/bin"]

[[variant]]
when = { option = { build_tool = { in = ["maven"] } } }

[variant.features."ghcr.io/devcontainers/features/java:1"]
installMaven = true

[variant.container]
path = ["/usr/local/sdkman/candidates/maven/current/bin"]

[[variant]]
when = { option = { build_tool = { in = ["gradle"] } } }

[variant.features."ghcr.io/devcontainers/features/java:1"]
installGradle = true

[variant.container]
path = ["/usr/local/sdkman/candidates/gradle/current/bin"]
```

Save it as `java.toml` in the user definition directory, then select it with `projectsetup init --addon java --set java.build_tool=maven`. The `#:schema` comment enables completion and inline validation in editors with TOML schema support, such as VS Code with Even Better TOML. [`schema/preset.schema.json`](schema/preset.schema.json) describes every field. `preset validate` checks the rules the schema cannot express and resolves the definition with its default options. It reports every problem with the file name.

Fields:

| Field | Purpose |
| --- | --- |
| `schema`, `kind`, `name`, `version`, `description` | Required. `schema = 1`; `kind` is `preset` or `addon`; `name` matches `^[a-z][a-z0-9-]*$`; `version` is `MAJOR.MINOR.PATCH`. |
| `[options.NAME]` | A value users set with `--set DEFINITION.NAME=VALUE`. Requires `description`, `default`, and exactly one of `choices` or `pattern`. A pattern must match the whole value. |
| `[image]` | `base` (presets only, and required for them), `apt` packages, `root_run` steps run as root, and `user_run` steps run as `vscode` after the final `USER vscode`. Each step is one line. |
| `[features."ID"]` | A Dev Container feature with its options. Values are strings, booleans, or numbers. |
| `[container]` | `path` entries added to `containerEnv.PATH` after the core entries, and `env` values for `containerEnv`. |
| `[setup]` | A bash `script` appended to `post-create.sh` after AI tool setup. |
| `[services.NAME]` | A Compose sidecar with `image`, `restart`, `environment`, `healthcheck`, named `volumes` (volume name to container path), and `app_depends_on` (`service_started` or `service_healthy`). |
| `[[variant]]` | An additive block with `image` (without `base`), `features`, `container`, `setup`, or `services`, applied when every condition in `when` matches: `preset`, `not_preset`, `addon`, and `option` with `in = [...]` or `below = N` (compares the value's leading integer). |
| `[detect]` | Presets only. `signals` are files reported as evidence; `match` rules detect the preset (when empty, any signal does); `supersedes` drops other detected presets; `[[detect.suggest]]` offers an add-on; `[[detect.warning]]` prints a message. |
| `[options.NAME.detect]` | Presets only. `sources` read a value from a file (its first word, or the first capture group of `pattern`) or from the `package-json-engines` built-in; the first source with a value wins. `choices` map each choice to the files that indicate it, such as lockfiles. |

Rules:

- Text can use `${option:NAME}` for the definition's own options and `${project:name}`, `${project:home}`, and `${project:workspace}`. Any other `${...}`, including shell variables, is left as written.
- Option values, including defaults and detected values, must also match `^[A-Za-z0-9][A-Za-z0-9._+-]*$`, so they are safe in shell scripts, Dockerfiles, and YAML.
- Detection file paths are relative to the project root. Globs cannot include a directory, and directories never match.
- Contributions are applied in this order: the preset, then add-ons sorted by name. Each definition's top-level block comes first, followed by its matching variants in file order. A variant can override its own definition's feature options, `env` values, and service fields, but two definitions cannot set the same feature, environment variable, or service.
- Feature PATH changes made through the feature's own `containerEnv` are replaced by the generated `containerEnv.PATH`, which `dworm exec` uses. List the directories the feature adds in `container.path`. The feature's other environment variables are kept.

Definitions cannot change the parts that `dworm` and the AI tools rely on: the `vscode` user and `/home/vscode`, the `app` service with its build, command, and workspace mount, the AI tool installer, mounts, and environment, the GitHub CLI feature, and the core PATH entries. `PATH`, `HOME`, `USER`, `CODEX_HOME`, and `CLAUDE_CONFIG_DIR` cannot be set, a sidecar cannot be named `app`, and there is no field for host mounts; sidecar volumes are named volumes.

### Remote definitions

Install definitions from an HTTPS URL or a GitHub repository:

```bash
projectsetup preset add https://example.com/presets/java.toml
projectsetup preset add github:owner/repo/presets/java.toml@v1.2.0
projectsetup preset add github:owner/repo@v1.2.0
```

`github:owner/repo[/path][@ref]` reads from `https://raw.githubusercontent.com/owner/repo/REF/path`; the ref defaults to `HEAD`. A path that does not end in `.toml`, or no path, names a directory containing `index.toml`. An index installs several definitions:

```toml
schema = 1
definitions = ["java.toml", "addons/kotlin.toml"]
```

Entries are relative `.toml` paths resolved against the index URL; an index lists at most 64. Each file is limited to 64 KiB, redirects must stay on HTTPS, and URLs cannot contain credentials. Every definition is validated before anything is installed, and `add` refuses names that already exist. `sources.toml` records each definition's URL, ref, SHA-256 hash, and fetch time.

`preset update [NAME]` fetches the recorded URLs again and shows a diff for each changed definition. It installs nothing if any fetch fails, refuses a definition whose `name` changed, and repairs a missing or modified installed file. `preset remove NAME` deletes a remote definition and its record. Both `add` and `update` ask for confirmation unless `--yes` is passed. Remove user definitions you wrote yourself by deleting their file.

`init`, `check`, and `upgrade` never use the network. Installed remote definitions are loaded like user definitions, but loading fails when a file no longer matches its recorded hash.

**Security:** a definition is code. Its `setup.script`, `root_run`, and `user_run` steps and the features it installs run in a container that mounts your AI tool credentials and history from the host and receives your forwarded SSH agent while `dworm up` runs. Install only definitions you trust, read the content or diff that `add` and `update` print before confirming, and prefer a `github:` location pinned to a tag or commit. Definitions are never updated automatically, and an updated definition affects a project only after `projectsetup upgrade --refresh-presets`.

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

`--addon postgres` (or `--database postgres`) adds a healthy PostgreSQL sidecar backed by the `postgres-data` named volume. New projects use PostgreSQL 18 (`postgres:18-trixie`, volume mounted at `/var/lib/postgresql`); pass `--set postgres.version=MAJOR` (or `--postgres-version MAJOR`) to choose another major version. Versions before 18 use the `-bookworm` image with the volume at `/var/lib/postgresql/data`. The generated development credentials are `projectsetup`/`projectsetup`; `DB_HOST` and `PGHOST` are `postgres`, and the database name is the normalized project name.

The major version is recorded in the manifest `options`, because a data volume can only be opened by the major version that created it. `upgrade` and `init --force` keep the recorded version; schema 1 manifests from v0.7.0 and earlier have no `postgresVersion` and are treated as PostgreSQL 17, which those releases generated. To move an existing project to a newer major version, dump the database, regenerate with `projectsetup init --force --set postgres.version=18` and the original options, remove the old `postgres-data` volume, and restore the dump.

The PostgreSQL port is not published to the host. `dworm` scans only the primary `app` container, and adding `forwardPorts` metadata would not expose the sidecar. Application database configuration is not rewritten automatically.

## SQLite

`--addon sqlite` (or `--database sqlite`) installs the `sqlite3` command and `libsqlite3-dev` development files in the primary app image. SQLite does not add a Compose service, dependency, volume, or database environment variables; the application stores its database in the workspace according to its own configuration.

## Redis

`--addon redis` adds a `redis` sidecar (`redis:8-trixie`) with a `redis-cli ping` health check and its data in the `redis-data` named volume at `/data`. The app container waits for the sidecar to become healthy, gets `redis-cli` from the `redis-tools` package, and has `REDIS_URL=redis://redis:6379`. Pass `--set redis.version=VERSION` to choose another version; the image is `redis:VERSION-trixie`. Like PostgreSQL, the port is not published to the host.

## Go and Rust

`--addon go` installs Go with the official Dev Container feature, 1.27 by default; pass `--set go.version=VERSION` to choose another. `/usr/local/go/bin` and `/go/bin`, where `go install` puts binaries, are on PATH.

`--addon rust` installs Rust with rustup and Cargo through the official feature, 1.99 by default; pass `--set rust.version=VERSION` to choose another. `/usr/local/cargo/bin` is on PATH.

Both directories are in `containerEnv`, so `dworm exec -- go ...` and `dworm exec -- cargo ...` work without a login shell. The add-ons combine with any preset and with each other.
