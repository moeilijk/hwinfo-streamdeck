#!/usr/bin/env bash
# Scan files with the Microsoft Defender engine and current definitions on
# Linux/WSL. Offline only: the engine runs under wine64 with the signature
# files Microsoft publishes for manual updates, so this covers signatures and
# local heuristics, not Defender's cloud/ML verdicts.
#
# Needs: wine64, x86_64-w64-mingw32-gcc, 7z, curl. No root, no Docker.
#
# Usage: scripts/defender-scan.sh [--update] [--verbose] <file>...
#   --update   download the latest definitions first (otherwise the cached
#              set is used; the cache is refreshed automatically after 24h)
#   --verbose  print archive members and file-type identifications
# Cache: $DEFENDER_SCAN_DIR, default ~/.cache/defender-scan (about 220 MB).
# Exit: 0 clean, 10 threat found, 1 unreadable input, 2 missing tool or
#       download/build failure, 3 engine refused to boot.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SRC="$HERE/defender-scan/mpclient-win.c"
WORK="${DEFENDER_SCAN_DIR:-$HOME/.cache/defender-scan}"
ENGINE_URL="https://go.microsoft.com/fwlink/?LinkID=121721&arch=x64"
MAX_AGE_HOURS="${DEFENDER_SCAN_MAX_AGE_HOURS:-24}"

WINE64="${WINE64:-}"
if [ -z "$WINE64" ]; then
  for c in wine64 /usr/lib/wine/wine64 /usr/lib/wine/wine; do
    if command -v "$c" >/dev/null 2>&1 || [ -x "$c" ]; then WINE64="$c"; break; fi
  done
fi

update=0
verbose=()
while [ $# -gt 0 ]; do
  case "$1" in
    --update) update=1; shift ;;
    --verbose|-v) verbose=(-v); shift ;;
    --) shift; break ;;
    -*) echo "unknown option: $1" >&2; exit 2 ;;
    *) break ;;
  esac
done
[ $# -ge 1 ] || { echo "usage: $0 [--update] [--verbose] <file>..." >&2; exit 2; }

[ -n "$WINE64" ] || { echo "missing tool: wine64 (apt install wine64)" >&2; exit 2; }
for t in x86_64-w64-mingw32-gcc 7z curl; do
  command -v "$t" >/dev/null 2>&1 || { echo "missing tool: $t" >&2; exit 2; }
done

files=()
for f in "$@"; do
  [ -r "$f" ] || { echo "cannot read: $f" >&2; exit 1; }
  files+=("$(readlink -f "$f")")
done

mkdir -p "$WORK/engine" "$WORK/quarantine"
cd "$WORK"

# 1. Engine and definitions. mpam-fe.exe is a self-extracting cabinet that 7z
#    reads directly; it contains mpengine.dll plus the base and delta .vdm files.
stale=0
if [ -f engine/mpengine.dll ] && [ -f engine/mpavdlta.vdm ]; then
  age_h=$(( ( $(date +%s) - $(stat -c %Y engine/mpavdlta.vdm) ) / 3600 ))
  [ "$age_h" -ge "$MAX_AGE_HOURS" ] && stale=1
else
  stale=1
fi
if [ $update = 1 ] || [ $stale = 1 ]; then
  echo "defender-scan: downloading definitions" >&2
  tmp=$(mktemp -d "$WORK/dl.XXXXXX")
  if curl -fsSL -o "$tmp/mpam-fe.exe" "$ENGINE_URL" && 7z x -y -o"$tmp/engine" "$tmp/mpam-fe.exe" >/dev/null \
     && [ -f "$tmp/engine/mpengine.dll" ] && [ -f "$tmp/engine/mpavbase.vdm" ]; then
    rm -f "$tmp/engine/MpSigStub.exe"
    rm -rf engine && mv "$tmp/engine" engine && rm -rf "$tmp"
    touch engine/mpavdlta.vdm
  else
    rm -rf "$tmp"
    if [ -f engine/mpengine.dll ]; then
      echo "defender-scan: download failed, using cached definitions" >&2
    else
      echo "defender-scan: download failed and no cached definitions" >&2
      exit 2
    fi
  fi
fi

# 2. Client, rebuilt when the source changes.
if [ ! -x mpclient-win.exe ] || [ "$SRC" -nt mpclient-win.exe ]; then
  x86_64-w64-mingw32-gcc -O1 -o mpclient-win.exe "$SRC" || exit 2
fi

# 3. Wine prefix, 64-bit only.
export WINEPREFIX="$WORK/prefix" WINEARCH=win64 WINEDEBUG=-all
if [ ! -d "$WINEPREFIX" ]; then
  "$WINE64" wineboot --init >/dev/null 2>&1 || true
fi

# 4. Report versions, then scan. Linux paths reach the client through Wine's Z: drive.
engine_ver=$( (strings -el engine/mpengine.dll || true) | grep -E '^1\.1\.[0-9]+\.[0-9]+$' | head -1)
sig_ver=$( (strings -el engine/mpavdlta.vdm || true) | grep -E '^1\.[0-9]{3}\.[0-9]+\.[0-9]+$' | head -1)
echo "defender-scan: engine ${engine_ver:-unknown}, definitions ${sig_ver:-unknown}, downloaded $(date -r engine/mpavdlta.vdm +%F)"

args=()
for f in "${files[@]}"; do args+=("Z:$f"); done
set +e
"$WINE64" ./mpclient-win.exe "${verbose[@]}" "${args[@]}"
rc=$?
set -e
exit $rc
