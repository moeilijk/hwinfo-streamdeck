#!/usr/bin/env bash
# Decides which hwinfo.exe and hwinfo-bridge.exe a release ships (issue #126).
#
# Windows reputation services judge unsigned files by hash, so a release
# should not ship new hashes without a reason. When the project's own Go code
# behind the two binaries has not changed since the previous release, this
# script replaces the freshly built binaries in the plugin directory with the
# ones from that release, byte for byte.
#
# Security comes first. The old binaries are only reused when they match the
# hashes recorded for that release in release-hashes.txt and govulncheck finds
# no known vulnerability in them. A dependency or toolchain update that fixes
# something the old binaries contain therefore ships with this release, and an
# asset that was replaced after publication is never carried forward.
#
# Whatever is shipped is recorded in release-hashes.txt for the version in the
# manifest; commit that file with the release.
#
# Environment:
#   REBUILD_BINARIES=1   keep the freshly built binaries, whatever changed
#   PREV_TAG=vX.Y.Z      compare against this release instead of the latest tag
#   RELEASE_REPO=o/r     GitHub repository to fetch the previous release from
#   HASHES_FILE=path     hash list (default release-hashes.txt in the repo root)
#   SDPLUGINDIR=path     plugin directory holding the binaries
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root_dir"

plugin_dir="${SDPLUGINDIR:-$root_dir/com.moeilijk.hwinfo.sdPlugin}"
hashes_file="${HASHES_FILE:-$root_dir/release-hashes.txt}"
cc="${WINCC:-x86_64-w64-mingw32-gcc}"
binaries=(hwinfo.exe hwinfo-bridge.exe)

# report prints the hashes of what is in the plugin directory now and records
# them for the version being released, unless that version is already tagged.
report() {
  local built="${1:-}" version tag tmp
  for bin in "${binaries[@]}"; do
    echo "  $bin $(sha256sum "$plugin_dir/$bin" | cut -d' ' -f1)"
  done
  version="$(python3 "$root_dir/scripts/manifest-version.py")"
  tag="v$version"
  if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
    echo "  $tag is already tagged; release-hashes.txt left as it is"
    return
  fi
  # Fresh binaries were built for this version; reused ones keep the
  # release they were built for.
  [[ -n "$built" ]] || built="$tag"
  tmp="$(mktemp)"
  awk -v a="$tag/hwinfo.exe" -v b="$tag/hwinfo-bridge.exe" '$2 != a && $2 != b' "$hashes_file" > "$tmp" || true
  for bin in "${binaries[@]}"; do
    echo "$(sha256sum "$plugin_dir/$bin" | cut -d' ' -f1)  $tag/$bin  built:$built" >> "$tmp"
  done
  cat "$tmp" > "$hashes_file"
  rm -f "$tmp"
  echo "  recorded for $tag in ${hashes_file#"$root_dir"/}"
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

# Fetch the previous binaries, check them against their recorded hashes and
# scan them. Only a clean result (exit 0) allows them to ship again.
set +e
bash "$root_dir/scripts/scan-release.sh" --out "$work" "$prev" 2>&1 | sed 's/^/  /'
scan_status=${PIPESTATUS[0]}
set -e

case "$scan_status" in
  0) ;;
  3)
    echo "binaries: rebuilt ($prev has known vulnerabilities; the fresh build ships instead)"
    report
    exit 0
    ;;
  *)
    echo "release-binaries: the $prev binaries could not be verified, so they are not reused" >&2
    echo "  The own Go code is unchanged since $prev. Fix the cause above," >&2
    echo "  or set REBUILD_BINARIES=1 to ship new binaries on purpose." >&2
    exit 1
    ;;
esac

for bin in "${binaries[@]}"; do
  cp "$work/$bin" "$plugin_dir/$bin"
done

echo "binaries: reused from $prev (own Go code unchanged, hashes verified, no known vulnerabilities)"
built="$(awk -v key="$prev/hwinfo.exe" '$2 == key { sub(/^built:/, "", $3); print $3 }' "$hashes_file" | head -n 1)"
report "${built:-$prev}"
