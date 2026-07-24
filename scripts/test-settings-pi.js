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
    this.checked = initial.checked || false;
    this.textContent = initial.textContent || "";
    this.style = initial.style || {};
    this.handlers = {};
    this.children = [];
    this.disabled = initial.disabled || false;
    this.selected = initial.selected || false;
    this.dataset = initial.dataset || {};
    this._innerHTML = "";
  }
  addEventListener(evt, fn) {
    this.handlers[evt] = this.handlers[evt] || [];
    this.handlers[evt].push(fn);
  }
  trigger(evt) {
    const fns = this.handlers[evt] || [];
    fns.forEach((fn) => fn({
      target: this,
      preventDefault() {},
      stopPropagation() {},
    }));
  }
  appendChild(child) {
    this.children.push(child);
  }
  querySelectorAll() {
    return [];
  }
  get options() {
    return this.children;
  }
  set innerHTML(value) {
    this._innerHTML = value;
    if (value === "") {
      this.children = [];
    }
  }
  get innerHTML() {
    return this._innerHTML;
  }
}

function loadSandbox(opts = {}) {
  const elements = {
    pollInterval: new FakeElement({ value: "1000" }),
    currentRate: new FakeElement({ textContent: "" }),
    tileBackground: new FakeElement({ value: "#112233" }),
    tileTextColor: new FakeElement({ value: "#aabbcc" }),
    showLabel: new FakeElement({ checked: true }),
    connectionStatus: new FakeElement({ textContent: "", style: {} }),
    addGlobalThresholdBtn: new FakeElement(),
    newGlobalThresholdName: new FakeElement(),
  };
  const sent = [];
  const intervalFns = [];

  const sandbox = {
    console,
    JSON,
    URLSearchParams,
    setTimeout: (fn) => {
      fn();
      return 1;
    },
    setInterval: (fn) => {
      intervalFns.push(fn);
      return intervalFns.length;
    },
    clearInterval: () => {},
    window: null,
    websocket: null,
    WebSocket: function () {
      return opts.mockSocket || { readyState: 1, send() {} };
    },
    document: {
      readyState: "complete",
      addEventListener() {},
      activeElement: null,
      getElementById(id) {
        return elements[id] || null;
      },
      createElement() {
        return new FakeElement();
      },
      querySelector() {
        return null;
      },
    },
  };
  sandbox.window = sandbox;

  vm.createContext(sandbox);
  vm.runInContext(fs.readFileSync("com.exension.hwinfo.sdPlugin/settings_pi.js", "utf8"), sandbox);

  sandbox.websocket = {
    readyState: 1,
    send(msg) {
      sent.push(JSON.parse(msg));
    },
  };

  return { sandbox, elements, sent, getIntervalFns: () => intervalFns };
}

function testShowLabelPayload() {
  const { sandbox, elements, sent } = loadSandbox();
  sandbox.context = "ctx-settings";
  sandbox.uuid = "ctx-fallback";

  sandbox.saveTileSettings("force");
  elements.showLabel.checked = false;
  sandbox.saveTileSettings("force");

  const updates = sent.filter(
    (m) => m.event === "sendToPlugin" && m.payload && m.payload.updateTileAppearance
  );
  assert(updates.length >= 2, "expected at least two updateTileAppearance events");
  assert(updates[0].payload.updateTileAppearance.showLabel === true, "first payload should be true");
  assert(updates[updates.length - 1].payload.updateTileAppearance.showLabel === false, "last payload should be false");
}

function testContextFanout() {
  const { sandbox, sent } = loadSandbox();
  sandbox.context = "ctx-action";
  sandbox.uuid = "ctx-pi";

  sandbox.saveTileSettings("force");
  const setSettings = sent.filter((m) => m.event === "setSettings");
  const updateMsgs = sent.filter((m) => m.event === "sendToPlugin" && m.payload && m.payload.updateTileAppearance);
  assert(setSettings.length === 1, "expected one setSettings message");
  assert(updateMsgs.length === 1, "expected one updateTileAppearance message");
  assert(setSettings[0].context === "ctx-pi", "setSettings should use PI uuid context");
  assert(updateMsgs[0].context === "ctx-pi", "sendToPlugin should use PI uuid context");
}

