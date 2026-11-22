#!/usr/bin/env bash
# Fetches the binaries of a published release, checks them against the hashes
# recorded in release-hashes.txt and scans them for known vulnerabilities with
# govulncheck in binary mode (issue #126).
#
# Usage: scripts/scan-release.sh [--out <dir>] [<tag>]
#   <tag>        release to scan; default: the latest release of the repository
#   --out <dir>  keep hwinfo.exe and hwinfo-bridge.exe of that release in <dir>
#
# Exit status: 0 clean, 3 known vulnerabilities found, 1 anything else
# (download failed, hash unknown or different, scanner failed). Only 0 means
# the binaries are fit to ship again.
#
# Environment:
#   RELEASE_REPO=o/r   GitHub repository (default moeilijk/hwinfo-streamdeck)
#   HASHES_FILE=path   hash list (default release-hashes.txt in the repo root)
#   GOVULNCHECK=cmd    scanner command (default: govulncheck from PATH or
#                      GOPATH/bin, else `go run` of the latest version)
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
repo="${RELEASE_REPO:-moeilijk/hwinfo-streamdeck}"
hashes_file="${HASHES_FILE:-$root_dir/release-hashes.txt}"
binaries=(hwinfo.exe hwinfo-bridge.exe)

out=""
tag=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --out) out="$2"; shift 2 ;;
    -*) echo "scan-release: unknown option $1" >&2; exit 1 ;;
    *) tag="$1"; shift ;;
  esac
done

if [[ -z "$tag" ]]; then
  tag="$(gh release view --repo "$repo" --json tagName --jq .tagName 2>/dev/null || true)"
  if [[ -z "$tag" ]]; then
    echo "scan-release: could not determine the latest release of $repo" >&2
    exit 1
  fi
fi

if [[ -n "${GOVULNCHECK:-}" ]]; then
  read -r -a scanner <<< "$GOVULNCHECK"
elif command -v govulncheck >/dev/null 2>&1; then
  scanner=(govulncheck)
elif [[ -x "$(go env GOPATH)/bin/govulncheck" ]]; then
  scanner=("$(go env GOPATH)/bin/govulncheck")
else
  scanner=(go run golang.org/x/vuln/cmd/govulncheck@latest)
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

if ! gh release download "$tag" --repo "$repo" --pattern '*.streamDeckPlugin' --dir "$work" >/dev/null; then
  echo "scan-release: could not download the $tag release asset from $repo" >&2
  exit 1
fi
package="$(find "$work" -name '*.streamDeckPlugin' | head -n 1)"
if [[ -z "$package" ]]; then
  echo "scan-release: release $tag has no .streamDeckPlugin asset" >&2
  exit 1
fi

echo "release: $tag ($repo)"
for bin in "${binaries[@]}"; do
  if ! unzip -p "$package" "com.moeilijk.hwinfo.sdPlugin/$bin" > "$work/$bin" || [[ ! -s "$work/$bin" ]]; then
    echo "scan-release: $bin not found in the $tag release asset" >&2
    exit 1
  fi
  got="$(sha256sum "$work/$bin" | cut -d' ' -f1)"
  want="$(awk -v key="$tag/$bin" '$2 == key { print $1 }' "$hashes_file" 2>/dev/null | head -n 1)"
  if [[ -z "$want" ]]; then
    echo "scan-release: no recorded hash for $tag/$bin in ${hashes_file#"$root_dir"/}" >&2
    echo "  Without it the download cannot be told apart from a replaced asset." >&2
    exit 1
  fi
  if [[ "$got" != "$want" ]]; then
    echo "scan-release: $tag/$bin does not match its recorded hash" >&2
    echo "  recorded:   $want" >&2
    echo "  downloaded: $got" >&2
    exit 1
  fi
  echo "  $bin $got (matches the recorded hash)"
done

vulnerable=0
for bin in "${binaries[@]}"; do
  set +e
  report="$("${scanner[@]}" -mode=binary "$work/$bin" 2>&1)"
  status=$?
  set -e
  case "$status" in
    0)
      echo "  $bin: no known vulnerabilities"
      ;;
    3)
      vulnerable=1
      ids="$(grep -oE '^Vulnerability #[0-9]+: [A-Z0-9-]+' <<< "$report" | awk '{print $3}' | paste -sd' ' -)"
      echo "  $bin: $(wc -w <<< "$ids") known vulnerabilities: $ids"
      ;;
    *)
      echo "scan-release: the scanner failed on $bin (exit $status)" >&2
      echo "$report" | tail -n 5 >&2
      exit 1
      ;;
  esac
done

if [[ -n "$out" ]]; then
  mkdir -p "$out"
  for bin in "${binaries[@]}"; do
    cp "$work/$bin" "$out/$bin"
  done
fi

if [[ "$vulnerable" == "1" ]]; then
  echo "result: $tag ships binaries with known vulnerabilities"
  exit 3
fi
echo "result: $tag is clean"
