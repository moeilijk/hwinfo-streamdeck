'use strict';
// Global settings stay complete. Stream Deck replaces the stored global
// settings on every setGlobalSettings, so the plugin is the only writer and
// always writes the whole object:
//   1. Saving a Derived preset keeps the favorites, the global thresholds and
//      the refresh rate.
//   2. A later plugin write (refresh rate) keeps the Derived presets.
//
// Key indices 4 and 19 (not used by the other suites).

const {
  pass, fail, summary, sleep,
  waitForDeckBridge, connectPI, sendToPlugin,
  createSlot, deleteSlot,
  mockReset, readGlobalSettings,
} = require('./helpers');

const SETTINGS_ACTION = 'com.moeilijk.hwinfo.settings';
const DERIVED_ACTION  = 'com.moeilijk.hwinfo.derived';
const KEY_SETTINGS = 4;
const KEY_DERIVED  = 19;
const PRESET_NAME  = 'it-global-settings-complete';
const THRESHOLD_NAME = 'it-global-settings-complete';
const SETTLE_MS = 600;

function same(a, b) { return JSON.stringify(a || null) === JSON.stringify(b || null); }

async function waitForGlobal(predicate, timeoutMs = 5000) {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const gs = readGlobalSettings();
    if (predicate(gs)) return gs;
    if (Date.now() > deadline) return gs;
    await sleep(100);
  }
}

