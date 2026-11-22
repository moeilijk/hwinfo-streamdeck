#!/usr/bin/env bash
# Rebuilds the binaries of a published release from its tag in the pinned
# build environment and compares them with release-hashes.txt (issue #126).
# The binaries do not carry their commit, so this is what ties a shipped hash
# to the source it was built from.
#
# Usage: scripts/verify-release.sh [<tag>]     default: the latest v* tag
#
# Exit status: 0 the rebuild matches the record, 1 it does not (or the check
# could not run), 2 the release predates the pinned build environment and
# cannot be rebuilt this way.
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
hashes_file="${HASHES_FILE:-$root_dir/release-hashes.txt}"
binaries=(hwinfo.exe hwinfo-bridge.exe)

tag="${1:-$(git -C "$root_dir" describe --tags --abbrev=0 --match 'v*')}"

recorded() { awk -v key="$tag/$1" '$2 == key { print $1 }' "$hashes_file" | head -n 1; }
built="$(awk -v key="$tag/hwinfo.exe" '$2 == key { sub(/^built:/, "", $3); print $3 }' "$hashes_file" | head -n 1)"
if [[ -z "$(recorded hwinfo.exe)" || -z "$built" ]]; then
  echo "verify-release: $tag is not in ${hashes_file#"$root_dir"/}" >&2
  exit 1
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
git clone -q "$root_dir" "$work/src"
if ! git -C "$work/src" checkout -q "refs/tags/$built" 2>/dev/null; then
  echo "verify-release: tag $built not found" >&2
  exit 1
fi

echo "release: $tag, binaries built for $built"
if [[ ! -x "$work/src/scripts/build-release-binaries.sh" || ! -f "$work/src/scripts/release-builder/Dockerfile" ]]; then
  echo "result: $built predates the pinned build environment and cannot be rebuilt for comparison"
  exit 2
fi

bash "$work/src/scripts/build-release-binaries.sh" --src "$work/src" --out "$work/out" | sed 's/^/  /'

status=0
for bin in "${binaries[@]}"; do
  got="$(sha256sum "$work/out/$bin" | cut -d' ' -f1)"
  want="$(recorded "$bin")"
  if [[ "$got" == "$want" ]]; then
    echo "  $bin: the rebuild matches the recorded hash"
  else
    echo "  $bin: the rebuild DIFFERS from the recorded hash" >&2
    echo "    recorded: $want" >&2
    echo "    rebuilt:  $got" >&2
    status=1
  fi
done
if [[ "$status" == "0" ]]; then
  echo "result: $tag is what its source builds"
else
  echo "result: $tag does not match a rebuild of $built"
fi
exit $status
