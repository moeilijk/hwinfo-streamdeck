#!/usr/bin/env node
"use strict";

// Real-DOM render test for the dial Property Inspector bulk flow (issuer's spec).
//
// Bulk Add is rule-based (the user already chose the sensor + reading above):
//   - All readings from this sensor          (sensor-all)
//   - This reading on the other matching sensors  (matching-category, same KIND)
//   - All readings in this numbered set       (numbered-family, e.g. all cores)
// Pick a rule, Preview, (de)select, Add. This test loads the ACTUAL dial_pi.html into
// jsdom, runs the ACTUAL pi_utils.js + dial_pi.js, drives the real <select>/<button>,
// and asserts the previewed candidates and the added pages. No server, no state
// mutated.

const fs = require("fs");
const path = require("path");
const assert = require("assert");

const repoRoot = path.resolve(__dirname, "..");

function loadJsdom() {
  for (const c of ["jsdom", path.resolve(repoRoot, "node_modules/jsdom"), path.resolve(repoRoot, "../DeckBridge/node_modules/jsdom")]) {
    try {
      return require(c).JSDOM;
    } catch (e) {
      /* next */
    }
  }
  throw new Error("jsdom not found (keep the sibling DeckBridge/node_modules/jsdom reachable).");
}

const JSDOM = loadJsdom();
const html = fs.readFileSync(path.join(repoRoot, "com.exension.hwinfo.sdPlugin/dial_pi.html"), "utf8");
const piUtils = fs.readFileSync(path.join(repoRoot, "com.exension.hwinfo.sdPlugin/pi_utils.js"), "utf8");
const dialPi = fs.readFileSync(path.join(repoRoot, "com.exension.hwinfo.sdPlugin/dial_pi.js"), "utf8");

function bootPi() {
  const dom = new JSDOM(html, { runScripts: "dangerously", pretendToBeVisual: true });
  const win = dom.window;
  win.WebSocket = function () {
    return { send() {}, readyState: 0 };
  };
  win.eval(piUtils);
  win.eval(dialPi);
  win.document.dispatchEvent(new win.Event("DOMContentLoaded"));
  return win;
}

// Realistic catalog: CPU cores + CPU Total; 3 disks sharing "Read"; an eth0 "Read"
// that must NOT be pulled into the disk match (different KIND); a lone mb reading.
const catalog = {
  sensors: [
    { uid: "/cpu", name: "CPU", category: "cpu" },
    { uid: "/storage/sda", name: "Virtual Disk (sda)", category: "disk" },
    { uid: "/storage/sdb", name: "Virtual Disk (sdb)", category: "disk" },
    { uid: "/storage/sdc", name: "Virtual Disk (sdc)", category: "disk" },
    { uid: "/net/eth0", name: "eth0", category: "network" },
    { uid: "/mb", name: "Motherboard", category: "motherboard" },
  ],
  readings: [
    { id: "1", sensorUid: "/cpu", label: "CPU Core #1", type: "Load", unit: "%", category: "cpu" },
    { id: "2", sensorUid: "/cpu", label: "CPU Core #2", type: "Load", unit: "%", category: "cpu" },
    { id: "3", sensorUid: "/cpu", label: "CPU Total", type: "Load", unit: "%", category: "cpu" },
    { id: "10", sensorUid: "/storage/sda", label: "Read", type: "Throughput", unit: "B/s", category: "disk" },
    { id: "11", sensorUid: "/storage/sdb", label: "Read", type: "Throughput", unit: "B/s", category: "disk" },
    { id: "12", sensorUid: "/storage/sdc", label: "Read", type: "Throughput", unit: "B/s", category: "disk" },
    { id: "30", sensorUid: "/net/eth0", label: "Read", type: "Throughput", unit: "B/s", category: "network" },
    { id: "20", sensorUid: "/mb", label: "Voltage", type: "Voltage", unit: "V", category: "motherboard" },
  ],
  sourceProfiles: [],
};

const $ = (win, id) => win.document.getElementById(id);
const fire = (win, el, t) => el.dispatchEvent(new win.Event(t, { bubbles: true }));

