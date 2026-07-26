'use strict';
// Composite per-slot global threshold suppress/unsuppress tests.
// Covers: composite slot-level SuppressedGlobalIDs (#41).
//
// Key indices 14–16.

const {
  pass, fail, summary, sleep,
  waitForDeckBridge, connectPI, waitForMessage, sendToPlugin, sdpi,
  createSlot, deleteSlot,
  mockSet, mockReset,
  getCompositeTileSettings, getTileSettings, readGlobalSettings,
} = require('./helpers');

const SETTINGS_ACTION  = 'com.moeilijk.hwinfo.settings';
const COMPOSITE_ACTION = 'com.moeilijk.hwinfo.composite';
const READING_ACTION   = 'com.moeilijk.hwinfo.reading';
const POLL_MS          = 1000;
const DWELL_MS         = 1000;
const COOLDOWN_MS      = 5000;

const KEY_SETTINGS  = 14;
const KEY_COMPOSITE = 15;
const KEY_READING   = 16;  // reference reading tile (not suppressed)

function waitForEval(extra = 0) { return sleep(POLL_MS * 3 + extra); }

async function configureCompositeSlot(wsPort, ctx, slotIdx, sensorUID, readingLabel) {
  const { ws } = await connectPI(wsPort, ctx, COMPOSITE_ACTION);
  const readingsP = waitForMessage(ws, msg => {
    if (msg.event !== 'sendToPropertyInspector') return undefined;
    const pl = msg.payload || {};
    if (pl.readings && Array.isArray(pl.readings) && pl.slotIndex === slotIdx) return pl.readings;
  });
  sdpi(ws, ctx, COMPOSITE_ACTION, `slot${slotIdx}_sensorSelect`, sensorUID);
  const readings = await readingsP;
  const r = readings.find(r => r.label === readingLabel);
  if (!r) { ws.close(); throw new Error(`reading "${readingLabel}" not found`); }
  sdpi(ws, ctx, COMPOSITE_ACTION, `slot${slotIdx}_readingSelect`, String(r.id));
  await sleep(800);
  ws.close();
}

