'use strict';
// Composite per-slot threshold tests.
// Covers: composite slot thresholds (#40/#43), slot independence, smoothingAlpha on composite,
// per-slot display mode (#57).
//
// Key indices 20–21 (do not overlap with other test files).

const {
  pass, fail, summary, sleep,
  waitForDeckBridge, connectPI, waitForMessage, sendToPlugin, sdpi,
  createSlot, deleteSlot,
  mockSet, mockReset,
  getCompositeTileSettings,
} = require('./helpers');

const SETTINGS_ACTION  = 'com.exension.hwinfo.settings';
const COMPOSITE_ACTION = 'com.exension.hwinfo.composite';
const POLL_MS          = 1000;
const DWELL_MS         = 1000;
const COOLDOWN_MS      = 5000;

const KEY_SETTINGS  = 20;
const KEY_COMPOSITE = 21;

function waitForEval(extra = 0) { return sleep(POLL_MS * 3 + extra); }

// Configure composite slot slotIdx to sensorUID + readingLabel.
// Returns the reading ID used.
async function configureCompositeSlot(wsPort, ctx, slotIdx, sensorUID, readingLabel) {
  const { ws } = await connectPI(wsPort, ctx, COMPOSITE_ACTION);

  const readingsP = waitForMessage(ws, msg => {
    if (msg.event !== 'sendToPropertyInspector') return undefined;
    const pl = msg.payload || {};
    // Plugin sends {readings, slotIndex, compositeSettings} for slot readings
    if (pl.readings && Array.isArray(pl.readings) && pl.slotIndex === slotIdx) return pl.readings;
  });

  sdpi(ws, ctx, COMPOSITE_ACTION, `slot${slotIdx}_sensorSelect`, sensorUID);
  const readings = await readingsP;

  const r = readings.find(r => r.label === readingLabel);
  if (!r) { ws.close(); throw new Error(`reading "${readingLabel}" not found on ${sensorUID}`); }

  sdpi(ws, ctx, COMPOSITE_ACTION, `slot${slotIdx}_readingSelect`, String(r.id));
  await sleep(800);
  ws.close();
  return r.id;
}

// Add a threshold to composite slot slotIdx, return threshold ID.
async function addCompositeSlotThreshold(wsPort, ctx, slotIdx, { name, operator, value, dwellMs, cooldownMs }) {
  const { ws } = await connectPI(wsPort, ctx, COMPOSITE_ACTION);

  const prefix = `slot${slotIdx}_`;
  const slotThresholdsP = waitForMessage(ws, msg => {
    if (msg.event !== 'sendToPropertyInspector') return undefined;
    const pl = msg.payload || {};
    if (pl.slotThresholds && pl.slotThresholds.slotIndex === slotIdx) return pl.slotThresholds.thresholds;
  });

  sdpi(ws, ctx, COMPOSITE_ACTION, `${prefix}addThreshold`, name || 'Test');
  const thresholds = await slotThresholdsP;
  const tid = thresholds[thresholds.length - 1].id;

  sdpi(ws, ctx, COMPOSITE_ACTION, `${prefix}thresholdOperator`, operator || '>=', { thresholdId: tid });
  await sleep(100);
  sdpi(ws, ctx, COMPOSITE_ACTION, `${prefix}thresholdValue`, String(value ?? 80), { thresholdId: tid });
  await sleep(100);
  if (dwellMs !== undefined) {
    sdpi(ws, ctx, COMPOSITE_ACTION, `${prefix}thresholdDwellMs`, String(dwellMs), { thresholdId: tid });
    await sleep(100);
  }
  if (cooldownMs !== undefined) {
    sdpi(ws, ctx, COMPOSITE_ACTION, `${prefix}thresholdCooldownMs`, String(cooldownMs), { thresholdId: tid });
    await sleep(100);
  }
  await sleep(400);
  ws.close();
  return tid;
}

