#!/usr/bin/env node
"use strict";

// Derived PI: saving a preset goes through the plugin. Stream Deck replaces
// the stored global settings on every setGlobalSettings, so a PI write of
// only derivedPresets would erase the favorites, the global thresholds and
// the refresh rate. The plugin merges the presets in and writes the whole
// object.

const fs = require("fs");
const vm = require("vm");

function assert(condition, msg) {
  if (!condition) {
    throw new Error(msg);
  }
}

function loadSandbox() {
  const sent = [];
  const select = { options: [], remove(i) { this.options.splice(i, 1); }, add(o) { this.options.push(o); } };
  const sandbox = {
    console,
    JSON,
    setTimeout: (fn) => { fn(); return 1; },
    document: {
      addEventListener() {},
      getElementById(id) { return id === "preset_load" ? select : null; },
      querySelector() { return null; },
      querySelectorAll() { return []; },
      createElement() { return {}; },
    },
  };
  sandbox.window = sandbox;
  vm.createContext(sandbox);
  // Same order as derived_pi.html: pi_utils.js first.
  vm.runInContext(fs.readFileSync("com.moeilijk.hwinfo.sdPlugin/pi_utils.js", "utf8"), sandbox);
  vm.runInContext(fs.readFileSync("com.moeilijk.hwinfo.sdPlugin/derived_pi.js", "utf8"), sandbox);
  vm.runInContext("websocket = { readyState: 1, send: function (m) { __sent.push(JSON.parse(m)); } }; uuid = 'ctx-derived'; actionInfo = { action: 'com.moeilijk.hwinfo.derived' };", Object.assign(sandbox, { __sent: sent }));
  return { sandbox, sent, select };
}

function testSavePresetGoesThroughPlugin() {
  const { sandbox, sent, select } = loadSandbox();
  vm.runInContext(
    "allPresets = [{ name: 'old', formula: 'max', slotCount: 2, slots: [] }];" +
      "currentSettings = { formula: 'sum', slotCount: 2, slots: [{ sensorUid: '/cpu', readingId: 3, readingLabel: 'Core' }] };",
    sandbox
  );
  sandbox.saveCurrentAsPreset("new");

  assert(!sent.some((m) => m.event === "setGlobalSettings"), "PI must not write global settings itself");
  const msgs = sent.filter((m) => m.event === "sendToPlugin" && m.payload && Array.isArray(m.payload.saveDerivedPresets));
  assert(msgs.length === 1, "expected one saveDerivedPresets message");
  const names = msgs[0].payload.saveDerivedPresets.map((p) => p.name);
  assert(names.join(",") === "old,new", "preset list should keep the old preset and add the new one, got " + names);
  assert(msgs[0].payload.saveDerivedPresets[1].slots[0].sensorUid === "/cpu", "new preset carries the slots");
  assert(select.options.some((o) => o.text === "new"), "preset select shows the new preset");
}

function testSavePresetReplacesSameName() {
  const { sandbox, sent } = loadSandbox();
  vm.runInContext("allPresets = [{ name: 'a', formula: 'max', slotCount: 2, slots: [] }]; currentSettings = { formula: 'sum', slotCount: 3, slots: [] };", sandbox);
  sandbox.saveCurrentAsPreset("a");
  const msg = sent.find((m) => m.event === "sendToPlugin" && m.payload && m.payload.saveDerivedPresets);
  assert(msg.payload.saveDerivedPresets.length === 1, "same name replaces, not duplicates");
  assert(msg.payload.saveDerivedPresets[0].formula === "sum", "replaced preset has the new formula");
}

testSavePresetGoesThroughPlugin();
testSavePresetReplacesSameName();
console.log("derived-pi tests ok (2 cases)");
