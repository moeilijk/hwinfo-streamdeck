// this is our global websocket, used to communicate from/to Stream Deck software
// and some info about our plugin, as sent by Stream Deck software
var websocket = null,
  uuid = null,
  actionInfo = {},
  inInfo = {},
  runningApps = [],
  currentSensors = [],
  currentSensorSettings = {},
  currentCatalog = null,
  currentReadings = [], // store readings to look up unit when selection changes
  thresholdAdvancedOpen = {},
  thresholdSignature = null,
  catalogControlsInitialized = false,
  snoozeControlsInitialized = false,
  isQT = navigator.appVersion.includes("QtWebEngine"),
  onchangeevt = "onchange"; // 'oninput'; // change this, if you want interactive elements act on any change, or while they're modified

var globalThresholds = [];
var currentSuppressedGlobalIDs = [];
var currentReadingType = "";

var snoozeDurationOptions = [300000, 900000, 3600000, 0];

function connectElgatoStreamDeckSocket(inPort, inUUID, inRegisterEvent, inInfo, inActionInfo) {
  uuid = inUUID;
  // please note: the incoming arguments are of type STRING, so
  // in case of the inActionInfo, we must parse it into JSON first
  actionInfo = JSON.parse(inActionInfo); // cache the info
  inInfo = JSON.parse(inInfo);
  websocket = new WebSocket("ws://" + ((typeof location !== "undefined" && location.hostname) ? location.hostname : "127.0.0.1") + ":" + inPort);

  /** Since the PI doesn't have access to native settings
   * Stream Deck sends some color settings to PI
   * We use these to adjust some styles (e.g. highlight-colors for checkboxes)
   */
  addDynamicStyles(inInfo.colors, "connectSocket");
  initPropertyInspector(5);

  // if connection was established, the websocket sends
  // an 'onopen' event, where we need to register our PI
  websocket.onopen = function () {
    var json = {
      event: inRegisterEvent,
      uuid: inUUID,
    };
    // register property inspector to Stream Deck
    websocket.send(JSON.stringify(json));
    sendValueToPlugin("propertyInspectorConnected", "property_inspector");
  };

  websocket.onmessage = function (evt) {
    // Received message from Stream Deck
    var jsonObj = JSON.parse(evt.data);
    var event = jsonObj["event"];
    if (
      "boolean" === typeof getPropFromString(jsonObj, "payload.error") &&
      event === "sendToPropertyInspector"
    ) {
      if (jsonObj.payload.error === true) {
        document.querySelector("#ui").style = "display:none";
        document.querySelector("#error").style = "display:block";
      } else if (jsonObj.payload.message === "show_ui") {
        document.querySelector("#ui").style = "display:block";
        document.querySelector("#error").style = "display:none";
        sendValueToPlugin("propertyInspectorConnected", "property_inspector");
      }
    }
    if (
      getPropFromString(jsonObj, "payload.sensors") &&
      event === "sendToPropertyInspector"
    ) {
      addSensors(
        document.querySelector("#sensorSelect"),
        jsonObj.payload.sensors,
        jsonObj.payload.settings
      );
    }
    if (
      getPropFromString(jsonObj, "payload.catalog") &&
      event === "sendToPropertyInspector"
    ) {
      currentCatalog = jsonObj.payload.catalog;
      renderFavoriteControls();
    }
    if (
      getPropFromString(jsonObj, "payload.readings") &&
      event === "sendToPropertyInspector"
    ) {
      addReadings(
        document.querySelector("#readingSelect"),
        jsonObj.payload.readings,
        jsonObj.payload.settings
      );
    }
    // Handle threshold updates from plugin (after add/remove)
    if (
      getPropFromString(jsonObj, "payload.thresholds") &&
      event === "sendToPropertyInspector"
    ) {
      maybeRenderThresholds(jsonObj.payload.thresholds, true);
    }
    // Handle global threshold list updates
    if (
      Array.isArray(getPropFromString(jsonObj, "payload.globalThresholds")) &&
      event === "sendToPropertyInspector"
    ) {
      globalThresholds = jsonObj.payload.globalThresholds;
      renderActiveGlobals();
    }
    if (getPropFromString(jsonObj, "payload.settings")) {
      var settings = jsonObj.payload.settings;
      currentSensorSettings = settings || {};
      if (currentSensors.length > 0) {
        renderSensorOptions(false);
      }
      if (settings.min === 0 && settings.max === 0) {
        // don't show 0, 0 min/max
      } else {
        document.querySelector("#min").value = settings.min;
        document.querySelector("#max").value = settings.max;
      }
      document.querySelector("#format input").value = settings.format;
      document.querySelector("#divisor input").value = settings.divisor || "";
      if (
        settings.format.length > 0 ||
        (settings.divisor && settings.divisor.length > 0)
      ) {
        var attr = document.createAttribute("open");
        attr.value = "open";
        document
          .querySelector("#advanced_details")
          .attributes.setNamedItem(attr);
      }
      if (settings.foregroundColor !== "") {
        document.querySelector("#foreground").value = settings.foregroundColor;
      }
      if (settings.backgroundColor !== "") {
        document.querySelector("#background").value = settings.backgroundColor;
      }
      if (settings.highlightColor !== "") {
        document.querySelector("#highlight").value = settings.highlightColor;
      }
      if (settings.valueTextColor !== "") {
        document.querySelector("#valuetext").value = settings.valueTextColor;
      }
      if (settings.titleFontSize !== "") {
        var tfsInp = document.querySelector("#titleFontSize input[type=range]");
        if (tfsInp) { tfsInp.value = settings.titleFontSize || 10.5; positionRangeVal(tfsInp); }
      }
      if (settings.valueFontSize !== "") {
        var vfsInp = document.querySelector("#valueFontSize input[type=range]");
        if (vfsInp) { vfsInp.value = settings.valueFontSize || 10.5; positionRangeVal(vfsInp); }
      }
      setSelectValue("graphMode", settings.graphMode || "both");
      var ghpInp = document.querySelector("#graphHeightPct input[type=range]");
      if (ghpInp) { ghpInp.value = settings.graphHeightPct || 100; positionRangeVal(ghpInp); }
      var gltInp = document.querySelector("#graphLineThickness input[type=range]");
      if (gltInp) { gltInp.value = settings.graphLineThickness || 1; positionRangeVal(gltInp); }
      var tsEl = document.querySelector("#textStroke");
      if (tsEl) { tsEl.checked = settings.textStroke === true; }
      var tscEl = document.querySelector("#textStrokeColor");
      if (tscEl && settings.textStrokeColor) { tscEl.value = settings.textStrokeColor; }
      setSelectValue("updateIntervalOverrideMs", String(settings.updateIntervalOverrideMs || 0));
      var saInp = document.querySelector("#smoothingAlpha input[type=range]");
      if (saInp) { saInp.value = settings.smoothingAlpha > 0 ? settings.smoothingAlpha : 1; positionRangeVal(saInp); }
      if (settings.graphUnit !== undefined) {
        document.querySelector("#graphUnit").value = settings.graphUnit;
      }
      applySnoozeDurationsToUI(settings);
      // Render dynamic thresholds
      if (settings.thresholds) {
        maybeRenderThresholds(settings.thresholds, false);
      }
      currentSuppressedGlobalIDs = Array.isArray(settings.suppressedGlobalIDs) ? settings.suppressedGlobalIDs : [];
      renderActiveGlobals();
      renderFavoriteControls();
    }
  };
}

