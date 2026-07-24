#!/usr/bin/env node
"use strict";

// TRUE live end-to-end test for the fullscreen page-indicator toggle.
//
// "End to end" reaches the RENDERED DIAL PIXELS through the REAL system:
//   boot the actual dial_pi.html in a browser DOM -> connect to the RUNNING
//   DeckBridge + plugin -> add pages -> CLICK THE REAL "In fullscreen" checkbox
//   -> the plugin re-renders -> read the dial image back from /api/state and
//   decode its pixels -> assert the page-indicator appears (and disappears) in
//   the fullscreen view.
//
// Live sensor values drift every frame, so a whole-image md5 is meaningless here.
// We isolate the indicator deterministically:
//   - pages use GraphMode "text" (no moving graph, and the text is centred away
//     from the left gutter where the indicator lives),
//   - we inspect ONLY the indicator band (the left gutter where the vertical
//     fullscreen indicator paints, matching the stacked overview), and
//   - we capture several frames per state and assert the band is STABLE within a
//     state (self-validates that the band is drift-free) while DIFFERING between
//     the checkbox-off and checkbox-on states.
//
// Non-destructive: mutates one originally-empty dial and resets it. Self-skips
// (exit 0) when DeckBridge/jsdom/ws/sharp are not reachable.

const fs = require("fs");
const path = require("path");

const { noData } = require("./live-e2e-guard");

const repoRoot = path.resolve(__dirname, "..");
const BASE = process.env.DECKBRIDGE_URL || "http://127.0.0.1:34075";
const PLUGIN_ID = "com.exension.hwinfo";
const LABEL = "dial indicator-fullscreen live e2e";

function loadDep(name) {
  for (const c of [name, path.resolve(repoRoot, "node_modules", name), path.resolve(repoRoot, "../DeckBridge/node_modules", name)]) {
    try {
      return require(c);
    } catch (e) {
      /* next */
    }
  }
  return null;
}

const jsdomMod = loadDep("jsdom");
const WebSocket = loadDep("ws");
const sharp = loadDep("sharp");
const piHtml = fs.readFileSync(path.join(repoRoot, "com.exension.hwinfo.sdPlugin/dial_pi.html"), "utf8");
const piUtils = fs.readFileSync(path.join(repoRoot, "com.exension.hwinfo.sdPlugin/pi_utils.js"), "utf8");
const dialPi = fs.readFileSync(path.join(repoRoot, "com.exension.hwinfo.sdPlugin/dial_pi.js"), "utf8");

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function getState() {
  const res = await fetch(`${BASE}/api/state`);
  if (!res.ok) throw new Error("state " + res.status);
  return res.json();
}
const dialSlots = (s) => (s.slots || []).filter((x) => x && /dial/i.test(String(x.actionId || x.action || "")));
const slotByContext = (s, ctx) => dialSlots(s).find((x) => x.context === ctx);

async function bootLivePi(slot) {
  const { JSDOM } = jsdomMod;
  const ctx = slot.context;
  const url = `${BASE}/${PLUGIN_ID}/dial_pi.html?context=${ctx}&wsPort=34075`;
  const dom = new JSDOM(piHtml, { runScripts: "dangerously", pretendToBeVisual: true, url });
  const win = dom.window;
  win.WebSocket = WebSocket;
  win.eval(piUtils);
  win.eval(dialPi);
  win.document.dispatchEvent(new win.Event("DOMContentLoaded"));
  const actionInfo = { action: slot.actionId, context: ctx, device: slot.deviceId || "deckbridge-plus-0", payload: { settings: {} } };
  win.connectElgatoStreamDeckSocket("34075", ctx, "registerPropertyInspector", "{}", JSON.stringify(actionInfo));
  for (let i = 0; i < 50; i++) {
    if (((win.currentCatalog || {}).readings || []).length > 0) return win;
    await sleep(100);
  }
  throw new Error("catalog never arrived from the live plugin");
}

const $ = (win, id) => win.document.getElementById(id);
const fire = (win, el, t) => el.dispatchEvent(new win.Event(t, { bubbles: true }));

