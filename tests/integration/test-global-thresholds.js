'use strict';
// Global threshold integration tests (Issue #41)
// Tests auto-apply by ReadingType, type-filter, and per-tile suppress/unsuppress.
//
// Prerequisites (all started by run-integration-tests.sh):
//   - DeckBridge running (ports from /tmp/deckbridge.log)
//   - plugin deployed under DeckBridge; mock-bridge serves sensors + control API on :9999

const {
  pass, fail, summary, sleep,
  waitForDeckBridge, connectPI, waitForMessage, sendToPlugin, sdpi,
  createSlot, deleteSlot,
  mockSet, mockReset,
  getTileSettings, readGlobalSettings,
} = require('./helpers');

const SETTINGS_ACTION  = 'com.exension.hwinfo.settings';
const READING_ACTION   = 'com.exension.hwinfo.reading';
const POLL_MS          = 1000;   // default plugin poll interval
const DWELL_MS         = 1000;   // default threshold dwell
const COOLDOWN_MS      = 5000;   // default threshold cooldown

// key indices used by this test — must not overlap with real tiles
const KEY_SETTINGS = 0;
const KEY_TEMP     = 1;
const KEY_LOAD     = 2;
const KEY_SUPPRESS = 3;

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// Wait long enough for the plugin to poll + satisfy dwell
function waitForEval(extra = 0) {
  return sleep(POLL_MS * 3 + extra);
}

// Connect as PI to a reading tile and get its initial payload (sensors + settings)
async function connectReadingPI(wsPort, context) {
  const { ws, payload } = await connectPI(wsPort, context, READING_ACTION);
  return { ws, sensors: payload.sensors || [], settings: payload.settings || {} };
}

// Wait for a readings payload (sent after sensorSelect)
function waitForReadings(ws, timeoutMs = 8000) {
  return waitForMessage(ws, msg => {
    if (msg.event !== 'sendToPropertyInspector') return undefined;
    const pl = msg.payload || {};
    if (pl.readings && Array.isArray(pl.readings)) return pl.readings;
  }, timeoutMs);
}

// Configure a reading tile to use a specific sensor+reading.
// Returns { readingId, readingType } of the selected reading.
async function configureTile(wsPort, context, sensorUID, readingLabel) {
  const { ws, sensors } = await connectReadingPI(wsPort, context);

  const sensor = sensors.find(s => s.uid === sensorUID);
  if (!sensor) {
    ws.close();
    throw new Error(`sensor ${sensorUID} not found in PI. Available: ${sensors.map(s=>s.uid).join(', ')}`);
  }

  // Select the sensor
  const readingsP = waitForReadings(ws);
  sdpi(ws, context, READING_ACTION, 'sensorSelect', sensorUID);
  const readings = await readingsP;

  const reading = readings.find(r => r.label === readingLabel);
  if (!reading) {
    ws.close();
    throw new Error(`reading "${readingLabel}" not found on ${sensorUID}. Available: ${readings.map(r=>r.label).join(', ')}`);
  }

  // Select the reading (no response message — fire and forget, settings saved async)
  sdpi(ws, context, READING_ACTION, 'readingSelect', String(reading.id));
  await sleep(800);
  ws.close();

  return { readingId: reading.id, readingType: reading.type };
}

// ─────────────────────────────────────────────────────────────────────────────
// Global threshold management via settings tile PI
// ─────────────────────────────────────────────────────────────────────────────

// Add a global threshold and return its ID.
// settingsWs must already be an open PI connection to the settings tile.
// After adding, reads the new ID from disk (global settings JSON).
async function addGlobalThreshold(settingsWs, settingsCtx, { name, readingType, operator, value, enabled }) {
  const before = (readGlobalSettings().globalThresholds || []).map(t => t.id);

  sendToPlugin(settingsWs, settingsCtx, SETTINGS_ACTION, { addGlobalThreshold: name || 'Test' });
  await sleep(600); // let plugin persist to disk

  const after = readGlobalSettings().globalThresholds || [];
  const newGlobal = after.find(t => !before.includes(t.id));
  if (!newGlobal) throw new Error('addGlobalThreshold: new threshold not found in global settings');

  const id = newGlobal.id;

  // Update fields
  const updates = [
    { field: 'thresholdReadingType', value: readingType || '' },
    { field: 'thresholdOperator', value: operator || '>=' },
    { field: 'thresholdValue', value: String(value ?? 0) },
  ];
  if (enabled !== undefined) {
    updates.push({ field: 'thresholdEnabled', value: String(enabled), checked: enabled });
  }

  for (const upd of updates) {
    sendToPlugin(settingsWs, settingsCtx, SETTINGS_ACTION, {
      updateGlobalThreshold: { id, field: upd.field, value: upd.value, checked: !!upd.checked }
    });
    await sleep(150);
  }
  await sleep(400);
  return id;
}

