package graph

import (
	_ "embed"
	"golang.org/x/image/font"
)

//go:embed fonts/RobotoCondensed-Bold.ttf
var condensedFontData []byte

var condensedFontManager = &FontFaceManager{fontData: condensedFontData, fontCache: make(map[float64]font.Face)}

// SetCondensedFont selects Roboto Condensed Bold; false preserves DejaVu Sans Bold.
func (g *Graph) SetCondensedFont(enabled bool) { g.condensedFont = enabled }

// SetTextShadow enables a soft oval backdrop using the configured text stroke color.
// It is independent of the existing outline option.
func (g *Graph) SetTextShadow(enabled bool) { g.textShadow = enabled }
