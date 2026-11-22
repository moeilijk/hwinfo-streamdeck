package hwinfostreamdeckplugin

import (
	"testing"
	"time"
)

// stubHardwareService reports poll time 1 (one nanosecond after the epoch),
// which the tiles treat as a source that stopped polling.
func newStaleSourcePlugin() *Plugin {
	return &Plugin{
		bridge:            &bridgeRuntime{hw: stubHardwareService{}},
		pollTimeCacheTTL:  time.Second,
		thresholdDirty:    make(map[string]bool),
		lastRenderTime:    make(map[string]time.Time),
		compositeSettings: make(map[string]*compositeActionSettings),
		compositeStates:   make(map[string]*compositeState),
		derivedSettings:   make(map[string]*derivedActionSettings),
		derivedStates:     make(map[string]*derivedState),
	}
}

func TestCompositeTileMarksUnavailableWhenSourceGone(t *testing.T) {
	p := newStaleSourcePlugin()
	state := &compositeState{lastPollTime: 42}
	p.compositeSettings["ctx"] = &compositeActionSettings{}
	p.compositeStates["ctx"] = state

	p.updateCompositeTile("ctx")

	if !state.unavailable {
		t.Fatal("unavailable = false, want true while the source is gone")
	}
	if state.lastPollTime != 0 {
		t.Fatalf("lastPollTime = %d, want 0 so the tile renders again on recovery", state.lastPollTime)
	}
}

func TestDerivedTileMarksUnavailableWhenSourceGone(t *testing.T) {
	p := newStaleSourcePlugin()
	state := &derivedState{lastPollTime: 42}
	p.derivedSettings["ctx"] = &derivedActionSettings{}
	p.derivedStates["ctx"] = state

	p.updateDerivedTile("ctx")

	if !state.unavailable {
		t.Fatal("unavailable = false, want true while the source is gone")
	}
	if state.lastPollTime != 0 {
		t.Fatalf("lastPollTime = %d, want 0 so the tile renders again on recovery", state.lastPollTime)
	}
}

func TestShowTileUnavailableKeepsStateOnRepeat(t *testing.T) {
	p := newStaleSourcePlugin()
	unavailable := false
	lastPollTime := uint64(42)

	p.showTileUnavailable("ctx", &unavailable, &lastPollTime)
	p.showTileUnavailable("ctx", &unavailable, &lastPollTime)

	if !unavailable || lastPollTime != 0 {
		t.Fatalf("unavailable = %v, lastPollTime = %d, want true and 0", unavailable, lastPollTime)
	}
}
