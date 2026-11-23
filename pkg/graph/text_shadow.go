package graph

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// DrawTextShadow draws a centered oval backdrop with a broad, soft edge.
// The backdrop follows the text bounds rather than individual glyphs.
func DrawTextShadow(dst draw.Image, face font.Face, text string, point fixed.Point26_6, clr color.Color) {
	bounds, _ := font.BoundString(face, text)
	if bounds.Empty() {
		return
	}
	bounds.Min = bounds.Min.Add(point)
	bounds.Max = bounds.Max.Add(point)
	drawOvalBackdrop(dst, bounds, clr)
}

// drawOvalBackdrop draws the soft oval around the given text bounds.
func drawOvalBackdrop(dst draw.Image, bounds fixed.Rectangle26_6, clr color.Color) {
	left, top := float64(bounds.Min.X)/64, float64(bounds.Min.Y)/64
	right, bottom := float64(bounds.Max.X)/64, float64(bounds.Max.Y)/64
	cx, cy := (left+right)/2, (top+bottom)/2
	ry := (bottom-top)/2 + 8
	rx := math.Max((right-left)/2+12, ry*1.35)
	r := image.Rect(int(math.Floor(cx-rx)), int(math.Floor(cy-ry)), int(math.Ceil(cx+rx)), int(math.Ceil(cy+ry))).Intersect(dst.Bounds())
	if r.Empty() {
		return
	}
	mask := image.NewAlpha(r)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			dx, dy := (float64(x)+0.5-cx)/rx, (float64(y)+0.5-cy)/ry
			distance := math.Sqrt(dx*dx + dy*dy)
			// Solid center, then a smooth fade over most of the ellipse radius.
			fade := math.Max(0, math.Min(1, (distance-0.3)/0.7))
			opacity := 0.82 * (1 - fade*fade*(3-2*fade))
			mask.SetAlpha(x, y, color.Alpha{A: uint8(math.Round(255 * opacity))})
		}
	}
	draw.DrawMask(dst, r, image.NewUniform(clr), image.Point{}, mask, r.Min, draw.Over)
}
