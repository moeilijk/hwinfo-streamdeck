#!/usr/bin/env node
"use strict";

// Real-DOM render test for the option controls of the reading tile.
//
// Loads the ACTUAL index_pi.html with the ACTUAL sdpi.css/local.css into jsdom,
// runs the ACTUAL pi_utils.js and index_pi.js in that window, and asserts what a
// user gets: the control is in the right section, it is visible, it shows the
// saved setting, and changing it sends the setting to the plugin.
//
// sdpi.css hides every input[type=checkbox]; a checkbox is only visible when it
// uses the repo's show-label-checkbox pattern. The control case at the end
// proves that cascade is really applied.

const fs = require("fs");
const path = require("path");

const repoRoot = path.resolve(__dirname, "..");
const sdpiDir = path.join(repoRoot, "com.moeilijk.hwinfo.sdPlugin");

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
const read = (f) => fs.readFileSync(path.join(sdpiDir, f), "utf8");
const html = read("index_pi.html");
const css = read("css/sdpi.css") + "\n" + read("css/local.css");

// Checkbox options: element id (= settings key) and the <summary> of its section.
const CHECKBOXES = [
  { id: "resetMinMaxOnPress", section: "Scale", label: "Reset on press" },
  { id: "condensedFont", section: "Display", label: "Condensed font" },
  { id: "textShadow", section: "Display", label: "Text backdrop" },
];

let failures = 0;
function check(cond, msg) {
  if (cond) {
    console.log("ok - " + msg);
  } else {
    failures++;
    console.error("FAIL - " + msg);
  }
}

function bootPi() {
  // The page is parsed as it is. With runScripts "outside-only" jsdom does not
  // run or fetch the page's own <script> and <link> elements, so the scripts
  // and the stylesheets are fed in by hand below.
  const dom = new JSDOM(html, { pretendToBeVisual: true, runScripts: "outside-only" });
  const win = dom.window;
  const style = win.document.createElement("style");
  style.textContent = css;
  win.document.head.appendChild(style);
  win.eval(read("pi_utils.js"));
  win.eval(read("index_pi.js"));
  const sent = [];
  win.eval("uuid = 'ctx-reading'; actionInfo = { action: 'com.moeilijk.hwinfo.reading' };");
  win.__sent = sent;
  win.eval("websocket = { readyState: 1, send: function (m) { window.__sent.push(JSON.parse(m)); } };");
  return { win, sent };
}

const { win, sent } = bootPi();
const doc = win.document;

CHECKBOXES.forEach(({ id, section, label }) => {
  const box = doc.getElementById(id);
  check(!!box && box.type === "checkbox", `${id}: checkbox exists in index_pi.html`);
  if (!box) return;

  const details = box.closest("details");
  const summary = details && details.querySelector("summary");
  check(!!summary && summary.textContent.trim() === section, `${id}: sits in the ${section} section`);

  const row = box.closest(".sdpi-item");
  check(!!row && row.classList.contains("show-label-row"), `${id}: uses the show-label-row pattern`);
  check(box.classList.contains("show-label-checkbox") && box.parentElement.classList.contains("show-label-cell"), `${id}: uses the show-label-checkbox pattern`);
  const rowLabel = row && row.querySelector(".sdpi-item-label");
  check(!!rowLabel && rowLabel.textContent.trim() === label, `${id}: row label is "${label}"`);

  const display = win.getComputedStyle(box).display;
  check(display !== "none", `${id}: is visible (computed display="${display}")`);

  check(box.checked === false, `${id}: unchecked by default`);
  win.applyDisplayOptionSettings({ [id]: true });
  check(box.checked === true, `${id}: shows a saved true setting`);
  win.applyDisplayOptionSettings({});
  check(box.checked === false, `${id}: shows a tile without the setting as off`);

  win.bindDisplayOptionControls();
  const before = sent.length;
  box.checked = true;
  box.dispatchEvent(new win.Event("change", { bubbles: true }));
  const msg = sent[sent.length - 1];
  const c = msg && msg.payload && msg.payload.sdpi_collection;
  check(sent.length === before + 1 && !!c && c.key === id && c.checked === true, `${id}: a user change sends { key: "${id}", checked: true }`);
  check(!!msg && msg.event === "sendToPlugin" && msg.context === "ctx-reading", `${id}: the message targets this tile`);
});

