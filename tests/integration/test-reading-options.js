'use strict';
// Reading tile options taken over from lhm-streamdeck (#120 and #104 there).
// Covers: "Reset on press" stays unavailable because HWiNFO cannot reset a
// sensor's MIN/MAX from outside, the Range modes Manual / Auto / Sensor Min /
// Max, the graph styles Classic / Soft / Soft spectrum, the unit as its own
// element, and the condensed font and text backdrop.
//
// Key indices 22–23 (not used by the other suites).

const http = require('http');
const path = require('path');
const {
  pass, fail, summary, sleep,
  waitForDeckBridge, connectPI, waitForMessage, sdpi,
  httpPost, createSlot, deleteSlot,
  mockSet, mockReset,
  getTileSettings,
  MOCK_PORT,
} = require('./helpers');

const sharp = require(path.resolve(__dirname, '../../../DeckBridge/node_modules/sharp'));

const READING_ACTION  = 'com.moeilijk.hwinfo.reading';
const DEVICE          = 'deckbridge-xl-0';
const POLL_MS         = 1000;

const KEY_TILE     = 22;
const KEY_RANGE    = 23;  // tile for the Range modes, reads "CPU Total"

const RESET_PATH = '/mockcpu/0/temperature/0'; // "CPU Package", the tile's reading
const OTHER_PATH = '/mockcpu/0/load/0';        // "CPU Total", must stay untouched

// Min/Value/Max of every local mock reading, straight from the mock-bridge.
function mockReadings() {
  return new Promise((resolve, reject) => {
    http.get({ host: '127.0.0.1', port: MOCK_PORT, path: '/list' }, res => {
      let out = '';
      res.on('data', c => out += c);
      res.on('end', () => {
        const found = {};
        JSON.parse(out).forEach(r => { found[r.path] = { min: r.min, value: r.current, max: r.max }; });
        resolve(found);
      });
    }).on('error', reject);
  });
}

// Set the MIN/MAX the mock-bridge reports for a reading.
function mockMinMax(readingPath, min, max) {
  return new Promise((resolve, reject) => {
    const data = JSON.stringify({ path: readingPath, min, max });
    const req = http.request({ host: '127.0.0.1', port: MOCK_PORT, path: '/minmax', method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(data) }
    }, res => { res.resume(); res.on('end', () => res.statusCode === 200 ? resolve() : reject(new Error('mock /minmax ' + res.statusCode))); });
    req.on('error', reject);
    req.write(data);
    req.end();
  });
}

async function configureTile(wsPort, ctx, sensorUID, readingLabel) {
  const { ws, payload } = await connectPI(wsPort, ctx, READING_ACTION);
  const sensor = (payload.sensors || []).find(s => s.uid === sensorUID);
  if (!sensor) { ws.close(); throw new Error(`sensor ${sensorUID} not found`); }
  const readingsP = waitForMessage(ws, msg => {
    if (msg.event !== 'sendToPropertyInspector') return undefined;
    const pl = msg.payload || {};
    if (pl.readings && Array.isArray(pl.readings)) return pl.readings;
  });
  sdpi(ws, ctx, READING_ACTION, 'sensorSelect', sensorUID);
  const r = (await readingsP).find(x => x.label === readingLabel);
  if (!r) { ws.close(); throw new Error(`reading "${readingLabel}" not found`); }
  sdpi(ws, ctx, READING_ACTION, 'readingSelect', String(r.id));
  await sleep(800);
  ws.close();
}

async function setOption(wsPort, ctx, key, checked) {
  const { ws } = await connectPI(wsPort, ctx, READING_ACTION);
  sdpi(ws, ctx, READING_ACTION, key, '', { checked });
  await sleep(600);
  ws.close();
}

function pressKey(piPort, keyIndex) {
  return httpPost(piPort, '/api/keys/press', { deviceId: DEVICE, keyIndex });
}

