var websocket = null,
  uuid = null,
  actionInfo = {},
  allSensors = [],
  allSources = [],
  allPresets = [],
  allFavorites = [],
  currentSettings = {},
  globalThresholds = [];

var onchangeevt = "onchange";

function updateRangeVal(id) {
  var inp = document.getElementById(id) || document.querySelector("#" + id + " input[type=range]");
  if (inp) positionRangeVal(inp);
}

function wireRangeVal(id) {
  var el = document.getElementById(id);
  var inp = (el && el.type === "range") ? el : (el && el.querySelector("input[type=range]")) || document.querySelector("#" + id + " input[type=range]");
  if (inp) inp.oninput = function() { positionRangeVal(this); };
}

function connectElgatoStreamDeckSocket(inPort, inUUID, inRegisterEvent, inInfo, inActionInfo) {
  uuid = inUUID;
  actionInfo = JSON.parse(inActionInfo);
  websocket = new WebSocket("ws://" + ((typeof location !== "undefined" && location.hostname) ? location.hostname : "127.0.0.1") + ":" + inPort);

  websocket.onopen = function () {
    websocket.send(JSON.stringify({ event: inRegisterEvent, uuid: inUUID }));
    websocket.send(JSON.stringify({ event: "getGlobalSettings", context: inUUID }));
    sendValueToPlugin("propertyInspectorConnected", "property_inspector");
  };

  websocket.onmessage = function (evt) {
    var jsonObj = JSON.parse(evt.data);
    var event = jsonObj["event"];

    // Global settings (presets storage)
    if (event === "didReceiveGlobalSettings") {
      var gs = (jsonObj.payload && jsonObj.payload.settings) ? jsonObj.payload.settings : {};
      allPresets = Array.isArray(gs.derivedPresets) ? gs.derivedPresets : [];
      populatePresetSelect(allPresets);
      return;
    }

    if (event !== "sendToPropertyInspector") return;
    var payload = jsonObj.payload || {};

    // Error state
    if (typeof payload.error === "boolean") {
      document.querySelector("#ui").style.display = payload.error ? "none" : "";
      document.querySelector("#error").style.display = payload.error ? "block" : "none";
      if (!payload.error && payload.message === "show_ui") {
        sendValueToPlugin("propertyInspectorConnected", "property_inspector");
      }
      return;
    }

    // Favorites list
    if (Array.isArray(payload.favorites)) {
      allFavorites = payload.favorites;
      for (var i = 0; i < 8; i++) {
        populateFavoriteSelect(i, allFavorites);
      }
    }


    // Sensor sources (local + remote machines); must land before the
    // sensor list so the Source filter applies to this payload's sensors.
    if (Array.isArray(payload.sources)) {
      allSources = payload.sources;
      renderSourceFilter();
    }

    // Sensor list
    if (Array.isArray(payload.sensors)) {
      allSensors = payload.sensors;
      for (var i = 0; i < 8; i++) {
        populateSensorSelect(i, allSensors);
      }
      populateAllSlotsSensorSelect(allSensors);
    }

    // Global threshold library updates
    if (Array.isArray(payload.globalThresholds)) {
      globalThresholds = payload.globalThresholds;
      renderDerivedActiveGlobals();
    }

    // Full settings object — must come before populateReadingSelect so currentSettings is current
    if (payload.derivedSettings) {
      currentSettings = payload.derivedSettings;
      applySettingsToUI(currentSettings);
      renderDerivedActiveGlobals();
    }

    // Readings for a specific slot
    if (Array.isArray(payload.readings) && typeof payload.slotIndex === "number") {
      populateReadingSelect(payload.slotIndex, payload.readings);
    }
  };
}

function sendValueToPlugin(value, event) {
  if (!websocket || websocket.readyState !== 1) return;
  websocket.send(JSON.stringify({
    event: "sendToPlugin",
    context: uuid,
    action: actionInfo.action,
    payload: { [event]: value }
  }));
}

function sendSdpi(key, value) {
  sendValueToPlugin({ key: key, value: String(value) }, "sdpi_collection");
}

function sendSdpiChecked(key, checked) {
  sendValueToPlugin({ key: key, value: "", checked: checked }, "sdpi_collection");
}

// --- favorites ---

