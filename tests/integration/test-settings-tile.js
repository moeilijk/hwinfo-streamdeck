'use strict';
// Settings tile tests: connectionStatus, setPollInterval.
//
// Key index 5.

const {
  pass, fail, summary, sleep,
  waitForDeckBridge, connectPI, waitForMessage, sendToPlugin,
  createSlot, deleteSlot,
  mockReset, readGlobalSettings,
} = require('./helpers');

const SETTINGS_ACTION = 'com.moeilijk.hwinfo.settings';
const KEY_SETTINGS = 5;

async function run() {
  console.log('── settings tile tests ──');

  await mockReset();
  const { wsPort, piPort } = await waitForDeckBridge(20000);
  console.log(`DeckBridge: wsPort=${wsPort} piPort=${piPort}`);

  await deleteSlot(piPort, KEY_SETTINGS);
  await sleep(300);

  const settingsCtx = await createSlot(piPort, KEY_SETTINGS, SETTINGS_ACTION);
  await sleep(1000);

  await sleep(1500);

  // ── TEST 1: connectionStatus = Connected ────────────────────────────────────
  console.log('\n[test 1] connectionStatus = Connected with mock running');
  {
    const { ws: ws1, payload } = await connectPI(wsPort, settingsCtx, SETTINGS_ACTION);
    if (payload.connectionStatus === 'Connected') {
      pass('test 1a — connectionStatus = Connected');
    } else {
      fail(`test 1a — expected Connected, got: ${payload.connectionStatus}`);
    }
    if (typeof payload.currentRate === 'number' && payload.currentRate > 0) {
      pass('test 1b — currentRate is a positive number');
    } else {
      fail(`test 1b — currentRate invalid: ${payload.currentRate}`);
    }
    if (payload.sourceProfiles === undefined) {
      pass('test 1c — no sourceProfiles in status payload (HWiNFO-only plugin)');
    } else {
      fail(`test 1c — unexpected sourceProfiles in payload: ${JSON.stringify(payload.sourceProfiles)}`);
    }
    ws1.close();
  }

  // ── TEST 2: setPollInterval changes currentRate ──────────────────────────────
  console.log('\n[test 2] setPollInterval changes currentRate');

  const originalRate = readGlobalSettings().pollInterval || 1000;
  const { ws } = await connectPI(wsPort, settingsCtx, SETTINGS_ACTION);

  // Send setPollInterval and wait for the requestSettingsStatus response
  const statusP = waitForMessage(ws, msg => {
    if (msg.event !== 'sendToPropertyInspector') return undefined;
    const pl = msg.payload || {};
    if (typeof pl.currentRate === 'number') return pl;
  });

  sendToPlugin(ws, settingsCtx, SETTINGS_ACTION, { setPollInterval: 2000 });
  sendToPlugin(ws, settingsCtx, SETTINGS_ACTION, { requestSettingsStatus: true });
  const status = await statusP;

  if (status.currentRate === 2000) {
    pass('test 2a — currentRate = 2000 after setPollInterval(2000)');
  } else {
    fail(`test 2a — expected 2000, got: ${status.currentRate}`);
  }

  // Verify persisted to global settings file
  await sleep(300);
  const gs = readGlobalSettings();
  if (gs.pollInterval === 2000) {
    pass('test 2b — pollInterval=2000 persisted to global settings');
  } else {
    fail(`test 2b — global settings pollInterval: ${gs.pollInterval}`);
  }

  // Reset poll interval to original
  sendToPlugin(ws, settingsCtx, SETTINGS_ACTION, { setPollInterval: originalRate });
  await sleep(500);
  ws.close();

  // ── Cleanup ─────────────────────────────────────────────────────────────────
  await mockReset();
  await deleteSlot(piPort, KEY_SETTINGS);

  summary();
}

run().catch(e => { console.error(e); process.exit(1); });
