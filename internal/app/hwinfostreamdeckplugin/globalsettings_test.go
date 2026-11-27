package hwinfostreamdeckplugin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/moeilijk/hwinfo-streamdeck/pkg/streamdeck"
)

// Stream Deck replaces the stored global settings on every setGlobalSettings
// (measured on Stream Deck 7.6.1, from the plugin and from a Property
// Inspector) and hands the plugin only what was written. These tests pin that
// a partial object never erases the rest, and that every write by the plugin
// carries the complete settings, Derived presets included.

const fullGlobalSettings = `{
 "pollInterval": 2000,
 "favoriteReadings": [{"id":"f1","sensorUid":"/gpu","sensorName":"GPU","readingId":"3","readingLabel":"GPU Core","readingUnit":"°C"}],
 "globalThresholds": [{"id":"g1","name":"Hot","enabled":true,"operator":">","value":80}],
 "derivedPresets": [{"name":"old","formula":"sum","slotCount":2,"slots":[]}]
}`

// gsRecorder is a Stream Deck stand-in that keeps the payload of every
// setGlobalSettings the plugin sends.
type gsRecorder struct {
	t      *testing.T
	sd     *streamdeck.StreamDeck
	mu     sync.Mutex
	writes []map[string]json.RawMessage
	titles []string
	seen   chan struct{}
	syncs  int
}

func newGSRecorder(t *testing.T) *gsRecorder {
	t.Helper()
	rec := &gsRecorder{t: t, seen: make(chan struct{}, 1024)}
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var ev struct {
				Event   string          `json:"event"`
				Payload json.RawMessage `json:"payload"`
			}
			if json.Unmarshal(data, &ev) != nil {
				continue
			}
			rec.mu.Lock()
			switch ev.Event {
			case "setGlobalSettings":
				var m map[string]json.RawMessage
				_ = json.Unmarshal(ev.Payload, &m)
				rec.writes = append(rec.writes, m)
			case "setTitle":
				var p struct {
					Title string `json:"title"`
				}
				_ = json.Unmarshal(ev.Payload, &p)
				rec.titles = append(rec.titles, p.Title)
			}
			rec.mu.Unlock()
			rec.seen <- struct{}{}
		}
	}))
	t.Cleanup(srv.Close)
	port := srv.URL[strings.LastIndex(srv.URL, ":")+1:]
	rec.sd = streamdeck.NewStreamDeck(port, "test", "registerPlugin", "")
	if err := rec.sd.Connect(); err != nil {
		t.Fatalf("connect to recorder: %v", err)
	}
	t.Cleanup(rec.sd.Close)
	return rec
}

// take returns the global settings writes since the previous call, after
// syncing on a marker sent through the same connection.
func (rec *gsRecorder) take() []map[string]json.RawMessage {
	rec.t.Helper()
	rec.syncs++
	marker := fmt.Sprintf("gs-sync-%d", rec.syncs)
	if err := rec.sd.SetTitle("sync", marker); err != nil {
		rec.t.Fatalf("send marker: %v", err)
	}
	deadline := time.After(5 * time.Second)
	for {
		rec.mu.Lock()
		for _, title := range rec.titles {
			if title == marker {
				out := rec.writes
				rec.writes = nil
				rec.titles = nil
				rec.mu.Unlock()
				return out
			}
		}
		rec.mu.Unlock()
		select {
		case <-rec.seen:
		case <-deadline:
			rec.t.Fatal("recorder did not receive the marker")
		}
	}
}

// newGSPlugin returns a plugin that has received the complete global
// settings once, with the writes of that first receive discarded.
func newGSPlugin(t *testing.T) (*Plugin, *gsRecorder) {
	rec := newGSRecorder(t)
	p := &Plugin{
		sd:                rec.sd,
		am:                newActionManager(2 * time.Second),
		globalSettings:    globalSettings{PollInterval: 2000},
		lastPollTime:      map[string]uint64{},
		lastRenderTime:    map[string]time.Time{},
		compositeSettings: map[string]*compositeActionSettings{},
		derivedSettings:   map[string]*derivedActionSettings{},
	}
	receiveGlobalSettings(p, fullGlobalSettings)
	rec.take()
	return p, rec
}

func receiveGlobalSettings(p *Plugin, raw string) {
	msg := json.RawMessage(raw)
	p.OnDidReceiveGlobalSettings(&streamdeck.EvDidReceiveGlobalSettings{
		Payload: streamdeck.EvDidReceiveGlobalSettingsPayload{Settings: &msg},
	})
}

func assertCompleteSettings(t *testing.T, gs globalSettings, wantPresets string) {
	t.Helper()
	if len(gs.FavoriteReadings) != 1 || gs.FavoriteReadings[0].ID != "f1" {
		t.Errorf("favorites = %+v, want f1", gs.FavoriteReadings)
	}
	if len(gs.GlobalThresholds) != 1 || gs.GlobalThresholds[0].ID != "g1" {
		t.Errorf("global thresholds = %+v, want g1", gs.GlobalThresholds)
	}
	if !strings.Contains(string(gs.DerivedPresets), wantPresets) {
		t.Errorf("derived presets = %s, want %s in it", gs.DerivedPresets, wantPresets)
	}
}