// Distinct heights of the graph in the rendered key image. The default graph is
// green on black, so the top-most green pixel of a column is the level drawn
// there. A graph with history shows several levels. A graph that was cleared is
// drawn flat at the current value, so it shows exactly one.
async function graphLevels(piPort, keyIndex) {
  const state = await new Promise((resolve, reject) => {
    http.get({ host: '127.0.0.1', port: piPort, path: '/api/state' }, res => {
      let out = '';
      res.on('data', c => out += c);
      res.on('end', () => { try { resolve(JSON.parse(out)); } catch (e) { reject(e); } });
    }).on('error', reject);
  });
  const slot = (state.slots || []).find(x => x.deviceId === DEVICE && x.keyIndex === keyIndex);
  if (!slot || !slot.imageDataUrl) throw new Error('no key image for index ' + keyIndex);
  const buf = Buffer.from(slot.imageDataUrl.split(',')[1], 'base64');
  const { data, info } = await sharp(buf).ensureAlpha().raw().toBuffer({ resolveWithObject: true });
  const green = (x, y) => {
    const o = (y * info.width + x) * 4;
    return data[o + 1] > data[o] + 30 && data[o + 1] > data[o + 2] + 30;
  };
  const tops = [];
  for (let x = 0; x < info.width; x++) {
    for (let y = 0; y < info.height; y++) {
      if (green(x, y)) { tops.push(y); break; }
    }
  }
  // Scaling the key image blurs an edge over a few pixels: heights that close
  // together are one level.
  const levels = [];
  tops.slice().sort((p, q) => p - q).forEach(y => {
    if (levels.length === 0 || y - levels[levels.length - 1] > 4) levels.push(y);
  });
  // span: distance between the highest and the lowest level, as a fraction of
  // the key height. last: height of the newest sample (right edge), 0 = bottom
  // of the key and 1 = top.
  const span = tops.length ? (Math.max(...tops) - Math.min(...tops)) / info.height : 0;
  const last = tops.length ? 1 - tops[tops.length - 1] / info.height : 0;
  return { levels: levels.length, columns: tops.length, width: info.width, span, last };
}

// Share of the key image that is clearly red, and the share that is clearly
// green. Classic fills everything below the line with the foreground colour.
// Soft fades its fill to the background, so little more than the line is green.
// Soft spectrum is the only style that draws red.
async function keyColors(piPort, keyIndex) {
  const state = await new Promise((resolve, reject) => {
    http.get({ host: '127.0.0.1', port: piPort, path: '/api/state' }, res => {
      let out = '';
      res.on('data', c => out += c);
      res.on('end', () => { try { resolve(JSON.parse(out)); } catch (e) { reject(e); } });
    }).on('error', reject);
  });
  const slot = (state.slots || []).find(x => x.deviceId === DEVICE && x.keyIndex === keyIndex);
  if (!slot || !slot.imageDataUrl) throw new Error('no key image for index ' + keyIndex);
  const buf = Buffer.from(slot.imageDataUrl.split(',')[1], 'base64');
  const { data, info } = await sharp(buf).ensureAlpha().raw().toBuffer({ resolveWithObject: true });
  let red = 0;
  let green = 0;
  for (let o = 0; o < data.length; o += 4) {
    const [r, g, b] = [data[o], data[o + 1], data[o + 2]];
    if (r > 150 && g < 80 && b < 80) red++;
    if (g > 60 && g > r + 30 && g > b + 30) green++;
  }
  const px = info.width * info.height;
  return { red: red / px, green: green / px };
}

