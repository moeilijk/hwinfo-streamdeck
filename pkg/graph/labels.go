package graph

import (
	"fmt"
	"image"
	"log"
	"math"
	"sort"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// defaultLineHeight is the line height of the default 10.5 pt label. A label's
// y is the baseline of such a label. Larger and smaller faces are shifted by
// the difference so their text stays in roughly the same band.
const defaultLineHeight = 10.5

// labelLine is one line of a label, positioned on the canvas.
type labelLine struct {
	text  string
	point fixed.Point26_6
}

// labelLayout is a label resolved to a face and positioned lines.
type labelLayout struct {
	label *Label
	face  font.Face
	lines []labelLine
}

// fontManager returns the manager of the font the graph draws its labels with.
func (g *Graph) fontManager() *FontFaceManager {
	if g.condensedFont {
		return condensedFontManager
	}
	return shared().fontFaceManager
}

// LineHeight is the line height in pixels of a label of the given font size in
// the font the graph currently draws with. It returns 0 when the font cannot be
// loaded.
func (g *Graph) LineHeight(fontSize float64) int {
	face, err := g.fontManager().GetFaceOfSize(fontSize)
	if err != nil {
		return 0
	}
	return face.Metrics().Height.Round()
}

// LineAdvance is the distance in pixels between the baselines of two lines of
// the given font size: the line height times the line spacing.
func (g *Graph) LineAdvance(fontSize float64) int {
	return lineAdvance(g.LineHeight(fontSize), g.lineSpacing)
}

func lineAdvance(lineHeight int, spacing float64) int {
	if spacing <= 0 {
		spacing = 1
	}
	advance := int(math.Round(float64(lineHeight) * spacing))
	if advance < 1 {
		advance = 1
	}
	return advance
}

// SetLineSpacing sets the multiplier on the font line height that separates
// the lines of a multi-line label. 0 or less selects the default of 1.
func (g *Graph) SetLineSpacing(spacing float64) {
	if spacing <= 0 {
		spacing = 0
	}
	g.lineSpacing = spacing
}

// SetLabelOffset shifts the baseline of a pre-set label by dy pixels. Negative
// moves the text up. Half pixels are honoured.
func (g *Graph) SetLabelOffset(key int, dy float64) error {
	l, ok := g.labels[key]
	if !ok {
		return fmt.Errorf("Label with key (%d) does not exist", key)
	}
	if math.IsNaN(dy) || math.IsInf(dy, 0) {
		dy = 0
	}
	l.offset = dy
	return nil
}

// SetLabelBackdropGroup makes a pre-set label share one text backdrop with the
// other labels of the same non-zero group. 0 gives it a backdrop of its own.
func (g *Graph) SetLabelBackdropGroup(key, group int) error {
	l, ok := g.labels[key]
	if !ok {
		return fmt.Errorf("Label with key (%d) does not exist", key)
	}
	l.backdropGroup = group
	return nil
}

// layoutLabel resolves a label to its face and the position of every line.
func (g *Graph) layoutLabel(l *Label) (labelLayout, bool) {
	face, err := g.fontManager().GetFaceOfSize(l.fontSize)
	if err != nil {
		log.Printf("drawLabel font: %v", err)
		return labelLayout{}, false
	}
	lineHeight := face.Metrics().Height.Round()
	// Signed arithmetic: an offset may push the baseline above the canvas, and
	// that has to clip instead of wrapping around to a huge unsigned value.
	baseline := float64(int(l.y)+int(float64(lineHeight)-defaultLineHeight)) + l.offset
	baseline = math.Max(0, math.Min(float64(g.height), baseline))
	advance := lineAdvance(lineHeight, g.lineSpacing)

	layout := labelLayout{label: l, face: face}
	for i, line := range newlineRegex.Split(printableLabelText(l.text), -1) {
		var lwidth float64
		for _, x := range line {
			awidth, ok := safeGlyphAdvance(face, rune(x))
			if !ok {
				log.Println("drawLabel: Failed to GlyphAdvance")
				return labelLayout{}, false
			}
			lwidth += unfix(awidth)
		}
		lx := (float64(g.width) / 2.) - (lwidth / 2.)
		y := baseline + float64(i*advance)
		layout.lines = append(layout.lines, labelLine{
			text:  line,
			point: fixed.Point26_6{X: fixed.Int26_6(lx * 64), Y: fixed.Int26_6(math.Round(y * 64))},
		})
	}
	return layout, true
}

// drawLabels draws every label onto the image. Labels are drawn in key order,
// and all text backdrops go down before any text, so the backdrop of one label
// never covers the text of another.
func (g *Graph) drawLabels() {
	keys := make([]int, 0, len(g.labels))
	for key := range g.labels {
		keys = append(keys, key)
	}
	sort.Ints(keys)

	layouts := make([]labelLayout, 0, len(keys))
	for _, key := range keys {
		if layout, ok := g.layoutLabel(g.labels[key]); ok {
			layouts = append(layouts, layout)
		}
	}
	if g.textShadow {
		g.drawBackdrops(layouts)
	}
	for _, layout := range layouts {
		g.drawLabelText(layout)
	}
}

// outlineColor is the colour of the text stroke and of the text backdrop.
func (g *Graph) outlineColor() *image.Uniform {
	if g.textStrokeColor != nil {
		return image.NewUniform(g.textStrokeColor)
	}
	return image.NewUniform(g.bgColor)
}

// drawBackdrops draws one soft oval behind the text of every label, or behind
// the joint text of the labels that share a backdrop group.
func (g *Graph) drawBackdrops(layouts []labelLayout) {
	var order []int
	bounds := map[int]fixed.Rectangle26_6{}
	for i, layout := range layouts {
		group := layout.label.backdropGroup
		if group == 0 {
			group = -(i + 1) // a group of its own
		}
		for _, line := range layout.lines {
			b, _ := font.BoundString(layout.face, line.text)
			if b.Empty() {
				continue
			}
			b.Min, b.Max = b.Min.Add(line.point), b.Max.Add(line.point)
			if have, ok := bounds[group]; ok {
				b = have.Union(b)
			} else {
				order = append(order, group)
			}
			bounds[group] = b
		}
	}
	clr := g.outlineColor()
	for _, group := range order {
		drawOvalBackdrop(g.img, bounds[group], clr)
	}
}

// drawLabelText draws the stroke and the text of a positioned label.
func (g *Graph) drawLabelText(layout labelLayout) {
	textSrc := image.NewUniform(layout.label.clr)
	for _, line := range layout.lines {
		d := &font.Drawer{Dst: g.img, Src: textSrc, Face: layout.face, Dot: line.point}
		if g.textStroke {
			d.Src = g.outlineColor()
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if dx == 0 && dy == 0 {
						continue
					}
					d.Dot = fixed.Point26_6{
						X: line.point.X + fixed.Int26_6(dx*64),
						Y: line.point.Y + fixed.Int26_6(dy*64),
					}
					safeDrawString(d, line.text)
				}
			}
			d.Src = textSrc
			d.Dot = line.point
		}
		safeDrawString(d, line.text)
	}
}
