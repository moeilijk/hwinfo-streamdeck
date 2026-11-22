#!/usr/bin/env bash
# Reports whether Ubuntu has newer mingw-w64 packages than the ones pinned in
# scripts/release-builder/Dockerfile (issue #126). govulncheck only knows Go
# code; this is the signal for the compiler, linker and runtime that go into
# hwinfo-bridge.exe.
#
# Exit status: 0 the pinned versions are current, 3 a newer version exists,
# 1 the check could not run.
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dockerfile="$root_dir/scripts/release-builder/Dockerfile"

engine="${CONTAINER_ENGINE:-}"
if [[ -z "$engine" ]]; then
  if command -v docker >/dev/null 2>&1; then engine=docker
  elif command -v podman >/dev/null 2>&1; then engine=podman
  else echo "check-builder-updates: neither docker nor podman found" >&2; exit 1
  fi
fi

arg() { sed -n "s/^ARG $1=//p" "$dockerfile" | head -n 1; }
base="$(sed -n 's/^FROM \([^@ ]*\).*/\1/p' "$dockerfile" | head -n 1)"
declare -A pinned=(
  [gcc-mingw-w64-x86-64]="$(arg GCC_MINGW_VERSION)"
  [binutils-mingw-w64-x86-64]="$(arg BINUTILS_MINGW_VERSION)"
  [mingw-w64-x86-64-dev]="$(arg MINGW_W64_DEV_VERSION)"
)

# The current archive of the same Ubuntu release, not the pinned snapshot.
current="$("$engine" run --rm "$base" bash -c '
  apt-get update >/dev/null 2>&1 || exit 1
  for p in gcc-mingw-w64-x86-64 binutils-mingw-w64-x86-64 mingw-w64-x86-64-dev; do
    echo "$p $(apt-cache policy "$p" | sed -n "s/^ *Candidate: //p")"
  done')" || { echo "check-builder-updates: could not query the Ubuntu archive" >&2; exit 1; }

echo "base: $base, snapshot: $(arg SNAPSHOT)"
status=0
while read -r pkg candidate; do
  if [[ -z "$candidate" || "$candidate" == "(none)" ]]; then
    echo "check-builder-updates: no candidate version for $pkg" >&2
    exit 1
  fi
  if [[ "$candidate" == "${pinned[$pkg]}" ]]; then
    echo "  $pkg ${pinned[$pkg]} (current)"
  else
    echo "  $pkg ${pinned[$pkg]} -> $candidate available"
    status=3
  fi
done <<< "$current"
if [[ "$status" == "0" ]]; then
  echo "result: the pinned mingw-w64 packages are current"
else
  echo "result: newer mingw-w64 packages exist; update the Dockerfile and rebuild the binaries"
fi
exit $status
