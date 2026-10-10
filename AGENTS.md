# Agent Instructions

## Project

This repository contains `projectsetup`, an opinionated Go CLI for generating deterministic Dev Container configurations for Node, Ruby, Rails, and Python projects.

Read `HANDOFF.md` before making architectural or implementation decisions. It defines the MVP scope, CLI contract, generation rules, `dworm` compatibility requirements, validation behavior, test strategy, and implementation order.

## Development Principles

- Treat the tool as a constrained configuration compiler, not a general-purpose template engine.
- Normalize detected, prompted, and flag-provided values into one typed model before rendering.
- Keep generation deterministic and output ordering stable.
- Prefer Go's standard library and add dependencies only when they clearly simplify the implementation.
- Keep terminal interaction separate from detection, normalization, generation, and validation logic.
- Prefer the smallest implementation that satisfies the current MVP. Do not add plugin systems, remote templates, migrations, or compatibility layers without a concrete requirement.
- Return actionable errors that identify the failed operation or file.
- Aggregate independent validation failures where practical.

## Generated Dev Containers

- Use Debian or Ubuntu-based glibc images with `/bin/bash`.
- Use `vscode` as both `containerUser` and `remoteUser`, with `/home/vscode` as its writable home.
- Ensure the final effective image user is `vscode`.
- Put environment needed by `dworm exec` in `containerEnv` or the image, not only in `remoteEnv`.
- Do not mount host `.ssh` directories or `.gitconfig`; `dworm` forwards those credentials.
- Use Docker Compose for every generated Dev Container; add the PostgreSQL sidecar only when selected.
- Do not automatically start application servers.
- Keep AI installation behavior shared across all presets rather than copying scripts.

## Code Organization

- Keep command parsing thin.
- Put project detection in dedicated, testable functions.
- Use validated enum-like Go types for AI tools; validate preset, add-on, and option names and values against the definition registry.
- Build `devcontainer.json` and the manifest from typed structures.
- Use embedded templates only for files such as Dockerfiles, shell scripts, and documentation.
- Write generated files atomically and preserve executable script modes.
- Consolidate packages when abstraction would otherwise create one-file wrappers with no useful boundary.

## Testing

- Add table-driven unit tests for detection, normalization, naming, and validation.
- Add golden tests for generated directory trees and executable modes.
- Keep normal unit tests independent of Docker.
- Gate Docker, Compose, and Dev Container build tests as integration tests.
- For behavior changes, test both valid output and relevant failure or ambiguity cases.

Before considering implementation work complete, run the applicable commands:

```bash
go test ./...
go vet ./...
```

Also run formatting on changed Go files:

```bash
gofmt -w <files>
```

When generated fixtures or integration behavior changes, run the relevant `docker compose config`, `devcontainer read-configuration`, or opt-in build checks described in `HANDOFF.md` when those tools are available.

## Scope Discipline

The first version supports Node, Ruby, Rails, Python, optional PostgreSQL, OpenCode, optional Claude Code and Codex, GitHub CLI, initialization, checking, and diagnostics. If a requested implementation detail conflicts with `HANDOFF.md`, call out the conflict and choose the user's latest explicit instruction.
