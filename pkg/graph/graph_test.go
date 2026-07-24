package graph

import (
	"image/color"
	"testing"
)

func TestGraphSupportsNonSquareCanvas(t *testing.T) {
	fg := &color.RGBA{0, 81, 40, 255}
	bg := &color.RGBA{0, 0, 0, 255}
	hl := &color.RGBA{0, 158, 0, 255}

	g := NewGraph(200, 100, 0, 100, fg, bg, hl)
	g.SetLabel(0, "Dial", 24, &color.RGBA{255, 255, 255, 255})
	g.Update(50)

	if _, err := g.EncodePNG(); err != nil {
		t.Fatalf("EncodePNG failed: %v", err)
	}
}

func TestGraphLabelRenderingHandlesUnsafeRunes(t *testing.T) {
	fg := &color.RGBA{0, 81, 40, 255}
	bg := &color.RGBA{0, 0, 0, 255}
	hl := &color.RGBA{0, 158, 0, 255}

	g := NewGraph(200, 100, 0, 100, fg, bg, hl)
	g.SetLabel(0, "CPU\x00 Core \ufffd", 24, &color.RGBA{255, 255, 255, 255})
	g.Update(50)

	if _, err := g.EncodePNG(); err != nil {
		t.Fatalf("EncodePNG failed: %v", err)
	}
}
