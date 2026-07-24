package hwinfostreamdeckplugin

import (
	"encoding/json"
	"testing"
)

// TestDecodeActionSettingsHWiNFOv205 guards the upgrade path for tiles that were
// configured with the original HWiNFO plugin (v2.0.5): numeric sensor/reading ids
// must pass through untouched so existing tiles keep working against the
// hwinfo-bridge, which produces the same ids.
func TestDecodeActionSettingsHWiNFOv205(t *testing.T) {
	legacy := json.RawMessage(`{
		"sensorUid": "1229870",
		"readingId": "134217730",
		"title": "CPU",
		"titleFontSize": 10.5,
		"valueFontSize": 10.5,
		"min": 0,
		"max": 100,
		"format": "%.0f°",
		"divisor": "",
		"isValid": true,
		"titleColor": "#b7b7b7",
		"foregroundColor": "#005128",
		"backgroundColor": "#000000",
		"highlightColor": "#009e00",
		"valueTextColor": "#ffffff",
		"inErrorState": false
	}`)

	settings, migrated, err := decodeActionSettings(&legacy)
	if err != nil {
		t.Fatalf("decodeActionSettings: %v", err)
	}
	if migrated {
		t.Fatalf("v2.0.5 settings must not trigger legacy LHM migration")
	}
	if settings.SensorUID != "1229870" {
		t.Errorf("SensorUID = %q, want %q", settings.SensorUID, "1229870")
	}
	if settings.ReadingID != 134217730 {
		t.Errorf("ReadingID = %d, want %d", settings.ReadingID, 134217730)
	}
	if !settings.IsValid {
		t.Errorf("IsValid = false, want true")
	}
	if settings.Title != "CPU" || settings.Format != "%.0f°" {
		t.Errorf("appearance fields altered: title=%q format=%q", settings.Title, settings.Format)
	}
}
