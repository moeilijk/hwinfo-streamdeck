'use strict';
const WebSocket = require('ws');
const http = require('http');

// ── terminal output ──────────────────────────────────────────────────────────
let passCount = 0, failCount = 0;
function pass(msg) { passCount++; console.log('[PASS]', msg); }
function fail(msg) { failCount++; console.error('[FAIL]', msg); }
function fatal(msg) { console.error('[FATAL]', msg); process.exit(1); }
function summary() {
  console.log(`\n── ${passCount} passed, ${failCount} failed ──`);
  if (failCount > 0) process.exit(1);
}

// ── timing ───────────────────────────────────────────────────────────────────
function sleep(ms) { return new Promise(r => setTimeout(r, ms)); }

// ── DeckBridge port discovery ────────────────────────────────────────────────
const LOG_PATH = '/tmp/deckbridge.log';
const fs = require('fs');

function readDeckBridgePorts() {
  let content = '';
  try { content = fs.readFileSync(LOG_PATH, 'utf8'); } catch (_) { return null; }
  // Log line: "Dashboard: http://127.0.0.1:PIPORT?wsPort=WSPORT"
  const lines = content.split('\n').reverse();
  for (const line of lines) {
    if (!line.includes('Dashboard:')) continue;
    const wsMatch = line.match(/wsPort=(\d+)/);
    const piMatch = line.match(/127\.0\.0\.1:(\d+)/);
    if (wsMatch && piMatch) return { wsPort: parseInt(wsMatch[1]), piPort: parseInt(piMatch[1]) };
  }
  return null;
}

async function waitForDeckBridge(timeoutMs = 15000) {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    const ports = readDeckBridgePorts();
    if (ports) return ports;
    await sleep(500);
  }
  fatal('DeckBridge not ready after ' + timeoutMs + 'ms');
}

// ── WebSocket helpers ────────────────────────────────────────────────────────
// connectPI opens a PI WebSocket connection and resolves on the first meaningful payload.
// - Reading tiles send { sensors, settings }
// - Settings tiles send { connectionStatus, currentRate, sourceProfiles, ... }
// - Composite tiles send { compositeSettings }
function connectPI(wsPort, context, action) {
  return new Promise((resolve, reject) => {
    const ws = new WebSocket(`ws://localhost:${wsPort}`);
    const timer = setTimeout(() => reject(new Error(`timeout connecting PI context=${context}`)), 10000);
    ws.on('error', e => { clearTimeout(timer); reject(e); });
    ws.on('open', () => ws.send(JSON.stringify({ event: 'registerPropertyInspector', uuid: context })));
    ws.on('message', raw => {
      const msg = JSON.parse(raw);
      if (msg.event !== 'sendToPropertyInspector') return;
      const pl = msg.payload || {};
      if (pl.error) { clearTimeout(timer); reject(new Error('plugin error: ' + JSON.stringify(pl))); return; }
      // Any of these fields indicates a successful connect response
      if (pl.sensors || pl.connectionStatus || pl.compositeSettings || pl.catalog) {
        clearTimeout(timer);
        resolve({ ws, payload: pl });
      }
    });
  });
}

function waitForMessage(ws, predicate, timeoutMs = 8000) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('waitForMessage timeout')), timeoutMs);
    const handler = raw => {
      const msg = JSON.parse(raw);
      const result = predicate(msg);
      if (result !== undefined && result !== false) {
        ws.off('message', handler);
        clearTimeout(timer);
        resolve(result);
      }
    };
    ws.on('message', handler);
  });
}

// Send a sendToPlugin event from "PI" side
function sendToPlugin(ws, context, action, payloadObj) {
  ws.send(JSON.stringify({ event: 'sendToPlugin', context, action, payload: payloadObj }));
}

// Send sdpi_collection event (standard sdpi input event)
function sdpi(ws, context, action, key, value, extra) {
  const col = { key, value: value !== undefined ? String(value) : '' };
  if (extra) Object.assign(col, extra);
  sendToPlugin(ws, context, action, { sdpi_collection: col });
}

// ── DeckBridge HTTP API ──────────────────────────────────────────────────────
function httpPost(piPort, path, body) {
  return new Promise((resolve, reject) => {
    const data = JSON.stringify(body);
    const req = http.request({ host: '127.0.0.1', port: piPort, path, method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(data) }
    }, res => {
      let out = '';
      res.on('data', c => out += c);
      res.on('end', () => {
        try { resolve(JSON.parse(out)); } catch (_) { resolve(out); }
      });
    });
    req.on('error', reject);
    req.write(data);
    req.end();
  });
}

async function createSlot(piPort, keyIndex, actionId) {
  // POST /api/slots returns full state object; find the new slot by keyIndex
  const state = await httpPost(piPort, '/api/slots', {
    keyIndex,
    pluginId: 'com.exension.hwinfo',
    actionId,
  });
  if (!state || !Array.isArray(state.slots)) {
    throw new Error('createSlot: unexpected response: ' + JSON.stringify(state).slice(0, 200));
  }
  const slot = state.slots.find(s => s.keyIndex === keyIndex && s.pluginId === 'com.exension.hwinfo' && s.actionId === actionId);
  if (!slot || !slot.context) {
    throw new Error(`createSlot: slot for keyIndex=${keyIndex} not found in state`);
  }
  return slot.context;
}

