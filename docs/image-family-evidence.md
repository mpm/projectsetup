# Reference family acceptance, 2026-10-11

This is a local measurement on this machine, using Docker 29.9.0, Compose 5.6.0, Dev Container CLI 0.89.0 and linux/amd64. The run occurred on 2026-10-11 Europe/Berlin (2026-10-10 UTC). [Machine-readable evidence](../examples/shared-images/evidence-2026-10-11.json) records full immutable IDs, layer diff IDs, runtime output, and separate container measurements. This does not describe the other server that motivated the roadmap.

The [workflow](image-family.md) built the foundation once, then Node from that exact artifact, then native Node from that exact Node artifact. The final run verifies root ownership throughout `/opt/node` and of `/usr/local/bin/gh`; Node extraction explicitly discards upstream archive ownership with `--no-same-owner`. No images were published and no existing resources were modified or removed.

| Artifact | Local content ID |
| --- | --- |
| Foundation | `sha256:61e40924be95ef8c41b2b3905446aadf9283c5ecf7facc32f6d1e02ef5fe6f1a` |
| Node | `sha256:dba1a8ffea93d401d4b7eb3c9faa9bda2c6a687edd2172b72ef4e78f6c0aecbb` |
| Node + native | `sha256:7cdf57b7e7cc7e247807b4a941cf46aaf3d9ba7ceacfb39c17a3b97f60c41f91` |
| Project A, jq | `sha256:a4ec998144d1deb213f73e65efe8649b66531725fa80aad2f3ffa612efba1070` |
| Project B, ripgrep | `sha256:a74d5a5daa37391695051f4162b33b30142cc201efe67612a4d9314d7516c27b` |

These are **local image content IDs**, not repository manifest digests. `RepoDigests` is empty for the built family. The unique local tags and separate content-ID verification guard are required for this local-only workflow; only the upstream Debian input uses an actual repository digest. No portable published family digest is claimed.

Both custom consuming definitions validate. Both generated consumers pass `projectsetup check`, real `docker compose config`, `devcontainer read-configuration`, and `projectsetup check --runtime` (zero runtime warnings). Both then start successfully through `devcontainer up`, execute their ordinary `npm ci` lifecycle, and create the fixture postinstall marker in their own temporary workspace bind. Direct non-login execution as `vscode` reports UID/GID 1000:1000, Node 22.14.0, npm 10.9.2, GitHub CLI 2.67.0, writable home, and jq 1.6 in A / ripgrep 13.0.0 in B. No AI credentials, SSH directories, Git configuration, or real application workspaces were mounted.

The native child was also executed mount-free with networking disabled: Node/npm/gh versions agree, gcc reports Debian 12.2.0, and `pkg-config --modversion libpq` reports 15.19. Before executing tools, its apt lists/archive caches and temporary download directory contain no leftover files. Node/npm can create normal ephemeral compile caches in the probe’s writable filesystem afterward; these are not baked image layers. Family inspections show final user `vscode`, no Docker volumes and no Dev Container lifecycle/mount metadata. Native artifact smoke checks prove installed tools/cache cleanup; no full native application was built.

Two relevant failure checks passed: requesting occupied UID 1 fails during account establishment with an actionable collision message; an incorrect expected ID in the local artifact lock fails without retagging/mutating an image. An ordinary `upgrade` with the user definition temporarily absent succeeds using byte-identical recorded TOML, confirming offline snapshot/artifact-reference preservation.

## Image layer measurement

Both consumers inherit all **five** Node-parent filesystem layers exactly. They also share the identical empty fixed-ID assertion layer, for a shared prefix of six layers. Neither runs a Node or GitHub CLI installer in its project layers. Each installs only its distinct project apt package and performs targeted parent-directory creation. Saved uncompressed layer tar payloads were measured by streaming `docker image save`:

| Image storage measure | Bytes |
| --- | ---: |
| A, saved layer tar total | 429,892,096 |
| B, saved layer tar total | 433,444,352 |
| Sum if both layer sets had independent copies | 863,336,448 |
| Actual unique layer tar union | 435,661,312 |
| Shared layer tar payload counted once | **427,675,136** |

The sharing avoids approximately **407.9 MiB (49.5%)** relative to that hypothetical independent-copy sum. This is a saved uncompressed layer-tar accounting comparison. It is **not physical daemon disk space reclaimed**, not compressed blob size, not a measurement of previously independently built projects, and not a projection for another server. Existing unrelated images/cache remain. `docker image inspect` reports different image logical sizes (419,525,220 and 423,091,073 bytes) because layer tar payloads include archive structure; those metrics are intentionally not mixed.

## UID adjustment and writable state

Generated `updateRemoteUserUID` is `false`, the invoking Linux user and artifacts both use 1000:1000, and each actual running container's image ID equals its built Compose image ID in the table. History inspection shows targeted parent creation and the fixed-ID assertion; startup creates **no additional UID-adjustment image or home-copying layer**. This is evidence for matching fixed-ID consumers, not a comparison with mismatching portable IDs or a claim that macOS sharing was exercised.

Separately, immediately after lifecycle/runtime verification, Docker's `inspect --size` reports:

| Container state | A | B |
| --- | ---: | ---: |
| Writable layer bytes (`SizeRw`) | 1,469,269 | 1,469,253 |
| Named/anonymous volume mounts | 0 | 0 |
| Temporary workspace bind mounts | 1 | 1 |

Bind-mounted workspace contents are host files, outside the image and writable-layer accounting. No database was selected; no database volume savings are claimed. The script removed only its two fixture Compose containers/networks after measurement. Final and earlier recipe-run artifacts, consumer images, cache, temporary fixture files and reports remain available; there was no pruning or cleanup of user resources.

Validation for this chunk: shell syntax checks, Python helper execution, `mise exec go@1.27.2 -- go test ./...`, `mise exec go@1.27.2 -- go vet ./...`, and `git diff --check` passed. No Go files or golden fixtures changed in this recipe/documentation chunk. Actual required Compose/read-configuration/build/runtime/startup checks are described above; no required check was unavailable.