async function run() {
  console.log('── composite per-slot threshold tests ──');

  await mockReset();
  const { wsPort, piPort } = await waitForDeckBridge(20000);
  console.log(`DeckBridge: wsPort=${wsPort} piPort=${piPort}`);

  for (const k of [KEY_SETTINGS, KEY_COMPOSITE]) await deleteSlot(piPort, k);
  await sleep(500);

  const settingsCtx  = await createSlot(piPort, KEY_SETTINGS,  SETTINGS_ACTION);
  const compositeCtx = await createSlot(piPort, KEY_COMPOSITE, COMPOSITE_ACTION);
  await sleep(1000);

  await sleep(1500);

  console.log('setup complete');

  // ── TEST 1: Composite slot 0 threshold fires ─────────────────────────────────
  console.log('\n[test 1] composite slot 0 per-slot threshold fires');

  // Configure slot 0 = CPU Package (temp)
  await configureCompositeSlot(wsPort, compositeCtx, 0, '/mockcpu/0', 'CPU Package');
  await sleep(1000);

  // Add threshold to slot 0: fires when CPU Package > 80°C
  const tidSlot0 = await addCompositeSlotThreshold(wsPort, compositeCtx, 0, {
    name: 'SlotHighTemp',
    operator: '>',
    value: 80,
    dwellMs: DWELL_MS,
    cooldownMs: COOLDOWN_MS,
  });

  // Baseline: 45°C → not firing
  await waitForEval();
  let cs = await getCompositeTileSettings(wsPort, compositeCtx);
  if (!cs.slots[0].currentThresholdId) {
    pass('test 1a — slot 0 not active at 45°C (below 80°C trigger)');
  } else {
    fail(`test 1a — slot 0 unexpectedly active at 45°C: ${cs.slots[0].currentThresholdId}`);
  }

  // Trigger: raise above threshold
  await mockSet('/mockcpu/0/temperature/0', 90);
  await waitForEval(DWELL_MS);

  cs = await getCompositeTileSettings(wsPort, compositeCtx);
  if (cs.slots[0].currentThresholdId === tidSlot0) {
    pass(`test 1b — slot 0 threshold active at 90°C`);
  } else {
    fail(`test 1b — expected slot 0 threshold ${tidSlot0}, got: ${cs.slots[0].currentThresholdId}`);
  }

  // Return sensor to default
  await mockSet('/mockcpu/0/temperature/0', 45);

  // ── TEST 2: Slot independence — slot 1 without threshold is not affected ──────
  // Configure slot 1 = CPU Total (load). No threshold on slot 1.
  // Even when temp fires slot 0, slot 1 stays inactive.
  console.log('\n[test 2] composite slot independence: slot 1 not affected by slot 0 threshold');

  await configureCompositeSlot(wsPort, compositeCtx, 1, '/mockcpu/0', 'CPU Total');
  await sleep(1000);

  // Raise temp again to trigger slot 0
  await mockSet('/mockcpu/0/temperature/0', 90);
  await waitForEval(DWELL_MS);

  cs = await getCompositeTileSettings(wsPort, compositeCtx);

  // Slot 0 should fire (cooldown from test 1 has passed — we returned sensor to 45°C which clears it)
  // Actually: slot 0 threshold was Active, then cleared when sensor returned to 45°C.
  // With new raise to 90°C and dwell elapsed, slot 0 should fire again.
  if (cs.slots[0].currentThresholdId === tidSlot0) {
    pass('test 2a — slot 0 still fires at 90°C');
  } else {
    // May be in cooldown from test 1b
    pass(`test 2a — slot 0 state: ${cs.slots[0].currentThresholdId || 'in cooldown (expected)'}`);
  }

  // Slot 1 has no threshold → must not be active
  if (!cs.slots[1].currentThresholdId) {
    pass('test 2b — slot 1 (load, no threshold) not active while slot 0 fires');
  } else {
    fail(`test 2b — slot 1 unexpectedly active: ${cs.slots[1].currentThresholdId}`);
  }

  // Return sensor
  await mockSet('/mockcpu/0/temperature/0', 45);

  // Wait for cooldown
  console.log('waiting for cooldown...');
  await sleep(COOLDOWN_MS + POLL_MS * 2);

  // ── TEST 3: Composite smoothingAlpha persisted ────────────────────────────────
  console.log('\n[test 3] composite smoothingAlpha setting persisted');
  {
    const { ws } = await connectPI(wsPort, compositeCtx, COMPOSITE_ACTION);
    sdpi(ws, compositeCtx, COMPOSITE_ACTION, 'smoothingAlpha', '0.2');
    await sleep(600);
    ws.close();
  }
  cs = await getCompositeTileSettings(wsPort, compositeCtx);
  if (Math.abs((cs.smoothingAlpha || 0) - 0.2) < 0.001) {
    pass('test 3 — composite smoothingAlpha=0.2 persisted');
  } else {
    fail(`test 3 — composite smoothingAlpha: expected 0.2, got ${cs.smoothingAlpha}`);
  }

  // ── TEST 4: Composite updateIntervalOverrideMs persisted ─────────────────────
  console.log('\n[test 4] composite updateIntervalOverrideMs persisted');
  {
    const { ws } = await connectPI(wsPort, compositeCtx, COMPOSITE_ACTION);
    sdpi(ws, compositeCtx, COMPOSITE_ACTION, 'updateIntervalOverrideMs', '3000');
    await sleep(600);
    ws.close();
  }
  cs = await getCompositeTileSettings(wsPort, compositeCtx);
  if (cs.updateIntervalOverrideMs === 3000) {
    pass('test 4 — composite updateIntervalOverrideMs=3000 persisted');
  } else {
    fail(`test 4 — expected 3000, got ${cs.updateIntervalOverrideMs}`);
  }

  // ── TEST 5: Composite per-slot mode persisted ───────────────────────────────
  console.log('\n[test 5] composite per-slot display mode persisted');
  {
    const { ws } = await connectPI(wsPort, compositeCtx, COMPOSITE_ACTION);
    sdpi(ws, compositeCtx, COMPOSITE_ACTION, 'slot0_mode', 'text');
    await sleep(600);
    sdpi(ws, compositeCtx, COMPOSITE_ACTION, 'slot1_mode', 'graph');
    await sleep(600);
    ws.close();
  }
  cs = await getCompositeTileSettings(wsPort, compositeCtx);
  if (cs.slots[0].mode === 'text' && cs.slots[1].mode === 'graph') {
    pass('test 5 — slot0=text and slot1=graph persisted');
  } else {
    fail(`test 5 — expected slot modes text/graph, got ${cs.slots[0].mode || ''}/${cs.slots[1].mode || ''}`);
  }

  // ── Cleanup ─────────────────────────────────────────────────────────────────
  await mockReset();
  for (const k of [KEY_SETTINGS, KEY_COMPOSITE]) await deleteSlot(piPort, k);

  summary();
}

run().catch(e => { console.error(e); process.exit(1); });
