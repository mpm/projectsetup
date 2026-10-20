# projectsetup v0.11.0

This release makes read-only host integrations actually read-only, and lets `upgrade` add and remove add-ons.

## Fixes

- **Read-only typed binds are honored under Compose.** Schema 2 `[[container.mounts]]` integrations were rendered into `devcontainer.json` `mounts`. For Compose projects (every generated project), Dev Container CLI 0.89.0 rewrites those into a short-syntax Compose override and drops `readonly`, so a `read_only = true` bind was writable in the container (`docker inspect` showed `RW=true`). Integrations are now `app` volumes in `compose.yaml` in long syntax (`type: bind`, `source`, `target`, `read_only: true`), like the Codex state mount. Each `${localEnv:NAME}` source reference becomes `${NAME:?export it on the host for projectsetup host integration TARGET}`, so a missing or empty variable still stops container creation and names the variable and target. Definition snapshots in `.devcontainer/presets/` keep the `${localEnv:NAME}` form.
- **Checks follow the new location.** `check` requires each integration's Compose bind with its exact source and access mode, rejects other `app` volumes while integrations are selected, and rejects integrations left in `devcontainer.json` `mounts`, pointing to `projectsetup upgrade`. `check --build` accepts only the generated AI mounts in effective Dev Container metadata.

## New

- **`upgrade --addon NAME` and `--remove-addon NAME`** (both repeatable). Added add-ons come from the installed built-in and user definitions and get their option defaults. Removed add-ons drop their recorded options, and removing `postgres` also drops its image reference. Everything else (preset, retained add-ons and their project copies, option values, PostgreSQL major and image, agents, ports, packages) is kept, and the result is validated like `init`. Combine with `--refresh-presets` to also adopt updated definitions for retained ones.

## Existing projects

Projects with host integrations should upgrade, then recreate the container so the read-only mode takes effect:

```bash
projectsetup upgrade
projectsetup check
dworm rebuild
```

To adopt an add-on without re-running `init --force`:

```bash
projectsetup upgrade --addon NAME
```

Projects without host integrations get identical output. There are no manifest or definition schema changes.

## Validation

- `go test ./...` and `go vet ./...` passed on the release code.
- New golden and table tests cover read-only and read-write directory, file and socket binds, Compose interpolation of missing or empty variables, validation of moved, changed and leftover mounts, upgrading a project in the old layout, and add-on changes on upgrade (including rejected names and retained project copies).
- The opt-in `TestSmokeHostIntegrationAccessModes` started a real container with `devcontainer up` (Dev Container CLI 0.89.0, Docker Compose 5.6.0, Linux amd64). `docker inspect` showed `RW=false` for read-only directory and socket binds and `RW=true` for a read-write file. A write to the read-only directory failed, and a write to the read-write file reached the host.

[All changes since v0.10.1](https://github.com/mpm/projectsetup/compare/v0.10.1...v0.11.0)