// Select options: element id (= settings key), section, row label, the option
// values in order and the value shown for a tile without the setting.
const SELECTS = [
  { id: "scaleMode", section: "Scale", label: "Range", values: ["manual", "auto", "sensor"], fallback: "manual" },
  { id: "graphStyle", section: "Display", label: "Graph style", values: ["classic", "soft", "soft-load"], fallback: "classic" },
  { id: "unitDisplay", section: "Display", label: "Unit", values: ["inline", "below", "hidden"], fallback: "inline" },
];

// Range options: id of the row (= settings key), row label, slider limits, the
// value shown for a tile without the setting, and a saved value to restore.
const RANGES = [
  { id: "unitFontSize", label: "Unit size", min: "8", max: "20", step: "0.5", fallback: "10.5", saved: 9 },
  { id: "unitOffset", label: "Unit offset", min: "-20", max: "20", step: "0.5", fallback: "0", saved: -3.5 },
  { id: "valueOffset", label: "Value offset", min: "-20", max: "20", step: "0.5", fallback: "0", saved: 6 },
  { id: "lineSpacing", label: "Line spacing", min: "0.5", max: "2", step: "0.1", fallback: "1", saved: 1.4 },
];

win.prepareDOMElements(doc);
win.bindDisplayOptionControls();

SELECTS.forEach(({ id, section, label, values, fallback }) => {
  const sel = doc.getElementById(id);
  check(!!sel && sel.tagName === "SELECT", `${id}: select exists in index_pi.html`);
  if (!sel) return;

  const summary = sel.closest("details") && sel.closest("details").querySelector("summary");
  check(!!summary && summary.textContent.trim() === section, `${id}: sits in the ${section} section`);
  const rowLabel = sel.closest(".sdpi-item").querySelector(".sdpi-item-label");
  check(!!rowLabel && rowLabel.textContent.trim() === label, `${id}: row label is "${label}"`);
  check(sel.classList.contains("sdpi-item-value") && sel.classList.contains("select"), `${id}: uses the sdpi select pattern`);
  check(JSON.stringify(Array.from(sel.options).map((o) => o.value)) === JSON.stringify(values), `${id}: offers ${values.join(", ")}`);
  check(win.getComputedStyle(sel).display !== "none", `${id}: is visible`);

  win.applyDisplayOptionSettings({});
  check(sel.value === fallback, `${id}: a tile without the setting shows "${fallback}"`);
  values.forEach((v) => {
    win.applyDisplayOptionSettings({ [id]: v });
    check(sel.value === v, `${id}: shows a saved "${v}" setting`);
  });

  win.applyDisplayOptionSettings({});
  const before = sent.length;
  sel.value = values[values.length - 1];
  sel.dispatchEvent(new win.Event("change", { bubbles: true }));
  const msg = sent[sent.length - 1];
  const c = msg && msg.payload && msg.payload.sdpi_collection;
  check(sent.length === before + 1 && !!c && c.key === id && c.value === values[values.length - 1], `${id}: a user change sends { key: "${id}", value: "${values[values.length - 1]}" }`);
});

