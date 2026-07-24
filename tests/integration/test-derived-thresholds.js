'use strict';
// Derived tile tests: global threshold auto-apply, suppress/unsuppress, settings persistence.
// Derived tiles have no per-tile addThreshold UI; thresholds come from global thresholds only.
//
// Key indices 24–26 (keys 30/31 are DeckBridge nav keys on the XL device).

const {
  pass, fail, summary, sleep,
  waitForDeckBridge, connectPI, waitForMessage, sendToPlugin, sdpi,
  createSlot, deleteSlot,
  mockSet, mockReset,
  getDerivedTileSettings, getTileSettings, readGlobalSettings,
} = require('./helpers');

const SETTINGS_ACTION = 'com.exension.hwinfo.settings';
const DERIVED_ACTION  = 'com.exension.hwinfo.derived';
const READING_ACTION  = 'com.exension.hwinfo.reading';
const POLL_MS         = 1000;
const DWELL_MS        = 1000;
const COOLDOWN_MS     = 5000;

const KEY_SETTINGS = 24;
const KEY_DERIVED  = 25;
const KEY_READING  = 26;  // reference tile to confirm global still fires

function waitForEval(extra = 0) { return sleep(POLL_MS * 3 + extra); }

// Configure a derived tile slot. Returns the reading id used.
async function configureDerivedSlot(wsPort, ctx, slotIdx, sensorUID, readingLabel) {
  const { ws } = await connectPI(wsPort, ctx, DERIVED_ACTION);
  const readingsP = waitForMessage(ws, msg => {
    if (msg.event !== 'sendToPropertyInspector') return undefined;
    const pl = msg.payload || {};
    if (pl.readings && Array.isArray(pl.readings) && pl.slotIndex === slotIdx) return pl.readings;
  });
  sdpi(ws, ctx, DERIVED_ACTION, `slot${slotIdx}_sensorSelect`, sensorUID);
  const readings = await readingsP;
  const r = readings.find(r => r.label === readingLabel);
  if (!r) { ws.close(); throw new Error(`reading "${readingLabel}" not found`); }
  sdpi(ws, ctx, DERIVED_ACTION, `slot${slotIdx}_readingSelect`, String(r.id));
  await sleep(800);
  ws.close();
  return r.id;
}

// Add a global threshold (all-types) from the settings tile PI.
async function addGlobalThresholdAllTypes(wsPort, settingsCtx, settingsWs, { name, operator, value }) {
  const before = (readGlobalSettings().globalThresholds || []).map(t => t.id);
  sendToPlugin(settingsWs, settingsCtx, SETTINGS_ACTION, { addGlobalThreshold: name || 'Test' });
  await sleep(600);
  const after = readGlobalSettings().globalThresholds || [];
  const newG = after.find(t => !before.includes(t.id));
  if (!newG) throw new Error('addGlobalThreshold: not found after add');
  const id = newG.id;
  // readingType="" means all types (no restriction)
  sendToPlugin(settingsWs, settingsCtx, SETTINGS_ACTION, {
    updateGlobalThreshold: { id, field: 'thresholdReadingType', value: '', checked: false }
  });
  await sleep(150);
  sendToPlugin(settingsWs, settingsCtx, SETTINGS_ACTION, {
    updateGlobalThreshold: { id, field: 'thresholdOperator', value: operator || '>', checked: false }
  });
  await sleep(150);
  sendToPlugin(settingsWs, settingsCtx, SETTINGS_ACTION, {
    updateGlobalThreshold: { id, field: 'thresholdValue', value: String(value ?? 80), checked: false }
  });
  await sleep(400);
  return id;
}

