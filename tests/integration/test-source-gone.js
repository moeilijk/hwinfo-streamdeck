'use strict';
// Source-gone tests (issue #127): when the local source stops polling (HWiNFO
// closed), Composite and Derived tiles fall back to the same placeholder as a
// Reading tile instead of keeping their last values, and they return to live
// rendering when the source comes back.
//
// Key indices 27-29.

const http = require('http');
const {
  pass, fail, summary, sleep,
  waitForDeckBridge, connectPI, waitForMessage, sdpi,
  createSlot, deleteSlot,
  mockSet, mockReset,
} = require('./helpers');

const READING_ACTION   = 'com.moeilijk.hwinfo.reading';
const COMPOSITE_ACTION = 'com.moeilijk.hwinfo.composite';
const DERIVED_ACTION   = 'com.moeilijk.hwinfo.derived';
const KEY_READING   = 27;
const KEY_COMPOSITE = 28;
const KEY_DERIVED   = 29;
const MOCK_PORT  = process.env.MOCK_CONTROL_PORT || 9999;
const STATE_BASE = process.env.DECKBRIDGE_URL || 'http://127.0.0.1:34075';

function mockLocalSource(available) {
  return new Promise((resolve, reject) => {
    const data = JSON.stringify({ id: 'local', available });
    const req = http.request({ host: '127.0.0.1', port: MOCK_PORT, path: '/source', method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(data) }
    }, res => {
      res.resume();
      res.on('end', () => res.statusCode === 200 ? resolve() : reject(new Error('mock /source ' + res.statusCode)));
    });
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

// Poll until the tile image equals (want=true) or differs from (want=false)
// `ref`; the poll interval may have been raised by earlier tests.
async function waitForImage(context, ref, want, timeoutMs = 15000) {
  const deadline = Date.now() + timeoutMs;
  let img = '';
  while (Date.now() < deadline) {
    await sleep(500);
    img = await tileImage(context);
    if (img && (img === ref) === want) return img;
  }
  return img;
}

async function configureSlot(wsPort, ctx, action, selectPrefix, slotIdx, sensorUID, readingLabel) {
  const { ws } = await connectPI(wsPort, ctx, action);
  const readingsP = waitForMessage(ws, msg => {
    if (msg.event !== 'sendToPropertyInspector') return undefined;
    const pl = msg.payload || {};
    if (!pl.readings || !Array.isArray(pl.readings)) return undefined;
    if (slotIdx === null || pl.slotIndex === slotIdx) return pl.readings;
  });
  sdpi(ws, ctx, action, `${selectPrefix}sensorSelect`, sensorUID);
  const readings = await readingsP;
  const r = readings.find(x => x.label === readingLabel);
  if (!r) { ws.close(); throw new Error(`reading "${readingLabel}" not found on ${sensorUID}`); }
  sdpi(ws, ctx, action, `${selectPrefix}readingSelect`, String(r.id));
  await sleep(800);
  ws.close();
}

async function run() {
  console.log('── source-gone tests ──');

  await mockReset();
  const { wsPort, piPort } = await waitForDeckBridge(20000);
  console.log(`DeckBridge: wsPort=${wsPort} piPort=${piPort}`);

  for (const k of [KEY_READING, KEY_COMPOSITE, KEY_DERIVED]) await deleteSlot(piPort, k);
  await sleep(300);
  const ctxReading   = await createSlot(piPort, KEY_READING,   READING_ACTION);
  const ctxComposite = await createSlot(piPort, KEY_COMPOSITE, COMPOSITE_ACTION);
  const ctxDerived   = await createSlot(piPort, KEY_DERIVED,   DERIVED_ACTION);
  await sleep(1500);

  await configureSlot(wsPort, ctxReading, READING_ACTION, '', null, '/mockcpu/0', 'CPU Package');
  await configureSlot(wsPort, ctxComposite, COMPOSITE_ACTION, 'slot0_', 0, '/mockcpu/0', 'CPU Package');
  {
    const { ws } = await connectPI(wsPort, ctxDerived, DERIVED_ACTION);
    sdpi(ws, ctxDerived, DERIVED_ACTION, 'derived_slotCount', '2');
    await sleep(300);
    sdpi(ws, ctxDerived, DERIVED_ACTION, 'derived_formula', 'average');
    await sleep(600);
    ws.close();
  }
  await configureSlot(wsPort, ctxDerived, DERIVED_ACTION, 'slot0_', 0, '/mockcpu/0', 'CPU Package');
  await configureSlot(wsPort, ctxDerived, DERIVED_ACTION, 'slot1_', 1, '/mockcpu/0', 'CPU Core 0');
  await sleep(2500);

  // ── TEST 1: all three tiles render live ──
  console.log('\n[test 1] tiles render while the source polls');
  const live = {
    reading:   await tileImage(ctxReading),
    composite: await tileImage(ctxComposite),
    derived:   await tileImage(ctxDerived),
  };
  for (const [name, img] of Object.entries(live)) {
    if (img.length > 0) pass(`test 1 — ${name} tile rendered an image`);
    else fail(`test 1 — ${name} tile has no image`);
  }

  // ── TEST 2: source gone → all three show the same placeholder ──
  console.log('\n[test 2] source stops polling');
  await mockLocalSource(false);
  const placeholder = await waitForImage(ctxReading, live.reading, false);
  if (placeholder && placeholder !== live.reading) pass('test 2a — reading tile leaves live rendering');
  else fail('test 2a — reading tile image unchanged after the source went away');
  const compositeDown = await waitForImage(ctxComposite, placeholder, true);
  if (compositeDown === placeholder) pass('test 2b — composite tile shows the reading tile placeholder');
  else fail('test 2b — composite tile does not show the placeholder');
  const derivedDown = await waitForImage(ctxDerived, placeholder, true);
  if (derivedDown === placeholder) pass('test 2c — derived tile shows the reading tile placeholder');
  else fail('test 2c — derived tile does not show the placeholder');

  // ── TEST 3: source back → composite and derived render live again ──
  console.log('\n[test 3] source polls again');
  await mockLocalSource(true);
  await mockSet('/mockcpu/0/temperature/0', 66);
  const compositeUp = await waitForImage(ctxComposite, placeholder, false);
  if (compositeUp && compositeUp !== placeholder) pass('test 3a — composite tile resumes rendering');
  else fail('test 3a — composite tile stayed on the placeholder');
  const derivedUp = await waitForImage(ctxDerived, placeholder, false);
  if (derivedUp && derivedUp !== placeholder) pass('test 3b — derived tile resumes rendering');
  else fail('test 3b — derived tile stayed on the placeholder');

  // ── TEST 4: a second outage shows the placeholder again ──
  console.log('\n[test 4] source stops polling a second time');
  await mockLocalSource(false);
  const compositeDown2 = await waitForImage(ctxComposite, placeholder, true);
  if (compositeDown2 === placeholder) pass('test 4a — composite tile shows the placeholder again');
  else fail('test 4a — composite tile does not show the placeholder on the second outage');
  const derivedDown2 = await waitForImage(ctxDerived, placeholder, true);
  if (derivedDown2 === placeholder) pass('test 4b — derived tile shows the placeholder again');
  else fail('test 4b — derived tile does not show the placeholder on the second outage');

  await mockLocalSource(true);
  await mockReset();
  for (const k of [KEY_READING, KEY_COMPOSITE, KEY_DERIVED]) await deleteSlot(piPort, k);

  summary();
}

run().catch(err => { console.error(err); process.exit(1); });
