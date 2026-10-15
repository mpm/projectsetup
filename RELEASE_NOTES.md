# projectsetup v0.10.1

A patch release fixing two shared-image problems found while adopting the v0.10.0 workflow in real projects.

## Fixes

- **AI installer accepts read-only owner-owned files.** Rerunning `install-ai-tools.sh` against mounted AI state no longer fails on valid read-only files such as Codex vendor libraries and Git pack files. Regular files must be readable and either writable or owned by the container user; their modes are preserved. Mounted directories still require read/write/search access. Unreadable files, foreign non-writable files and inaccessible directories still fail and name the host source to repair. Host ownership and permissions are never changed.
- **Bundler 4 runtime probes.** `projectsetup check --runtime` now recognizes Bundler 4's bare `X.Y.Z` version output as well as the traditional `Bundler version X.Y.Z`, while still requiring the exact declared release.

## Existing projects

Install v0.10.1 and refresh generated scripts:

```bash
projectsetup upgrade
projectsetup check
```

To repair selected agents in an existing running container afterward:

```bash
dworm exec -- .devcontainer/scripts/install-ai-tools.sh
```

Recorded options, image references and definition snapshots are retained; there are no schema changes.

## Validation

- `go test ./...` and `go vet ./...` passed on the release code.
- New regression tests cover read-only owner-owned Codex files (modes and content unchanged, no permission repair attempted, command link refreshed) and Bundler 4 version output.
- Both fixes were exercised in real Node, Ruby and Rails consumer adoptions on Linux amd64 using a local build of this code.

[All changes since v0.10.0](https://github.com/mpm/projectsetup/compare/v0.10.0...v0.10.1)
