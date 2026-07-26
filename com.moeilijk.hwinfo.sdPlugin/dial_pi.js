var websocket = null,
  uuid = null,
  actionInfo = {},
  currentSettings = { activeIndex: 0, pages: [] },
  currentCatalog = { sensors: [], readings: [] },
  pageSelectionDraft = { sensorUid: "", readingId: "" },
  thresholdAdvancedOpen = Object.create(null);

var globalThresholds = [];
var bulkPreviewCandidates = [];

// Reject the prototype-pollution keys before any dynamic property write whose key
// can originate from settings/catalog data, so a malicious sensor/reading name
// (e.g. "__proto__") cannot reach Object.prototype.
function isUnsafeKey(k) {
  return k === "__proto__" || k === "constructor" || k === "prototype";
}
var snoozeDurationOptions = [300000, 900000, 3600000, 0];
var dialPageColorPalette = [
  { foregroundColor: "#005128", highlightColor: "#009e00" },
  { foregroundColor: "#003f73", highlightColor: "#00a2ff" },
  { foregroundColor: "#5a3b87", highlightColor: "#b06cff" },
  { foregroundColor: "#6a4a00", highlightColor: "#ffbf33" },
  { foregroundColor: "#6f1d1b", highlightColor: "#ff5a4f" },
  { foregroundColor: "#004b50", highlightColor: "#00d6d6" },
  { foregroundColor: "#4d3d00", highlightColor: "#d8d000" },
  { foregroundColor: "#00421f", highlightColor: "#39d98a" },
  { foregroundColor: "#4b184f", highlightColor: "#ff66d8" },
  { foregroundColor: "#5b2b00", highlightColor: "#ff8a1f" },
  { foregroundColor: "#173b64", highlightColor: "#66c2ff" },
  { foregroundColor: "#3f4f13", highlightColor: "#b5e853" },
  { foregroundColor: "#4f2333", highlightColor: "#ff7aa8" },
  { foregroundColor: "#1d4a45", highlightColor: "#5ef2c2" },
  { foregroundColor: "#2f2d6b", highlightColor: "#8f8cff" },
  { foregroundColor: "#5a3216", highlightColor: "#d98b45" }
];

function dialDefaultPageColors(index) {
  var palette = dialPageColorPalette.length ? dialPageColorPalette : [{ foregroundColor: "#005128", highlightColor: "#009e00" }];
  var i = Number(index) || 0;
  if (i < 0) i = 0;
  return palette[i % palette.length];
}

function connectElgatoStreamDeckSocket(inPort, inUUID, inRegisterEvent, inInfo, inActionInfo) {
  uuid = inUUID;
  actionInfo = JSON.parse(inActionInfo);
  var info = JSON.parse(inInfo);
  websocket = new WebSocket("ws://" + ((typeof location !== "undefined" && location.hostname) ? location.hostname : "127.0.0.1") + ":" + inPort);

  websocket.onopen = function () {
    websocket.send(JSON.stringify({ event: inRegisterEvent, uuid: inUUID }));
    sendValueToPlugin("propertyInspectorConnected", "property_inspector");
  };

  websocket.onmessage = function (evt) {
    var msg = JSON.parse(evt.data);
    if (msg.event !== "sendToPropertyInspector") return;
    var payload = msg.payload || {};

    if (typeof payload.error === "boolean") {
      document.getElementById("ui").style.display = payload.error ? "none" : "";
      document.getElementById("error").style.display = payload.error ? "block" : "none";
      return;
    }

    if (payload.catalog) {
      currentCatalog = payload.catalog;
      renderSelectedPageSelection();
      renderActiveGlobals();
    }
    if (Array.isArray(payload.globalThresholds)) {
      globalThresholds = payload.globalThresholds;
      renderActiveGlobals();
    }
    if (payload.dialSettings) {
      currentSettings = normalizeSettings(payload.dialSettings);
      renderPages();
    }
  };
}

function sendValueToPlugin(value, key) {
  if (!websocket || websocket.readyState !== 1) return;
  websocket.send(JSON.stringify({
    event: "sendToPlugin",
    context: uuid,
    action: actionInfo.action,
    payload: { [key]: value }
  }));
}

function normalizeSettings(settings) {
  settings = settings || {};
  if (!Array.isArray(settings.pages)) settings.pages = [];
  settings.pages = settings.pages.map(normalizePage);
  if (typeof settings.activeIndex !== "number") settings.activeIndex = 0;
  if (settings.activeIndex < 0) settings.activeIndex = 0;
  if (settings.pages.length && settings.activeIndex >= settings.pages.length) {
    settings.activeIndex = settings.pages.length - 1;
  }
  return settings;
}

function normalizePage(page) {
  page = page || {};
  if (page.min === undefined || page.min === null || page.min === "") page.min = 0;
  if (page.max === undefined || page.max === null || page.max === "") page.max = 100;
  if (!page.format) page.format = "";
  if (!page.divisor) page.divisor = "";
  if (!page.graphUnit) page.graphUnit = "";
  if (!page.titleColor) page.titleColor = "#b7b7b7";
  if (!page.foregroundColor) page.foregroundColor = "#005128";
  if (!page.backgroundColor) page.backgroundColor = "#000000";
  if (!page.highlightColor) page.highlightColor = "#009e00";
  if (!page.valueTextColor) page.valueTextColor = "#ffffff";
  if (!page.graphMode) page.graphMode = "both";
  if (!page.graphHeightPct) page.graphHeightPct = 100;
  if (!page.graphLineThickness) page.graphLineThickness = 1;
  if (page.smoothingAlpha === undefined || page.smoothingAlpha === null || page.smoothingAlpha === "") page.smoothingAlpha = 0;
  if (!Array.isArray(page.thresholds)) page.thresholds = [];
  if (!Array.isArray(page.suppressedGlobalIDs)) page.suppressedGlobalIDs = [];
  page.snoozeDurations = normalizeSnoozeDurations(page.snoozeDurations);
  if (!page.currentThresholdId) page.currentThresholdId = "";
  if (!page.titleFontSize) page.titleFontSize = 0;
  if (!page.valueFontSize) page.valueFontSize = 0;
  if (!page.textStrokeColor) page.textStrokeColor = page.backgroundColor || "#000000";
  if (page.showTitleInGraph === undefined || page.showTitleInGraph === null) page.showTitleInGraph = true;
  page.textStroke = !!page.textStroke;
  return page;
}

function saveSettings() {
  currentSettings = normalizeSettings(currentSettings);
  sendValueToPlugin(currentSettings, "dialSetSettings");
  renderPages();
}

