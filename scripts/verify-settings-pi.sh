#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root_dir"

echo "check: settings_pi.js syntax"
node --check com.moeilijk.hwinfo.sdPlugin/settings_pi.js

echo "check: pi_utils.js syntax"
node --check com.moeilijk.hwinfo.sdPlugin/pi_utils.js

echo "check: index_pi.js syntax"
node --check com.moeilijk.hwinfo.sdPlugin/index_pi.js

echo "check: composite_pi.js syntax"
node --check com.moeilijk.hwinfo.sdPlugin/composite_pi.js

echo "check: derived_pi.js syntax"
node --check com.moeilijk.hwinfo.sdPlugin/derived_pi.js

echo "check: dial_pi.js syntax"
node --check com.moeilijk.hwinfo.sdPlugin/dial_pi.js

echo "test: settings PI functional script"
node scripts/test-settings-pi.js

echo "test: reading PI functional script"
node scripts/test-reading-pi.js

echo "test: dial PI functional script"
node scripts/test-dial-pi.js

if node -e "require('../DeckBridge/node_modules/jsdom')" >/dev/null 2>&1 \
  || node -e "require('jsdom')" >/dev/null 2>&1; then
  echo "test: dial bulk render (jsdom, real DOM)"
  node scripts/test-dial-bulk-render.js
  echo "test: dial indicator-fullscreen render (jsdom, real CSS)"
  node scripts/test-dial-indicator-fullscreen-render.js
else
  echo "skip: dial bulk render (jsdom not reachable)"
fi

# Live, non-destructive e2e against a running DeckBridge + plugin (real catalog,
# real WebSocket, real DOM). Self-skips with exit 0 when DeckBridge/jsdom is absent.
echo "test: dial bulk live e2e (skips if DeckBridge not running)"
node scripts/test-dial-bulk-live-e2e.js

echo "test: dial indicator-fullscreen live e2e (skips if DeckBridge not running)"
node scripts/test-dial-indicator-fullscreen-live-e2e.js

echo "test: dial stacked overview live e2e (skips if DeckBridge not running)"
node scripts/test-dial-stacked-live-e2e.js

echo "test: dial reverse-rotation live e2e (skips if DeckBridge not running)"
node scripts/test-dial-reverse-live-e2e.js

# Text-rendering tests load DejaVuSans-Bold.ttf from the package CWD; provide it
# for the dial package and clean it up afterwards.
dial_pkg="internal/app/hwinfostreamdeckplugin"
if [ ! -f "$dial_pkg/DejaVuSans-Bold.ttf" ]; then
  cp DejaVuSans-Bold.ttf "$dial_pkg/DejaVuSans-Bold.ttf"
  trap 'rm -f "'"$dial_pkg"'/DejaVuSans-Bold.ttf"' EXIT
fi
# On WSL the interop layer can execute Windows test binaries, so we test the
# real GOOS=windows build. Elsewhere (CI on plain Linux) run the same packages
# with the host GOOS; the windows compile itself is covered by the build step.
if [ -f /proc/sys/fs/binfmt_misc/WSLInterop ]; then
  echo "test: Go targets (windows via WSL interop)"
  GOOS=windows GOARCH=amd64 GOCACHE=/tmp/go-build go test \
    ./cmd/hwinfo_streamdeck_plugin \
    ./internal/app/hwinfostreamdeckplugin \
    ./pkg/streamdeck \
    ./cmd/mock-bridge
else
  echo "test: Go targets (host GOOS)"
  GOCACHE=/tmp/go-build go test \
    ./cmd/hwinfo_streamdeck_plugin \
    ./internal/app/hwinfostreamdeckplugin \
    ./pkg/streamdeck \
    ./cmd/mock-bridge
fi

echo "verify-settings-pi: ok"
