#!/usr/bin/env bash
# Antivirus check for a release package (issue #116).
#
# Scans the .streamDeckPlugin package and the hwinfo.exe / hwinfo-bridge.exe
# it contains:
#   1. Microsoft Defender engine with current definitions, offline, via
#      scripts/defender-scan.sh. Always runs and is the hard gate.
#   2. VirusTotal hash lookup via scripts/virustotal-file.js when VT_API_KEY
#      is set (upload only with --upload).
#   3. OPSWAT MetaDefender Cloud and Kaspersky OpenTIP via
#      scripts/av-online-check.js when their keys are set (upload only with
#      --upload).
# Reports go to build/av-reports/<package>-<timestamp>/ with a summary.md.
# API keys (VIRUSTOTAL_APIKEY, METADEFENDER_APIKEY, OPENTIP_APIKEY) come from the
# environment or from $AV_CHECK_ENV, default ~/.config/hwinfo-streamdeck/av.env
# (KEY=value lines; the file stays outside the repository).
#
# Usage: scripts/av-check.sh [--upload] [--reanalyze] [--offline] [--update] [package]
#   package    defaults to the newest build/com.moeilijk.hwinfo-*.streamDeckPlugin
#   --upload   allow submitting unknown files to the online services
#   --reanalyze ask VirusTotal to rescan files it already knows (no upload)
#   --offline  skip the online services
#   --update   force a definitions download for the Defender scan
# Exit: 0 clean, 10 at least one detection, 1 a scan could not be completed.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"

env_file="${AV_CHECK_ENV:-$HOME/.config/hwinfo-streamdeck/av.env}"
if [ -r "$env_file" ]; then
  set -a; . "$env_file"; set +a
fi

upload=0; offline=0; reanalyze=0; update=()
while [ $# -gt 0 ]; do
  case "$1" in
    --upload) upload=1; shift ;;
    --reanalyze) reanalyze=1; shift ;;
    --offline) offline=1; shift ;;
    --update) update=(--update); shift ;;
    -*) echo "unknown option: $1" >&2; exit 1 ;;
    *) break ;;
  esac
done

pkg="${1:-}"
if [ -z "$pkg" ]; then
  pkg=$(ls -t "$ROOT"/build/com.moeilijk.hwinfo-*.streamDeckPlugin 2>/dev/null | head -1)
  [ -n "$pkg" ] || { echo "no package in build/; run make release or pass a path" >&2; exit 1; }
fi
[ -r "$pkg" ] || { echo "cannot read package: $pkg" >&2; exit 1; }
pkg=$(readlink -f "$pkg")
pkgname=$(basename "$pkg" .streamDeckPlugin)

stamp=$(date +%Y%m%d-%H%M%S)
report="$ROOT/build/av-reports/$pkgname-$stamp"
mkdir -p "$report"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# Extract the two executables from the package.
unzip -q -j -o "$pkg" 'com.moeilijk.hwinfo.sdPlugin/hwinfo.exe' 'com.moeilijk.hwinfo.sdPlugin/hwinfo-bridge.exe' -d "$work" \
  || { echo "package does not contain hwinfo.exe and hwinfo-bridge.exe" >&2; exit 1; }
files=("$pkg" "$work/hwinfo.exe" "$work/hwinfo-bridge.exe")

{
  echo "# Antivirus check: $pkgname"
  echo
  echo "Date: $(date -u +'%Y-%m-%d %H:%M UTC')"
  echo
  echo "## Files"
  echo
  echo '```'
  sha256sum "${files[@]}" | sed "s|$work/||; s|$ROOT/||"
  echo '```'
  echo
} > "$report/summary.md"

detections=0
errors=0
note() { echo "$*" | tee -a "$report/summary.md"; }

# 1. Defender, offline engine.
note "## Microsoft Defender engine (offline)"
note ""
"$HERE/defender-scan.sh" "${update[@]}" "${files[@]}" 2>&1 | sed "s|^Z:||; s|^$work/||; s|^$ROOT/||" | tee "$report/defender.txt"
rc=${PIPESTATUS[0]}
case $rc in
  0)  note "Result: clean." ;;
  10) note "Result: DETECTION, see defender.txt."; detections=$((detections+1)) ;;
  *)  note "Result: scan failed (exit $rc), see defender.txt."; errors=$((errors+1)) ;;
