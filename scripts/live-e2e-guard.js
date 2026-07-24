"use strict";

// Shared guard for the dial live e2e tests.
//
// A run with no data cannot validate anything, so "infrastructure down / no data"
// must NOT read as a pass. noData() FAILS by default (exit 1). Set
// DECKBRIDGE_OPTIONAL=1 (e.g. a CI host that genuinely has no emulator) to downgrade
// those conditions to a real skip instead.
const OPTIONAL = process.env.DECKBRIDGE_OPTIONAL === "1";

function noData(label, reason) {
  if (OPTIONAL) {
    console.log("skip: " + label + " (" + reason + ")");
    return 0;
  }
  console.error("FAIL - " + label + ": no data to validate against (" + reason + ").");
  console.error("       Start DeckBridge + plugin, or set DECKBRIDGE_OPTIONAL=1 to allow skipping.");
  return 1;
}

module.exports = { OPTIONAL, noData };
