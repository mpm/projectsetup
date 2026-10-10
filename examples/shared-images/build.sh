#!/usr/bin/env bash
# Local reference workflow; artifacts remain available, never publishes/prunes.
set -euo pipefail
recipe=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
output=${1:?usage: build.sh NEW_OUTPUT_DIRECTORY}
[[ ! -e "$output" ]] || { echo "output already exists: $output" >&2; exit 1; }
[[ $(docker info --format '{{.OSType}}/{{.Architecture}}') == linux/x86_64 ]] || {
  echo 'this verified recipe targets linux/amd64 only; verify other inputs before porting' >&2; exit 1;
}
mkdir -p "$output/config/presets"
cp "$recipe"/*.Dockerfile "$output/"
sha256sum "$recipe"/*.Dockerfile > "$output/recipes.sha256"
docker version > "$output/docker-version.txt"
output=$(cd -- "$output" && pwd)
prefix="local/projectsetup-reference-$(date +%s)-$$"
uid=$(id -u); gid=$(id -g)
[[ "$uid" != 0 && "$gid" != 0 ]] || { echo 'run as a non-root consumer with positive primary IDs' >&2; exit 1; }
for variant in foundation node node-native; do
  tag="$prefix-$variant:1"
  if docker image inspect "$tag" >/dev/null 2>&1; then echo "refusing existing tag $tag" >&2; exit 1; fi
  args=()
  case "$variant" in
    foundation) args+=(--build-arg "VSCODE_UID=$uid" --build-arg "VSCODE_GID=$gid");;
    node) args+=(--build-arg "FOUNDATION=$prefix-foundation:1");;
    node-native) args+=(--build-arg "NODE=$prefix-node:1");;
  esac
  if [[ -f "$output/artifacts.lock" ]]; then "$recipe/verify-artifacts.sh" "$output/artifacts.lock"; fi
  docker build --platform linux/amd64 "${args[@]}" -f "$recipe/$variant.Dockerfile" -t "$tag" "$recipe"
  docker image inspect "$tag" > "$output/$variant.inspect.json"
  printf '%s %s\n' "$tag" "$(docker image inspect --format '{{.Id}}' "$tag")" >> "$output/artifacts.lock"
done
for variant in node node-native; do
  cat > "$output/config/presets/reference-$variant.toml" <<TOML
schema = 2
kind = "preset"
name = "reference-$variant"
version = "1.0.0"
description = "Local shared Node CLI bundle ($variant)"
[image]
base = "$prefix-$variant:1"
[image.ownership]
mode = "fixed"
uid = $uid
gid = $gid
[image.preinstalled]
core_packages = ["bash", "ca-certificates", "curl", "git", "gnupg", "sudo"]
[image.preinstalled.tools.gh]
version = "2.67.0"
executable = "/usr/local/bin/gh"
path = ["/usr/local/bin"]
[image.preinstalled.tools.node]
version = "22.14.0"
executable = "/opt/node/bin/node"
path = ["/opt/node/bin"]
[image.preinstalled.tools.npm]
version = "10.9.2"
executable = "/opt/node/bin/npm"
path = ["/opt/node/bin"]
[setup]
script = '''
if [[ -f package.json && -f package-lock.json ]]; then
  npm ci
fi
'''
TOML
done
printf 'Local artifacts and consuming definitions recorded in %s\n' "$output"
