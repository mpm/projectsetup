# Projectsetup Implementation Handoff

## Planned Extensions

See [Shared Toolchain Images: Implementation Roadmap](SHARED_IMAGES_ROADMAP.md) for the planned shared-image consumption, UID/GID and ownership handling, customizable image-family recipes, optional build/export workflow, and exact PostgreSQL reference support. That roadmap describes future work; the contract below documents existing behavior.

## Purpose

`projectsetup` is an opinionated command-line tool that creates reliable Dev Container setups for local software projects. It targets the author's workflow:

- Development happens inside a Dev Container.
- `dworm` starts and accesses the container while forwarding SSH, Git credentials, GPG, and listening TCP ports.
- OpenCode and optionally Claude Code and Codex are installed inside the container.
- AI tool configuration, credentials, installations, and caches persist through bind mounts from the host.
- Every project uses Docker Compose so the primary container consistently runs on a user-defined network; PostgreSQL is an optional sidecar.

The tool must produce deterministic, validated files. It is a constrained configuration compiler, not an AI generator and not a general-purpose templating framework.

## MVP Scope

The first version supports:

- Node projects
- Ruby projects, including gems
- Ruby on Rails projects
- Python projects
- Optional SQLite in the primary container or PostgreSQL sidecar
- OpenCode, enabled by default
- Optional Claude Code and Codex
- GitHub CLI
- Project detection and interactive initialization
- Non-interactive initialization through flags
- Static validation with `projectsetup check`
- Host and dependency diagnostics with `projectsetup doctor`
- Optional container build validation with `projectsetup check --build`

Explicitly out of scope for the first version:

