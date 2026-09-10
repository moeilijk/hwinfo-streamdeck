#!/usr/bin/env bash
# Disposable Windows 11 VM with the real Microsoft Defender, cloud protection
# included, on QEMU/KVM (issue #116). Runs entirely from Linux/WSL: no Hyper-V,
# no UAC, no Windows-side scripts. The host talks to the VM over ssh (OpenSSH
# server installed by scripts/defender-vm/firstlogon.ps1 during the unattended
# installation) and to QEMU over QMP.
#
# Needs: qemu-system-x86_64 with KVM (/dev/kvm, user in group kvm), swtpm,
# xorrisofs, ssh/scp, python3, a Windows 11 ISO. About 25 GB disk for the VM.
#
# Usage: scripts/defender-vm.sh <command> [args]
#   create  --iso <win11.iso>   unattended install, then OpenSSH; takes a while
#   prepare                     update the Defender platform, reboot so the
#                               platform switch is committed, update the
#                               definitions, validate the cloud connection,
#                               shut down
#   clean                       mark the current disk as the clean baseline;
#                               every scan starts from a copy of it
#   scan [--out <dir>] [--min-signatures <ver>] <file>..
#                               copy files into a fresh VM, update the
#                               definitions, let real-time protection and
#                               MpCmdRun judge the files, collect detections,
#                               definition versions and the Defender event
#                               log, shut down and discard. With
#                               --min-signatures the scan fails (exit 1) when
#                               the definitions end up below that version.
#   status | screenshot [file] | ssh [cmd] | stop | destroy
# Env: DEFENDER_VM_DIR (default ~/.cache/defender-vm), DEFENDER_VM_RAM (6G),
#      DEFENDER_VM_CPUS (4), DEFENDER_VM_DISK (60G), DEFENDER_VM_SSH_PORT (2222),
#      DEFENDER_VM_VNC (127.0.0.1:0, first free port from 5900; empty disables).
# Exit: 0 clean, 10 detection, 1 failure (scan); 0/1 for the other commands.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ASSETS="$HERE/defender-vm"
VMDIR="${DEFENDER_VM_DIR:-$HOME/.cache/defender-vm}"
RAM="${DEFENDER_VM_RAM:-6G}"
CPUS="${DEFENDER_VM_CPUS:-4}"
DISK="${DEFENDER_VM_DISK:-60G}"
SSH_PORT="${DEFENDER_VM_SSH_PORT:-2222}"
VNC="${DEFENDER_VM_VNC-127.0.0.1:0}"
# The Secure Boot firmware builds (*.ms.fd, *.secboot.fd) keep their variable
# store behind SMM; every UEFI variable write then enters SMM, which KVM nested
# under Hyper-V/WSL cannot enter ("KVM: entry failed, hardware error"). The
# plain build has no SMM and no Secure Boot; autounattend.xml bypasses the
# Secure Boot check of the Windows 11 installer. Defender does not care.
OVMF_CODE="${OVMF_CODE:-/usr/share/OVMF/OVMF_CODE_4M.fd}"
OVMF_VARS="${OVMF_VARS:-/usr/share/OVMF/OVMF_VARS_4M.fd}"
GUEST_USER=scan

BASE_DISK="$VMDIR/disk.qcow2"
BASE_VARS="$VMDIR/ovmf_vars.fd"
BASE_TPM="$VMDIR/tpm"
RUN="$VMDIR/run"
QMP="$VMDIR/qmp.sock"
PIDFILE="$VMDIR/qemu.pid"
KEY="$VMDIR/id_ed25519"
CLEAN_STAMP="$VMDIR/clean.stamp"

log() { echo "defender-vm: $*" >&2; }
die() { log "$*"; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "missing tool: $1"; }

