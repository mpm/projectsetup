# projectsetup v0.14.0

This release makes containers use the standard Codex installation in `CODEX_HOME` instead of a separate projectsetup-managed copy, so `codex update` on the host or in any container updates the one installation every container runs.

## Changed

- `CODEX_HOME` (default `~/.codex`) is now mounted at its **host path** inside the container, for example `/home/alex/.codex`, and the `app` service's Compose environment sets `CODEX_HOME` to the same path. The path is interpolated from the host's `HOME` or `CODEX_HOME` when the container starts; generated files contain no user-specific path. The container's `HOME` stays `/home/vscode`.
- The official Codex installer writes absolute links into `CODEX_HOME/packages/standalone`. With identical paths they resolve on the host and in every container, and Codex recognizes its own installation, so `codex update` works everywhere.
- The setup script runs the official installer with the real `CODEX_HOME` only when no release is selected or on `--update codex`, and links the container's `~/.local/bin/codex` to `CODEX_HOME/packages/standalone/current/bin/codex`. It no longer rewrites installer links or takes its own lock; the installer serializes concurrent installations itself.
- When the shared release is built for another platform, such as a macOS host's own installation, the script leaves it untouched and installs a container-local Linux copy in `~/.local/share/codex-container`.
- `~/.local/share/codex` is no longer mounted or created, and `CODEX_HOME` is no longer set in `devcontainer.json` `containerEnv`.
- `check` validates the identical-path Codex bind and the Compose `CODEX_HOME`, and rejects a `CODEX_HOME` in `containerEnv` or inherited effective metadata. Definition mounts that overlap the host Codex state directory are rejected.
- `doctor` reports the standalone installation in `CODEX_HOME` and warns when the obsolete `~/.local/share/codex` installation is still present.

## Existing projects

Regenerate each project that selects Codex and recreate its container:

```bash
projectsetup upgrade
dworm down
dworm up -d
```

If you used the old shared installation on the host through `~/.local/share/codex/bin` on your PATH, remove that PATH entry and use the standard installation (`~/.local/bin/codex`). After every project has been upgraded and its container recreated, remove the old installation:

```bash
rm -rf ~/.local/share/codex
```

Projects that have not been upgraded still mount that directory, so it is not removed automatically.

[All changes since v0.13.0](https://github.com/mpm/projectsetup/compare/v0.13.0...v0.14.0)