// ─────────────────────────────────────────────────────────────────────────────
// Suppress/unsuppress a global on a tile
// ─────────────────────────────────────────────────────────────────────────────
async function suppressGlobal(wsPort, tileContext, tileAction, globalId) {
  const { ws } = await connectPI(wsPort, tileContext, tileAction);
  sendToPlugin(ws, tileContext, tileAction, { sdpi_collection: { key: 'suppressGlobal', value: globalId } });
  await sleep(500);
  ws.close();
}

async function unsuppressGlobal(wsPort, tileContext, tileAction, globalId) {
  const { ws } = await connectPI(wsPort, tileContext, tileAction);
  sendToPlugin(ws, tileContext, tileAction, { sdpi_collection: { key: 'unsuppressGlobal', value: globalId } });
  await sleep(500);
  ws.close();
}

// ─────────────────────────────────────────────────────────────────────────────
// Main test runner
// ─────────────────────────────────────────────────────────────────────────────
async function run() {
  console.log('── global threshold integration tests ──');

  // ── Setup ──────────────────────────────────────────────────────────────────
  await mockReset();
  const { wsPort, piPort } = await waitForDeckBridge(20000);
  console.log(`DeckBridge: wsPort=${wsPort} piPort=${piPort}`);

  // Clean up any tiles left over from prior runs
  for (const k of [KEY_SETTINGS, KEY_TEMP, KEY_LOAD, KEY_SUPPRESS]) {
    await deleteSlot(piPort, k);
  }
  await sleep(500);

  // Create tiles
  const settingsCtx = await createSlot(piPort, KEY_SETTINGS, SETTINGS_ACTION);
  const tempCtx     = await createSlot(piPort, KEY_TEMP,     READING_ACTION);
  const loadCtx     = await createSlot(piPort, KEY_LOAD,     READING_ACTION);
  const suppressCtx = await createSlot(piPort, KEY_SUPPRESS, READING_ACTION);
  await sleep(1000); // let plugin process WillAppear events

  await sleep(1500); // let plugin reconnect to mock source

  // Configure tiles with specific sensors/readings
  await configureTile(wsPort, tempCtx,     '/mockcpu/0', 'CPU Package');
  await configureTile(wsPort, loadCtx,     '/mockcpu/0', 'CPU Total');
  await configureTile(wsPort, suppressCtx, '/mockcpu/0', 'CPU Package');
  await sleep(1000);

  console.log('setup complete — tiles configured');

  // ── Open settings PI for global threshold management ───────────────────────
  const { ws: settingsWs, payload: settingsPayload } = await connectPI(wsPort, settingsCtx, SETTINGS_ACTION);
  // Store wsPort on ws object so clearAllGlobalThresholds can reconnect if needed
  settingsWs._wsPort = wsPort;

  // Clean up any leftover global thresholds from previous test runs (read from disk)
  {
    const gs = readGlobalSettings();
    const existingGlobals = gs.globalThresholds || [];
    for (const g of existingGlobals) {
      sendToPlugin(settingsWs, settingsCtx, SETTINGS_ACTION, { deleteGlobalThreshold: g.id });
      await sleep(200);
    }
    if (existingGlobals.length > 0) {
      console.log(`cleaned up ${existingGlobals.length} leftover global threshold(s)`);
      await sleep(500);
    }
  }

  // ── TEST 1: Auto-apply — temperature global fires on temperature tile ───────
  console.log('\n[test 1] auto-apply: Temp global fires on Temp tile');

  const gTempId = await addGlobalThreshold(settingsWs, settingsCtx, {
    name: 'HighTemp',
    readingType: 'Temp',
    operator: '>',
    value: 80,
    enabled: true,
  });

  // Baseline: sensor at 45°C → threshold should NOT be active
  await waitForEval();
  let settings = await getTileSettings(wsPort, tempCtx, READING_ACTION);
  if (settings.currentThresholdId === '') {
    pass('test 1a — baseline: no active threshold at 45°C');
  } else {
    fail(`test 1a — expected no active threshold at 45°C, got: ${settings.currentThresholdId}`);
  }

  // Trigger: raise sensor above threshold
  await mockSet('/mockcpu/0/temperature/0', 90);
  await waitForEval(DWELL_MS);  // dwell is 1 poll cycle; wait 4 cycles total

  settings = await getTileSettings(wsPort, tempCtx, READING_ACTION);
  if (settings.currentThresholdId === gTempId) {
    pass(`test 1b — global threshold ${gTempId} active at 90°C`);
  } else {
    fail(`test 1b — expected currentThresholdId=${gTempId}, got: ${settings.currentThresholdId}`);
  }

  // Return sensor to default
  await mockSet('/mockcpu/0/temperature/0', 45);

  // ── TEST 2: Type filter — Temp global does NOT fire on Load tile ────────────
  console.log('\n[test 2] type filter: Temp global does NOT fire on Load tile');

  // Set load to 90% (above any percentage threshold — but our global is Temp type)
  await mockSet('/mockcpu/0/load/0', 90);
  await waitForEval(DWELL_MS);

  settings = await getTileSettings(wsPort, loadCtx, READING_ACTION);
  if (!settings.currentThresholdId) {
    pass('test 2 — Temp global correctly ignored on Load tile at 90%');
  } else {
    fail(`test 2 — Load tile unexpectedly has active threshold: ${settings.currentThresholdId}`);
  }

  // Restore load
  await mockSet('/mockcpu/0/load/0', 20);

  // ── Wait for cooldown on temp tile before continuing ────────────────────────
  // temp tile was active in test 1; cooldown is 5s
  console.log('waiting for threshold cooldown...');
  await sleep(COOLDOWN_MS + POLL_MS * 2);

  // ── TEST 3: Suppress — suppressed global does not fire on tile ─────────────
  console.log('\n[test 3] suppress: suppressed global does not fire');

  // Suppress gTempId on suppressCtx
  await suppressGlobal(wsPort, suppressCtx, READING_ACTION, gTempId);

  // Raise temperature
  await mockSet('/mockcpu/0/temperature/0', 90);
  await waitForEval(DWELL_MS);

  settings = await getTileSettings(wsPort, suppressCtx, READING_ACTION);
  if (!settings.currentThresholdId) {
    pass('test 3a — suppressed global does not fire');
  } else {
    fail(`test 3a — suppressed global unexpectedly fired: ${settings.currentThresholdId}`);
  }

  // Confirm the non-suppressed tile (tempCtx) DOES fire (cooldown has expired)
  settings = await getTileSettings(wsPort, tempCtx, READING_ACTION);
  if (settings.currentThresholdId === gTempId) {
    pass('test 3b — unsuppressed tile correctly fires during same period');
  } else {
    fail(`test 3b — expected tempCtx to fire, got: ${settings.currentThresholdId}`);
  }

  // ── TEST 4: Unsuppress — global fires again after unsuppress ───────────────
  console.log('\n[test 4] unsuppress: global fires after unsuppress');

  // Unsuppress on suppressCtx (sensor still at 90°C)
  await unsuppressGlobal(wsPort, suppressCtx, READING_ACTION, gTempId);
  await waitForEval(DWELL_MS);  // wait for dwell after unsuppress

  settings = await getTileSettings(wsPort, suppressCtx, READING_ACTION);
  if (settings.currentThresholdId === gTempId) {
    pass(`test 4 — global fires on tile after unsuppress`);
  } else {
    fail(`test 4 — expected ${gTempId} to fire after unsuppress, got: ${settings.currentThresholdId}`);
  }

  // ── TEST 5: PI visibility — global appears in PI for matching type ──────────
  // Regression: PI was showing "None match" because it compared LHM type string
  // ("Temperature") against the stored ReadingType short form ("Temp").
  console.log('\n[test 5] PI visibility: global with readingType="Temp" visible on Temp tile');

  {
    // Connect as PI to tempCtx and collect both the sensors payload and the
    // globalThresholds broadcast that follows. We simulate the PI filter logic
    // to verify the type mapping would resolve correctly.
    const { ws: piWs } = await connectPI(wsPort, tempCtx, READING_ACTION);

    // sensorSelect → triggers readings payload with type field
    const readingsP = waitForMessage(piWs, msg => {
      if (msg.event !== 'sendToPropertyInspector') return undefined;
      const pl = msg.payload || {};
      if (pl.readings && Array.isArray(pl.readings)) return pl;
    });
    sdpi(piWs, tempCtx, READING_ACTION, 'sensorSelect', '/mockcpu/0');
    const readingsPl = await readingsP;
    const readings = readingsPl.readings;
    const tempReading = readings.find(r => r.label === 'CPU Package');

    if (!tempReading) {
      fail('test 5a — CPU Package reading not in payload');
    } else {
      pass(`test 5a — CPU Package reading received, raw type="${tempReading.type}"`);

      // Simulate PI-side normalizeReadingType mapping (same code as pi_utils.js)
      function normalizeReadingType(t) {
        switch ((t || '').toLowerCase()) {
          case 'temp': case 'temperature': return 'Temp';
          case 'volt': case 'voltage':     return 'Volt';
          case 'fan':                      return 'Fan';
          case 'current':                  return 'Current';
          case 'power':                    return 'Power';
          case 'clock':                    return 'Clock';
          case 'usage': case 'load': case 'control': case 'level': return 'Usage';
          case 'none':                     return 'None';
          default:                         return t ? 'Other' : '';
        }
      }
      const mappedType = normalizeReadingType(tempReading.type);

      // Verify global threshold with readingType="Temp" matches after mapping
      const globalGs = readGlobalSettings().globalThresholds || [];
      const tempGlobal = globalGs.find(g => g.id === gTempId);
      if (!tempGlobal) {
        fail('test 5b — global threshold not found in settings');
      } else if (mappedType === tempGlobal.readingType) {
        pass(`test 5b — mapped type "${mappedType}" matches global readingType "${tempGlobal.readingType}" — no "None match"`);
      } else {
        fail(`test 5b — type mismatch: mapped="${mappedType}" vs global="${tempGlobal.readingType}"`);
      }

      // Also verify a Load reading maps differently, confirming type filter blocks it
      const loadReading = readings.find(r => r.label === 'CPU Total');
      if (loadReading) {
        const loadMapped = normalizeReadingType(loadReading.type);
        if (loadMapped !== tempGlobal.readingType) {
          pass(`test 5c — Load reading maps to "${loadMapped}", correctly excluded from Temp global`);
        } else {
          fail(`test 5c — Load reading unexpectedly matched Temp global after mapping`);
        }
      }

      // 5d — both canonical HWiNFO types and legacy long forms must normalize
      const voltageTypes = [
        { raw: 'Volt',    expected: 'Volt' },
        { raw: 'Voltage', expected: 'Volt' },
        { raw: 'Temp',    expected: 'Temp' },
        { raw: 'Fan',     expected: 'Fan' },
        { raw: 'Usage',   expected: 'Usage' },
        { raw: 'Load',    expected: 'Usage' },
        { raw: 'Control', expected: 'Usage' },
        { raw: 'Clock',   expected: 'Clock' },
      ];
      let type5dOk = true;
      for (const { raw, expected } of voltageTypes) {
        const got = normalizeReadingType(raw);
        if (got !== expected) {
          fail(`test 5d — normalizeReadingType("${raw}") = "${got}", expected "${expected}"`);
          type5dOk = false;
        }
      }
      if (type5dOk) {
        pass('test 5d — all type strings normalize correctly (Voltage→Volt, Load→Usage, etc.)');
      }
    }
    piWs.close();
  }

  // ── Cleanup ────────────────────────────────────────────────────────────────
  await mockReset();
  // Remove global threshold
  sendToPlugin(settingsWs, settingsCtx, SETTINGS_ACTION, { deleteGlobalThreshold: gTempId });
  await sleep(300);
  settingsWs.close();

  for (const k of [KEY_SETTINGS, KEY_TEMP, KEY_LOAD, KEY_SUPPRESS]) {
    await deleteSlot(piPort, k);
  }

  summary();
}

run().catch(e => { fail(e.stack || e.message); process.exit(1); });