function sortBy(key) {
  return function (a, b) {
    if (a[key] > b[key]) return 1;
    if (b[key] > a[key]) return -1;
    return 0;
  };
}

function sensorMatchesFilter(sensor, term, category) {
  var searchText = (sensor.searchText || `${sensor.name || ""} ${sensor.category || ""}`).toLowerCase();
  var sensorCategory = (sensor.category || "other").toLowerCase();
  if (category && sensorCategory !== category) {
    return false;
  }
  if (!term) {
    return true;
  }
  return searchText.includes(term);
}

function renderSensorOptions(triggerSelectionChange) {
  var el = document.querySelector("#sensorSelect");
  if (!el) {
    return;
  }

  var i;
  for (i = el.options.length - 1; i >= 0; i--) {
    el.remove(i);
  }

  el.removeAttribute("disabled");

  var searchInput = document.querySelector("#sensorSearch");
  var categorySelect = document.querySelector("#sensorCategoryFilter");
  var term = searchInput ? searchInput.value.trim().toLowerCase() : "";
  var category = categorySelect ? categorySelect.value.trim().toLowerCase() : "";
  var settings = currentSensorSettings || {};
  var sensors = (currentSensors || []).slice().sort(sortBy("name"));
  var filteredSensors = sensors.filter(function(sensor) {
    return sensorMatchesFilter(sensor, term, category);
  });
  if (settings.sensorUid && !filteredSensors.some(function(sensor) {
    return sensor.uid === settings.sensorUid;
  })) {
    sensors.forEach(function(sensor) {
      if (sensor.uid === settings.sensorUid) {
        filteredSensors.unshift(sensor);
      }
    });
  }

  var option = document.createElement("option");
  option.text = "Choose a sensor";
  option.disabled = true;
  if (settings.isValid !== true || !filteredSensors.some(function(sensor) {
    return sensor.uid === settings.sensorUid;
  })) {
    option.selected = true;
  }
  el.add(option);

  if (filteredSensors.length === 0) {
    option = document.createElement("option");
    option.text = "No sensors match";
    option.disabled = true;
    el.add(option);
    return;
  }

  filteredSensors.forEach(function(sensor) {
    option = document.createElement("option");
    option.text = sensor.name;
    option.value = sensor.uid;
    option.dataset.category = sensor.category || "";
    if (settings.sensorUid === sensor.uid) {
      option.selected = true;
      if (triggerSelectionChange) {
        setTimeout(function () {
          var event = new Event("change");
          el.dispatchEvent(event);
        }, 0);
      }
    }
    el.add(option);
  });
}

function addSensors(el, sensors, settings) {
  currentSensors = Array.isArray(sensors) ? sensors.slice() : [];
  currentSensorSettings = settings || {};
  renderSensorOptions(true);
  renderFavoriteControls();
}

