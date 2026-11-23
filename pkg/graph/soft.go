package graph

import (
	"image/color"
	"math"
)

// softSample applies a five-sample binomial filter to the rendered history only.
// Raw readings and labels remain unchanged. Repeated edge samples avoid fading
// a constant signal toward zero at either end of the history.
func (g *Graph) softSample(index int) float64 {
	if len(g.yvals) == 0 {
		return 0
	}
	weights := [...]float64{1, 4, 6, 4, 1}
	value := 0.0
	for offset, weight := range weights {
		i := max(0, min(len(g.yvals)-1, index+offset-2))
		value += float64(g.yvals[i]) * weight
	}
	return value / 16
}

// softY interpolates the filtered samples with a continuous, monotone slope.
// Three pixels per sample keep history readable.
func (g *Graph) softY(x float64) float64 {
	const spacing = 3.0
	position := float64(len(g.yvals)-1) - (float64(g.width-1)-x)/spacing
	if position <= 0 {
		return g.softSample(0)
	}
	i := int(position)
	if i >= len(g.yvals)-1 {
		return g.softSample(len(g.yvals) - 1)
	}
	t := position - float64(i)
	y0, y1 := g.softSample(i), g.softSample(i+1)
	delta := y1 - y0
	m0, m1 := 0.0, 0.0
	if i > 0 {
		m0 = monotoneSlope(y0-g.softSample(i-1), delta)
	}
	if i+2 < len(g.yvals) {
		m1 = monotoneSlope(delta, g.softSample(i+2)-y1)
	}
	t2, t3 := t*t, t*t*t
	return (2*t3-3*t2+1)*y0 + (t3-2*t2+t)*m0 + (-2*t3+3*t2)*y1 + (t3-t2)*m1
}

func monotoneSlope(left, right float64) float64 {
	if left*right <= 0 {
		return 0
	}
	return 2 * left * right / (left + right)
}

func mix(base, ink color.RGBA, opacity float64) color.RGBA {
	a := math.Max(0, math.Min(1, opacity)) * float64(ink.A) / 255
	return color.RGBA{
		uint8(math.Round(float64(base.R)*(1-a) + float64(ink.R)*a)),
		uint8(math.Round(float64(base.G)*(1-a) + float64(ink.G)*a)),
		uint8(math.Round(float64(base.B)*(1-a) + float64(ink.B)*a)), 255,
	}
}

// loadColor maps a fraction of the configured scale to saturated traffic-light colors.
// Every historical sample keeps its own color instead of recoloring the whole chart.
func loadColor(fraction float64) color.RGBA {
	green := color.RGBA{0, 255, 24, 255}
	yellow := color.RGBA{255, 240, 0, 255}
	red := color.RGBA{255, 0, 0, 255}
	// Keep the green range closer to the baseline and reach yellow halfway up.
	const yellowPoint = 0.5
	// Reach pure red earlier so the upper band stays vivid rather than orange.
	const redPoint = 0.85
	fraction = math.Max(0, math.Min(1, fraction))
	if fraction <= yellowPoint {
		return mix(green, yellow, fraction/yellowPoint)
	}
	return mix(yellow, red, (fraction-yellowPoint)/(redPoint-yellowPoint))
}

func (g *Graph) drawSoftGraph() {
	if len(g.yvals) == 0 {
		return
	}
	bg, fill, line := g.BackgroundColor(), g.ForegroundColor(), g.HighlightColor()
	thickness := math.Max(1.2, float64(g.lineThickness))
	fillOpacity, fillPower := 0.55, 2.0
	if g.style == "soft-load" {
		thickness = math.Max(1.6, thickness)
		fillOpacity, fillPower = 0.95, 1.2
	}
	bottom := float64(g.height) - 3
	for x := 0; x < g.width; x++ {
		level := math.Max(0, math.Min(float64(g.effectiveHeight()-1), g.softY(float64(x))))
		if g.style == "soft-load" {
			line = loadColor(level / math.Max(1, float64(g.effectiveHeight()-1)))
		}
		yline := math.Max(2, bottom-level)
		// Account for the slope so diagonal strokes retain a consistent width.
		slope := (g.softY(float64(x)+0.5) - g.softY(float64(x)-0.5))
		for y := 0; y < g.height; y++ {
			pixel := bg
			if float64(y) >= yline && float64(y) <= bottom {
				fade := 1 - (float64(y)-yline)/math.Max(1, bottom-yline)
				if g.style == "soft-load" {
					// A fixed vertical gradient, clipped by the curve. Neither hue
					// nor opacity depends on the sample's height.
					fade = (bottom - float64(y)) / math.Max(1, float64(g.effectiveHeight()-1))
					fill = loadColor(fade)
				}
				pixel = mix(pixel, fill, fillOpacity*math.Pow(fade, fillPower))
			}
			distance := math.Abs(float64(y)-yline) / math.Sqrt(1+slope*slope)
			pixel = mix(pixel, line, 0.09*math.Exp(-distance*distance/6))
			pixel = mix(pixel, line, math.Max(0, math.Min(1, thickness/2+0.5-distance)))
			g.img.SetRGBA(x, y, pixel)
		}
	}
}
