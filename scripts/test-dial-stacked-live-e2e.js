#!/usr/bin/env node
"use strict";

// TRUE live end-to-end test for the STACKED dial overview — the WHOLE display.
//
// "End to end" reaches the RENDERED DIAL PIXELS through the REAL system:
//   boot the actual dial_pi.html in a browser DOM -> connect to the RUNNING
//   DeckBridge + plugin -> configure a 3-page dial with overviewStyle="stacked"
//   and defaultView="overview" -> the plugin re-renders -> read the dial image
//   back from /api/state and decode ALL of its pixels -> assert the COMPLETE
//   stacked display, not just one element:
//     1. three EQUAL strips (active centred), each rendering its own graph,
//     2. the active strip carries the bright-blue selection border in the
//        MIDDLE third only (so the layout is equal + centred, not dominant),
//     3. the vertical page indicator is drawn in the LEFT column, and
//     4. the graph area on the right is FREE of the indicator colour.
//
// Determinism over live, drifting sensor values: the page graphs are forced to a
// green scheme and the indicator to red, so "indicator" (red) and "graph" (green)
// are separable by colour anywhere on the real rendered image.
//
// Non-destructive: mutates one originally-empty dial and resets it. Self-skips
// (exit 0) when DeckBridge/jsdom/ws/sharp are not reachable.

const fs = require("fs");
const path = require("path");

const { noData } = require("./live-e2e-guard");

const repoRoot = path.resolve(__dirname, "..");
const BASE = process.env.DECKBRIDGE_URL || "http://127.0.0.1:34075";
const PLUGIN_ID = "com.exension.hwinfo";
const LABEL = "dial stacked live e2e";
const W = 200;
const H = 100;
const LEFT_COL = 18; // dialStackedGutter(size=6 default): reserved for the vertical indicator

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

// Decode the whole dial image to raw RGB.
async function captureImage(ctx) {
  const slot = slotByContext(await getState(), ctx);
  const url = ((slot || {}).feedback || {}).imageDataUrl || "";
  const b64 = url.replace(/^data:image\/png;base64,/, "");
  if (!b64) return null;
  const buf = Buffer.from(b64, "base64");
  const { data, info } = await sharp(buf).ensureAlpha().raw().toBuffer({ resolveWithObject: true });
  return { data, W: info.width, H: info.height, ch: info.channels };
}

const at = (im, x, y) => {
  const i = (y * im.W + x) * im.ch;
  return [im.data[i], im.data[i + 1], im.data[i + 2]];
};
const isRed = (p) => p[0] > 150 && p[1] < 90 && p[2] < 90; // the ACTIVE indicator dot (full red)
const isAnyDot = (p) => p[0] > 80 && p[1] < 80 && p[2] < 80; // active OR inactive dot (inactive ~140 red)
const isBlue = (p) => p[2] > 180 && p[1] > 100 && p[1] < 215 && p[0] < 90; // active border {0,150,255}
// The page graph fill (#004000) / highlight (#00ff00). The neighbour strips are
// dimmed by a translucent black overlay, so the fill green drops to ~46; keep the
// threshold low enough to still see it while excluding grey text / white / blue.
const isGreen = (p) => p[1] > 40 && p[1] > p[0] + 20 && p[1] > p[2] + 20;
// Bright value/title text drawn on every strip. The value is white (#ffffff);
// even on the dimmed neighbour strips (translucent-black overlay) it stays ~185,
// so a >150 threshold proves each strip rendered its reading. This is the
// deterministic per-strip content signal (the live sparkline fill depends on
// sensor-sample accumulation and near-zero CPU values, so it is verified
// deterministically in the Go render test instead).
const isText = (p) => p[0] > 150 && p[1] > 150 && p[2] > 150;

function countRegion(im, x0, x1, y0, y1, pred) {
  let n = 0;
  for (let y = y0; y < y1; y++) for (let x = x0; x < x1; x++) if (pred(at(im, x, y))) n++;
  return n;
}

// Vertical runs in the indicator column [x0,x1): one run per drawn dot. Each run
// carries its pixel height and vertical centre, so we can count the dots and find
// which one is the (elongated) active marker and where it sits.
function columnRuns(im, x0, x1, pred) {
  const runs = [];
  let start = -1;
  for (let y = 0; y < im.H; y++) {
    let on = false;
    for (let x = x0; x < x1; x++) {
      if (pred(at(im, x, y))) { on = true; break; }
    }
    if (on && start < 0) start = y;
    if (!on && start >= 0) { runs.push({ y0: start, y1: y, h: y - start, center: (start + y - 1) / 2 }); start = -1; }
  }
  if (start >= 0) runs.push({ y0: start, y1: im.H, h: im.H - start, center: (start + im.H - 1) / 2 });
  return runs;
}

// Capture frames until `ok(image)` holds (e.g. the indicator has settled into a
// new position after a change), falling back to the last frame seen.
async function captureWhen(ctx, ok, tries = 40) {
  let last = null;
  for (let i = 0; i < tries; i++) {
    const im = await captureImage(ctx);
    if (im) {
      last = im;
      if (ok(im)) return im;
    }
    await sleep(400);
  }
  return last;
}