function addReadings(el, readings, settings) {
  var i;
  for (i = el.options.length - 1; i >= 0; i--) {
    el.remove(i);
  }

  // Store readings globally for unit lookup
  currentReadings = readings;
  // Derive current reading type for active globals display
  // r.id is serialized as string (json:"id,string"); readingId is a number — compare loosely
  var matchedR = readings.find(function(r) { return String(r.id) === String(currentSensorSettings.readingId); });
  currentReadingType = normalizeReadingType(matchedR ? matchedR.type : "");
  renderActiveGlobals();

  el.removeAttribute("disabled");

  var option = document.createElement("option");
  option.text = "Choose a reading";
  option.disabled = true;
  if (settings.isValid !== true) {
    option.selected = true;
  }
  el.add(option);

  var sortedReadings = readings.slice().sort(compareReadings);
  sortedReadings.forEach((r) => {
    var option = document.createElement("option");
    option.textContent = readingOptionLabel(r);
    option.value = r.id;
    option.dataset.unit = r.unit || r.prefix; // store unit in data attribute
    if (settings.readingId === r.id) {
      option.selected = true;
      // Show/hide graphUnit based on selected reading
      updateGraphUnitVisibility(r.unit || r.prefix);
    }
    el.add(option);
  });

  if (!el.dataset.unitListenerBound) {
    el.addEventListener("change", function() {
      var selectedOption = el.options[el.selectedIndex];
      if (selectedOption && selectedOption.dataset.unit) {
        updateGraphUnitVisibility(selectedOption.dataset.unit);
      }
      renderFavoriteControls();
    });
    el.dataset.unitListenerBound = "true";
  }

  renderFavoriteControls();
}

// Show graphUnit only for throughput readings (units containing /s)
function updateGraphUnitVisibility(unit) {
  var container = document.querySelector("#graphUnitContainer");
  if (container) {
    if (unit && unit.includes("/s")) {
      container.style.display = "";
    } else {
      container.style.display = "none";
    }
  }
}

function initPropertyInspector(initDelay) {
  setupCatalogControls();
  bindSnoozeControls();
  wireRangeDisplays();
  prepareDOMElements(document);
}

function wireRangeDisplays() {
  ["titleFontSize", "valueFontSize", "graphHeightPct", "graphLineThickness", "smoothingAlpha"].forEach(function(id) {
    var inp = document.querySelector("#" + id + " input[type=range]");
    if (inp) {
      positionRangeVal(inp);
      inp.oninput = function() { positionRangeVal(this); };
    }
  });
  var textStrokeEl = document.querySelector("#textStroke");
  if (textStrokeEl) {
    textStrokeEl.onchange = function() {
      sendValueToPlugin({ key: "textStroke", value: "", checked: this.checked }, "sdpi_collection");
    };
  }
}

function setupCatalogControls() {
  if (catalogControlsInitialized) {
    return;
  }

  var sensorSearch = document.querySelector("#sensorSearch");
  if (sensorSearch) {
    sensorSearch.oninput = function() {
      renderSensorOptions(false);
    };
  }

  var sensorCategoryFilter = document.querySelector("#sensorCategoryFilter");
  if (sensorCategoryFilter) {
    sensorCategoryFilter.onchange = function() {
      renderSensorOptions(false);
    };
  }

  var favoriteToggleBtn = document.querySelector("#favoriteToggleBtn");
  if (favoriteToggleBtn) {
    favoriteToggleBtn.onclick = function() {
      sendValueToPlugin({
        key: "toggleFavoriteCurrent",
        value: "toggle"
      }, "sdpi_collection");
    };
  }

  var applyFavoriteBtn = document.querySelector("#applyFavoriteBtn");
  if (applyFavoriteBtn) {
    applyFavoriteBtn.onclick = function() {
      var favoriteSelect = document.querySelector("#favoriteSelect");
      if (!favoriteSelect || !favoriteSelect.value) {
        return;
      }
      sendValueToPlugin({
        key: "applyFavorite",
        value: favoriteSelect.value
      }, "sdpi_collection");
    };
  }

  var removeFavoriteBtn = document.querySelector("#removeFavoriteBtn");
  if (removeFavoriteBtn) {
    removeFavoriteBtn.onclick = function() {
      var favoriteSelect = document.querySelector("#favoriteSelect");
      if (!favoriteSelect || !favoriteSelect.value) {
        return;
      }
      sendValueToPlugin({
        key: "removeFavorite",
        value: favoriteSelect.value
      }, "sdpi_collection");
    };
  }

  var favoriteSelect = document.querySelector("#favoriteSelect");
  if (favoriteSelect) {
    favoriteSelect.onchange = function() {
      renderFavoriteControls();
    };
  }

  catalogControlsInitialized = true;
}

function normalizeSnoozeDurations(values) {
  if (!Array.isArray(values)) {
    return [];
  }

  var seen = {};
  values.forEach(function(value) {
    var parsed = parseInt(value, 10);
    if (!isNaN(parsed)) {
      seen[parsed] = true;
    }
  });

  return snoozeDurationOptions.filter(function(value) {
    return seen[value] === true;
  });
}

function readSnoozeDurationsFromUI() {
  var selected = [];
  Array.from(document.querySelectorAll(".snooze-duration")).forEach(function(button) {
    if (button.classList.contains("is-selected")) {
      selected.push(parseInt(button.dataset.value, 10));
    }
  });
  return normalizeSnoozeDurations(selected);
}

function setSnoozePresetSelected(button, selected) {
  if (!button || !button.classList) {
    return;
  }
  button.classList.toggle("is-selected", selected === true);
}

function applySnoozeDurationsToUI(settings) {
  var selected = normalizeSnoozeDurations(settings && settings.snoozeDurations ? settings.snoozeDurations : []);
  var selectedMap = {};
  selected.forEach(function(value) {
    selectedMap[String(value)] = true;
  });

  Array.from(document.querySelectorAll(".snooze-duration")).forEach(function(button) {
    setSnoozePresetSelected(button, selectedMap[button.dataset.value] === true);
  });
}

function bindSnoozeControls() {
  if (snoozeControlsInitialized) {
    return;
  }

  Array.from(document.querySelectorAll(".snooze-duration")).forEach(function(button) {
    button.addEventListener("click", function() {
      setSnoozePresetSelected(button, !button.classList.contains("is-selected"));
      var selection = readSnoozeDurationsFromUI().map(function(value) {
        return String(value);
      });
      sendValueToPlugin({
        key: "snoozeDurations",
        value: selection.join(","),
        selection: selection
      }, "sdpi_collection");
    });
  });

  snoozeControlsInitialized = true;
}

