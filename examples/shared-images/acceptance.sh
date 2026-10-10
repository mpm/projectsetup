#!/usr/bin/env bash
# Build/runtime/startup acceptance against new isolated fixture projects only.
set -euo pipefail
recipe=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
output=$(cd -- "${1:?usage: acceptance.sh BUILD_OUTPUT PROJECTSETUP_BINARY}" && pwd)
cli=${2:?absolute path to projectsetup binary}
[[ "$cli" = /* && -x "$cli" ]] || { echo 'supply an executable absolute CLI path' >&2; exit 1; }
export PROJECTSETUP_CONFIG_DIR="$output/config"
[[ ! -e "$output/consumers" ]] || { echo 'consumer fixtures already exist; use a new output directory' >&2; exit 1; }
mkdir "$output/consumers"
projects=()
cleanup() {
  # Only unique fixture stacks that this invocation generated; retain their images.
  for project in "${projects[@]}"; do
    docker compose -f "$project/.devcontainer/compose.yaml" down >/dev/null || true
  done
}
trap cleanup EXIT
"$recipe/verify-artifacts.sh" "$output/artifacts.lock"
for variant in node node-native; do
  "$cli" preset validate "$output/config/presets/reference-$variant.toml"
done
native=$(awk '$1 ~ /-node-native:1$/ { print $1 }' "$output/artifacts.lock")
docker run --rm --network none --user vscode --entrypoint /bin/bash "$native" --noprofile --norc -c '
  set -eu
  # Check baked caches before npm/Node create normal ephemeral runtime caches.
  test -z "$(find /var/lib/apt/lists /var/cache/apt/archives /tmp -type f -print -quit)"
  id; node --version; npm --version; gh --version
  gcc --version | head -1; pkg-config --modversion libpq
  test "$(stat -c %u:%g /opt/node/bin/node)" = 0:0
  test "$(stat -c %u:%g /usr/local/bin/gh)" = 0:0
  test -z "$(find /opt/node ! -user root -print -quit)"
  test -w "$HOME"
' > "$output/native.runtime.txt"
for suffix in a b; do
  project="$output/consumers/$suffix"
  mkdir "$project"
  name="ps-reference-$(basename "$output" | tr -cd 'a-z0-9-')-$suffix"
  package=jq
  [[ "$suffix" == a ]] || package=ripgrep
  cat > "$project/package.json" <<'JSON'
{"name":"reference-fixture","version":"1.0.0","scripts":{"postinstall":"node -e \"require('fs').writeFileSync('.reference-dependencies-installed','ok')\""}}
JSON
  cat > "$project/package-lock.json" <<'JSON'
{"name":"reference-fixture","version":"1.0.0","lockfileVersion":3,"requires":true,"packages":{"":{"name":"reference-fixture","version":"1.0.0","hasInstallScript":true}}}
JSON
  printf 'audit=false\nfund=false\nupdate-notifier=false\n' > "$project/.npmrc"
  "$recipe/verify-artifacts.sh" "$output/artifacts.lock"
  (
    cd "$project"
    "$cli" init --non-interactive --preset reference-node --name "$name" --ai none --system-package "$package"
    "$cli" check
    docker compose -f .devcontainer/compose.yaml config > "$output/$suffix.compose.yaml"
    devcontainer read-configuration --workspace-folder "$project" > "$output/$suffix.configuration.json"
    "$cli" check --runtime
  ) > "$output/$suffix.check.log" 2>&1
  projects+=("$project")
  "$recipe/verify-artifacts.sh" "$output/artifacts.lock"
  devcontainer up --workspace-folder "$project" > "$output/$suffix.up.json" 2> "$output/$suffix.up.log"
  container=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["containerId"])' "$output/$suffix.up.json")
  docker inspect --size "$container" > "$output/$suffix.container.json"
  docker exec --user vscode "$container" /bin/bash --noprofile --norc -c \
    'id; node --version; npm --version; gh --version; test -w "$HOME"; test -f .reference-dependencies-installed; if command -v jq; then jq --version; else rg --version; fi' > "$output/$suffix.runtime.txt"
  image=$(docker inspect --format '{{.Image}}' "$container")
  [[ "$image" == "$(docker image inspect --format '{{.Id}}' "$name-app:latest")" ]] || {
    echo 'startup used a different image; inspect unexpected UID-adjustment behavior' >&2; exit 1;
  }
  docker image inspect "$image" > "$output/$suffix.final-image.json"
  # Compose gives each unique project a stable local image tag for measurement.
  printf '%s-app:latest\n' "$name" >> "$output/consumer-images.txt"
done
mapfile -t images < "$output/consumer-images.txt"
"$recipe/measure.py" "${images[@]}" > "$output/measurement.json"
printf 'Acceptance evidence recorded in %s; owned fixture containers/networks will be removed.\n' "$output"