// Where the text sits in the key image: rows and columns (as fractions of the
// key size) that hold white pixels (the value) and magenta pixels (the unit,
// once it is given that colour).
async function keyText(piPort, keyIndex) {
  const state = await new Promise((resolve, reject) => {
    http.get({ host: '127.0.0.1', port: piPort, path: '/api/state' }, res => {
      let out = '';
      res.on('data', c => out += c);
      res.on('end', () => { try { resolve(JSON.parse(out)); } catch (e) { reject(e); } });
    }).on('error', reject);
  });
  const slot = (state.slots || []).find(x => x.deviceId === DEVICE && x.keyIndex === keyIndex);
  if (!slot || !slot.imageDataUrl) throw new Error('no key image for index ' + keyIndex);
  const buf = Buffer.from(slot.imageDataUrl.split(',')[1], 'base64');
  const { data, info } = await sharp(buf).ensureAlpha().raw().toBuffer({ resolveWithObject: true });
  const box = () => ({ n: 0, top: 1, bottom: 0, left: 1, right: 0 });
  const white = box(), magenta = box();
  const add = (b, x, y) => {
    b.n++;
    b.top = Math.min(b.top, y / info.height); b.bottom = Math.max(b.bottom, y / info.height);
    b.left = Math.min(b.left, x / info.width); b.right = Math.max(b.right, x / info.width);
  };
  // The title is light grey and sits in the top third: only look below it.
  for (let y = Math.floor(info.height * 0.35); y < info.height; y++) {
    for (let x = 0; x < info.width; x++) {
      const o = (y * info.width + x) * 4;
      const [r, g, b] = [data[o], data[o + 1], data[o + 2]];
      if (r > 215 && g > 215 && b > 215) add(white, x, y);
      if (r > 170 && b > 170 && g < 110) add(magenta, x, y);
    }
  }
  return { white, magenta };
}

async function setValue(wsPort, ctx, key, value) {
  const { ws } = await connectPI(wsPort, ctx, READING_ACTION);
  sdpi(ws, ctx, READING_ACTION, key, value);
  await sleep(600);
  ws.close();
}

// Alternate the reading between two values so the graph shows both.
async function alternate(path, a, b, rounds) {
  for (let i = 0; i < rounds; i++) {
    await mockSet(path, a);
    await sleep(POLL_MS * 2);
    await mockSet(path, b);
    await sleep(POLL_MS * 2);
  }
}

