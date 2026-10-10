# Shared Toolchain Images: Implementation Roadmap

Status: Phase 1 declaration design and schema-evolution decisions completed; executable shared-image support is not yet implemented.

This roadmap records the shared-image design discussed with the user. It is a handoff for a future implementation session. Read `AGENTS.md` and `HANDOFF.md` first, then this document. `HANDOFF.md` remains the contract for existing behavior; this roadmap describes proposed extensions.

## Goal

Let many projectsetup-generated Dev Containers reuse the same prebuilt toolchain layers, while preserving custom presets, custom CLI tools, deterministic generation, and dworm compatibility.

The guiding decision is: **shared-image support belongs in projectsetup; the contents of a user's image family should remain customizable.** Consuming shared images and publishing an official image catalog are separate deliverables.

## Motivation and evidence

The user supplied an analysis of a different developer server. It reported independently built Node/Ruby/Rails toolchain layers despite a common Ubuntu base, and large UID-adjustment layers caused by recursively changing ownership of a populated home directory. Image layers were the largest Docker storage category, but temporary files and writable container filesystems also consumed substantial space.

Treat those measurements as motivation, not measurements of this machine. Runtime versions and image digests in that report must be verified before using them in build recipes. Do not silently upgrade application runtimes or databases as part of this feature.

Identical version settings in separate project builds do not guarantee identical layers. Projects should derive from the same built image artifact to guarantee reuse of its layers on the same Docker daemon and architecture.

## Current implementation baseline

- Custom presets can already set `image.base`, including a literal digest-pinned reference. Add-ons cannot replace the base.
- Definitions can contribute apt packages, `root_run`, `user_run`, features, PATH/environment, detection, and post-create scripts.
- Projects snapshot selected definitions and record their hashes. `check` and ordinary `upgrade` use those copies; `upgrade --refresh-presets` explicitly adopts registry changes.
- Built-in runtime presets request Dev Container features. Merely replacing their base does not stop them requesting those features.
- `internal/generate/devcontainer.go` always requests the GitHub CLI feature and constructs `containerEnv.PATH` independently of image PATH.
- `internal/generate/templates/Dockerfile.tmpl` always installs core apt packages and recursively chowns `/home/vscode/.local`.
- `internal/generate/templates/install-ai-tools.sh` can recursively chown unwritable directories, including a parent containing mounted host state. Codex-specific paths already use a non-recursive, fail-with-guidance policy.
- There is no explicit generated UID/GID policy or `updateRemoteUserUID` setting.
- The built-in PostgreSQL add-on accepts a major version only. Full references cannot be generic option values under the current shell-safe option character restriction.
- Static validation checks declared/generated configuration; `check --build` builds but does not currently run runtime smoke checks.

Reconfirm this baseline against the code before implementation; subsequent sessions may change it.

## Design model

### Shared toolchain image

Contains the OS, common utilities, GitHub CLI, pinned runtimes, reusable native dependencies, and common CLI bundles. Establish its user IDs before installing user-owned toolchains. Publish or retain one artifact for all consuming projects.

Record exact runtime versions in its recipe, feature installer locks where features are used, and the resulting image digest. Exact runtime versions alone do not make apt repositories or every installer download reproducible; the digest fixes the consumed artifact.

### Project preset

Describes the selected image, expected installed capabilities, runtime versions and paths, detection, and project dependency setup. It retains operations such as `npm ci`, `bundle install`, and `uv sync` without reinstalling a runtime that is already baked in.

An installed-runtime declaration is a contract, not proof from static parsing. Build/run verification must check the actual artifact. Avoid exposing an independent runtime option that appears to change a fixed image's runtime without selecting a corresponding image.

### Project-specific add-ons

Continue to provide sidecars, extra packages/tools, environment, and setup steps. Shared expensive additions should move into an image variant when enough projects use them. Installing a custom add-on independently in every project does not guarantee layer sharing.

### Example image family

```text
Ubuntu foundation + common utilities + gh
├── Node
│   ├── Node + common Go tools
│   └── Node + Ruby
│       └── Rails native dependencies + PostgreSQL client
├── Ruby-only, when useful
└── Python + uv, when useful
```