function populateFavoriteSelect(slotIdx, favorites) {
  var el = byId("slot" + slotIdx + "_favoriteSelect");
  if (!el) return;
  while (el.options.length) el.remove(0);
  var ph = document.createElement("option");
  ph.value = "";
  ph.text = favorites.length ? "— from favorite —" : "— no favorites saved —";
  ph.disabled = true;
  ph.selected = true;
  el.add(ph);
  favorites.forEach(function (fav) {
    var opt = document.createElement("option");
    opt.value = fav.id;
    var cat = fav.category ? fav.category.toUpperCase() + " — " : "";
    opt.text = cat + fav.sensorName + " — " + fav.readingLabel;
    el.add(opt);
  });
}

// --- sensor / reading dropdowns ---

function sourceOfUid(uid) {
  var i = (uid || "").indexOf("::");
  return i >= 0 ? uid.slice(0, i) : "";
}

// The Source filter only appears when the bridge reports more than one
// source (i.e. HWiNFO monitors remote machines). It filters the sensor
// dropdowns of all slots; the stored uids carry the source themselves.
function selectedSourceFilter() {
  if (!allSources || allSources.length <= 1) return null;
  var el = byId("sourceFilter");
  return el ? el.value : null;
}

function firstConfiguredSource() {
  var slots = currentSettings.slots || [];
  for (var i = 0; i < slots.length; i++) {
    if (slots[i] && slots[i].sensorUid) return sourceOfUid(slots[i].sensorUid);
  }
  return "";
}

function renderSourceFilter() {
  var row = byId("sourceFilterRow");
  var heading = byId("sourceHeading");
  var el = byId("sourceFilter");
  if (!row || !el) return;

  var multi = allSources && allSources.length > 1;
  row.style.display = multi ? "" : "none";
  if (heading) heading.style.display = multi ? "" : "none";
  if (!multi) return;

  var selected = el.dataset.userChoice !== undefined ? el.value : firstConfiguredSource();

  while (el.options.length) el.remove(0);
  allSources.forEach(function (src) {
    var opt = document.createElement("option");
    opt.value = src.id;
    opt.text = src.name;
    if (src.id === selected) opt.selected = true;
    el.add(opt);
  });

  if (!el.dataset.bound) {
    el.dataset.bound = "1";
    el.addEventListener("change", function () {
      el.dataset.userChoice = "1";
      for (var i = 0; i < 8; i++) {
        populateSensorSelect(i, allSensors);
      }
      populateAllSlotsSensorSelect(allSensors);
    });
  }
}

function applySourceFilterToSensors(sorted, currentUid) {
  var source = selectedSourceFilter();
  if (source === null) return sorted;
  return sorted.filter(function (sensor) {
    return (sensor.source || "") === source || sensor.uid === currentUid;
  });
}

function populateSensorSelect(slotIdx, sensors) {
  var el = byId("slot" + slotIdx + "_sensorSelect");
  if (!el) return;
  var currentUid = (currentSettings.slots && currentSettings.slots[slotIdx])
    ? currentSettings.slots[slotIdx].sensorUid : "";
  var sorted = applySourceFilterToSensors(sensors.slice().sort(function (a, b) {
    return a.name > b.name ? 1 : a.name < b.name ? -1 : 0;
  }), currentUid);

  while (el.options.length) el.remove(0);
  var ph = document.createElement("option");
  ph.text = "Choose a sensor";
  ph.disabled = true;
  if (!currentUid) ph.selected = true;
  el.add(ph);

  sorted.forEach(function (sensor) {
    var opt = document.createElement("option");
    opt.text = sensor.name;
    opt.value = sensor.uid;
    if (sensor.uid === currentUid) opt.selected = true;
    el.add(opt);
  });
  el.removeAttribute("disabled");
}

function populateAllSlotsSensorSelect(sensors) {
  var el = byId("allSlots_sensorSelect");
  if (!el) return;
  var sorted = applySourceFilterToSensors(sensors.slice().sort(function (a, b) {
    return a.name > b.name ? 1 : a.name < b.name ? -1 : 0;
  }), "");

  while (el.options.length) el.remove(0);
  var ph = document.createElement("option");
  ph.value = "";
  ph.text = "Set all slots to…";
  ph.disabled = true;
  ph.selected = true;
  el.add(ph);

  sorted.forEach(function (sensor) {
    var opt = document.createElement("option");
    opt.text = sensor.name;
    opt.value = sensor.uid;
    el.add(opt);
  });
  el.removeAttribute("disabled");
}

