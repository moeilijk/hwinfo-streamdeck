// Settings Property Inspector for the HWiNFO settings tile
var websocket = null,
  uuid = null,
  action = "com.moeilijk.hwinfo.settings",
  actionInfo = {},
  inInfo = {},
  context = null,
  appearanceSignature = null,
  appearancePollTimer = null,
  statusPollTimer = null,
  uiBound = false;
var allowedIntervals = [250, 500, 1000, 2000, 5000, 10000];
var globalThresholds = [];
var globalThresholdAdvancedOpen = {};

function parseJSONOrEmpty(raw) {
  if (!raw || typeof raw !== "string") {
    return {};
  }
  try {
    return JSON.parse(raw);
  } catch (_err) {
    return {};
  }
}

function byId(id) {
  return document.getElementById(id);
}

function normalizeInterval(value) {
  var v = parseInt(value, 10);
  if (isNaN(v)) {
    return 1000;
  }
  for (var i = 0; i < allowedIntervals.length; i++) {
    if (allowedIntervals[i] === v) {
      return v;
    }
  }
  // Map unknown values to the nearest supported option.
  var nearest = allowedIntervals[0];
  var nearestDiff = Math.abs(v - nearest);
  for (var j = 1; j < allowedIntervals.length; j++) {
    var diff = Math.abs(v - allowedIntervals[j]);
    if (diff < nearestDiff) {
      nearest = allowedIntervals[j];
      nearestDiff = diff;
    }
  }
  return nearest;
}

function sdkContext() {
  // Property Inspector must use its own registration UUID as context.
  // Using the action context causes Stream Deck to reject messages as "wrong context".
  return uuid || context;
}

function normalizeHex(value, fallback) {
  if (typeof value !== "string") {
    return fallback;
  }
  var v = value.trim().toLowerCase();
  if (!/^#[0-9a-f]{6}$/.test(v)) {
    return fallback;
  }
  return v;
}

function readTileSettingsFromUI() {
  var bg = byId("tileBackground");
  var text = byId("tileTextColor");
  var label = byId("showLabel");
  if (!bg || !text || !label) {
    return {
      tileBackground: "#000000",
      tileTextColor: "#ffffff",
      showLabel: true
    };
  }
  return {
    tileBackground: normalizeHex(bg.value, "#000000"),
    tileTextColor: normalizeHex(text.value, "#ffffff"),
    showLabel: label.checked === true
  };
}

function tileSettingsSignature(settings) {
  return [
    settings.tileBackground,
    settings.tileTextColor,
    settings.showLabel ? "1" : "0"
  ].join("|");
}

function applyTileSettingsToUI(settings) {
  var bgEl = byId("tileBackground");
  var textEl = byId("tileTextColor");
  var showEl = byId("showLabel");
  if (!bgEl || !textEl || !showEl) {
    return;
  }

  var bg = normalizeHex(settings.tileBackground, "#000000");
  var text = normalizeHex(settings.tileTextColor, "#ffffff");
  var show = settings.showLabel !== undefined ? settings.showLabel === true : true;

  if (bgEl.value !== bg) {
    bgEl.value = bg;
  }
  if (textEl.value !== text) {
    textEl.value = text;
  }
  if (showEl.checked !== show) {
    showEl.checked = show;
  }

  appearanceSignature = tileSettingsSignature(readTileSettingsFromUI());
}

function sendJson(payload) {
  if (!websocket || websocket.readyState !== 1) {
    return false;
  }
  websocket.send(JSON.stringify(payload));
  return true;
}

function requestSettingsStatus(fullRefresh) {
  var ctx = sdkContext();
  if (!ctx) {
    return;
  }
  var payload = fullRefresh ? { settingsConnected: true } : { requestSettingsStatus: true };
  sendJson({
    action: action,
    event: "sendToPlugin",
    context: ctx,
    payload: payload
  });
}

