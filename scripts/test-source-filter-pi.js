#!/usr/bin/env node
"use strict";

// Functional unit tests for the Source filter (remote machine sensors) in
// all four Property Inspectors, following the VM-sandbox pattern of
// test-reading-pi.js / test-dial-pi.js. The filter must:
// - stay hidden with a single source and not filter anything;
// - appear with >1 source and filter sensors to the selected source;
// - keep the currently configured sensor visible even when filtered out.

const fs = require("fs");
const vm = require("vm");

function assert(condition, msg) {
  if (!condition) {
    throw new Error(msg);
  }
}

class StubElement {
  constructor(initial = {}) {
    this.value = initial.value || "";
    this.text = initial.text || "";
    this.textContent = "";
    this.disabled = initial.disabled === true;
    this.selected = false;
    this.checked = false;
    this.dataset = {};
    this.style = {};
    this.options = [];
    this.handlers = {};
    this.classList = { add() {}, remove() {}, toggle() {}, contains() { return false; } };
  }
  add(option) { this.options.push(option); }
  appendChild(option) { this.options.push(option); }
  remove(index) { this.options.splice(index, 1); }
  removeAttribute(name) { if (name === "disabled") this.disabled = false; }
  setAttribute() {}
  addEventListener(evt, fn) {
    this.handlers[evt] = this.handlers[evt] || [];
    this.handlers[evt].push(fn);
  }
  trigger(evt) { (this.handlers[evt] || []).forEach((fn) => fn({ target: this })); }
  querySelector() { return null; }
  querySelectorAll() { return []; }
  set innerHTML(v) { if (v === "") this.options = []; }
  get innerHTML() { return ""; }
}

