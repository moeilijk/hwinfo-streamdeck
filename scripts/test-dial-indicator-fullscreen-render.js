#!/usr/bin/env node
"use strict";

// Real-DOM render test: the dial "In fullscreen" page-indicator toggle must be a
// VISIBLE checkbox in the Property Inspector. sdpi.css sets
//   input[type="checkbox"] { display: none; }
// so a bare sdpi checkbox is invisible unless it uses the repo's visible
// show-label-checkbox pattern (local.css overrides display to a native checkbox).
//
// This loads the ACTUAL dial_pi.html + the ACTUAL sdpi.css/local.css into jsdom
// and asserts getComputedStyle(display) is not "none". A control case proves the
// CSS cascade is really applied: a bare sdpi-item-value checkbox computes to
// display:none, so this test fails if the toggle ever regresses to that markup.

const fs = require("fs");
const path = require("path");
const assert = require("assert");

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
const html = fs.readFileSync(path.join(sdpiDir, "dial_pi.html"), "utf8");
const sdpiCss = fs.readFileSync(path.join(sdpiDir, "css/sdpi.css"), "utf8");
const localCss = fs.readFileSync(path.join(sdpiDir, "css/local.css"), "utf8");

function domWithCss(bodyHtml) {
  const dom = new JSDOM(
    `<!DOCTYPE html><html><head><style>${sdpiCss}\n${localCss}</style></head><body>${bodyHtml}</body></html>`,
    { pretendToBeVisual: true }
  );
  return dom.window;
}

let failures = 0;
function check(cond, msg) {
  if (cond) {
    console.log("ok - " + msg);
  } else {
    failures++;
    console.error("FAIL - " + msg);
  }
}

// 1) The real toggle, rendered with the real CSS, must be visible.
const win = domWithCss(html.replace(/^[\s\S]*?<body[^>]*>/i, "").replace(/<\/body>[\s\S]*$/i, ""));
const toggle = win.document.getElementById("indicatorFullscreen");
check(!!toggle, "indicatorFullscreen checkbox exists in dial_pi.html");
if (toggle) {
  const display = win.getComputedStyle(toggle).display;
  check(display !== "none", `indicatorFullscreen is visible (computed display="${display}", not none)`);
  check(toggle.type === "checkbox", "indicatorFullscreen is a checkbox input");
}

// 2) Control: a bare sdpi-item-value checkbox MUST compute to display:none.
//    This proves the CSS cascade is actually applied, so case (1) is meaningful
//    and would fail if the toggle regressed to the invisible bare markup.
const ctrl = domWithCss('<input class="sdpi-item-value" id="bareBox" type="checkbox" />');
const bare = ctrl.document.getElementById("bareBox");
check(
  ctrl.getComputedStyle(bare).display === "none",
  "control: a bare sdpi checkbox computes display:none (cascade is applied)"
);

if (failures) {
  console.error(`test-dial-indicator-fullscreen-render: ${failures} failure(s)`);
  process.exit(1);
}
console.log("test-dial-indicator-fullscreen-render: ok");