function connectElgatoStreamDeckSocket(inPort, inUUID, inRegisterEvent, inInfo, inActionInfo) {
  uuid = inUUID;
  actionInfo = parseJSONOrEmpty(inActionInfo);
  if (actionInfo["action"]) {
    action = actionInfo["action"];
  }
  context = actionInfo["context"];
  if (!context && actionInfo["payload"]) {
    context = actionInfo["payload"]["context"];
  }

  // Fallback to query string values if PI host uses URL params.
  try {
    var qs = new URLSearchParams(window.location.search);
    if (!context) {
      context = qs.get("context");
    }
    if (!action) {
      action = qs.get("action") || "com.moeilijk.hwinfo.settings";
    }
  } catch (_err) {
    // Ignore URL parsing errors in constrained PI runtimes.
  }
  if (!context) {
    context = uuid;
  }
  inInfo = parseJSONOrEmpty(inInfo);
  bindUIHandlers();
  websocket = new WebSocket("ws://" + ((typeof location !== "undefined" && location.hostname) ? location.hostname : "127.0.0.1") + ":" + inPort);

  websocket.onopen = function () {
    // Register with Stream Deck
    sendJson({
      event: inRegisterEvent,
      uuid: inUUID,
    });

    // Request global settings
    sendJson({
      event: "getGlobalSettings",
      context: inUUID,
    });

    // Request action settings (for tile appearance).
    var ctx = sdkContext();
    if (ctx) {
      sendJson({
        event: "getSettings",
        context: ctx,
      });
    }

    // Notify plugin that settings PI is connected
    requestSettingsStatus(true);
    if (statusPollTimer !== null) {
      window.clearInterval(statusPollTimer);
    }
    statusPollTimer = window.setInterval(function() {
      requestSettingsStatus(false);
    }, 1000);

    // Keep saving resilient even if some WebView color events are missed.
    if (appearancePollTimer !== null) {
      window.clearInterval(appearancePollTimer);
    }
    appearancePollTimer = window.setInterval(function() {
      var current = readTileSettingsFromUI();
      if (tileSettingsSignature(current) !== appearanceSignature) {
        saveTileSettings("poll");
      }
    }, 300);
  };

  websocket.onmessage = function (evt) {
    var jsonObj = parseJSONOrEmpty(evt.data);
    var event = jsonObj["event"];

    // Handle global settings received (poll interval + source profiles + global thresholds)
    if (event === "didReceiveGlobalSettings") {
      var settings = {};
      if (jsonObj.payload && jsonObj.payload.settings) {
        settings = jsonObj.payload.settings;
      }
      var interval = normalizeInterval(settings.pollInterval || 1000);
      var pollEl = byId("pollInterval");
      var rateEl = byId("currentRate");
      if (pollEl) {
        pollEl.value = interval;
      }
      if (rateEl) {
        rateEl.textContent = interval + "ms";
      }
      if (Array.isArray(settings.globalThresholds)) {
        globalThresholds = settings.globalThresholds;
        renderGlobalThresholds(globalThresholds);
      }
    }

    // Handle action settings received (tile appearance)
    if (event === "didReceiveSettings") {
      var settings = {};
      if (jsonObj.payload && jsonObj.payload.settings) {
        settings = jsonObj.payload.settings;
      }
      applyTileSettingsToUI(settings);
    }

    // Handle status updates from plugin
    if (event === "sendToPropertyInspector") {
      var payload = jsonObj.payload || {};
      if (Array.isArray(payload.globalThresholds)) {
        globalThresholds = payload.globalThresholds;
        renderGlobalThresholds(globalThresholds);
      }
      if (payload.connectionStatus !== undefined) {
        var statusEl = byId("connectionStatus");
        if (statusEl) {
          statusEl.textContent = payload.connectionStatus;
          statusEl.style.color = payload.connectionStatus === "Connected" ? "#4a4" : "#a44";
        }
      }
      if (payload.currentRate !== undefined) {
        var currentRateEl = byId("currentRate");
        if (currentRateEl) {
          currentRateEl.textContent = payload.currentRate + "ms";
        }
      }
    }
  };

  websocket.onclose = function () {
    if (appearancePollTimer !== null) {
      window.clearInterval(appearancePollTimer);
      appearancePollTimer = null;
    }
    if (statusPollTimer !== null) {
      window.clearInterval(statusPollTimer);
      statusPollTimer = null;
    }
  };
}

