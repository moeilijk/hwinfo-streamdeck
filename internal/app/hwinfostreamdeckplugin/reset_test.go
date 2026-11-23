package hwinfostreamdeckplugin

import "testing"

func TestKeyPressResetsMinMax(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
		alert   string
		want    bool
	}{
		{name: "option off", want: false},
		{name: "option on", enabled: true, want: true},
		{name: "option on, alert active", enabled: true, alert: "t1", want: false},
		{name: "option off, alert active", alert: "t1", want: false},
	} {
		s := &actionSettings{ResetMinMaxOnPress: tc.enabled, CurrentThresholdID: tc.alert}
		if got := keyPressResetsMinMax(s); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestTakePendingScaleReset(t *testing.T) {
	p := &Plugin{}
	if p.takePendingScaleReset("ctx", 100) {
		t.Fatal("history cleared without a pending reset")
	}

	p.scaleResetAfter = map[string]uint64{"ctx": 100, "other": 100}
	p.smoothedValues = map[string]float64{"ctx": 42, "other": 7}

	// A sample polled before or at the reset still holds pre-reset data.
	for _, pollTime := range []uint64{99, 100} {
		if p.takePendingScaleReset("ctx", pollTime) {
			t.Fatalf("history cleared on a sample from before the reset (pollTime %d)", pollTime)
		}
	}
	if _, pending := p.scaleResetAfter["ctx"]; !pending {
		t.Fatal("pending reset dropped before a fresh sample arrived")
	}

	if !p.takePendingScaleReset("ctx", 101) {
		t.Fatal("history not cleared on the first sample after the reset")
	}
	if _, kept := p.smoothedValues["ctx"]; kept {
		t.Fatal("smoothed value survived the reset")
	}
	if p.takePendingScaleReset("ctx", 102) {
		t.Fatal("history cleared twice for one reset")
	}
	if _, pending := p.scaleResetAfter["other"]; !pending || p.smoothedValues["other"] != 7 {
		t.Fatal("another tile lost its state")
	}
}
