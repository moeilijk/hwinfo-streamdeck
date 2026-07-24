#!/usr/bin/env node
"use strict";

const fs = require("fs");
const vm = require("vm");

function assert(condition, msg) {
  if (!condition) {
    throw new Error(msg);
  }
}

class FakeElement {
  constructor(initial = {}) {
    this.value = initial.value || "";
    this.text = initial.text || "";
    this.textContent = initial.textContent || "";
    this.checked = initial.checked === true;
    this.disabled = initial.disabled === true;
    this.dataset = initial.dataset || {};
    this.style = initial.style || {};
    this.handlers = {};
    this.classList = {
      values: new Set(initial.classNames || []),
      add: (...names) => names.forEach((name) => this.classList.values.add(name)),
      remove: (...names) => names.forEach((name) => this.classList.values.delete(name)),
      contains: (name) => this.classList.values.has(name),
      toggle: (name, force) => {
        if (force === undefined) {
          if (this.classList.values.has(name)) {
            this.classList.values.delete(name);
            return false;
          }
          this.classList.values.add(name);
          return true;
        }
        if (force) {
          this.classList.values.add(name);
          return true;
        }
        this.classList.values.delete(name);
        return false;
      },
    };
  }
  addEventListener(evt, fn) {
    this.handlers[evt] = this.handlers[evt] || [];
    this.handlers[evt].push(fn);
  }
  trigger(evt) {
    const fns = this.handlers[evt] || [];
    fns.forEach((fn) => fn({ target: this }));
  }
}

class FakeOption extends FakeElement {
  constructor() {
    super();
    this.selected = false;
  }
}

class FakeSelect extends FakeElement {
  constructor() {
    super({ disabled: true });
    this.options = [];
  }
  add(option) {
    this.options.push(option);
  }
  remove(index) {
    this.options.splice(index, 1);
  }
  removeAttribute(name) {
    if (name === "disabled") {
      this.disabled = false;
    }
  }
}

function loadSandbox() {
  const snoozeButtons = [
    new FakeElement({ dataset: { value: "300000" } }),
    new FakeElement({ dataset: { value: "900000" } }),
    new FakeElement({ dataset: { value: "3600000" } }),
    new FakeElement({ dataset: { value: "0" } }),
  ];

  const selectors = {
    "#snooze5m": snoozeButtons[0],
    "#snooze15m": snoozeButtons[1],
    "#snooze1h": snoozeButtons[2],
    "#snoozeUntilResumed": snoozeButtons[3],
  };

  const sent = [];
  const sandbox = {
    console,
    JSON,
    setTimeout: (fn) => {
      fn();
      return 1;
    },
    clearTimeout: () => {},
    navigator: {
      appVersion: "QtWebEngine",
    },
    document: {
      querySelector(selector) {
        return selectors[selector] || null;
      },
      querySelectorAll(selector) {
        if (selector === ".snooze-duration") {
          return snoozeButtons;
        }
        return [];
      },
      addEventListener() {},
      body: {
        appendChild() {},
      },
    },
    window: null,
    websocket: null,
    uuid: "ctx-reading",
    actionInfo: {
      action: "com.exension.hwinfo.reading",
    },
    Event: function Event(type) {
      this.type = type;
    },
  };
  sandbox.window = sandbox;
  sandbox.addEventListener = () => {};

  vm.createContext(sandbox);
  vm.runInContext(fs.readFileSync("com.exension.hwinfo.sdPlugin/index_pi.js", "utf8"), sandbox);

  sandbox.uuid = "ctx-reading";
  sandbox.actionInfo = {
    action: "com.exension.hwinfo.reading",
  };
  sandbox.websocket = {
    readyState: 1,
    send(msg) {
      sent.push(JSON.parse(msg));
    },
  };

  return { sandbox, sent, snoozeButtons };
}

function loadScriptSandbox(scriptPath, extra = {}) {
  const graphUnitContainer = new FakeElement({ style: {} });
  const elementsById = extra.elementsById || {};
  const querySelectors = Object.assign({
    "#graphUnitContainer": graphUnitContainer,
  }, extra.querySelectors || {});

  const sandbox = {
    console,
    JSON,
    setTimeout: (fn) => {
      fn();
      return 1;
    },
    clearTimeout: () => {},
    navigator: {
      appVersion: "QtWebEngine",
    },
    document: {
      querySelector(selector) {
        return querySelectors[selector] || null;
      },
      querySelectorAll() {
        return [];
      },
      getElementById(id) {
        return elementsById[id] || null;
      },
      createElement(tag) {
        if (tag === "option") {
          return new FakeOption();
        }
        return new FakeElement();
      },
      addEventListener() {},
      body: {
        appendChild() {},
      },
    },
    window: null,
    websocket: null,
    uuid: "ctx-reading",
    actionInfo: {
      action: "com.exension.hwinfo.reading",
    },
    Event: function Event(type) {
      this.type = type;
    },
  };
  sandbox.window = sandbox;
  sandbox.addEventListener = () => {};

  vm.createContext(sandbox);
  vm.runInContext(fs.readFileSync("com.exension.hwinfo.sdPlugin/pi_utils.js", "utf8"), sandbox);
  vm.runInContext(fs.readFileSync(scriptPath, "utf8"), sandbox);
  return sandbox;
}