function pageTitle(page) {
  return page.title || page.readingLabel || page.sensorUid || "Reading";
}

function renderPages() {
  var list = document.getElementById("pageList");
  list.innerHTML = "";
  currentSettings.pages.forEach(function (page, index) {
    var opt = document.createElement("option");
    opt.value = String(index);
    opt.textContent = pageTitle(page);
    if (index === currentSettings.activeIndex) opt.selected = true;
    list.appendChild(opt);
  });
  var pagesLabel = document.getElementById("pagesLabel");
  if (pagesLabel) {
    var pageCount = currentSettings.pages.length;
    pagesLabel.textContent = pageCount ? "Pages (" + pageCount + ")" : "Pages";
  }
  updatePageButtons();
  renderDialSettings();
  renderPageSettings();
}

// Action-level (whole-dial) settings, separate from the per-page settings.
function renderDialSettings() {
  setValue("defaultView", currentSettings.defaultView || "fullscreen");
  setValue("overviewStyle", currentSettings.overviewStyle || "stacked");
  setValue("overviewPages", currentSettings.overviewPages || "3");
  setValue("indicatorStyle", currentSettings.indicatorStyle || "auto");
  setValue("indicatorFullscreen", currentSettings.indicatorFullscreen === true);
  setValue("reverseRotation", currentSettings.reverseRotation === true);
  setValue("indicatorColor", currentSettings.indicatorColor || "#bec6ce");
  setValue("indicatorSize", currentSettings.indicatorSize != null ? currentSettings.indicatorSize : 6);
  setValue("separatorWidth", currentSettings.separatorWidth != null ? currentSettings.separatorWidth : 3);
  setValue("separatorColor", currentSettings.separatorColor || "#363e46");
  // Overview style also applies when the default view is Fullscreen (the dial can
  // be toggled into the overview at runtime), so the option is always shown.
}

function selectedPageIndex() {
  var list = document.getElementById("pageList");
  var raw = list.selectedIndex >= 0 ? list.selectedIndex : currentSettings.activeIndex;
  var idx = parseInt(raw, 10);
  // Always a non-negative integer: prevents a string key (e.g. "__proto__") from
  // reaching currentSettings.pages[idx] as a prototype-pollution gadget.
  return Number.isInteger(idx) && idx >= 0 ? idx : 0;
}

function updatePageButtons() {
  var idx = selectedPageIndex();
  var count = currentSettings.pages.length;
  document.getElementById("removePageBtn").disabled = count === 0;
  document.getElementById("moveUpBtn").disabled = count === 0 || idx <= 0;
  document.getElementById("moveDownBtn").disabled = count === 0 || idx >= count - 1;
  var addBtn = document.getElementById("addPageBtn");
  var reading = document.getElementById("pageReadingSelect");
  if (addBtn && reading) addBtn.disabled = reading.disabled || !reading.value;
  var bulkPreviewBtn = document.getElementById("bulkPreviewBtn");
  if (bulkPreviewBtn && reading) bulkPreviewBtn.disabled = reading.disabled || !reading.value;
}

function selectedPage() {
  var idx = selectedPageIndex();
  if (idx < 0 || idx >= currentSettings.pages.length) return null;
  currentSettings.pages[idx] = normalizePage(currentSettings.pages[idx]);
  return currentSettings.pages[idx];
}

function resetPageSelectionDraft(page) {
  pageSelectionDraft = {
    sensorUid: page ? page.sensorUid || "" : "",
    readingId: page ? String(page.readingId || "") : ""
  };
}

function sensorMatchesFilter(sensor, term, category) {
  var searchText = (sensor.searchText || [sensor.name, sensor.category, sensor.uid].join(" ")).toLowerCase();
  var sensorCategory = (sensor.category || "other").toLowerCase();
  if (category && sensorCategory !== category) return false;
  return !term || searchText.indexOf(term) !== -1;
}

function readingsForSensor(sensorUid) {
  return (currentCatalog.readings || []).filter(function (reading) {
    return reading.sensorUid === sensorUid;
  }).sort(compareReadings);
}

function populateSelectedPageSensors() {
  var sel = document.getElementById("pageSensorSelect");
  if (!sel) return;
  var selectedSensorUid = pageSelectionDraft.sensorUid || sel.value;
  var search = (document.getElementById("pageSensorSearch").value || "").trim().toLowerCase();
  var category = (document.getElementById("pageSensorCategoryFilter").value || "").trim().toLowerCase();
  var sensors = (currentCatalog.sensors || []).slice().sort(function (a, b) {
    return (a.name || "") > (b.name || "") ? 1 : (a.name || "") < (b.name || "") ? -1 : 0;
  });
  var filtered = sensors.filter(function (sensor) {
    return sensorMatchesFilter(sensor, search, category);
  });
  if (selectedSensorUid && !filtered.some(function (sensor) { return sensor.uid === selectedSensorUid; })) {
    sensors.forEach(function (sensor) {
      if (sensor.uid === selectedSensorUid) filtered.unshift(sensor);
    });
  }
  sel.innerHTML = "";
  filtered.forEach(function (sensor) {
    var opt = document.createElement("option");
    opt.value = sensor.uid;
    opt.textContent = sensor.name || sensor.uid;
    if (sensor.uid === selectedSensorUid) opt.selected = true;
    sel.appendChild(opt);
  });
  sel.disabled = filtered.length === 0;
  if (!selectedSensorUid && filtered.length > 0) sel.value = filtered[0].uid;
  updatePageButtons();
}

function populateSelectedPageReadings() {
  var sel = document.getElementById("pageReadingSelect");
  if (!sel) return;
  var sensorSel = document.getElementById("pageSensorSelect");
  var sensorUid = pageSelectionDraft.sensorUid || (sensorSel ? sensorSel.value : "");
  var selectedReadingId = pageSelectionDraft.readingId || sel.value;
  var readings = readingsForSensor(sensorUid);
  sel.innerHTML = "";
  readings.forEach(function (reading) {
    var opt = document.createElement("option");
    opt.value = String(reading.id);
    opt.textContent = readingOptionLabel(reading);
    if (String(reading.id) === String(selectedReadingId)) opt.selected = true;
    sel.appendChild(opt);
  });
  sel.disabled = readings.length === 0;
  if (!selectedReadingId && readings.length > 0) {
    sel.value = String(readings[0].id);
    pageSelectionDraft.readingId = sel.value;
  }
  updatePageButtons();
}

function renderSelectedPageSelection() {
  populateSelectedPageSensors();
  populateSelectedPageReadings();
}