function currentFavoriteSelection() {
  if (!currentCatalog || !Array.isArray(currentCatalog.favorites)) {
    return null;
  }

  var settings = currentSensorSettings || {};
  if (!settings.sensorUid || !settings.readingId) {
    return null;
  }

  for (var i = 0; i < currentCatalog.favorites.length; i++) {
    var favorite = currentCatalog.favorites[i];
    if (
      favorite.sensorUid === settings.sensorUid &&
      String(favorite.readingId) === String(settings.readingId)
    ) {
      return favorite;
    }
  }

  return null;
}

function favoriteOptionLabel(favorite) {
  var category = favorite.category ? favorite.category.toUpperCase() : "OTHER";
  return `${category} - ${favorite.sensorName} - ${favorite.readingLabel}`;
}

function renderFavoriteControls() {
  var toggleBtn = document.querySelector("#favoriteToggleBtn");
  var favoriteSelect = document.querySelector("#favoriteSelect");
  var applyBtn = document.querySelector("#applyFavoriteBtn");
  var removeBtn = document.querySelector("#removeFavoriteBtn");
  if (!toggleBtn || !favoriteSelect || !applyBtn || !removeBtn) {
    return;
  }

  var settings = currentSensorSettings || {};
  var currentFavorite = currentFavoriteSelection();
  var hasValidSelection = !!(settings.isValid && settings.sensorUid && settings.readingId);
  toggleBtn.disabled = !hasValidSelection;
  toggleBtn.textContent = currentFavorite ? "Remove Current" : "Save Current";

  var favorites = currentCatalog && Array.isArray(currentCatalog.favorites)
    ? currentCatalog.favorites.slice()
    : [];
  favorites.sort(function(a, b) {
    var left = favoriteOptionLabel(a).toLowerCase();
    var right = favoriteOptionLabel(b).toLowerCase();
    if (left > right) return 1;
    if (right > left) return -1;
    return 0;
  });

  var previousValue = favoriteSelect.value;
  while (favoriteSelect.options.length > 0) {
    favoriteSelect.remove(0);
  }

  if (favorites.length === 0) {
    var emptyOption = document.createElement("option");
    emptyOption.text = "No favorites saved";
    emptyOption.value = "";
    favoriteSelect.add(emptyOption);
    favoriteSelect.disabled = true;
    applyBtn.disabled = true;
    removeBtn.disabled = true;
    return;
  }

  favoriteSelect.disabled = false;

  var placeholder = document.createElement("option");
  placeholder.text = "Choose favorite";
  placeholder.value = "";
  placeholder.disabled = true;
  favoriteSelect.add(placeholder);

  favorites.forEach(function(favorite) {
    var option = document.createElement("option");
    option.value = favorite.id;
    option.text = favoriteOptionLabel(favorite);
    favoriteSelect.add(option);
  });

  var selectedValue = previousValue;
  if (!selectedValue && currentFavorite) {
    selectedValue = currentFavorite.id;
  }
  favoriteSelect.value = selectedValue && favorites.some(function(favorite) {
    return favorite.id === selectedValue;
  }) ? selectedValue : "";

  applyBtn.disabled = favoriteSelect.value === "";
  removeBtn.disabled = favoriteSelect.value === "";
}

function revealSdpiWrapper() {
  const el = document.querySelector(".sdpi-wrapper");
  el && el.classList.remove("hidden");
}

// openUrl in default browser
function openUrl(url) {
  if (websocket && websocket.readyState === 1) {
    const json = {
      event: "openUrl",
      payload: {
        url: url,
      },
    };
    websocket.send(JSON.stringify(json));
  }
}

// our method to pass values to the plugin
function sendValueToPlugin(value, param) {
  if (websocket && websocket.readyState === 1) {
    const json = {
      action: actionInfo["action"],
      event: "sendToPlugin",
      context: uuid,
      payload: {
        [param]: value,
      },
    };
    websocket.send(JSON.stringify(json));
  }
}

if (!isQT) {
  document.addEventListener("DOMContentLoaded", function () {
    initPropertyInspector(100);
  });
}

/** the beforeunload event is fired, right before the PI will remove all nodes */
window.addEventListener("beforeunload", function (e) {
  e.preventDefault();
  sendValueToPlugin("propertyInspectorWillDisappear", "property_inspector");
  // Don't set a returnValue to the event, otherwise Chromium with throw an error.  // e.returnValue = '';
});

/** CREATE INTERACTIVE HTML-DOM
 * where elements can be clicked or act on their 'change' event.
 * Messages are then processed using the 'handleSdpiItemClick' method below.
 */