function sampleReadings() {
  return [
    { id: 10, prefix: "%", unit: "%", type: "Load", label: "CPU Core #10" },
    { id: 2, prefix: "%", unit: "%", type: "Load", label: "CPU Core #2" },
    { id: 1, prefix: "%", unit: "%", type: "Load", label: "CPU Core #1" },
    { id: 20, prefix: "MHz", unit: "MHz", type: "Clock", label: "Core #10" },
    { id: 12, prefix: "MHz", unit: "MHz", type: "Clock", label: "Core #2" },
    { id: 11, prefix: "MHz", unit: "MHz", type: "Clock", label: "Core #1" },
  ];
}

function optionTexts(select) {
  return select.options.slice(1).map((option) => option.textContent || option.text);
}

function testNormalizeSnoozeDurations() {
  const { sandbox } = loadSandbox();
  const result = sandbox.normalizeSnoozeDurations([900000, 123, 0, 300000, 900000, 3600000]);
  assert(Array.isArray(result), "normalizeSnoozeDurations should return an array");
  assert(result.join(",") === "300000,900000,3600000,0", `unexpected normalized durations: ${result.join(",")}`);
}

function testApplySnoozeDurationsToUI() {
  const { sandbox, snoozeButtons } = loadSandbox();
  sandbox.applySnoozeDurationsToUI({
    snoozeDurations: [900000, 0],
  });

  assert(snoozeButtons[0].classList.contains("is-selected") === false, "5m should stay unselected");
  assert(snoozeButtons[1].classList.contains("is-selected") === true, "15m should be selected");
  assert(snoozeButtons[2].classList.contains("is-selected") === false, "1h should stay unselected");
  assert(snoozeButtons[3].classList.contains("is-selected") === true, "until resumed should be selected");
}

function testReadSnoozeDurationsFromUI() {
  const { sandbox, snoozeButtons } = loadSandbox();
  snoozeButtons[2].classList.add("is-selected");
  snoozeButtons[0].classList.add("is-selected");

  const result = sandbox.readSnoozeDurationsFromUI();
  assert(result.join(",") === "300000,3600000", `unexpected selected durations: ${result.join(",")}`);
}

function testBindSnoozeControlsSendsPayload() {
  const { sandbox, sent, snoozeButtons } = loadSandbox();
  sandbox.bindSnoozeControls();

  snoozeButtons[3].classList.add("is-selected");
  snoozeButtons[0].trigger("click");

  assert(sent.length >= 1, "expected snooze control change to send a plugin message");
  const msg = sent[sent.length - 1];
  assert(msg.event === "sendToPlugin", "expected sendToPlugin event");
  assert(msg.action === "com.exension.hwinfo.reading", "unexpected action in plugin payload");
  assert(msg.context === "ctx-reading", "unexpected context in plugin payload");
  assert(msg.payload && msg.payload.sdpi_collection, "missing sdpi_collection payload");
  assert(msg.payload.sdpi_collection.key === "snoozeDurations", "unexpected payload key");
  assert(
    Array.isArray(msg.payload.sdpi_collection.selection) &&
      msg.payload.sdpi_collection.selection.join(",") === "300000,0",
    `unexpected snooze selection payload: ${JSON.stringify(msg.payload.sdpi_collection.selection)}`
  );
}

function testIndexReadingSortGroupsByPrefixAndNaturalLabel() {
  const select = new FakeSelect();
  const sandbox = loadScriptSandbox("com.exension.hwinfo.sdPlugin/index_pi.js", {
    querySelectors: {
      "#graphUnitContainer": new FakeElement({ style: {} }),
    },
  });
  sandbox.renderFavoriteControls = () => {};
  sandbox.updateGraphUnitVisibility = () => {};

  sandbox.addReadings(select, sampleReadings(), { isValid: false });

  const got = optionTexts(select);
  const want = [
    "CPU Core #1 (%)",
    "CPU Core #2 (%)",
    "CPU Core #10 (%)",
    "Core #1 (MHz)",
    "Core #2 (MHz)",
    "Core #10 (MHz)",
  ];
  assert(got.join("|") === want.join("|"), `unexpected index reading order: ${got.join("|")}`);
}