// For range sliders that reuse the tile range-wrap markup, the real <input> is
// nested inside the #id container; return it so the shared helpers work as-is.
function fieldInput(host) {
  if (!host) return null;
  if (host.tagName === "INPUT" || host.tagName === "SELECT" || host.tagName === "TEXTAREA") return host;
  return host.querySelector("input[type=range]") || host;
}

function setValue(id, value) {
  var el = fieldInput(document.getElementById(id));
  if (!el) return;
  if (el.type === "checkbox") el.checked = !!value;
  else el.value = value === undefined || value === null ? "" : String(value);
  if (el.type === "range" && typeof positionRangeVal === "function") positionRangeVal(el);
}

function renderPageSettings() {
  var panel = document.getElementById("pageSettings");
  var page = selectedPage();
  resetPageSelectionDraft(page);
  panel.hidden = !page;
  if (!page) return;
  setValue("pageTitle", page.title || "");
  setValue("showTitleInGraph", page.showTitleInGraph !== false);
  setValue("graphMode", page.graphMode || "both");
  setValue("minValue", page.min);
  setValue("maxValue", page.max);
  setValue("formatValue", page.format || "");
  setValue("divisorValue", page.divisor || "");
  setValue("graphUnit", page.graphUnit || "");
  setValue("titleFontSize", page.titleFontSize || 14);
  setValue("valueFontSize", page.valueFontSize || 18);
  setValue("smoothingAlpha", page.smoothingAlpha > 0 ? page.smoothingAlpha : 1);
  setValue("graphHeightPct", page.graphHeightPct || 100);
  setValue("graphLineThickness", page.graphLineThickness || 1);
  setValue("titleColor", page.titleColor || "#b7b7b7");
  setValue("valueTextColor", page.valueTextColor || "#ffffff");
  setValue("backgroundColor", page.backgroundColor || "#000000");
  setValue("foregroundColor", page.foregroundColor || "#005128");
  setValue("highlightColor", page.highlightColor || "#009e00");
  setValue("textStroke", page.textStroke);
  setValue("textStrokeColor", page.textStrokeColor || page.backgroundColor || "#000000");
  applySnoozeDurationsToUI(page);
  renderThresholds(page.thresholds || []);
  renderActiveGlobals();
  renderSelectedPageSelection();
}

function bindPageField(id, key, parser) {
  var el = fieldInput(document.getElementById(id));
  if (!el || el.dataset.bound) return;
  el.dataset.bound = "1";
  var handler = function () {
    var page = selectedPage();
    if (!page || isUnsafeKey(key)) return;
    var raw = el.type === "checkbox" ? el.checked : el.value;
    page[key] = parser ? parser(raw) : raw;
    if (el.type === "range" && typeof positionRangeVal === "function") positionRangeVal(el);
    saveSettings();
  };
  el.addEventListener("input", handler);
  el.addEventListener("change", handler);
}

// bindActionField binds an action-level control (writes to currentSettings, the
// whole dial) instead of the selected page.
function bindActionField(id, key, parser) {
  var el = fieldInput(document.getElementById(id));
  if (!el || el.dataset.bound) return;
  el.dataset.bound = "1";
  var handler = function () {
    if (isUnsafeKey(key)) return;
    var raw = el.type === "checkbox" ? el.checked : el.value;
    currentSettings[key] = parser ? parser(raw) : raw;
    if (el.type === "range" && typeof positionRangeVal === "function") positionRangeVal(el);
    saveSettings();
  };
  el.addEventListener("input", handler);
  el.addEventListener("change", handler);
}

function bindDialSettings() {
  bindActionField("defaultView", "defaultView");
  bindActionField("overviewStyle", "overviewStyle");
  bindActionField("overviewPages", "overviewPages");
  bindActionField("indicatorStyle", "indicatorStyle");
  bindActionField("indicatorFullscreen", "indicatorFullscreen", function (v) { return !!v; });
  bindActionField("reverseRotation", "reverseRotation", function (v) { return !!v; });
  bindActionField("indicatorColor", "indicatorColor");
  bindActionField("indicatorSize", "indicatorSize", function (v) { return Number(v) || 6; });
  bindActionField("separatorWidth", "separatorWidth", function (v) { return Number(v) || 0; });
  bindActionField("separatorColor", "separatorColor");
}

function bindPageSettings() {
  bindPageField("pageTitle", "title");
  bindPageField("showTitleInGraph", "showTitleInGraph", function (v) { return !!v; });
  bindPageField("graphMode", "graphMode");
  bindPageField("minValue", "min", function (v) { return Number(v) || 0; });
  bindPageField("maxValue", "max", function (v) { return Number(v) || 100; });
  bindPageField("formatValue", "format");
  bindPageField("divisorValue", "divisor");
  bindPageField("graphUnit", "graphUnit");
  bindPageField("titleFontSize", "titleFontSize", function (v) { return Number(v) || 0; });
  bindPageField("valueFontSize", "valueFontSize", function (v) { return Number(v) || 0; });
  bindPageField("graphHeightPct", "graphHeightPct", function (v) { return Number(v) || 100; });
  bindPageField("graphLineThickness", "graphLineThickness", function (v) { return Number(v) || 1; });
  bindPageField("smoothingAlpha", "smoothingAlpha", function (v) { return Number(v) || 0; });
  bindPageField("titleColor", "titleColor");
  bindPageField("valueTextColor", "valueTextColor");
  bindPageField("backgroundColor", "backgroundColor");
  bindPageField("foregroundColor", "foregroundColor");
  bindPageField("highlightColor", "highlightColor");
  bindPageField("textStroke", "textStroke", function (v) { return !!v; });
  bindPageField("textStrokeColor", "textStrokeColor");
}

function normalizeSnoozeDurations(values) {
  if (!Array.isArray(values)) return [];
  var seen = Object.create(null);
  values.forEach(function (value) {
    var parsed = parseInt(value, 10);
    if (!isNaN(parsed)) seen[parsed] = true;
  });
  return snoozeDurationOptions.filter(function (value) {
    return seen[value] === true;
  });
}

function readSnoozeDurationsFromUI() {
  var selected = [];
  Array.from(document.querySelectorAll(".snooze-duration")).forEach(function (button) {
    if (button.classList.contains("is-selected")) {
      selected.push(parseInt(button.dataset.value, 10));
    }
  });
  return normalizeSnoozeDurations(selected);
}

function setSnoozePresetSelected(button, selected) {
  if (!button || !button.classList) return;
  button.classList.toggle("is-selected", selected === true);
}

