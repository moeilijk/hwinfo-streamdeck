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

// TestDecodeActionSettingsRemoteSourceUID guards the remote-sensor uid scheme:
// a source-qualified sensorUid travels through the stable `sensorUid` json tag
// as an opaque string, without a new settings field and without touching plain
// local uids.
func TestDecodeActionSettingsRemoteSourceUID(t *testing.T) {
	remote := json.RawMessage(`{
		"sensorUid": "remote0::1229870",
		"readingId": "134217730",
		"isValid": true
	}`)

	settings, migrated, err := decodeActionSettings(&remote)
	if err != nil {
		t.Fatalf("decodeActionSettings: %v", err)
	}
	if migrated {
		t.Fatalf("remote uid must not trigger legacy migration")
	}
	if settings.SensorUID != "remote0::1229870" {
		t.Errorf("SensorUID = %q, want opaque source-qualified uid", settings.SensorUID)
	}

	out, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var roundTrip map[string]interface{}
	if err := json.Unmarshal(out, &roundTrip); err != nil {
		t.Fatalf("unmarshal roundtrip: %v", err)
	}
	if roundTrip["sensorUid"] != "remote0::1229870" {
		t.Errorf("roundtrip sensorUid = %v, want remote0::1229870", roundTrip["sensorUid"])
	}
}
