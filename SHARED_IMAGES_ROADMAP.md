# Shared Toolchain Images: Implementation Roadmap

Status: Phase 1 implementation and digest-pinned end-to-end consumption documentation completed. Ownership policy, measured two-project shared-layer acceptance, and the explicit host-integrations extension remain pending. Schedule host integrations alongside or immediately after Phase 2.

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
- Generated Dev Container configuration requests the GitHub CLI feature unless the preset declares `gh`, and includes declared tool paths in `containerEnv.PATH` independently of image PATH.
- `internal/generate/templates/Dockerfile.tmpl` installs only undeclared core apt packages, preserves explicit project packages, and omits apt entirely when no packages remain. It still recursively chowns `/home/vscode/.local`.
- `internal/generate/templates/install-ai-tools.sh` can recursively chown unwritable directories, including a parent containing mounted host state. Codex-specific paths already use a non-recursive, fail-with-guidance policy.
- There is no explicit generated UID/GID policy or `updateRemoteUserUID` setting.
- The built-in PostgreSQL add-on accepts a major version only. Full references cannot be generic option values under the current shell-safe option character restriction.
- Static validation checks declared/generated configuration; `check --build` also inspects base/final effective metadata for nonempty preinstalled claims, but does not run runtime smoke checks.
- `check --runtime` explicitly builds and inspects metadata, then executes isolated built-artifact capability probes as `vscode` with generated containerEnv. Unsupported custom version probes fail explicitly; no static/build-only success is presented as runtime proof.

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
- [x] Extend parsing, registry resolution, normalization, editor schema, and options listing together. Definition schemas 1/2, literal/ownership validation, selected installer conflicts, deterministic separate preinstalled PATH data, and additive listing metadata are implemented. Mixed selections, schema 2 snapshot/manifest round trips, hash rejection, and absent/empty legacy rendering are tested.
- [x] Generate project Dockerfiles and Dev Container configuration without requesting installation of capabilities explicitly provided by the image. Declared `gh` suppresses the core GitHub CLI feature; consumption-only presets omit runtime installers and resolution rejects known conflicts. Declared tool paths precede definition paths with deterministic first-occurrence deduplication. Static checks use the same expected feature/PATH model and reject reintroduced installers (including tags/digests) and PATH drift. Dockerfiles preserve the selected base, project apt packages, and root/user steps; core apt suppression is implemented in the separate item below. Shared-image golden output, executable modes, offline snapshot regeneration, valid/failure cases, and absent/empty legacy rendering are covered.
- [x] Avoid redundant core apt installation for images that declare the required prerequisites. Preserve project-specific packages and root/user steps. Generation filters only the core set in historical order and omits the whole apt step when empty; definition groups and explicit system packages remain even when they overlap claims. Static checks accept declared bash and require undeclared core/explicit project packages. Partial/all claims, absent/empty legacy output, ordered root/user steps, add-on/variant contributions, missing-package failures, shared-image golden modes, and offline regeneration are covered.
- [x] Account for Dev Container metadata inherited from prebuilt images. `check --build` preflights the locally available base artifact and rejects inherited lifecycle/project/credential mounts (including Docker VOLUME), malformed metadata, conflicting effective environment/users, and disabled inherited UID adjustment before building. Baked feature IDs remain descriptive records, not installer requests or inferred capability claims. After building, stopped probes inspect the final image user/HOME and CLI-merged installation requests, environment, mounts, and exactly one project post-create command. Probes never start or execute setup and are removed on success/failure. Ordinary checks warn when shared metadata remains uninspected; absent/empty contracts preserve legacy output/build behavior. See [Metadata consumption contract](#metadata-consumption-contract-implemented).
- [x] Preserve runtime version-file checks for custom shared-image presets. Explicit Node/Ruby/Python pins bind to declared identities independently of preset names/options/detection signals; every present pin is checked. Nonempty claims require a literal base. Conventional runtime options are compatibility constraints checked during resolution/normalization, not image selectors; ambiguous generic version options fail. See [Fixed-runtime consistency contract](#fixed-runtime-consistency-contract-implemented).
- [x] Add a separate opt-in runtime verification path, or extend an existing explicit integration path, to inspect the built artifact's capabilities. `check --runtime` implies building and successful metadata inspection, then checks core prerequisites/user/home, declared executable identity and PATH, and supported exact releases through non-login direct execution as `vscode`. Unsupported custom version probes fail explicitly; build-only checks warn that capabilities remain unverified. See [Runtime verification contract](#runtime-verification-contract-implemented).
- [x] Document literal digest-pinned image references and an end-to-end custom preset example. See [Consume a digest-pinned shared toolchain](docs/shared-images.md) for an explicitly illustrative Node/npm/gh preset, two distinct project-package consumers, metadata/runtime verification, offline regeneration, and deliberate artifact refresh/recreation. Artifact values are not verified releases or a published image; ownership and measured shared-layer acceptance remain separate pending work.

Acceptance: two projects can derive from the same shared image, run their expected tools, and add distinct project packages without rebuilding the baked runtimes. Existing presets still generate and validate correctly.

Parsing-item verification: changed Go files were formatted and `go test ./...` and `go vet ./...` passed using `mise exec go@1.27.2` (the default Go shim was unset). Existing golden fixtures also passed `docker compose config` and representative `devcontainer read-configuration` checks. This item changes no generated fixtures or container installation behavior; opt-in build/smoke and shared-artifact verification remain outside this step.

Tool-consumption verification: changed Go files were formatted; `go test ./...`, `go vet ./...`, all golden `docker compose config` checks, and representative `devcontainer read-configuration` checks (including the new shared-image fixture) passed using `mise exec go@1.27.2`. Existing golden trees were unchanged. The shared fixture uses illustrative image/opaque-feature references for configuration testing; it is not a buildable or verified artifact. Opt-in builds/smoke tests were not run for this configuration-only item; actual shared-artifact verification remains the separate checklist item above.

Core-apt verification: changed Go files were formatted; `go test ./...` and `go vet ./...` passed using `mise exec go@1.27.2`. All 15 golden `docker compose config` checks and all eight representative `devcontainer read-configuration` checks passed, including shared-node. Existing preset golden trees remain byte-identical; only shared-node's redundant core apt line changed. Tests cover partial/all claims, no apt work when no packages remain, overlapping preset/add-on/variant/flag packages, root/user ordering, and missing undeclared/explicit packages. Existing parser rejection tests for unknown/duplicate claims also pass. Opt-in builds/smoke tests were not run: the changed shared fixture uses illustrative references and is not buildable; existing buildable fixtures are unchanged. Actual artifact capability verification remains pending.

Metadata verification: changed Go files were formatted; `go test ./...` and `go vet ./...` passed using `mise exec go@1.27.2`, including all 15 golden Compose checks and eight representative read-configuration checks. Every golden tree remains byte-identical. Docker-independent tests cover object/array/absent/empty/malformed metadata, baked and opaque feature IDs, string/argv/parallel lifecycle failures, inherited mounts, environment/user/UID-policy failures, AI mount interpolation, legacy/empty-contract gating, both CLI build-result forms, external failure gating, and stopped-probe cleanup failures. The opt-in `TestSharedImageMetadataIntegration` passed with `PROJECTSETUP_METADATA_BASE=ps-lang-node-app:latest`: a local test artifact with unsafe hooks/mounts fails before building; repaired toolchain-only metadata builds and merges without fetching its intentionally invalid baked feature installer tag, preserves image ENV, and never executes project setup. This host's Docker Compose/buildx required the invocation-only `BUILDX_BAKE_ENTITLEMENTS_FS=0` setting to read the CLI-generated Dockerfile outside the build context. Test images/probes were cleaned up. Broad preset build/first-run smoke tests were not rerun because generated installation behavior is unchanged; executable/version/ownership verification remains pending and no runtime release claim was verified by this metadata test.

Fixed-runtime verification: changed Go files were formatted; `go test ./...` and `go vet ./...` passed using `mise exec go@1.27.2`. All 15 golden `docker compose config` checks and eight representative `devcontainer read-configuration` checks passed; golden trees remain byte-identical. Docker-independent tests cover optionless/signal-free custom presets checked from snapshots, exact/major/minor/prefix/prerelease pins, patch and moving-selector failures, contradictory pins and aggregation, staged replacement failure preserving the original manifest, requirement expressions, all five runtime identities' option constraints, mismatched defaults, ambiguous multi-runtime options, configurable-base rejection, and absent/empty/core-only legacy behavior. Opt-in builds/smoke checks were not run because generated installation and integration behavior are unchanged. Actual artifact runtime verification remains the next incomplete item.

Runtime verification (final recovery review): reviewed the interrupted changes against declaration, metadata, fixed-runtime, offline, and legacy contracts before marking this item complete. Added disabled inherited Docker healthchecks, actual CA certificate parsing (nonempty garbage is rejected), explicit missing-Docker gating, and stronger final-metadata/failure aggregation/CLI/static/legacy coverage. Changed Go files were formatted; `mise exec go@1.27.2 -- go test ./...`, `mise exec go@1.27.2 -- go vet ./...`, and `git diff --check` passed. All 15 golden trees remain byte-identical with executable modes preserved; all 15 golden Compose checks and eight representative read-configuration checks passed. The focused metadata and runtime integrations passed against the available local `ps-lang-node-app:latest` artifact with invocation-only `BUILDX_BAKE_ENTITLEMENTS_FS=0`. Eight runtime scenarios cover real Node/GitHub CLI releases, wrong release, missing executable, PATH shadowing, unsupported custom probe, missing CA bundle, corrupt nonempty CA bundle, and unwritable home. Actual probe inspection confirms vscode/home, no mounts/network, and disabled inherited healthchecks; manifests and definition snapshots remain byte-identical. Tests cover create/start/exec/metadata failures, owned cleanup, and cleanup-error guidance, and confirm static/build-only checks never execute runtime probes. No test probe containers or tagged test images remain. Other supported version-output parsers are unit-tested; broad preset build/first-run smoke tests were not rerun because generation and lifecycle behavior are unchanged. No required checks were unavailable. The digest-pinned end-to-end documentation item is next; ownership policy and two-project shared-layer acceptance remain separate pending work.

### Declaration contract (design completed)

Documentation verification: the exact TOML in [the digest-pinned consumption walkthrough](docs/shared-images.md) passed `preset validate` and generated two consumers with distinct `jq`/`ripgrep` project packages. Both passed static checks, ordinary upgrade with the user definition temporarily absent (snapshot bytes preserved), explicit refresh to a changed illustrative digest/definition release, `docker compose config`, and `devcontainer read-configuration`. `mise exec go@1.27.2 -- go test ./...`, `mise exec go@1.27.2 -- go vet ./...`, and `git diff --check` passed. No Go files or golden fixtures changed. No build/runtime checks were run for this documentation-only chunk because its artifact is explicitly illustrative and unavailable; prior real runtime integration evidence remains recorded above. This validation proves the documented configuration workflow, not actual digest availability, releases, ownership compatibility, or shared-layer savings.

This section specifies the Phase 1 declaration contract. Its syntax is accepted by the parser and editor schema in definition schema 2; schema 1 rejects these fields. Parsing, resolution, normalization, snapshots, listings, generated tool/feature/PATH consumption, core apt suppression, explicit effective metadata inspection, runtime version-file binding, and opt-in executable artifact verification are implemented.

#### Shape and ownership

Add an optional `image.preinstalled` contract to the selected preset's **top-level image only**. Like `image.base`, it cannot be contributed by add-ons or variants: it describes the selected base artifact, not something a later project layer installs. An absent contract means existing installation behavior. Empty lists/maps make no claims.

Declaration fields (illustrative values, not verified image releases or references):

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

Project version-file compatibility checks are tied to reserved runtime identities rather than custom preset names or an assumed `options.version`. A fixed image's declared version is the comparison target, using the existing major/minor-versus-patch conventions without interpreting Python requirement expressions as exact versions. Selecting a different runtime requires selecting a different image/preset, not merely changing a runtime option.

Snapshot the declaration with its preset so ordinary checks/upgrades remain offline and retain the image contract. The schema evolution decision below retains manifest schema 2 without additional runtime fields; do not add a second independently editable runtime source of truth.

Opt-in artifact verification tests declared executables, reported versions, and command lookup through non-login execution as `vscode`, as well as core prerequisites/user/home and effective inherited metadata. Custom tool keys have no inferred version-output parser; verification reports unsupported version probes as errors rather than claiming full verification. A successful build or static check proves neither installed capabilities nor metadata safety.

Implementation tests for the later checklist items should cover absent/empty contracts preserving legacy output, partial/all core package claims, explicit project package preservation, missing/invalid tool fields, illegal add-on/variant declarations, known installer conflicts across definitions, custom tools, deterministic/deduplicated PATH, fixed-version consistency, snapshot round trips, and the `gh` feature present/absent cases. Keep these Docker-independent; actual artifact checks remain opt-in.

### Runtime verification contract (implemented)

- `check --runtime` implies static/external validation, a real Dev Container build, and successful existing base/final metadata inspection. Requires nonempty tool/core claims; absent/empty contracts retain historical static/build behavior and are rejected only when runtime verification is explicitly requested. No schema, snapshots, generated output, migration, or implicit artifact lookup changes.
- Only after metadata checks succeed, create an owned network-disabled, mount-free container from the built image with generated `containerEnv`, user `vscode`, workdir `/home/vscode`, disabled inherited healthchecks, and an overridden `/bin/sleep` entrypoint. Probe via non-login direct `docker exec --user vscode`; inspection shells use `bash --noprofile --norc` with BASH_ENV/ENV cleared, while direct tool probes retain the generated/image environment. Never start Compose services or run Dev Container lifecycle/application/AI setup. Reject final Docker VOLUME declarations before creating a probe.
- Check Debian/Ubuntu identity, glibc, executable bash, non-root vscode identity, passwd/environment home agreement, and real home temporary-file creation/writing. Check all six core Debian packages (whether claimed or installed by the project), a nonempty OpenSSL-parseable CA bundle, and curl/Git/gpg/gpg-agent/sudo commands. This is offline trust-store initialization evidence, not a remote TLS connection test.
- For every declared tool, require an executable file and PATH lookup resolving to the same file, permitting symlinks but rejecting shadowing. Run the absolute executable and lookup command, parse tool-specific version output, and compare the exact literal release. Supported identities: node, ruby, python, go, rust, gh, npm, pnpm, yarn, bundler, pip, uv, poetry, cargo. Unsupported custom version parsers and unrecognized output are errors, never warnings or partial success.
- Aggregate independent failures. Remove only owned probes with forced volume-aware cleanup after start/exec failures and success; cleanup failures identify the resource and repair command. Builds retain images/cache. Metadata, build, and static failures gate runtime execution. Build-only shared checks explicitly warn that executable capabilities remain unverified.
- This verifies capabilities of the built artifact; it does not verify dependencies, first-run setup, mounted host state, adjusted UID/GID compatibility, or shared-layer reuse. Phase 2 ownership and the later two-project acceptance demonstration remain pending.

### Fixed-runtime consistency contract (implemented)

- Nonempty tool/core-package claims require a literal base reference. Reject `$`/backtick expansion, including option/project placeholders. Absent/empty contracts retain historical base expansion. Configurable image-family selection remains outside Phase 1: separate presets pin separate artifacts, and explicit refresh/reselection adopts changes.
- Check `.node-version` and `.nvmrc` for declared `node`, `.ruby-version` for `ruby`, and `.python-version` for `python`. Use every present, nonempty file, with existing first-word/prefix cleanup and release-line/patch comparison conventions. Fixed claims are checked even without detection signals, matching preset names, or runtime options. Diagnostics identify the file, runtime, preset, and fixed declaration and advise compatible image selection or pin correction. Independent failures are aggregated; staged generation uses the same checks.
- Optional preset `TOOL_version` options constrain the corresponding declared runtime (`node`, `ruby`, `python`, `go`, `rust`). An optional `version` constrains a single declared runtime, but is rejected as ambiguous for multiple declared runtimes. Effective defaults and explicit values must agree with the fixed declaration during resolution/normalization. Prefer no runtime options; compatible constraints never select an image or reinstall runtimes. Other option names retain their existing semantics; there is no heuristic binding of arbitrary option names.
- For these fixed-runtime options, generic detection-source comparisons yield to explicit version-file checks. In particular a regex-extracted Python requirement lower bound is not an exact release pin. Requirement-range solving, new Go/Rust version-file formats, and executable verification are not part of this item. Existing option/lockfile/suggestion checks remain for absent/empty contracts and unrelated options.
- Manifest schema 2 and snapshot bytes/hashes remain the source of truth. No additional editable runtime state, built-in changes, generated fixture changes, image lookup, or implicit migration is introduced.

### Metadata consumption contract (implemented)

- Inspection applies to nonempty installed tool/core-package claims, not merely definition schema 2 or an empty table. No schema/manifest fields, built-in snapshots, generated files, or automatic image migrations change.
- Initialization/static checks stay offline and never inspect/pull images. Ordinary external `check` warns that shared metadata is uninspected. Run `check --build` before container startup. The base must already be locally available; missing images/daemon failures produce actionable diagnostics to pull/build locally and retry.
- Inspect `devcontainer.metadata` as an array of objects or its supported legacy single-object form. Permit baked feature IDs, customizations, and compatible tool environment/user metadata. Do not treat IDs as installer requests, installation proof, or automatic suppression rules. Reject malformed/null metadata and nonempty inherited lifecycle commands or mounts; lifecycle overrides in the project cannot remove additive inherited hooks. Reject Docker VOLUME declarations before creating a probe.
- Use `devcontainer read-configuration --container-id ... --include-merged-configuration` against a newly created, stopped probe to inspect actual CLI merge behavior. Base probes merge current project configuration; final-image probes carry the project identity labels used by `up` so its recorded project commands are not appended twice. Validate generated feature requests, containerEnv/PATH, reserved remoteEnv, vscode users/home, mounts, the default UID adjustment policy, and exactly one generated post-create hook. Other nonempty hooks, including hooks contributed by explicit features, fail closed. Final image inspection additionally checks Docker's effective user and an explicitly configured HOME.
- Do not start probes, run lifecycle scripts, mount host credentials/workspaces, or modify existing containers/images. Remove only each created probe and its anonymous volumes on every subsequent success/failure; cleanup failures identify the owned resource and repair command. Builds retain their normal image/cache output.
- A consuming Dockerfile's label reset is **not** sufficient: the CLI inspects the FROM image before building and may re-emit its metadata. Repair the separately built toolchain artifact's recipe/label, retaining required tool environment in image ENV or preset contributions. No hidden metadata sanitization or template/installer rewriting is added.
- Metadata checks prove configuration isolation, not executable versions, filesystem capabilities, writable homes/mounts, glibc compatibility, or UID/GID compatibility. Those remain the later verification/ownership items.

### Schema evolution contract (decisions completed)

This section records the second Phase 1 item's schema decisions. Parsing/resolution, generated tool consumption, and core apt suppression implement these format gates and compatibility rules.

#### Definition format

- Introduce **definition schema 2** for `image.preinstalled`, using exactly the declaration contract above. Files using the declaration must set `schema = 2`, including an explicitly empty `[image.preinstalled]` table. Schema 1 continues to reject the field rather than silently ignoring image claims.
- The implementation will read **definition schemas 1 and 2**, including mixed-schema preset/add-on selections. Schema 2 retains all existing fields and contribution semantics and adds only the optional top-level preset image contract. Add-ons and variants cannot declare it, even if empty. Unknown fields and unsupported schema numbers remain errors; do not infer a schema from field presence or fall back to schema 1.
- Absence of `image.preinstalled` in either supported schema means no installed-capability claims. In schema 2, an empty table or a contract whose lists/maps are all empty likewise makes no claims and preserves existing core apt installation, the GitHub CLI feature request, definition installers, and PATH rendering. An empty or omitted `core_packages` makes no package claims; an empty or omitted `tools` makes no tool claims, independently of the other member. Never infer installed capabilities from `image.base`, a preset name, or a definition's version.
- Represent presence with an optional `*Preinstalled` on `Image`, so validation can distinguish an absent contract from an empty declaration in forbidden schema/ownership positions. Its members and validation rules remain those in the declaration contract; this is not a new installation-policy enum or user option.
- Keep existing built-ins at `schema = 1` and retain their raw bytes and hashes. Do not mechanically bump their definition release `version` or schema. A definition author explicitly adopting schema 2 changes the raw snapshot/hash; the definition release version remains separate from the format version.
- `schema/preset.schema.json` and the parser now accept both formats and enforce schema/ownership restrictions together. The editor schema also describes tool fields, reserved names, concrete versions, and unique core packages; `preset validate` additionally checks path cleanliness, executable parent membership, and selected installer conflicts. Acceptance is parsing support, not proof of artifact capabilities or completed generation consumption.

#### Manifest and snapshots

- Keep **manifest schema 2** and its current JSON shape. Continue reading **manifest schemas 1 and 2**; no schema 3 or new manifest fields are needed for this declaration.
- The manifest's existing preset reference (`name`, definition release `version`, `source`, and `sha256`) pins the exact TOML snapshot, including its format number, base reference, and preinstalled contract. Do not copy installed tools, versions, paths, core-package claims, or the base reference into independently editable manifest fields. Do not add a definition-schema field to `Ref`: the pinned file already supplies it.
- Load and validate schema 2 manifests against their recorded snapshots offline. Ordinary `check` and `upgrade` retain schema 1 snapshots without converting, reserializing, or enriching them; future schema 2 snapshots must also retain their exact bytes and hashes. Regeneration still writes manifest schema 2 and preserves recorded options and selections.
- Manifest schema 1 retains its existing built-in conversion, including PostgreSQL 17 when no version was recorded. It does not gain preinstalled claims. Explicit `upgrade` still writes manifest schema 2 and snapshots; do not silently switch legacy projects to shared images.
- `upgrade --refresh-presets` remains the explicit way to adopt registry definitions and their image contracts. Validate the original manifest and snapshots before refresh, preserve recorded option values, and fill defaults only for newly added options as today. `init --force` retains its existing explicit replacement and add-on-option preservation semantics. No automatic migration, image lookup, or network resolution is added.
- Older projectsetup binaries may parse the unchanged manifest but must reject an unsupported schema 2 definition snapshot (or its unknown declaration fields). This fail-closed behavior is intentional: an older binary must not regenerate a shared-image project while discarding its installation contract. A manifest bump would duplicate the definition format gate without adding source data.

#### Other versioned formats and verification

- Remote indexes and `sources.toml` remain at their existing independent schema 1; their paths, source records, and digest pinning already accommodate either definition format. Neither is a definition schema or a manifest schema.
- `init --list-options --json` remains at **listing schema 2**. Installed-capability declarations are fixed snapshot data, not configurable options. Listings now expose additive `definitionSchema` and optional `preinstalled` metadata derived from the typed definition (`corePackages` and `tools`); text options listings label them as fixed declarations. No synthesized runtime options or independently editable manifest state are added.
- Tests cover strict schema/unknown-field rejection, manifest schema 1 conversion, snapshot hash/version checks, and legacy golden output. Parsing implementation adds dual-definition-schema acceptance/rejection, empty-contract ownership gates, parser/editor agreement, literal/path/version failures, mixed selections, deterministic preinstalled PATH data, installer conflicts (including tags/digests and matching variants), schema 2 snapshot/manifest round trips, and absent/empty-contract rendering compatibility. Subsequent generation items must test the declared installation choices and final combined PATH.

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

## Extension alongside or after Phase 2: Explicit host integrations

Support selected project-specific host integrations through typed preset/add-on declarations. The first concrete use case is forwarding a Linux Wayland socket into the app container. This is a planned extension, not existing host-mount support, and is independent of the shared toolchain artifact's contents.

Current definitions already support non-reserved environment variables through `[container.env]`, and their snapshots preserve those contributions through `upgrade`. They cannot declare host mounts. Ordinary static checks may accept a manually added mount, but regeneration discards it; that acceptance does not imply persistence or complete source/access validation. `HANDOFF.md` deliberately reserves host mounts to the core, so implementing this extension requires an explicit revision of that contract.

- [ ] Design a constrained typed bind-mount declaration for presets/add-ons, with explicit source, absolute container target, and read-only/read-write behavior. Decide definition/editor schema evolution and compatibility before implementation; do not introduce arbitrary JSON/Compose fragments or a generic template engine.
- [ ] Support explicit host-environment references in mount sources, including `XDG_RUNTIME_DIR` and `WAYLAND_DISPLAY`. Define missing-variable behavior and any supported defaults without shell evaluation or implicit host discovery. Reuse existing `[container.env]` for container variables; keep runtime environment available to non-login `dworm exec`, not only editor `remoteEnv`.
- [ ] Normalize mount contributions with deterministic ordering, reject conflicting/overlapping targets where they would hide core-managed paths, and preserve ownership of workspace/AI mounts and the prohibition on host `.ssh`/`.gitconfig` mounts.
- [ ] Snapshot the declarations and preserve them through ordinary `upgrade` and regeneration. Retain host-variable references rather than persisting machine-specific resolved paths. Keep generation and static checks offline, with existing presets and absent host-integration declarations retaining their current behavior.
- [ ] Validate selected integrations with actionable errors for missing required host variables/sources, incorrect source kinds, and invalid targets. Distinguish directory, file, and Unix-socket sources; never auto-create a directory in place of a missing file/socket or recursively change host ownership.
- [ ] Integrate source/access diagnostics with Phase 2's user-ID and bind-mount policy, including Wayland socket access as `vscode` on Linux. Define unsupported-host behavior explicitly; integrations remain opt-in and do not change portable defaults.
- [ ] Extend generation and effective metadata validation from the same typed mount model. Accept explicitly declared project mounts alongside core-managed mounts while retaining rejection of unexpected inherited mounts/hooks. Keep built-artifact runtime probes mount-free and separate from host-integration verification.
- [ ] Add Docker-independent parsing, normalization, conflict, missing-variable/source, source-kind, snapshot/upgrade, and legacy tests, plus golden output and an opt-in Wayland connection check. Document selection, required host environment, regeneration, and the temporary manual-mount workaround.

First acceptance example: a selected Wayland integration generates a bind mount from `${localEnv:XDG_RUNTIME_DIR}/${localEnv:WAYLAND_DISPLAY}` to `/run/host-wayland/wayland-0` and sets `containerEnv.WAYLAND_DISPLAY` to `/run/host-wayland/wayland-0`. The configuration survives `projectsetup upgrade` without manual reapplication; on a compatible Linux host, a test client connects through non-login execution as `vscode`. Missing variables/socket or insufficient access produce useful diagnostics. No display server or application starts automatically, and existing AI mounts and `dworm` credential forwarding continue to work.

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

Image build/export command names and TOML field names are intentionally undecided. Phase 1's preinstalled declaration fields are implemented and documented above; they describe artifact consumption, not a build/export command.

## Phase 5: Exact PostgreSQL references

This is useful adjacent work, not required for shared app-toolchain images.

- [ ] Extend the built-in add-on to support exact patch tags and optional digests while retaining an explicit major-version model for data compatibility and mount-layout selection.
- [ ] Validate image references through a dedicated typed field/validator. Do not relax the shell-safe character restriction for every generic option just to admit `/`, `:`, and `@`.
- [ ] Preserve existing major-only manifests and `init --force`/`upgrade` version preservation.
- [ ] Test PostgreSQL before/after 18 mount layouts, valid patch/digest references, and inconsistent version/reference declarations.
- [ ] Document major-version migration as a separate dump/restore or pg_upgrade operation. Configuration regeneration must not migrate or delete database volumes.

## Verification strategy

Follow the repository's table-driven, golden, and opt-in integration testing conventions.

- Unit tests: declaration parsing, legacy compatibility, normalization, image/runtime inconsistency, UID/GID policy, explicit host-mount/environment validation, and actionable validation failures.
- Golden tests: traditional presets, shared-image presets, project-specific additions, fixed-ID policy, opt-in host integrations, deterministic output, definition snapshots, and executable script modes.
- Installer tests: targeted parent creation, unwritable mounts, and nested host mounts are handled without recursive ownership changes; preserve AI tool installation/update behavior.
- Docker-independent static checks remain in the normal test suite.
- Opt-in integration checks: effective Dev Container metadata, Compose configuration, artifact user/home/IDs, actual runtime versions and PATH through non-login execution, first-run writable mounts, and explicitly selected host integrations such as a Wayland socket connection.
- Layer-sharing check: two project images share the intended parent layers; a matching fixed-ID variant does not acquire a large home-copying UID layer. Record measurements without hardcoding server-specific savings.

For each implementation phase, run:

```bash
gofmt -w <changed-go-files>
go test ./...
go vet ./...
```

When fixtures or integration behavior change, run applicable `docker compose config`, `devcontainer read-configuration`, and opt-in build/smoke checks from `HANDOFF.md` when the tools are available. Report unavailable checks explicitly.

## Scope boundaries

This roadmap covers shared-image support, associated ownership behavior, explicit opt-in host integrations, reusable image recipes, and optional PostgreSQL pinning improvements. Cleanup of the remote server's temporary files, removal of Docker resources, application runtime upgrades, database migration, and temporary-file retention policies are separate tasks requiring their own explicit instructions.

## Starting a future session

Suggested prompt:

> Read AGENTS.md, HANDOFF.md, and SHARED_IMAGES_ROADMAP.md. Reconfirm the current implementation, then implement Phase 1 together with the Phase 2 ownership prerequisites needed for safe consumption of populated shared images. Preserve existing presets and upgrade behavior, add the relevant tests and documentation, and run the required checks. Identify concrete schema/API decisions before implementing them; keep later phases separate unless needed for a working end-to-end example.