async function run() {
  console.log('── reading option tests ──');

  await mockReset();
  const { wsPort, piPort } = await waitForDeckBridge(20000);
  console.log(`DeckBridge: wsPort=${wsPort} piPort=${piPort}`);

  for (const k of [KEY_TILE, KEY_RANGE]) await deleteSlot(piPort, k);
  await sleep(500);

  const ctx = await createSlot(piPort, KEY_TILE, READING_ACTION);
  await sleep(1500);
  await configureTile(wsPort, ctx, '/mockcpu/0', 'CPU Package');

  // Give the tile a visible history.
  await mockSet(RESET_PATH, 30);
  await sleep(POLL_MS * 2);
  await mockSet(RESET_PATH, 90);
  await sleep(POLL_MS * 2);
  await mockSet(RESET_PATH, 60);
  await sleep(POLL_MS * 8);
  console.log('setup complete — tile configured, history recorded');

  // ── TEST 1: the source cannot reset, and the inspector is told so ───────────
  console.log('\n[test 1] Reset on press is not offered');
  {
    const { ws, payload } = await connectPI(wsPort, ctx, READING_ACTION);
    ws.close();
    if (payload.resetSupported === false) pass('test 1 — the inspector payload says the source cannot reset MIN/MAX');
    else fail(`test 1 — resetSupported is ${JSON.stringify(payload.resetSupported)}, expected false`);
  }
  let s = await getTileSettings(wsPort, ctx, READING_ACTION);
  if (!s.resetMinMaxOnPress) pass('test 1 — resetMinMaxOnPress is off for a new tile');
  else fail('test 1 — resetMinMaxOnPress is on for a new tile');

  // ── TEST 2: a stored option does nothing without a source that can reset ────
  console.log('\n[test 2] a key press never resets, even with the option stored');
  await setOption(wsPort, ctx, 'resetMinMaxOnPress', true);
  const before = await mockReadings();
  const histBefore = await graphLevels(piPort, KEY_TILE);
  await pressKey(piPort, KEY_TILE);
  await sleep(POLL_MS * 3);
  const now = await mockReadings();
  if (now[RESET_PATH].min === before[RESET_PATH].min && now[RESET_PATH].max === before[RESET_PATH].max) {
    pass(`test 2 — the source keeps its range ${before[RESET_PATH].min}..${before[RESET_PATH].max}`);
  } else {
    fail(`test 2 — source range changed: ${JSON.stringify(now[RESET_PATH])}`);
  }
  const histStill = await graphLevels(piPort, KEY_TILE);
  if (histBefore.levels >= 2 && histStill.levels >= 2) {
    pass(`test 2 — the key keeps its graph history (${histStill.levels} levels)`);
  } else {
    fail(`test 2 — expected a graph with history before and after the press, got ${histBefore.levels} and ${histStill.levels} levels`);
  }

  // ── Range modes ───────────────────────────────────────────────────
  // "CPU Total" is held at 20 % and then moves between 50 and 54 %. Each mode has to draw the newest sample at the height that follows
  // from its range.
  await mockReset();
  const rangeCtx = await createSlot(piPort, KEY_RANGE, READING_ACTION);
  await sleep(1000);
  await configureTile(wsPort, rangeCtx, '/mockcpu/0', 'CPU Total');

  console.log('\n[test 5] Range defaults to Manual');
  s = await getTileSettings(wsPort, rangeCtx, READING_ACTION);
  if (!s.scaleMode) pass('test 5 — a new tile has no scaleMode (Manual)');
  else fail(`test 5 — a new tile has scaleMode=${JSON.stringify(s.scaleMode)}`);
  // Make sure the start value is in the history before the swing begins: the
  // Auto range in test 6 is derived from all three values.
  await mockSet(OTHER_PATH, 20);
  await sleep(POLL_MS * 3);
  await alternate(OTHER_PATH, 50, 54, 3);
  const manual = await graphLevels(piPort, KEY_RANGE);
  if (manual.last > 0.4 && manual.last < 0.65) {
    pass(`test 5 — Manual 0..100 draws 54 % at ${(manual.last * 100).toFixed(0)} % of the key height`);
  } else {
    fail(`test 5 — Manual 0..100 draws 54 % at ${(manual.last * 100).toFixed(0)} % of the key height, expected about 54 %`);
  }

  console.log('\n[test 6] Auto fits the range to the recent readings');
  await setValue(wsPort, rangeCtx, 'scaleMode', 'auto');
  s = await getTileSettings(wsPort, rangeCtx, READING_ACTION);
  if (s.scaleMode === 'auto') pass('test 6 — scaleMode=auto persisted');
  else fail(`test 6 — scaleMode not persisted: ${JSON.stringify(s.scaleMode)}`);
  if (s.min === 0 && s.max === 100) pass('test 6 — the manual limits 0..100 are kept');
  else fail(`test 6 — manual limits changed: ${s.min}..${s.max}`);
  await alternate(OTHER_PATH, 50, 54, 2);
  const auto = await graphLevels(piPort, KEY_RANGE);
  // The history holds the start value 20 and the 50/54 swing. Auto pads
  // 20..54 and rounds outward to 10..60, so 54 sits at 88 % of the height
  // instead of the 54 % it has on the manual scale.
  if (Math.abs(auto.last - 0.88) < 0.08) {
    pass(`test 6 — Auto draws 54 % at ${(auto.last * 100).toFixed(0)} % of the key height (range 10..60)`);
  } else {
    fail(`test 6 — Auto draws 54 % at ${(auto.last * 100).toFixed(0)} % of the key height, expected about 88 %`);
  }

  console.log('\n[test 7] Sensor Min / Max follows the range recorded by the source');
  await setValue(wsPort, rangeCtx, 'scaleMode', 'sensor');
  s = await getTileSettings(wsPort, rangeCtx, READING_ACTION);
  if (s.scaleMode === 'sensor') pass('test 7 — scaleMode=sensor persisted');
  else fail(`test 7 — scaleMode not persisted: ${JSON.stringify(s.scaleMode)}`);
  // Let the source report a recorded range of 40..60 around the value.
  await mockMinMax(OTHER_PATH, 40, 60);
  await mockSet(OTHER_PATH, 45);
  await sleep(POLL_MS * 3);
  const src = (await mockReadings())[OTHER_PATH];
  const want = (45 - Math.floor(src.min)) / (Math.ceil(src.max) - Math.floor(src.min));
  const sensor = await graphLevels(piPort, KEY_RANGE);
  if (src.min === 40 && src.max === 60) pass('test 7 — the source recorded 40..60');
  else fail(`test 7 — the source recorded ${src.min}..${src.max}, expected 40..60`);
  if (Math.abs(sensor.last - want) < 0.12) {
    pass(`test 7 — 45 % is drawn at ${(sensor.last * 100).toFixed(0)} % of the key height (range ${src.min}..${src.max})`);
  } else {
    fail(`test 7 — 45 % is drawn at ${(sensor.last * 100).toFixed(0)} % of the key height, expected about ${(want * 100).toFixed(0)} %`);
  }

  console.log('\n[test 8] back to Manual restores the manual limits');
  await setValue(wsPort, rangeCtx, 'scaleMode', 'manual');
  await sleep(POLL_MS * 2);
  const back = await graphLevels(piPort, KEY_RANGE);
  if (Math.abs(back.last - 0.45) < 0.12) pass(`test 8 — 45 % is drawn at ${(back.last * 100).toFixed(0)} % of the key height on 0..100`);
  else fail(`test 8 — 45 % is drawn at ${(back.last * 100).toFixed(0)} % of the key height, expected about 45 %`);

  // ── Graph styles  ──────────────────────────────────────────────────
  // Same tile, manual 0..100, reading held at 95 % so the top of the scale is drawn.
  await mockSet(OTHER_PATH, 95);
  await sleep(POLL_MS * 4);

  console.log('\n[test 9] Classic stays the default look');
  s = await getTileSettings(wsPort, rangeCtx, READING_ACTION);
  if (!s.graphStyle) pass('test 9 — the tile has no graphStyle (Classic)');
  else fail(`test 9 — the tile has graphStyle=${JSON.stringify(s.graphStyle)}`);
  const classic = await keyColors(piPort, KEY_RANGE);
  if (classic.red === 0 && classic.green > 0.25) pass(`test 9 — Classic fills ${(classic.green * 100).toFixed(0)} % of the key with green at 95 % and draws no red`);
  else fail(`test 9 — Classic: green on ${(classic.green * 100).toFixed(0)} %, red on ${(classic.red * 100).toFixed(1)} % of the key`);

  console.log('\n[test 10] Soft draws a faded fill');
  await setValue(wsPort, rangeCtx, 'graphStyle', 'soft');
  s = await getTileSettings(wsPort, rangeCtx, READING_ACTION);
  if (s.graphStyle === 'soft') pass('test 10 — graphStyle=soft persisted');
  else fail(`test 10 — graphStyle not persisted: ${JSON.stringify(s.graphStyle)}`);
  await sleep(POLL_MS * 3);
  const soft = await keyColors(piPort, KEY_RANGE);
  if (soft.red === 0 && soft.green > 0.005 && soft.green < classic.green / 3) {
    pass(`test 10 — Soft leaves a line and a faded fill: green on ${(soft.green * 100).toFixed(0)} % of the key (Classic ${(classic.green * 100).toFixed(0)} %), no red`);
  } else {
    fail(`test 10 — Soft: green on ${(soft.green * 100).toFixed(0)} % of the key (Classic ${(classic.green * 100).toFixed(0)} %), red on ${(soft.red * 100).toFixed(1)} %`);
  }

  console.log('\n[test 11] Soft spectrum draws red at the top of the range');
  await setValue(wsPort, rangeCtx, 'graphStyle', 'soft-load');
  await sleep(POLL_MS * 3);
  const spectrum = await keyColors(piPort, KEY_RANGE);
  if (spectrum.red > 0.03) pass(`test 11 — Soft spectrum draws red on ${(spectrum.red * 100).toFixed(1)} % of the key at 95 %`);
  else fail(`test 11 — Soft spectrum draws red on only ${(spectrum.red * 100).toFixed(1)} % of the key at 95 %`);

  console.log('\n[test 12] back to Classic');
  await setValue(wsPort, rangeCtx, 'graphStyle', 'classic');
  await sleep(POLL_MS * 3);
  const again = await keyColors(piPort, KEY_RANGE);
  // The fill keeps growing while 95 % scrolls in, so it is at least what it was.
  if (again.red === 0 && again.green > classic.green - 0.05) pass(`test 12 — the key is filled like Classic again (${(again.green * 100).toFixed(0)} %)`);
  else fail(`test 12 — after switching back: green on ${(again.green * 100).toFixed(0)} % (was ${(classic.green * 100).toFixed(0)} %), red ${(again.red * 100).toFixed(1)} %`);

  // ── Unit as its own element  ──────────────────────────────────────────
  // Same tile, "CPU Total" at 95 %. The unit gets magenta so it can be told
  // apart from the white value in the key image.
  console.log('\n[test 13] the unit is next to the value by default');
  s = await getTileSettings(wsPort, rangeCtx, READING_ACTION);
  if (!s.unitDisplay && !s.unitTextColor && !s.unitFontSize) pass('test 13 — the tile has no unit settings');
  else fail(`test 13 — unexpected unit settings: ${JSON.stringify({ unitDisplay: s.unitDisplay, unitTextColor: s.unitTextColor, unitFontSize: s.unitFontSize })}`);
  await setValue(wsPort, rangeCtx, 'valueFontSize', '16');
  await setValue(wsPort, rangeCtx, 'unitTextColor', '#ff00ff');
  await sleep(POLL_MS * 2);
  const inline = await keyText(piPort, KEY_RANGE);
  if (inline.white.n > 0 && inline.magenta.n === 0) pass('test 13 — one white "95%" label, no separate unit');
  else fail(`test 13 — inline: ${inline.white.n} white and ${inline.magenta.n} magenta pixels`);

  console.log('\n[test 14] Below value draws the unit under the value');
  await setValue(wsPort, rangeCtx, 'unitDisplay', 'below');
  await setValue(wsPort, rangeCtx, 'unitFontSize', '9');
  s = await getTileSettings(wsPort, rangeCtx, READING_ACTION);
  if (s.unitDisplay === 'below' && s.unitFontSize === 9 && s.unitTextColor === '#ff00ff') pass('test 14 — unitDisplay, unitFontSize and unitTextColor persisted');
  else fail(`test 14 — not persisted: ${JSON.stringify({ unitDisplay: s.unitDisplay, unitFontSize: s.unitFontSize, unitTextColor: s.unitTextColor })}`);
  await sleep(POLL_MS * 2);
  const belowText = await keyText(piPort, KEY_RANGE);
  if (belowText.white.n > 0 && belowText.magenta.n > 0 && belowText.magenta.top > belowText.white.bottom) {
    pass(`test 14 — the unit starts at ${(belowText.magenta.top * 100).toFixed(0)} % of the key height, below the value that ends at ${(belowText.white.bottom * 100).toFixed(0)} %`);
  } else {
    fail(`test 14 — value rows ${(belowText.white.top * 100).toFixed(0)}..${(belowText.white.bottom * 100).toFixed(0)} %, unit rows ${(belowText.magenta.top * 100).toFixed(0)}..${(belowText.magenta.bottom * 100).toFixed(0)} % (${belowText.magenta.n} px)`);
  }
  const unitHeight = belowText.magenta.bottom - belowText.magenta.top;
  const valueHeight = belowText.white.bottom - belowText.white.top;
  if (unitHeight < valueHeight) pass('test 14 — the 9 pt unit is smaller than the 16 pt value');
  else fail(`test 14 — unit height ${(unitHeight * 100).toFixed(0)} %, value height ${(valueHeight * 100).toFixed(0)} %`);

  console.log('\n[test 15] offsets move the unit and the value');
  await setValue(wsPort, rangeCtx, 'unitOffset', '6');
  await setValue(wsPort, rangeCtx, 'valueOffset', '-6');
  await sleep(POLL_MS * 2);
  const moved = await keyText(piPort, KEY_RANGE);
  const px = 1 / 72;
  if (Math.abs((moved.magenta.top - belowText.magenta.top) - 6 * px) < 1.5 * px) pass('test 15 — unitOffset 6 moves the unit 6 px down');
  else fail(`test 15 — unit moved ${((moved.magenta.top - belowText.magenta.top) / px).toFixed(1)} px, expected 6`);
  if (Math.abs((moved.white.top - belowText.white.top) + 6 * px) < 1.5 * px) pass('test 15 — valueOffset -6 moves the value 6 px up');
  else fail(`test 15 — value moved ${((moved.white.top - belowText.white.top) / px).toFixed(1)} px, expected -6`);
  await setValue(wsPort, rangeCtx, 'unitFontSize', '99');
  s = await getTileSettings(wsPort, rangeCtx, READING_ACTION);
  if (s.unitFontSize === 9) pass('test 15 — an out-of-range unit size is refused');
  else fail(`test 15 — unitFontSize became ${s.unitFontSize}`);

  console.log('\n[test 16] Hidden leaves the unit out');
  await setValue(wsPort, rangeCtx, 'unitDisplay', 'hidden');
  await sleep(POLL_MS * 2);
  const hidden = await keyText(piPort, KEY_RANGE);
  if (hidden.white.n > 0 && hidden.magenta.n === 0) pass('test 16 — the value is drawn, the unit is not');
  else fail(`test 16 — hidden: ${hidden.white.n} white and ${hidden.magenta.n} magenta pixels`);

  // ── Condensed font and text backdrop  ──────────────────────────────
  console.log('\n[test 17] Condensed font draws narrower text');
  await setValue(wsPort, rangeCtx, 'unitDisplay', 'inline');
  await setValue(wsPort, rangeCtx, 'valueOffset', '0');
  await sleep(POLL_MS * 2);
  const regular = await keyText(piPort, KEY_RANGE);
  await setOption(wsPort, rangeCtx, 'condensedFont', true);
  s = await getTileSettings(wsPort, rangeCtx, READING_ACTION);
  if (s.condensedFont === true && !s.unitDisplay) pass('test 17 — condensedFont persisted, unitDisplay back to the default');
  else fail(`test 17 — settings: ${JSON.stringify({ condensedFont: s.condensedFont, unitDisplay: s.unitDisplay })}`);
  await sleep(POLL_MS * 2);
  const condensed = await keyText(piPort, KEY_RANGE);
  const widthOf = t => t.white.right - t.white.left;
  if (widthOf(condensed) < widthOf(regular) * 0.9) {
    pass(`test 17 — "95%" is ${(widthOf(condensed) * 100).toFixed(0)} % of the key wide, was ${(widthOf(regular) * 100).toFixed(0)} %`);
  } else {
    fail(`test 17 — "95%" is ${(widthOf(condensed) * 100).toFixed(0)} % of the key wide, was ${(widthOf(regular) * 100).toFixed(0)} %`);
  }

  console.log('\n[test 18] Text backdrop darkens the graph behind the text');
  const plain = await keyColors(piPort, KEY_RANGE);
  await setOption(wsPort, rangeCtx, 'textShadow', true);
  s = await getTileSettings(wsPort, rangeCtx, READING_ACTION);
  if (s.textShadow === true) pass('test 18 — textShadow persisted');
  else fail(`test 18 — textShadow not persisted: ${JSON.stringify(s.textShadow)}`);
  await sleep(POLL_MS * 2);
  const backed = await keyColors(piPort, KEY_RANGE);
  if (backed.green < plain.green - 0.05) pass(`test 18 — the backdrop covers the fill: green on ${(backed.green * 100).toFixed(0)} % of the key, was ${(plain.green * 100).toFixed(0)} %`);
  else fail(`test 18 — green on ${(backed.green * 100).toFixed(0)} % of the key with the backdrop, ${(plain.green * 100).toFixed(0)} % without`);

  // ── Cleanup ─────────────────────────────────────────────────────────────────
  await mockReset();
  for (const k of [KEY_TILE, KEY_RANGE]) await deleteSlot(piPort, k);

  summary();
}

run().catch(e => { console.error(e); process.exit(1); });