// Save tile appearance settings
function saveTileSettings(reason) {
  if (!websocket || websocket.readyState !== 1) {
    return;
  }
  var ctx = sdkContext();
  if (!ctx) {
    return;
  }

  var settings = readTileSettingsFromUI();
  var sig = tileSettingsSignature(settings);
  if (reason !== "force" && sig === appearanceSignature) {
    return;
  }
  appearanceSignature = sig;

  // Persist action settings.
  sendJson({
    event: "setSettings",
    context: ctx,
    payload: settings
  });

  // Trigger immediate redraw path.
  sendJson({
    action: action,
    event: "sendToPlugin",
    context: ctx,
    payload: {
      updateTileAppearance: settings
    }
  });
}

function scheduleTileSettingsSave() {
  window.setTimeout(function() {
    saveTileSettings("ui");
  }, 0);
}

// Expose handlers for inline PI control attributes.
window.saveTileSettings = saveTileSettings;
window.scheduleTileSettingsSave = scheduleTileSettingsSave;

function isActivelyEditing(el) {
  return !!el && document.activeElement === el;
}

function applyInputValue(el, value) {
  if (!el || isActivelyEditing(el)) {
    return;
  }
  var next = value === undefined || value === null ? "" : String(value);
  if (el.value !== next) {
    el.value = next;
  }
}

function bindUIHandlers() {
  if (uiBound) {
    return;
  }

  var pollEl = byId("pollInterval");
  var bgEl = byId("tileBackground");
  var textEl = byId("tileTextColor");
  var showEl = byId("showLabel");
  if (!pollEl || !bgEl || !textEl || !showEl) {
    return;
  }

  pollEl.addEventListener("change", function(e) {
    if (!websocket || websocket.readyState !== 1) {
      return;
    }
    var interval = normalizeInterval(e.target.value);
    e.target.value = interval;

    sendJson({
      event: "setGlobalSettings",
      context: uuid,
      payload: { pollInterval: interval }
    });

    var rateEl = byId("currentRate");
    if (rateEl) {
      rateEl.textContent = interval + "ms";
    }

    sendJson({
      action: action,
      event: "sendToPlugin",
      context: sdkContext(),
      payload: {
        setPollInterval: interval
      }
    });
  });

  bgEl.addEventListener("change", scheduleTileSettingsSave);
  bgEl.addEventListener("input", scheduleTileSettingsSave);
  textEl.addEventListener("change", scheduleTileSettingsSave);
  textEl.addEventListener("input", scheduleTileSettingsSave);
  showEl.addEventListener("change", scheduleTileSettingsSave);
  showEl.addEventListener("input", scheduleTileSettingsSave);
  showEl.addEventListener("click", scheduleTileSettingsSave);

  bindGlobalThresholdControls();
  appearanceSignature = tileSettingsSignature(readTileSettingsFromUI());
  uiBound = true;
}

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", bindUIHandlers);
} else {
  bindUIHandlers();
}

// --- Global threshold library ---

function sendGlobalThresholdUpdate(id, field, value, checked) {
  var upd = { id: id, field: field, value: String(value) };
  if (typeof checked === "boolean") upd.checked = checked;
  sendJson({
    action: action,
    event: "sendToPlugin",
    context: sdkContext(),
    payload: { updateGlobalThreshold: upd }
  });
}

function renderGlobalThresholds(thresholds) {
  var container = byId("globalThresholdsContainer");
  if (!container) return;
  var list = thresholds || [];

  var existingItems = container.querySelectorAll(".threshold-item");
  var existingIds = Array.prototype.map.call(existingItems, function(el) { return el.dataset.thresholdId; });
  var incomingIds = list.map(function(t) { return t.id; });

  if (JSON.stringify(existingIds) !== JSON.stringify(incomingIds)) {
    container.innerHTML = "";
    list.forEach(function(t) { container.appendChild(createGlobalThresholdElement(t)); });
    return;
  }

  var active = document.activeElement;
  list.forEach(function(t) {
    var item = container.querySelector('.threshold-item[data-threshold-id="' + t.id + '"]');
    if (!item || (active && item.contains(active))) return;
    applyInputValue(item.querySelector(".threshold-name"), t.name || "");
    applyInputValue(item.querySelector(".threshold-text"), t.text || "");
    applyInputValue(item.querySelector(".threshold-value"), t.value != null ? t.value : "");
    applyInputValue(item.querySelector(".threshold-hysteresis"), t.hysteresis != null ? t.hysteresis : "");
    applyInputValue(item.querySelector(".threshold-dwell"), t.dwellMs != null ? t.dwellMs : "");
    applyInputValue(item.querySelector(".threshold-cooldown"), t.cooldownMs != null ? t.cooldownMs : "");
  });
}

