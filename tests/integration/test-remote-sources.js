'use strict';
// Remote source tests (issue #101): a second sensor source appears in the
// catalog, its readings render on a key, the tile shows a clear (frozen
// placeholder) state when the source goes away, and local tiles keep
// updating throughout.
//
// Key indices 9-10.

const http = require('http');
const {
  pass, fail, summary, sleep,
  waitForDeckBridge, connectPI, waitForMessage, sendToPlugin, sdpi,
  createSlot, deleteSlot,
  mockReset,
} = require('./helpers');

const READING_ACTION = 'com.moeilijk.hwinfo.reading';
const KEY_REMOTE = 9;
const KEY_LOCAL = 10;
const MOCK_PORT = process.env.MOCK_CONTROL_PORT || 9999;
const STATE_BASE = process.env.DECKBRIDGE_URL || 'http://127.0.0.1:34075';

function mockSource(present, available) {
  return new Promise((resolve, reject) => {
    const data = JSON.stringify({ id: 'remote0', present, available });
    const req = http.request({ host: '127.0.0.1', port: MOCK_PORT, path: '/source', method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(data) }
    }, res => {
      let out = '';
      res.on('data', c => out += c);
      res.on('end', () => resolve(out));
    });
    req.on('error', reject);
    req.write(data);
    req.end();
  });
}

function mockSet(path, value) {
  return new Promise((resolve, reject) => {
    const data = JSON.stringify({ path, value });
    const req = http.request({ host: '127.0.0.1', port: MOCK_PORT, path: '/set', method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(data) }
    }, res => { res.resume(); res.on('end', resolve); });
    req.on('error', reject);
    req.write(data);
    req.end();
  });
}

async function tileImage(context) {
  const res = await fetch(`${STATE_BASE}/api/state`);
  if (!res.ok) throw new Error('state ' + res.status);
  const state = await res.json();
  const slot = (state.slots || []).find(s => s.context === context);
  return slot ? (slot.imageDataUrl || '') : '';
}

// Poll until the tile image differs from `from` (the poll interval may have
// been raised to 2s by earlier tests, so allow a generous window).
async function waitForImageChange(context, from, timeoutMs = 12000) {
  const deadline = Date.now() + timeoutMs;
  let img = from;
  while (Date.now() < deadline) {
    await sleep(500);
    img = await tileImage(context);
    if (img && img !== from) return img;
  }
  return img;
}

async function configureTile(wsPort, ctx, sensorUid, readingLabel) {
  const { ws, payload } = await connectPI(wsPort, ctx, READING_ACTION);
  const sensor = (payload.sensors || []).find(s => s.uid === sensorUid);
  if (!sensor) { ws.close(); throw new Error(`sensor ${sensorUid} not in PI payload`); }
  const readingsP = waitForMessage(ws, msg => {
    if (msg.event !== 'sendToPropertyInspector') return undefined;
    const pl = msg.payload || {};
    if (pl.readings && Array.isArray(pl.readings)) return pl.readings;
  });
  sdpi(ws, ctx, READING_ACTION, 'sensorSelect', sensorUid);
  const readings = await readingsP;
  const reading = readings.find(r => r.label === readingLabel);
  if (!reading) { ws.close(); throw new Error(`reading ${readingLabel} not found for ${sensorUid}`); }
  sdpi(ws, ctx, READING_ACTION, 'readingSelect', String(reading.id));
  await sleep(800);
  ws.close();
  return reading;
}

