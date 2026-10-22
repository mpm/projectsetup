# projectsetup v0.12.0

This release adds host-path-preserving primary-checkout and shared-worktree mounts for orchestrated Git worktrees without changing the normal Dev Container workspace.

## New

- Every `devcontainer.json` mounts `${localWorkspaceFolder}` to the identical container path. The existing `/workspaces/<project>` workspace and `workspaceFolder` are unchanged.
- Every existing `app` service bind-mounts `${HOME}/.local/share/projectsetup/worktrees` at the identical path. It uses Compose long syntax with `bind.create_host_path: false`; no sidecar, service, project, network, or volume is added.
- `init` and `upgrade` create the global root as the invoking user, prefer mode `0700` for newly created directories, and verify directory plus write/search access before atomically installing generated output. Compose is never allowed to create the source.
- Manifest schema 2 now includes the required canonical symbolic field `"worktreeRoot": "${localEnv:HOME}/.local/share/projectsetup/worktrees"`. It is not configurable and the schema number remains 2. Older strict binaries reject the unknown field rather than silently dropping the mount.

## Validation and compatibility

- `check` semantically validates both core mounts, identical worktree source/target, app-only placement, `create_host_path: false`, host-root access, and merged Dev Container metadata. Definition mounts that overlap the primary checkout or worktree root are rejected.
- Schema 1 and pre-v0.12 schema 2 manifests that omit `worktreeRoot` remain readable by `upgrade`. `check` gives actionable upgrade guidance; a present noncanonical value is invalid. Upgrade preserves all existing selections and writes current schema 2 output.
- Golden fixtures cover every preset combination. Tests also cover v0.11-style upgrade, atomic host-preparation failures, definition collisions, external Compose/Dev Container parsing, and PostgreSQL remaining exactly one sidecar.
- `PROJECTSETUP_WORKTREE_TESTS=1 go test ./internal/generate -run TestSmokeHostPathWorktree` is an opt-in reversible runtime test. It creates a real linked Git worktree under an isolated global root, starts one uniquely named fixture, verifies both identical host-path mounts and Git access from the container, and removes only that fixture container/network and temporary data. It never removes volumes.

## Existing projects

Regenerate and recreate the container:

```bash
projectsetup upgrade
projectsetup check
dworm down
dworm up -d
```

The worktree root is shared across generated app containers. Keep only intended development worktrees below it. Massimo is responsible for collision-safe child names using project digest plus stable checkout ID; projectsetup does not namespace by repository basename.

[All changes since v0.11.0](https://github.com/mpm/projectsetup/compare/v0.11.0...v0.12.0)