function applySnoozeDurationsToUI(page) {
  var selected = normalizeSnoozeDurations(page && page.snoozeDurations ? page.snoozeDurations : []);
  var selectedMap = Object.create(null);
  selected.forEach(function (value) {
    selectedMap[String(value)] = true;
  });
  Array.from(document.querySelectorAll(".snooze-duration")).forEach(function (button) {
    setSnoozePresetSelected(button, selectedMap[button.dataset.value] === true);
  });
}

function bindSnoozeControls() {
  Array.from(document.querySelectorAll(".snooze-duration")).forEach(function (button) {
    if (button.dataset.bound) return;
    button.dataset.bound = "1";
    button.addEventListener("click", function () {
      var page = selectedPage();
      if (!page) return;
      setSnoozePresetSelected(button, !button.classList.contains("is-selected"));
      page.snoozeDurations = readSnoozeDurationsFromUI();
      saveSettings();
    });
  });
}

function parseOptionalNumber(value) {
  if (value === undefined || value === null || value === "") return 0;
  var parsed = Number(value);
  return isNaN(parsed) ? 0 : parsed;
}

function parseOptionalInt(value) {
  return Math.round(parseOptionalNumber(value));
}

function createThreshold(name) {
  var id = "threshold-" + Date.now().toString(36) + "-" + Math.random().toString(36).slice(2, 8);
  return {
    id: id,
    name: name || "New Threshold",
    text: "",
    textColor: "#ffffff",
    enabled: true,
    operator: ">=",
    value: 0,
    hysteresis: 0,
    dwellMs: 0,
    cooldownMs: 0,
    sticky: false,
    backgroundColor: "#333300",
    foregroundColor: "#999900",
    highlightColor: "#ffff00",
    valueTextColor: "#ffff00"
  };
}

function updateThresholdField(threshold, key, value) {
  if (!threshold || isUnsafeKey(key)) return;
  if (key === "enabled" || key === "sticky") {
    threshold[key] = value === true || value === "true";
    return;
  }
  if (key === "value" || key === "hysteresis") {
    threshold[key] = parseOptionalNumber(value);
    return;
  }
  if (key === "dwellMs" || key === "cooldownMs") {
    threshold[key] = parseOptionalInt(value);
    return;
  }
  threshold[key] = value;
}

function findThreshold(page, thresholdId) {
  if (!page || !Array.isArray(page.thresholds)) return null;
  return page.thresholds.find(function (threshold) {
    return threshold.id === thresholdId;
  }) || null;
}

function addThresholdToSelectedPage(name) {
  var page = selectedPage();
  if (!page) return null;
  page.thresholds.push(createThreshold(name));
  saveSettings();
  return page.thresholds[page.thresholds.length - 1];
}

function removeThresholdFromSelectedPage(thresholdId) {
  var page = selectedPage();
  if (!page) return;
  page.thresholds = page.thresholds.filter(function (threshold) {
    return threshold.id !== thresholdId;
  });
  if (page.currentThresholdId === thresholdId) page.currentThresholdId = "";
  delete thresholdAdvancedOpen[thresholdId];
  saveSettings();
}

function reorderSelectedPageThreshold(thresholdId, direction) {
  var page = selectedPage();
  if (!page) return;
  var idx = page.thresholds.findIndex(function (threshold) {
    return threshold.id === thresholdId;
  });
  var next = direction === "up" ? idx - 1 : idx + 1;
  if (idx < 0 || next < 0 || next >= page.thresholds.length) return;
  var threshold = page.thresholds[idx];
  page.thresholds[idx] = page.thresholds[next];
  page.thresholds[next] = threshold;
  saveSettings();
}

function updateSelectedPageThreshold(thresholdId, key, value) {
  var page = selectedPage();
  var threshold = findThreshold(page, thresholdId);
  if (!threshold) return;
  updateThresholdField(threshold, key, value);
  saveSettings();
}

function bindThresholdControls() {
  var addThresholdBtn = document.querySelector("#addThresholdBtn");
  var newThresholdName = document.querySelector("#newThresholdName");
  if (addThresholdBtn && !addThresholdBtn.dataset.bound) {
    addThresholdBtn.dataset.bound = "1";
    addThresholdBtn.addEventListener("click", function (e) {
      e.preventDefault();
      e.stopPropagation();
      var name = newThresholdName && newThresholdName.value.trim() ? newThresholdName.value.trim() : "New Threshold";
      addThresholdToSelectedPage(name);
      if (newThresholdName) newThresholdName.value = "";
    });
  }
  if (newThresholdName && !newThresholdName.dataset.bound) {
    newThresholdName.dataset.bound = "1";
    newThresholdName.addEventListener("keypress", function (e) {
      if (e.key === "Enter" && addThresholdBtn) {
        e.preventDefault();
        e.stopPropagation();
        addThresholdBtn.click();
      }
    });
  }
}

function renderThresholds(thresholds) {
  var container = document.querySelector("#thresholdsContainer");
  if (!container) return;
  thresholds = Array.isArray(thresholds) ? thresholds : [];

  var existingItems = container.querySelectorAll(".threshold-item");
  var existingIds = Array.prototype.map.call(existingItems, function (el) { return el.dataset.thresholdId; });
  var incomingIds = thresholds.map(function (t) { return t.id; });

  if (JSON.stringify(existingIds) !== JSON.stringify(incomingIds)) {
    container.innerHTML = "";
    thresholds.forEach(function (threshold, index) {
      container.appendChild(createThresholdElement(threshold, index, thresholds.length));
    });
    return;
  }

  var active = document.activeElement;
  thresholds.forEach(function (t) {
    var item = container.querySelector('.threshold-item[data-threshold-id="' + t.id + '"]');
    if (!item || (active && item.contains(active))) return;
    var set = function (sel, val) {
      var el = item.querySelector(sel);
      if (el && el.value !== String(val == null ? "" : val)) el.value = val == null ? "" : val;
    };
    set(".threshold-name", t.name || "");
    set(".threshold-text", t.text || "");
    set(".threshold-value", t.value != null ? t.value : "");
    set(".threshold-hysteresis", t.hysteresis != null ? t.hysteresis : "");
    set(".threshold-dwell", t.dwellMs != null ? t.dwellMs : "");
    set(".threshold-cooldown", t.cooldownMs != null ? t.cooldownMs : "");
  });
}

function bindDebouncedInput(input, handler) {
  var timeout;
  input.addEventListener("input", function (e) {
    clearTimeout(timeout);
    timeout = setTimeout(function () {
      handler(e.target.value);
    }, 300);
  });
}