qmp() {
  # qmp <command> [json-arguments]
  python3 - "$QMP" "$1" "${2:-{\}}" <<'PY'
import json, socket, sys
path, cmd, args = sys.argv[1], sys.argv[2], json.loads(sys.argv[3])
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
s.settimeout(30)
s.connect(path)
f = s.makefile("rw")
f.readline()
for msg in ({"execute": "qmp_capabilities"}, {"execute": cmd, "arguments": args}):
    f.write(json.dumps(msg) + "\n"); f.flush()
    while True:
        line = f.readline()
        if not line: sys.exit(1)
        r = json.loads(line)
        if "event" in r: continue
        break
if "error" in r:
    print(json.dumps(r["error"])); sys.exit(1)
print(json.dumps(r.get("return")))
PY
}

vm_pid() { [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null && cat "$PIDFILE"; }
vm_running() { [ -n "$(vm_pid || true)" ]; }

ssh_opts() {
  echo -i "$KEY" -p "$SSH_PORT" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
       -o ConnectTimeout=5 -o LogLevel=ERROR -o BatchMode=yes
}
# vm_ps <powershell>  runs PowerShell in the guest (sshd default shell is powershell.exe)
vm_ps() { ssh $(ssh_opts) "$GUEST_USER@127.0.0.1" "$1"; }
vm_put() { scp $(ssh_opts | sed 's/-p /-P /') "$1" "$GUEST_USER@127.0.0.1:$2"; }
vm_get() { scp $(ssh_opts | sed 's/-p /-P /') "$GUEST_USER@127.0.0.1:$1" "$2"; }

wait_ssh() {
  local deadline=$(( $(date +%s) + ${1:-600} ))
  while [ "$(date +%s)" -lt "$deadline" ]; do
    if vm_ps 'Write-Output ready' 2>/dev/null | grep -q ready; then return 0; fi
    vm_running || die "VM exited while waiting for ssh: $(tail -3 "$VMDIR/qemu.log" 2>/dev/null)"
    sleep 10
  done
  return 1
}

wait_off() {
  local deadline=$(( $(date +%s) + ${1:-300} ))
  while vm_running && [ "$(date +%s)" -lt "$deadline" ]; do sleep 3; done
  if vm_running; then log "VM still running, killing it"; qmp quit >/dev/null 2>&1 || kill "$(vm_pid)" || true; sleep 2; fi
  pkill -f "swtpm socket --tpmstate dir=$VMDIR" 2>/dev/null || true
}

# PowerShell snippet: prints "defender-ready <product> <signatures>" when the
# service answers and the running platform (AMProductVersion) is the newest
# one under ProgramData\Microsoft\Windows Defender\Platform. While a platform
# update is being installed the directory is absent or newer than the running
# service, the service restarts, and a signature update fired into that window
# is lost (MpCmdRun still reports "finished").
DEFENDER_READY_PS='
  try {
    if ((Get-Service WinDefend).Status -ne "Running") { "not-ready service"; exit }
    $s = Get-MpComputerStatus -ErrorAction Stop; Get-MpPreference -ErrorAction Stop | Out-Null
    $p = Get-ChildItem "$env:ProgramData\Microsoft\Windows Defender\Platform" -Directory -ErrorAction SilentlyContinue | Sort-Object Name -Descending | Select-Object -First 1
    if (-not $p) { "not-ready no-platform-dir"; exit }
    $pv = $p.Name -replace "-.*$", ""
    if ($s.AMProductVersion -ne $pv) { "not-ready service $($s.AMProductVersion) platform $pv"; exit }
    "defender-ready $($s.AMProductVersion) $($s.AntivirusSignatureVersion)"
  } catch { "not-ready " + $_.Exception.Message }
'

wait_defender_ready() {
  local deadline=$(( $(date +%s) + ${1:-300} )) state=""
  while [ "$(date +%s)" -lt "$deadline" ]; do
    state="$(vm_ps "$DEFENDER_READY_PS" 2>/dev/null | tr -d '\r' | grep -m1 -E '^(defender-ready|not-ready)' || true)"
    case "$state" in defender-ready*) log "Defender ready: ${state#defender-ready }"; return 0 ;; esac
    sleep 10
  done
  log "Defender not ready: ${state:-no answer}"
  return 1
}

