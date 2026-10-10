# Build a local shared image family

The [reference recipes](../examples/shared-images/) build one small customizable family:

```text
Debian 12 foundation: bash, certificates, curl, git, GnuPG, sudo, GitHub CLI
└── Node 22.14.0 + npm 10.9.2, installed under /opt/node
    └── Native modules: build-essential, pkg-config, libpq-dev
```

This is a reference for your CLI bundles, not an official published catalog or a request to upgrade any application's runtime. Choose pins compatible with your projects. Add commonly reused tools to the foundation; add browsers, ffmpeg, or specialist compilers to a separately selected child. Derive children from the **actual built parent**, rather than repeating its installation instructions in separate builds. Ruby-only/Python projects should get their own foundation children when needed, without inheriting Node unnecessarily.

## Verified inputs and limits

The checked-in reference targets **linux/amd64 only**. The Debian base uses the architecture-specific manifest `debian:bookworm-slim@sha256:a4672c0cb26fbdde88e38fa2dfb6c681942306680e41e4378b28770b6e79ee91`, observed through the official registry index on 2026-10-11 (local date). Its upstream [image source](https://github.com/debuerreotype/docker-debian-artifacts) and [official manifest definitions](https://github.com/docker-library/official-images/blob/master/library/debian) identify the Debian/glibc family. The same observed index also had arm64, but this recipe does not claim to build it. Porting requires selecting its corresponding base manifest and archive checksums and then rerunning acceptance on that architecture.

Node's [official release checksums](https://nodejs.org/download/release/v22.14.0/SHASUMS256.txt) identify the x64 archive SHA-256 `69b09dba5c8dcb05c4e4273a4340db1005abeafe3927efda2bc5b249e80437ec`. GitHub CLI's [2.67.0 release](https://github.com/cli/cli/releases/tag/v2.67.0) and [checksums](https://github.com/cli/cli/releases/download/v2.67.0/gh_2.67.0_checksums.txt) identify the amd64 archive SHA-256 `d77623479bec017ef8eebadfefc785bafd4658343b3eb6d3f3e26fd5e11368d5`. These sources were checked before building, and every download is verified in its installation layer. Neither recipe uses Dev Container features or a remote shell installer, so there are no feature installer pins/locks to resolve. Introducing features requires verifying and retaining their exact installer locks separately.

The workflow reproduces the inheritance and verified binary inputs, **not byte-identical rebuilds**: apt repositories are live, and build metadata can change. The resulting content ID pins each local artifact. For long-term rebuild reproducibility, retain apt snapshot/package inputs as well. Keep security/runtime updates explicit; these conservative release pins are examples, not a recommendation of the latest releases.

The foundation establishes the invoking non-root user's primary UID/GID before installing tools. Collisions report the existing account/group and fail rather than deleting it. Tools under `/opt/node` and `/usr/local/bin/gh` are root-owned and readable/executable by `vscode`; `/home/vscode` is writable. Download archives and apt lists are removed in the same layers that create them. The native child's installed compiler/dev libraries are useful runtime development dependencies, not leftover compilation caches.

No artifact contains application dependencies, workspaces, AI credentials/state, host mounts, `VOLUME`, lifecycle commands, or inherited Dev Container project metadata. AI selection and optional Wayland access remain consumer concerns.

## Build and retain local artifacts

Run from the repository on a compatible Linux host with Docker, Dev Container CLI, Go, and Python 3. Use a **new** output path, outside an application repository:

```bash
mise exec go@1.27.2 -- go build -o /tmp/projectsetup-reference-cli ./cmd/projectsetup
examples/shared-images/build.sh /tmp/my-image-family-v1
examples/shared-images/verify-artifacts.sh /tmp/my-image-family-v1/artifacts.lock
```

The build creates unique local tags, builds the foundation once, then builds Node from that foundation and native Node from that Node artifact. It records full image inspection JSON, recipe hashes/copies, Docker version, and tag-to-content-ID pairs in `artifacts.lock`. Keep this output with your artifact records. It does not publish, delete existing resources, or prune caches. Builds leave images/cache on the local daemon intentionally.

**Local content IDs and registry digests differ.** This builder cannot use a raw `sha256:IMAGE_ID` in `FROM`: BuildKit interprets it as a repository/tag. Generated local presets therefore use unique local tags, guarded against the immutable content IDs recorded in the lock before acceptance builds/startup. A local tag itself is mutable, and projectsetup's TOML snapshot alone cannot detect its retargeting. Run `verify-artifacts.sh` before every subsequent local build or recreation. Do not retag these artifacts or treat this guard as a portable registry digest pin. Retain/save the images if another daemon needs exactly these contents.

For registry distribution, explicitly publish through your own authorized registry workflow, inspect the resulting `RepoDigests`, and replace each preset's literal `image.base` with its real `repository@sha256:...` reference. Verify architecture, runtime declarations, and IDs again. This reference workflow never publishes. See [digest-pinned consumption](shared-images.md) for the registry contract.

## Generate consumers and run acceptance

`build.sh` writes custom schema 2 `reference-node` and `reference-node-native` definitions under the output's `config/presets/`. Each fixes Node/npm/gh capabilities and matching ownership. Selecting the native preset selects the native **artifact**; it does not reinstall its expensive packages in each project. Definition variants/add-ons remain useful for project-only contributions, but cannot replace a preset's base/capability contract.

For isolated acceptance, use the supplied script:

```bash
# Only if this Compose/Bake installation needs read access to the CLI's temporary Dockerfile:
BUILDX_BAKE_ENTITLEMENTS_FS=0 examples/shared-images/acceptance.sh \
  /tmp/my-image-family-v1 /tmp/projectsetup-reference-cli
```

The entitlement setting is invocation-scoped, not a host configuration change. The script validates both custom definitions, generates two new npm fixture projects from the same Node artifact, and installs `jq` only in A and `ripgrep` only in B. It runs static checks, Compose configuration, Dev Container read-configuration, `check --runtime`, and real `devcontainer up`. Fixture `npm ci` writes a postinstall marker, proving dependency setup actually ran. Direct non-login `docker exec --user vscode` checks runtime versions, IDs, writable home, marker, and distinct packages. AI tools are disabled for this isolated measurement; the [ownership integration test](shared-images.md#establish-fixed-ids-before-installing-toolchains) separately covers first-run AI mounts.

The script records each actual running container's image ID, size, mounts, and runtime output before removing only its unique fixture containers/networks through Compose. Its fixture source directories, reports, and images remain for review. It does not touch existing application containers or database volumes. Rerun against a new output directory; the script refuses to overwrite its consumers.

For a real existing npm project without `.devcontainer`, use the generated registry explicitly:

```bash
export PROJECTSETUP_CONFIG_DIR=/tmp/my-image-family-v1/config
/path/to/projectsetup preset show reference-node-native
/path/to/projectsetup init --non-interactive --preset reference-node-native --name my-app --ai none
/path/to/projectsetup check
examples/shared-images/verify-artifacts.sh /tmp/my-image-family-v1/artifacts.lock
/path/to/projectsetup check --runtime
```

Use a stable retained config directory for ongoing maintenance. Review existing generated edits before `--force` or upgrade. The generated setup runs `npm ci` only when `package.json` and `package-lock.json` both exist, and never starts a server. Other package managers require their own verified capability/setup definitions. Start/recreate with the normal Dev Container/dworm workflow after checks; direct execution gets declared PATH through `containerEnv`.

## Measure images separately from running state

`measure.py IMAGE_A:TAG IMAGE_B:TAG` streams `docker image save` and sums saved uncompressed layer tar bytes. `measurement.json` reports each image's content ID/diff-ID sequence, the shared prefix/count, the union of layer tar payloads, and the hypothetical sum if both images' layers were stored independently. The difference equals shared layer tar bytes. This is an auditable layer-sharing measure, **not filesystem space reclaimed**, compressed registry traffic, build cache size, or a prediction for another machine. BuildKit can retain additional cache/artifacts; old independently built images remain untouched.

`a.container.json`/`b.container.json` separately record `SizeRw` and mounts from actual startup. Workspace bind contents live on the host, outside image storage. These fixtures have no database or named volumes. Existing database/AI volumes and all other containers are outside the measurement. Compare the captured running image ID to the generated Compose image ID and inspect its history: with matching fixed IDs and `updateRemoteUserUID: false`, startup should use the built image directly, without a new UID-adjustment image/home-copy layer.

See [the recorded acceptance run](image-family-evidence.md) for measured values, IDs, and limits from this machine.

## Update explicitly

1. Review desired OS/runtime/CLI releases, architectures, checksums and feature installer locks (if any); update recipe inputs deliberately and rebuild into a new output directory. Never change IDs on a populated installed home; rebuild from account establishment.
2. Verify the new artifacts, update a retained custom preset's literal base and **all** measured declarations together, and bump its definition release. When publishing separately, use its actual repository digest. Local artifacts need the associated content-ID lock guard.
3. In each selected project, run `projectsetup upgrade --refresh-presets`, then `projectsetup check`, the content-ID guard for local artifacts, and `projectsetup check --runtime`. Refresh validates original snapshots and explicitly adopts the local registry definitions while retaining existing options.
4. Recreate the selected project's container through the normal workflow. Configuration regeneration does not replace a running container. Preserve database volumes; migrations are a separate operation.

Ordinary `projectsetup upgrade` uses exact project snapshots offline and retains their original artifact reference and selections. It does not adopt a rebuilt family, resolve a moving tag, refresh a registry, or migrate a database. Do not manually edit snapshot hashes. An explicit new family build and definition refresh is the boundary for adoption.

## Build/export decision

Phase 4 is conditional. These three small recipes share one actual parent chain, use direct pinned binary archives, and generate consumer contracts in one place; there is currently no duplicated feature-installation machinery to drift. Keep this documented workflow for now. A first-class build/export command is deferred until multiple maintained families duplicate definition installation contributions or feature locks. At that point separate image inputs from project state, reuse typed installation contributions, preserve deterministic recipe/lock/provenance output, and require explicit local builds/publishing. Do not introduce a command or template framework solely to wrap these three Dockerfiles.