function prepareDOMElements(baseElement) {
  baseElement = baseElement || document;
  Array.from(baseElement.querySelectorAll(".sdpi-item-value")).forEach(
    (el, i) => {
      if (el.dataset && el.dataset.localOnly === "true") {
        return;
      }
      const elementsToClick = [
        "BUTTON",
        "OL",
        "UL",
        "TABLE",
        "METER",
        "PROGRESS",
        "CANVAS",
      ].includes(el.tagName);
      const evt = elementsToClick ? "onclick" : onchangeevt || "onchange";

      /** Look for <input><span> combinations, where we consider the span as label for the input
       * we don't use `labels` for that, because a range could have 2 labels.
       */
      const inputGroup = el.querySelectorAll("input, span");
      if (inputGroup.length === 2) {
        const offs = inputGroup[0].tagName === "INPUT" ? 1 : 0;
        inputGroup[offs].innerText = inputGroup[1 - offs].value;
        inputGroup[1 - offs]["oninput"] = function () {
          inputGroup[offs].innerText = inputGroup[1 - offs].value;
        };
      }
      /** We look for elements which have an 'clickable' attribute
       * we use these e.g. on an 'inputGroup' (<span><input type="range"><span>) to adjust the value of
       * the corresponding range-control
       */
      Array.from(el.querySelectorAll(".clickable")).forEach((subel, subi) => {
        subel["onclick"] = function (e) {
          handleSdpiItemClick(e.target, subi);
        };
      });
      el[evt] = function (e) {
        handleSdpiItemClick(e.target, i);
      };
    }
  );

  baseElement.querySelectorAll("textarea").forEach((e) => {
    const maxl = e.getAttribute("maxlength");
    e.targets = baseElement.querySelectorAll(`[for='${e.id}']`);
    if (e.targets.length) {
      let fn = () => {
        for (let x of e.targets) {
          x.innerText = maxl
            ? `${e.value.length}/${maxl}`
            : `${e.value.length}`;
        }
      };
      fn();
      e.onkeyup = fn;
    }
  });

  // Add threshold button handler
  const addThresholdBtn = document.querySelector("#addThresholdBtn");
  if (addThresholdBtn) {
    addThresholdBtn.addEventListener("click", function() {
      const nameInput = document.querySelector("#newThresholdName");
      const name = nameInput.value.trim() || "New Threshold";
      sendValueToPlugin({
        key: "addThreshold",
        value: name
      }, "sdpi_collection");
      nameInput.value = "";
    });
  }

  // Allow Enter key to add threshold
  const newThresholdName = document.querySelector("#newThresholdName");
  if (newThresholdName) {
    newThresholdName.addEventListener("keypress", function(e) {
      if (e.key === "Enter") {
        document.querySelector("#addThresholdBtn").click();
      }
    });
  }
}

function handleSdpiItemClick(e, idx) {
  /** Following items are containers, so we won't handle clicks on them */
  if (["OL", "UL", "TABLE"].includes(e.tagName)) {
    return;
  }
  // console.log('--- handleSdpiItemClick ---', e, `type: ${e.type}`, e.tagName, `inner: ${e.innerText}`);

  /** SPANS are used inside a control as 'labels'
   * If a SPAN element calls this function, it has a class of 'clickable' set and is thereby handled as
   * clickable label.
   */

  if (e.tagName === "SPAN") {
    const inp = e.parentNode.querySelector("input");
    if (e.getAttribute("value")) {
      return inp && (inp.value = e.getAttribute("value"));
    }
  }

  const selectedElements = [];
  const isList = ["LI", "OL", "UL", "DL", "TD"].includes(e.tagName);
  const sdpiItem = e.closest(".sdpi-item");
  const sdpiItemGroup = e.closest(".sdpi-item-group");
  let sdpiItemChildren = isList
    ? sdpiItem.querySelectorAll(e.tagName === "LI" ? "li" : "td")
    : sdpiItem.querySelectorAll(".sdpi-item-child > input");

  if (isList) {
    const siv = e.closest(".sdpi-item-value");
    if (!siv.classList.contains("multi-select")) {
      for (let x of sdpiItemChildren) x.classList.remove("selected");
    }
    if (!siv.classList.contains("no-select")) {
      e.classList.toggle("selected");
    }
  }

  if (sdpiItemGroup && !sdpiItemChildren.length) {
    for (let x of ["input", "meter", "progress"]) {
      sdpiItemChildren = sdpiItemGroup.querySelectorAll(x);
      if (sdpiItemChildren.length) break;
    }
  }

  if (e.selectedIndex) {
    idx = e.selectedIndex;
  } else {
    sdpiItemChildren.forEach((ec, i) => {
      if (ec.classList.contains("selected")) {
        selectedElements.push(ec.innerText);
      }
      if (ec === e) idx = i;
    });
  }

  const returnValue = {
    key: e.id || sdpiItem.id,
    value: isList
      ? e.innerText
      : e.value
        ? e.type === "file"
          ? decodeURIComponent(e.value.replace(/^C:\\fakepath\\/, ""))
          : e.value
        : e.getAttribute("value"),
    group: sdpiItemGroup ? sdpiItemGroup.id : false,
    index: idx,
    selection: selectedElements,
    checked: e.checked,
  };

  /** Just simulate the original file-selector:
   * If there's an element of class '.sdpi-file-info'
   * show the filename there
   */
  if (e.type === "file") {
    const info = sdpiItem.querySelector(".sdpi-file-info");
    if (info) {
      const s = returnValue.value.split("/").pop();
      info.innerText =
        s.length > 28
          ? s.substr(0, 10) + "..." + s.substr(s.length - 10, s.length)
          : s;
    }
  }

  sendValueToPlugin(returnValue, "sdpi_collection");
}

function updateKeyForDemoCanvas(cnv) {
  sendValueToPlugin(
    {
      key: "your_canvas",
      value: cnv.toDataURL(),
    },
    "sdpi_collection"
  );
}

/** Stream Deck software passes system-highlight color information
 * to Property Inspector. Here we 'inject' the CSS styles into the DOM
 * when we receive this information. */