// Range: the manual Min/Max inputs are only shown in Manual. Reset on press is
// only offered when the plugin reports that the sensor source can reset, and
// then it stays available in every mode.
{
  const manual = doc.getElementById("paramsgroup");
  const resetRow = doc.getElementById("resetMinMaxOnPressRow");
  const shown = (el) => win.getComputedStyle(el).display !== "none";
  check(!shown(resetRow), "Reset on press is hidden until the plugin reports reset support");
  win.setResetSupported(false);
  check(!shown(resetRow), "Reset on press is hidden for a source that cannot reset");
  win.setResetSupported(true);
  check(shown(resetRow), "Reset on press is shown for a source that can reset");
  [["manual", true], ["auto", false], ["sensor", false]].forEach(([mode, want]) => {
    win.applyDisplayOptionSettings({ scaleMode: mode });
    check(shown(manual) === want, `scaleMode ${mode} (saved): Min/Max inputs ${want ? "shown" : "hidden"}`);
    check(shown(resetRow), `scaleMode ${mode} (saved): Reset on press stays shown`);
  });
  const sel = doc.getElementById("scaleMode");
  [["auto", false], ["manual", true], ["sensor", false]].forEach(([mode, want]) => {
    sel.value = mode;
    sel.dispatchEvent(new win.Event("change", { bubbles: true }));
    check(shown(manual) === want, `scaleMode ${mode} (picked by the user): Min/Max inputs ${want ? "shown" : "hidden"}`);
  });
  win.setResetSupported(false);
}

RANGES.forEach(({ id, label, min, max, step, fallback, saved }) => {
  const row = doc.getElementById(id);
  const inp = row && row.querySelector("input[type=range]");
  check(!!inp, `${id}: range exists in index_pi.html`);
  if (!inp) return;
  const summary = row.closest("details").querySelector("summary");
  check(summary.textContent.trim() === "Display", `${id}: sits in the Display section`);
  check(row.querySelector(".sdpi-item-label").textContent.trim() === label, `${id}: row label is "${label}"`);
  check(!!row.querySelector(".range-wrap .range-val"), `${id}: uses the range-wrap pattern with a value bubble`);
  check(inp.min === min && inp.max === max && inp.step === step, `${id}: runs from ${min} to ${max} in steps of ${step}`);

  win.applyDisplayOptionSettings({ unitDisplay: "below" });
  check(inp.value === fallback, `${id}: a tile without the setting shows ${fallback}`);
  win.applyDisplayOptionSettings({ unitDisplay: "below", [id]: saved });
  check(inp.value === String(saved), `${id}: shows a saved ${saved}`);
  check(row.querySelector(".range-val").textContent === String(saved), `${id}: the value bubble shows ${saved}`);

  const before = sent.length;
  inp.value = fallback;
  inp.dispatchEvent(new win.Event("change", { bubbles: true }));
  const msg = sent[sent.length - 1];
  const c = msg && msg.payload && msg.payload.sdpi_collection;
  check(sent.length === before + 1 && !!c && c.key === id && c.value === fallback, `${id}: a user change sends { key: "${id}", value: "${fallback}" }`);
});

// The unit follows the value until it has a size and a colour of its own.
{
  const size = doc.querySelector("#unitFontSize input[type=range]");
  const colour = doc.getElementById("unitTextColor");
  win.applyDisplayOptionSettings({ valueFontSize: 16, valueTextColor: "#ff8800" });
  check(size.value === "16", "unitFontSize: follows a value size of 16 while unset");
  check(colour.value === "#ff8800", "unitTextColor: follows the value colour while unset");
  win.applyDisplayOptionSettings({ valueFontSize: 16, valueTextColor: "#ff8800", unitFontSize: 9, unitTextColor: "#00aaff" });
  check(size.value === "9" && colour.value === "#00aaff", "unitFontSize/unitTextColor: own settings win over the value's");

  const before = sent.length;
  colour.value = "#123456";
  colour.dispatchEvent(new win.Event("change", { bubbles: true }));
  const c = sent[sent.length - 1] && sent[sent.length - 1].payload.sdpi_collection;
  check(sent.length === before + 1 && !!c && c.key === "unitTextColor" && c.value === "#123456", 'unitTextColor: a user change sends { key: "unitTextColor", value: "#123456" }');
}