# Definitions from the newest platform's MpCmdRun; prints the resulting
# AntivirusSignatureVersion (Get-MpComputerStatus, not the MpCmdRun output,
# which shows the versions from before the update).
update_definitions() {
  vm_ps '
    $ErrorActionPreference = "Continue"
    $p = Get-ChildItem "$env:ProgramData\Microsoft\Windows Defender\Platform" -Directory -ErrorAction SilentlyContinue | Sort-Object Name -Descending | Select-Object -First 1
    $mp = if ($p) { Join-Path $p.FullName "MpCmdRun.exe" } else { "$env:ProgramFiles\Windows Defender\MpCmdRun.exe" }
    & $mp -SignatureUpdate 2>&1 | Out-Null
    "signatures " + (Get-MpComputerStatus).AntivirusSignatureVersion
  ' 2>/dev/null | tr -d '\r' | grep -m1 '^signatures' || echo "signatures unknown"
}

start_vm() {
  # start_vm <disk> <vars> <tpmdir> [extra qemu args...]
  local disk="$1" vars="$2" tpmdir="$3"; shift 3
  vm_running && die "VM already running (pid $(vm_pid))"
  need qemu-system-x86_64; need swtpm
  [ -r /dev/kvm ] && [ -w /dev/kvm ] || die "/dev/kvm not accessible (is $USER in group kvm?)"
  mkdir -p "$tpmdir"
  swtpm socket --tpmstate "dir=$tpmdir" --ctrl "type=unixio,path=$tpmdir/swtpm.sock" --tpm2 \
    --log "file=$tpmdir/swtpm.log,level=1" --daemon --pid "file=$tpmdir/swtpm.pid"
  local display=(-display none)
  # ",to=9" lets QEMU pick the first free display up to :9 (ports 5900-5909).
  [ -n "$VNC" ] && display+=(-vnc "$VNC,to=9")
  # -svm hides nested virtualization from Windows: with it visible, Windows 11
  # starts its own hypervisor (VBS), which KVM nested under Hyper-V/WSL cannot
  # run ("invalid vmcb", QEMU aborts). The hv-* flags are the usual Windows
  # enlightenments.
  setsid qemu-system-x86_64 \
    -name av-test -machine q35,accel=kvm -cpu host,-svm,hv-relaxed,hv-vapic,hv-time,hv-spinlocks=0x1fff -smp "$CPUS" -m "$RAM" \
    -rtc base=localtime,clock=host \
    -drive "if=pflash,format=raw,readonly=on,file=$OVMF_CODE" \
    -drive "if=pflash,format=raw,file=$vars" \
    -chardev "socket,id=chrtpm,path=$tpmdir/swtpm.sock" -tpmdev emulator,id=tpm0,chardev=chrtpm -device tpm-tis,tpmdev=tpm0 \
    -device ahci,id=ahci \
    -drive "id=disk0,file=$disk,format=qcow2,if=none,discard=unmap" -device ide-hd,drive=disk0,bus=ahci.0,bootindex=1 \
    -netdev "user,id=net0,hostfwd=tcp:127.0.0.1:$SSH_PORT-:22" -device e1000e,netdev=net0 \
    -device qemu-xhci -device usb-tablet -vga std \
    "${display[@]}" \
    -qmp "unix:$QMP,server,nowait" -pidfile "$PIDFILE" \
    "$@" >"$VMDIR/qemu.log" 2>&1 < /dev/null &
  disown
  local i; for i in $(seq 1 20); do [ -s "$PIDFILE" ] && break; sleep 0.5; done
  if ! vm_running; then
    pkill -f "swtpm socket --tpmstate dir=$tpmdir" 2>/dev/null || true
    die "QEMU did not start: $(tail -5 "$VMDIR/qemu.log")"
  fi
  log "VM started (pid $(cat "$PIDFILE"), ssh port $SSH_PORT${VNC:+, vnc $VNC}; qemu output in $VMDIR/qemu.log)"
}

fresh_run() {
  # Copies of the clean baseline for one disposable run.
  rm -rf "$RUN"; mkdir -p "$RUN/tpm"
  qemu-img create -q -f qcow2 -b "$BASE_DISK" -F qcow2 "$RUN/disk.qcow2"
  cp "$BASE_VARS" "$RUN/ovmf_vars.fd"
  cp "$BASE_TPM"/tpm2-* "$RUN/tpm/" 2>/dev/null || true
}

