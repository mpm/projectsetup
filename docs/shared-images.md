# Consume a digest-pinned shared toolchain

This walkthrough uses an independently built Node toolchain artifact from two projects. It covers the supported definition schema 2 consumption contract. It does not build or publish an image family; recipes and layer measurements remain later [roadmap work](../SHARED_IMAGES_ROADMAP.md).

## Prepare the artifact

Choose a Debian/Ubuntu glibc image for your container architecture with `/bin/bash`, non-root `vscode`, and writable `/home/vscode`. Prefer root-owned readable runtime installations outside the home directory. Keep credentials, workspaces, application dependencies, lifecycle commands, host mounts, and Docker `VOLUME` declarations out of the artifact. [Inherited metadata checks](../README.md#inherited-dev-container-metadata) reject unsafe project metadata; clearing a label in the consuming Dockerfile cannot repair it.

Obtain the immutable repository digest from your own image build/publish records or inspect a trusted image you have explicitly pulled:

```bash
docker image inspect --format '{{json .RepoDigests}}' YOUR_LOCAL_IMAGE
```

Select the repository reference ending in `@sha256:` followed by 64 hexadecimal characters. Verify the target architecture and the actual executable paths and releases against your artifact; do not copy release numbers from another machine. A local-only image may have no `RepoDigests`: record a repository digest through your separate artifact workflow before using this digest-pinned example. An image ID is not a repository digest.

**Every digest, release, and tool path below is illustrative.** The example reference does not identify an available or verified artifact. Replace the base and all capability declarations with your measured values before a real build. A digest pins artifact contents; it does not make the original apt repositories or installer downloads reproducible.

## Author the consuming preset

Save this as `shared-node.toml` in the user definition directory (`~/.config/projectsetup/presets/` on Linux). Alternatively set `PROJECTSETUP_CONFIG_DIR` to a dedicated directory and use its `presets/` subdirectory. The TOML `base` must contain the literal reference, rather than a shell variable or `${option:...}` placeholder.

```toml
schema = 2
kind = "preset"
name = "shared-node"
version = "1.0.0"
description = "Fixed Node toolchain with npm project setup"

[image]
base = "registry.example/team/node-toolchain@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

[image.preinstalled]
core_packages = ["bash", "ca-certificates", "curl", "git", "gnupg", "sudo"]

[image.preinstalled.tools.node]
version = "22.14.0"
executable = "/opt/node/bin/node"
path = ["/opt/node/bin"]

[image.preinstalled.tools.npm]
version = "10.9.2"
executable = "/opt/node/bin/npm"
path = ["/opt/node/bin"]

[image.preinstalled.tools.gh]
version = "2.67.0"
executable = "/usr/bin/gh"
path = ["/usr/bin"]

[setup]
script = '''
if [[ -f package.json && -f package-lock.json ]]; then
  npm ci
fi
'''
```

This preset has no runtime option or runtime installer feature: the artifact fixes the Node release. `npm` is a separate declaration, and declared `gh` suppresses the core GitHub CLI feature. Each `core_packages` entry suppresses only that core apt requirement; claim only packages actually provided with working prerequisites. Explicit project packages remain installed. Other package managers need their own declaration and project setup script.

Validate the file before selection:

```bash
projectsetup preset validate ~/.config/projectsetup/presets/shared-node.toml
projectsetup preset show shared-node
```

Use the actual path if you chose another config directory. Validation proves definition consistency, not artifact contents. `.node-version` and `.nvmrc` are checked against declared Node even though this preset has no detection or runtime options. Major/minor pins may match the fixed release; an explicit patch pin must match it. To change the runtime, select a different artifact and corresponding declaration.

## Generate and verify two consumers

In each existing npm project root, review any existing generated-file edits before replacing configuration. For projects without `.devcontainer`, run:

```bash
# First project root
projectsetup init --non-interactive --preset shared-node --name shared-app-a --ai none --system-package jq
projectsetup check

# Second project root
projectsetup init --non-interactive --preset shared-node --name shared-app-b --ai none --system-package ripgrep
projectsetup check
```

Each Dockerfile derives from the same literal artifact and installs its distinct project package. Neither requests the declared runtime or GitHub CLI installers. Generated PATH entries make declared tools available to non-login execution; they do not depend on shell startup files. This example disables AI mounts so consumption can be evaluated separately; selecting AI tools retains the shared installer and host-state mechanism.

Before building, explicitly pull the **real** literal reference you placed in the preset, or otherwise make that exact repository-digest reference available on the local daemon:

```bash
docker pull YOUR_REPOSITORY@sha256:YOUR_REAL_DIGEST
```

In each project, run these explicit verification levels:

```bash
projectsetup check --build
projectsetup check --runtime
```

`--build` preflights the locally available base's effective Dev Container metadata, builds the project, and inspects final metadata and user/HOME. It does not execute project setup or prove installed releases. `--runtime` implies another build and metadata inspection, then checks core prerequisites, writable image home, and every declared executable/release through isolated non-login execution as `vscode`. It uses no network, workspace, credentials, or lifecycle scripts and removes its owned probes. Images and build cache remain. Unsupported custom tool version probes fail explicitly; these three tool identities have supported probes.

After successful checks, start each project using the normal workflow:

```bash
dworm up
```

Keep that process alive for forwarding; in another terminal in the same project root:

```bash
dworm exec -- node --version
dworm exec -- npm --version
dworm exec -- gh --version
```

First startup runs the normal post-create script, including `npm ci` when both files exist. No application server starts automatically. Actual startup uses host workspace mounts and, in this portable example, Dev Container UID adjustment. Fixed-ID consumption is available through the contract below. Host mount access and AI first-run behavior still require startup verification. Do not treat this walkthrough as proof of safe shared-home ownership or measured two-project layer savings; measured two-project layer and ownership acceptance remains in Phase 3.

## Establish fixed IDs before installing toolchains

For a populated shared home, choose a base artifact with the intended `vscode` UID/GID, then add `[image.ownership]` to the consuming preset:

```toml
[image.ownership]
mode = "fixed"
uid = 1000
gid = 1000
```

Use your actual positive IDs (maximum 2147483647). On Linux they must equal `id -u` and the primary `id -g` of the invoking user; do not assume every machine uses 1000. macOS fixed-ID support is for Docker Desktop sharing and does not require numeric equality. Other hosts and remote Docker daemons are unsupported. Normal check/staged generation validates host IDs and workspace/AI source write/search access. Startup must still verify effective bind access; runtime artifact probes intentionally mount no host state.

The shared image author establishes IDs before installing any user-owned toolchain. This minimal recipe fragment assumes an existing `vscode` account and primary group, `/home/vscode`, and no populated user installations yet. Replace the base with a trusted, verified Debian/Ubuntu glibc artifact and choose the IDs for its intended consumers:

```dockerfile
FROM YOUR_VERIFIED_BASE
USER root
ARG VSCODE_UID=1000
ARG VSCODE_GID=1000
RUN set -eu; \
    old_group="$(id -gn vscode)"; \
    uid_owner="$(getent passwd "$VSCODE_UID" | cut -d: -f1 || true)"; \
    gid_owner="$(getent group "$VSCODE_GID" | cut -d: -f1 || true)"; \
    if [ -n "$uid_owner" ] && [ "$uid_owner" != vscode ]; then \
      echo "UID $VSCODE_UID belongs to $uid_owner; select another base or ID" >&2; exit 1; \
    fi; \
    if [ -n "$gid_owner" ] && [ "$gid_owner" != "$old_group" ]; then \
      echo "GID $VSCODE_GID belongs to $gid_owner; select another base or ID" >&2; exit 1; \
    fi; \
    groupmod -g "$VSCODE_GID" "$old_group"; \
    usermod -u "$VSCODE_UID" -g "$VSCODE_GID" vscode; \
    chown "$VSCODE_UID:$VSCODE_GID" /home/vscode
# Install root-owned readable runtimes under /opt here where practical.
# Install any required user-owned tools only after the account IDs above exist.
ENV HOME=/home/vscode
USER vscode
```

Do not use this fragment to renumber an existing populated toolchain; rebuild it from the account-establishment stage. Account collisions fail with an owner and remedy rather than deleting unrelated users/groups. Verify supplementary group membership and sudo configuration for your chosen base. The consumer Dockerfile asserts existing IDs before its installation steps and never mutates accounts; `check --runtime` verifies final numeric IDs. Fixed consumption disables Dev Container UID adjustment, avoiding that mechanism's home-copying layer. Actual savings still need the Phase 3 measurements.

AI mounts remain outside the artifact. The shared installer creates/repairs only exact unmounted container parent directories. It checks mounted sources and nested entries for access, never recursively chowns them, and reports the host source requiring repair. Correct access deliberately on the host or choose a compatible image; do not run recursive ownership repair across credentials, caches, or history.

To exercise the isolated ownership integration test against an existing local compatible image:

```bash
PROJECTSETUP_OWNERSHIP_TESTS=1 PROJECTSETUP_OWNERSHIP_BASE=YOUR_LOCAL_IMAGE \
  go test ./internal/generate -run '^TestFixedOwnershipIntegration$' -v
```

The base needs matching Linux host IDs, the core prerequisites, and `/usr/bin/gh`. The test derives and removes its own images, uses temporary bind sources and a fake offline Codex installer, and never mounts your credentials. If your Compose/Bake version requires a filesystem entitlement for the Dev Container CLI's temporary Dockerfile, grant read access to that test-owned temporary path or use an invocation-scoped `BUILDX_BAKE_ENTITLEMENTS_FS=0` for this isolated test. No host configuration change is required. This test verifies ownership and first-run access; it does not measure shared layers or test macOS sharing.

## Regenerate offline and adopt updates explicitly

Each project's manifest records a SHA-256 hash of the exact preset TOML copied into `.devcontainer/presets/`. That definition hash pins the base reference and declarations; it is distinct from the image's registry digest. Do not edit project snapshots or their hashes manually.

```bash
projectsetup upgrade
projectsetup check
```

Ordinary upgrade uses the recorded snapshots offline, retaining the literal artifact even if the user registry has changed or the original definition was removed. It preserves options, packages, ports, and AI selection. Static checks also use snapshots and never pull an image; installed external configuration tools may run without building it. Offline regeneration does not promise an offline Docker build: project apt/dependency steps can still need a network.

To adopt another artifact deliberately, update the user definition's literal digest and measured declarations together, bump its definition release version (for example to `1.0.1`), and validate it. If project version pins need changing, make that application decision explicitly too. Then, in each selected consumer:

```bash
projectsetup upgrade --refresh-presets
projectsetup check
# Explicitly make the new literal artifact available locally before these checks.
projectsetup check --build
projectsetup check --runtime
```

Refresh resolves all recorded preset/add-on names from the local registry, retaining recorded options and filling only newly introduced defaults. It validates the original snapshots first; a corrupted project snapshot is not repaired by refresh. Remote definitions must be updated through a separate explicit `preset update` before refresh. Refresh does not resolve a moving image tag, fetch an image, or replace an existing running container. Recreate the project's container through your normal Dev Container workflow to apply the new files; preserve database volumes and treat database migration separately.
