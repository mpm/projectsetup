# projectsetup v0.8.0

## What changed

- New projects default to the latest stable runtimes: **Node 26** (was 24), **Ruby 4.0** (was 3.3), and **Python 3.14** (was 3.13). Versions detected from `.node-version`, `.nvmrc`, `.ruby-version`, `Gemfile`, `.python-version`, and similar files still take precedence. Node 26 becomes the active LTS line on 2026-10-28.
- New PostgreSQL sidecars use **PostgreSQL 18** (`postgres:18-trixie`), replacing `postgres:17-bookworm`. Following the image's layout change in version 18, the `postgres-data` volume is now mounted at `/var/lib/postgresql` instead of `/var/lib/postgresql/data`.
- The PostgreSQL major version is recorded as `postgresVersion` in `projectsetup.json`. A data volume can only be opened by the major version that created it, so the recorded version is never changed implicitly.
- Add `init --postgres-version MAJOR` to choose a different major version. Versions before 18 use the `-bookworm` image and the old data path.
- `init --list-options --json` includes `postgresVersion` in each preset's defaults.

## Existing projects

Existing projects keep the language version recorded in their manifest.

Manifests from v0.7.0 and earlier that use PostgreSQL have no `postgresVersion` and are treated as PostgreSQL 17, the version those releases generated. `projectsetup check` passes for them unchanged. `projectsetup upgrade` keeps `postgres:17-bookworm` and the existing volume path, and records `"postgresVersion": "17"`. `init --force` also keeps the version from an existing manifest unless `--postgres-version` is passed.

To move an existing database to PostgreSQL 18:

1. Dump the database.
2. Regenerate with `projectsetup init --force --postgres-version 18`, passing the project's original options.
3. Remove the old `postgres-data` volume.
4. Recreate the containers and restore the dump.

## Validation

Passed on Linux amd64:

- `go test ./...` and `go vet ./...` with Go 1.27.1; `gofmt -l` reported no files.
- Updated golden fixtures. New tests cover default and explicit PostgreSQL versions, rejection of invalid or misplaced `--postgres-version` values, image and data path selection, legacy manifests in `check` and `upgrade`, and `init --force` keeping the recorded version.
- A generated Node + PostgreSQL project passed `projectsetup check`, `docker compose config`, and `devcontainer read-configuration`. Its sidecar became healthy as PostgreSQL 18.6, with data in `/var/lib/postgresql/18/docker` inside the mounted volume.
- Dev Containers generated with the new defaults built and ran with `devcontainer up` for Node, Ruby, and Python, reporting Node v26.10.0, Ruby 4.0.7, and Python 3.14.8.

Not verified: Rails container builds and arm64/macOS hosts.
