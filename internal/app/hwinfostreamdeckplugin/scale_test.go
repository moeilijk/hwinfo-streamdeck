package hwinfostreamdeckplugin

import (
	"math"
	"testing"
)

func TestSensorScale(t *testing.T) {
	for _, tc := range []struct {
		low, high, value float64
		min, max         int
	}{
		{32.4, 66.7, 45, 32, 67},
		{0, 0, 0, 0, 1},
		{50, 50, 50, 50, 51},
		{10, 20, 25, 10, 25},
		{-4.2, -1.1, -3, -5, -1},
		{30, 10, 20, 0, 100},
		{math.NaN(), 20, 10, 0, 100},
	} {
		min, max := sensorScale(tc.low, tc.high, tc.value, 0, 100)
		if min != tc.min || max != tc.max {
			t.Errorf("%+v: got %d..%d", tc, min, max)
		}
	}
}