function populateReadingSelect(slotIdx, readings) {
  var el = byId("slot" + slotIdx + "_readingSelect");
  if (!el) return;
  var slot = currentSettings.slots ? currentSettings.slots[slotIdx] : null;
  var currentRid = slot ? String(slot.readingId) : "";

  while (el.options.length) el.remove(0);
  var ph = document.createElement("option");
  ph.text = "Choose a reading";
  ph.disabled = true;
  if (!slot || !slot.isValid) ph.selected = true;
  el.add(ph);

  readings.slice().sort(compareReadings).forEach(function (r) {
    var opt = document.createElement("option");
    opt.text = readingOptionLabel(r);
    opt.value = String(r.id);
    if (String(r.id) === currentRid) opt.selected = true;
    el.add(opt);
  });
  el.removeAttribute("disabled");
}

// --- presets ---

function populatePresetSelect(presets) {
  var el = byId("preset_load");
  if (!el) return;
  while (el.options.length) el.remove(0);
  var ph = document.createElement("option");
  ph.value = "";
  ph.text = "— select preset —";
  ph.disabled = true;
  ph.selected = true;
  el.add(ph);
  presets.forEach(function (p) {
    var opt = document.createElement("option");
    opt.text = p.name;
    opt.value = p.name;
    el.add(opt);
  });
}

function saveCurrentAsPreset(name) {
  if (!name) return;
  var slots = [];
  for (var i = 0; i < 8; i++) {
    var s = (currentSettings.slots && currentSettings.slots[i]) ? currentSettings.slots[i] : {};
    slots.push({
      sensorUid:    s.sensorUid    || "",
      readingId:    s.readingId    || 0,
      readingLabel: s.readingLabel || "",
      isValid:      s.isValid      || false,
      divisor:      s.divisor      || "",
      graphUnit:    s.graphUnit    || ""
    });
  }
  var preset = {
    name:      name,
    formula:   currentSettings.formula   || "sum",
    slotCount: currentSettings.slotCount || 2,
    slots:     slots
  };
  var updated = allPresets.filter(function (p) { return p.name !== name; });
  updated.push(preset);
  allPresets = updated;
  populatePresetSelect(allPresets);
  if (websocket && websocket.readyState === 1) {
    websocket.send(JSON.stringify({
      event: "setGlobalSettings",
      context: uuid,
      payload: { derivedPresets: allPresets }
    }));
  }
}

function loadPresetByName(name) {
  var preset = null;
  for (var i = 0; i < allPresets.length; i++) {
    if (allPresets[i].name === name) { preset = allPresets[i]; break; }
  }
  if (!preset) return;
  // Update local state so applySettingsToUI works when the backend responds
  currentSettings.formula   = preset.formula;
  currentSettings.slotCount = preset.slotCount;
  currentSettings.slots     = preset.slots;
  sendValueToPlugin({
    formula:   preset.formula,
    slotCount: preset.slotCount,
    slots:     preset.slots
  }, "loadDerivedPreset");
  // Reset the load select back to placeholder
  var el = byId("preset_load");
  if (el) el.selectedIndex = 0;
}

// --- populate all UI fields from settings ---

