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
	"github.com/moeilijk/hwinfo-streamdeck/pkg/graph"
	hwsensorsservice "github.com/moeilijk/hwinfo-streamdeck/pkg/service"
	"github.com/moeilijk/hwinfo-streamdeck/pkg/streamdeck"
)

// sdRecorder is a Stream Deck stand-in: a WebSocket server the real
// streamdeck client connects to, recording every event the plugin sends.
type sdRecorder struct {
	t      *testing.T
	sd     *streamdeck.StreamDeck
	mu     sync.Mutex
	events []sdEvent
	seen   chan struct{}
	syncs  int
}

type sdEvent struct {
	Event   string `json:"event"`
	Context string `json:"context"`
	Payload struct {
		Image   string `json:"image"`
		Title   string `json:"title"`
		Error   *bool  `json:"error"`
		Message string `json:"message"`
	} `json:"payload"`
}

func newSDRecorder(t *testing.T) *sdRecorder {
	t.Helper()
	rec := &sdRecorder{t: t, seen: make(chan struct{}, 1024)}
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
			var ev sdEvent
			if json.Unmarshal(data, &ev) != nil {
				continue
			}
			rec.mu.Lock()
			rec.events = append(rec.events, ev)
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
	rec.take()
	return rec
}

// take returns the events sent since the previous call. It sends a marker
// through the same connection and waits for it, so everything the plugin
// sent before is in.
func (rec *sdRecorder) take() []sdEvent {
	rec.t.Helper()
	rec.syncs++
	marker := fmt.Sprintf("sync-%d", rec.syncs)
	if err := rec.sd.SetTitle("sync", marker); err != nil {
		rec.t.Fatalf("send marker: %v", err)
	}
	deadline := time.After(5 * time.Second)
	for {
		rec.mu.Lock()
		for i, ev := range rec.events {
			if ev.Event == "setTitle" && ev.Payload.Title == marker {
				out := append([]sdEvent(nil), rec.events[:i]...)
				rec.events = rec.events[i+1:]
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

// tileHW is a bridge whose local and remote poll times a test can move.
type tileHW struct {
	mu            sync.Mutex
	local, remote uint64
}

func (h *tileHW) set(local, remote uint64) {
	h.mu.Lock()
	h.local, h.remote = local, remote
	h.mu.Unlock()
}

func (h *tileHW) PollTime() (uint64, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.local, nil
}

func (h *tileHW) Sensors() ([]hwsensorsservice.Sensor, error) { return nil, nil }

func (h *tileHW) ReadingsForSensorID(string) ([]hwsensorsservice.Reading, error) {
	return []hwsensorsservice.Reading{stubReading{id: 1, typ: "Load", label: "CPU", unit: "%"}}, nil
}

func (h *tileHW) Sources() ([]hwsensorsservice.Source, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return []hwsensorsservice.Source{
		stubSource{id: hwsensorsservice.LocalSourceID, pollTime: h.local, available: true},
		stubSource{id: "remote0", pollTime: h.remote, available: true},
	}, nil
}

const (
	localUID        = "/cpu"
	remoteUID       = "remote0::/cpu"
	placeholderData = "data:image/png;base64,cG5n" // base64 of "png"
)

type tileFixture struct {
	t    *testing.T
	p    *Plugin
	hw   *tileHW
	rec  *sdRecorder
	tick time.Time
}

func newTileFixture(t *testing.T) *tileFixture {
	t.Helper()
	rec := newSDRecorder(t)
	hw := &tileHW{}
	p := &Plugin{
		bridge:            &bridgeRuntime{hw: hw},
		sd:                rec.sd,
		lastPollTime:      map[string]uint64{},
		lastRenderTime:    map[string]time.Time{},
		smoothedValues:    map[string]float64{},
		divisorCache:      map[string]divisorCacheEntry{},
		thresholdDirty:    map[string]bool{},
		thresholdStates:   map[string]map[string]*thresholdRuntimeState{},
		thresholdSnoozes:  map[string]*thresholdSnoozeState{},
		compositeSettings: map[string]*compositeActionSettings{},
		compositeStates:   map[string]*compositeState{},
		derivedSettings:   map[string]*derivedActionSettings{},
		derivedStates:     map[string]*derivedState{},
		dialSettings:      map[string]*dialActionSettings{},
		dialStates:        map[string]*dialState{},
		placeholderImage:  []byte("png"),
	}
	return &tileFixture{t: t, p: p, hw: hw, rec: rec, tick: time.Now()}
}

// poll moves the sources one poll forward (or leaves one stale) and drops
// the plugin's poll time cache, as a new ticker cycle would find it.
func (f *tileFixture) poll(localLive, remoteLive bool) {
	f.tick = f.tick.Add(time.Millisecond)
	stale := uint64(time.Now().Add(-time.Minute).UnixNano())
	local, remote := stale, stale
	if localLive {
		local = uint64(f.tick.UnixNano())
	}
	if remoteLive {
		remote = uint64(f.tick.UnixNano())
	}
	f.hw.set(local, remote)
	f.p.mu.Lock()
	invalidatePollCacheForRuntime(f.p.bridge)
	f.p.mu.Unlock()
}

func (f *tileFixture) composite(ctx string, uids ...string) {
	cs := &compositeActionSettings{SlotCount: len(uids), Mode: "graph"}
	for i, uid := range uids {
		cs.Slots[i] = compositeSlotSettings{SensorUID: uid, ReadingID: 1, IsValid: true}
	}
	f.p.compositeSettings[ctx] = cs
	f.p.compositeStates[ctx] = &compositeState{graphs: initCompositeGraphs(cs)}
}

func (f *tileFixture) derived(ctx string, uids ...string) {
	ds := &derivedActionSettings{SlotCount: len(uids), Formula: "sum", Min: 0, Max: 100}
	for i, uid := range uids {
		ds.Slots[i] = derivedSlotSettings{SensorUID: uid, ReadingID: 1, IsValid: true}
	}
	f.p.derivedSettings[ctx] = ds
	f.p.derivedStates[ctx] = &derivedState{graph: initDerivedGraph(ds)}
}

// expect compares what the tile sent since the last check with a list like
// "image", "placeholder", "pi-error", "pi-show-ui".
func (f *tileFixture) expect(step string, want ...string) {
	f.t.Helper()
	var got []string
	for _, ev := range f.rec.take() {
		switch {
		case ev.Event == "setImage" && ev.Payload.Image == placeholderData:
			got = append(got, "placeholder")
		case ev.Event == "setImage":
			got = append(got, "image")
		case ev.Event == "sendToPropertyInspector" && ev.Payload.Error != nil && *ev.Payload.Error:
			got = append(got, "pi-error")
		case ev.Event == "sendToPropertyInspector" && ev.Payload.Message == "show_ui":
			got = append(got, "pi-show-ui")
		default:
			got = append(got, ev.Event)
		}
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		f.t.Fatalf("%s: sent %v, want %v", step, got, want)
	}
}

func TestCompositeFollowsLocalSource(t *testing.T) {
	f := newTileFixture(t)
	f.composite("c", localUID)

	f.poll(true, true)
	f.p.updateCompositeTile("c")
	f.expect("source polling", "image")

	f.poll(false, true)
	f.p.updateCompositeTile("c")
	f.expect("source gone", "pi-error", "placeholder")

	f.p.updateCompositeTile("c")
	f.expect("source still gone")

	f.poll(true, true)
	f.p.updateCompositeTile("c")
	f.expect("source back", "pi-show-ui", "image")

	f.poll(false, true)
	f.p.updateCompositeTile("c")
	f.expect("second outage", "pi-error", "placeholder")
}

func TestCompositeKeepsRenderingWhileOneSlotIsLive(t *testing.T) {
	f := newTileFixture(t)
	f.composite("c", localUID, remoteUID)

	f.poll(true, true)
	f.p.updateCompositeTile("c")
	f.expect("both polling", "image")

	f.poll(true, false)
	f.p.updateCompositeTile("c")
	f.expect("remote gone", "image")

	f.poll(false, false)
	f.p.updateCompositeTile("c")
	f.expect("both gone", "pi-error", "placeholder")
}

func TestCompositeOnRemoteSourceIgnoresLocalSource(t *testing.T) {
	f := newTileFixture(t)
	f.composite("c", remoteUID)

	f.poll(false, true)
	f.p.updateCompositeTile("c")
	f.expect("remote polling, local not", "image")

	// The remote machine is gone while HWiNFO itself runs: placeholder on
	// the key, but the inspector stays usable to pick another source.
	f.poll(true, false)
	f.p.updateCompositeTile("c")
	f.expect("remote gone, local polling", "placeholder")

	f.poll(true, true)
	f.p.updateCompositeTile("c")
	f.expect("remote back", "image")
}

func TestDerivedFollowsLocalSource(t *testing.T) {
	f := newTileFixture(t)
	f.derived("d", localUID, localUID)

	f.poll(true, true)
	f.p.updateDerivedTile("d")
	f.expect("source polling", "image")

	f.poll(false, true)
	f.p.updateDerivedTile("d")
	f.expect("source gone", "pi-error", "placeholder")

	f.p.updateDerivedTile("d")
	f.expect("source still gone")

	f.poll(true, true)
	f.p.updateDerivedTile("d")
	f.expect("source back", "pi-show-ui", "image")
}

func TestDerivedShowsPlaceholderWithTooFewLiveSlots(t *testing.T) {
	f := newTileFixture(t)
	f.derived("d", localUID, remoteUID)

	f.poll(true, true)
	f.p.updateDerivedTile("d")
	f.expect("both polling", "image")

	// One live value is not enough for the formula. HWiNFO itself still
	// runs, so the inspector is not put in its error state.
	f.poll(true, false)
	f.p.updateDerivedTile("d")
	f.expect("remote gone", "placeholder")

	f.poll(true, false)
	f.p.updateDerivedTile("d")
	f.expect("remote still gone")

	f.poll(true, true)
	f.p.updateDerivedTile("d")
	f.expect("remote back", "image")
}

func TestDerivedOnRemoteSourceIgnoresLocalSource(t *testing.T) {
	f := newTileFixture(t)
	f.derived("d", remoteUID, remoteUID)

	f.poll(false, true)
	f.p.updateDerivedTile("d")
	f.expect("remote polling, local not", "image")
}

func TestDerivedWithOneSlotConfigured(t *testing.T) {
	f := newTileFixture(t)
	f.derived("d", localUID)

	// Nothing to compute yet and nothing went away: the key is left alone.
	f.poll(true, true)
	f.p.updateDerivedTile("d")
	f.expect("source polling")

	f.poll(false, true)
	f.p.updateDerivedTile("d")
	f.expect("source gone", "pi-error", "placeholder")

	f.poll(true, true)
	f.p.updateDerivedTile("d")
	f.expect("source back", "pi-show-ui")
}

func TestDialPageUnavailableWhenPollTimeStopsAdvancing(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		uid                   string
		localLive, remoteLive bool
		wantMessage           string
	}{
		{"local polling", localUID, true, true, ""},
		{"local stopped polling", localUID, false, true, "HWiNFO unavailable"},
		{"remote polling", remoteUID, true, true, ""},
		{"remote stopped polling", remoteUID, true, false, "Source unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newTileFixture(t)
			settings := &dialActionSettings{Pages: []actionSettings{{SensorUID: tc.uid, ReadingID: 1, IsValid: true, Min: 0, Max: 100}}}
			state := &dialState{graphs: []*graph.Graph{newDialGraph(&settings.Pages[0])}}
			f.poll(tc.localLive, tc.remoteLive)

			render, _ := f.p.updateDialPage("dl", settings, state, 0, true, time.Now())

			if render.messageValue != tc.wantMessage {
				t.Fatalf("messageValue = %q, want %q", render.messageValue, tc.wantMessage)
			}
			if (len(render.image) > 0) != (tc.wantMessage == "") {
				t.Fatalf("image bytes = %d with message %q", len(render.image), render.messageValue)
			}
		})
	}
}
