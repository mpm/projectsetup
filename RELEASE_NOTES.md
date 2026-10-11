# projectsetup v0.10.0

Reuse a shared toolchain image across projects while keeping each project's dependencies, setup, and AI state separate. This release adds explicit image capability contracts, build and runtime verification, and a measured local image-family workflow.

## Shared toolchain images

- **Definition schema 2** adds optional `image.preinstalled` declarations for tools, exact releases, executable paths, PATH entries, and core apt packages. Generation reuses declared capabilities, omits covered core installers, and keeps tools available to non-login `dworm exec`. Conflicting known installers and incompatible project version pins fail before files are replaced.
- **Build verification** inspects inherited Dev Container metadata before building and checks the resulting image's user, home, and effective configuration. Unsafe inherited lifecycle commands and mounts are rejected.
- **`projectsetup check --runtime`** builds and verifies declared capabilities through isolated, network-disabled probes as `vscode`. It checks supported tool versions, executable lookup, core prerequisites, and writable home without mounting a workspace or credentials or running project lifecycle scripts. Independent failures are aggregated; unsupported custom version probes fail explicitly.
- **Fixed image ownership** lets a preset declare the image's existing `vscode` UID/GID and disable Dev Container UID adjustment. Consumers assert the IDs rather than renumbering a populated toolchain. Linux requires matching invoking-user IDs and accessible workspace/AI sources; macOS support targets Docker Desktop sharing. Remote Docker daemons are unsupported for fixed ownership.
- **Explicit Linux host integrations** add typed bind declarations, including optional Wayland access, with source and numeric-access validation. These integrations remain opt-in consumer configuration.
- **Customizable local image-family recipes** demonstrate a Debian foundation, Node toolchain child, and native-development child, with verified archive checksums, artifact locks, and two independent project consumers. The recorded Linux amd64 run shares approximately 407.9 MiB of saved uncompressed layer-tar payload (49.5% of a hypothetical independent-copy sum). This measures shared layer payload, not reclaimed daemon disk space.

Start with [shared-image consumption](https://github.com/mpm/projectsetup/blob/v0.10.0/docs/shared-images.md), [building a local family](https://github.com/mpm/projectsetup/blob/v0.10.0/docs/image-family.md), and the [measured acceptance evidence](https://github.com/mpm/projectsetup/blob/v0.10.0/docs/image-family-evidence.md).

The reference recipes are Linux amd64 examples, not a published image catalog. Local tags are checked against recorded content IDs; portable registry consumption requires a real repository digest. A first-class image build/export command remains deferred. Ordinary generation and checks do not establish that an image actually contains its declared tools; request runtime verification explicitly.

## PostgreSQL image pins

`--postgres-image` selects a literal maintenance tag, optionally with a SHA-256 registry digest, while `--postgres-version` retains the data-compatibility major. Validation rejects moving tags, malformed references, and mismatched majors. Upgrade and regeneration preserve the pin; `--postgres-image ''` restores the major-only default.

The selected major still controls the data-volume layout. Configuration regeneration does not migrate or delete database data. See [exact PostgreSQL references](https://github.com/mpm/projectsetup/blob/v0.10.0/docs/exact-postgres.md).

## AI installer repairs

- Rerunning the generated `install-ai-tools.sh` **without arguments now checks all AI tools selected for that container**. Previously, an empty tool list silently skipped every tool. Explicit arguments still override the defaults; `--update` still requires tool names.
- Claude's container command links to a particular binary in the host-mounted versions directory. Rerunning the helper refreshes that link after host updates or cleanup, selecting the highest executable version by version number rather than file modification time and validating it with `--version`. Missing or invalid installations use the native installer.
- AI parent-directory repairs remain targeted. The helper validates mounted state access and reports the host source needing repair without recursively changing host ownership.

## Existing projects

Install v0.10.0, review any local generated-file edits, and run:

```bash
projectsetup upgrade
projectsetup check
```

This refreshes generated scripts while retaining recorded options, AI selection, image references, and exact definition snapshots. To repair selected agents in an existing running container afterward:

```bash
dworm exec -- .devcontainer/scripts/install-ai-tools.sh
```

Shared images are opt-in through custom presets; built-in presets keep their existing installation behavior. Definition schemas 1 and 2 are supported, and generated manifests remain schema 2. Older projectsetup versions cannot consume schema 2 definitions or manifests containing the new optional fields.

To adopt a changed shared image, deliberately update its custom definition and measured declarations, run `projectsetup upgrade --refresh-presets`, verify the artifact with `projectsetup check --runtime`, and recreate the container. Ordinary upgrade retains the recorded artifact; it does not pull a new image or replace a running container. Database major upgrades require a separate migration.

## Validation

- `go test ./...`, `go vet ./...`, and generated-fixture Docker Compose and Dev Container configuration checks passed on the release code.
- Regression tests cover no-argument AI selection, explicit overrides, deleted Claude targets, and version ordering despite reversed file timestamps.
- The checked-in image-family evidence records real Linux amd64 builds, runtime checks, two-project startup and dependency setup, fixed-ID behavior, and shared-layer measurements. These opt-in acceptance runs were recorded during feature development; they were not repeated for release packaging.
- The local image-family workflow was not verified on arm64 or macOS. Runtime probes verify image capabilities; they do not replace first-start validation of host mounts or application setup.

[All changes since v0.9.0](https://github.com/mpm/projectsetup/compare/v0.9.0...v0.10.0)