function deleteSlot(piPort, keyIndex, deviceId) {
  return new Promise((resolve) => {
    const params = `?keyIndex=${keyIndex}&deviceId=${encodeURIComponent(deviceId || 'deckbridge-xl-0')}`;
    const req = http.request({ host: '127.0.0.1', port: piPort, path: '/api/slots' + params, method: 'DELETE' },
      res => { res.resume(); res.on('end', resolve); });
    req.on('error', resolve);
    req.end();
  });
}

// ── Mock sensor server ───────────────────────────────────────────────────────
const MOCK_PORT = 9999;

function mockSet(path, value) {
  return new Promise((resolve, reject) => {
    const data = JSON.stringify({ path, value });
    const req = http.request({ host: '127.0.0.1', port: MOCK_PORT, path: '/set', method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(data) }
    }, res => {
      let out = '';
      res.on('data', c => out += c);
      res.on('end', () => resolve(JSON.parse(out)));
    });
    req.on('error', reject);
    req.write(data);
    req.end();
  });
}

function mockReset() {
  return new Promise((resolve, reject) => {
    const req = http.request({ host: '127.0.0.1', port: MOCK_PORT, path: '/reset', method: 'POST' },
      res => { res.resume(); res.on('end', resolve); });
    req.on('error', reject);
    req.end();
  });
}

// ── DeckBridge profile + global settings readers ─────────────────────────────
const PROFILE_PATH = `${process.env.HOME}/.config/DeckBridge/profiles/default.json`;
const GLOBAL_SETTINGS_PATH = `${process.env.HOME}/.config/DeckBridge/settings/com.exension.hwinfo.json`;

function readGlobalSettings() {
  try { return JSON.parse(fs.readFileSync(GLOBAL_SETTINGS_PATH, 'utf8')); } catch (_) { return {}; }
}

function readProfile() {
  try { return JSON.parse(fs.readFileSync(PROFILE_PATH, 'utf8')); } catch (_) { return null; }
}

function getSlotSettings(context) {
  const profile = readProfile();
  if (!profile) return null;
  for (const page of profile.pages || []) {
    for (const slot of page.slots || []) {
      if (slot.context === context) return slot.settings || {};
    }
  }
  return null;
}

// ── Get current derived tile settings via PI reconnect ───────────────────────
async function getDerivedTileSettings(wsPort, context) {
  return new Promise((resolve, reject) => {
    const ws = new WebSocket(`ws://localhost:${wsPort}`);
    const timer = setTimeout(() => { ws.close(); reject(new Error('getDerivedTileSettings timeout')); }, 10000);
    ws.on('error', e => { clearTimeout(timer); reject(e); });
    ws.on('open', () => ws.send(JSON.stringify({ event: 'registerPropertyInspector', uuid: context })));
    ws.on('message', raw => {
      const msg = JSON.parse(raw);
      if (msg.event !== 'sendToPropertyInspector') return;
      const pl = msg.payload || {};
      if (pl.error) return;
      if (pl.derivedSettings) {
        clearTimeout(timer);
        ws.close();
        resolve(pl.derivedSettings);
      }
    });
  });
}

// ── Get current composite tile settings via PI reconnect ─────────────────────
async function getCompositeTileSettings(wsPort, context) {
  return new Promise((resolve, reject) => {
    const ws = new WebSocket(`ws://localhost:${wsPort}`);
    const timer = setTimeout(() => { ws.close(); reject(new Error('getCompositeTileSettings timeout')); }, 10000);
    ws.on('error', e => { clearTimeout(timer); reject(e); });
    ws.on('open', () => ws.send(JSON.stringify({ event: 'registerPropertyInspector', uuid: context })));
    ws.on('message', raw => {
      const msg = JSON.parse(raw);
      if (msg.event !== 'sendToPropertyInspector') return;
      const pl = msg.payload || {};
      if (pl.error) return;
      if (pl.compositeSettings) {
        clearTimeout(timer);
        ws.close();
        resolve(pl.compositeSettings);
      }
    });
  });
}

// ── Get current tile settings via PI reconnect ────────────────────────────────
async function getTileSettings(wsPort, context, action) {
  return new Promise((resolve, reject) => {
    const ws = new WebSocket(`ws://localhost:${wsPort}`);
    const timer = setTimeout(() => { ws.close(); reject(new Error('getTileSettings timeout')); }, 10000);
    ws.on('error', e => { clearTimeout(timer); reject(e); });
    ws.on('open', () => ws.send(JSON.stringify({ event: 'registerPropertyInspector', uuid: context })));
    ws.on('message', raw => {
      const msg = JSON.parse(raw);
      if (msg.event !== 'sendToPropertyInspector') return;
      const pl = msg.payload || {};
      if (pl.error) return;
      // Reading tile sends {sensors, settings} or {catalog, settings}
      if (pl.settings) {
        clearTimeout(timer);
        ws.close();
        resolve(pl.settings);
      }
    });
  });
}

module.exports = {
  pass, fail, fatal, summary, sleep,
  waitForDeckBridge, connectPI, waitForMessage, sendToPlugin, sdpi,
  httpPost, createSlot, deleteSlot,
  mockSet, mockReset,
  readProfile, getSlotSettings, getTileSettings, getCompositeTileSettings, getDerivedTileSettings,
  readGlobalSettings,
  MOCK_PORT,
};
