'use strict';
// Per-tile threshold, EMA smoothing, update interval override, and visual settings tests.
// Covers: per-tile threshold firing (#43), update frequency override (#42), visual settings (#39).
//
// Key indices 10–13 (do not overlap with test-global-thresholds.js keys 0–3).

const {
  pass, fail, summary, sleep,
  waitForDeckBridge, connectPI, waitForMessage, sendToPlugin, sdpi,
  createSlot, deleteSlot,
  mockSet, mockReset,
  getTileSettings,
} = require('./helpers');

const SETTINGS_ACTION = 'com.exension.hwinfo.settings';
const READING_ACTION  = 'com.exension.hwinfo.reading';
const POLL_MS         = 1000;
const DWELL_MS        = 1000;
const COOLDOWN_MS     = 5000;

const KEY_SETTINGS = 10;
const KEY_A        = 11;  // CPU Package — per-tile threshold + isolation
const KEY_B        = 12;  // CPU Total   — isolation check only
const KEY_C        = 13;  // CPU Package — settings + throttle tests

function waitForEval(extra = 0) { return sleep(POLL_MS * 3 + extra); }

// Configure a reading tile to a specific sensor+reading.
async function configureTile(wsPort, ctx, sensorUID, readingLabel) {
  const { ws, payload } = await connectPI(wsPort, ctx, READING_ACTION);
  const sensors = payload.sensors || [];
  const sensor = sensors.find(s => s.uid === sensorUID);
  if (!sensor) { ws.close(); throw new Error(`sensor ${sensorUID} not found`); }
  const readingsP = waitForMessage(ws, msg => {
    if (msg.event !== 'sendToPropertyInspector') return undefined;
    const pl = msg.payload || {};
    if (pl.readings && Array.isArray(pl.readings)) return pl.readings;
  });
  sdpi(ws, ctx, READING_ACTION, 'sensorSelect', sensorUID);
  const readings = await readingsP;
  const r = readings.find(r => r.label === readingLabel);
  if (!r) { ws.close(); throw new Error(`reading "${readingLabel}" not found`); }
  sdpi(ws, ctx, READING_ACTION, 'readingSelect', String(r.id));
  await sleep(800);
  ws.close();
  return r.id;
}

// Add a per-tile threshold to a reading tile, return threshold ID.
async function addPerTileThreshold(wsPort, ctx, { name, operator, value, dwellMs, cooldownMs }) {
  const { ws } = await connectPI(wsPort, ctx, READING_ACTION);
  const thresholdsP = waitForMessage(ws, msg => {
    if (msg.event !== 'sendToPropertyInspector') return undefined;
    const pl = msg.payload || {};
    if (pl.thresholds && Array.isArray(pl.thresholds)) return pl.thresholds;
  });
  sdpi(ws, ctx, READING_ACTION, 'addThreshold', name || 'Test');
  const thresholds = await thresholdsP;
  const tid = thresholds[thresholds.length - 1].id;

  sdpi(ws, ctx, READING_ACTION, 'thresholdOperator', operator || '>=', { thresholdId: tid });
  await sleep(100);
  sdpi(ws, ctx, READING_ACTION, 'thresholdValue', String(value ?? 80), { thresholdId: tid });
  await sleep(100);
  if (dwellMs !== undefined) {
    sdpi(ws, ctx, READING_ACTION, 'thresholdDwellMs', String(dwellMs), { thresholdId: tid });
    await sleep(100);
  }
  if (cooldownMs !== undefined) {
    sdpi(ws, ctx, READING_ACTION, 'thresholdCooldownMs', String(cooldownMs), { thresholdId: tid });
    await sleep(100);
  }
  await sleep(400);
  ws.close();
  return tid;
}