function createGlobalThresholdElement(threshold) {
  var template = document.querySelector("#globalThresholdTemplate");
  if (!template) return document.createDocumentFragment();
  var clone = template.content.cloneNode(true);
  var wrapper = clone.querySelector(".threshold-item");
  wrapper.dataset.thresholdId = threshold.id;

  var nameInput = clone.querySelector(".threshold-name");
  nameInput.value = threshold.name || "";

  var textInput = clone.querySelector(".threshold-text");
  textInput.value = threshold.text || "";

  var readingTypeSelect = clone.querySelector(".threshold-reading-type");
  if (readingTypeSelect) readingTypeSelect.value = threshold.readingType || "";

  var operatorSelect = clone.querySelector(".threshold-operator");
  operatorSelect.value = threshold.operator || ">=";

  var valueInput = clone.querySelector(".threshold-value");
  valueInput.value = threshold.value != null ? threshold.value : "";

  var hysteresisInput = clone.querySelector(".threshold-hysteresis");
  hysteresisInput.value = threshold.hysteresis != null ? threshold.hysteresis : "";

  var dwellInput = clone.querySelector(".threshold-dwell");
  dwellInput.value = threshold.dwellMs != null ? threshold.dwellMs : "";

  var cooldownInput = clone.querySelector(".threshold-cooldown");
  cooldownInput.value = threshold.cooldownMs != null ? threshold.cooldownMs : "";

  var bgInput = clone.querySelector(".threshold-bg");
  bgInput.value = threshold.backgroundColor || "#333300";
  var fgInput = clone.querySelector(".threshold-fg");
  fgInput.value = threshold.foregroundColor || "#999900";
  var hlInput = clone.querySelector(".threshold-hl");
  hlInput.value = threshold.highlightColor || "#ffff00";
  var vtInput = clone.querySelector(".threshold-vt");
  vtInput.value = threshold.valueTextColor || "#ffff00";
  var tcInput = clone.querySelector(".threshold-tc");
  if (tcInput) tcInput.value = threshold.textColor || "#ffffff";

  var toggleBtn = clone.querySelector(".threshold-toggle");
  var settingsDiv = clone.querySelector(".threshold-settings");
  var stickyBtn = clone.querySelector(".threshold-sticky-toggle");
  var advancedToggleBtn = clone.querySelector(".threshold-advanced-toggle");
  var advancedPanel = clone.querySelector(".threshold-advanced-panel");

  var isEnabled = threshold.enabled !== false;
  var isSticky = threshold.sticky === true;
  var isAdvancedOpen = globalThresholdAdvancedOpen[threshold.id] === true;
  var thresholdId = threshold.id;

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
  function updateAdvancedState() {
    if (!advancedToggleBtn || !advancedPanel) return;
    advancedToggleBtn.textContent = isAdvancedOpen ? "Advanced ▼" : "Advanced ▶";
    advancedPanel.style.display = isAdvancedOpen ? "block" : "none";
  }
  updateToggleState();
  updateStickyState();
  updateAdvancedState();

  toggleBtn.addEventListener("click", function() {
    isEnabled = !isEnabled;
    updateToggleState();
    sendGlobalThresholdUpdate(thresholdId, "thresholdEnabled", isEnabled ? "true" : "false", isEnabled);
  });

  var nameTimeout;
  nameInput.addEventListener("input", function(e) {
    clearTimeout(nameTimeout);
    nameTimeout = setTimeout(function() {
      sendGlobalThresholdUpdate(thresholdId, "thresholdName", e.target.value);
    }, 300);
  });

  var textTimeout;
  textInput.addEventListener("input", function(e) {
    clearTimeout(textTimeout);
    textTimeout = setTimeout(function() {
      sendGlobalThresholdUpdate(thresholdId, "thresholdText", e.target.value);
    }, 300);
  });

  if (readingTypeSelect) {
    readingTypeSelect.addEventListener("change", function(e) {
      sendGlobalThresholdUpdate(thresholdId, "thresholdReadingType", e.target.value);
    });
  }

  operatorSelect.addEventListener("change", function(e) {
    sendGlobalThresholdUpdate(thresholdId, "thresholdOperator", e.target.value);
  });

  var valueTimeout;
  valueInput.addEventListener("input", function(e) {
    clearTimeout(valueTimeout);
    valueTimeout = setTimeout(function() {
      sendGlobalThresholdUpdate(thresholdId, "thresholdValue", e.target.value);
    }, 300);
  });

  var hysteresisTimeout;
  hysteresisInput.addEventListener("input", function(e) {
    clearTimeout(hysteresisTimeout);
    hysteresisTimeout = setTimeout(function() {
      sendGlobalThresholdUpdate(thresholdId, "thresholdHysteresis", e.target.value);
    }, 300);
  });

  var dwellTimeout;
  dwellInput.addEventListener("input", function(e) {
    clearTimeout(dwellTimeout);
    dwellTimeout = setTimeout(function() {
      sendGlobalThresholdUpdate(thresholdId, "thresholdDwellMs", e.target.value);
    }, 300);
  });

  var cooldownTimeout;
  cooldownInput.addEventListener("input", function(e) {
    clearTimeout(cooldownTimeout);
    cooldownTimeout = setTimeout(function() {
      sendGlobalThresholdUpdate(thresholdId, "thresholdCooldownMs", e.target.value);
    }, 300);
  });

  if (stickyBtn) {
    stickyBtn.addEventListener("click", function() {
      isSticky = !isSticky;
      updateStickyState();
      sendGlobalThresholdUpdate(thresholdId, "thresholdSticky", isSticky ? "true" : "false", isSticky);
    });
  }

  if (advancedToggleBtn) {
    advancedToggleBtn.addEventListener("click", function() {
      isAdvancedOpen = !isAdvancedOpen;
      globalThresholdAdvancedOpen[thresholdId] = isAdvancedOpen;
      updateAdvancedState();
    });
  }

  bgInput.addEventListener("change", function(e) { sendGlobalThresholdUpdate(thresholdId, "thresholdBackgroundColor", e.target.value); });
  fgInput.addEventListener("change", function(e) { sendGlobalThresholdUpdate(thresholdId, "thresholdForegroundColor", e.target.value); });
  hlInput.addEventListener("change", function(e) { sendGlobalThresholdUpdate(thresholdId, "thresholdHighlightColor", e.target.value); });
  vtInput.addEventListener("change", function(e) { sendGlobalThresholdUpdate(thresholdId, "thresholdValueTextColor", e.target.value); });
  if (tcInput) {
    tcInput.addEventListener("change", function(e) { sendGlobalThresholdUpdate(thresholdId, "thresholdTextColor", e.target.value); });
  }

  var removeBtn = clone.querySelector(".threshold-remove");
  removeBtn.addEventListener("click", function() {
    delete globalThresholdAdvancedOpen[thresholdId];
    sendJson({
      action: action,
      event: "sendToPlugin",
      context: sdkContext(),
      payload: { deleteGlobalThreshold: thresholdId }
    });
    wrapper.remove();
  });

  return clone;
}

function bindGlobalThresholdControls() {
  var addBtn = byId("addGlobalThresholdBtn");
  if (addBtn && !addBtn.dataset.bound) {
    addBtn.dataset.bound = "1";
    addBtn.addEventListener("click", function(e) {
      e.preventDefault();
      e.stopPropagation();
      var nameEl = byId("newGlobalThresholdName");
      var name = nameEl ? nameEl.value.trim() : "";
      sendJson({
        action: action,
        event: "sendToPlugin",
        context: sdkContext(),
        payload: { addGlobalThreshold: name }
      });
      if (nameEl) nameEl.value = "";
    });
  }
}
