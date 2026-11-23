package graph

import (
	"image/color"
	"math"
	"testing"
)

func TestAutoRange(t *testing.T) {
	for _, tc := range []struct {
		name     string
		history  []float64
		next     float64
		min, max int
	}{
		{"idle", nil, 0, 0, 1},
		{"temperature", []float64{53, 54}, 55, 51, 57},
		{"headroom", nil, 48, 47, 49},
		{"clock", nil, 4243, 4150, 4350},
		{"fraction", nil, 0.04, 0, 1},
		{"negative", []float64{-20, -5}, -10, -25, 0},
		{"mixed", []float64{-10, 20}, 15, -20, 30},
		{"retain peak", []float64{100, 5}, 4, 0, 150},
		{"expire peak", []float64{100, 5, 4}, 3, 2, 6},
		{"new peak", []float64{5, 4, 3}, 150, 0, 200},
		{"invalid", []float64{math.NaN(), math.Inf(1)}, 10, 9, 11},
		{"overflow", nil, math.MaxFloat64, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := &Graph{width: 3, values: tc.history}
			min, max := g.AutoRange(tc.next)
			if min != tc.min || max != tc.max {
				t.Fatalf("got %d..%d, want %d..%d", min, max, tc.min, tc.max)
			}
		})
	}
}

func TestRangeChangeReprojectsHistory(t *testing.T) {
	g := NewGraph(72, 72, 0, 100, &color.RGBA{}, &color.RGBA{}, &color.RGBA{})
	for _, v := range []float64{0, 100, 50} {
		g.Update(v)
	}
	g.SetRange(0, 200)
	want := []uint8{0, 36, 18}
	for i, y := range g.Series() {
		if y != want[i] {
			t.Fatalf("sample %d: %d != %d", i, y, want[i])
		}
	}
	g.Clear()
	if len(g.values) != 0 {
		t.Fatal("Clear retained automatic range history")
	}
}

// A value outside the range is drawn at the nearest edge. Without the clamp the
// row was stored unclamped in a uint8, so a value far above the maximum wrapped
// around and came back as a low line after a redraw.
func TestValuesOutsideRangeClampToEdge(t *testing.T) {
	for _, tc := range []struct {
		value float64
		want  uint8
	}{
		{-10, 0},
		{0, 0},
		{50, 36},
		{100, 71},
		{150, 71},
		{400, 71},
		{math.NaN(), 0},
	} {
		g := NewGraph(72, 72, 0, 100, &color.RGBA{}, &color.RGBA{}, &color.RGBA{})
		g.Update(tc.value)
		if got := g.Series()[0]; got != tc.want {
			t.Errorf("value %v: row %d, want %d", tc.value, got, tc.want)
		}
	}
}

// An empty or inverted range cannot place a value. It draws at the bottom
// instead of dividing by zero.
func TestDegenerateRangeDrawsAtBottom(t *testing.T) {
	for _, r := range [][2]int{{50, 50}, {100, 0}} {
		g := NewGraph(72, 72, r[0], r[1], &color.RGBA{}, &color.RGBA{}, &color.RGBA{})
		g.Update(75)
		if got := g.Series()[0]; got != 0 {
			t.Errorf("range %d..%d: row %d, want 0", r[0], r[1], got)
		}
	}
}

// SetMin and SetMax (used by the composite, derived and dial tiles) reproject
// the recorded samples the same way SetRange does.
func TestSetMinSetMaxReprojectHistory(t *testing.T) {
	g := NewGraph(72, 72, 0, 100, &color.RGBA{}, &color.RGBA{}, &color.RGBA{})
	for _, v := range []float64{0, 100, 50} {
		g.Update(v)
	}
	g.SetMin(50)
	g.SetMax(150)
	want := []uint8{0, 36, 0}
	for i, y := range g.Series() {
		if y != want[i] {
			t.Fatalf("sample %d: row %d, want %d", i, y, want[i])
		}
	}
}