async function run() {
  console.log('── derived tile threshold + settings tests ──');

  await mockReset();
  const { wsPort, piPort } = await waitForDeckBridge(20000);
  console.log(`DeckBridge: wsPort=${wsPort} piPort=${piPort}`);

  for (const k of [KEY_SETTINGS, KEY_DERIVED, KEY_READING]) await deleteSlot(piPort, k);
  await sleep(500);

  const settingsCtx = await createSlot(piPort, KEY_SETTINGS, SETTINGS_ACTION);
  const derivedCtx  = await createSlot(piPort, KEY_DERIVED,  DERIVED_ACTION);
  const readingCtx  = await createSlot(piPort, KEY_READING,  READING_ACTION);
  await sleep(1000);

  await sleep(1500);

  // Configure derived tile: 2 slots, formula=average, both CPU Package temp
  {
    const { ws } = await connectPI(wsPort, derivedCtx, DERIVED_ACTION);
    sdpi(ws, derivedCtx, DERIVED_ACTION, 'derived_slotCount', '2');
    await sleep(300);
    sdpi(ws, derivedCtx, DERIVED_ACTION, 'derived_formula', 'average');
    await sleep(600);
    ws.close();
  }
  await configureDerivedSlot(wsPort, derivedCtx, 0, '/mockcpu/0', 'CPU Package');
  await configureDerivedSlot(wsPort, derivedCtx, 1, '/mockcpu/0', 'CPU Core 0');
  await sleep(1000);

  // Configure reference reading tile: CPU Package temp
  {
    const { ws, payload } = await connectPI(wsPort, readingCtx, READING_ACTION);
    const sensors = payload.sensors || [];
    const sensor = sensors.find(s => s.uid === '/mockcpu/0');
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

  console.log('setup complete — derived tile configured (avg of 2 temp slots)');

  // Open settings tile PI for global threshold management
  const { ws: settingsWs } = await connectPI(wsPort, settingsCtx, SETTINGS_ACTION);

  // Clean up any leftover global thresholds
  {
    const gs = readGlobalSettings();
    for (const g of (gs.globalThresholds || [])) {
      sendToPlugin(settingsWs, settingsCtx, SETTINGS_ACTION, { deleteGlobalThreshold: g.id });
      await sleep(200);
    }
  }

  // ── TEST 1: Global threshold (all types) fires on derived tile ───────────────
  console.log('\n[test 1] global threshold (readingType="") fires on derived tile');

  const gId = await addGlobalThresholdAllTypes(wsPort, settingsCtx, settingsWs, {
    name: 'AllTypesHigh',
    operator: '>',
    value: 80,
  });

  // Baseline: sensors at 45°C/42°C → average ~43.5°C < 80°C
  await waitForEval();
  let ds = await getDerivedTileSettings(wsPort, derivedCtx);
  if (!ds.currentThresholdId) {
    pass('test 1a — derived tile not active at ~43.5°C average');
  } else {
    fail(`test 1a — unexpected active threshold: ${ds.currentThresholdId}`);
  }

  // Trigger: raise both sensors above threshold
  await mockSet('/mockcpu/0/temperature/0', 90);
  await mockSet('/mockcpu/0/temperature/1', 90);
  await waitForEval(DWELL_MS);

  ds = await getDerivedTileSettings(wsPort, derivedCtx);
  if (ds.currentThresholdId === gId) {
    pass('test 1b — global threshold active on derived tile at 90°C average');
  } else {
    fail(`test 1b — expected ${gId}, got: ${ds.currentThresholdId}`);
  }

  // Also verify reference reading tile fires (confirms global is working)
  const rs = await getTileSettings(wsPort, readingCtx, READING_ACTION);
  if (rs.currentThresholdId === gId) {
    pass('test 1c — global threshold also fires on reference reading tile');
  } else {
    fail(`test 1c — reading tile expected ${gId}, got: ${rs.currentThresholdId}`);
  }

  await mockSet('/mockcpu/0/temperature/0', 45);
  await mockSet('/mockcpu/0/temperature/1', 42);

  // Wait for cooldown
  console.log('waiting for cooldown...');
  await sleep(COOLDOWN_MS + POLL_MS * 2);

  // ── TEST 2: Derived tile suppress of global threshold ────────────────────────
  console.log('\n[test 2] derived tile suppresses global threshold');

  // Suppress on derived tile using derived_suppressGlobal
  {
    const { ws } = await connectPI(wsPort, derivedCtx, DERIVED_ACTION);
    sendToPlugin(ws, derivedCtx, DERIVED_ACTION, {
      sdpi_collection: { key: 'derived_suppressGlobal', value: gId }
    });
    await sleep(500);
    ws.close();
  }

  // Raise sensors: derived should not fire, reading tile should
  await mockSet('/mockcpu/0/temperature/0', 90);
  await mockSet('/mockcpu/0/temperature/1', 90);
  await waitForEval(DWELL_MS);

  ds = await getDerivedTileSettings(wsPort, derivedCtx);
  if (!ds.currentThresholdId) {
    pass('test 2a — suppressed global does not fire on derived tile');
  } else {
    fail(`test 2a — suppressed global unexpectedly fired: ${ds.currentThresholdId}`);
  }

  const rs2 = await getTileSettings(wsPort, readingCtx, READING_ACTION);
  if (rs2.currentThresholdId === gId) {
    pass('test 2b — global still fires on unsuppressed reading tile');
  } else {
    fail(`test 2b — reading tile expected ${gId}, got: ${rs2.currentThresholdId}`);
  }

  // ── TEST 3: Unsuppress restores firing ────────────────────────────────────────
  console.log('\n[test 3] unsuppress restores derived tile firing');

  {
    const { ws } = await connectPI(wsPort, derivedCtx, DERIVED_ACTION);
    sendToPlugin(ws, derivedCtx, DERIVED_ACTION, {
      sdpi_collection: { key: 'derived_unsuppressGlobal', value: gId }
    });
    await sleep(500);
    ws.close();
  }
  // Sensors still at 90°C
  await waitForEval(DWELL_MS);

  ds = await getDerivedTileSettings(wsPort, derivedCtx);
  if (ds.currentThresholdId === gId) {
    pass('test 3 — global fires on derived tile after unsuppress');
  } else {
    fail(`test 3 — expected ${gId} after unsuppress, got: ${ds.currentThresholdId}`);
  }

  await mockSet('/mockcpu/0/temperature/0', 45);
  await mockSet('/mockcpu/0/temperature/1', 42);

  console.log('waiting for cooldown...');
  await sleep(COOLDOWN_MS + POLL_MS * 2);

  // ── TEST 4: derived_smoothingAlpha and derived_updateIntervalOverrideMs ───────
  console.log('\n[test 4] derived tile settings persistence');
  {
    const { ws } = await connectPI(wsPort, derivedCtx, DERIVED_ACTION);
    sdpi(ws, derivedCtx, DERIVED_ACTION, 'derived_smoothingAlpha', '0.4');
    await sleep(100);
    sdpi(ws, derivedCtx, DERIVED_ACTION, 'derived_updateIntervalOverrideMs', '2500');
    await sleep(100);
    sdpi(ws, derivedCtx, DERIVED_ACTION, 'derived_graphHeightPct', '70');
    await sleep(100);
    sdpi(ws, derivedCtx, DERIVED_ACTION, 'derived_graphLineThickness', '2');
    await sleep(100);
    sdpi(ws, derivedCtx, DERIVED_ACTION, 'derived_textStroke', '1', { checked: true });
    await sleep(600);
    ws.close();
  }
  ds = await getDerivedTileSettings(wsPort, derivedCtx);
  const checks = [
    [Math.abs((ds.smoothingAlpha || 0) - 0.4) < 0.001, 'smoothingAlpha=0.4'],
    [ds.updateIntervalOverrideMs === 2500,              'updateIntervalOverrideMs=2500'],
    [ds.graphHeightPct === 70,                          'graphHeightPct=70'],
    [ds.graphLineThickness === 2,                       'graphLineThickness=2'],
    [ds.textStroke === true,                            'textStroke=true'],
  ];
  let allOk = true;
  for (const [ok, label] of checks) {
    if (!ok) { fail(`test 4 — ${label} not persisted (got: ${JSON.stringify(ds[label.split('=')[0]])})`); allOk = false; }
  }
  if (allOk) pass('test 4 — smoothingAlpha, updateIntervalOverrideMs, graphHeightPct, graphLineThickness, textStroke all persisted');

  // ── Cleanup ─────────────────────────────────────────────────────────────────
  sendToPlugin(settingsWs, settingsCtx, SETTINGS_ACTION, { deleteGlobalThreshold: gId });
  await sleep(300);
  settingsWs.close();
  await mockReset();
  for (const k of [KEY_SETTINGS, KEY_DERIVED, KEY_READING]) await deleteSlot(piPort, k);

  summary();
}

run().catch(e => { console.error(e); process.exit(1); });