function addDynamicStyles(clrs, fromWhere) {
  const node =
    document.getElementById("#sdpi-dynamic-styles") ||
    document.createElement("style");
  if (!clrs.mouseDownColor)
    clrs.mouseDownColor = fadeColor(clrs.highlightColor, -100);
  const clr = clrs.highlightColor.slice(0, 7);
  const clr1 = fadeColor(clr, 100);
  const clr2 = fadeColor(clr, 60);
  const metersActiveColor = fadeColor(clr, -60);

  node.setAttribute("id", "sdpi-dynamic-styles");
  node.textContent = `

    input[type="radio"]:checked + label span,
    input[type="checkbox"]:checked + label span {
        background-color: ${clrs.highlightColor};
    }

    input[type="radio"]:active:checked + label span,
    input[type="radio"]:active + label span,
    input[type="checkbox"]:active:checked + label span,
    input[type="checkbox"]:active + label span {
      background-color: ${clrs.mouseDownColor};
    }

    input[type="radio"]:active + label span,
    input[type="checkbox"]:active + label span {
      background-color: ${clrs.buttonPressedBorderColor};
    }

    td.selected,
    td.selected:hover,
    li.selected:hover,
    li.selected {
      color: white;
      background-color: ${clrs.highlightColor};
    }

    .sdpi-file-label > label:active,
    .sdpi-file-label.file:active,
    label.sdpi-file-label:active,
    label.sdpi-file-info:active,
    input[type="file"]::-webkit-file-upload-button:active,
    button:active {
      background-color: ${clrs.buttonPressedBackgroundColor};
      color: ${clrs.buttonPressedTextColor};
      border-color: ${clrs.buttonPressedBorderColor};
    }

    ::-webkit-progress-value,
    meter::-webkit-meter-optimum-value {
        background: linear-gradient(${clr2}, ${clr1} 20%, ${clr} 45%, ${clr} 55%, ${clr2})
    }

    ::-webkit-progress-value:active,
    meter::-webkit-meter-optimum-value:active {
        background: linear-gradient(${clr}, ${clr2} 20%, ${metersActiveColor} 45%, ${metersActiveColor} 55%, ${clr})
    }
    `;
  document.body.appendChild(node);
}

/** UTILITIES */

/** Helper function to construct a list of running apps
 * from a template string.
 * -> information about running apps is received from the plugin
 */

function sdpiCreateList(el, obj, cb) {
  if (el) {
    el.style.display = obj.value.length ? "block" : "none";
    Array.from(document.querySelectorAll(`.${el.id}`)).forEach((subel, i) => {
      subel.style.display = obj.value.length ? "flex" : "none";
    });
    if (obj.value.length) {
      // Build DOM safely instead of injecting HTML.
      el.textContent = "";
      const wrapper = document.createElement("div");
      wrapper.className = "sdpi-item";
      if (obj.type) {
        wrapper.className += " " + String(obj.type);
      }
      wrapper.id =
        obj.id || window.btoa(new Date().getTime().toString()).substr(0, 8);

      const label = document.createElement("div");
      label.className = "sdpi-item-label";
      label.textContent = obj.label || "";

      const list = document.createElement("ul");
      list.className = "sdpi-item-value";
      if (obj.selectionType) {
        list.className += " " + String(obj.selectionType);
      }

      obj.value.forEach((e) => {
        const li = document.createElement("li");
        li.textContent = e && e.name ? e.name : "";
        list.appendChild(li);
      });

      wrapper.appendChild(label);
      wrapper.appendChild(list);
      el.appendChild(wrapper);
      setTimeout(function () {
        prepareDOMElements(el);
        if (cb) cb();
      }, 10);
      return;
    }
  }
  if (cb) cb();
}

/** get a JSON property from a (dot-separated) string
 * Works on nested JSON, e.g.:
 * jsn = {
 *      propA: 1,
 *      propB: 2,
 *      propC: {
 *          subA: 3,
 *          subB: {
 *             testA: 5,
 *             testB: 'Hello'
 *          }
 *      }
 *  }
 *  getPropFromString(jsn,'propC.subB.testB') will return 'Hello';
 */
const getPropFromString = (jsn, str, sep = ".") => {
  const arr = str.split(sep);
  return arr.reduce(
    (obj, key) => (obj && obj.hasOwnProperty(key) ? obj[key] : undefined),
    jsn
  );
};

