# projectsetup v0.6.0

## What changed

- Add `projectsetup self-update` for Linux and macOS on amd64 and arm64, with stable-release selection, required SHA-256 archive verification, symlink-aware executable replacement, and actionable errors.
- Preserve current or newer installations and refuse unversioned or dirty development builds. Recognize versioned `go install` build metadata.
- Suggest the new command in best-effort stderr update notifications, retaining the release URL.
- Update source builds to Go 1.27.1 and `go-selfupdate` v1.6.0. Prebuilt binaries remain available for all four supported host targets.

## Update an existing installation

Older binaries (including v0.5.0) need the installer or a manual release installation once to gain the command:

```bash
curl -fsSL https://raw.githubusercontent.com/mpm/projectsetup/main/install.sh | sh
```

After installing this release:

```bash
projectsetup self-update
```

Run it from any directory. The executable's directory must be writable; the command never invokes sudo. The next invocation runs the new version. `projectsetup upgrade` still upgrades generated project configuration.

## Implementation and packaging

- `internal/version`: pinned `go-selfupdate` v1.6.0 integration, shared stable discovery/comparison, build metadata handling, and local HTTP/subprocess tests.
- `internal/cli` and `cmd/projectsetup`: command dispatch, cancellation, output, and notification wiring.
- `go.mod` / `go.sum`: current stable Go 1.27.1 and updater dependencies. CI and release builds select Go from `go.mod`; source installations require Go 1.27.1 or newer. Prebuilt binaries do not require a Go installation.
- `README.md`: usage, supported installations, permissions, and development-build policy.
- The release workflow retains the four version-bearing nested archives and SHA-256 archive entries in `checksums.txt`. Packaging matches the inspected published v0.5.0 release.

## Validation

Passed on Linux amd64:

- `go test ./...` and `go vet ./...` with Go 1.27.1 and `go-selfupdate` v1.6.0.
- `go test -race ./...` with Go 1.27.1.
- `CGO_ENABLED=0` cross-builds for Linux/macOS amd64/arm64.
- Local HTTP fixtures covering all four platform names, stable selection, current/newer no-ops, nested extraction, replacement and executable modes, checksum failures preserving the old executable, network errors/deadlines, missing assets, unwritable directories, and replacement failure.
- A disposable running executable invoked through a symlink updated successfully outside a workspace, preserved the symlink, and reported the new version in a subsequent subprocess.

Verification used disposable executables and did not replace an installed projectsetup executable. Generated container fixtures and release packaging were not changed.

Native runtime replacement must be distinguished from cross-build verification: macOS and Linux arm64 require their own runtime smoke tests. Rollback-failure reporting follows the pinned library API; a failed final rename plus failed rollback is not fault-injected by the application tests.