func assertWrittenComplete(t *testing.T, writes []map[string]json.RawMessage, wantPresets string) {
	t.Helper()
	if len(writes) == 0 {
		t.Fatal("plugin wrote no global settings")
	}
	last := writes[len(writes)-1]
	var gs globalSettings
	buf, _ := json.Marshal(last)
	if err := json.Unmarshal(buf, &gs); err != nil {
		t.Fatalf("written settings: %v", err)
	}
	assertCompleteSettings(t, gs, wantPresets)
}

// A Property Inspector of an older version writes only the presets. The
// plugin keeps everything else, the refresh rate included, and writes the
// complete settings back, so the stored copy is whole again.
func TestPartialPresetWriteKeepsTheRest(t *testing.T) {
	p, rec := newGSPlugin(t)
	receiveGlobalSettings(p, `{"derivedPresets":[{"name":"old"},{"name":"new","formula":"sum","slotCount":2,"slots":[]}]}`)

	assertCompleteSettings(t, p.globalSettings, `"new"`)
	if p.globalSettings.PollInterval != 2000 {
		t.Errorf("poll interval = %d, want 2000 kept", p.globalSettings.PollInterval)
	}
	assertWrittenComplete(t, rec.take(), `"new"`)
}

// A Property Inspector of an older version writes only the refresh rate.
func TestPartialPollIntervalWriteKeepsTheRest(t *testing.T) {
	p, rec := newGSPlugin(t)
	receiveGlobalSettings(p, `{"pollInterval":500}`)

	if p.globalSettings.PollInterval != 500 {
		t.Errorf("poll interval = %d, want 500", p.globalSettings.PollInterval)
	}
	assertCompleteSettings(t, p.globalSettings, `"old"`)
	assertWrittenComplete(t, rec.take(), `"old"`)
}

// A complete object is taken as is and not written back.
func TestCompleteGlobalSettingsAreNotWrittenBack(t *testing.T) {
	p, rec := newGSPlugin(t)
	receiveGlobalSettings(p, fullGlobalSettings)
	assertCompleteSettings(t, p.globalSettings, `"old"`)
	if writes := rec.take(); len(writes) != 0 {
		t.Errorf("plugin wrote %d times for a complete object, want 0", len(writes))
	}
}

// Every write by the plugin carries the Derived presets: saving a favorite,
// adding a global threshold or changing the refresh rate must not drop them.
func TestPluginWriteKeepsDerivedPresets(t *testing.T) {
	p, rec := newGSPlugin(t)
	if err := p.persistGlobalSettings(); err != nil {
		t.Fatal(err)
	}
	assertWrittenComplete(t, rec.take(), `"old"`)

	p.handleGlobalAddThreshold("Warm")
	writes := rec.take()
	if len(writes) == 0 {
		t.Fatal("adding a global threshold wrote nothing")
	}
	if !strings.Contains(string(writes[len(writes)-1]["derivedPresets"]), `"old"`) {
		t.Errorf("threshold write lost the presets: %s", writes[len(writes)-1]["derivedPresets"])
	}

	p.setPollInterval(5000)
	writes = rec.take()
	if len(writes) == 0 {
		t.Fatal("changing the refresh rate wrote nothing")
	}
	last := writes[len(writes)-1]
	if string(last["pollInterval"]) != "5000" {
		t.Errorf("written poll interval = %s, want 5000", last["pollInterval"])
	}
	if !strings.Contains(string(last["derivedPresets"]), `"old"`) {
		t.Errorf("refresh rate write lost the presets: %s", last["derivedPresets"])
	}
	if !strings.Contains(string(last["favoriteReadings"]), `"f1"`) {
		t.Errorf("refresh rate write lost the favorites: %s", last["favoriteReadings"])
	}
}

// The Derived Property Inspector sends its preset list to the plugin, which
// writes it together with everything else.
func TestSaveDerivedPresetsWritesComplete(t *testing.T) {
	p, rec := newGSPlugin(t)
	if err := p.saveDerivedPresets(json.RawMessage(`[{"name":"old"},{"name":"mine","formula":"max","slotCount":3,"slots":[]}]`)); err != nil {
		t.Fatal(err)
	}
	assertCompleteSettings(t, p.globalSettings, `"mine"`)
	assertWrittenComplete(t, rec.take(), `"mine"`)

	if err := p.saveDerivedPresets(json.RawMessage(`{"name":"not a list"}`)); err == nil {
		t.Error("a non-list was accepted as presets")
	}
	if writes := rec.take(); len(writes) != 0 {
		t.Errorf("a rejected preset list was written")
	}
	if !strings.Contains(string(p.globalSettings.DerivedPresets), `"mine"`) {
		t.Errorf("a rejected preset list replaced the stored one: %s", p.globalSettings.DerivedPresets)
	}
}

func TestMergeGlobalSettings(t *testing.T) {
	current := globalSettings{PollInterval: 1000, GlobalThresholds: []Threshold{{ID: "g1", Name: "Hot"}}}

	merged, incomplete, err := mergeGlobalSettings(current, json.RawMessage(`{"globalThresholds":[{"id":"g1","name":"Changed"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !incomplete {
		t.Error("an object without pollInterval should count as incomplete")
	}
	if merged.GlobalThresholds[0].Name != "Changed" || merged.PollInterval != 1000 {
		t.Errorf("merged = %+v", merged)
	}
	if current.GlobalThresholds[0].Name != "Hot" {
		t.Errorf("merge changed the current settings: %+v", current.GlobalThresholds)
	}

	if _, _, err := mergeGlobalSettings(current, json.RawMessage(`[1,2]`)); err == nil {
		t.Error("a non-object was accepted")
	}
}