function createThresholdElement(threshold, index, total) {
  var template = document.querySelector("#thresholdTemplate");
  var clone = template.content.cloneNode(true);
  var wrapper = clone.querySelector(".threshold-item");
  wrapper.dataset.thresholdId = threshold.id;

  var nameInput = clone.querySelector(".threshold-name");
  var textInput = clone.querySelector(".threshold-text");
  var operatorSelect = clone.querySelector(".threshold-operator");
  var valueInput = clone.querySelector(".threshold-value");
  var hysteresisInput = clone.querySelector(".threshold-hysteresis");
  var dwellInput = clone.querySelector(".threshold-dwell");
  var cooldownInput = clone.querySelector(".threshold-cooldown");
  var bgInput = clone.querySelector(".threshold-bg");
  var fgInput = clone.querySelector(".threshold-fg");
  var hlInput = clone.querySelector(".threshold-hl");
  var vtInput = clone.querySelector(".threshold-vt");
  var tcInput = clone.querySelector(".threshold-tc");

  nameInput.value = threshold.name || "";
  textInput.value = threshold.text || "";
  operatorSelect.value = threshold.operator || ">=";
  valueInput.value = threshold.value !== undefined && threshold.value !== null ? threshold.value : "";
  hysteresisInput.value = threshold.hysteresis !== undefined && threshold.hysteresis !== null ? threshold.hysteresis : "";
  dwellInput.value = threshold.dwellMs !== undefined && threshold.dwellMs !== null ? threshold.dwellMs : "";
  cooldownInput.value = threshold.cooldownMs !== undefined && threshold.cooldownMs !== null ? threshold.cooldownMs : "";
  bgInput.value = threshold.backgroundColor || "#333300";
  fgInput.value = threshold.foregroundColor || "#999900";
  hlInput.value = threshold.highlightColor || "#ffff00";
  vtInput.value = threshold.valueTextColor || "#ffff00";
  if (tcInput) tcInput.value = threshold.textColor || "#ffffff";

  var moveUpBtn = clone.querySelector(".threshold-move-up");
  var moveDownBtn = clone.querySelector(".threshold-move-down");
  if (moveUpBtn) moveUpBtn.disabled = index === 0;
  if (moveDownBtn) moveDownBtn.disabled = index === total - 1;

  var toggleBtn = clone.querySelector(".threshold-toggle");
  var stickyBtn = clone.querySelector(".threshold-sticky-toggle");
  var bringFrontBtn = clone.querySelector(".threshold-bringfront-toggle");
  var advancedToggleBtn = clone.querySelector(".threshold-advanced-toggle");
  var advancedPanel = clone.querySelector(".threshold-advanced-panel");
  var settingsDiv = clone.querySelector(".threshold-settings");
  var thresholdId = threshold.id;
  var isEnabled = threshold.enabled !== false;
  var isSticky = threshold.sticky === true;
  var isBringFront = threshold.bringToFront === true;
  var isAdvancedOpen = thresholdAdvancedOpen[thresholdId] === true;

  function updateToggleState() {
    toggleBtn.textContent = isEnabled ? "on" : "off";
    toggleBtn.style.background = isEnabled ? "#4a4" : "#a44";
    settingsDiv.style.display = isEnabled ? "block" : "none";
  }

  function updateStickyState() {
    if (!stickyBtn) return;
    stickyBtn.textContent = isSticky ? "on" : "off";
    stickyBtn.style.background = isSticky ? "#4a4" : "#a44";
    stickyBtn.style.color = "#fff";
  }

  function updateBringFrontState() {
    if (!bringFrontBtn) return;
    bringFrontBtn.textContent = isBringFront ? "on" : "off";
    bringFrontBtn.style.background = isBringFront ? "#4a4" : "#a44";
    bringFrontBtn.style.color = "#fff";
  }

  function updateAdvancedState() {
    if (!advancedToggleBtn || !advancedPanel) return;
    advancedToggleBtn.textContent = isAdvancedOpen ? "Advanced ▼" : "Advanced ▶";
    advancedPanel.style.display = isAdvancedOpen ? "block" : "none";
  }

  updateToggleState();
  updateStickyState();
  updateBringFrontState();
  updateAdvancedState();

  toggleBtn.addEventListener("click", function () {
    isEnabled = !isEnabled;
    updateToggleState();
    updateSelectedPageThreshold(thresholdId, "enabled", isEnabled);
  });
  bindDebouncedInput(nameInput, function (value) { updateSelectedPageThreshold(thresholdId, "name", value); });
  bindDebouncedInput(textInput, function (value) { updateSelectedPageThreshold(thresholdId, "text", value); });
  operatorSelect.addEventListener("change", function (e) { updateSelectedPageThreshold(thresholdId, "operator", e.target.value); });
  bindDebouncedInput(valueInput, function (value) { updateSelectedPageThreshold(thresholdId, "value", value); });
  bindDebouncedInput(hysteresisInput, function (value) { updateSelectedPageThreshold(thresholdId, "hysteresis", value); });
  bindDebouncedInput(dwellInput, function (value) { updateSelectedPageThreshold(thresholdId, "dwellMs", value); });
  bindDebouncedInput(cooldownInput, function (value) { updateSelectedPageThreshold(thresholdId, "cooldownMs", value); });
  if (stickyBtn) {
    stickyBtn.addEventListener("click", function () {
      isSticky = !isSticky;
      updateStickyState();
      updateSelectedPageThreshold(thresholdId, "sticky", isSticky);
    });
  }
  if (bringFrontBtn) {
    bringFrontBtn.addEventListener("click", function () {
      isBringFront = !isBringFront;
      updateBringFrontState();
      updateSelectedPageThreshold(thresholdId, "bringToFront", isBringFront);
    });
  }
  if (advancedToggleBtn) {
    advancedToggleBtn.addEventListener("click", function () {
      isAdvancedOpen = !isAdvancedOpen;
      thresholdAdvancedOpen[thresholdId] = isAdvancedOpen;
      updateAdvancedState();
    });
  }
  bgInput.addEventListener("change", function (e) { updateSelectedPageThreshold(thresholdId, "backgroundColor", e.target.value); });
  fgInput.addEventListener("change", function (e) { updateSelectedPageThreshold(thresholdId, "foregroundColor", e.target.value); });
  hlInput.addEventListener("change", function (e) { updateSelectedPageThreshold(thresholdId, "highlightColor", e.target.value); });
  vtInput.addEventListener("change", function (e) { updateSelectedPageThreshold(thresholdId, "valueTextColor", e.target.value); });
  if (tcInput) tcInput.addEventListener("change", function (e) { updateSelectedPageThreshold(thresholdId, "textColor", e.target.value); });
  if (moveUpBtn) moveUpBtn.addEventListener("click", function () { reorderSelectedPageThreshold(thresholdId, "up"); });
  if (moveDownBtn) moveDownBtn.addEventListener("click", function () { reorderSelectedPageThreshold(thresholdId, "down"); });
  clone.querySelector(".threshold-remove").addEventListener("click", function () {
    removeThresholdFromSelectedPage(thresholdId);
    wrapper.remove();
  });

  return clone;
}