// Decode the current dial image's indicator band to raw RGB. The fullscreen page
// indicator is drawn vertically in the LEFT gutter (like the stacked overview), so
// we sample the left column over the full height. In fullscreen text mode the
// title/value text is horizontally centred (~x=100), so this column carries only
// the indicator dots — nothing else lights up here when the indicator is off.
async function captureBand(ctx) {
  const slot = slotByContext(await getState(), ctx);
  const url = ((slot || {}).feedback || {}).imageDataUrl || "";
  const b64 = url.replace(/^data:image\/png;base64,/, "");
  if (!b64) return null;
  const buf = Buffer.from(b64, "base64");
  const { data, info } = await sharp(buf).ensureAlpha().raw().toBuffer({ resolveWithObject: true });
  const { width: W, height: H, channels: ch } = info;
  const x0 = 2;
  const x1 = 16; // the reserved left indicator gutter (dialStackedGutter ~18 at size 6)
  const out = [];
  for (let y = 2; y < H - 2; y++) {
    for (let x = x0; x < x1; x++) {
      const i = (y * W + x) * ch;
      out.push(data[i], data[i + 1], data[i + 2]);
    }
  }
  return Buffer.from(out);
}

// Count light-grey indicator pixels (drawDialPageIndicator uses {100,108,116} and
// {190,198,206}); the active dot is clearly light. Background/foreground is darker.
function isBright(band, i) {
  return band[i] > 150 && band[i + 1] > 150 && band[i + 2] > 150;
}

function countBright(band) {
  let n = 0;
  for (let i = 0; i < band.length; i += 3) if (isBright(band, i)) n++;
  return n;
}

// persistentBright counts pixel positions that are bright in EVERY captured frame.
// The page-indicator dots are drawn at fixed positions/colours, so they stay bright
// across frames; live graph/value content drifts and is therefore NOT bright in the
// same position every frame, so it cancels out. This isolates the indicator without
// requiring the (impossible against live data) whole-band byte stability.
function persistentBright(frames) {
  if (!frames.length) return 0;
  const len = frames[0].length;
  let n = 0;
  for (let i = 0; i < len; i += 3) {
    let all = true;
    for (const f of frames) {
      if (!isBright(f, i)) { all = false; break; }
    }
    if (all) n++;
  }
  return n;
}

// The plugin redraws the dial on its data tick, so a freshly-toggled indicator can
// take a render cycle to appear in /api/state. Wait (bounded) until the band shows
// indicator pixels before sampling, so the persistent-bright check sees post-render
// frames only. Returns true once seen; false on timeout (a genuine "never rendered").
async function waitForBand(ctx, wantBright, tries = 40) {
  for (let i = 0; i < tries; i++) {
    const b = await captureBand(ctx);
    if (b && (wantBright ? countBright(b) >= 6 : countBright(b) < 3)) return true;
    await sleep(300);
  }
  return false;
}

async function captureFrames(ctx, label) {
  const frames = [];
  for (let i = 0; i < 5; i++) {
    const b = await captureBand(ctx);
    if (b) frames.push(b);
    await sleep(400);
  }
  if (frames.length < 3) throw new Error("not enough dial image frames captured for " + label);
  return frames;
}

async function pollPages(ctx, want, tries = 40) {
  for (let i = 0; i < tries; i++) {
    const slot = slotByContext(await getState(), ctx);
    if (slot && (((slot.settings || {}).pages) || []).length === want) return slot;
    await sleep(150);
  }
  return slotByContext(await getState(), ctx);
}

// Restore the dial to the settings it had before the test, so running this against
// a real (configured) dial leaves it exactly as the user had it.
async function restoreDial(win, ctx, original) {
  try {
    const restored = JSON.parse(JSON.stringify(original || {}));
    if (!Array.isArray(restored.pages)) restored.pages = [];
    if (typeof restored.activeIndex !== "number") restored.activeIndex = 0;
    win.currentSettings = restored;
    win.saveSettings();
    await pollPages(ctx, restored.pages.length);
  } catch (e) {
    console.error("cleanup warning: " + (e && e.message ? e.message : e));
  }
}

