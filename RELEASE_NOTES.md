# projectsetup v0.13.0

This release selects every supported AI tool by default and makes the container's `claude` command follow the shared Claude Code installation instead of pinning one version.

## Changed

- `init` without `--ai` now selects OpenCode, Claude Code, and Codex instead of OpenCode only. The interactive prompt offers all three first and uses them as its default. `--ai` still narrows the selection, and `--ai none` selects no tools. `init --list-options --json` reports `"defaultAITools": ["claude", "codex", "opencode"]`.
- Existing manifests keep their recorded selection; `upgrade` does not add tools.

## Fixed

- `~/.local/bin/claude` in the container is now a launcher script instead of a symlink to the version that was newest when the container was created. On every call it runs the highest release version in the host-mounted `~/.local/share/claude/versions` directory, so:
  - Claude Code updates made on the host take effect in the container immediately.
  - Host cleanup of old versions no longer leaves a dangling `claude` command.
  - `claude update` inside the container installs into the shared directory and is used on the next call. Claude Code's updater does not replace a launcher that is not a symlink into `versions/`; `claude doctor` notes this, which is expected.
- Only plain version names are considered, so partial or foreign files in `versions/` are ignored, and a symlinked `versions` directory is followed.

## Existing projects

Regenerate the scripts, then either recreate the container or replace the old symlink in place:

```bash
projectsetup upgrade
dworm exec -- .devcontainer/scripts/install-ai-tools.sh claude
```

[All changes since v0.12.0](https://github.com/mpm/projectsetup/compare/v0.12.0...v0.13.0)