function stubDocument(named) {
  const auto = new Proxy(named, {
    get(target, id) {
      if (typeof id !== "string") return target[id];
      if (!(id in target)) target[id] = new StubElement();
      return target[id];
    },
    has() { return true; },
  });
  return {
    querySelector(selector) { return auto[selector.replace(/^#/, "")]; },
    querySelectorAll() { return []; },
    getElementById(id) { return auto[id]; },
    createElement() { return new StubElement(); },
    addEventListener() {},
    body: { appendChild() {} },
  };
}

function loadSandbox(scripts, named) {
  const sandbox = {
    console,
    JSON,
    setTimeout: (fn) => { fn(); return 1; },
    clearTimeout: () => {},
    navigator: { appVersion: "QtWebEngine" },
    location: { hostname: "127.0.0.1" },
    WebSocket: function WebSocket() {},
    document: stubDocument(named),
    window: null,
    websocket: { readyState: 1, send() {} },
    Event: function Event(type) { this.type = type; },
  };
  sandbox.window = sandbox;
  sandbox.addEventListener = () => {};
  vm.createContext(sandbox);
  for (const script of scripts) {
    vm.runInContext(fs.readFileSync("com.moeilijk.hwinfo.sdPlugin/" + script, "utf8"), sandbox);
  }
  return sandbox;
}

const TWO_SOURCES = [
  { id: "", name: "Local" },
  { id: "remote0", name: "Remote 1" },
];
const SENSORS = [
  { uid: "100", name: "Local CPU", category: "cpu", source: "" },
  { uid: "remote0::200", name: "Remote CPU", category: "cpu", source: "remote0" },
];

function optionTexts(select) {
  return select.options.map((o) => o.text || o.textContent).filter(Boolean);
}

function testReadingPI() {
  const named = {
    sensorSelect: new StubElement(),
    sensorSearch: new StubElement(),
    sensorCategoryFilter: new StubElement(),
    sourceFilter: new StubElement(),
    sourceFilterRow: new StubElement(),
    sourceHeading: new StubElement(),
  };
  const sandbox = loadSandbox(["index_pi.js"], named);

  // Single source: row hidden, no filtering.
  sandbox.currentSources = [TWO_SOURCES[0]];
  sandbox.currentSensors = SENSORS.slice();
  sandbox.currentSensorSettings = {};
  sandbox.renderSourceFilter();
  assert(named.sourceFilterRow.style.display === "none", "reading: source row must stay hidden with one source");
  sandbox.renderSensorOptions(false);
  assert(optionTexts(named.sensorSelect).includes("Remote CPU"), "reading: no filtering with one source");

  // Two sources, tile configured on the remote sensor: filter defaults to
  // that source and only its sensors are listed.
  sandbox.currentSources = TWO_SOURCES.slice();
  sandbox.currentSensorSettings = { sensorUid: "remote0::200", isValid: true };
  sandbox.renderSourceFilter();
  assert(named.sourceFilterRow.style.display === "", "reading: source row must appear with two sources");
  assert(named.sourceFilter.options.length === 2, "reading: source dropdown lists both sources");
  const selected = named.sourceFilter.options.find((o) => o.selected);
  assert(selected && selected.value === "remote0", "reading: filter defaults to the configured sensor's source");
  named.sourceFilter.value = "remote0";
  sandbox.renderSensorOptions(false);
  let texts = optionTexts(named.sensorSelect);
  assert(texts.includes("Remote CPU") && !texts.includes("Local CPU"), "reading: only the selected source's sensors listed");

  // Switching to Local keeps the configured (remote) sensor visible.
  named.sourceFilter.value = "";
  named.sourceFilter.trigger("change");
  texts = optionTexts(named.sensorSelect);
  assert(texts.includes("Local CPU"), "reading: local sensors listed after switch");
  assert(texts.includes("Remote CPU"), "reading: configured sensor stays visible when filtered out");
}

function testDialPI() {
  const named = {
    pageSensorSelect: new StubElement(),
    pageSensorSearch: new StubElement(),
    pageSensorCategoryFilter: new StubElement(),
    pageSourceFilter: new StubElement(),
    sourceFilterRow: new StubElement(),
    sourceHeading: new StubElement(),
  };
  const sandbox = loadSandbox(["pi_utils.js", "dial_pi.js"], named);

  sandbox.currentCatalog = { sensors: SENSORS.slice(), readings: [], sources: TWO_SOURCES.slice() };
  sandbox.resetPageSelectionDraft({ sensorUid: "remote0::200", readingId: 1 });
  sandbox.renderSourceFilter();
  assert(named.sourceFilterRow.style.display === "", "dial: source row must appear with two sources");
  const selected = named.pageSourceFilter.options.find((o) => o.selected);
  assert(selected && selected.value === "remote0", "dial: filter defaults to the draft's source");
  named.pageSourceFilter.value = "remote0";
  sandbox.populateSelectedPageSensors();
  let texts = named.pageSensorSelect.options.map((o) => o.textContent);
  assert(texts.includes("Remote CPU") && !texts.includes("Local CPU"), "dial: only the selected source's sensors listed");

  // Single source hides the row and disables filtering.
  sandbox.currentCatalog.sources = [TWO_SOURCES[0]];
  sandbox.renderSourceFilter();
  assert(named.sourceFilterRow.style.display === "none", "dial: source row hidden with one source");
  sandbox.populateSelectedPageSensors();
  texts = named.pageSensorSelect.options.map((o) => o.textContent);
  assert(texts.includes("Local CPU") && texts.includes("Remote CPU"), "dial: no filtering with one source");
}

function testSlotPI(script, slotCount, label) {
  const named = {
    sourceFilter: new StubElement(),
    sourceFilterRow: new StubElement(),
    sourceHeading: new StubElement(),
  };
  named["slot0_sensorSelect"] = new StubElement();
  const sandbox = loadSandbox(["pi_utils.js", script], named);

  sandbox.allSources = TWO_SOURCES.slice();
  sandbox.allSensors = SENSORS.slice();
  sandbox.currentSettings = { slots: [{ sensorUid: "remote0::200" }] };
  sandbox.renderSourceFilter();
  assert(named.sourceFilterRow.style.display === "", label + ": source row must appear with two sources");
  const selected = named.sourceFilter.options.find((o) => o.selected);
  assert(selected && selected.value === "remote0", label + ": filter defaults to first configured slot's source");
  named.sourceFilter.value = "remote0";
  sandbox.populateSensorSelect(0, sandbox.allSensors);
  let texts = optionTexts(named["slot0_sensorSelect"]);
  assert(texts.includes("Remote CPU") && !texts.includes("Local CPU"), label + ": only the selected source's sensors listed");

  // The configured slot sensor survives switching the filter to Local.
  named.sourceFilter.value = "";
  sandbox.populateSensorSelect(0, sandbox.allSensors);
  texts = optionTexts(named["slot0_sensorSelect"]);
  assert(texts.includes("Local CPU") && texts.includes("Remote CPU"), label + ": configured sensor stays visible when filtered out");

  // Single source: no filtering.
  sandbox.allSources = [TWO_SOURCES[0]];
  sandbox.renderSourceFilter();
  assert(named.sourceFilterRow.style.display === "none", label + ": source row hidden with one source");
  sandbox.populateSensorSelect(0, sandbox.allSensors);
  texts = optionTexts(named["slot0_sensorSelect"]);
  assert(texts.includes("Local CPU") && texts.includes("Remote CPU"), label + ": no filtering with one source");
}

function main() {
  testReadingPI();
  console.log("ok: reading PI source filter");
  testDialPI();
  console.log("ok: dial PI source filter");
  testSlotPI("composite_pi.js", 4, "composite");
  console.log("ok: composite PI source filter");
  testSlotPI("derived_pi.js", 8, "derived");
  console.log("ok: derived PI source filter");
  console.log("ALL SOURCE FILTER PI TESTS PASSED");
}

main();
