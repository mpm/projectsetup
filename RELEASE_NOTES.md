# projectsetup v0.16.0

This release makes the Python feature's bundled development tools opt-in, reducing image build time and implicit third-party package installation.

## Changed

- Built-in Python definition v1.1.0 disables the Python feature's bundled development tools by default with native `"installTools": false`, avoiding implicit pipx installs of flake8, black, mypy, pytest, pylint, and the rest of the upstream bundle. Projects should declare their own development dependencies in `requirements-dev.txt` or `pyproject.toml`.
- New preset option `python.install_tools` accepts `false` (default) or `true`. Enable the bundle explicitly with `projectsetup init --non-interactive --preset python --set python.install_tools=true`. The definition uses a two-value choice and a conditional variant to emit a JSON boolean without changing the option schema.
- `/usr/local/py-utils/bin` is included in generated PATH only when the bundle is enabled. The feature also skips installing pipx when disabled; the preset does not depend on it. Selected uv/Poetry setup still uses `python -m pip install --user`.

## Existing Python projects

Exact definition snapshots and hashes remain authoritative: plain `upgrade` preserves the old Python definition and behavior. Explicitly adopt the new default and rebuild:

```bash
projectsetup upgrade --refresh-presets
projectsetup check
dworm rebuild
```

Refresh preserves recorded selections and fills the newly introduced `python.install_tools` with `"false"`; subsequent refreshes preserve its recorded value. Rebuilding is required to remove the tools from an existing image. Custom/shared-image presets retain their own installation and preinstalled-capability contracts; this changes only the built-in Python definition.

Legacy schema 1 manifests have no definition snapshots and continue to migrate using current built-ins, including the new default.

The published `ghcr.io/devcontainers/features/python:1` definition (v1.8.1 when checked) calls the option `installTools`, defaults it to `true`, and uses `toolsToInstall` for the bundle list. Its [installer](https://github.com/devcontainers/features/blob/main/src/python/install.sh) gates pipx and the bundle behind `installTools`; Python installation is independent. Other feature options retain their upstream defaults.

## Verification

- Unit/golden tests, executable-mode comparisons, `go vet`, Compose configuration, and Dev Container configuration checks passed.
- A fresh Python 3.12 project with SQLite passed `check` and a real `check --build` with zero warnings. The build check took 234 seconds (3m 54s) on the local Docker daemon with its existing cache available.
- Isolated runs of the built image verified Python 3.12.16 and pip, with pipx and all 12 upstream bundled tools absent both from command lookup and `/usr/local/py-utils/bin`. Installing uv and Poetry with `python -m pip install --user` succeeded without pipx.
- Regression coverage uses the exact historical Python v1.0.0 snapshot/hash to verify plain upgrade retention, explicit refresh/default filling, selection preservation, and refusal of tampered snapshots. Default, explicit false/true, and invalid option choices are covered for all three package managers.
