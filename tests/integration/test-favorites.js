'use strict';
// Favorites tests: toggle (save), apply, remove.
//
// Key indices 6–7.

const {
  pass, fail, summary, sleep,
  waitForDeckBridge, connectPI, waitForMessage, sendToPlugin, sdpi,
  createSlot, deleteSlot,
  mockReset, readGlobalSettings,
} = require('./helpers');

const SETTINGS_ACTION = 'com.moeilijk.hwinfo.settings';
const READING_ACTION  = 'com.moeilijk.hwinfo.reading';
const KEY_SETTINGS = 8;  // settings tile for favorite removal
const KEY_TILE_A   = 6;  // tile we save the favorite from
const KEY_TILE_B   = 7;  // tile we apply the favorite to

// Wait for a sendToPropertyInspector message containing pl.catalog
// (favorites may be null when empty — Go marshals nil slice as null)
function waitForCatalog(ws, timeoutMs = 8000) {
  return waitForMessage(ws, msg => {
    if (msg.event !== 'sendToPropertyInspector') return undefined;
    const pl = msg.payload || {};
    if (pl.catalog) return pl;
  }, timeoutMs);
}

async function run() {
  console.log('── favorites tests ──');

  await mockReset();
  const { wsPort, piPort } = await waitForDeckBridge(20000);
  console.log(`DeckBridge: wsPort=${wsPort} piPort=${piPort}`);

  for (const k of [KEY_SETTINGS, KEY_TILE_A, KEY_TILE_B]) await deleteSlot(piPort, k);
  await sleep(300);

  const settingsCtx = await createSlot(piPort, KEY_SETTINGS, SETTINGS_ACTION);
  const ctxA = await createSlot(piPort, KEY_TILE_A, READING_ACTION);
  const ctxB = await createSlot(piPort, KEY_TILE_B, READING_ACTION);
  await sleep(800);

  await sleep(1500);

  // Remove any pre-existing favorites
  {
    const gs = readGlobalSettings();
    if ((gs.favoriteReadings || []).length > 0) {
      const { ws } = await connectPI(wsPort, settingsCtx, SETTINGS_ACTION);
      for (const fav of gs.favoriteReadings) {
        sendToPlugin(ws, settingsCtx, SETTINGS_ACTION, { sdpi_collection: { key: 'removeFavorite', value: fav.id } });
        await sleep(150);
      }
      await sleep(300);
      ws.close();
    }
  }

  // Configure tile A: CPU Package temp
  const { ws: wsA, payload: initA } = await connectPI(wsPort, ctxA, READING_ACTION);
  const sensor = (initA.sensors || []).find(s => s.uid === '/mockcpu/0');
  if (!sensor) { wsA.close(); throw new Error('sensor /mockcpu/0 not found'); }

  const readingsP = waitForMessage(wsA, msg => {
    if (msg.event !== 'sendToPropertyInspector') return undefined;
    const pl = msg.payload || {};
    if (pl.readings && Array.isArray(pl.readings)) return pl.readings;
  });
  sdpi(wsA, ctxA, READING_ACTION, 'sensorSelect', '/mockcpu/0');
  const readings = await readingsP;
  const reading = readings.find(r => r.label === 'CPU Package');
  if (!reading) { wsA.close(); throw new Error('CPU Package reading not found'); }
  sdpi(wsA, ctxA, READING_ACTION, 'readingSelect', String(reading.id));
  await sleep(800);

  console.log('setup complete — tile A configured with CPU Package');

  // ── TEST 1: Toggle favorite (save) ──────────────────────────────────────────
  console.log('\n[test 1] toggle favorite — save');

  const catalogAfterToggleP = waitForCatalog(wsA, 8000);
  sdpi(wsA, ctxA, READING_ACTION, 'toggleFavoriteCurrent', '');
  const catalogAfterToggle = await catalogAfterToggleP;
  const favorites = catalogAfterToggle.catalog.favorites;

  if (favorites.length === 1) {
    pass('test 1a — 1 favorite after toggle');
  } else {
    fail(`test 1a — expected 1 favorite, got ${favorites.length}`);
  }

  const fav = favorites[0];
  const expectedId = `/mockcpu/0|${reading.id}`;
  if (fav && fav.id === expectedId) {
    pass(`test 1b — favorite id matches: ${fav.id}`);
  } else {
    fail(`test 1b — expected id ${expectedId}, got: ${fav && fav.id}`);
  }
  if (fav && fav.readingLabel === 'CPU Package') {
    pass('test 1c — favorite readingLabel = CPU Package');
  } else {
    fail(`test 1c — readingLabel: ${fav && fav.readingLabel}`);
  }

  wsA.close();

  // ── TEST 2: Apply favorite to tile B ─────────────────────────────────────────
  console.log('\n[test 2] apply favorite to tile B');

  const { ws: wsB } = await connectPI(wsPort, ctxB, READING_ACTION);

  // Wait for the catalog message (sent after initial sensors message)
  const initialCatalogP = waitForCatalog(wsB, 8000);
  const initialCatalog = await initialCatalogP;

  if ((initialCatalog.catalog.favorites || []).length === 1) {
    pass('test 2a — favorite visible in tile B catalog');
  } else {
    fail(`test 2a — expected 1 favorite in tile B, got ${(initialCatalog.catalog.favorites || []).length}`);
  }

  // applyFavorite calls sendReadingsToPropertyInspector → sends { readings: [...], settings: { ... } }
  const applyResultP = waitForMessage(wsB, msg => {
    if (msg.event !== 'sendToPropertyInspector') return undefined;
    const pl = msg.payload || {};
    if (pl.readings && pl.settings) return pl;
  }, 8000);
  sdpi(wsB, ctxB, READING_ACTION, 'applyFavorite', fav.id);
  const applyResult = await applyResultP;

  if (applyResult.settings.sensorUid === '/mockcpu/0') {
    pass('test 2b — sensorUid applied correctly to tile B');
  } else {
    fail(`test 2b — expected /mockcpu/0, got: ${applyResult.settings.sensorUid}`);
  }

  wsB.close();

  // ── TEST 3: Remove favorite ──────────────────────────────────────────────────
  console.log('\n[test 3] remove favorite');

  const { ws: wsA2 } = await connectPI(wsPort, ctxA, READING_ACTION);
  const catalogBeforeRemoveP = waitForCatalog(wsA2, 8000);
  await catalogBeforeRemoveP;  // consume initial catalog

  const catalogAfterRemoveP = waitForCatalog(wsA2, 8000);
  sdpi(wsA2, ctxA, READING_ACTION, 'removeFavorite', fav.id);
  const catalogAfterRemove = await catalogAfterRemoveP;

  if ((catalogAfterRemove.catalog.favorites || []).length === 0) {
    pass('test 3 — favorites list empty after remove');
  } else {
    fail(`test 3 — expected 0 favorites, got ${(catalogAfterRemove.catalog.favorites || []).length}`);
  }

  wsA2.close();

  // ── Cleanup ─────────────────────────────────────────────────────────────────
  await mockReset();
  for (const k of [KEY_SETTINGS, KEY_TILE_A, KEY_TILE_B]) await deleteSlot(piPort, k);

  summary();
}

run().catch(e => { console.error(e); process.exit(1); });