async function run() {
  console.log('── composite slot global suppress/unsuppress tests ──');

  await mockReset();
  const { wsPort, piPort } = await waitForDeckBridge(20000);
  console.log(`DeckBridge: wsPort=${wsPort} piPort=${piPort}`);

  for (const k of [KEY_SETTINGS, KEY_COMPOSITE, KEY_READING]) await deleteSlot(piPort, k);
  await sleep(500);

  const settingsCtx  = await createSlot(piPort, KEY_SETTINGS,  SETTINGS_ACTION);
  const compositeCtx = await createSlot(piPort, KEY_COMPOSITE, COMPOSITE_ACTION);
  const readingCtx   = await createSlot(piPort, KEY_READING,   READING_ACTION);
  await sleep(1000);

  await sleep(1500);

  // Configure composite slot 0 = CPU Package (temp)
  await configureCompositeSlot(wsPort, compositeCtx, 0, '/mockcpu/0', 'CPU Package');
  await sleep(1000);

  // Configure reference reading tile = CPU Package (temp)
  {
    const { ws, payload } = await connectPI(wsPort, readingCtx, READING_ACTION);
    const sensor = (payload.sensors || []).find(s => s.uid === '/mockcpu/0');
    if (!sensor) { ws.close(); throw new Error('sensor not found'); }
    const readingsP = waitForMessage(ws, msg => {
      if (msg.event !== 'sendToPropertyInspector') return undefined;
      const pl = msg.payload || {};
      if (pl.readings && Array.isArray(pl.readings)) return pl.readings;
    });
    sdpi(ws, readingCtx, READING_ACTION, 'sensorSelect', '/mockcpu/0');
    const readings = await readingsP;
    const r = readings.find(r => r.label === 'CPU Package');
    sdpi(ws, readingCtx, READING_ACTION, 'readingSelect', String(r.id));
    await sleep(800);
    ws.close();
  }
  await sleep(1000);

  console.log('setup complete');

  // Open settings PI for global threshold management
  const { ws: settingsWs } = await connectPI(wsPort, settingsCtx, SETTINGS_ACTION);

  // Clean up any leftover global thresholds
  {
    const gs = readGlobalSettings();
    for (const g of (gs.globalThresholds || [])) {
      sendToPlugin(settingsWs, settingsCtx, SETTINGS_ACTION, { deleteGlobalThreshold: g.id });
      await sleep(200);
    }
  }

  // Add global threshold for Temp type
  const before = (readGlobalSettings().globalThresholds || []).map(t => t.id);
  sendToPlugin(settingsWs, settingsCtx, SETTINGS_ACTION, { addGlobalThreshold: 'TempHigh' });
  await sleep(600);
  const after = readGlobalSettings().globalThresholds || [];
  const newG = after.find(t => !before.includes(t.id));
  if (!newG) { settingsWs.close(); throw new Error('global threshold not created'); }
  const gId = newG.id;

  sendToPlugin(settingsWs, settingsCtx, SETTINGS_ACTION, {
    updateGlobalThreshold: { id: gId, field: 'thresholdReadingType', value: 'Temp', checked: false }
  });
  await sleep(150);
  sendToPlugin(settingsWs, settingsCtx, SETTINGS_ACTION, {
    updateGlobalThreshold: { id: gId, field: 'thresholdOperator', value: '>', checked: false }
  });
  await sleep(150);
  sendToPlugin(settingsWs, settingsCtx, SETTINGS_ACTION, {
    updateGlobalThreshold: { id: gId, field: 'thresholdValue', value: '80', checked: false }
  });
  await sleep(400);

  // ── TEST 1: Global threshold auto-applies to composite slot ─────────────────
  console.log('\n[test 1] global Temp threshold auto-applies to composite slot 0');

  // Baseline: 45°C → not firing
  await waitForEval();
  let cs = await getCompositeTileSettings(wsPort, compositeCtx);
  if (!cs.slots[0].currentThresholdId) {
    pass('test 1a — composite slot 0 not active at 45°C');
  } else {
    fail(`test 1a — unexpected active threshold: ${cs.slots[0].currentThresholdId}`);
  }

  // Trigger
  await mockSet('/mockcpu/0/temperature/0', 90);
  await waitForEval(DWELL_MS);

  cs = await getCompositeTileSettings(wsPort, compositeCtx);
  if (cs.slots[0].currentThresholdId === gId) {
    pass('test 1b — global Temp threshold active on composite slot 0 at 90°C');
  } else {
    fail(`test 1b — expected ${gId}, got: ${cs.slots[0].currentThresholdId}`);
  }

  await mockSet('/mockcpu/0/temperature/0', 45);
  console.log('waiting for cooldown...');
  await sleep(COOLDOWN_MS + POLL_MS * 2);

  // ── TEST 2: Composite slot suppress ─────────────────────────────────────────
  console.log('\n[test 2] composite slot 0 suppresses global threshold');

  // Suppress on slot 0 of composite tile
  {
    const { ws } = await connectPI(wsPort, compositeCtx, COMPOSITE_ACTION);
    sendToPlugin(ws, compositeCtx, COMPOSITE_ACTION, {
      sdpi_collection: { key: 'slot0_suppressGlobal', value: gId }
    });
    await sleep(500);
    ws.close();
  }

  // Raise temp: composite slot 0 should NOT fire, reading tile SHOULD fire
  await mockSet('/mockcpu/0/temperature/0', 90);
  await waitForEval(DWELL_MS);

  cs = await getCompositeTileSettings(wsPort, compositeCtx);
  if (!cs.slots[0].currentThresholdId) {
    pass('test 2a — composite slot 0 suppressed: global does not fire');
  } else {
    fail(`test 2a — composite slot 0 unexpectedly fired: ${cs.slots[0].currentThresholdId}`);
  }

  const rs = await getTileSettings(wsPort, readingCtx, READING_ACTION);
  if (rs.currentThresholdId === gId) {
    pass('test 2b — global still fires on unsuppressed reading tile');
  } else {
    fail(`test 2b — reading tile expected ${gId}, got: ${rs.currentThresholdId}`);
  }

  // ── TEST 3: Composite slot unsuppress ────────────────────────────────────────
  console.log('\n[test 3] composite slot 0 unsuppress: global fires again');

  {
    const { ws } = await connectPI(wsPort, compositeCtx, COMPOSITE_ACTION);
    sendToPlugin(ws, compositeCtx, COMPOSITE_ACTION, {
      sdpi_collection: { key: 'slot0_unsuppressGlobal', value: gId }
    });
    await sleep(500);
    ws.close();
  }
  // Sensors still at 90°C
  await waitForEval(DWELL_MS);

  cs = await getCompositeTileSettings(wsPort, compositeCtx);
  if (cs.slots[0].currentThresholdId === gId) {
    pass('test 3 — global fires on composite slot 0 after unsuppress');
  } else {
    fail(`test 3 — expected ${gId} after unsuppress, got: ${cs.slots[0].currentThresholdId}`);
  }

  // ── Cleanup ─────────────────────────────────────────────────────────────────
  await mockSet('/mockcpu/0/temperature/0', 45);
  sendToPlugin(settingsWs, settingsCtx, SETTINGS_ACTION, { deleteGlobalThreshold: gId });
  await sleep(300);
  settingsWs.close();
  await mockReset();
  for (const k of [KEY_SETTINGS, KEY_COMPOSITE, KEY_READING]) await deleteSlot(piPort, k);

  summary();
}

run().catch(e => { console.error(e); process.exit(1); });
