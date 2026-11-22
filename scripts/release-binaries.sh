#!/usr/bin/env bash
# Decides which hwinfo.exe and hwinfo-bridge.exe a release ships (issue #126).
#
# Windows reputation services judge unsigned files by hash, so a release
# should not ship new hashes without a reason. When the project's own Go code
# behind the two binaries has not changed since the previous release, this
# script replaces the freshly built binaries in the plugin directory with the
# ones from that release, byte for byte. Dependency and toolchain updates
# alone do not count as a reason; they ride along with the next release that
# changes the code, or are forced with REBUILD_BINARIES=1 when one of them
# fixes a security issue that affects the plugin.
#
# Environment:
#   REBUILD_BINARIES=1   keep the freshly built binaries, whatever changed
#   PREV_TAG=vX.Y.Z      compare against this release instead of the latest tag
#   RELEASE_REPO=o/r     GitHub repository to fetch the previous release from
#   SDPLUGINDIR=path     plugin directory holding the binaries
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root_dir"

plugin_dir="${SDPLUGINDIR:-$root_dir/com.moeilijk.hwinfo.sdPlugin}"
repo="${RELEASE_REPO:-moeilijk/hwinfo-streamdeck}"
cc="${WINCC:-x86_64-w64-mingw32-gcc}"
binaries=(hwinfo.exe hwinfo-bridge.exe)

report() {
  for bin in "${binaries[@]}"; do
    echo "  $bin $(sha256sum "$plugin_dir/$bin" | cut -d' ' -f1)"
  done
}

if [[ "${REBUILD_BINARIES:-}" == "1" ]]; then
  echo "binaries: rebuilt (REBUILD_BINARIES=1)"
  report
  exit 0
fi

prev="${PREV_TAG:-$(git describe --tags --abbrev=0 --match 'v*' 2>/dev/null || true)}"
if [[ -z "$prev" ]]; then
  echo "binaries: rebuilt (no previous release tag)"
  report
  exit 0
fi

# Directories of the project's own packages that are compiled into the two
# binaries. Test files are not compiled in and do not count.
mapfile -t dirs < <(
  GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC="$cc" go list -deps \
    -f '{{if .Module}}{{if .Module.Main}}{{.Dir}}{{end}}{{end}}' \
    ./cmd/hwinfo_streamdeck_plugin ./cmd/hwinfo-bridge |
    sed "s#^$root_dir/##" | sort -u
)
if [[ ${#dirs[@]} -eq 0 ]]; then
  echo "release-binaries: could not list the packages behind the binaries" >&2
  exit 1
fi

# Committed changes since the previous release plus anything uncommitted,
# because the release is built from the working tree.
changed="$(
  {
    git diff --name-only "$prev" HEAD -- "${dirs[@]}"
    git status --porcelain -- "${dirs[@]}" | cut -c4-
  } | grep -v '_test\.go$' | sort -u || true
)"

if [[ -n "$changed" ]]; then
  echo "binaries: rebuilt (own Go code changed since $prev)"
  echo "$changed" | sed 's/^/  changed: /'
  report
  exit 0
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

if ! gh release download "$prev" --repo "$repo" --pattern '*.streamDeckPlugin' --dir "$work" >/dev/null; then
  echo "release-binaries: could not download the $prev release asset from $repo" >&2
  echo "  The own Go code is unchanged since $prev, so this release should reuse its binaries." >&2
  echo "  Fix the download, or set REBUILD_BINARIES=1 to ship new binaries on purpose." >&2
  exit 1
fi

package="$(find "$work" -name '*.streamDeckPlugin' | head -n 1)"
for bin in "${binaries[@]}"; do
  if ! unzip -p "$package" "com.moeilijk.hwinfo.sdPlugin/$bin" > "$work/$bin" || [[ ! -s "$work/$bin" ]]; then
    echo "release-binaries: $bin not found in the $prev release asset" >&2
    exit 1
  fi
done
for bin in "${binaries[@]}"; do
  cp "$work/$bin" "$plugin_dir/$bin"
done

echo "binaries: reused from $prev (own Go code unchanged since then)"
report