// Wait until the plugin has rendered the stacked overview with all three strips
// showing their content. The active blue border appears in the middle third and
// each strip draws its (always-present) value/title text; poll until both hold so
// the assertions see a settled frame (falling back to the last frame seen).
async function waitForStacked(ctx, tries = 60) {
  let last = null;
  for (let i = 0; i < tries; i++) {
    const im = await captureImage(ctx);
    if (im) {
      last = im;
      const bordered = countRegion(im, LEFT_COL, im.W, 34, 66, isBlue) >= 4;
      const top = countRegion(im, LEFT_COL, im.W, 2, 32, isText);
      const mid = countRegion(im, LEFT_COL, im.W, 35, 65, isText);
      const bot = countRegion(im, LEFT_COL, im.W, 68, 98, isText);
      if (bordered && top > 0 && mid > 0 && bot > 0) return im;
    }
    await sleep(500);
  }
  return last;
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

// Pick three distinct readings (prefer distinct sensors) to fill three pages.
function pickThreeReadings(readings) {
  const seen = new Set();
  const out = [];
  for (const r of readings) {
    const key = r.sensorUid + ":" + r.id;
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(r);
    if (out.length === 3) break;
  }
  return out;
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
  const chosen = pickThreeReadings(readings);
  if (chosen.length < 3) return noData(LABEL, "live catalog has fewer than 3 readings");

  let failures = 0;
  const assert = require("assert");
  const check = (name, fn) => {
    try {
      fn();
      console.log("ok   - " + name);
    } catch (e) {
      failures++;
      console.error("FAIL - " + name + "\n       " + (e && e.message ? e.message : e));
    }
  };

  try {
    // Build three valid pages, forced to a green graph scheme so "graph" pixels
    // are separable from the red indicator anywhere on the rendered image.
    const pages = chosen.map((r, i) => {
      const p = win.pageFromReading(r.sensorUid, r, i);
      p.foregroundColor = "#004000";
      p.highlightColor = "#00ff00";
      p.backgroundColor = "#000000";
      p.valueTextColor = "#ffffff";
      p.titleColor = "#b7b7b7";
      return p;
    });
    win.currentSettings = {
      activeIndex: 1, // middle page active -> middle strip is the active one
      pages: pages,
      sourceProfileId: win.currentSettings.sourceProfileId || "",
      overviewStyle: "stacked",
      defaultView: "overview",
      indicatorStyle: "dots",
      indicatorColor: "#ff0000", // red, so the indicator is unmistakable on the left
    };
    win.saveSettings();

    await pollPages(ctx, 3);
    const im = await waitForStacked(ctx);
    if (process.env.HWINFO_DUMP_DIAL) {
      const slot = slotByContext(await getState(), ctx);
      const dumpB64 = (((slot || {}).feedback || {}).imageDataUrl || "").replace(/^data:image\/png;base64,/, "");
      if (dumpB64) fs.writeFileSync(process.env.HWINFO_DUMP_DIAL, Buffer.from(dumpB64, "base64"));
    }
    check("the dial rendered the stacked overview", () => {
      assert.ok(im, "no dial image came back from /api/state");
      assert.strictEqual(im.W, W, "unexpected width " + im.W);
      assert.strictEqual(im.H, H, "unexpected height " + im.H);
    });
    if (!im) throw new Error("no image; cannot assert the display");

    // 1. Three EQUAL strips, active centred: the blue selection border is in the
    //    MIDDLE third only. If the layout were "dominant", the active strip would
    //    not be a clean centred third.
    check("active strip's blue border is in the MIDDLE third", () => {
      assert.ok(countRegion(im, LEFT_COL, W, 34, 66, isBlue) >= 6, "no blue border in the middle third");
    });
    check("top and bottom thirds are NOT the active strip (no blue border)", () => {
      assert.ok(countRegion(im, LEFT_COL, W, 1, 33, isBlue) === 0, "blue border bled into the top third");
      assert.ok(countRegion(im, LEFT_COL, W, 67, 99, isBlue) === 0, "blue border bled into the bottom third");
    });

    // 2. Every strip renders its OWN content (all three readable, not 1 + 2
    //    hints): the always-drawn value/title text appears in each third. The
    //    sparkline pixel-correctness is locked deterministically in the Go render
    //    test (TestRenderDialStackedNativeStrips); here we prove, end-to-end on
    //    the real display, that no strip is blank.
    const contentTop = countRegion(im, LEFT_COL, W, 2, 32, isText);
    const contentMid = countRegion(im, LEFT_COL, W, 35, 65, isText);
    const contentBot = countRegion(im, LEFT_COL, W, 68, 98, isText);
    check("each of the three strips renders its own reading", () => {
      assert.ok(contentTop > 0, "top strip has no content");
      assert.ok(contentMid > 0, "middle (active) strip has no content");
      assert.ok(contentBot > 0, "bottom strip has no content");
    });

    // 3. The vertical page indicator is drawn in the LEFT column.
    check("the vertical page indicator is drawn in the LEFT column", () => {
      assert.ok(countRegion(im, 2, LEFT_COL, 0, H, isAnyDot) >= 6, "no indicator pixels in the left column");
    });

    // 3b. The indicator is CORRECT, not just present: one dot per page, exactly
    //     one (elongated) active dot, and the active dot tracks the active page.
    const dotsX0 = 4;
    const dotsX1 = 14;
    const dots = columnRuns(im, dotsX0, dotsX1, isAnyDot);
    const activeDots = columnRuns(im, dotsX0, dotsX1, isRed);
    check("the indicator has one dot per page (3 pages -> 3 dots)", () => {
      assert.strictEqual(dots.length, 3, "saw " + dots.length + " dots, want 3");
    });
    check("exactly one dot is the active (elongated, full-colour) marker", () => {
      assert.strictEqual(activeDots.length, 1, "saw " + activeDots.length + " active dots, want 1");
      const others = dots.map((d) => d.h).sort((a, b) => a - b);
      assert.ok(activeDots[0].h > others[0], "active dot must be taller than an inactive dot");
    });
    const activeCenterMid = activeDots.length === 1 ? activeDots[0].center : null;
    check("the active dot sits beside the active (middle) strip", () => {
      assert.ok(activeCenterMid !== null && activeCenterMid > 33 && activeCenterMid < 67, "active dot centre " + activeCenterMid + " is not beside the middle strip");
    });

    // 3c. LIVE: move the active page to the top and prove the active dot follows
    //     it upward (the indicator updates, it is not a static decoration).
    win.currentSettings.activeIndex = 0;
    win.saveSettings();
    const im2 = await captureWhen(ctx, (img) => {
      const a = columnRuns(img, dotsX0, dotsX1, isRed);
      return a.length === 1 && a[0].center < (activeCenterMid == null ? 0 : activeCenterMid) - 4;
    });
    check("the active dot moves up when the active page changes (live indicator)", () => {
      const a = im2 ? columnRuns(im2, dotsX0, dotsX1, isRed) : [];
      assert.strictEqual(a.length, 1, "after switching, saw " + a.length + " active dots, want 1");
      assert.ok(activeCenterMid !== null && a[0].center < activeCenterMid - 4, "active dot centre " + (a[0] && a[0].center) + " did not move up from " + activeCenterMid);
    });

    // 4. The graph area on the right is FREE of the indicator colour (no bleed).
    check("the graph area on the right is free of the indicator", () => {
      assert.strictEqual(countRegion(im, LEFT_COL + 2, W, 0, H, isRed), 0, "indicator colour bled into the graph area");
    });

    // 5. #56 tester ask: cap the overview to 2 bigger strips even with 3 pages.
    //    The layout splits at y~50 instead of into thirds, so the active (top)
    //    strip's blue border now reaches into y 40..48 (where the three-strip
    //    layout had none) and there is no third active strip below y 60. The page
    //    indicator still reflects ALL three pages.
    win.currentSettings.activeIndex = 1; // active goes on the TOP strip when capped to 2
    win.currentSettings.overviewPages = "2";
    win.saveSettings();
    const imCap = await captureWhen(ctx, (img) =>
      countRegion(img, LEFT_COL, W, 40, 48, isBlue) >= 4 && countRegion(img, LEFT_COL, W, 60, H, isBlue) === 0);
    check("cap to 2: the active top strip grows past the old top-third (blue border at y 40..48)", () => {
      assert.ok(imCap, "no capped image came back");
      assert.ok(countRegion(imCap, LEFT_COL, W, 40, 48, isBlue) >= 4, "the top strip did not grow taller than a third");
    });
    check("cap to 2: there is no third strip (no active border below y 60)", () => {
      assert.strictEqual(countRegion(imCap, LEFT_COL, W, 60, H, isBlue), 0, "a third/active strip is still drawn below");
    });
    check("cap to 2: both visible strips render their reading", () => {
      assert.ok(countRegion(imCap, LEFT_COL, W, 2, 48, isText) > 0, "top strip has no content");
      assert.ok(countRegion(imCap, LEFT_COL, W, 52, 98, isText) > 0, "bottom strip has no content");
    });
    check("cap to 2: the indicator still shows all three pages", () => {
      const allDots = columnRuns(imCap, dotsX0, dotsX1, isAnyDot);
      assert.strictEqual(allDots.length, 3, "saw " + allDots.length + " dots, want 3 (full page count preserved)");
    });

    console.log(
      "\nstacked render: blue(mid)=" + countRegion(im, LEFT_COL, W, 34, 66, isBlue) +
      " text/strip=[" + contentTop + "," + contentMid + "," + contentBot + "]" +
      " dots=" + dots.length + " activeDotCentre=" + activeCenterMid
    );
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
    console.error("dial stacked live e2e error: " + (e && e.stack ? e.stack : e));
    process.exit(1);
  });