/*
    Quick utility to lighten or darken a color (doesn't take color-drifting, etc. into account)
    Usage:
    fadeColor('#061261', 100); // will lighten the color
    fadeColor('#200867'), -100); // will darken the color
*/
function fadeColor(col, amt) {
  const min = Math.min,
    max = Math.max;
  const num = parseInt(col.replace(/#/g, ""), 16);
  const r = min(255, max((num >> 16) + amt, 0));
  const g = min(255, max((num & 0x0000ff) + amt, 0));
  const b = min(255, max(((num >> 8) & 0x00ff) + amt, 0));
  return "#" + (g | (b << 8) | (r << 16)).toString(16).padStart(6, 0);
}

/** DYNAMIC THRESHOLDS */

// Send a threshold update to the plugin
function sendThresholdUpdate(key, thresholdId, value, checked) {
  var payload = {
    key: key,
    thresholdId: thresholdId,
    value: value
  };
  // For checkbox inputs, include the checked boolean
  if (typeof checked === "boolean") {
    payload.checked = checked;
  }
  sendValueToPlugin(payload, "sdpi_collection");
}

// Render all thresholds from the settings
function renderThresholds(thresholds) {
  const container = document.querySelector("#thresholdsContainer");
  if (!container) return;

  const existingItems = container.querySelectorAll(".threshold-item");
  const existingIds = Array.prototype.map.call(existingItems, function(el) { return el.dataset.thresholdId; });
  const incomingIds = thresholds.map(function(t) { return t.id; });

  if (JSON.stringify(existingIds) !== JSON.stringify(incomingIds)) {
    container.innerHTML = "";
    thresholds.forEach(function(threshold, index) {
      container.appendChild(createThresholdElement(threshold, index, thresholds.length));
    });
    return;
  }

  const active = document.activeElement;
  thresholds.forEach(function(t) {
    const item = container.querySelector('.threshold-item[data-threshold-id="' + t.id + '"]');
    if (!item || (active && item.contains(active))) return;
    const set = function(sel, val) {
      const el = item.querySelector(sel);
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

function thresholdsSignature(thresholds) {
  return JSON.stringify(thresholds.map((t) => ({
    id: t.id,
    enabled: t.enabled,
    name: t.name,
    text: t.text,
    operator: t.operator,
    value: t.value,
    hysteresis: t.hysteresis,
    dwellMs: t.dwellMs,
    cooldownMs: t.cooldownMs,
    sticky: t.sticky,
    backgroundColor: t.backgroundColor,
    foregroundColor: t.foregroundColor,
    highlightColor: t.highlightColor,
    valueTextColor: t.valueTextColor,
    textColor: t.textColor,
  })));
}

function maybeRenderThresholds(thresholds, force) {
  if (!thresholds) return;
  const sig = thresholdsSignature(thresholds);
  if (force || sig !== thresholdSignature) {
    thresholdSignature = sig;
    renderThresholds(thresholds);
  }
}

// Create a threshold element from the template
function createThresholdElement(threshold, index, total) {
  const template = document.querySelector("#thresholdTemplate");
  const clone = template.content.cloneNode(true);
  const wrapper = clone.querySelector(".threshold-item");

  wrapper.dataset.thresholdId = threshold.id;

  // Set values
  const nameInput = clone.querySelector(".threshold-name");
  nameInput.value = threshold.name || "";

  const textInput = clone.querySelector(".threshold-text");
  textInput.value = threshold.text || "";

  const operatorSelect = clone.querySelector(".threshold-operator");
  operatorSelect.value = threshold.operator || ">=";

  const valueInput = clone.querySelector(".threshold-value");
  valueInput.value =
    threshold.value !== undefined && threshold.value !== null ? threshold.value : "";

  const hysteresisInput = clone.querySelector(".threshold-hysteresis");
  hysteresisInput.value =
    threshold.hysteresis !== undefined && threshold.hysteresis !== null
      ? threshold.hysteresis
      : "";

  const dwellInput = clone.querySelector(".threshold-dwell");
  dwellInput.value =
    threshold.dwellMs !== undefined && threshold.dwellMs !== null
      ? threshold.dwellMs
      : "";

  const cooldownInput = clone.querySelector(".threshold-cooldown");
  cooldownInput.value =
    threshold.cooldownMs !== undefined && threshold.cooldownMs !== null
      ? threshold.cooldownMs
      : "";

  const stickyBtn = clone.querySelector(".threshold-sticky-toggle");
  const advancedToggleBtn = clone.querySelector(".threshold-advanced-toggle");
  const advancedPanel = clone.querySelector(".threshold-advanced-panel");

  const bgInput = clone.querySelector(".threshold-bg");
  bgInput.value = threshold.backgroundColor || "#333300";

  const fgInput = clone.querySelector(".threshold-fg");
  fgInput.value = threshold.foregroundColor || "#999900";

  const hlInput = clone.querySelector(".threshold-hl");
  hlInput.value = threshold.highlightColor || "#ffff00";

  const vtInput = clone.querySelector(".threshold-vt");
  vtInput.value = threshold.valueTextColor || "#ffff00";

  const tcInput = clone.querySelector(".threshold-tc");
  if (tcInput) {
    tcInput.value = threshold.textColor || "#ffffff";
  }

  const moveUpBtn = clone.querySelector(".threshold-move-up");
  const moveDownBtn = clone.querySelector(".threshold-move-down");
  if (moveUpBtn) {
    moveUpBtn.disabled = index === 0;
  }
  if (moveDownBtn) {
    moveDownBtn.disabled = index === total - 1;
  }

  // Toggle button setup
  const toggleBtn = clone.querySelector(".threshold-toggle");
  const settingsDiv = clone.querySelector(".threshold-settings");
  let isEnabled = threshold.enabled;
  let isSticky = threshold.sticky === true;
  let isAdvancedOpen = thresholdAdvancedOpen[threshold.id] === true;

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

  // Add event listeners
  const thresholdId = threshold.id;

  // Enable/disable toggle button
  toggleBtn.addEventListener("click", function() {
    isEnabled = !isEnabled;
    updateToggleState();
    sendThresholdUpdate("thresholdEnabled", thresholdId, isEnabled ? "true" : "false", isEnabled);
  });

  // Name input with debounce
  let nameTimeout;
  nameInput.addEventListener("input", function(e) {
    clearTimeout(nameTimeout);
    nameTimeout = setTimeout(function() {
      sendThresholdUpdate("thresholdName", thresholdId, e.target.value);
    }, 300);
  });

  // Text input with debounce
  let textTimeout;
  textInput.addEventListener("input", function(e) {
    clearTimeout(textTimeout);
    textTimeout = setTimeout(function() {
      sendThresholdUpdate("thresholdText", thresholdId, e.target.value);
    }, 300);
  });

  // Operator select
  operatorSelect.addEventListener("change", function(e) {
    sendThresholdUpdate("thresholdOperator", thresholdId, e.target.value);
  });

  // Value input with debounce
  let valueTimeout;
  valueInput.addEventListener("input", function(e) {
    clearTimeout(valueTimeout);
    valueTimeout = setTimeout(function() {
      sendThresholdUpdate("thresholdValue", thresholdId, e.target.value);
    }, 300);
  });

  let hysteresisTimeout;
  hysteresisInput.addEventListener("input", function(e) {
    clearTimeout(hysteresisTimeout);
    hysteresisTimeout = setTimeout(function() {
      sendThresholdUpdate("thresholdHysteresis", thresholdId, e.target.value);
    }, 300);
  });

  let dwellTimeout;
  dwellInput.addEventListener("input", function(e) {
    clearTimeout(dwellTimeout);
    dwellTimeout = setTimeout(function() {
      sendThresholdUpdate("thresholdDwellMs", thresholdId, e.target.value);
    }, 300);
  });

  let cooldownTimeout;
  cooldownInput.addEventListener("input", function(e) {
    clearTimeout(cooldownTimeout);
    cooldownTimeout = setTimeout(function() {
      sendThresholdUpdate("thresholdCooldownMs", thresholdId, e.target.value);
    }, 300);
  });

  if (stickyBtn) {
    stickyBtn.addEventListener("click", function() {
      isSticky = !isSticky;
      updateStickyState();
      sendThresholdUpdate(
        "thresholdSticky",
        thresholdId,
        isSticky ? "true" : "false",
        isSticky
      );
    });
  }

  if (advancedToggleBtn) {
    advancedToggleBtn.addEventListener("click", function() {
      isAdvancedOpen = !isAdvancedOpen;
      thresholdAdvancedOpen[thresholdId] = isAdvancedOpen;
      updateAdvancedState();
    });
  }

  // Color inputs
  bgInput.addEventListener("change", function(e) {
    sendThresholdUpdate("thresholdBackgroundColor", thresholdId, e.target.value);
  });

  fgInput.addEventListener("change", function(e) {
    sendThresholdUpdate("thresholdForegroundColor", thresholdId, e.target.value);
  });

  hlInput.addEventListener("change", function(e) {
    sendThresholdUpdate("thresholdHighlightColor", thresholdId, e.target.value);
  });

  vtInput.addEventListener("change", function(e) {
    sendThresholdUpdate("thresholdValueTextColor", thresholdId, e.target.value);
  });

  if (tcInput) {
    tcInput.addEventListener("change", function(e) {
      sendThresholdUpdate("thresholdTextColor", thresholdId, e.target.value);
    });
  }

  if (moveUpBtn) {
    moveUpBtn.addEventListener("click", function() {
      sendValueToPlugin({
        key: "reorderThreshold",
        thresholdId: thresholdId,
        value: "up"
      }, "sdpi_collection");
    });
  }

  if (moveDownBtn) {
    moveDownBtn.addEventListener("click", function() {
      sendValueToPlugin({
        key: "reorderThreshold",
        thresholdId: thresholdId,
        value: "down"
      }, "sdpi_collection");
    });
  }

  // Remove button
  const removeBtn = clone.querySelector(".threshold-remove");
  removeBtn.addEventListener("click", function() {
    delete thresholdAdvancedOpen[thresholdId];
    sendValueToPlugin({
      key: "removeThreshold",
      thresholdId: thresholdId
    }, "sdpi_collection");
    // Remove from DOM immediately for responsiveness
    wrapper.remove();
  });

  return clone;
}

/** ACTIVE GLOBAL THRESHOLDS */

function renderActiveGlobals() {
  var container = document.querySelector("#globalRefsContainer");
  if (!container) return;
  container.innerHTML = "";
  var active = (globalThresholds || []).filter(function(gt) {
    return !gt.readingType || gt.readingType === currentReadingType;
  });
  if (active.length === 0) {
    if (globalThresholds && globalThresholds.length > 0 && currentReadingType) {
      var none = document.createElement("div");
      none.className = "sdpi-item";
      none.innerHTML = '<div class="sdpi-item-label" style="color:#666;">None match</div><div class="sdpi-item-value" style="color:#666;">sensor type: ' + currentReadingType + '</div>';
      container.appendChild(none);
    }
    return;
  }
  active.forEach(function(gt) {
    var suppressed = currentSuppressedGlobalIDs.indexOf(gt.id) !== -1;
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
    btn.title = suppressed ? "Click to enable for this tile" : "Click to disable for this tile";
    btn.addEventListener("click", function() {
      var isSuppressed = currentSuppressedGlobalIDs.indexOf(gt.id) !== -1;
      if (isSuppressed) {
        currentSuppressedGlobalIDs = currentSuppressedGlobalIDs.filter(function(id) { return id !== gt.id; });
        sendValueToPlugin({ key: "unsuppressGlobal", value: gt.id }, "sdpi_collection");
      } else {
        currentSuppressedGlobalIDs = currentSuppressedGlobalIDs.concat([gt.id]);
        sendValueToPlugin({ key: "suppressGlobal", value: gt.id }, "sdpi_collection");
      }
      var nowSuppressed = currentSuppressedGlobalIDs.indexOf(gt.id) !== -1;
      btn.style.background = nowSuppressed ? "#a44" : "#4a4";
      btn.textContent = nowSuppressed ? "off" : "on";
      btn.title = nowSuppressed ? "Click to enable for this tile" : "Click to disable for this tile";
    });
    valCell.appendChild(span);
    valCell.appendChild(btn);
    row.appendChild(label);
    row.appendChild(valCell);
    container.appendChild(row);
  });
}
