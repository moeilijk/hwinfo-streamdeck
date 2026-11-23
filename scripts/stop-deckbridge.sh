#!/usr/bin/env bash
# Stop the DeckBridge emulator and everything it started, of every plugin.
# Usage: scripts/stop-deckbridge.sh [executable ...]
#
# DeckBridge loads all plugins in ~/.config/DeckBridge/plugins, not only this
# one, and each plugin starts helpers of its own (a bridge, a sensor source).
# Killing only the daemon, or only this plugin's processes, leaves the rest
# running as orphans that keep their ports. So this stops:
#
#   1. every DeckBridge daemon (node dist/index.js in the DeckBridge directory)
#      with all its descendants,
#   2. every process that runs from a plugin directory of DeckBridge, which
#      catches orphans left behind by an earlier, less careful stop,
#   3. every process whose executable is one of the paths given as arguments
#      (helpers started next to the emulator, such as a mock sensor source).
#
# Processes are found through /proc (working directory and executable), never
# by matching command lines: a command-line match also hits the shell that
# runs the script whenever the pattern happens to be in its own arguments.
#
# Exits 1 when something is still running or a port is still held afterwards.
set -uo pipefail

deckbridge_dir="${DECKBRIDGE_DIR:-$HOME/projects/GitHub/DeckBridge}"
plugins_dir="${DECKBRIDGE_PLUGINS_DIR:-$HOME/.config/DeckBridge/plugins}"
daemon_port="${DECKBRIDGE_PORT:-34075}"

deckbridge_real="$(realpath -m "$deckbridge_dir")"
plugins_real="$(realpath -m "$plugins_dir")"
extra=()
for exe in "$@"; do extra+=("$(realpath -m "$exe")"); done

proc_cwd() { readlink "/proc/$1/cwd" 2>/dev/null || true; }
proc_exe() { readlink "/proc/$1/exe" 2>/dev/null | sed 's/ (deleted)$//' || true; }
proc_comm() { cat "/proc/$1/comm" 2>/dev/null || true; }

all_pids() { ls /proc | grep -E '^[0-9]+$'; }

is_daemon() {
  [[ "$(proc_comm "$1")" == "node" ]] || return 1
  [[ "$(proc_cwd "$1")" == "$deckbridge_real" ]] || return 1
  tr '\0' '\n' < "/proc/$1/cmdline" 2>/dev/null | grep -qx 'dist/index.js'
}

descendants() {
  local child
  for child in $(pgrep -P "$1" 2>/dev/null); do
    echo "$child"
    descendants "$child"
  done
}

under() { [[ "$1" == "$2" || "$1" == "$2"/* ]]; }

targets() {
  local pid exe cwd e
  for pid in $(all_pids); do
    [[ "$pid" == "$$" || "$pid" == "$BASHPID" ]] && continue
    if is_daemon "$pid"; then
      echo "$pid"
      descendants "$pid"
      continue
    fi
    cwd="$(proc_cwd "$pid")"
    exe="$(proc_exe "$pid")"
    if [[ -n "$cwd" ]] && under "$cwd" "$plugins_real"; then echo "$pid"; continue; fi
    if [[ -n "$exe" ]] && under "$exe" "$plugins_real"; then echo "$pid"; continue; fi
    if [[ -n "$cwd" ]] && under "$cwd" "$deckbridge_real/test-plugin"; then echo "$pid"; continue; fi
    for e in "${extra[@]:-}"; do
      [[ -n "$e" && "$exe" == "$e" ]] && echo "$pid"
    done
  done | sort -un
}

describe() { printf '%s %s (%s)' "$1" "$(proc_comm "$1")" "$(proc_cwd "$1" | sed "s|^$HOME|~|")"; }

found="$(targets)"
if [[ -z "$found" ]]; then
  echo "stop-deckbridge: nothing running"
else
  for pid in $found; do echo "stop-deckbridge: stopping $(describe "$pid")"; done
  # SIGKILL: the DeckBridge daemon installs a SIGTERM handler that does not
  # reliably exit.
  # shellcheck disable=SC2086
  kill -9 $found 2>/dev/null || true
fi

# A helper may be started by a plugin in the moment its daemon dies, so look
# again until nothing is left.
for _ in $(seq 1 20); do
  left="$(targets)"
  [[ -z "$left" ]] && break
  # shellcheck disable=SC2086
  kill -9 $left 2>/dev/null || true
  sleep 0.25
done

status=0
left="$(targets)"
if [[ -n "$left" ]]; then
  for pid in $left; do echo "stop-deckbridge: STILL RUNNING $(describe "$pid")" >&2; done
  status=1
fi
for _ in $(seq 1 20); do
  ss -ltn 2>/dev/null | grep -q ":$daemon_port " || break
  sleep 0.25
done
if ss -ltn 2>/dev/null | grep -q ":$daemon_port "; then
  echo "stop-deckbridge: port $daemon_port is still in use" >&2
  status=1
fi
exit "$status"