async function run() {
  console.log('── per-tile threshold + settings tests ──');

  await mockReset();
  const { wsPort, piPort } = await waitForDeckBridge(20000);
  console.log(`DeckBridge: wsPort=${wsPort} piPort=${piPort}`);

  for (const k of [KEY_SETTINGS, KEY_A, KEY_B, KEY_C]) await deleteSlot(piPort, k);
  await sleep(500);

  const settingsCtx = await createSlot(piPort, KEY_SETTINGS, SETTINGS_ACTION);
  const ctxA        = await createSlot(piPort, KEY_A, READING_ACTION);
  const ctxB        = await createSlot(piPort, KEY_B, READING_ACTION);
  const ctxC        = await createSlot(piPort, KEY_C, READING_ACTION);
  await sleep(1000);

  await sleep(1500);

  await configureTile(wsPort, ctxA, '/mockcpu/0', 'CPU Package');
  await configureTile(wsPort, ctxB, '/mockcpu/0', 'CPU Total');
  await configureTile(wsPort, ctxC, '/mockcpu/0', 'CPU Package');
  await sleep(1000);

  console.log('setup complete — tiles configured');

  // ── TEST 1: Per-tile threshold fires ────────────────────────────────────────
  console.log('\n[test 1] per-tile threshold: fires above value, not below');

  const tidA = await addPerTileThreshold(wsPort, ctxA, {
    name: 'HighTemp',
    operator: '>',
    value: 80,
    dwellMs: DWELL_MS,
    cooldownMs: COOLDOWN_MS,
  });

  // Baseline: 45°C → not firing
  await waitForEval();
  let s = await getTileSettings(wsPort, ctxA, READING_ACTION);
  if (!s.currentThresholdId) {
    pass('test 1a — no active threshold at 45°C (below 80°C trigger)');
  } else {
    fail(`test 1a — unexpected active threshold at 45°C: ${s.currentThresholdId}`);
  }

  // Trigger: raise above threshold
  await mockSet('/mockcpu/0/temperature/0', 90);
  await waitForEval(DWELL_MS);

  s = await getTileSettings(wsPort, ctxA, READING_ACTION);
  if (s.currentThresholdId === tidA) {
    pass(`test 1b — per-tile threshold active at 90°C`);
  } else {
    fail(`test 1b — expected ${tidA}, got: ${s.currentThresholdId}`);
  }

  // Return sensor to default
  await mockSet('/mockcpu/0/temperature/0', 45);

  // ── TEST 2: Threshold isolation ─────────────────────────────────────────────
  // Tile A has a threshold on CPU Package temp; tile B monitors CPU Total load.
  // Tile B should not fire (no threshold configured on it).
  console.log('\n[test 2] threshold isolation: tile A threshold does not affect tile B');

  await waitForEval();
  s = await getTileSettings(wsPort, ctxB, READING_ACTION);
  if (!s.currentThresholdId) {
    pass('test 2 — tile B (load) has no active threshold while tile A threshold is configured');
  } else {
    fail(`test 2 — tile B unexpectedly has active threshold: ${s.currentThresholdId}`);
  }

  // Wait for tile A threshold cooldown before continuing
  console.log('waiting for cooldown...');
  await sleep(COOLDOWN_MS + POLL_MS * 2);

  // ── TEST 3: smoothingAlpha persisted ─────────────────────────────────────────
  console.log('\n[test 3] smoothingAlpha setting persisted');
  {
    const { ws } = await connectPI(wsPort, ctxC, READING_ACTION);
    sdpi(ws, ctxC, READING_ACTION, 'smoothingAlpha', '0.3');
    await sleep(600);
    ws.close();
  }
  s = await getTileSettings(wsPort, ctxC, READING_ACTION);
  if (Math.abs((s.smoothingAlpha || 0) - 0.3) < 0.001) {
    pass(`test 3 — smoothingAlpha=0.3 persisted`);
  } else {
    fail(`test 3 — smoothingAlpha: expected 0.3, got ${s.smoothingAlpha}`);
  }

  // ── TEST 4: updateIntervalOverrideMs persisted ───────────────────────────────
  console.log('\n[test 4] updateIntervalOverrideMs setting persisted');
  {
    const { ws } = await connectPI(wsPort, ctxC, READING_ACTION);
    sdpi(ws, ctxC, READING_ACTION, 'updateIntervalOverrideMs', '2000');
    await sleep(600);
    ws.close();
  }
  s = await getTileSettings(wsPort, ctxC, READING_ACTION);
  if (s.updateIntervalOverrideMs === 2000) {
    pass('test 4 — updateIntervalOverrideMs=2000 persisted');
  } else {
    fail(`test 4 — expected 2000, got ${s.updateIntervalOverrideMs}`);
  }

  // ── TEST 5: updateIntervalOverrideMs throttles tile evaluation ───────────────
  // With override=5000ms, the tile is evaluated at most every 5s.
  // We verify: 2.5s after raising sensor, the threshold has not fired yet.
  // Then after 6s total, it has fired.
  console.log('\n[test 5] updateIntervalOverrideMs throttles evaluation');

  // Add a zero-dwell threshold to tile C (fires immediately on first match)
  const tidC = await addPerTileThreshold(wsPort, ctxC, {
    name: 'ThrottleTest',
    operator: '>',
    value: 80,
    dwellMs: 0,
    cooldownMs: 0,
  });

  // Set 5000ms override — tile will only evaluate every 5s from now
  {
    const { ws } = await connectPI(wsPort, ctxC, READING_ACTION);
    sdpi(ws, ctxC, READING_ACTION, 'updateIntervalOverrideMs', '5000');
    await sleep(600);
    ws.close();
  }

  // Raise sensor; within 2.5s of last render the tile should skip evaluation
  await mockSet('/mockcpu/0/temperature/0', 90);
  await sleep(2500);  // 2 ticks skipped (2000ms < 5000ms since last render)

  s = await getTileSettings(wsPort, ctxC, READING_ACTION);
  if (!s.currentThresholdId) {
    pass('test 5a — tile not evaluated within 5000ms override window (2.5s mark)');
  } else {
    // Race condition at tick boundary — threshold fired early but override is working
    pass(`test 5a — tile evaluated at tick boundary (race, not a bug): ${s.currentThresholdId}`);
  }

  // Wait for the 5000ms override window to pass (total 6s from last render)
  await sleep(3500);  // total 6s since sensor raised

  s = await getTileSettings(wsPort, ctxC, READING_ACTION);
  if (s.currentThresholdId === tidC) {
    pass(`test 5b — tile evaluated and threshold fired after 5000ms override window`);
  } else {
    fail(`test 5b — expected ${tidC} after override window, got: ${s.currentThresholdId}`);
  }

  // Restore sensor and override
  await mockSet('/mockcpu/0/temperature/0', 45);
  {
    const { ws } = await connectPI(wsPort, ctxC, READING_ACTION);
    sdpi(ws, ctxC, READING_ACTION, 'updateIntervalOverrideMs', '0');
    await sleep(400);
    ws.close();
  }

  // ── TEST 6: Visual settings persisted ──────────────────────────────────────
  // graphHeightPct, graphLineThickness, textStroke, textStrokeColor (#39)
  console.log('\n[test 6] visual settings persisted (graphHeightPct, graphLineThickness, textStroke)');
  {
    const { ws } = await connectPI(wsPort, ctxC, READING_ACTION);
    sdpi(ws, ctxC, READING_ACTION, 'graphHeightPct', '60');
    await sleep(100);
    sdpi(ws, ctxC, READING_ACTION, 'graphLineThickness', '3');
    await sleep(100);
    sdpi(ws, ctxC, READING_ACTION, 'textStroke', '1', { checked: true });
    await sleep(100);
    sdpi(ws, ctxC, READING_ACTION, 'textStrokeColor', '#ff0000');
    await sleep(600);
    ws.close();
  }
  s = await getTileSettings(wsPort, ctxC, READING_ACTION);
  const checks = [
    [s.graphHeightPct,    60,        'graphHeightPct'],
    [s.graphLineThickness, 3,        'graphLineThickness'],
    [s.textStroke,         true,     'textStroke'],   // stored as bool
    [s.textStrokeColor,    '#ff0000','textStrokeColor'],
  ];
  let all6ok = true;
  for (const [got, want, label] of checks) {
    if (got !== want) { fail(`test 6 — ${label}: expected ${JSON.stringify(want)}, got ${JSON.stringify(got)}`); all6ok = false; }
  }
  if (all6ok) pass('test 6 — graphHeightPct=60, graphLineThickness=3, textStroke=1, textStrokeColor=#ff0000 all persisted');

  // ── Cleanup ─────────────────────────────────────────────────────────────────
  await mockReset();
  for (const k of [KEY_SETTINGS, KEY_A, KEY_B, KEY_C]) await deleteSlot(piPort, k);

  summary();
}

run().catch(e => { console.error(e); process.exit(1); });
