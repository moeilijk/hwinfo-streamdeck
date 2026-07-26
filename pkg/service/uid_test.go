package hwsensorsservice

import "testing"

func TestComposeSplitRoundTrip(t *testing.T) {
	cases := []struct{ source, sensor, uid string }{
		{"", "12345600", "12345600"},
		{"remote0", "12345600", "remote0::12345600"},
		{"remote1", "/mockcpu/0", "remote1::/mockcpu/0"},
	}
	for _, c := range cases {
		if got := ComposeSensorUID(c.source, c.sensor); got != c.uid {
			t.Errorf("ComposeSensorUID(%q, %q) = %q, want %q", c.source, c.sensor, got, c.uid)
		}
		gotSource, gotSensor := SplitSensorUID(c.uid)
		if gotSource != c.source || gotSensor != c.sensor {
			t.Errorf("SplitSensorUID(%q) = (%q, %q), want (%q, %q)", c.uid, gotSource, gotSensor, c.source, c.sensor)
		}
	}
}

// Stored v3 settings hold plain local uids; they must keep resolving to the
// local source forever.
func TestSplitSensorUIDLegacyLocal(t *testing.T) {
	source, sensor := SplitSensorUID("/mockcpu/0")
	if source != LocalSourceID || sensor != "/mockcpu/0" {
		t.Errorf("legacy local uid split = (%q, %q)", source, sensor)
	}
}

func TestSourceDisplayName(t *testing.T) {
	if got := SourceDisplayName(""); got != "Local" {
		t.Errorf("local display name = %q", got)
	}
	if got := SourceDisplayName(RemoteSourceID(0)); got != "Remote 1" {
		t.Errorf("remote0 display name = %q", got)
	}
	if got := SourceDisplayName("weird"); got != "weird" {
		t.Errorf("unknown source display name = %q", got)
	}
}