- Arbitrary YAML or JSON fragments; presets use the constrained schema in [Preset System](#preset-system)
- Automatic migration of hand-written Dev Containers
- Automatic migrations of previously generated configurations; the explicit `upgrade` command reads supported older manifest schemas and writes the current one
- Databases other than SQLite and PostgreSQL; the Redis add-on from the preset system roadmap is the one additional sidecar
- Alpine or other musl-based images
- Windows containers
- Full application generators such as `rails new`

## Technology

Implement the CLI in Go.

Reasons:

- Produces a dependency-free executable that can be installed next to `dworm`.
- Strongly typed configuration makes invalid combinations harder to represent.
- `embed` can package shell and Dockerfile templates in the binary.
- The standard library handles JSON, templates, process execution, and filesystem operations.

Prefer the Go standard library. A small CLI or prompting dependency is acceptable if it materially simplifies the implementation, but avoid adopting a large framework by default. Keep business logic independent of terminal prompting so it is straightforward to test.

## CLI Contract

The executable is named `projectsetup`.

### Initialize

Interactive mode:

```bash
projectsetup init
```

Non-interactive examples:

```bash
projectsetup init --preset node
projectsetup init --preset ruby --ruby-version 4.0
projectsetup init --preset python --python-version 3.14
projectsetup init --preset rails --database postgres --ai opencode,claude
```

Initial flags:

```text
--preset NAME                preset definition (projectsetup preset list)
--addon NAME                 repeatable add-on definition
--set [DEF.]OPTION=VALUE     repeatable; DEF defaults to the preset
--name NAME
--database none|postgres|sqlite    alias for --addon
--postgres-version MAJOR
--ai TOOL[,TOOL...]          opencode, claude, codex; or none
--node-version VERSION
--ruby-version VERSION
--python-version VERSION
--package-manager npm|pnpm|yarn|pip|poetry|uv
--port PORT                  repeatable
--system-package PACKAGE     repeatable
--non-interactive
--force
--list-options               print accepted values and exit
--json                       with --list-options, print JSON
```

`init --list-options [--json]` lists the presets, add-ons, every definition's options with defaults and choices or patterns, AI tools, and the project-name pattern, built from the same registry (built-in and user definitions) that normalization validates against. Option patterns must match the whole value. It does not detect, prompt, or touch the project directory, and rejects generation flags. The JSON format is `OptionsSchemaVersion` 2.

Behavior:

- Operate on the current directory in v1. A target directory flag can be added later.
- Detect defaults from project files before prompting.
- Ask only about missing or ambiguous values. Interactive prompts run in this order: preset, preset options in name order (skipping single detected values), add-ons (detected suggestions as the default), AI tools, confirmation.
- In `--non-interactive` mode, fail with an actionable error when a required value cannot be inferred.
- Show the normalized configuration before writing in interactive mode and request confirmation.
- Refuse to overwrite an existing `.devcontainer` directory unless it is recognized as generated by `projectsetup` and the user passed `--force`.
- Never overwrite unrelated files.
- Create required host mount source directories before reporting success.
- Run the same static validation as `projectsetup check` after rendering.
- If validation fails, report the generated file and reason. Prefer rendering into a temporary directory and moving files into place only after internal validation passes.

### Upgrade

```bash
projectsetup upgrade
```

Behavior:

- Read `.devcontainer/projectsetup.json` and regenerate from that source of truth using the current templates.
- Preserve all normalized manifest values rather than re-running detection or applying new defaults. An explicit `upgrade --ai TOOL[,TOOL...]` replaces only the agent selection (or `none`); `--refresh-presets` resolves the recorded names from the local registry instead of the project copies, keeping option values and filling defaults only for options the new definitions add. Validate the original manifest before applying overrides.
- Upgrade only configurations whose supported manifest identifies them as generated by `projectsetup` and whose directory contains only known generated paths. `devcontainer-lock.json`, which the Dev Container CLI writes on start, counts as generated and is dropped by the replacement.
- Refuse hand-written, unrecognized, or unsupported-schema configurations.
- Use the same staged validation and atomic replacement behavior as `init --force`.

### Check

```bash
projectsetup check
projectsetup check --build
```

Static checks:

- The manifest parses and uses a supported schema version.
- Generated files exist.
- `devcontainer.json` parses as JSON or JSONC, depending on the chosen output format.
- Docker Compose configuration is valid.
- The configured service exists in Compose.
- Workspace source, target, and `workspaceFolder` agree.
- `containerUser`, `remoteUser`, image user, and home paths agree.
- Required host bind-mount source directories exist.
- Post-create scripts exist and are executable.
- No `~/.ssh` or `~/.gitconfig` mounts conflict with `dworm`.
- Ports are valid and warn when outside `dworm`'s scanned range of 1024 through 20000.
- Selected language versions agree with project version files where present.
- The selected package manager agrees with detected lockfiles.
- PostgreSQL environment and service names agree.

Use installed tools for additional validation when available:

```bash
devcontainer read-configuration --workspace-folder .
docker compose -f .devcontainer/compose.yaml config
```

Missing external validation tools should produce a warning during normal static checks rather than preventing checks implemented internally.

`--build` should run a real Dev Container build/up validation using the `devcontainer` CLI. It may leave the environment running only if that behavior is clearly documented; preferably clean up only resources created by the check. Never remove a pre-existing user container.

### Doctor

```bash
projectsetup doctor
```

Report:

- `docker` location and version
- Docker daemon availability
- `devcontainer` location and version
- `dworm` location and version
- Host architecture and OS
- `SSH_AUTH_SOCK` presence and socket validity
- GPG agent socket discovery
- AI tool host directory presence and writability
- Whether the environment satisfies known `dworm` constraints

Doctor is diagnostic and should clearly distinguish errors from warnings.

## Source Configuration

Write the normalized source configuration to:

```text
.devcontainer/projectsetup.json
```

Schema 1, written before the preset system; current releases write [manifest schema 2](#manifest-schema-2):

```json
{
  "schemaVersion": 1,
  "projectName": "example",
  "preset": "python",
  "database": "postgres",
  "aiTools": ["opencode", "claude"],
  "packageManager": "uv",
  "languageVersion": "3.13",
  "ports": [8000],
  "systemPackages": [],
  "generatedBy": "projectsetup"
}
```

This manifest is the source of truth for checks and eventual regeneration. Do not reverse-engineer configuration from rendered templates when the manifest is available.

Use one `languageVersion` field in the persisted schema unless separate version fields prove simpler during implementation. In Go, a typed normalized model should still make the selected preset explicit.

Generated files should include a short generated-file marker where the format permits comments. The manifest's `generatedBy` field identifies the directory when comments are unavailable.

## Internal Model

A possible normalized model:

```go
type Config struct {
	SchemaVersion  int
	ProjectName   string
	Preset         Preset
	Database       Database
	AITools        []AITool
	PackageManager PackageManager
	LanguageVersion string
	Ports          []int
	SystemPackages []string
	Workspace      Workspace
	Container      Container
}

type Workspace struct {
	HostPath      string
	ContainerPath string
}

type Container struct {
	User               string
	Home               string
	ServiceName        string
	ComposeProjectName string
}
```

Normalize all inferred, prompted, and flag-provided values into this model before rendering. Templates must not independently derive project names, workspace paths, users, services, or home directories.

Represent enums as validated Go types. Reject unsupported values before filesystem changes.

## Project Detection

Detection supplies defaults; it must not silently resolve ambiguity.

### Node

Signals:

- `package.json`
- `.node-version`
- `.nvmrc`
- `package-lock.json`
- `pnpm-lock.yaml`
- `yarn.lock`

Rules:

- Infer Node when `package.json` exists. Report it alongside strong Ruby, Rails, or Python signals so ambiguity can be resolved explicitly.
- Prefer `.node-version`, then `.nvmrc`, then `package.json` engines for the version.
- Infer `npm`, `pnpm`, or `yarn` from a single lockfile.
- Multiple lockfiles are ambiguous and require a prompt or explicit flag.
- Default to a documented current Node version only when no project version is available.

### Ruby

Signals:

- `Gemfile`
- `Gemfile.lock`
- `.ruby-version`
- A root-level `*.gemspec`

Rules:

- Read `.ruby-version` when available.
- Treat Bundler as part of the Ruby runtime rather than a selectable package manager.
- Prefer Rails over generic Ruby when Rails-specific evidence exists, so Rails projects are not reported as ambiguous Ruby and Rails roots.
- The post-create dependency setup should run `bundle install` only when `Gemfile` exists.

### Rails

Signals:

- `Gemfile` containing Rails
- `bin/rails`
- `config/application.rb`
- `.ruby-version`
- `Gemfile.lock`

Rules:

- Infer Rails only from Rails-specific signals, not every Ruby project.
- Read `.ruby-version` when available.
- PostgreSQL may be suggested when `config/database.yml` uses the PostgreSQL adapter, but do not silently add it if detection is uncertain.
- The post-create application setup should run `bin/setup --skip-server` only when executable `bin/setup` exists.

### Python

Signals:

- `pyproject.toml`
- `requirements.txt`
- `requirements-dev.txt`
- `poetry.lock`
- `uv.lock`
- `Pipfile`
- `.python-version`

Rules:

- Prefer `.python-version`, then `pyproject.toml`'s `requires-python` when it identifies a usable version.
- Infer `uv` from `uv.lock`, Poetry from `poetry.lock` or Poetry metadata, and `pip` from requirements files.
- A `Pipfile` should be reported as detected but Pipenv support is out of scope unless explicitly added; prompt the user to select a supported manager.
- Multiple incompatible lockfiles are ambiguous.
- Do not assume a web framework. Suggested ports should be conservative: no port unless a recognized framework signal provides a strong default or the user selects one.
- PostgreSQL remains an independent capability.

### Mixed Repositories

If strong signals for multiple presets exist at the same root, interactive mode asks the user to choose. Non-interactive mode requires `--preset`. Rails is the one specificity exception: Rails-specific evidence suppresses generic Ruby detection at that root.

The MVP operates on one project root and does not attempt monorepo workspace selection.

## Generated Layout

All setups:

```text
.devcontainer/
├── devcontainer.json
├── Dockerfile
├── compose.yaml
├── projectsetup.json
└── scripts/
    ├── install-ai-tools.sh
    └── post-create.sh
```

Use Compose for every generated setup. A project without a database or with SQLite has only the primary `app` service; selecting PostgreSQL adds the `postgres` service and its data volume. SQLite is installed in the app image. The project-scoped user-defined network is intentional so Docker's embedded DNS behavior is consistent across presets.

## Presets

### Shared Base

All presets must provide:

- A Debian or Ubuntu-based glibc image compatible with the installed `dworm_endpoint`.
- `/bin/bash`.
- `curl`, CA certificates, Git, GnuPG, and `sudo` as needed by setup and `dworm`.
- A non-root `vscode` user with a writable `/home/vscode`.
- Both `containerUser` and `remoteUser` set to `vscode`.
- The final effective Dockerfile user set to `vscode`.
- GitHub CLI through a Dev Container feature unless the base image already provides a reliable equivalent.
- AI tool mounts based on the selected tools.
- PATH available through `containerEnv`, because `dworm exec` uses direct `docker exec` and does not apply `remoteEnv`.

Do not mount the host's `.ssh` directory or `.gitconfig`. `dworm` forwards the SSH agent and installs a temporary Git credential helper.

### Node Preset

Use a stable Dev Containers Debian/Ubuntu base and the official Node feature with an explicit version.

Post-create dependency setup:

- `npm ci` for `package-lock.json`
- `pnpm install --frozen-lockfile` for `pnpm-lock.yaml`
- `yarn install --immutable` when appropriate for modern Yarn, with a cautious fallback decision documented in code
- Skip dependency installation when no package manifest exists

Do not automatically start a development server.

### Ruby Preset

Use the same proven versioned Ruby feature as Rails, without Rails-specific features. Include its mise shim directory in `containerEnv.PATH` so Ruby and Bundler work through `dworm exec`.

Post-create dependency setup:

- Run AI tool setup first.
- Run `bundle install` when a `Gemfile` exists.
- Skip dependency setup when no `Gemfile` exists.
- Do not add Node, Active Storage support, a database, a forwarded port, or an application server by default.

### Rails Preset

Use the official Rails Dev Container image or feature set already proven by the existing Rails example. Keep the Ruby version synchronized with `.ruby-version`.

Likely features:

- GitHub CLI
- Rails Active Storage support
- PostgreSQL client when PostgreSQL is selected
- Node
- Docker-outside-of-Docker only when there is a concrete workflow need; avoid enabling it unconditionally

Post-create behavior:

- Run AI tool setup first.
- Run `bin/setup --skip-server` when `bin/setup` exists and is executable.
- Otherwise run a conservative Bundler setup if a `Gemfile` exists.
- Do not start Rails automatically.

### Python Preset

Use an official Dev Containers Python image or the official Python feature on the shared base, with an explicit version.

The container should include common build prerequisites only when needed. Avoid installing a large universal native dependency set. Users can add packages with repeatable `--system-package` flags.

Post-create dependency setup:

- `uv sync --frozen` when `uv.lock` exists
- `uv sync` when using uv without a lockfile and a `pyproject.toml` exists
- `poetry install` when Poetry is selected
- `python -m pip install -r requirements.txt` when pip is selected and the file exists
- Also install `requirements-dev.txt` when present and pip is selected
- Skip application dependency setup when no matching manifest exists

The image or generated setup must ensure the selected package manager is available. Prefer a Dev Container feature when reliable; otherwise install it in a dedicated, idempotent setup script. Do not install Python package managers into a bind-mounted AI tool directory.

Do not automatically create or activate a project virtual environment unless required by the selected package manager. For uv and Poetry, prefer an in-project `.venv` if that is the documented project convention; otherwise retain the tool default in v1 and document it.

Do not automatically start Django, Flask, FastAPI, or another server.

## PostgreSQL Capability

When selected, extend the shared Compose configuration with:

- One primary development service.
- One `postgres` service.
- A pinned PostgreSQL major version.
- A named volume for database data.
- Development-only credentials.
- A health check.
- Primary service dependency using a healthy condition where supported.
- `DB_HOST=postgres` in `containerEnv`.

Keep service name, hostname, credentials, and generated environment consistent from one normalized database model.

Do not forward the PostgreSQL sidecar port merely for `dworm`: `dworm` scans only the primary service. Add a host Compose port only when explicitly desired because publishing a database port can conflict with local services. `forwardPorts` does not expose sidecar ports through `dworm`.

Application-specific database names and framework configuration are not rewritten in v1. `check` should warn when a detected application configuration appears incompatible.

## Go, Rust, and Redis Add-ons

- `go` installs the official Go feature with an explicit `version` option (default `1.27`) and adds `/usr/local/go/bin` and `/go/bin` to `containerEnv.PATH`.
- `rust` installs the official Rust feature with an explicit `version` option (default `1.99`) and adds `/usr/local/cargo/bin` to `containerEnv.PATH`.
- `redis` adds a `redis` sidecar (`redis:VERSION-trixie`, default version `8`) with a `redis-cli ping` health check and the `redis-data` named volume at `/data`, installs `redis-tools` in the app image, and sets `REDIS_URL=redis://redis:6379`. Like PostgreSQL, its port is not published.

The generated `containerEnv.PATH` replaces PATH changes that features make through their own `containerEnv`, so every definition that installs a feature lists its PATH entries: the Node feature needs `/usr/local/share/nvm/current/bin` (node and rails presets), the Python feature `/usr/local/python/current/bin` and `/usr/local/py-utils/bin`, and the Ruby feature the mise shims. The opt-in smoke test checks each preset runtime and add-on tool through a non-login `docker exec`, as `dworm exec` runs commands.

## SQLite Capability

When selected, install `sqlite3` and `libsqlite3-dev` in the primary application image. Do not add a SQLite Compose service, dependency, volume, or database environment variables. Application-specific database paths and framework configuration are not rewritten.

## AI Tool Persistence

OpenCode mounts:

```text
${localEnv:HOME}/.config/opencode -> /home/vscode/.config/opencode
${localEnv:HOME}/.local/share/opencode -> /home/vscode/.local/share/opencode
${localEnv:HOME}/.opencode -> /home/vscode/.opencode
${localEnv:HOME}/.cache/opencode -> /home/vscode/.cache/opencode
```

Claude mounts when enabled:

```text
${localEnv:HOME}/.claude -> /home/vscode/.claude
${localEnv:HOME}/.local/share/claude -> /home/vscode/.local/share/claude
```

Set:

```text
CLAUDE_CONFIG_DIR=/home/vscode/.claude
```

Codex mounts when enabled:

```text
Compose source: ${CODEX_HOME:-${HOME}/.codex} -> /home/vscode/.codex
${localEnv:HOME}/.local/share/codex -> /home/vscode/.local/share/codex
```

Set `CODEX_HOME=/home/vscode/.codex` in `containerEnv`. Use Compose for state because Dev Container interpolation does not support nested defaults. Host directory creation, checks, and doctor honor an absolute exported host `CODEX_HOME`; it must be consistent at generation and container startup.

Use the official standalone installer with installation-only `CODEX_HOME=~/.local/share/codex` and `CODEX_INSTALL_DIR=~/.local/share/codex/bin`. Keep runtime state separate. Normalize the installer's current-release and command links to relative links; link the local command through `~/.local/bin/codex`. Mount the entire installation directory. Require a shared executable compatible with the Linux container's CPU architecture and fail on an existing incompatible or broken binary without replacing it. Document how to put the shared command on the host PATH; do not migrate arbitrary existing host installations.

File-based Codex credentials and all local history/session state are shared. Doctor checks auth.json metadata only and warns about missing file-based login or keyring/auto configuration. Never silently rewrite host Codex configuration, print credentials, or recursively chown its state directory.

Maintain one canonical embedded `install-ai-tools.sh` used by all presets. It should accept selected tools as arguments rather than having preset-specific copies.

Required behavior:

- `set -euo pipefail`
- Repair ownership of required parent directories when Dev Container features created them as root.
- Assert bind mounts are writable.
- Validate binaries with `--version`.
- By default install only missing or invalid tools through their native installers, except incompatible existing Codex binaries must fail for host repair. Explicit `--update TOOL...` updates requested tools, reports versions, and verifies the result. Reject invalid arguments before mutations.
- Keep OpenCode at `~/.opencode/bin/opencode` with a link in `~/.local/bin`.
- Discover and link the newest persisted Claude binary under `~/.local/share/claude/versions`.
- Ensure OpenCode's unmounted `~/.local/state` is writable.
- Fail with actionable errors.

Create host mount source directories during `init` before users run `dworm up`.

## `dworm` Compatibility

The installed version observed during design was `dworm v0.4.1`.

Important constraints:

- Users must run `dworm` from the same project root used to create the container.
- `dworm up` invokes `devcontainer up --workspace-folder "$PWD"`.
- `/bin/bash` must exist.
- The injected endpoint is currently Linux amd64 and dynamically linked against glibc.
- `dworm exec` and `dworm shell` use direct `docker exec`.
- Direct execution does not apply Dev Container `remoteEnv`; required PATH entries belong in `containerEnv` or the image's `ENV`.
- The configured container user needs a valid writable home.
- SSH and Git credential forwarding remain active only while the original `dworm up` process is alive.
- The SSH socket is exposed in the container at `/tmp/dworm-ssh-agent.sock`, but later `dworm exec` calls do not automatically receive `SSH_AUTH_SOCK`.
- Listening TCP ports from 1024 through 20000 in the primary container are dynamically forwarded.
- Sidecar ports are not scanned.
- `forwardPorts` is editor metadata and is not consumed by `dworm`.

Generated documentation should explain the normal workflow:

```bash
dworm up
dworm exec -- <command>
dworm shell
```

Avoid claiming that servers start automatically unless a future option explicitly configures that behavior.

## Rendering Strategy

Construct `devcontainer.json` and `projectsetup.json` from Go structs and serialize them with `encoding/json`. Prefer strict JSON over JSONC unless comments provide essential value.

For Compose, either:

- Build a small typed representation and serialize YAML with one focused YAML dependency (chosen: `go.yaml.in/yaml/v3`; `check` parses Compose with the same library), or
- Use a tightly controlled template populated only from the normalized model.

Do not implement a generic text-substitution layer.

Use embedded templates for:

- Dockerfiles
- Shell scripts
- Generated `.devcontainer/README.md`, if included

Write files atomically. Preserve executable modes for scripts. Sort set-like values such as tools and system packages to keep output reproducible.

## Proposed Go Package Layout

```text
cmd/projectsetup/main.go
internal/cli/
    init.go
    check.go
    doctor.go
internal/config/
    model.go
    manifest.go
    normalize.go
internal/detect/
    detect.go           evaluates definition detection rules
internal/generate/
    generate.go
    devcontainer.go
    compose.go
    templates.go
internal/presets/
    presets.go          definition schema, strict parsing, validation
    resolve.go          registry, conditions, placeholder expansion, merging
    load.go             user definitions, project copies, refs, listings
    remote.go           remote locations, HTTPS fetching, indexes, sources.toml
    builtin.go
    builtin/*.toml      embedded preset and add-on definitions
internal/cli/preset.go  preset list, show, validate, eject
internal/cli/preset_remote.go  preset add, update, remove
schema/preset.schema.json
internal/validate/
    validate.go
    external.go
internal/doctor/
    doctor.go
internal/fsutil/
    atomic.go
templates/
    dockerfiles/
    scripts/
    docs/
```

Keep packages small. If this layout creates one-file abstraction packages, consolidate them rather than following it mechanically.

## Error Handling

- Errors must state what failed, where, and how the user can correct it.
- Aggregate independent validation failures so users do not need repeated check cycles.
- Distinguish errors, which make the setup invalid, from warnings, which represent compatibility or convention risks.
- External command errors should include the command and useful stderr without dumping excessive output.
- Do not continue writing when normalization or internal rendering validation fails.

## Testing

### Unit Tests

- Detection for every supported signal and ambiguity.
- Configuration normalization and invalid combinations.
- Workspace and service-name sanitization.
- Version precedence.
- Package manager inference.
- Validation rules.
- Host mount calculation.

### Golden Tests

Render representative configurations and compare the full directory tree against checked-in golden files:

- Node, OpenCode, no database
- Node, OpenCode and Claude, PostgreSQL
- Ruby, OpenCode, no database
- Rails, OpenCode and Claude, PostgreSQL
- Rails without PostgreSQL
- Python with pip, OpenCode, no database
- Python with uv, OpenCode and Claude, PostgreSQL
- Python with Poetry
- Ruby with Go, Node with Rust, and Python with Redis

Golden tests must verify executable file modes as well as contents.

### Integration Tests

At minimum:

- Parse every generated `devcontainer.json`.
- Run `docker compose config` for each Compose golden fixture when Docker Compose is available.
- Run `devcontainer read-configuration` for representative fixtures when the CLI is available.
- Provide an opt-in test that builds one fixture per preset.

Do not require Docker for the normal fast unit test suite.

## Existing References

Use these only as behavioral references; consolidate and correct them rather than copying them wholesale.

Node example:

```text
/home/malte/projekte/bergwiese/ksb2/.devcontainer/
```

Rails example:

```text
/home/malte/projekte/bergwiese/dailymusicdrills/rails-app/.devcontainer/
```

Older two-phase Rails generator:

```text
/home/malte/projekte/bergwiese/bw_rails_stack/
```

`dworm` source:

```text
/home/malte/projekte/dworm/
```

Known inconsistencies to avoid:

- Hard-coded Compose workspace targets that disagree with dynamic `workspaceFolder` values.
- PATH configured only in `remoteEnv` even though `dworm exec` bypasses it.
- Port documentation that disagrees with application defaults.
- Documentation claiming a server starts when post-create explicitly skips it.
- Separate, drifting copies of AI installation scripts.
- Unconditional Docker-outside-of-Docker support without a project requirement.
- SSH or Git configuration mounts that conflict with `dworm` forwarding.

## Preset System

Presets and add-ons are described by TOML definition files instead of Go code. `projectsetup` still owns the core that makes generated setups work with `dworm`; definitions can only contribute to it.

### Core owned by Go

Definitions cannot change:

- The `vscode` user, `/home/vscode`, `containerUser`, `remoteUser`, and the final Dockerfile `USER vscode`.
- The Compose project name, the `app` service, its build, command, workspace volume, and `workspaceFolder`.
- AI tool mounts, AI environment variables, and the canonical `install-ai-tools.sh`. Post-create runs AI setup before any definition script.
- The GitHub CLI feature and the core `containerEnv.PATH` entries.
- Host mounts. Definitions have no mount field; sidecar volumes are named volumes only.

### Definition files

One TOML format with two kinds, decoded strictly with `github.com/pelletier/go-toml/v2`:

- `kind = "preset"`: exactly one per project. Sets `image.base` and usually the language runtime, package managers, and dependency setup.
- `kind = "addon"`: zero or more per project, such as `postgres`, `sqlite`, `rust`, or `go`. Add-ons cannot set `image.base`.

Fields:

```toml
schema = 1
kind = "preset"                 # or "addon"
name = "node"                   # ^[a-z][a-z0-9-]*$
version = "1.0.0"               # MAJOR.MINOR.PATCH
description = "Node.js with npm, pnpm, or Yarn"

[options.version]               # option names: ^[a-z][a-z0-9_]*$
description = "Node.js version"
default = "26"                  # required
pattern = '^[0-9]+(\.[0-9]+){0,2}$'   # exactly one of pattern or choices

[image]
base = "mcr.microsoft.com/devcontainers/base:ubuntu-24.04"   # presets only, top level only
apt = []                        # rendered as one Dockerfile line per contributing block
root_run = []                   # single-line RUN steps as root, after apt
user_run = []                   # single-line RUN steps as vscode, after the final USER vscode

[features."ghcr.io/devcontainers/features/node:1"]
version = "${option:version}"

[container]
path = []                       # absolute containerEnv.PATH entries after the core user entries
env = {}                        # containerEnv; PATH, HOME, USER, CODEX_HOME, CLAUDE_CONFIG_DIR are reserved

[setup]
script = '''...'''              # bash appended to post-create.sh after AI setup

[services.postgres]             # sidecar; the name must not be "app"
image = "postgres:${option:version}-trixie"
restart = "unless-stopped"
environment = { POSTGRES_DB = "${project:name}" }
volumes = { postgres-data = "/var/lib/postgresql" }   # named volume -> container path
app_depends_on = "service_healthy"                     # or service_started
[services.postgres.healthcheck]
test = ["CMD-SHELL", "pg_isready"]
interval = "5s"
timeout = "5s"
retries = 10

[[variant]]                     # additive block applied when every condition matches
when = { preset = ["rails"], not_preset = [], addon = ["postgres"], option = { version = { in = ["17"], below = 18 } } }
# The block may contain image (without base), features, container, setup, and services.
```

Detection rules (presets only):

```toml
[options.version.detect]          # first source with a value wins
sources = [
  { file = ".node-version" },     # first word, without a leading "v" or "ruby-"
  { file = "pyproject.toml", pattern = '(\d+\.\d+)' },   # first capture group
  { builtin = "package-json-engines" },                   # Go parser, see presets.DetectBuiltins
]

[options.package_manager.detect.choices]   # every choice with a matching file, in declaration order
npm = [{ file = "package-lock.json" }]
poetry = [{ file = "poetry.lock" }, { file = "pyproject.toml", pattern = '(?m)^\[tool\.poetry\]' }]

[detect]
signals = ["Gemfile", "*.gemspec"]  # reported when present; globs cannot include a directory
match = [{ file = "bin/rails" }]    # any rule detects the preset; when empty, any present signal does
supersedes = ["ruby"]               # drop these presets when this one is detected

[[detect.suggest]]                  # offered as a default, never added silently
addon = "postgres"
match = [{ file = "config/database.yml", pattern = 'adapter:\s*postgresql' }]

[[detect.warning]]
message = "Pipfile detected, but Pipenv is not supported"
match = [{ file = "Pipfile" }]
```

Paths are clean and relative to the project root. Directories never match. Detected values are validated later, during normalization, not during detection.

Rules:

- Placeholders are limited to `${option:NAME}` (the definition's own options) and `${project:name|home|workspace}`. Unknown names in either namespace are errors; all other text, including shell `${VAR}`, is literal. There is no `text/template` in definitions.
- Option values must match the option's `choices` or anchored `pattern` and the shell-safe character set `^[A-Za-z0-9][A-Za-z0-9._+-]*$`.
- Contributions are applied in this order: the preset, then add-ons sorted by name. Each definition's top-level block comes first, followed by its matching variants in file order. A variant can override its own definition's feature options, env values, and service fields. Two different definitions cannot set the same feature, env key, or service.
- A condition's `below` compares the option value's leading integer.
- Output stays deterministic. JSON maps are sorted. Compose services are ordered `app` first, then the remaining services sorted by name.

### Sources and lookup

- Built-in definitions are embedded from `internal/presets/builtin/*.toml`.
- User definitions live in `os.UserConfigDir()/projectsetup/presets/` (`~/.config/projectsetup/presets` on Linux). `PROJECTSETUP_CONFIG_DIR` replaces `os.UserConfigDir()/projectsetup`, so definitions are read from `$PROJECTSETUP_CONFIG_DIR/presets` and `sources.toml` will live next to that directory. Each file is `NAME.toml`; other files are ignored. A user definition cannot reuse a built-in name; `preset eject NAME --as NEW` copies a built-in (or user) definition, changing only its `name` line, for editing. Copied variant conditions and `supersedes` still name the original definitions.
- Remote definitions are installed only by `preset add URL|github:owner/repo[/path][@ref]`. They are fetched over HTTPS with the standard library, validated completely before installing, and recorded with their source URL and sha256 in `sources.toml`. A URL may point to one definition or to an `index.toml` listing several. `preset update` shows a diff and asks for confirmation. `init`, `check`, and `upgrade` never use the network.
  - `github:owner/repo[/path][@ref]` maps to `https://raw.githubusercontent.com/owner/repo/REF/path`; the ref defaults to `HEAD`, and a path that does not end in `.toml` (or no path) names a directory holding `index.toml`.
  - A URL whose last path segment is `index.toml` is an index: `schema = 1` and `definitions = ["a.toml", "dir/b.toml"]`, clean relative `.toml` paths resolved against the index URL, at most 64 entries. Every file is at most 64 KiB, redirects must stay on HTTPS, and URLs cannot contain credentials.
  - Installed definitions are written to the user directory as `NAME.toml` and are recorded in `sources.toml` as `[[definition]]` tables with `name`, the definition file `url`, the `ref` of a github location, `sha256`, and `fetched`. `Load` uses the URL as the definition's source, so manifests record it, and fails when a file does not match its recorded digest or a recorded file is missing.
  - `add` refuses built-in names and names that already exist; `update [NAME]` re-fetches each recorded URL, refuses a changed `name`, and installs nothing if any fetch fails; `remove NAME` deletes only remote definitions. `add` and `update` print the content or diff and a credential warning, and accept `--yes` to skip confirmation. `sources.toml` is written before definition files and after removals, so an interruption never leaves an unpinned remote definition; `update` repairs a missing or mismatched file.
  - `add`, `update`, and `preset validate` resolve each definition with default options: a preset alone, an add-on on top of a minimal preset.
- Each generated project gets a copy of the exact resolved definition files in `.devcontainer/presets/`. The manifest records each definition's name, version, source, and sha256. `check` and `upgrade` use that copy, so they do not depend on the local registry; `upgrade --refresh-presets` re-resolves from the registry.
- Remote definitions run shell code in a container that has AI credentials mounted and a forwarded SSH agent. Install them only on explicit command, show their content before installing, pin hashes, and never update them automatically.

### Manifest schema 2

```json
{
  "schemaVersion": 2,
  "projectName": "example",
  "preset": { "name": "node", "version": "1.0.0", "source": "builtin", "sha256": "..." },
  "addons": [{ "name": "postgres", "version": "1.0.0", "source": "builtin", "sha256": "..." }],
  "options": { "node": { "version": "26", "package_manager": "npm" }, "postgres": { "version": "18" } },
  "aiTools": ["opencode"],
  "ports": [],
  "systemPackages": [],
  "generatedBy": "projectsetup"
}
```

`options` records every option of every selected definition, including defaults; definitions without options have no entry. `check` and `upgrade` require that normalizing the manifest reproduces it.

`check` and `upgrade` continue to read schema 1 manifests. Those map to the built-in definitions: `database` becomes the `postgres` or `sqlite` add-on, `languageVersion` and `packageManager` become preset options, and a missing PostgreSQL version means `17`. `upgrade` writes schema 2.

`init --force` keeps the option values of add-ons that the existing manifest also selected unless they are set explicitly, so a PostgreSQL data volume stays readable.

The CLI gains a repeatable `--addon NAME` and `--set [DEFINITION.]OPTION=VALUE`. These flags remain as aliases: `--node-version`, `--ruby-version`, `--python-version`, `--package-manager`, `--database`, and `--postgres-version`.

## Preset System Roadmap

Each phase ends with `go test ./...`, `go vet ./...`, and gofmt passing.

### Phase 1: Definition schema and loader

- [x] Add `internal/presets` with typed definitions, strict TOML decoding, aggregated validation errors that name the file, placeholder checking and expansion, conditions, and `Resolve`.
- [x] Add table-driven tests for valid definitions, every rejection rule, placeholder handling, variant matching, merge order, and cross-definition conflicts.

### Phase 2: Built-ins as embedded definitions

- [x] Port node, ruby, rails, python, postgres, and sqlite to `internal/presets/builtin/*.toml`.
- [x] Read preset package managers, default language versions, and the default PostgreSQL version from the definitions.
- [x] Render the Dockerfile, `devcontainer.json`, and `post-create.sh` from the resolved definitions. Golden output for these files must stay byte-identical.
- [x] Render Compose from typed YAML nodes and make `check` parse Compose semantically, so files generated by earlier releases still pass. Prove equivalence for Compose golden changes with `docker compose config`.
- [x] Derive the `check` rules for features, containerEnv, PATH, apt packages, and sidecar services from the resolved definitions. Remove the preset-specific switches.

### Phase 3: Data-driven detection

- [x] Add detection to the schema: `[detect]` with `signals`, `match` (a file plus an optional regex), `supersedes`, `[[detect.suggest]]`, and `[[detect.warning]]`; and `[options.NAME.detect]` with value `sources` (`file`, `file` plus `pattern`, or `builtin`) or lockfile `choices`.
- [x] Express the Gemfile Ruby version, `requires-python`, and Poetry metadata as regex rules. Keep only `package-json-engines` as a Go built-in, because it needs JSON parsing.
- [x] Port the four detectors to the built-in definitions and evaluate them generically in `internal/detect`. A characterization test pinned the previous signals, versions, candidates, suggestions, and warnings before the port. `internal/detect/{node,ruby,rails,python}.go` are removed.
- [x] `check`'s language-version, lockfile, and PostgreSQL convention checks use the same rule-based detection. Mapping detected values to `languageVersion`, `packageManager`, and `database` remains until phase 4 replaces those manifest fields with per-definition options.

### Phase 4: Open names, manifest v2, user definitions

- [x] Replace the closed `Preset`, `PackageManager`, and `Database` enums in the normalized model with registry-validated names and per-definition options. Keep the old flags as aliases.
- [x] Write manifest schema 2 and the `.devcontainer/presets/` copies, and extend the generated-path allow-list. `upgrade` reads schema 1 and writes schema 2.
- [x] Load user definitions from the config directory and reject name collisions with built-ins.
- [x] Add `preset list [--json]`, `preset show NAME`, `preset validate FILE`, and `preset eject NAME --as NEW`. `preset validate` also resolves a preset with default options and warns about built-in names and file-name mismatches.
- [x] Publish `schema/preset.schema.json` for editor completion and test that it accepts every built-in. A test also compares its properties with the TOML fields of `presets.Definition`.
- [x] Generate `init --list-options [--json]` from the registry. `OptionsSchemaVersion` is now 2: `packageManagers`, `databases`, and `defaults` are replaced by `addons`, `definitions`, and `defaultAITools`.

### Phase 5: Remote definitions

- [x] Add `preset add URL|github:owner/repo[/path][@ref]`, supporting a single file or `index.toml`.
- [x] Add `preset update [NAME]` with a diff and confirmation, and `preset remove NAME`.
- [x] Record `sources.toml` with the URL, ref, sha256, and fetch time. Enforce HTTPS and size limits, and write into the registry atomically.
- [x] Test with `httptest` servers, including failure, oversize, invalid-definition, and hash-change cases.

### Phase 6: New built-in add-ons

- [x] Add `go`, `rust`, and `redis` add-ons, each with the PATH entries its feature needs, because the generated `containerEnv.PATH` replaces feature PATH changes.
- [x] Add golden fixtures (`ruby-go`, `node-rust`, `python-redis`) and an opt-in build and smoke test per add-on.
- [x] Smoke-test each preset runtime through a non-login `docker exec`. This found that the Node and Python features' PATH entries were missing from the node, rails, and python presets; they are now listed.

### Phase 7: Documentation and release

- [x] Update the README with authoring documentation and a security note, the projectsetup skill, and release notes.

## Implementation Order

- [x] Initialize the Go module and CLI command dispatch.
- [x] Define enums, the normalized configuration model, manifest schema, and validation.
- [x] Implement project detection for Node, Ruby, Rails, and Python with table-driven tests.
- [x] Implement non-interactive `init` flags first so generation is easy to test.
- [x] Implement shared rendering, AI mounts, and the canonical AI installer.
- [x] Add Compose-based Node, Ruby, Rails, and Python presets.
- [x] Add the PostgreSQL Compose capability.
- [x] Add golden fixtures and external configuration validation.
- [x] Add interactive prompting on top of the same normalization path.
- [x] Implement `check` and aggregate diagnostics.
- [x] Implement `doctor`.
- [x] Run real builds for the original three presets and correct first-run permission or lifecycle problems.
- [x] Build the Ruby preset from a clean workspace.
- [x] Smoke-test the Ruby preset from a clean host-directory state.
- [x] Add installation and usage documentation.
- [x] Add explicit same-schema upgrades for previously generated configurations.

## MVP Acceptance Criteria

The first version is complete when:

- A user can initialize an existing Node, Ruby, Rails, or Python project interactively.
- The same setup can be generated non-interactively with stable output.
- OpenCode works after first container creation and persists its state on the host.
- Claude Code and Codex do the same when selected. Codex shares its installation separately from credentials/history; test relative installation links across different home paths and custom CODEX_HOME resolution.
- `dworm up`, `dworm shell`, and normal `dworm exec -- ...` commands work with the generated primary container.
- PostgreSQL setups produce a healthy sidecar and a consistent `DB_HOST` value.
- Static checks identify mismatched paths, users, services, versions, lockfiles, mounts, and scripts before a build.
- Golden tests cover the supported preset combinations.
- At least one generated setup for each preset has been built and smoke-tested from a clean host-directory state.
- The README accurately describes limitations, especially `dworm` session lifetime, sidecar port forwarding, and the absence of automatic server startup.

## Decisions To Keep Simple

When implementation exposes an unspecified detail, prefer the smallest deterministic behavior:

- One primary service name, such as `app`, across all presets.
- Workspace path `/workspaces/<sanitized-project-name>`.
- User `vscode` and home `/home/vscode`.
- OpenCode enabled by default; Claude and Codex opt-in. Accept any validated agent combination, deduplicated and sorted.
- No database by default unless confidently detected and confirmed interactively.
- No default forwarded application port when framework detection is uncertain.
- No server auto-start.
- Refuse ambiguous lockfiles instead of guessing.
- Warn about unsupported existing conventions instead of silently rewriting application files.

These defaults can be expanded after real generated projects demonstrate a concrete need.
