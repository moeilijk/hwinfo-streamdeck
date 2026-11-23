package graph

import (
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
	"image"
	"image/color"
	"testing"
)

func TestCondensedFontAndOvalBackdrop(t *testing.T) {
	face, err := condensedFontManager.GetFaceOfSize(22)
	if err != nil {
		t.Fatal(err)
	}
	if font.MeasureString(face, "5077").Ceil() > 60 {
		t.Fatal("value does not fit a tile")
	}
	img := image.NewRGBA(image.Rect(0, 0, 72, 72))
	text := "FPS"
	point := fixed.Point26_6{X: (fixed.I(72) - font.MeasureString(face, text)) / 2, Y: fixed.I(42)}
	DrawTextShadow(img, face, text, point, color.White)
	if img.RGBAAt(36, 34).A < 200 {
		t.Fatal("missing centered backdrop")
	}
	if img.RGBAAt(0, 0).A != 0 {
		t.Fatal("backdrop escapes its ellipse")
	}
	partial := 0
	for _, a := range img.Pix {
		if a > 0 && a < 200 {
			partial++
		}
	}
	if partial < 100 {
		t.Fatal("backdrop lacks a gradual edge")
	}
}
