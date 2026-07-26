#!/usr/bin/env node
"use strict";

// TRUE end-to-end test for the dial reverse-rotation toggle against a RUNNING
// DeckBridge + plugin. It reaches the actual encoder path, not a unit helper:
//   PI sets reverseRotation -> settings PERSIST to the plugin -> a real
//   /api/dials/rotate with +1 tick steps to the PREVIOUS page instead of the next.
//
// A unit test of dialRotateTicks alone passed while the emu ran a STALE plugin
// binary and reverse did nothing; this test exists to catch exactly that gap.
//
// Non-destructive: it snapshots the target dial's settings and RESTORES them.
// Guarded: skips cleanly (exit 0) when DeckBridge/jsdom/ws is not reachable.

const fs = require("fs");
const path = require("path");

const { noData } = require("./live-e2e-guard");

const repoRoot = path.resolve(__dirname, "..");
const BASE = process.env.DECKBRIDGE_URL || "http://127.0.0.1:34075";
const PLUGIN_ID = "com.moeilijk.hwinfo";
const LABEL = "dial reverse live e2e";

function loadDep(name) {
  for (const c of [name, path.resolve(repoRoot, "node_modules", name), path.resolve(repoRoot, "../DeckBridge/node_modules", name)]) {
    try { return require(c); } catch (e) { /* next */ }
  }
  return null;
}

const jsdomMod = loadDep("jsdom");
const WebSocket = loadDep("ws");
const piHtml = fs.readFileSync(path.join(repoRoot, "com.moeilijk.hwinfo.sdPlugin/dial_pi.html"), "utf8");
const piUtils = fs.readFileSync(path.join(repoRoot, "com.moeilijk.hwinfo.sdPlugin/pi_utils.js"), "utf8");
const dialPi = fs.readFileSync(path.join(repoRoot, "com.moeilijk.hwinfo.sdPlugin/dial_pi.js"), "utf8");

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const getState = async () => (await fetch(`${BASE}/api/state`)).json();
const dialSlots = (s) => (s.slots || []).filter((x) => x && String(x.actionId || x.action || "") === "com.moeilijk.hwinfo.dial");
const slotByContext = async (ctx) => (await getState()).slots.find((s) => s.context === ctx);

async function rotate(deviceId, dialIndex, ticks) {
  const r = await fetch(`${BASE}/api/dials/rotate`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ deviceId, dialIndex, ticks }),
  });
  if (!r.ok) throw new Error("rotate " + r.status + " " + (await r.text()));
}

async function bootLivePi(slot) {
  const { JSDOM } = jsdomMod;
  const url = `${BASE}/${PLUGIN_ID}/dial_pi.html?context=${slot.context}&wsPort=34075`;
  const dom = new JSDOM(piHtml, { runScripts: "dangerously", pretendToBeVisual: true, url });
  const win = dom.window;
  win.WebSocket = WebSocket;
  win.eval(piUtils);
  win.eval(dialPi);
  win.document.dispatchEvent(new win.Event("DOMContentLoaded"));
  const actionInfo = { action: slot.actionId, context: slot.context, device: slot.deviceId || "deckbridge-plus-0", payload: { settings: {} } };
  win.connectElgatoStreamDeckSocket("34075", slot.context, "registerPropertyInspector", "{}", JSON.stringify(actionInfo));
  for (let i = 0; i < 50; i++) {
    if (((win.currentCatalog || {}).readings || []).length > 0) return win;
    await sleep(100);
  }
  throw new Error("catalog never arrived from the live plugin");
}

async function applySettings(win, mutate) {
  mutate(win.currentSettings);
  win.saveSettings();
  await sleep(400);
}

async function main() {
  if (!jsdomMod || !WebSocket) return noData(LABEL, "jsdom/ws not installed");
  let state;
  try { state = await getState(); } catch (e) {
    return noData(LABEL, "DeckBridge not reachable at " + BASE);
  }
  // Direction is only observable with >=3 pages (with 2, +1 and -1 both wrap to the
  // same index). Pick the dial with the most pages.
  const target = dialSlots(state)
    .filter((s) => (((s.settings || {}).pages) || []).length >= 3)
    .sort((a, b) => ((b.settings.pages || []).length - (a.settings.pages || []).length))[0];
  if (!target) return noData(LABEL, "no dial with >=3 pages to show rotation direction");
  const ctx = target.context;
  const deviceId = target.deviceId || "deckbridge-plus-0";
  const dialIndex = target.keyIndex >= 1000 ? target.keyIndex - 1000 : target.keyIndex;
  const N = (target.settings.pages || []).length;
  const start = 2 % N;
  const original = JSON.parse(JSON.stringify(target.settings || {}));

  const win = await bootLivePi(target);
  let failures = 0;
  const ok = (name, cond) => { console.log((cond ? "ok   - " : "FAIL - ") + name); if (!cond) failures++; };

  try {
    // reverse OFF: a +1 (clockwise) tick steps FORWARD.
    await applySettings(win, (s) => { s.reverseRotation = false; s.activeIndex = start; });
    await rotate(deviceId, dialIndex, 1);
    await sleep(400);
    const fwd = (await slotByContext(ctx)).settings.activeIndex;
    ok(`reverse OFF: +1 tick goes forward ${start}->${(start + 1) % N} (got ${fwd})`, fwd === (start + 1) % N);

    // reverse ON: the same +1 tick steps BACKWARD.
    await applySettings(win, (s) => { s.reverseRotation = true; s.activeIndex = start; });
    const persisted = (await slotByContext(ctx)).settings.reverseRotation;
    ok("reverseRotation persisted to the plugin", persisted === true);
    await rotate(deviceId, dialIndex, 1);
    await sleep(400);
    const back = (await slotByContext(ctx)).settings.activeIndex;
    ok(`reverse ON: +1 tick goes backward ${start}->${(start - 1 + N) % N} (got ${back})`, back === (start - 1 + N) % N);
  } finally {
    win.currentSettings = JSON.parse(JSON.stringify(original));
    win.saveSettings();
    await sleep(400);
    const r = (await slotByContext(ctx)).settings || {};
    const n = (r.pages || []).length;
    console.log(n === N ? `cleanup: dial restored (${N} pages, reverse=${r.reverseRotation})` : `cleanup WARNING: ${n} pages, expected ${N}`);
    if (n !== N) failures++;
  }
  return failures > 0 ? 1 : 0;
}

main().then((code) => process.exit(code)).catch((e) => {
  console.error("dial reverse live e2e error: " + (e && e.stack ? e.stack : e));
  process.exit(1);
});