function readingForPage(page) {
  if (!page) return null;
  return (currentCatalog.readings || []).find(function (reading) {
    return reading.sensorUid === page.sensorUid && String(reading.id) === String(page.readingId);
  }) || null;
}

function activeGlobalThresholdsForPage(page) {
  var reading = readingForPage(page);
  var readingType = reading ? reading.type || "" : "";
  return (globalThresholds || []).filter(function (gt) {
    return !gt.readingType || gt.readingType === readingType;
  });
}

function setGlobalSuppressed(page, id, suppressed) {
  if (!page) return;
  var refs = Array.isArray(page.suppressedGlobalIDs) ? page.suppressedGlobalIDs : [];
  var has = refs.indexOf(id) !== -1;
  if (suppressed && !has) refs = refs.concat([id]);
  if (!suppressed && has) refs = refs.filter(function (candidate) { return candidate !== id; });
  page.suppressedGlobalIDs = refs;
}

function renderActiveGlobals() {
  var container = document.querySelector("#globalRefsContainer");
  if (!container) return;
  var section = document.querySelector("#globalThresholdsSection");
  var page = selectedPage();
  container.innerHTML = "";
  if (!page) {
    if (section) section.hidden = true;
    return;
  }

  var active = activeGlobalThresholdsForPage(page);
  if (active.length === 0) {
    if (section) section.hidden = true;
    return;
  }
  if (section) section.hidden = false;

  active.forEach(function (gt) {
    var suppressed = page.suppressedGlobalIDs.indexOf(gt.id) !== -1;
    var row = document.createElement("div");
    row.className = "sdpi-item";
    var label = document.createElement("div");
    label.className = "sdpi-item-label";
    label.textContent = gt.name || gt.id;
    var valCell = document.createElement("div");
    valCell.className = "sdpi-item-value";
    valCell.style.cssText = "display:flex;align-items:center;gap:4px;";
    var span = document.createElement("span");
    span.style.color = "#888";
    span.style.fontSize = "9pt";
    span.textContent = (gt.operator || ">=") + " " + (gt.value != null ? gt.value : "");
    var btn = document.createElement("button");
    btn.style.cssText = "width:50px;padding:0;background:" + (suppressed ? "#a44" : "#4a4") + ";color:#fff;";
    btn.textContent = suppressed ? "off" : "on";
    btn.title = suppressed ? "Click to enable for this page" : "Click to disable for this page";
    btn.addEventListener("click", function () {
      var selected = selectedPage();
      if (!selected) return;
      var isSuppressed = selected.suppressedGlobalIDs.indexOf(gt.id) !== -1;
      setGlobalSuppressed(selected, gt.id, !isSuppressed);
      saveSettings();
    });
    valCell.appendChild(span);
    valCell.appendChild(btn);
    row.appendChild(label);
    row.appendChild(valCell);
    container.appendChild(row);
  });
}

function bindSelectedPageSelection() {
  var search = document.getElementById("pageSensorSearch");
  var category = document.getElementById("pageSensorCategoryFilter");
  var sensor = document.getElementById("pageSensorSelect");
  var reading = document.getElementById("pageReadingSelect");
  if (search && !search.dataset.bound) {
    search.dataset.bound = "1";
    search.addEventListener("input", renderSelectedPageSelection);
  }
  if (category && !category.dataset.bound) {
    category.dataset.bound = "1";
    category.addEventListener("change", renderSelectedPageSelection);
  }
  if (sensor && !sensor.dataset.bound) {
    sensor.dataset.bound = "1";
    sensor.addEventListener("change", function () {
      clearBulkPreview();
      pageSelectionDraft.sensorUid = sensor.value;
      pageSelectionDraft.readingId = "";
      populateSelectedPageReadings();
    });
  }
  if (reading && !reading.dataset.bound) {
    reading.dataset.bound = "1";
    reading.addEventListener("change", function () {
      clearBulkPreview();
      pageSelectionDraft.readingId = String(reading.value || "");
      updatePageButtons();
    });
  }
}

function pageFromReading(sensorUid, reading, index, title) {
  var pageColors = dialDefaultPageColors(index);
  return normalizePage({
    sensorUid: sensorUid,
    readingId: String(reading.id),
    readingLabel: reading.label,
    title: title || reading.label,
    min: 0,
    max: 0,
    format: "",
    divisor: "",
    graphUnit: "",
    isValid: true,
    titleColor: "#b7b7b7",
    foregroundColor: pageColors.foregroundColor,
    backgroundColor: "#000000",
    highlightColor: pageColors.highlightColor,
    valueTextColor: "#ffffff",
    graphMode: "both",
    graphHeightPct: 100,
    graphLineThickness: 1,
    thresholds: [],
    suppressedGlobalIDs: [],
    snoozeDurations: [],
    currentThresholdId: "",
    textStroke: false,
    textStrokeColor: "#000000"
  });
}

function selectedDraftReading() {
  var sensorSel = document.getElementById("pageSensorSelect");
  var readingSel = document.getElementById("pageReadingSelect");
  if (!sensorSel || !readingSel) return null;
  var sensorUid = pageSelectionDraft.sensorUid || sensorSel.value;
  var readingId = pageSelectionDraft.readingId || readingSel.value;
  var reading = readingsForSensor(sensorUid).find(function (item) { return String(item.id) === String(readingId); });
  return reading ? { sensorUid: sensorUid, reading: reading } : null;
}

function addSelectedPage(event) {
  if (event && typeof event.preventDefault === "function") event.preventDefault();
  if (event && typeof event.stopPropagation === "function") event.stopPropagation();
  var selected = selectedDraftReading();
  if (!selected) return;
  currentSettings.pages.push(pageFromReading(selected.sensorUid, selected.reading, currentSettings.pages.length));
  currentSettings.activeIndex = currentSettings.pages.length - 1;
  saveSettings();
}

function bindAddPageControl() {
  var addBtn = document.getElementById("addPageBtn");
  if (!addBtn || addBtn.dataset.bound) return;
  addBtn.dataset.bound = "1";
  addBtn.addEventListener("click", addSelectedPage);
}

