package graph

import "math"

// AutoRange fits both bounds to the next sample and retained history with padding.
// Old peaks stop affecting the range when they leave history.
// It does not mutate the configured manual range or the stored readings.
func (g *Graph) AutoRange(next float64) (int, int) {
	low, high := math.Inf(1), math.Inf(-1)
	include := func(value float64) {
		// Leave enough space for padding and outward rounding before int conversion.
		if math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) >= float64(math.MaxInt)/16 {
			return
		}
		low = math.Min(low, value)
		high = math.Max(high, value)
	}
	start := 0
	if len(g.values) >= g.width && len(g.values) > 0 {
		start = 1 // Update will discard this sample.
	}
	for _, value := range g.values[start:] {
		include(value)
	}
	include(next)
	if math.IsInf(low, 1) {
		return 0, 1
	}
	// Keep small fluctuations readable without exaggerating sensor noise.
	padding := math.Max(0.1, math.Max((high-low)*0.1, math.Max(math.Abs(low), math.Abs(high))*0.02))
	lower, upper := low-padding, high+padding
	if low >= 0 {
		lower = math.Max(0, lower)
	}
	if high <= 0 && low < 0 {
		upper = math.Min(0, upper)
	}
	step := autoScaleStep((upper - lower) / 5)
	min, max := int(math.Floor(lower/step)*step), int(math.Ceil(upper/step)*step)
	if min == max {
		max = min + 1
	}
	return min, max
}

func autoScaleStep(value float64) float64 {
	value = math.Max(1, value)
	step := math.Pow(10, math.Floor(math.Log10(value)))
	for _, multiplier := range []float64{1, 2, 5, 10} {
		bound := multiplier * step
		if bound >= value {
			return bound
		}
	}
	return step * 10
}
