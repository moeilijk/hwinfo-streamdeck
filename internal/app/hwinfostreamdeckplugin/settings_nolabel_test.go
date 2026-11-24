package hwinfostreamdeckplugin

import (
	"image/color"
	"testing"
	"time"

	"github.com/moeilijk/hwinfo-streamdeck/pkg/graph"
	hwsensorsservice "github.com/moeilijk/hwinfo-streamdeck/pkg/service"
)

// No recovery by label (user decision, lhm-streamdeck #77, confirmed for both
// plugins in lhm-streamdeck #121): a stale ReadingID must NOT be silently
// re-mapped to a reading with the same label. Labels are not unique within a
// sensor, so a re-map can pick the wrong reading; the user re-selects instead.
func TestSyncSettingsDoesNotRecoverByLabel(t *testing.T) {
	readings := []hwsensorsservice.Reading{
		stubReading{id: 111, typ: "Temperature", label: "Tctl", unit: "°C"},
	}
	settings := actionSettings{
		SensorUID:    "/cpu",
		ReadingID:    999, // stale id from before the source renamed its sensors
		ReadingLabel: "Tctl",
		IsValid:      true,
	}

	changed := syncSettingsWithReadings(&settings, readings)

	if settings.ReadingID != 999 {
		t.Fatalf("ReadingID rewritten to %d via label match; silent re-mapping must not happen", settings.ReadingID)
	}
	if changed {
		t.Fatalf("settings reported as changed; stale ids must stay untouched")
	}
}

// Control: a matching id still fills in missing display metadata.
func TestSyncSettingsFillsLabelFromValidID(t *testing.T) {
	readings := []hwsensorsservice.Reading{
		stubReading{id: 111, typ: "Temperature", label: "Tctl", unit: "°C"},
	}
	settings := actionSettings{SensorUID: "/cpu", ReadingID: 111}

	if !syncSettingsWithReadings(&settings, readings) {
		t.Fatalf("expected change: empty label should be filled from the matching reading")
	}
	if settings.ReadingLabel != "Tctl" || !settings.IsValid {
		t.Fatalf("label/valid not filled: %+v", settings)
	}
}

// staleIDPlugin has one source whose sensor carries two readings with the
// same label, as HWiNFO and LHM both have (measured: "GPU Fan1" twice on one
// GPU). The tile asks for an id that no longer exists.
func staleIDPlugin(pollTime uint64) *Plugin {
	p := &Plugin{
		graphs:           make(map[string]*graph.Graph),
		thresholdStates:  make(map[string]map[string]*thresholdRuntimeState),
		thresholdSnoozes: make(map[string]*thresholdSnoozeState),
		thresholdDirty:   make(map[string]bool),
		lastPollTime:     make(map[string]uint64),
		lastRenderTime:   make(map[string]time.Time),
		smoothedValues:   make(map[string]float64),
		divisorCache:     make(map[string]divisorCacheEntry),
		pollTimeCacheTTL: time.Second,
	}
	// The plugin's source: hwinfo has one bridge runtime, lhm a runtime per
	// source profile (the source model differs until the core shares one).
	p.bridge = &bridgeRuntime{
		hw: stubHardwareService{
			pollTime: pollTime,
			readingsBySensor: map[string][]hwsensorsservice.Reading{
				"/gpu": {
					stubReading{id: 1, typ: "Fan", label: "GPU Fan1", unit: "RPM"},
					stubReading{id: 2, typ: "Fan", label: "GPU Fan1", unit: "RPM"},
				},
			},
		},
	}
	return p
}

// A dial page whose reading id is gone shows "Reading missing" and keeps its
// id. It must not pick another reading with the same label.
func TestDialPageDoesNotRecoverByLabel(t *testing.T) {
	p := staleIDPlugin(uint64(time.Now().UnixNano()))
	settings := &dialActionSettings{Pages: []actionSettings{{
		SensorUID:    "/gpu",
		ReadingID:    999,
		ReadingLabel: "GPU Fan1",
		IsValid:      true,
		Min:          0,
		Max:          100,
	}}}
	state := &dialState{graphs: []*graph.Graph{newDialGraph(&settings.Pages[0])}}

	render, changed := p.updateDialPage("dl", settings, state, 0, true, time.Now())

	if render.messageValue != "Reading missing" {
		t.Fatalf("messageValue = %q, want \"Reading missing\"", render.messageValue)
	}
	if changed || settings.Pages[0].ReadingID != 999 {
		t.Fatalf("page re-mapped to reading %d (changed=%v); a stale id must stay as it is", settings.Pages[0].ReadingID, changed)
	}
}

// A reading tile whose reading id is gone falls back to the placeholder and
// keeps its id. It must not pick another reading with the same label.
func TestReadingTileDoesNotRecoverByLabel(t *testing.T) {
	f := newTileFixture(t)
	p := staleIDPlugin(uint64(time.Now().UnixNano()))
	p.sd = f.rec.sd
	p.placeholderImage = []byte("png")
	settings := &actionSettings{SensorUID: "/gpu", ReadingID: 999, ReadingLabel: "GPU Fan1", IsValid: true, Min: 0, Max: 100}
	black := &color.RGBA{0, 0, 0, 255}
	p.graphs["r"] = graph.NewGraph(tileWidth, tileHeight, 0, 100, black, black, black)

	p.updateTiles(&actionData{action: "com.moeilijk.hwinfo.reading", context: "r", settings: settings})

	sentImage := ""
	for _, ev := range f.rec.take() {
		if ev.Event == "setImage" {
			sentImage = ev.Payload.Image
		}
	}
	if sentImage != placeholderData {
		if len(sentImage) > 40 {
			sentImage = sentImage[:40] + "..."
		}
		t.Fatalf("key image %q, want the placeholder", sentImage)
	}
	if settings.ReadingID != 999 {
		t.Fatalf("tile re-mapped to reading %d; a stale id must stay as it is", settings.ReadingID)
	}
}