This is a usage-driven example, not a mandatory inheritance tree. Do not require Node for Ruby-only or Python projects. Keep browsers, ffmpeg, and other heavy specialist tools in appropriate variants. Each project still has its own container, workspace, network, and database volumes.

## Compatibility requirements

- Keep Debian/Ubuntu glibc images, `/bin/bash`, user `vscode`, and home `/home/vscode`.
- Keep both `containerUser` and `remoteUser` as `vscode`, and the final effective image user as `vscode`.
- Keep Compose for every project and retain project-scoped services and volumes.
- Keep AI installation, credentials, configuration, and host mounts in the existing shared mechanism. Do not bake credentials or project state into toolchain images.
- Make required runtime paths/environment work through non-login direct `docker exec`, as used by dworm.
- Preserve existing generated configurations and their upgrade semantics. Image changes must be explicit.
- Support custom user and remote definitions without a plugin system or arbitrary configuration fragments.
- Keep normal generation/checks offline and independent of Docker where they are today. Network resolution/building belongs in explicit operations.

## Phase 1: First-class consumption of shared images

Implement the smallest typed extension that makes a custom shared-image preset reliable.

- [x] Design declarations for preinstalled runtimes/tools and their versions/paths, including GitHub CLI and any core image prerequisites the generator will skip installing. See [Declaration contract](#declaration-contract-design-completed).
- [x] Decide definition and manifest schema evolution explicitly. Retain support for existing definition snapshots and manifests; default existing definitions to current installation behavior. See [Schema evolution contract](#schema-evolution-contract-decisions-completed).
- [ ] Extend parsing, registry resolution, normalization, editor schema, and options listing together.
- [ ] Generate project Dockerfiles and Dev Container configuration without requesting installation of capabilities explicitly provided by the image.
- [ ] Avoid redundant core apt installation for images that declare the required prerequisites. Preserve project-specific packages and root/user steps.
- [ ] Account for Dev Container metadata inherited from prebuilt images. Inspect effective feature, environment, user, and lifecycle behavior; baked features must not cause repeated toolchain installation or inherited project setup.
- [ ] Preserve runtime version-file checks for custom shared-image presets. Define how fixed installed versions and any configurable image selection relate; reject inconsistent combinations.
- [ ] Add a separate opt-in runtime verification path, or extend an existing explicit integration path, to inspect the built artifact's capabilities. Do not represent static checks or build success alone as proof that its runtimes exist and work.
- [ ] Document literal digest-pinned image references and an end-to-end custom preset example.

Acceptance: two projects can derive from the same shared image, run their expected tools, and add distinct project packages without rebuilding the baked runtimes. Existing presets still generate and validate correctly.

### Declaration contract (design completed)

This section specifies the first Phase 1 item. It is an implementation contract for subsequent items, **not supported authoring syntax yet**. Current definition schema 1 rejects these fields. Schema numbers and compatibility handling are decided in the schema evolution contract below; parser and editor-schema changes remain the next checklist work. No generated output or runtime behavior changes in these design steps.

#### Shape and ownership

Add an optional `image.preinstalled` contract to the selected preset's **top-level image only**. Like `image.base`, it cannot be contributed by add-ons or variants: it describes the selected base artifact, not something a later project layer installs. An absent contract means existing installation behavior. Empty lists/maps make no claims.

Proposed fields (illustrative values, not verified image releases or references):

```toml
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
```

Use typed structures in `internal/presets`, alongside the existing `Image` model:

```go
type Preinstalled struct {
    CorePackages []CorePackage
    Tools        map[string]InstalledTool
}

type InstalledTool struct {
    Version    string
    Executable string
    Path       []string
}
```

`CorePackage` is a validated string type with the six values listed above, matching the current core apt line exactly. There is no `skip_install`, arbitrary feature-suppression list, or blanket "complete base" boolean. The resolved model carries the validated contract separately from installation contributions; generation and static validation consume that same model.

#### Tool identity, versions, and paths

- Tool keys use the existing definition-name pattern `^[a-z][a-z0-9-]*$`. Reserve `node`, `ruby`, `python`, `go`, `rust`, and `gh` for their known runtime/tool meanings. Use `gh` for GitHub CLI and `rust` with a `rustc` executable. Other keys support custom CLIs without a plugin system. Package managers and companion commands such as `npm`, `pnpm`, `yarn`, `bundler` (executable `bundle`), `pip`, `uv`, `poetry`, and `cargo` are separate declarations; declaring a runtime does not implicitly assert their presence or versions.
- Every tool requires `version`, `executable`, and a nonempty `path` list. Versions describe one concrete installed release, not a requested range, feature default, `latest`, or `lts`. Known runtime/tool keys above require numeric `MAJOR.MINOR.PATCH`, optionally followed by a `-` prerelease or `+` build suffix. Other tools use a literal release token matching the existing shell-safe option character set; reject moving selectors `latest`, `lts`, `stable`, and `nightly`. Tool-specific interpretation belongs to explicit artifact verification, not a generic semver assumption.
- Version, executable, and PATH entries are literals: reject placeholders and shell/environment expansion. A runtime option must not rewrite the declaration while leaving the image fixed. Start with a separate preset per fixed artifact; configurable image-family selection is outside this declaration design.
- Executable and PATH entries are clean absolute Linux paths without whitespace, `:`, control characters, or expansion markers. Executable names need not equal tool keys. Require the executable's parent directory in its `path` list. Symlinks and shim paths are allowed; the image author guarantees that they resolve for `vscode` without shell initialization. Static validation does not inspect the filesystem.
- Merge tool PATH entries by sorted tool key, preserving each tool's declared list order and deduplicating first occurrences. Final PATH order is the existing core AI/user entries, preinstalled tool entries, existing definition `container.path` entries in contribution order, then the existing system fallback directories. With declared tools, deduplicate the combined list without reordering; absent/empty tool maps preserve current PATH rendering byte-for-byte. Keep this in `containerEnv`, so non-login direct `docker exec` does not depend on image PATH or `remoteEnv`. Additional non-reserved environment still uses `container.env`; do not introduce a second environment map.
- Reject `opencode`, `claude`, and `codex` declarations: their binaries and state remain owned by the shared AI installer and host mounts, which could hide image-installed binaries. A declaration never changes mount or credential behavior.

#### Installation boundaries and conflicts

- Declaring `gh` suppresses only the core request for `ghcr.io/devcontainers/features/github-cli:1`. Absence retains that request. GitHub CLI remains required either through the core feature or the image contract; there is no opt-out without an equivalent. Static validation must expect the same choice as generation.
- Preinstalled runtime declarations do not remove arbitrary features. A shared-image preset omits the corresponding runtime installer feature. Reject a declaration combined with a known installer for the same capability anywhere in the selected preset/add-on contributions, even when its version matches: reinstalling defeats the contract and may change the artifact. Initial recognized repositories are `ghcr.io/devcontainers/features/{node,python,go,rust,github-cli}` and `ghcr.io/rails/devcontainer/features/ruby`; recognize repository identity independently of feature tag/digest. Diagnostics name the tool and the contributing definition/feature. Thus a stock `go` add-on conflicts with an image declaring `go`; use a consumption-only custom add-on or omit it.
- Unknown/custom features remain explicit installation contributions. Do not guess which tools an opaque feature or shell step installs. Image authors must remove redundant installers and supply appropriate project setup scripts. No feature-to-tool plugin registry or automatic rewriting of `root_run`, `user_run`, or setup shell code is introduced.
- `core_packages` asserts that each named Debian/Ubuntu package and its usable prerequisites already exist. Skip only those names from the **core** apt package set; omissions retain core installation. Package versions belong in the image recipe and consumed artifact digest, not this runtime release map. Reject unknown or duplicate core package names. Listing `gnupg` includes the working GnuPG commands needed by setup/credential forwarding; listing `ca-certificates` includes an initialized usable CA trust store.
- Preserve all definition `image.apt` groups, explicit `--system-package` requests, and root/user steps, even if they overlap a claimed core package. Emit the apt step only if core or project packages remain. The declaration does not suppress package-manager setup or application dependency installation (`npm ci`, `bundle install`, `uv sync`, etc.).
- `/bin/bash`, Debian/Ubuntu glibc compatibility, the existing `vscode` user, writable `/home/vscode`, and final `USER vscode` remain unconditional image requirements. The contract cannot change user/home, UID/GID policy, Compose, or directory ownership repair. Core package skipping must not be confused with safe support for populated home directories; Phase 2 still applies.

#### Validation and follow-through

Definition validation checks shape, ownership, literals, paths, versions, and reserved names; resolution checks installer conflicts and produces deterministic tool/PATH ordering. Independent failures should be aggregated with the source file and field or contributing definition. Missing capabilities in a real image cannot be diagnosed by parsing TOML.

Keep project version-file compatibility checks tied to reserved runtime identities rather than custom preset names or an assumed `options.version`. A fixed image's declared version is the comparison target. The later version-check item must implement that binding and the existing major/minor-versus-patch comparison conventions, without pretending that every Python requirement expression is an exact version. Selecting a different runtime requires selecting a different image/preset, not merely changing a runtime option.

Snapshot the declaration with its preset so ordinary checks/upgrades remain offline and retain the image contract. The schema evolution decision below retains manifest schema 2 without additional runtime fields; do not add a second independently editable runtime source of truth.

Later opt-in artifact verification must test declared executables, reported versions, and command lookup through non-login execution as `vscode`, as well as core prerequisites/user/home and effective inherited metadata. Custom tool keys have no inferred version-output parser; verification must report any unsupported version probe explicitly rather than claim full verification. A successful build or static check proves neither installed capabilities nor metadata safety.

Implementation tests for the later checklist items should cover absent/empty contracts preserving legacy output, partial/all core package claims, explicit project package preservation, missing/invalid tool fields, illegal add-on/variant declarations, known installer conflicts across definitions, custom tools, deterministic/deduplicated PATH, fixed-version consistency, snapshot round trips, and the `gh` feature present/absent cases. Keep these Docker-independent; actual artifact checks remain opt-in.

### Schema evolution contract (decisions completed)

This section completes the second Phase 1 item. It defines the implementation boundary for the following parsing/resolution item; it does not enable shared-image declarations in the current CLI.

#### Definition format

- Introduce **definition schema 2** for `image.preinstalled`, using exactly the declaration contract above. Files using the declaration must set `schema = 2`, including an explicitly empty `[image.preinstalled]` table. Schema 1 continues to reject the field rather than silently ignoring image claims.
- The implementation will read **definition schemas 1 and 2**, including mixed-schema preset/add-on selections. Schema 2 retains all existing fields and contribution semantics and adds only the optional top-level preset image contract. Add-ons and variants cannot declare it, even if empty. Unknown fields and unsupported schema numbers remain errors; do not infer a schema from field presence or fall back to schema 1.
- Absence of `image.preinstalled` in either supported schema means no installed-capability claims. In schema 2, an empty table or a contract whose lists/maps are all empty likewise makes no claims and preserves existing core apt installation, the GitHub CLI feature request, definition installers, and PATH rendering. An empty or omitted `core_packages` makes no package claims; an empty or omitted `tools` makes no tool claims, independently of the other member. Never infer installed capabilities from `image.base`, a preset name, or a definition's version.
- Represent presence with an optional `*Preinstalled` on `Image`, so validation can distinguish an absent contract from an empty declaration in forbidden schema/ownership positions. Its members and validation rules remain those in the declaration contract; this is not a new installation-policy enum or user option.
- Keep existing built-ins at `schema = 1` and retain their raw bytes and hashes. Do not mechanically bump their definition release `version` or schema. A definition author explicitly adopting schema 2 changes the raw snapshot/hash; the definition release version remains separate from the format version.
- Extend `schema/preset.schema.json` in the next item to accept both formats and enforce schema/ownership restrictions. The parser and editor schema must agree. Keep the currently published schema and parser at schema 1 until that coordinated implementation is ready; merely accepting schema 2 now would misleadingly advertise support.

#### Manifest and snapshots

- Keep **manifest schema 2** and its current JSON shape. Continue reading **manifest schemas 1 and 2**; no schema 3 or new manifest fields are needed for this declaration.
- The manifest's existing preset reference (`name`, definition release `version`, `source`, and `sha256`) pins the exact TOML snapshot, including its format number, base reference, and preinstalled contract. Do not copy installed tools, versions, paths, core-package claims, or the base reference into independently editable manifest fields. Do not add a definition-schema field to `Ref`: the pinned file already supplies it.
- Load and validate schema 2 manifests against their recorded snapshots offline. Ordinary `check` and `upgrade` retain schema 1 snapshots without converting, reserializing, or enriching them; future schema 2 snapshots must also retain their exact bytes and hashes. Regeneration still writes manifest schema 2 and preserves recorded options and selections.
- Manifest schema 1 retains its existing built-in conversion, including PostgreSQL 17 when no version was recorded. It does not gain preinstalled claims. Explicit `upgrade` still writes manifest schema 2 and snapshots; do not silently switch legacy projects to shared images.
- `upgrade --refresh-presets` remains the explicit way to adopt registry definitions and their image contracts. Validate the original manifest and snapshots before refresh, preserve recorded option values, and fill defaults only for newly added options as today. `init --force` retains its existing explicit replacement and add-on-option preservation semantics. No automatic migration, image lookup, or network resolution is added.
- Older projectsetup binaries may parse the unchanged manifest but must reject an unsupported schema 2 definition snapshot (or its unknown declaration fields). This fail-closed behavior is intentional: an older binary must not regenerate a shared-image project while discarding its installation contract. A manifest bump would duplicate the definition format gate without adding source data.

#### Other versioned formats and verification

- Remote indexes and `sources.toml` remain at their existing independent schema 1; their paths, source records, and digest pinning already accommodate either definition format. Neither is a definition schema or a manifest schema.
- `init --list-options --json` remains at **listing schema 2**. Installed-capability declarations are fixed snapshot data, not configurable options. Any additive declaration information exposed by the next listing implementation must be derived from the same typed definition and follow the listing's existing additive-field policy; do not synthesize runtime options or duplicate editable state.
- Existing tests cover strict schema/unknown-field rejection, manifest schema 1 conversion and schema 2 round trips, snapshot hash/version checks, and legacy golden output. Run them for this decision step. The following implementation item must add dual-definition-schema acceptance/rejection, absent/empty-contract compatibility, mixed selections, and schema 2 snapshot/manifest round trips before claiming parser support. Subsequent generation items must demonstrate byte-identical legacy output and the declared installation choices.

## Phase 2: Explicit ownership policy

Coordinate this phase with Phase 1 before claiming support for populated shared home directories.

- [ ] Define an explicit UID/GID policy for image authors and consumers while retaining `vscode` as the username.
- [ ] Support a fixed-ID image contract and explicitly render `updateRemoteUserUID: false` when that policy is selected.
- [ ] Validate host/image ID compatibility where it matters on Linux, including workspace and AI bind-mount writability. Define macOS and other supported-host behavior separately from Linux host IDs.
- [ ] Keep portable/default behavior available; do not hardcode `1001:1001` globally or disable UID adjustment for all projects.
- [ ] Establish configured IDs before installing user-owned toolchains in shared-image recipes. Handle UID/GID collisions with actionable errors rather than deleting unrelated accounts.
- [ ] Prefer root-owned, readable runtime installations outside the home directory where practical.
- [ ] Replace broad recursive ownership repair in the generated Dockerfile and AI installer with targeted creation/parent repair and writable-mount checks.
- [ ] Never recursively chown mounted host credentials, history, caches, or a parent spanning those mounts. Report which host source needs repair when a mount is unwritable.

Acceptance: a matching fixed-ID image avoids toolchain-copying UID-adjustment layers; mismatched IDs produce actionable diagnostics; first-run AI setup still works without recursively modifying host state.

## Phase 3: Reference recipes and the user's image family

- [ ] Provide a small documented reference recipe/workflow, using custom definitions for the user's CLI bundles and variants.
- [ ] Verify chosen runtime releases, base-image references, architecture support, and feature installer pins before building.
- [ ] Build the foundation once and derive useful variants from the actual shared parent artifacts.
- [ ] Install common CLIs once; keep expensive specialist dependencies scoped to their consumers.
- [ ] Keep compiler/package caches out of final layers. Clean temporary build data in the same layer, or use suitable cache mounts/multi-stage builds.
- [ ] Ensure image metadata excludes project-specific mounts, credentials, lifecycle scripts, and dependency installation.
- [ ] Record immutable references and demonstrate consuming presets with appropriate dependency setup.
- [ ] Document explicit image updates, definition refresh, configuration regeneration, and container recreation. Plain `upgrade` must not silently move projects to new artifacts.
- [ ] Measure shared layers and UID-adjustment behavior using two representative projects. Report image savings separately from writable container/volume usage.

Acceptance: a reusable, versioned family can be built locally or published to the user's registry and consumed by ordinary projectsetup presets. Maintenance of an official published projectsetup catalog is a later decision, not a prerequisite.

## Phase 4: Image build/export workflow, if needed

After consumption works, determine whether separately maintained recipes are causing duplication or drift. If justified, add an explicit image-build/export workflow that reuses definition installation contributions.

- [ ] Separate image-build inputs from project initialization inputs in the typed model.
- [ ] Reuse runtime/tool installation definitions while excluding sidecars, workspace mounts, AI credentials/state, and application dependency setup.
- [ ] Keep image output deterministic and feature locks associated with the image recipe.
- [ ] Record provenance: contributing definitions/options, runtime versions, architecture, user IDs, and resulting artifact references.
- [ ] Support explicit local builds; make any publishing explicit. Avoid automatic remote builds or a hidden combinatorial image catalog.

Image build/export command names and TOML field names are intentionally undecided. Phase 1's preinstalled declaration fields are designed above, but must be documented as actual supported syntax only after implementation.

## Phase 5: Exact PostgreSQL references

This is useful adjacent work, not required for shared app-toolchain images.

- [ ] Extend the built-in add-on to support exact patch tags and optional digests while retaining an explicit major-version model for data compatibility and mount-layout selection.
- [ ] Validate image references through a dedicated typed field/validator. Do not relax the shell-safe character restriction for every generic option just to admit `/`, `:`, and `@`.
- [ ] Preserve existing major-only manifests and `init --force`/`upgrade` version preservation.
- [ ] Test PostgreSQL before/after 18 mount layouts, valid patch/digest references, and inconsistent version/reference declarations.
- [ ] Document major-version migration as a separate dump/restore or pg_upgrade operation. Configuration regeneration must not migrate or delete database volumes.

## Verification strategy

Follow the repository's table-driven, golden, and opt-in integration testing conventions.

- Unit tests: declaration parsing, legacy compatibility, normalization, image/runtime inconsistency, UID/GID policy, and actionable validation failures.
- Golden tests: traditional presets, shared-image presets, project-specific additions, fixed-ID policy, deterministic output, definition snapshots, and executable script modes.
- Installer tests: targeted parent creation, unwritable mounts, and nested host mounts are handled without recursive ownership changes; preserve AI tool installation/update behavior.
- Docker-independent static checks remain in the normal test suite.
- Opt-in integration checks: effective Dev Container metadata, Compose configuration, artifact user/home/IDs, actual runtime versions and PATH through non-login execution, and first-run writable mounts.
- Layer-sharing check: two project images share the intended parent layers; a matching fixed-ID variant does not acquire a large home-copying UID layer. Record measurements without hardcoding server-specific savings.

For each implementation phase, run:

```bash
gofmt -w <changed-go-files>
go test ./...
go vet ./...
```

When fixtures or integration behavior change, run applicable `docker compose config`, `devcontainer read-configuration`, and opt-in build/smoke checks from `HANDOFF.md` when the tools are available. Report unavailable checks explicitly.

## Scope boundaries

This roadmap covers shared-image support, associated ownership behavior, reusable image recipes, and optional PostgreSQL pinning improvements. Cleanup of the remote server's temporary files, removal of Docker resources, application runtime upgrades, database migration, and temporary-file retention policies are separate tasks requiring their own explicit instructions.

## Starting a future session

Suggested prompt:

> Read AGENTS.md, HANDOFF.md, and SHARED_IMAGES_ROADMAP.md. Reconfirm the current implementation, then implement Phase 1 together with the Phase 2 ownership prerequisites needed for safe consumption of populated shared images. Preserve existing presets and upgrade behavior, add the relevant tests and documentation, and run the required checks. Identify concrete schema/API decisions before implementing them; keep later phases separate unless needed for a working end-to-end example.