function applySettingsToUI(s) {
  var formula = s.formula || "sum";
  var slotCount = s.slotCount || 2;
  setInputValue("titleFontSize", s.titleFontSize != null ? s.titleFontSize : 10.5);
  updateRangeVal("titleFontSize");
  setInputValue("valueFontSize", s.valueFontSize != null ? s.valueFontSize : 10.5);
  updateRangeVal("valueFontSize");
  setSelectValue("derived_formula", formula);
  setSelectValue("derived_slotCount", String(slotCount));
  updateSlotCountForFormula(formula);
  updateSlotVisibility(slotCount);

  setColorValue("derived_highlightColor", s.highlightColor);
  setColorValue("derived_foregroundColor", s.foregroundColor);
  setColorValue("derived_backgroundColor", s.backgroundColor);
  setColorValue("derived_valueTextColor", s.valueTextColor);
  setColorValue("derived_titleColor", s.titleColor);
  setInputValue("derived_graphHeightPct", s.graphHeightPct || 100);
  updateRangeVal("derived_graphHeightPct");
  setInputValue("derived_graphLineThickness", s.graphLineThickness || 1);
  updateRangeVal("derived_graphLineThickness");
  var tsDerived = byId("derived_textStroke");
  if (tsDerived) tsDerived.checked = s.textStroke === true;
  setColorValue("derived_textStrokeColor", s.textStrokeColor || "#000000");
  setSelectValue("derived_updateIntervalOverrideMs", String(s.updateIntervalOverrideMs || 0));
  var saInp = byId("derived_smoothingAlpha") && byId("derived_smoothingAlpha").querySelector("input[type=range]");
  if (saInp) { saInp.value = s.smoothingAlpha > 0 ? s.smoothingAlpha : 1; positionRangeVal(saInp); }
  setInputValue("derived_min", s.min != null ? s.min : "");
  setInputValue("derived_max", s.max != null ? s.max : "");
  setInputValue("derived_format", s.format || "");
  setInputValue("derived_divisor", s.divisor || "");
  setSelectValue("derived_graphUnit", s.graphUnit || "");

  var slots = s.slots || [];
  for (var i = 0; i < 8; i++) {
    var slot = slots[i] || {};
    setInputValue("slot" + i + "_divisor", slot.divisor || "");
    setSelectValue("slot" + i + "_graphUnit", slot.graphUnit || "");

    if (allSensors.length > 0) {
      setSelectValue("slot" + i + "_sensorSelect", slot.sensorUid || "");
    }
  }
}

// --- slot count locking ---

function updateSlotCountForFormula(formula) {
  var el = byId("derived_slotCount");
  if (!el) return;
  if (formula === "delta") {
    el.value = "2";
    el.style.pointerEvents = "none";
    el.style.opacity = "0.5";
    updateSlotVisibility(2);
    sendSdpi("derived_slotCount", "2");
  } else {
    el.style.pointerEvents = "";
    el.style.opacity = "";
  }
}

// --- slot visibility ---

function updateSlotVisibility(count) {
  for (var i = 0; i < 8; i++) {
    var sec = document.getElementById("slot-section-" + i);
    if (sec) sec.style.display = i < count ? "" : "none";
  }
}

// --- wire up all events after DOM ready ---

document.addEventListener("DOMContentLoaded", function () {
  // Presets
  var presetLoad = byId("preset_load");
  if (presetLoad) {
    presetLoad[onchangeevt] = function () {
      if (presetLoad.value) loadPresetByName(presetLoad.value);
    };
  }
  var presetSaveas = byId("preset_saveas");
  if (presetSaveas) {
    presetSaveas.onchange = function () {
      var name = presetSaveas.value.trim();
      if (name) {
        saveCurrentAsPreset(name);
        presetSaveas.value = "";
      }
    };
  }

  // All-slots sensor select
  var allSlotsSel = byId("allSlots_sensorSelect");
  if (allSlotsSel) {
    allSlotsSel[onchangeevt] = function () {
      if (!allSlotsSel.value) return;
      sendSdpi("allSlots_sensorSelect", allSlotsSel.value);
      // Reset to placeholder after triggering
      allSlotsSel.selectedIndex = 0;
    };
  }

  // Tile-wide
  bindSdpiValue("titleFontSize", sendSdpi, onchangeevt);
  wireRangeVal("titleFontSize");
  bindSdpiValue("valueFontSize", sendSdpi, onchangeevt);
  wireRangeVal("valueFontSize");
  bindSdpiValue("derived_formula", sendSdpi, onchangeevt, function (val) {
    updateSlotCountForFormula(val);
  });
  bindSdpiValue("derived_slotCount", sendSdpi, onchangeevt, function (val) {
    updateSlotVisibility(parseInt(val, 10));
  });
  bindSdpiValue("derived_highlightColor", sendSdpi, onchangeevt);
  bindSdpiValue("derived_foregroundColor", sendSdpi, onchangeevt);
  bindSdpiValue("derived_backgroundColor", sendSdpi, onchangeevt);
  bindSdpiValue("derived_valueTextColor", sendSdpi, onchangeevt);
  bindSdpiValue("derived_titleColor", sendSdpi, onchangeevt);
  bindSdpiValue("derived_min", sendSdpi, "onchange");
  bindSdpiValue("derived_max", sendSdpi, "onchange");
  bindSdpiValue("derived_format", sendSdpi, "onchange");
  bindSdpiValue("derived_divisor", sendSdpi, "onchange");
  bindSdpiValue("derived_graphUnit", sendSdpi, onchangeevt);
  bindSdpiValue("derived_graphHeightPct", sendSdpi, onchangeevt);
  wireRangeVal("derived_graphHeightPct");
  bindSdpiValue("derived_graphLineThickness", sendSdpi, onchangeevt);
  wireRangeVal("derived_graphLineThickness");
  var tsDerivedEl = byId("derived_textStroke");
  if (tsDerivedEl) tsDerivedEl.onchange = function() { sendSdpiChecked("derived_textStroke", this.checked); };
  bindSdpiValue("derived_textStrokeColor", sendSdpi, onchangeevt);
  bindSdpiValue("derived_updateIntervalOverrideMs", sendSdpi, onchangeevt);
  (function() {
    var inp = document.querySelector("#derived_smoothingAlpha input[type=range]");
    if (inp) {
      inp.oninput = function() { positionRangeVal(this); };
      inp.onchange = function() { sendSdpi("derived_smoothingAlpha", this.value); };
    }
  })();

  for (var i = 0; i < 8; i++) {
    wireFavoriteSelect(i);
    wireSensorSelect(i);
    wireReadingSelect(i);
    bindSdpiValue("slot" + i + "_divisor", sendSdpi, "onchange");
    bindSdpiValue("slot" + i + "_graphUnit", sendSdpi, onchangeevt);
  }
});

