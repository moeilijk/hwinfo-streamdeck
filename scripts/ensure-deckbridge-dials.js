#!/usr/bin/env node
"use strict";

// Make sure the emulated Stream Deck + has the two dial slots the live dial
// e2e suites in `make verify` need: dial 0 without pages and dial 1 with at
// least three pages. A slot that is already there is left as it is.
//
// The suites take their data from the dial on the deck and fail (not skip)
// when there is none, so without this a fresh or cleaned DeckBridge profile
// breaks `make verify`. Called by scripts/deploy-deckbridge.sh.

const http = require("http");
const path = require("path");

const repoRoot = path.resolve(__dirname, "..");
const H = require(path.join(repoRoot, "tests/integration/helpers.js"));
const WebSocket = require(path.resolve(repoRoot, "../DeckBridge/node_modules/ws"));

const DEVICE = "deckbridge-plus-0";
const ACTION = "com.moeilijk.hwinfo.dial";
const EMPTY_DIAL = 1000;
const PAGES_DIAL = 1001;
const WANT_PAGES = 3;

function getState(piPort) {
  return new Promise((resolve, reject) => {
    http
      .get(`http://127.0.0.1:${piPort}/api/state`, (res) => {
        let out = "";
        res.on("data", (c) => (out += c));
        res.on("end", () => {
          try {
            resolve(JSON.parse(out));
          } catch (e) {
            reject(e);
          }
        });
      })
      .on("error", reject);
  });
}

const dialAt = (state, keyIndex) => (state.slots || []).find((s) => s.deviceId === DEVICE && s.keyIndex === keyIndex && s.actionId === ACTION);
const pagesOf = (slot) => ((slot && slot.settings && slot.settings.pages) || []).length;

async function ensureSlot(piPort, keyIndex) {
  let slot = dialAt(await getState(piPort), keyIndex);
  if (slot) return slot;
  const state = await H.httpPost(piPort, "/api/slots", { deviceId: DEVICE, keyIndex, pluginId: "com.moeilijk.hwinfo", actionId: ACTION });
  slot = dialAt(state, keyIndex);
  if (!slot) throw new Error(`dial slot ${keyIndex} was not created: ${JSON.stringify(state).slice(0, 200)}`);
  console.log(`dials: created dial ${keyIndex - 1000} on ${DEVICE}`);
  return slot;
}

// The first readings of the live catalog, as dial pages.
function catalogPages(wsPort, context) {
  return new Promise((resolve, reject) => {
    const ws = new WebSocket(`ws://localhost:${wsPort}`);
    const timer = setTimeout(() => {
      ws.close();
      reject(new Error("no sensor catalog from the plugin (is a sensor source running?)"));
    }, 15000);
    ws.on("error", reject);
    ws.on("open", () => {
      ws.send(JSON.stringify({ event: "registerPropertyInspector", uuid: context }));
      ws.send(JSON.stringify({ event: "sendToPlugin", context, action: ACTION, payload: { requestDialCatalog: true } }));
    });
    ws.on("message", (raw) => {
      const pl = JSON.parse(raw).payload || {};
      const readings = (pl.catalog && pl.catalog.readings) || [];
      if (readings.length < WANT_PAGES) return;
      clearTimeout(timer);
      const pages = readings.slice(0, WANT_PAGES).map((r) => ({
        sourceProfileId: "",
        sensorUid: r.sensorUid,
        readingId: String(r.id),
        readingLabel: r.label,
        title: r.label,
        min: 0,
        max: 100,
        format: "",
        divisor: "",
        graphUnit: "",
        isValid: true,
      }));
      ws.send(JSON.stringify({ event: "sendToPlugin", context, action: ACTION, payload: { dialSetSettings: { pages, activeIndex: 0 } } }));
      setTimeout(() => {
        ws.close();
        resolve();
      }, 500);
    });
  });
}

async function main() {
  const { wsPort, piPort } = await H.waitForDeckBridge(20000);
  await ensureSlot(piPort, EMPTY_DIAL);
  const paged = await ensureSlot(piPort, PAGES_DIAL);
  if (pagesOf(paged) < WANT_PAGES) {
    await H.sleep(1500); // let the plugin register the new slot
    await catalogPages(wsPort, paged.context);
    const deadline = Date.now() + 10000;
    while (Date.now() < deadline && pagesOf(dialAt(await getState(piPort), PAGES_DIAL)) < WANT_PAGES) await H.sleep(300);
    console.log(`dials: gave dial ${PAGES_DIAL - 1000} ${WANT_PAGES} pages`);
  }
  const state = await getState(piPort);
  const empty = dialAt(state, EMPTY_DIAL);
  const withPages = dialAt(state, PAGES_DIAL);
  console.log(`dials: dial 0 has ${pagesOf(empty)} pages, dial 1 has ${pagesOf(withPages)} pages`);
  if (!empty || pagesOf(withPages) < WANT_PAGES) {
    throw new Error("the dials for the live dial e2e suites are not in place");
  }
}

main().then(
  () => process.exit(0),
  (e) => {
    console.error("ensure-deckbridge-dials: " + (e && e.message ? e.message : e));
    process.exit(1);
  }
);
