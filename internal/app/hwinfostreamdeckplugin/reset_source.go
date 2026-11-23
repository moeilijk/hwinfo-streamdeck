package hwinfostreamdeckplugin

import "errors"

// The product-specific side of "Reset on press" (see reset.go).
//
// The plugin reads HWiNFO through its shared memory and has no channel to
// send HWiNFO a command, so it cannot reset a sensor's MIN/MAX. The inspector
// does not offer the option and a key press never triggers a reset.

var errResetUnsupported = errors.New("the sensor source cannot reset MIN/MAX")

// sourceResetSupported reports whether the sensor source can reset MIN/MAX.
func (p *Plugin) sourceResetSupported() bool {
	return false
}

// resetSourceMinMax resets the MIN/MAX of the reading behind settings at the
// sensor source.
func (p *Plugin) resetSourceMinMax(settings actionSettings) error {
	return errResetUnsupported
}

// sameResetTarget reports whether two settings point at the same reading of
// the same source. The source is part of the sensor UID.
func sameResetTarget(a, b *actionSettings) bool {
	return a.SensorUID == b.SensorUID && a.ReadingID == b.ReadingID
}
