#!/usr/bin/env bash
# Local tags are mutable; guard every build/start against the recorded content IDs.
set -euo pipefail
lock=${1:?usage: verify-artifacts.sh OUTPUT_DIRECTORY/artifacts.lock}
while read -r tag expected; do
  actual=$(docker image inspect --format '{{.Id}}' "$tag")
  [[ "$actual" == "$expected" ]] || {
    printf 'artifact changed: %s expected %s, found %s; restore it or explicitly rebuild/refresh\n' "$tag" "$expected" "$actual" >&2
    exit 1
  }
done < "$lock"