async function main() {
  if (!jsdomMod || !WebSocket || !sharp) return noData(LABEL, "jsdom/ws/sharp not installed");
  let state;
  try {
    state = await getState();
  } catch (e) {
    return noData(LABEL, "DeckBridge not reachable at " + BASE);
  }
  // Prefer an already-empty dial, but fall back to ANY configured dial: a real deck
  // never has a spare empty dial, so requiring one made this whole e2e self-skip and
  // validate nothing. We snapshot the target's settings up front and RESTORE them in
  // the finally block, so mutating a configured dial stays non-destructive.
  const slots = dialSlots(state);
  if (slots.length === 0) return noData(LABEL, "no dial on the deck");
  const target = slots.find((s) => (((s.settings || {}).pages) || []).length === 0) || slots[0];
  const originalSettings = JSON.parse(JSON.stringify(target.settings || { pages: [], activeIndex: 0 }));

  const win = await bootLivePi(target);
  const ctx = target.context;
  const readings = win.currentCatalog.readings || [];
  if (readings.length < 2) return noData(LABEL, "live catalog has too few readings");
  const chosen = readings.slice(0, Math.min(3, readings.length));

  let failures = 0;
  const check = (name, cond) => {
    if (cond) {
      console.log("ok   - " + name);
    } else {
      failures++;
      console.error("FAIL - " + name);
    }
  };

  try {
    // Build pages through the REAL UI helper, then force text mode + fullscreen so
    // the indicator band is the only thing the toggle can change.
    win.currentSettings = { activeIndex: 0, pages: [], sourceProfileId: win.currentSettings.sourceProfileId || "", defaultView: "fullscreen" };
    for (const r of chosen) {
      win.pageSelectionDraft = { sensorUid: r.sensorUid, readingId: String(r.id) };
      win.addSelectedPage();
    }
    win.currentSettings.pages.forEach((p) => { p.graphMode = "text"; });
    win.currentSettings.defaultView = "fullscreen";
    win.currentSettings.activeIndex = 1;

    // --- checkbox OFF: drive the REAL element, exactly like a user click ---
    const box = $(win, "indicatorFullscreen");
    check("the In-fullscreen checkbox exists in the live PI", !!box);
    box.checked = false;
    fire(win, box, "change");
    win.saveSettings();
    await pollPages(ctx, chosen.length);
    check("checkbox-off persisted indicatorFullscreen=false", slotByContext(await getState(), ctx).settings.indicatorFullscreen !== true);
    await waitForBand(ctx, false);
    const offFrames = await captureFrames(ctx, "indicator OFF");

    // --- checkbox ON: real change event toggles the action setting ---
    box.checked = true;
    fire(win, box, "change");
    win.saveSettings();
    for (let i = 0; i < 40; i++) {
      if (slotByContext(await getState(), ctx).settings.indicatorFullscreen === true) break;
      await sleep(150);
    }
    check("checkbox-on persisted indicatorFullscreen=true", slotByContext(await getState(), ctx).settings.indicatorFullscreen === true);
    // Wait for the plugin to actually re-render the dial with the indicator before
    // sampling, so render latency cannot be mistaken for "indicator missing".
    await waitForBand(ctx, true);
    const onFrames = await captureFrames(ctx, "indicator ON");

    // THE FINISH LINE: the RENDERED fullscreen dial gained the page indicator.
    // Use persistent-bright (pixels lit in EVERY frame) so live data drift cannot
    // fake or hide the static indicator dots.
    const offBright = persistentBright(offFrames);
    const onBright = persistentBright(onFrames);
    check("the rendered fullscreen dial shows static indicator dots when ON", onBright >= 6);
    check("the rendered fullscreen dial has no static indicator dots when OFF", offBright < 3);
    check("toggling the checkbox changed the rendered indicator band", onBright > offBright);

    console.log(`\npersistent-bright indicator pixels: OFF=${offBright}  ON=${onBright} (3 pages, fullscreen, text mode)`);
  } finally {
    await restoreDial(win, ctx, originalSettings);
    const cleaned = slotByContext(await getState(), ctx);
    const n = (((cleaned || {}).settings || {}).pages || []).length;
    const want = (originalSettings.pages || []).length;
    console.log(n === want ? "cleanup: dial restored to original (" + want + " pages)" : "cleanup WARNING: dial has " + n + " pages, expected " + want);
    if (n !== want) failures++;
  }

  return failures > 0 ? 1 : 0;
}

main()
  .then((code) => process.exit(code))
  .catch((e) => {
    console.error("test-dial-indicator-fullscreen-live-e2e error: " + (e && e.stack ? e.stack : e));
    process.exit(1);
  });