cmd_create() {
  local iso=""
  while [ $# -gt 0 ]; do case "$1" in --iso) iso="$2"; shift 2 ;; *) die "unknown option: $1" ;; esac; done
  [ -n "$iso" ] && [ -r "$iso" ] || die "create needs --iso <windows11.iso>"
  need qemu-img; need xorrisofs; need ssh-keygen; need python3
  [ -f "$OVMF_CODE" ] && [ -f "$OVMF_VARS" ] || die "OVMF firmware not found (apt install ovmf)"
  [ -e "$BASE_DISK" ] && [ -f "$CLEAN_STAMP" ] && die "$BASE_DISK exists; run destroy first"
  rm -rf "$VMDIR"
  mkdir -p "$VMDIR" "$BASE_TPM"; chmod 700 "$VMDIR"

  ssh-keygen -q -t ed25519 -N '' -C defender-vm -f "$KEY"
  local password; password="$(head -c 24 /dev/urandom | base64 | tr -d '/+=' | head -c 20)"
  printf '%s\n' "$password" > "$VMDIR/password"; chmod 600 "$VMDIR/password"

  # Small ISO with autounattend.xml in its root and the first-logon script.
  local stage="$VMDIR/unattend"; rm -rf "$stage"; mkdir -p "$stage/setup"
  sed "s|@@PASSWORD@@|$password|g" "$ASSETS/autounattend.xml" > "$stage/autounattend.xml"
  sed "s|@@PUBKEY@@|$(cat "$KEY.pub")|g" "$ASSETS/firstlogon.ps1" > "$stage/setup/firstlogon.ps1"
  xorrisofs -quiet -o "$VMDIR/unattend.iso" -J -R -V UNATTEND "$stage" 2>/dev/null
  rm -rf "$stage"

  qemu-img create -q -f qcow2 "$BASE_DISK" "$DISK"
  cp "$OVMF_VARS" "$BASE_VARS"
  start_vm "$BASE_DISK" "$BASE_VARS" "$BASE_TPM" \
    -drive "id=cd0,file=$iso,format=raw,if=none,media=cdrom,readonly=on" -device ide-cd,drive=cd0,bus=ahci.1,bootindex=0 \
    -drive "id=cd1,file=$VMDIR/unattend.iso,format=raw,if=none,media=cdrom,readonly=on" -device ide-cd,drive=cd1,bus=ahci.2

  # The Windows ISO asks to "press any key" before booting from DVD; answer
  # that during the first boot only. Later reboots time out into the disk.
  log "installing Windows (unattended); pressing a key for the DVD prompt"
  local i; for i in $(seq 1 25); do qmp send-key '{"keys":[{"type":"qcode","data":"ret"}]}' >/dev/null 2>&1 || true; sleep 2; done
  log "waiting for the installation and OpenSSH (this takes a while)"
  wait_ssh 5400 || die "no ssh after 90 minutes; check '$0 screenshot'"
  log "installation done; sshd reachable"
  cmd_prepare --running
}

cmd_prepare() {
  local running=0; [ "${1:-}" = "--running" ] && running=1
  if [ "$running" = 0 ]; then
    [ -e "$BASE_DISK" ] || die "no VM; run create first"
    start_vm "$BASE_DISK" "$BASE_VARS" "$BASE_TPM"
    wait_ssh 600 || die "no ssh"
  fi
  # The first update after a fresh install pulls in a platform update: the
  # service restarts into the new platform and the definitions of that same
  # call are lost. Without a reboot the switch is not committed and every
  # boot repeats it, so: update, wait for the switch, reboot, then update the
  # definitions on the committed platform and verify.
  log "first update (may install a platform update)"
  update_definitions >/dev/null
  log "waiting for the platform switch to settle"
  wait_defender_ready 600 || die "Defender platform switch did not settle"
  log "rebooting to commit the platform"
  vm_ps 'Restart-Computer -Force' >/dev/null 2>&1 || true
  sleep 30
  wait_ssh 600 || die "no ssh after reboot"
  wait_defender_ready 600 || die "Defender not ready after reboot"
  log "updating definitions on the committed platform"
  local before after
  before="$(vm_ps '(Get-MpComputerStatus).AntivirusSignatureVersion' 2>/dev/null | tr -d '\r')"
  after="$(update_definitions)"
  log "definitions before ${before:-?}, ${after}"
  log "validating cloud connection"
  vm_ps '
    $ErrorActionPreference = "Continue"
    $p = Get-ChildItem "$env:ProgramData\Microsoft\Windows Defender\Platform" -Directory -ErrorAction SilentlyContinue | Sort-Object Name -Descending | Select-Object -First 1
    $mp = if ($p) { Join-Path $p.FullName "MpCmdRun.exe" } else { "$env:ProgramFiles\Windows Defender\MpCmdRun.exe" }
    & $mp -ValidateMapsConnection | Out-String
    Get-MpComputerStatus | Select-Object AMProductVersion, AMEngineVersion, AntivirusSignatureVersion, AntivirusSignatureLastUpdated, RealTimeProtectionEnabled, IsTamperProtected | Format-List | Out-String
    Get-MpPreference | Select-Object MAPSReporting, SubmitSamplesConsent, CloudBlockLevel, DisableRealtimeMonitoring | Format-List | Out-String
  '
  log "shutting down"
  vm_ps 'Stop-Computer -Force' >/dev/null 2>&1 || true
  wait_off 300
  log "prepare done; run '$0 clean' to freeze this state as the baseline"
}

cmd_clean() {
  [ -e "$BASE_DISK" ] || die "no VM; run create first"
  vm_running && die "stop the VM first"
  date -Is > "$CLEAN_STAMP"
  log "baseline frozen: $BASE_DISK ($(qemu-img info --output=json "$BASE_DISK" | python3 -c 'import json,sys; print(round(json.load(sys.stdin)["actual-size"]/2**30,1))') GiB used)"
}

cmd_scan() {
  local out="" minsig=""
  while [ $# -gt 0 ]; do case "$1" in --out) out="$2"; shift 2 ;; --min-signatures) minsig="$2"; shift 2 ;; -*) die "unknown option: $1" ;; *) break ;; esac; done
  [ $# -ge 1 ] || die "scan needs at least one file"
  [ -f "$CLEAN_STAMP" ] || die "no clean baseline; run create, prepare and clean first"
  local f; for f in "$@"; do [ -r "$f" ] || die "cannot read: $f"; done
  [ -n "$out" ] || out="$VMDIR/reports/$(date +%Y%m%d-%H%M%S)"
  mkdir -p "$out"

  fresh_run
  start_vm "$RUN/disk.qcow2" "$RUN/ovmf_vars.fd" "$RUN/tpm"
  wait_ssh 600 || { wait_off 60; die "no ssh"; }
  log "waiting for the Defender service to settle after boot"
  wait_defender_ready 300 || { wait_off 60; die "Defender service not ready"; }

  vm_ps 'Remove-Item -Recurse -Force C:\avscan\in -ErrorAction SilentlyContinue; New-Item -ItemType Directory -Force C:\avscan\in | Out-Null; (Get-Date -Format o) | Set-Content C:\avscan\scan-start' >/dev/null
  local names=()
  for f in "$@"; do
    local n; n="$(basename "$f")"; names+=("$n")
    log "copying $n into the VM (real-time protection judges it on write)"
    vm_put "$f" "C:/avscan/in/$n" || log "copy of $n failed or was blocked"
  done
  # Mark the copies as downloaded from the internet (Mark of the Web, zone 3)
  # so Defender and SmartScreen treat them like a browser download.
  vm_ps 'Get-ChildItem C:\avscan\in -File | ForEach-Object { Set-Content -Path $_.FullName -Stream Zone.Identifier -Value "[ZoneTransfer]`r`nZoneId=3`r`nReferrerUrl=https://github.com/`r`nHostUrl=https://github.com/" -ErrorAction SilentlyContinue }' >/dev/null 2>&1 || true
  local names_ps; names_ps="$(printf "'%s'," "${names[@]}")"; names_ps="${names_ps%,}"

  log "updating definitions and scanning"
  vm_ps "
    \$ErrorActionPreference = 'Continue'
    \$p = Get-ChildItem \"\$env:ProgramData\Microsoft\Windows Defender\Platform\" -Directory -ErrorAction SilentlyContinue | Sort-Object Name -Descending | Select-Object -First 1
    \$mp = if (\$p) { Join-Path \$p.FullName 'MpCmdRun.exe' } else { \"\$env:ProgramFiles\Windows Defender\MpCmdRun.exe\" }
    \$r = [ordered]@{}
    \$s0 = Get-MpComputerStatus
    \$r.signatures_before = [ordered]@{ version = \$s0.AntivirusSignatureVersion; updated = \$s0.AntivirusSignatureLastUpdated.ToString('o') }
    # MpCmdRun prints the versions from BEFORE the update; the result is read
    # from Get-MpComputerStatus afterwards.
    \$r.sigupdate = (& \$mp -SignatureUpdate 2>&1 | Out-String)
    \$r.sigupdate_exit = \$LASTEXITCODE
    \$r.maps = (& \$mp -ValidateMapsConnection 2>&1 | Out-String)
    \$s = Get-MpComputerStatus
    \$r.versions = [ordered]@{ platform = \$s.AMProductVersion; engine = \$s.AMEngineVersion; signatures = \$s.AntivirusSignatureVersion; signatures_updated = \$s.AntivirusSignatureLastUpdated.ToString('o'); realtime = \$s.RealTimeProtectionEnabled }
    \$pref = Get-MpPreference
    \$r.cloud = [ordered]@{ maps = \$pref.MAPSReporting; samples = \$pref.SubmitSamplesConsent; blocklevel = \$pref.CloudBlockLevel }
    Start-Sleep -Seconds 20
    \$r.files = @()
    foreach (\$n in @($names_ps)) {
      \$path = \"C:\avscan\in\\\$n\"
      \$e = [ordered]@{ name = \$n; present_after_copy = (Test-Path \$path) }
      if (\$e.present_after_copy) {
        \$e.scan_output = (& \$mp -Scan -ScanType 3 -File \$path -DisableRemediation 2>&1 | Out-String)
        \$e.scan_exit = \$LASTEXITCODE
      }
      \$r.files += \$e
    }
    \$since = Get-Date (Get-Content C:\avscan\scan-start)
    \$r.threats = @(Get-MpThreatDetection -ErrorAction SilentlyContinue | Where-Object { \$_.InitialDetectionTime -ge \$since.AddMinutes(-2) } | ForEach-Object {
      \$t = Get-MpThreat -ThreatID \$_.ThreatID -ErrorAction SilentlyContinue
      [ordered]@{ threat = \$t.ThreatName; severity = \$t.SeverityID; resources = \$_.Resources; time = \$_.InitialDetectionTime.ToString('o'); action = \$_.ActionSuccess; process = \$_.ProcessName }
    })
    \$r.events = @(Get-WinEvent -FilterHashtable @{ LogName = 'Microsoft-Windows-Windows Defender/Operational'; StartTime = \$since.AddMinutes(-2) } -ErrorAction SilentlyContinue |
      Where-Object { \$_.Id -in 1006,1007,1008,1015,1116,1117,1118,1119,1121,1122 } |
      ForEach-Object { [ordered]@{ time = \$_.TimeCreated.ToString('o'); id = \$_.Id; message = \$_.Message } })
    \$r.detected = (\$r.threats.Count -gt 0) -or (\$r.files | Where-Object { \$_.scan_exit -eq 2 }).Count -gt 0 -or (\$r.files | Where-Object { -not \$_.present_after_copy }).Count -gt 0
    \$r | ConvertTo-Json -Depth 6 | Set-Content -Path C:\avscan\report.json -Encoding utf8
    'report written'
  " || log "guest scan script reported errors"
  vm_get 'C:/avscan/report.json' "$out/report.json" || die "no report from the VM"

  log "shutting down and discarding the run"
  vm_ps 'Stop-Computer -Force' >/dev/null 2>&1 || true
  wait_off 300
  rm -rf "$RUN"

  local rc=0
  python3 - "$out/report.json" "$out/summary.md" "$minsig" <<'PY' || rc=$?
import json, sys
r = json.load(open(sys.argv[1], encoding="utf-8-sig"))
v, c, minsig = r["versions"], r["cloud"], sys.argv[3]
ver = lambda x: tuple(int(n) for n in x.split("."))
before = r.get("signatures_before", {}).get("version", "?")
stale = bool(minsig) and ver(v["signatures"]) < ver(minsig)
sig = f"signatures {v['signatures']} (updated {v.get('signatures_updated', '?')[:19]}, before the update {before}"
sig += f", required at least {minsig}" if minsig else ""
sig += ")"
lines = ["# Defender VM scan", "",
         f"- Platform {v['platform']}, engine {v['engine']}, {sig}, real-time protection {v['realtime']}",
         f"- Cloud: MAPS {c['maps']}, sample submission {c['samples']}, block level {c['blocklevel']}",
         f"- Cloud connection: {'ok' if 'ValidateMapsConnection successfully' in r['maps'] else 'NOT validated'}", ""]
for f in r["files"]:
    if not f["present_after_copy"]:
        lines.append(f"- **{f['name']}**: removed by real-time protection on copy")
    else:
        lines.append(f"- **{f['name']}**: MpCmdRun exit {f.get('scan_exit')} ({'threat found' if f.get('scan_exit') == 2 else 'clean' if f.get('scan_exit') == 0 else 'error'})")
if r["threats"]:
    lines += ["", "## Detections"] + [f"- {t['threat']} on {t['resources']} at {t['time']}" for t in r["threats"]]
if stale:
    lines += ["", f"Result: FAILED, definitions {v['signatures']} are older than the required {minsig}; the verdict above does not count"]
else:
    lines += ["", f"Result: {'DETECTION' if r['detected'] else 'clean'}"]
open(sys.argv[2], "w").write("\n".join(lines) + "\n")
print("\n".join(lines))
sys.exit(1 if stale else 10 if r["detected"] else 0)
PY
  log "report: $out"
  return $rc
}

cmd_status() {
  if vm_running; then echo "running (pid $(vm_pid)); ssh port $SSH_PORT${VNC:+; vnc $VNC}"; else echo "stopped"; fi
  [ -e "$BASE_DISK" ] && echo "disk: $BASE_DISK" || echo "no VM created"
  [ -f "$CLEAN_STAMP" ] && echo "clean baseline: $(cat "$CLEAN_STAMP")" || echo "no clean baseline yet"
}

cmd_screenshot() {
  vm_running || die "VM not running"
  local f="${1:-$VMDIR/screen-$(date +%H%M%S).png}"
  qmp screendump "{\"filename\":\"$f\",\"format\":\"png\"}" >/dev/null
  echo "$f"
}

cmd_ssh() { vm_running || die "VM not running"; if [ $# -gt 0 ]; then vm_ps "$*"; else ssh $(ssh_opts | sed 's/-o BatchMode=yes//') -t "$GUEST_USER@127.0.0.1"; fi; }

cmd_stop() {
  vm_running || { log "not running"; return 0; }
  vm_ps 'Stop-Computer -Force' >/dev/null 2>&1 || qmp system_powerdown >/dev/null 2>&1 || true
  wait_off 300
  log "stopped"
}

cmd_destroy() {
  vm_running && cmd_stop
  rm -rf "$VMDIR"
  log "removed $VMDIR"
}

case "${1:-}" in
  create) shift; cmd_create "$@" ;;
  prepare) shift; cmd_prepare "$@" ;;
  clean) cmd_clean ;;
  scan) shift; cmd_scan "$@" ;;
  status) cmd_status ;;
  screenshot) shift; cmd_screenshot "$@" ;;
  ssh) shift; cmd_ssh "$@" ;;
  stop) cmd_stop ;;
  destroy) cmd_destroy ;;
  *) sed -n '2,25p' "$0" | sed 's/^# \{0,1\}//'; exit 1 ;;
esac
