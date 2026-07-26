#!/usr/bin/env bash
# Run integration tests against DeckBridge + mock-bridge.
# Usage: scripts/run-integration-tests.sh [--keep-alive]
#
# Steps:
#   1. Deploy plugin (+ mock-bridge) to DeckBridge and restart DeckBridge
#   2. Wait for the mock-bridge control API on :9999
#   3. Run tests/integration/*.js
#   4. Tear down unless --keep-alive is passed
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$SCRIPT_DIR/.."
KEEP_ALIVE=false

for arg in "$@"; do
  [[ "$arg" == "--keep-alive" ]] && KEEP_ALIVE=true
done

# The key-index layout of these tests (keys 0..70) needs the 32-key XL
# emulator device; DeckBridge picks its primary device from this env var.
export DECKBRIDGE_DEVICE_PROFILE=streamdeck-xl

# ── cleanup on exit ────────────────────────────────────────────────────────────
cleanup() {
  echo ""
  echo "── cleanup ──"
  if [[ "$KEEP_ALIVE" == "false" ]]; then
    pkill -9 -f "dist/index.js" 2>/dev/null || true
    pkill -9 -f "sdPlugin/hwinfo" 2>/dev/null || true
    pkill -9 -f "sdPlugin/mock-bridge" 2>/dev/null || true
    echo "DeckBridge stopped"
  else
    echo "DeckBridge kept alive (--keep-alive)"
  fi
}
trap cleanup EXIT

# ── 1. Deploy + start DeckBridge (builds plugin + mock-bridge) ────────────────
echo "── DeckBridge deploy ──"
bash "$SCRIPT_DIR/deploy-deckbridge.sh"
echo "DeckBridge deploy: OK"

# ── 2. Wait for mock-bridge control API ───────────────────────────────────────
# The plugin process (spawned by DeckBridge) starts mock-bridge, which serves
# the control API on :9999.
echo "── mock-bridge control API ──"
MOCK_OK=false
for _ in $(seq 1 40); do
  if curl -sf http://127.0.0.1:9999/list >/dev/null 2>&1; then
    MOCK_OK=true
    break
  fi
  sleep 0.5
done
if [[ "$MOCK_OK" != "true" ]]; then
  echo "ERROR: mock-bridge control API not responding on :9999"
  exit 1
fi
echo "mock-bridge: OK"

# ── 3. Run tests ───────────────────────────────────────────────────────────────
echo ""
echo "── integration tests ──"
cd "$ROOT"

# The repo ships no node_modules; resolve 'ws' from the DeckBridge checkout.
export NODE_PATH="${NODE_PATH:-$HOME/projects/GitHub/DeckBridge/node_modules}"

OVERALL_EXIT=0

for TEST_FILE in \
  tests/integration/test-global-thresholds.js \
  tests/integration/test-per-tile-thresholds.js \
  tests/integration/test-composite-thresholds.js \
  tests/integration/test-derived-thresholds.js \
  tests/integration/test-composite-global-suppress.js \
  tests/integration/test-settings-tile.js \
  tests/integration/test-favorites.js \
  tests/integration/test-remote-sources.js; do
  echo ""
  echo "── $TEST_FILE ──"
  node "$TEST_FILE"
  FILE_EXIT=$?
  if [[ $FILE_EXIT -ne 0 ]]; then
    OVERALL_EXIT=$FILE_EXIT
    echo "FAILED: $TEST_FILE (exit $FILE_EXIT)"
  fi
done

echo ""
if [[ $OVERALL_EXIT -eq 0 ]]; then
  echo "ALL TESTS PASSED"
else
  echo "SOME TESTS FAILED"
fi

exit $OVERALL_EXIT