function select(win, sensorUid, readingId) {
  win.currentSettings = { activeIndex: 0, pages: [], sourceProfileId: "" };
  win.currentCatalog = catalog;
  win.pageSelectionDraft = { sensorUid: sensorUid, readingId: "" };
  win.renderSelectedPageSelection();
  $(win, "pageSensorSelect").value = sensorUid;
  fire(win, $(win, "pageSensorSelect"), "change");
  $(win, "pageReadingSelect").value = String(readingId);
  fire(win, $(win, "pageReadingSelect"), "change");
}

function preview(win, ruleValue) {
  $(win, "bulkRule").value = ruleValue;
  fire(win, $(win, "bulkRule"), "change");
  $(win, "bulkPreviewBtn").click();
}

const previewLabels = (win) => Array.from($(win, "bulkPreviewList").options).map((o) => o.textContent);
const pageListTitles = (win) => Array.from($(win, "pageList").options).map((o) => o.textContent);
const readingOnly = (labels) => labels.map((l) => l.replace(/^.*— /, "")).sort();

let failures = 0;
function test(name, fn) {
  try {
    fn();
    console.log("ok   - " + name);
  } catch (e) {
    failures++;
    console.error("FAIL - " + name + "\n       " + (e && e.message ? e.message : e));
  }
}

test("rule 'All readings from this sensor' previews and adds the sensor's readings", () => {
  const win = bootPi();
  select(win, "/cpu", "1");
  preview(win, "sensor-all");
  assert.deepStrictEqual(readingOnly(previewLabels(win)), ["CPU Core #1", "CPU Core #2", "CPU Total"], "preview " + JSON.stringify(previewLabels(win)));
  $(win, "bulkAddBtn").click();
  assert.deepStrictEqual(pageListTitles(win).slice().sort(), ["CPU Core #1", "CPU Core #2", "CPU Total"], "pages " + JSON.stringify(pageListTitles(win)));
});

test("rule 'This reading on matching sensors' covers same-KIND sensors only (no network leak)", () => {
  const win = bootPi();
  select(win, "/storage/sda", "10"); // disk "Read"
  preview(win, "matching-category");
  // The 3 disks share "Read"; eth0's "Read" is a different KIND and must be excluded.
  assert.deepStrictEqual(readingOnly(previewLabels(win)), ["Read", "Read", "Read"], "preview " + JSON.stringify(previewLabels(win)));
  assert.ok(!previewLabels(win).some((l) => l.indexOf("eth0") !== -1), "eth0 must not appear: " + JSON.stringify(previewLabels(win)));
});

test("'This reading on matching sensors' adds distinguishable, sensor-qualified pages", () => {
  const win = bootPi();
  select(win, "/storage/sda", "10");
  preview(win, "matching-category");
  $(win, "bulkAddBtn").click();
  const titles = pageListTitles(win);
  assert.deepStrictEqual(
    titles.slice().sort(),
    ["Virtual Disk (sda) Read", "Virtual Disk (sdb) Read", "Virtual Disk (sdc) Read"],
    "pages " + JSON.stringify(titles)
  );
  assert.strictEqual(new Set(titles).size, titles.length, "titles distinct");
});

test("a rule with no matches shows a clear hint, not a silent empty list", () => {
  const win = bootPi();
  select(win, "/cpu", "3"); // CPU Total exists on no other CPU
  preview(win, "matching-category");
  assert.deepStrictEqual(previewLabels(win), ["No matching readings"], "preview " + JSON.stringify(previewLabels(win)));
  assert.strictEqual($(win, "bulkAddBtn").disabled, true, "Add disabled when nothing matches");
});

test("rule 'All readings in this numbered set' previews the core family", () => {
  const win = bootPi();
  select(win, "/cpu", "1");
  preview(win, "numbered-family");
  assert.deepStrictEqual(readingOnly(previewLabels(win)), ["CPU Core #1", "CPU Core #2"], "preview " + JSON.stringify(previewLabels(win)));
  $(win, "bulkAddBtn").click();
  assert.deepStrictEqual(pageListTitles(win).slice().sort(), ["CPU Core #1", "CPU Core #2"], "pages " + JSON.stringify(pageListTitles(win)));
});