function sensorByUid(uid) {
  return (currentCatalog.sensors || []).find(function (sensor) {
    return sensor.uid === uid;
  }) || null;
}

function bulkMatchText(value) {
  return String(value || "").trim().replace(/\s+/g, " ").toLowerCase();
}

function bulkNumberedReadingStem(label) {
  var normalized = bulkMatchText(label);
  var match = normalized.match(/^(.*?)(?:\s*#\s*|\s+)(\d+)$/);
  if (!match) return "";
  return match[1].trim();
}

function sameBulkField(left, right) {
  var l = bulkMatchText(left);
  var r = bulkMatchText(right);
  return !l || !r || l === r;
}

// Category of a reading (cpu, gpu, memory, disk, network, ...). Used to scope
// "this reading on matching sensors" to the same KIND of sensor (disks↔disks).
function readingCategory(reading, sensorUid) {
  if (reading && reading.category) return bulkMatchText(reading.category);
  var sensor = sensorByUid(sensorUid || (reading && reading.sensorUid));
  return bulkMatchText((sensor && sensor.category) || "other");
}

function bulkCandidateKey(sensorUid, readingId) {
  return sensorUid + ":" + String(readingId);
}

function existingBulkPageKeys() {
  var seen = Object.create(null);
  (currentSettings.pages || []).forEach(function (page) {
    if (!page || !page.sensorUid || page.readingId == null) return;
    seen[bulkCandidateKey(page.sensorUid, page.readingId)] = true;
  });
  return seen;
}

// Three concrete bulk rules (the issuer's spec): all readings on the seed's sensor;
// the whole numbered family on that sensor (all cores / all fans); the same reading
// on all matching (same-category) sensors. Each INCLUDES the seed. Readings already
// on this dial are still LISTED (so the skip is visible) but flagged duplicate, so the
// preview shows them greyed-out and deselected instead of silently dropping them.
function buildBulkCandidates(rule) {
  var selected = selectedDraftReading();
  if (!selected) return [];
  var seed = selected.reading;
  var seedSensor = selected.sensorUid;
  var seedCat = readingCategory(seed, seedSensor);
  var existing = existingBulkPageKeys();
  var batchSeen = Object.create(null);
  var candidates = [];
  function addCandidate(sensorUid, reading) {
    var key = bulkCandidateKey(sensorUid, reading.id);
    if (batchSeen[key]) return; // never list the same reading twice within one rule
    batchSeen[key] = true;
    var sensor = sensorByUid(sensorUid) || {};
    candidates.push({
      sensorUid: sensorUid,
      reading: reading,
      duplicate: !!existing[key], // already a page on this dial — show it, don't add it
      label: (sensor.name || sensorUid) + " — " + (reading.label || reading.id)
    });
  }
  var readings = currentCatalog.readings || [];

  if (rule === "numbered-family") {
    var stem = bulkNumberedReadingStem(seed.label);
    if (!stem) return [];
    var family = readings.filter(function (r) {
      return r.sensorUid === seedSensor && // cores/fans live on the one sensor
        bulkNumberedReadingStem(r.label) === stem &&
        sameBulkField(seed.type, r.type) && sameBulkField(seed.unit, r.unit);
    });
    if (family.length < 2) return []; // a "set" needs more than just the seed
    family.forEach(function (r) { addCandidate(r.sensorUid, r); });
    return candidates;
  }

  if (rule === "matching-category") {
    var matched = readings.filter(function (r) {
      return readingCategory(r, r.sensorUid) === seedCat && // same KIND of sensor
        bulkMatchText(r.label) === bulkMatchText(seed.label) &&
        sameBulkField(seed.type, r.type) && sameBulkField(seed.unit, r.unit);
    });
    var sensorsSeen = Object.create(null);
    matched.forEach(function (r) { sensorsSeen[r.sensorUid] = 1; });
    if (Object.keys(sensorsSeen).length < 2) return []; // needs >1 matching sensor
    matched.forEach(function (r) { addCandidate(r.sensorUid, r); });
    return candidates;
  }

  // "sensor-all": every reading on the seed's sensor.
  readingsForSensor(seedSensor).forEach(function (r) {
    addCandidate(seedSensor, r);
  });
  return candidates;
}

function bulkRuleValue() {
  var sel = document.getElementById("bulkRule");
  return sel && sel.value ? sel.value : "sensor-all";
}

// Fill the preview from the selected rule. When a rule yields nothing, say so plainly
// (a disabled line) instead of a silent empty list, so "no matches" never reads as
// "broken".
function generateBulkPreview() {
  var candidates = buildBulkCandidates(bulkRuleValue());
  renderBulkPreview(candidates);
  if (candidates.length === 0) {
    var list = document.getElementById("bulkPreviewList");
    if (list) {
      var hint = document.createElement("option");
      hint.disabled = true;
      hint.selected = false; // not a choice — just a readable, un-highlighted note
      hint.textContent = "No matching readings";
      list.appendChild(hint);
    }
  }
}

function renderBulkPreview(candidates) {
  bulkPreviewCandidates = candidates || [];
  var list = document.getElementById("bulkPreviewList");
  if (!list) return;
  list.innerHTML = "";
  var template = bulkNameFormatValue();
  bulkPreviewCandidates.forEach(function (candidate, index) {
    var opt = document.createElement("option");
    opt.value = String(index);
    // With a name template, preview the RESULTING title so the format is visible
    // before adding; otherwise show the sensor/reading identity.
    var name = template ? (applyBulkNameTemplate(template, candidate) || candidate.label) : candidate.label;
    if (candidate.duplicate) {
      // Already on the dial: list it so the skip is visible, but deselect + disable
      // so it cannot be re-added and reads as a duplicate, not a candidate.
      opt.textContent = name + " (added)";
      opt.selected = false;
      opt.disabled = true;
    } else {
      opt.textContent = name;
      opt.selected = true;
    }
    list.appendChild(opt);
  });
  // Keep the Add button's count in sync as the tester (de)selects candidates.
  if (!list.dataset.countListenerBound) {
    list.addEventListener("change", updateBulkAddButton);
    list.dataset.countListenerBound = "true";
  }
  updateBulkAddButton();
}

// updateBulkAddButton reflects how many pages the bulk-add would create in the
// button label, so the count is visible before committing.
function updateBulkAddButton() {
  var list = document.getElementById("bulkPreviewList");
  var addBtn = document.getElementById("bulkAddBtn");
  if (!addBtn) return;
  var selected = list
    ? Array.from(list.options).filter(function (o) { return o.selected && !o.disabled; }).length
    : 0;
  addBtn.disabled = selected === 0;
  addBtn.textContent = selected ? "Add Selected (" + selected + ")" : "Add Selected";
}

function clearBulkPreview() {
  renderBulkPreview([]);
}

// Cross-sensor matches (e.g. "This reading across sensors") all share the same
// reading label by definition, so bare labels would render N identical pages
// ("Read", "Read", "Read"). Qualify with the sensor name when the match is on a
// different sensor than the seed — skipping it when the label already names the
// sensor, to avoid doubling ("GPU GPU Core").
// bulkReadingNumber pulls the trailing index out of a numbered reading label
// ("CPU Core #11" -> "11"), so a name template can render just the number.
function bulkReadingNumber(label) {
  var m = String(label || "").match(/(\d+)\s*$/);
  return m ? m[1] : "";
}

// bulkNameFormatValue is the user's optional title template for bulk-added pages.
function bulkNameFormatValue() {
  var el = document.getElementById("bulkNameFormat");
  return el && el.value ? el.value : "";
}

// applyBulkNameTemplate expands the supported tokens against one candidate. Empty
// template -> "" so callers fall back to the default title logic.
//   %n = number in the reading label, %r = full reading label, %s = sensor name
function applyBulkNameTemplate(template, candidate) {
  if (!template) return "";
  var reading = candidate.reading || {};
  var label = reading.label || String(reading.id || "");
  var sensor = sensorByUid(candidate.sensorUid) || {};
  return template
    .replace(/%n/g, bulkReadingNumber(label))
    .replace(/%r/g, label)
    .replace(/%s/g, sensor.name || candidate.sensorUid || "")
    .trim();
}

function bulkPageTitle(candidate, seedSensorUid, qualifyAll) {
  // A non-empty name template wins over the default sensor/reading title logic.
  var templated = applyBulkNameTemplate(bulkNameFormatValue(), candidate);
  if (templated) return templated;
  var label = candidate.reading.label || String(candidate.reading.id);
  // Same-label-across-sensors adds must qualify EVERY page (incl. the seed sensor) so
  // they stay distinguishable; same-sensor adds keep their already-distinct labels.
  if (!candidate.sensorUid || (!qualifyAll && candidate.sensorUid === seedSensorUid)) return label;
  var sensor = sensorByUid(candidate.sensorUid);
  var sensorName = (sensor && sensor.name) || candidate.sensorUid;
  if (bulkMatchText(label).indexOf(bulkMatchText(sensorName)) !== -1) return label;
  return sensorName + " " + label;
}

function addBulkSelectedPages() {
  var list = document.getElementById("bulkPreviewList");
  if (!list) return;
  var selected = Array.from(list.options).filter(function (opt) { return opt.selected; });
  if (selected.length === 0) return;
  var firstIndex = currentSettings.pages.length;
  var seedSensorUid = pageSelectionDraft.sensorUid || "";
  // The "matching sensors" rule adds the same label across sensors, so every page
  // must be sensor-qualified to stay distinguishable.
  var qualifyAll = bulkRuleValue() === "matching-category";
  var seen = existingBulkPageKeys();
  var added = 0;
  selected.forEach(function (opt) {
    var candidate = bulkPreviewCandidates[parseInt(opt.value, 10)];
    if (!candidate) return;
    var key = bulkCandidateKey(candidate.sensorUid, candidate.reading.id);
    if (seen[key]) return;
    seen[key] = true;
    var title = bulkPageTitle(candidate, seedSensorUid, qualifyAll);
    currentSettings.pages.push(pageFromReading(candidate.sensorUid, candidate.reading, currentSettings.pages.length, title));
    added++;
  });
  if (added > 0) {
    currentSettings.activeIndex = firstIndex;
    saveSettings();
  }
  renderBulkPreview([]);
}

function bindBulkControls() {
  var rule = document.getElementById("bulkRule");
  var previewBtn = document.getElementById("bulkPreviewBtn");
  var addBtn = document.getElementById("bulkAddBtn");
  if (rule && !rule.dataset.bound) {
    rule.dataset.bound = "1";
    rule.addEventListener("change", clearBulkPreview);
  }
  if (previewBtn && !previewBtn.dataset.bound) {
    previewBtn.dataset.bound = "1";
    previewBtn.addEventListener("click", function (e) {
      e.preventDefault();
      generateBulkPreview();
    });
  }
  if (addBtn && !addBtn.dataset.bound) {
    addBtn.dataset.bound = "1";
    addBtn.addEventListener("click", function (e) {
      e.preventDefault();
      addBulkSelectedPages();
    });
  }
  var nameFormat = document.getElementById("bulkNameFormat");
  if (nameFormat && !nameFormat.dataset.bound) {
    nameFormat.dataset.bound = "1";
    // Re-render the preview names live as the template is edited (only when a
    // preview is showing), so the user sees the resulting titles immediately.
    nameFormat.addEventListener("input", function () {
      if (bulkPreviewCandidates && bulkPreviewCandidates.length) renderBulkPreview(bulkPreviewCandidates);
    });
  }
}

function removeSelectedPage() {
  var idx = selectedPageIndex();
  if (idx < 0 || idx >= currentSettings.pages.length) return;
  currentSettings.pages.splice(idx, 1);
  if (currentSettings.activeIndex >= currentSettings.pages.length) {
    currentSettings.activeIndex = Math.max(0, currentSettings.pages.length - 1);
  }
  saveSettings();
}

function moveSelectedPage(delta) {
  var idx = selectedPageIndex();
  var next = idx + delta;
  if (idx < 0 || next < 0 || next >= currentSettings.pages.length) return;
  var page = currentSettings.pages[idx];
  currentSettings.pages[idx] = currentSettings.pages[next];
  currentSettings.pages[next] = page;
  currentSettings.activeIndex = next;
  saveSettings();
}

document.addEventListener("DOMContentLoaded", function () {
  bindAddPageControl();
  document.getElementById("removePageBtn").addEventListener("click", removeSelectedPage);
  document.getElementById("moveUpBtn").addEventListener("click", function () { moveSelectedPage(-1); });
  document.getElementById("moveDownBtn").addEventListener("click", function () { moveSelectedPage(1); });
  document.getElementById("pageList").addEventListener("change", function () {
    currentSettings.activeIndex = selectedPageIndex();
    saveSettings();
    renderPageSettings();
  });
  bindDialSettings();
  bindPageSettings();
  bindSnoozeControls();
  bindThresholdControls();
  bindSelectedPageSelection();
  bindBulkControls();
});
