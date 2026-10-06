# projectsetup v0.7.0

## What changed

- Add `projectsetup init --list-options [--json]`, which lists the values `init` accepts: presets, package managers per preset, databases, AI tools, the project name pattern, and per-preset defaults (package manager, language version, database, AI tools). The listing comes from the same typed tables that parsing, validation, normalization, and the prompts use, so it always matches the installed version. It does not run detection, prompt, or write files. (#1)
- Project names must now match `^[a-z0-9][a-z0-9_-]*$`. That one name is used for the Compose project `name:`, the devcontainer.json `name`, the manifest `projectName`, the `/workspaces/<name>` folder, and `PGDATABASE`. Previously a name like `a.b` gave the Compose project `a-b` and `a.b` everywhere else. (#2)
- `--name` is never rewritten. With `--non-interactive`, an invalid name is an error that suggests a valid alternative (for example, `a.b` suggests `a-b`). Interactive mode offers the normalized name and asks again until the name is valid. (#2)
- A name taken from the directory is still normalized; a directory name that is already valid stays unchanged. (#2)

## Listing accepted values

```bash
projectsetup init --list-options --json
```

The JSON includes a `schemaVersion` (currently `1`) for the listing format. Ruby and Rails have no package-manager choice, so their `packageManagers` entry is an empty array and their default package manager is `null`. `--list-options` can be combined only with `--json` and `--non-interactive`; `--json` requires `--list-options`.

## Existing projects

Manifests created by v0.6.0 or earlier with a name containing a dot (for example `a.b`) no longer pass `projectsetup check`, and `projectsetup upgrade` refuses to rename them. To regenerate with a valid name, run:

```bash
projectsetup init --force --name a-b
```

`init --force` runs detection again, so pass your original preset, database, and AI options along with it. Projects with valid names are not affected.

## Validation

Passed on Linux amd64:

- `go test ./...` and `go vet ./...` with Go 1.27.1, plus a `gofmt -l` check that reported no files.
- Table-driven tests for name validation and normalization, rejection of the names from issue #2, consistency of the name across all generated files, interactive re-prompting, and `check`/`upgrade` behavior with old dotted manifests.
- An exact JSON golden test for `--list-options --json` that also confirms no files are written. Further tests run every listed preset × package manager × database through `init --non-interactive`, check that the listed defaults match normalization, and confirm that `bun` is rejected and not listed.

Generated container fixtures were unchanged.