esac
grep -E "^defender-scan: engine" "$report/defender.txt" | sed 's/^defender-scan: /Versions: /' >> "$report/summary.md"
note ""

# 1b. The real Defender, cloud protection included, in the QEMU/KVM test VM
#     (scripts/defender-vm.sh) when a clean baseline exists.
note "## Microsoft Defender in the test VM (cloud protection)"
note ""
vmdir="${DEFENDER_VM_DIR:-$HOME/.cache/defender-vm}"
if [ ! -f "$vmdir/clean.stamp" ]; then
  note "Skipped: no VM baseline in $vmdir (see scripts/defender-vm.sh create/prepare/clean)."
else
  "$HERE/defender-vm.sh" scan --out "$report/defender-vm" "${files[@]}" 2>&1 | tee "$report/defender-vm-run.txt"
  rc=${PIPESTATUS[0]}
  case $rc in
    0)  note "Result: clean." ;;
    10) note "Result: DETECTION, see defender-vm/summary.md."; detections=$((detections+1)) ;;
    *)  note "Result: VM scan failed (exit $rc), see defender-vm-run.txt."; errors=$((errors+1)) ;;
  esac
  [ -f "$report/defender-vm/summary.md" ] && sed -n '3,4p' "$report/defender-vm/summary.md" >> "$report/summary.md"
fi
note ""

if [ $offline = 1 ]; then
  note "Online services skipped (--offline)."
else
  # 2. VirusTotal.
  note "## VirusTotal"
  note ""
  if [ -z "${VT_API_KEY:-${VIRUSTOTAL_API_KEY:-${VIRUSTOTAL_APIKEY:-}}}" ]; then
    note "Skipped: VT_API_KEY not set."
  else
    vtopts=()
    [ $upload = 1 ] && vtopts=(--upload-missing --wait)
    [ $reanalyze = 1 ] && vtopts+=(--reanalyze --wait)
    node "$HERE/virustotal-file.js" "${vtopts[@]}" "${files[@]}" 2>&1 | sed "s|$work/||" | tee "$report/virustotal.txt"
    rc=${PIPESTATUS[0]}
    if [ $rc -ne 0 ]; then
      note "Result: lookup failed (exit $rc), see virustotal.txt."; errors=$((errors+1))
    elif grep -E "^stats: .*malicious=[1-9]" "$report/virustotal.txt" >/dev/null; then
      note "Result: DETECTION, see virustotal.txt."; detections=$((detections+1))
    elif grep -E "^stats: " "$report/virustotal.txt" >/dev/null; then
      note "Result: clean for the files VirusTotal knows."
    else
      note "Result: no report (files not known to VirusTotal; rerun with --upload to submit)."
    fi
  fi
  note ""

  # 3. MetaDefender and OpenTIP.
  note "## OPSWAT MetaDefender Cloud and Kaspersky OpenTIP"
  note ""
  olopts=(--json-out "$report")
  [ $upload = 1 ] && olopts+=(--upload)
  node "$HERE/av-online-check.js" "${olopts[@]}" "${files[@]}" 2>&1 | sed "s|$work/||" | tee "$report/online.txt"
  rc=${PIPESTATUS[0]}
  case $rc in
    0)  note "Result: clean or skipped, see online.txt." ;;
    10) note "Result: DETECTION, see online.txt."; detections=$((detections+1)) ;;
    *)  note "Result: request failed (exit $rc), see online.txt."; errors=$((errors+1)) ;;
  esac
  note ""
fi

note "## Verdict"
note ""
if [ $detections -gt 0 ]; then
  note "DETECTIONS in $detections scan(s). Do not publish; see the report files in $report."
  exit 10
elif [ $errors -gt 0 ]; then
  note "INCOMPLETE: $errors scan(s) failed. Report: $report"
  exit 1
else
  note "Clean. Report: $report"
  exit 0
fi