// Unit: its size, offset and colour are only shown while the unit is drawn
// below the value. Value offset and line spacing are always available.
{
  const shown = (id) => win.getComputedStyle(doc.getElementById(id)).display !== "none";
  const unitOnly = ["unitFontSize", "unitOffset", "unitTextColorRow"];
  const always = ["valueOffset", "lineSpacing", "unitDisplay"];
  [["inline", false], ["below", true], ["hidden", false]].forEach(([mode, want]) => {
    win.applyDisplayOptionSettings({ unitDisplay: mode });
    check(unitOnly.every((id) => shown(id) === want), `unitDisplay ${mode} (saved): unit size, offset and colour ${want ? "shown" : "hidden"}`);
    check(always.every(shown), `unitDisplay ${mode} (saved): value offset and line spacing shown`);
  });
  const sel = doc.getElementById("unitDisplay");
  [["below", true], ["hidden", false], ["below", true], ["inline", false]].forEach(([mode, want]) => {
    sel.value = mode;
    sel.dispatchEvent(new win.Event("change", { bubbles: true }));
    check(unitOnly.every((id) => shown(id) === want), `unitDisplay ${mode} (picked by the user): unit size, offset and colour ${want ? "shown" : "hidden"}`);
  });
}

// The Format help has to warn about a duplicated unit.
check(/leave the unit out of the format/i.test(doc.getElementById("format").parentElement.textContent), "Format help mentions leaving the unit out when it is shown below or hidden");

// The plugin's own messages drive "Reset on press". This boots a second page
// through the real connectElgatoStreamDeckSocket with a stand-in WebSocket and
// feeds it the payloads the plugin sends, so the message handler itself runs.
{
  const live = bootPi().win;
  live.eval(`
    window.__socket = null;
    WebSocket = function () { window.__socket = this; this.readyState = 1; this.send = function () {}; };
    window.__handlerErrors = [];
  `);
  live.connectElgatoStreamDeckSocket(
    "1",
    "ctx-live",
    "registerPropertyInspector",
    JSON.stringify({ colors: { highlightColor: "#0078ffff" } }),
    JSON.stringify({ action: "com.moeilijk.hwinfo.reading" })
  );
  const row = live.document.getElementById("resetMinMaxOnPressRow");
  const stroke = live.document.getElementById("textStroke");
  const shown = () => live.getComputedStyle(row).display !== "none";
  const settings = { format: "", divisor: "", min: 0, max: 100, textStroke: true, thresholds: null };
  const deliver = (payload) => {
    try {
      live.__socket.onmessage({ data: JSON.stringify({ event: "sendToPropertyInspector", payload }) });
      return null;
    } catch (e) {
      return e;
    }
  };

  check(!shown(), "live page: Reset on press is hidden before the plugin has answered");
  let err = deliver({ sensors: [], settings, resetSupported: true });
  check(err === null, "live page: the settings message is handled without an error" + (err ? " (" + err.message + ")" : ""));
  check(shown(), "live page: resetSupported true in the plugin's message shows Reset on press");
  check(stroke.checked === true, "live page: the settings after Reset on press in the handler are still applied");

  // Later messages carry settings without the field. They say nothing about
  // the source and must leave the row as it is.
  err = deliver({ readings: [], settings });
  check(err === null && shown(), "live page: a readings message without resetSupported keeps Reset on press shown");
  err = deliver({ thresholds: [], settings, globalThresholds: [] });
  check(err === null && shown(), "live page: a thresholds message without resetSupported keeps Reset on press shown");

  err = deliver({ sensors: [], settings, resetSupported: false });
  check(err === null && !shown(), "live page: resetSupported false hides Reset on press");
}

// Control: a bare checkbox computes to display:none, so the visibility checks
// above fail if a control ever regresses to that markup.
const ctrl = new JSDOM(`<!DOCTYPE html><html><head><style>${css}</style></head><body><input class="sdpi-item-value" id="bareBox" type="checkbox" /></body></html>`, { pretendToBeVisual: true }).window;
check(ctrl.getComputedStyle(ctrl.document.getElementById("bareBox")).display === "none", "control: a bare sdpi checkbox computes display:none (cascade is applied)");

if (failures) {
  console.error(`test-reading-options-render: ${failures} failure(s)`);
  process.exit(1);
}
console.log("test-reading-options-render: ok");