function testPollIntervalEvents() {
  const { sandbox, elements, sent } = loadSandbox();
  sandbox.context = "ctx-settings";
  sandbox.uuid = "ctx-fallback";
  elements.pollInterval.value = "2000";
  elements.pollInterval.trigger("change");

  const global = sent.find((m) => m.event === "setGlobalSettings");
  assert(global, "missing setGlobalSettings");
  assert(global.payload.pollInterval === 2000, "poll interval payload mismatch");

  const pollMsgs = sent.filter((m) => m.event === "sendToPlugin" && m.payload && m.payload.setPollInterval === 2000);
  assert(pollMsgs.length === 1, "expected one setPollInterval message");
}

function testDidReceiveSettingsAppliesUi() {
  const ws = {
    readyState: 1,
    send() {},
    onopen: null,
    onmessage: null,
  };
  const { sandbox, elements } = loadSandbox({ mockSocket: ws });
  sandbox.connectElgatoStreamDeckSocket("12345", "uuid-x", "registerPropertyInspector", "{}", JSON.stringify({
    action: "com.exension.hwinfo.settings",
    context: "ctx-x",
  }));
  ws.onmessage({
    data: JSON.stringify({
      event: "didReceiveSettings",
      payload: {
        settings: {
          tileBackground: "#334455",
          tileTextColor: "#fedcba",
          showLabel: false,
        },
      },
    }),
  });
  assert(elements.tileBackground.value === "#334455", "tileBackground not applied");
  assert(elements.tileTextColor.value === "#fedcba", "tileTextColor not applied");
  assert(elements.showLabel.checked === false, "showLabel not applied");
}

function testMalformedInputsDoNotCrash() {
  const ws = {
    readyState: 1,
    send() {},
    onopen: null,
    onmessage: null,
  };
  const { sandbox } = loadSandbox({ mockSocket: ws });
  sandbox.connectElgatoStreamDeckSocket("12345", "uuid-x", "registerPropertyInspector", "{bad", "{bad");
  ws.onmessage({ data: "{bad" });
}

function testPollingFallbackSave() {
  const { sandbox, elements, sent, getIntervalFns } = loadSandbox();
  sandbox.context = "ctx-settings";
  sandbox.uuid = "ctx-fallback";

  const ws = {
    readyState: 1,
    send(msg) {
      sent.push(JSON.parse(msg));
    },
    onopen: null,
    onmessage: null,
  };
  sandbox.WebSocket = function () {
    return ws;
  };
  sandbox.connectElgatoStreamDeckSocket("12345", "uuid-x", "registerPropertyInspector", "{}", JSON.stringify({
    action: "com.exension.hwinfo.settings",
    context: "ctx-settings",
  }));
  ws.onopen();

  elements.tileBackground.value = "#445566";
  const tick = getIntervalFns()[1];
  assert(typeof tick === "function", "interval fallback function missing");
  tick();

  const updates = sent.filter((m) => m.event === "sendToPlugin" && m.payload && m.payload.updateTileAppearance);
  assert(updates.length >= 1, "polling fallback did not send update");
}

