/** Normalize a reading type string to its canonical short form ("Temp") */
function normalizeReadingType(t) {
  switch ((t || "").toLowerCase()) {
    case "temp": case "temperature": return "Temp";
    case "volt": case "voltage":     return "Volt";
    case "fan":                      return "Fan";
    case "current":                  return "Current";
    case "power":                    return "Power";
    case "clock":                    return "Clock";
    case "usage": case "load": case "control": case "level": return "Usage";
    case "none":                     return "None";
    default:                         return t ? "Other" : "";
  }
}

function naturalCompare(left, right) {
  var a = String(left || "");
  var b = String(right || "");
  var partsA = a.match(/(\d+|\D+)/g) || [];
  var partsB = b.match(/(\d+|\D+)/g) || [];
  var maxLen = Math.max(partsA.length, partsB.length);

  for (var i = 0; i < maxLen; i++) {
    var partA = partsA[i];
    var partB = partsB[i];
    if (partA === undefined) return -1;
    if (partB === undefined) return 1;

    var numA = /^\d+$/.test(partA);
    var numB = /^\d+$/.test(partB);
    if (numA && numB) {
      var diff = parseInt(partA, 10) - parseInt(partB, 10);
      if (diff !== 0) return diff;
      continue;
    }

    var textA = partA.toLowerCase();
    var textB = partB.toLowerCase();
    if (textA > textB) return 1;
    if (textB > textA) return -1;
  }

  return 0;
}

function compareReadings(a, b) {
  var prefixDiff = naturalCompare(a.prefix || a.unit || "", b.prefix || b.unit || "");
  if (prefixDiff !== 0) return prefixDiff;

  var labelDiff = naturalCompare(a.label || "", b.label || "");
  if (labelDiff !== 0) return labelDiff;

  var typeDiff = naturalCompare(a.type || "", b.type || "");
  if (typeDiff !== 0) return typeDiff;

  return naturalCompare(String(a.id || ""), String(b.id || ""));
}

// readingOptionLabel is the single source of truth for how a reading is shown in
// every sensor/reading dropdown (key, dial, derived, composite). Keeping it here
// means the actions can never drift apart again (e.g. "Memory (%)" vs "% Memory").
function readingOptionLabel(reading) {
  if (!reading) return "";
  var label = reading.label || "";
  return reading.unit ? label + " (" + reading.unit + ")" : label;
}

function byId(id) {
  return document.getElementById(id);
}

function normalizeHexColor(hex) {
  if (typeof hex !== "string" || !hex) return hex;
  if (hex.length === 4 && hex.charAt(0) === "#") {
    return "#" + hex[1] + hex[1] + hex[2] + hex[2] + hex[3] + hex[3];
  }
  return hex;
}

function setInputValue(id, val) {
  var el = byId(id);
  if (el) {
    el.value = val == null ? "" : val;
  }
  return el;
}

function setColorValue(id, hex) {
  var el = byId(id);
  if (!el || !hex) return el;
  el.value = normalizeHexColor(hex);
  return el;
}

function setSelectValue(id, val) {
  var el = byId(id);
  if (!el || val == null) return el;
  var target = String(val);
  for (var i = 0; i < el.options.length; i++) {
    if (el.options[i].value === target) {
      el.selectedIndex = i;
      break;
    }
  }
  return el;
}

function bindValueChange(id, eventName, handler) {
  var el = byId(id);
  if (!el) return null;
  el[eventName || "onchange"] = function () {
    handler(el.value, el);
  };
  return el;
}

function positionRangeVal(inp) {
  var wrap = inp.parentElement;
  var span = wrap && wrap.querySelector(".range-val");
  if (!span) return;
  span.textContent = inp.value;
  requestAnimationFrame(function() {
    var pct = (parseFloat(inp.value) - parseFloat(inp.min)) / (parseFloat(inp.max) - parseFloat(inp.min));
    span.style.left = (pct * (inp.offsetWidth - 14) + 7) + "px";
  });
}

function bindSdpiValue(id, sendSdpi, eventName, extra) {
  return bindValueChange(id, eventName, function (value, el) {
    sendSdpi(id, value);
    if (extra) {
      extra(value, el);
    }
  });
}