async function run() {
  console.log('── global settings complete tests ──');

  await mockReset();
  const { wsPort, piPort } = await waitForDeckBridge(20000);
  console.log(`DeckBridge: wsPort=${wsPort} piPort=${piPort}`);

  for (const k of [KEY_SETTINGS, KEY_DERIVED]) await deleteSlot(piPort, k);
  await sleep(300);
  const settingsCtx = await createSlot(piPort, KEY_SETTINGS, SETTINGS_ACTION);
  const derivedCtx = await createSlot(piPort, KEY_DERIVED, DERIVED_ACTION);
  await sleep(1800);

  const { ws: wsD } = await connectPI(wsPort, derivedCtx, DERIVED_ACTION);
  const { ws: wsS } = await connectPI(wsPort, settingsCtx, SETTINGS_ACTION);

  // DeckBridge echoes every setGlobalSettings back to the plugin and writes its
  // file asynchronously (the Stream Deck app sends no such echo), so writes
  // that follow each other quickly can land out of order. Every step below
  // waits for its own result and a short settle before the next one.

  // Remove thresholds an aborted earlier run of this test left behind: an
  // enabled ">= 0" threshold fires on every tile and breaks later suites.
  for (const stale of (readGlobalSettings().globalThresholds || []).filter(g => g.name === THRESHOLD_NAME)) {
    sendToPlugin(wsS, settingsCtx, SETTINGS_ACTION, { deleteGlobalThreshold: stale.id });
    await waitForGlobal(gs => !(gs.globalThresholds || []).some(g => g.id === stale.id));
    await sleep(SETTLE_MS);
  }

  // A global threshold of our own, so "unchanged" is checked on real content.
  const thresholdsBefore = (readGlobalSettings().globalThresholds || []).map(g => g.id);
  sendToPlugin(wsS, settingsCtx, SETTINGS_ACTION, { addGlobalThreshold: THRESHOLD_NAME });
  const withThreshold = await waitForGlobal(gs => (gs.globalThresholds || []).length > thresholdsBefore.length);
  const ownThreshold = (withThreshold.globalThresholds || []).find(g => !thresholdsBefore.includes(g.id));
  await sleep(SETTLE_MS);

  const before = readGlobalSettings();
  const originalPresets = (before.derivedPresets || []).filter(p => p.name !== PRESET_NAME);
  const originalInterval = before.pollInterval;
  if (!ownThreshold || typeof originalInterval !== 'number') {
    fail(`setup: expected our global threshold and a refresh rate, got ${JSON.stringify(before)}`);
    summary();
    return;
  }
  console.log(`setup: ${(before.favoriteReadings || []).length} favorites, ${(before.globalThresholds || []).length} global thresholds, ${originalPresets.length} presets, refresh rate ${originalInterval} ms`);

  // ── TEST 1: saving a preset keeps the rest ─────────────────────────────────
  console.log('\n[test 1] save a Derived preset');
  const newPreset = { name: PRESET_NAME, formula: 'sum', slotCount: 2, slots: [] };
  sendToPlugin(wsD, derivedCtx, DERIVED_ACTION, { saveDerivedPresets: originalPresets.concat([newPreset]) });
  await waitForGlobal(gs => (gs.derivedPresets || []).some(p => p.name === PRESET_NAME));
  await sleep(SETTLE_MS);
  const afterSave = readGlobalSettings();

  if ((afterSave.derivedPresets || []).some(p => p.name === PRESET_NAME)) pass('preset is stored');
  else fail(`preset not stored: ${JSON.stringify(afterSave.derivedPresets)}`);
  if (same(afterSave.favoriteReadings, before.favoriteReadings)) pass('favorites unchanged');
  else fail(`favorites changed: ${JSON.stringify(afterSave.favoriteReadings)}`);
  if (same(afterSave.globalThresholds, before.globalThresholds)) pass('global thresholds unchanged');
  else fail(`global thresholds changed: ${JSON.stringify(afterSave.globalThresholds)}`);
  if (afterSave.pollInterval === originalInterval) pass('refresh rate unchanged');
  else fail(`refresh rate changed: ${afterSave.pollInterval}`);

  // ── TEST 2: a plugin write keeps the presets ───────────────────────────────
  console.log('\n[test 2] change the refresh rate');
  const otherInterval = originalInterval === 2000 ? 5000 : 2000;
  sendToPlugin(wsS, settingsCtx, SETTINGS_ACTION, { setPollInterval: otherInterval });
  await waitForGlobal(gs => gs.pollInterval === otherInterval);
  await sleep(SETTLE_MS);
  const afterRate = readGlobalSettings();

  if (afterRate.pollInterval === otherInterval) pass(`refresh rate stored (${otherInterval} ms)`);
  else fail(`refresh rate not stored: ${afterRate.pollInterval}`);
  if ((afterRate.derivedPresets || []).some(p => p.name === PRESET_NAME)) pass('presets kept after a plugin write');
  else fail(`presets lost after a plugin write: ${JSON.stringify(afterRate.derivedPresets)}`);
  if (same(afterRate.favoriteReadings, before.favoriteReadings)) pass('favorites kept after a plugin write');
  else fail(`favorites changed: ${JSON.stringify(afterRate.favoriteReadings)}`);
  if (same(afterRate.globalThresholds, before.globalThresholds)) pass('global thresholds kept after a plugin write');
  else fail(`global thresholds changed: ${JSON.stringify(afterRate.globalThresholds)}`);

  // ── cleanup: put the presets, the refresh rate and the thresholds back ─────
  sendToPlugin(wsD, derivedCtx, DERIVED_ACTION, { saveDerivedPresets: originalPresets });
  await waitForGlobal(gs => !(gs.derivedPresets || []).some(p => p.name === PRESET_NAME));
  await sleep(SETTLE_MS);
  sendToPlugin(wsS, settingsCtx, SETTINGS_ACTION, { setPollInterval: originalInterval });
  await waitForGlobal(gs => gs.pollInterval === originalInterval);
  await sleep(SETTLE_MS);
  sendToPlugin(wsS, settingsCtx, SETTINGS_ACTION, { deleteGlobalThreshold: ownThreshold.id });
  await waitForGlobal(gs => !(gs.globalThresholds || []).some(g => g.id === ownThreshold.id));
  await sleep(SETTLE_MS);

  const restored = readGlobalSettings();
  if (restored.pollInterval === originalInterval
    && !(restored.derivedPresets || []).some(p => p.name === PRESET_NAME)
    && !(restored.globalThresholds || []).some(g => g.name === THRESHOLD_NAME)) {
    pass('cleanup: presets, refresh rate and thresholds back as before');
  } else {
    fail(`cleanup left changes behind: ${JSON.stringify(restored)}`);
  }
  wsD.close();
  wsS.close();
  for (const k of [KEY_SETTINGS, KEY_DERIVED]) await deleteSlot(piPort, k);

  summary();
}

run().catch(err => { console.error('[FATAL]', err); process.exit(1); });