async function run() {
  console.log('── remote source tests ──');

  await mockReset();
  const { wsPort, piPort } = await waitForDeckBridge(20000);
  console.log(`DeckBridge: wsPort=${wsPort} piPort=${piPort}`);

  for (const k of [KEY_REMOTE, KEY_LOCAL]) await deleteSlot(piPort, k);
  await sleep(300);
  const ctxRemote = await createSlot(piPort, KEY_REMOTE, READING_ACTION);
  const ctxLocal = await createSlot(piPort, KEY_LOCAL, READING_ACTION);
  await sleep(1500);

  // ── TEST 1: without remote source, no remote sensors in the PI payload ──
  console.log('\n[test 1] no remote source by default');
  {
    const { ws, payload } = await connectPI(wsPort, ctxRemote, READING_ACTION);
    const remoteSensors = (payload.sensors || []).filter(s => String(s.uid).startsWith('remote0::'));
    if (remoteSensors.length === 0) pass('test 1a — no remote sensors without remote source');
    else fail(`test 1a — unexpected remote sensors: ${remoteSensors.length}`);
    ws.close();
  }

  // ── TEST 2: remote source appears with its sensors and source list ──
  console.log('\n[test 2] remote source appears');
  await mockSource(true, true);
  await sleep(2000);
  {
    const { ws, payload } = await connectPI(wsPort, ctxRemote, READING_ACTION);
    const remoteSensor = (payload.sensors || []).find(s => s.uid === 'remote0::/remotecpu/0');
    if (remoteSensor && remoteSensor.source === 'remote0') pass('test 2a — remote sensor listed with source id');
    else fail('test 2a — remote sensor missing or without source id');
    const catalogP = waitForMessage(ws, msg => {
      if (msg.event !== 'sendToPropertyInspector') return undefined;
      const pl = msg.payload || {};
      if (pl.catalog) return pl.catalog;
    });
    sendToPlugin(ws, ctxRemote, READING_ACTION, { sdpi_collection: { key: 'propertyInspectorConnected', value: 'property_inspector' } });
    const catalog = await catalogP;
    const names = (catalog.sources || []).map(s => s.name);
    if (names.includes('Local') && names.includes('Remote 1')) pass('test 2b — catalog lists Local and Remote 1');
    else fail(`test 2b — catalog sources: ${JSON.stringify(names)}`);
    ws.close();
  }

  // ── TEST 3: remote reading renders and updates on the key ──
  console.log('\n[test 3] remote reading renders');
  await configureTile(wsPort, ctxRemote, 'remote0::/remotecpu/0', 'CPU Package');
  await configureTile(wsPort, ctxLocal, '/mockcpu/0', 'CPU Package');
  await sleep(1500);
  const imgA = await tileImage(ctxRemote);
  if (imgA.length > 0) pass('test 3a — remote tile rendered an image');
  else fail('test 3a — remote tile has no image');
  await mockSet('/remotecpu/0/temperature/0', 85);
  await sleep(2000);
  const imgB = await tileImage(ctxRemote);
  if (imgB && imgB !== imgA) pass('test 3b — remote tile re-renders on value change');
  else fail('test 3b — remote tile image did not change');

  // ── TEST 4: source unavailable → clear frozen state; local unaffected ──
  console.log('\n[test 4] remote source unavailable');
  const localBefore = await tileImage(ctxLocal);
  await mockSource(true, false);
  await sleep(3000);
  const imgDown = await tileImage(ctxRemote);
  if (imgDown && imgDown !== imgB) pass('test 4a — tile leaves live rendering when source is unavailable');
  else fail('test 4a — tile image unchanged after source went unavailable');
  await mockSet('/remotecpu/0/temperature/0', 60);
  await sleep(2000);
  const imgDown2 = await tileImage(ctxRemote);
  if (imgDown2 === imgDown) pass('test 4b — unavailable tile no longer follows values');
  else fail('test 4b — unavailable tile still re-renders on value changes');
  await mockSet('/mockcpu/0/temperature/0', 90);
  await sleep(2000);
  const localAfter = await tileImage(ctxLocal);
  if (localAfter && localAfter !== localBefore) pass('test 4c — local tile keeps updating');
  else fail('test 4c — local tile stopped updating');

  // ── TEST 5: source recovers ──
  console.log('\n[test 5] remote source recovers');
  await mockSource(true, true);
  await mockSet('/remotecpu/0/temperature/0', 42);
  const imgUp = await waitForImageChange(ctxRemote, imgDown);
  if (imgUp && imgUp !== imgDown) pass('test 5 — tile resumes rendering after recovery');
  else fail('test 5 — tile did not recover');

  // cleanup: remove remote source again
  await mockSource(false, false);
  await mockReset();

  summary();
}

run().catch(err => { console.error(err); process.exit(1); });
