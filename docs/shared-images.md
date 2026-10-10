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

First startup runs the normal post-create script, including `npm ci` when both files exist. No application server starts automatically. Actual startup uses host workspace mounts and Dev Container UID adjustment; mount access, AI first-run behavior, fixed UID/GID policy, and large populated-home ownership costs are separate from artifact verification. Do not treat this walkthrough as proof of safe shared-home ownership or measured two-project layer savings; those acceptance checks remain in Phases 2 and 3.

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