function testCompositeReadingSortUsesNaturalLabelOrder() {
  const select = new FakeSelect();
  const sandbox = loadScriptSandbox("com.exension.hwinfo.sdPlugin/composite_pi.js", {
    elementsById: {
      slot0_readingSelect: select,
    },
  });
  sandbox.currentSettings = {
    slots: [{ readingId: 0, isValid: false }],
  };

  sandbox.populateReadingSelect(0, sampleReadings());

  const got = optionTexts(select);
  const want = [
    "CPU Core #1 (%)",
    "CPU Core #2 (%)",
    "CPU Core #10 (%)",
    "Core #1 (MHz)",
    "Core #2 (MHz)",
    "Core #10 (MHz)",
  ];
  assert(got.join("|") === want.join("|"), `unexpected composite reading order: ${got.join("|")}`);
}

function testDerivedReadingSortUsesNaturalLabelOrder() {
  const select = new FakeSelect();
  const sandbox = loadScriptSandbox("com.exension.hwinfo.sdPlugin/derived_pi.js", {
    elementsById: {
      slot0_readingSelect: select,
    },
  });
  sandbox.currentSettings = {
    slots: [{ readingId: 0, isValid: false }],
  };

  sandbox.populateReadingSelect(0, sampleReadings());

  const got = optionTexts(select);
  const want = [
    "CPU Core #1 (%)",
    "CPU Core #2 (%)",
    "CPU Core #10 (%)",
    "Core #1 (MHz)",
    "Core #2 (MHz)",
    "Core #10 (MHz)",
  ];
  assert(got.join("|") === want.join("|"), `unexpected derived reading order: ${got.join("|")}`);
}

function testCompositeApplySettingsClearsStaleBoundsAndKeepsZero() {
  const slot0Min = new FakeElement({ value: "stale" });
  const slot0Max = new FakeElement({ value: "stale" });
  const slot1Min = new FakeElement({ value: "stale" });
  const slot1Max = new FakeElement({ value: "stale" });
  const sandbox = loadScriptSandbox("com.exension.hwinfo.sdPlugin/composite_pi.js", {
    elementsById: {
      slot0_min: slot0Min,
      slot0_max: slot0Max,
      slot1_min: slot1Min,
      slot1_max: slot1Max,
    },
  });

  sandbox.applySettingsToUI({
    slotCount: 2,
    slots: [{ min: 0, max: 0 }, {}],
  });

  assert(String(slot0Min.value) === "0", `expected slot0 min to keep zero, got ${slot0Min.value}`);
  assert(String(slot0Max.value) === "0", `expected slot0 max to keep zero, got ${slot0Max.value}`);
  assert(slot1Min.value === "", `expected slot1 min to clear stale value, got ${slot1Min.value}`);
  assert(slot1Max.value === "", `expected slot1 max to clear stale value, got ${slot1Max.value}`);
}

function testDerivedApplySettingsClearsStaleBoundsAndKeepsZero() {
  const derivedMin = new FakeElement({ value: "stale" });
  const derivedMax = new FakeElement({ value: "stale" });
  const sandbox = loadScriptSandbox("com.exension.hwinfo.sdPlugin/derived_pi.js", {
    elementsById: {
      derived_min: derivedMin,
      derived_max: derivedMax,
    },
  });

  sandbox.applySettingsToUI({
    formula: "sum",
    slotCount: 2,
    min: 0,
    max: 0,
    slots: [],
  });

  assert(String(derivedMin.value) === "0", `expected derived min to keep zero, got ${derivedMin.value}`);
  assert(String(derivedMax.value) === "0", `expected derived max to keep zero, got ${derivedMax.value}`);
}

function main() {
  testNormalizeSnoozeDurations();
  testApplySnoozeDurationsToUI();
  testReadSnoozeDurationsFromUI();
  testBindSnoozeControlsSendsPayload();
  testIndexReadingSortGroupsByPrefixAndNaturalLabel();
  testCompositeReadingSortUsesNaturalLabelOrder();
  testDerivedReadingSortUsesNaturalLabelOrder();
  testCompositeApplySettingsClearsStaleBoundsAndKeepsZero();
  testDerivedApplySettingsClearsStaleBoundsAndKeepsZero();
  process.stdout.write("reading-pi tests ok (9 cases)\n");
}

main();
