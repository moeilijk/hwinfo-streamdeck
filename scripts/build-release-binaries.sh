#!/usr/bin/env bash
# Builds hwinfo.exe and hwinfo-bridge.exe in the pinned build environment of
# scripts/release-builder/Dockerfile (issue #126).
#
# Usage: scripts/build-release-binaries.sh [--src <dir>] [--out <dir>]
#   --src <dir>  source tree to build (default: this repository)
#   --out <dir>  where the two binaries go (default: the plugin directory)
#
# Environment:
#   CONTAINER_ENGINE=docker|podman   default: docker when present, else podman
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
src="$root_dir"
out="$root_dir/com.moeilijk.hwinfo.sdPlugin"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --src) src="$(cd "$2" && pwd)"; shift 2 ;;
    --out) mkdir -p "$2"; out="$(cd "$2" && pwd)"; shift 2 ;;
    *) echo "build-release-binaries: unknown argument $1" >&2; exit 1 ;;
  esac
done

engine="${CONTAINER_ENGINE:-}"
if [[ -z "$engine" ]]; then
  if command -v docker >/dev/null 2>&1; then engine=docker
  elif command -v podman >/dev/null 2>&1; then engine=podman
  else
    echo "build-release-binaries: neither docker nor podman found" >&2
    exit 1
  fi
fi

# The build environment and the flags belong to the tree that is built, so a
# release is rebuilt with what it was built with.
builder_dir="$src/scripts/release-builder"
if [[ ! -f "$builder_dir/Dockerfile" ]]; then
  echo "build-release-binaries: $src has no scripts/release-builder/Dockerfile" >&2
  exit 2
fi
flags="$(make -s -C "$src" --eval='print-flags: ; @echo $(GOBUILDFLAGS)' print-flags)"
image="hwinfo-release-builder:$(sha256sum "$builder_dir/Dockerfile" | cut -c1-12)"

if ! "$engine" image inspect "$image" >/dev/null 2>&1; then
  echo "builder: building image $image"
  "$engine" build -q -t "$image" "$builder_dir" >/dev/null
fi

echo "builder: $image, flags: $flags"
"$engine" run --rm \
  --user "$(id -u):$(id -g)" \
  -v "$src":/src:ro \
  -v "$out":/out \
  -e BUILD_FLAGS="$flags" \
  -w /src \
  "$image" bash -euc '
    mkdir -p "$HOME"
    GOOS=windows GOARCH=amd64 go build $BUILD_FLAGS -o /out/hwinfo.exe ./cmd/hwinfo_streamdeck_plugin
    GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc go build $BUILD_FLAGS -o /out/hwinfo-bridge.exe ./cmd/hwinfo-bridge
  '
for bin in hwinfo.exe hwinfo-bridge.exe; do
  echo "  $bin $(sha256sum "$out/$bin" | cut -d" " -f1)"
done
