#!/usr/bin/env bash
# Deploy the plugin to DeckBridge (Linux test harness) and restart the daemon.
# On Linux the plugin spawns ./mock-bridge, which serves mock HWiNFO-style
# sensors over the same gRPC interface as the real hwinfo-bridge, plus a
# control API on :9999 (/set, /reset, /list) for the integration tests.
# Usage: scripts/deploy-deckbridge.sh
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
plugin_src="$root_dir/com.moeilijk.hwinfo.sdPlugin"
plugin_dst="$HOME/.config/DeckBridge/plugins/com.moeilijk.hwinfo.sdPlugin"
deckbridge_dir="$HOME/projects/GitHub/DeckBridge"
log_file="/tmp/deckbridge.log"

# ── build ──────────────────────────────────────────────────────────────────
echo "build: $plugin_src/hwinfo + mock-bridge (linux)"
(
  cd "$root_dir"
  GOOS=linux GOARCH=amd64 go build -o "$plugin_src/hwinfo"      ./cmd/hwinfo_streamdeck_plugin
  GOOS=linux GOARCH=amd64 go build -o "$plugin_src/mock-bridge" ./cmd/mock-bridge
  chmod +x "$plugin_src/hwinfo" "$plugin_src/mock-bridge"
)

# ── stop existing DeckBridge daemon ────────────────────────────────────────
echo "stop: DeckBridge and everything it started"
# stop-deckbridge.sh finds the processes through /proc (working directory and
# executable, not command lines) and also stops the helpers of the other
# plugins DeckBridge loads. It uses SIGKILL and fails when a process or the
# daemon port survives, so a stale daemon can never keep serving old code.
# The two executables are named as well for the layout where the plugin
# directory of DeckBridge is a symlink to this checkout.
bash "$root_dir/scripts/stop-deckbridge.sh" "$plugin_src/hwinfo" "$plugin_src/mock-bridge"

# A bridge that outlives its plugin keeps the control port: the new bridge
# then cannot bind it, and the tests steer the old one while the plugin reads
# the new one.
if ss -ltn 2>/dev/null | grep -q ":${MOCK_CONTROL_PORT:-9999} "; then
  echo "ERROR: port ${MOCK_CONTROL_PORT:-9999} is still in use; the mock-bridge control API cannot start" >&2
  exit 1
fi

# ── deploy plugin files ────────────────────────────────────────────────────
echo "copy: $plugin_src -> $plugin_dst"
mkdir -p "$plugin_dst"
rsync -a --delete "$plugin_src/" "$plugin_dst/"

# Inject CodePathLinux into the deployed manifest so DeckBridge runs the
# Linux plugin binary. Skip when plugin_dst is a symlink pointing back at the
# source (development layout), to avoid corrupting the committed manifest.json.
plugin_src_real=$(realpath "$plugin_src")
plugin_dst_real=$(realpath "$plugin_dst" 2>/dev/null || echo "")
if [[ "$plugin_src_real" == "$plugin_dst_real" ]]; then
  echo "note: plugin_dst is symlinked to source — skipping CodePathLinux injection"
else
  python3 - "$plugin_dst/manifest.json" <<'PYEOF'
import json, sys
path = sys.argv[1]
with open(path) as f:
    m = json.load(f)
m['CodePathLinux'] = 'hwinfo'
with open(path, 'w') as f:
    json.dump(m, f, indent=2)
PYEOF
fi

# ── start DeckBridge daemon ─────────────────────────────────────────────────
echo "start: DeckBridge"
cd "$deckbridge_dir"
node dist/index.js >"$log_file" 2>&1 &
daemon_pid=$!

# wait for dashboard URL to appear in log (max 15s)
echo "waiting for dashboard..."
for i in $(seq 1 30); do
  if grep -q "Dashboard:" "$log_file" 2>/dev/null; then
    break
  fi
  sleep 0.5
done

dashboard_url=$(grep "Dashboard:" "$log_file" 2>/dev/null | tail -1 | sed 's/.*Dashboard: //')
if [[ -z "$dashboard_url" ]]; then
  echo "DeckBridge started (PID $daemon_pid) but dashboard URL not found yet."
  echo "Check: $log_file"
else
  echo ""
  echo "DeckBridge PID: $daemon_pid"
  echo "Dashboard:      $dashboard_url"
  echo "Log:            $log_file"
fi