function testStatusHeartbeatIsLightweight() {
  const { sandbox, sent, getIntervalFns } = loadSandbox();
  sandbox.context = "ctx-settings";
  sandbox.uuid = "ctx-pi";

  const ws = {
    readyState: 1,
    send(msg) {
      sent.push(JSON.parse(msg));
    },
    onopen: null,
    onmessage: null,
  };
  sandbox.WebSocket = function () {
    return ws;
  };
  sandbox.connectElgatoStreamDeckSocket("12345", "uuid-x", "registerPropertyInspector", "{}", JSON.stringify({
    action: "com.exension.hwinfo.settings",
    context: "ctx-settings",
  }));
  ws.onopen();

  const statusTick = getIntervalFns()[0];
  assert(typeof statusTick === "function", "status heartbeat function missing");
  statusTick();

  const statusMessages = sent.filter(
    (m) => m.event === "sendToPlugin" && m.payload && (m.payload.settingsConnected || m.payload.requestSettingsStatus)
  );
  assert(statusMessages.length >= 2, "expected initial and heartbeat status messages");
  assert(statusMessages[0].payload.settingsConnected === true, "initial message should request full settings state");
  assert(statusMessages[statusMessages.length - 1].payload.requestSettingsStatus === true, "heartbeat should request lightweight status only");
}

function testFocusedInputIsNotOverwritten() {
  const { sandbox, elements } = loadSandbox();
  elements.newGlobalThresholdName.value = "editing";
  sandbox.document.activeElement = elements.newGlobalThresholdName;

  sandbox.applyInputValue(elements.newGlobalThresholdName, "other");
  sandbox.document.activeElement = null;
  sandbox.applyInputValue(elements.newGlobalThresholdName, "synced");

  assert(elements.newGlobalThresholdName.value === "synced", "unfocused input should sync");
}

function testAddGlobalThresholdButtonSendsCommand() {
  const { sandbox, elements, sent } = loadSandbox();
  sandbox.context = "ctx-settings";
  sandbox.uuid = "ctx-pi";
  elements.newGlobalThresholdName.value = "Global CPU";

  elements.addGlobalThresholdBtn.trigger("click");

  const add = sent.find((m) => m.event === "sendToPlugin" && m.payload && m.payload.addGlobalThreshold === "Global CPU");
  assert(add, "add global threshold command missing");
  assert(elements.newGlobalThresholdName.value === "", "name input should clear after add");
}

function testGlobalThresholdWithoutEnabledRendersOpen() {
  const { sandbox } = loadSandbox();
  const elements = {};
  [
    ".threshold-item",
    ".threshold-name",
    ".threshold-text",
    ".threshold-reading-type",
    ".threshold-operator",
    ".threshold-value",
    ".threshold-hysteresis",
    ".threshold-dwell",
    ".threshold-cooldown",
    ".threshold-bg",
    ".threshold-fg",
    ".threshold-hl",
    ".threshold-vt",
    ".threshold-tc",
    ".threshold-toggle",
    ".threshold-settings",
    ".threshold-sticky-toggle",
    ".threshold-advanced-toggle",
    ".threshold-advanced-panel",
    ".threshold-remove",
  ].forEach((selector) => {
    elements[selector] = new FakeElement();
  });
  elements[".threshold-item"].dataset = {};

  const fakeClone = {
    querySelector(selector) {
      return elements[selector] || null;
    },
  };
  sandbox.document.querySelector = (selector) => {
    if (selector !== "#globalThresholdTemplate") return null;
    return { content: { cloneNode: () => fakeClone } };
  };

  sandbox.createGlobalThresholdElement({ id: "g1", name: "Global CPU" });

  assert(elements[".threshold-toggle"].textContent === "on", "missing enabled should render as on");
  assert(elements[".threshold-settings"].style.display === "block", "settings should be visible for enabled threshold");
}

function main() {
  testShowLabelPayload();
  testContextFanout();
  testPollIntervalEvents();
  testDidReceiveSettingsAppliesUi();
  testMalformedInputsDoNotCrash();
  testPollingFallbackSave();
  testStatusHeartbeatIsLightweight();
  testFocusedInputIsNotOverwritten();
  testAddGlobalThresholdButtonSendsCommand();
  testGlobalThresholdWithoutEnabledRendersOpen();
  process.stdout.write("settings-pi tests ok (10 cases)\n");
}

main();
