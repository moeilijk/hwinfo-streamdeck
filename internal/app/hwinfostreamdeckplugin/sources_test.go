package hwinfostreamdeckplugin

import (
	"testing"
	"time"

	hwsensorsservice "github.com/moeilijk/hwinfo-streamdeck/pkg/service"
)

func pluginWithSources(sources []hwsensorsservice.Source) *Plugin {
	return &Plugin{
		bridge: &bridgeRuntime{hw: stubHardwareService{sources: sources}},
	}
}

func TestPollTimeForUIDRoutesBySource(t *testing.T) {
	now := uint64(time.Now().UnixNano())
	p := pluginWithSources([]hwsensorsservice.Source{
		stubSource{id: "", pollTime: 1, available: true},
		stubSource{id: "remote0", pollTime: now, available: true},
		stubSource{id: "remote1", pollTime: now, available: false},
	})

	// Plain (local) uid: the local poll time via the existing path.
	if pt, err := p.pollTimeForUID("12345600"); err != nil || pt != 1 {
		t.Errorf("local uid: pollTime = %d, err = %v; want 1, nil", pt, err)
	}
	// Source-qualified uid: that source's poll time.
	if pt, err := p.pollTimeForUID("remote0::12345600"); err != nil || pt != now {
		t.Errorf("remote0 uid: pollTime = %d, err = %v; want %d, nil", pt, err, now)
	}
	// Unavailable source: error, so tiles fall into their unavailable path.
	if _, err := p.pollTimeForUID("remote1::12345600"); err == nil {
		t.Error("remote1 (unavailable) uid: want error, got nil")
	}
	// Vanished source: error.
	if _, err := p.pollTimeForUID("remote9::12345600"); err == nil {
		t.Error("remote9 (absent) uid: want error, got nil")
	}
}

func TestSourceFreshForUID(t *testing.T) {
	now := uint64(time.Now().UnixNano())
	stale := uint64(time.Now().Add(-time.Minute).UnixNano())
	p := pluginWithSources([]hwsensorsservice.Source{
		stubSource{id: "remote0", pollTime: now, available: true},
		stubSource{id: "remote1", pollTime: stale, available: true},
	})

	// Local uids are always fresh here: the tile-level local gate decides.
	if !p.sourceFreshForUID("12345600") {
		t.Error("local uid must be fresh")
	}
	if !p.sourceFreshForUID("remote0::1") {
		t.Error("recently polled remote source must be fresh")
	}
	// A source that stopped polling (e.g. 12h shared-memory limit) must not
	// keep feeding its last value.
	if p.sourceFreshForUID("remote1::1") {
		t.Error("stale remote source must not be fresh")
	}
	if p.sourceFreshForUID("remote9::1") {
		t.Error("absent remote source must not be fresh")
	}
}