test("de-selecting a previewed page excludes it from the add", () => {
  const win = bootPi();
  select(win, "/storage/sda", "10");
  preview(win, "matching-category");
  const list = $(win, "bulkPreviewList");
  Array.from(list.options).find((o) => o.textContent.indexOf("sdb") !== -1).selected = false;
  $(win, "bulkAddBtn").click();
  assert.deepStrictEqual(pageListTitles(win).slice().sort(), ["Virtual Disk (sda) Read", "Virtual Disk (sdc) Read"], "pages " + JSON.stringify(pageListTitles(win)));
});

// #56 feedback item 6: readings already on the dial must be VISIBLE in the preview
// (marked + deselected + disabled), not silently dropped, so the skip is communicated.
test("a reading already on the dial is shown as a disabled, deselected duplicate", () => {
  const win = bootPi();
  select(win, "/cpu", "1");
  // Pre-add CPU Core #1 so it is an existing page when we preview the family.
  preview(win, "numbered-family");
  Array.from($(win, "bulkPreviewList").options).forEach((o) => {
    if (o.textContent.indexOf("CPU Core #2") !== -1) o.selected = false;
  });
  $(win, "bulkAddBtn").click();
  assert.deepStrictEqual(pageListTitles(win), ["CPU Core #1"], "only #1 added: " + JSON.stringify(pageListTitles(win)));

  // Re-preview the same family WITHOUT wiping the existing pages (the select() helper
  // resets currentSettings, so re-drive the draft selection directly instead).
  win.pageSelectionDraft = { sensorUid: "/cpu", readingId: "" };
  win.renderSelectedPageSelection();
  $(win, "pageSensorSelect").value = "/cpu";
  fire(win, $(win, "pageSensorSelect"), "change");
  $(win, "pageReadingSelect").value = "1";
  fire(win, $(win, "pageReadingSelect"), "change");
  preview(win, "numbered-family");
  const opts = Array.from($(win, "bulkPreviewList").options);
  const dup = opts.find((o) => o.textContent.indexOf("CPU Core #1") !== -1);
  const fresh = opts.find((o) => o.textContent.indexOf("CPU Core #2") !== -1);
  assert.ok(dup, "duplicate still listed (not dropped): " + JSON.stringify(previewLabels(win)));
  assert.strictEqual(dup.disabled, true, "duplicate is disabled");
  assert.strictEqual(dup.selected, false, "duplicate is deselected");
  assert.ok(dup.textContent.indexOf("(added)") !== -1, "duplicate is marked: " + dup.textContent);
  assert.strictEqual(fresh.disabled, false, "non-duplicate stays enabled");
  assert.strictEqual(fresh.selected, true, "non-duplicate stays selected");
  // The Add button counts only the addable (non-duplicate) selection.
  assert.ok($(win, "bulkAddBtn").textContent.indexOf("(1)") !== -1, "add count excludes duplicate: " + $(win, "bulkAddBtn").textContent);

  // Adding now appends only #2; #1 is not duplicated.
  $(win, "bulkAddBtn").click();
  assert.deepStrictEqual(pageListTitles(win).slice().sort(), ["CPU Core #1", "CPU Core #2"], "no duplicate page added: " + JSON.stringify(pageListTitles(win)));
});

// #56 feedback item 4: a name template renames bulk-added pages; the preview shows
// the resulting titles live and the added pages use them.
test("a name template renames bulk-added pages and previews them live", () => {
  const win = bootPi();
  select(win, "/cpu", "1");
  preview(win, "numbered-family");
  // Type a short template; the preview must re-render to the resulting titles.
  const fmt = $(win, "bulkNameFormat");
  fmt.value = "Core %n";
  fire(win, fmt, "input");
  assert.deepStrictEqual(previewLabels(win).slice().sort(), ["Core 1", "Core 2"], "preview shows templated names: " + JSON.stringify(previewLabels(win)));
  $(win, "bulkAddBtn").click();
  assert.deepStrictEqual(pageListTitles(win).slice().sort(), ["Core 1", "Core 2"], "added pages use the template: " + JSON.stringify(pageListTitles(win)));
});

if (failures > 0) {
  console.error("\n" + failures + " test(s) failed");
  process.exit(1);
}
console.log("\nall dial bulk render tests passed");
