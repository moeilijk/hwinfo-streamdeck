package hwinfostreamdeckplugin

import "math"

// sensorScale includes the current sample after a sensor reset and rounds outward.
func sensorScale(low, high, value float64, fallbackMin, fallbackMax int) (int, int) {
	for _, v := range []float64{low, high, value} {
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) >= float64(math.MaxInt)/2 {
			return fallbackMin, fallbackMax
		}
	}
	if low > high {
		return fallbackMin, fallbackMax
	}
	min := int(math.Floor(math.Min(low, value)))
	max := int(math.Ceil(math.Max(high, value)))
	if max <= min {
		max = min + 1
	}
	return min, max
}
