package graph

import (
	"image/color"
	"math"
	"testing"
)

func TestSoftCurveDoesNotOvershoot(t *testing.T) {
	g := NewGraph(72, 72, 0, 100, &color.RGBA{160, 160, 160, 255}, &color.RGBA{0, 0, 0, 255}, &color.RGBA{220, 220, 220, 255})
	g.SetStyle("soft")
	for _, v := range []float64{0, 100, 0, 100, 0, 50} {
		g.Update(v)
	}
	for x := 0.0; x < 72; x += 0.1 {
		y := g.softY(x)
		if y < 0 || y > 71 || math.IsNaN(y) {
			t.Fatalf("curve exceeds scale at %f: %f", x, y)
		}
	}
}

func TestSoftCurveKeepsSlopeAcrossSamples(t *testing.T) {
	g := &Graph{width: 25, yvals: []uint8{0, 10, 20, 30, 40, 50, 60, 70, 80}}
	const epsilon = 0.001
	left := (g.softY(12) - g.softY(12-epsilon)) / epsilon
	right := (g.softY(12+epsilon) - g.softY(12)) / epsilon
	if math.Abs(left-10.0/3) > 0.01 || math.Abs(right-left) > 0.01 {
		t.Fatalf("rising line should not flatten at samples: slopes %f, %f", left, right)
	}
	// Interpolation must stay within neighbouring filtered samples.
	g.width = 13
	g.yvals = []uint8{0, 60, 61, 10, 40}
	for x := 0.0; x < 12; x += 0.01 {
		i := int(x / 3)
		a, b := g.softSample(i), g.softSample(i+1)
		if y := g.softY(x); y < math.Min(a, b)-1e-9 || y > math.Max(a, b)+1e-9 {
			t.Fatalf("curve overshot neighbouring samples at %f: %f", x, y)
		}
	}
}

func TestSoftFilterReducesSpikesWithoutChangingHistory(t *testing.T) {
	g := &Graph{width: 25, yvals: []uint8{0, 0, 0, 0, 80, 0, 0, 0, 0}}
	if peak := g.softY(12); peak >= 40 || peak <= 0 {
		t.Fatalf("isolated spike was not softened: %f", peak)
	}
	if g.softY(9) <= 0 || g.softY(15) <= 0 {
		t.Fatal("spike should spread into a rounded bump")
	}
	if g.yvals[4] != 80 || g.yvals[3] != 0 {
		t.Fatal("render smoothing changed raw history")
	}
	g.yvals = []uint8{60, 60, 60, 60, 60}
	for i := range g.yvals {
		if g.softSample(i) != 60 {
			t.Fatal("smoothing changed a sustained value")
		}
	}
}

func TestSoftFillFadesToBackground(t *testing.T) {
	g := NewGraph(72, 72, 0, 100, &color.RGBA{160, 160, 160, 255}, &color.RGBA{0, 0, 0, 255}, &color.RGBA{220, 220, 220, 255})
	g.SetStyle("soft")
	g.Update(50)
	if g.img.RGBAAt(35, 40).R <= g.img.RGBAAt(35, 65).R {
		t.Fatal("fill should fade toward baseline")
	}
	if g.img.RGBAAt(35, 10).R != 0 {
		t.Fatal("upper background should remain clear")
	}
}

func TestLoadColorScale(t *testing.T) {
	for _, tc := range []struct {
		fraction float64
		want     color.RGBA
	}{
		{-1, color.RGBA{0, 255, 24, 255}},
		{0, color.RGBA{0, 255, 24, 255}},
		{0.25, color.RGBA{128, 248, 12, 255}},
		{0.5, color.RGBA{255, 240, 0, 255}},
		{0.85, color.RGBA{255, 0, 0, 255}},
		{1, color.RGBA{255, 0, 0, 255}},
		{2, color.RGBA{255, 0, 0, 255}},
	} {
		if got := loadColor(tc.fraction); got != tc.want {
			t.Errorf("loadColor(%v) = %v, want %v", tc.fraction, got, tc.want)
		}
	}
}

func TestSpectrumFillIsIndependentOfCurveHeight(t *testing.T) {
	render := func(value float64) *Graph {
		g := NewGraph(72, 72, 0, 100, &color.RGBA{160, 160, 160, 255}, &color.RGBA{0, 0, 0, 255}, &color.RGBA{220, 220, 220, 255})
		g.SetStyle("soft-load")
		g.Update(value)
		return g
	}
	low, high := render(60), render(100)
	for y := 45; y <= 69; y++ {
		if low.img.RGBAAt(35, y) != high.img.RGBAAt(35, y) {
			t.Fatalf("gradient changed with curve height at y=%d", y)
		}
	}
	if low.img.RGBAAt(35, 10).R != 0 || high.img.RGBAAt(35, 10).R == 0 {
		t.Fatal("curve must clip the fixed gradient")
	}
}