function wireFavoriteSelect(slotIdx) {
  bindValueChange("slot" + slotIdx + "_favoriteSelect", onchangeevt, function (value, el) {
    if (!value) return;
    sendSdpi("slot" + slotIdx + "_applyFavorite", value);
    el.selectedIndex = 0;
  });
}

function wireSensorSelect(slotIdx) {
  bindValueChange("slot" + slotIdx + "_sensorSelect", onchangeevt, function (value) {
    sendSdpi("slot" + slotIdx + "_sensorSelect", value);
  });
}

function wireReadingSelect(slotIdx) {
  bindValueChange("slot" + slotIdx + "_readingSelect", onchangeevt, function (value) {
    sendSdpi("slot" + slotIdx + "_readingSelect", value);
  });
}

// --- active global thresholds for derived tile ---
// Derived tiles aggregate multiple sensor types, so only "all types" globals (readingType="") are shown.

function renderDerivedActiveGlobals() {
  var container = byId("derivedGlobalRefsContainer");
  if (!container) return;
  container.innerHTML = "";
  var active = (globalThresholds || []).filter(function(gt) { return !gt.readingType; });
  if (active.length === 0) return;
  var suppressed = Array.isArray(currentSettings.suppressedGlobalIDs) ? currentSettings.suppressedGlobalIDs : [];
  active.forEach(function(gt) {
    var isSuppressed = suppressed.indexOf(gt.id) !== -1;
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
    btn.style.cssText = "width:50px;padding:0;background:" + (isSuppressed ? "#a44" : "#4a4") + ";color:#fff;";
    btn.textContent = isSuppressed ? "off" : "on";
    (function(gtId) {
      btn.addEventListener("click", function() {
        var sups = Array.isArray(currentSettings.suppressedGlobalIDs) ? currentSettings.suppressedGlobalIDs.slice() : [];
        var idx = sups.indexOf(gtId);
        if (idx !== -1) {
          sups.splice(idx, 1);
          sendSdpi("derived_unsuppressGlobal", gtId);
        } else {
          sups.push(gtId);
          sendSdpi("derived_suppressGlobal", gtId);
        }
        currentSettings.suppressedGlobalIDs = sups;
        var nowSuppressed = sups.indexOf(gtId) !== -1;
        btn.style.background = nowSuppressed ? "#a44" : "#4a4";
        btn.textContent = nowSuppressed ? "off" : "on";
      });
    })(gt.id);
    valCell.appendChild(span);
    valCell.appendChild(btn);
    row.appendChild(label);
    row.appendChild(valCell);
    container.appendChild(row);
  });
}
