package graph

import (
	"image/color"
	"os"
	"testing"
)

var (
	testBlack = &color.RGBA{0, 0, 0, 255}
	testWhite = &color.RGBA{255, 255, 255, 255}
	testBlue  = &color.RGBA{0, 0, 255, 255}
	testRed   = &color.RGBA{255, 0, 0, 255}
)

// useRepoFont loads the plugin's DejaVu Sans Bold into the shared manager. The
// manager otherwise reads the TTF from the working directory, which in this
// package does not hold it: drawLabel then logs an error and draws no text.
func useRepoFont(t *testing.T) {
	t.Helper()
	ttf, err := os.ReadFile("../../com.moeilijk.hwinfo.sdPlugin/DejaVuSans-Bold.ttf")
	if err != nil {
		t.Fatalf("read plugin font: %v", err)
	}
	GetSharedFontFaceManager().SetFontData(ttf)
}

// labelGraph is a black 72×72 graph without history.
func labelGraph(condensed bool) *Graph {
	g := NewGraph(72, 72, 0, 100, testBlack, testBlack, testBlack)
	g.SetCondensedFont(condensed)
	return g
}

// render draws the labels the way EncodePNG does, and leaves them in g.img.
func render(t *testing.T, g *Graph) {
	t.Helper()
	for i := range g.img.Pix {
		g.img.Pix[i] = 0
	}
	g.drawLabels()
}

// band is the first and last row holding a pixel that is mostly the given
// colour (anti-aliased edges included). ok is false when there is none.
func band(g *Graph, clr *color.RGBA) (top, bottom int, ok bool) {
	top, bottom = -1, -1
	for y := 0; y < g.height; y++ {
		for x := 0; x < g.width; x++ {
			if near(g.img.RGBAAt(x, y), clr) {
				if top < 0 {
					top = y
				}
				bottom = y
				break
			}
		}
	}
	return top, bottom, top >= 0
}

func near(c color.RGBA, want *color.RGBA) bool {
	d := func(a, b uint8) int {
		if a > b {
			return int(a - b)
		}
		return int(b - a)
	}
	return d(c.R, want.R) < 90 && d(c.G, want.G) < 90 && d(c.B, want.B) < 90
}

func count(g *Graph, clr *color.RGBA) int {
	n := 0
	for y := 0; y < g.height; y++ {
		for x := 0; x < g.width; x++ {
			if g.img.RGBAAt(x, y) == *clr {
				n++
			}
		}
	}
	return n
}

var bothFonts = []struct {
	name      string
	condensed bool
}{{"DejaVu Sans Bold", false}, {"Roboto Condensed Bold", true}}

// The repo font really draws: without it every pixel test below would pass on
// an empty image.
func TestLabelsDrawText(t *testing.T) {
	useRepoFont(t)
	for _, f := range bothFonts {
		g := labelGraph(f.condensed)
		g.SetLabel(1, "1500", 44, testWhite)
		g.SetLabelFontSize(1, 10.5)
		render(t, g)
		top, bottom, ok := band(g, testWhite)
		if !ok {
			t.Fatalf("%s: no text drawn", f.name)
		}
		if bottom != 43 || top < 32 || top > 38 {
			t.Errorf("%s: 10.5 pt text at baseline 44 covers rows %d..%d", f.name, top, bottom)
		}
	}
}

// Issue #104: the two lines of "1500\nRPM" overlapped at larger sizes because
// the line advance was a fixed 12 px. It now follows the font's line height.
func TestTwoLineLabelDoesNotOverlap(t *testing.T) {
	useRepoFont(t)
	for _, f := range bothFonts {
		for _, size := range []float64{8, 10.5, 14, 20} {
			line := func(first, second string) (int, int) {
				g := labelGraph(f.condensed)
				g.SetLabel(1, first+"\n"+second, 30, testWhite)
				g.SetLabelFontSize(1, size)
				render(t, g)
				top, bottom, ok := band(g, testWhite)
				if !ok {
					t.Fatalf("%s size %v: no text drawn", f.name, size)
				}
				return top, bottom
			}
			_, firstBottom := line("1500", " ")
			secondTop, _ := line(" ", "1500")
			if secondTop <= firstBottom {
				t.Errorf("%s size %v: line 2 starts at row %d, line 1 ends at row %d", f.name, size, secondTop, firstBottom)
			}
		}
	}
}

func TestLineAdvanceFollowsMetricsAndSpacing(t *testing.T) {
	useRepoFont(t)
	for _, f := range bothFonts {
		for _, size := range []float64{9, 10.5, 20} {
			g := labelGraph(f.condensed)
			height := g.LineHeight(size)
			if height <= 0 {
				t.Fatalf("%s size %v: no line height", f.name, size)
			}
			for _, tc := range []struct {
				spacing float64
				want    int
			}{{0, height}, {1, height}, {0.5, (height + 1) / 2}, {2, 2 * height}} {
				g.SetLineSpacing(tc.spacing)
				if got := g.LineAdvance(size); got != tc.want {
					t.Errorf("%s size %v spacing %v: advance %d, want %d", f.name, size, tc.spacing, got, tc.want)
				}
				// The drawn distance between the two baselines is that advance.
				g.SetLabel(1, "H\nH", 20, testWhite)
				g.SetLabelFontSize(1, size)
				layout, ok := g.layoutLabel(g.labels[1])
				if !ok || len(layout.lines) != 2 {
					t.Fatalf("%s size %v: layout failed", f.name, size)
				}
				if got := (layout.lines[1].point.Y - layout.lines[0].point.Y).Round(); got != tc.want {
					t.Errorf("%s size %v spacing %v: baselines %d px apart, want %d", f.name, size, tc.spacing, got, tc.want)
				}
			}
		}
	}
}

func TestLabelOffsetMovesText(t *testing.T) {
	useRepoFont(t)
	for _, f := range bothFonts {
		at := func(offset float64) (int, int) {
			g := labelGraph(f.condensed)
			g.SetLabel(1, "1500", 44, testWhite)
			g.SetLabelFontSize(1, 10.5)
			if err := g.SetLabelOffset(1, offset); err != nil {
				t.Fatal(err)
			}
			render(t, g)
			top, bottom, _ := band(g, testWhite)
			return top, bottom
		}
		top0, bottom0 := at(0)
		for _, offset := range []float64{-12, -3, 5, 16} {
			top, bottom := at(offset)
			if top != top0+int(offset) || bottom != bottom0+int(offset) {
				t.Errorf("%s offset %v: rows %d..%d, want %d..%d", f.name, offset, top, bottom, top0+int(offset), bottom0+int(offset))
			}
		}
	}
	if err := labelGraph(false).SetLabelOffset(7, 1); err == nil {
		t.Error("SetLabelOffset on a missing label must fail")
	}
}

// A baseline pushed outside the canvas is held at its edge. With the unsigned
// arithmetic drawLabel used before, a negative baseline wrapped to a huge value.
func TestBaselineClampsToCanvas(t *testing.T) {
	useRepoFont(t)
	for _, tc := range []struct {
		offset float64
		want   int
	}{{-1000, 0}, {-45, 0}, {0, 44}, {1000, 72}} {
		g := labelGraph(false)
		g.SetLabel(1, "1500", 44, testWhite)
		g.SetLabelFontSize(1, 10.5)
		g.SetLabelOffset(1, tc.offset)
		layout, ok := g.layoutLabel(g.labels[1])
		if !ok {
			t.Fatal("layout failed")
		}
		if got := layout.lines[0].point.Y.Round(); got != tc.want {
			t.Errorf("offset %v: baseline %d, want %d", tc.offset, got, tc.want)
		}
		render(t, g) // must not panic
	}
}

// The default 10.5 pt geometry of the three existing labels is unchanged, and
// larger faces keep the shift the old unsigned arithmetic happened to produce.
func TestDefaultBaselinesUnchanged(t *testing.T) {
	useRepoFont(t)
	for _, tc := range []struct {
		size float64
		want int
	}{{8, 42}, {9, 43}, {10.5, 44}, {12, 45}, {14, 47}, {16, 49}, {20, 53}} {
		g := labelGraph(false)
		g.SetLabel(1, "1500", 44, testWhite)
		g.SetLabelFontSize(1, tc.size)
		layout, _ := g.layoutLabel(g.labels[1])
		if got := layout.lines[0].point.Y.Round(); got != tc.want {
			t.Errorf("size %v: baseline %d, want %d", tc.size, got, tc.want)
		}
	}
}

// Every backdrop is drawn before any text, so the backdrop of one label cannot
// dim the text of another. Drawing them per label in map order did.
func TestBackdropNeverCoversOtherLabelsText(t *testing.T) {
	useRepoFont(t)
	for _, f := range bothFonts {
		value := func(withUnit bool) int {
			g := labelGraph(f.condensed)
			g.SetTextShadow(true)
			g.SetTextStrokeColor(testRed)
			g.SetLabel(1, "1500", 40, testWhite)
			g.SetLabelFontSize(1, 14)
			g.SetLabel(3, "", 54, testBlue)
			g.SetLabelFontSize(3, 9)
			if withUnit {
				g.SetLabelText(3, "RPM")
			}
			render(t, g)
			return count(g, testWhite)
		}
		alone, together := value(false), value(true)
		if alone == 0 {
			t.Fatalf("%s: no value text drawn", f.name)
		}
		if together != alone {
			t.Errorf("%s: %d solid value pixels with the unit's backdrop, %d without", f.name, together, alone)
		}
	}
}

// Labels of one backdrop group get a single oval around their joint text.
func TestBackdropGroupJoinsLabels(t *testing.T) {
	useRepoFont(t)
	redAt := func(grouped bool, x, y int) uint8 {
		g := labelGraph(false)
		g.SetTextShadow(true)
		g.SetTextStrokeColor(testRed)
		g.SetLabel(1, "5", 36, testWhite)
		g.SetLabelFontSize(1, 14)
		g.SetLabel(3, "W", 54, testWhite)
		g.SetLabelFontSize(3, 9)
		if grouped {
			g.SetLabelBackdropGroup(1, 1)
			g.SetLabelBackdropGroup(3, 1)
		}
		render(t, g)
		return g.img.RGBAAt(x, y).R
	}
	// Left of both short texts, halfway between them: outside two small ovals,
	// inside the one oval around both.
	if got := redAt(false, 14, 44); got != 0 {
		t.Errorf("separate backdrops reach x=14 (red %d)", got)
	}
	if got := redAt(true, 14, 44); got == 0 {
		t.Error("the joint backdrop does not reach x=14")
	}
	if err := labelGraph(false).SetLabelBackdropGroup(7, 1); err == nil {
		t.Error("SetLabelBackdropGroup on a missing label must fail")
	}
}

// Without the option nothing but the text is drawn.
func TestNoBackdropByDefault(t *testing.T) {
	useRepoFont(t)
	g := labelGraph(false)
	g.SetTextStrokeColor(testRed)
	g.SetLabel(1, "1500", 44, testWhite)
	g.SetLabelFontSize(1, 14)
	render(t, g)
	for i := 0; i < len(g.img.Pix); i += 4 {
		if p := g.img.Pix[i : i+3]; p[0] != p[1] || p[1] != p[2] {
			t.Fatalf("coloured pixel %v without a backdrop or stroke", p)
		}
	}
}

// The condensed font is narrower: that is what it is offered for.
func TestCondensedFontIsNarrower(t *testing.T) {
	useRepoFont(t)
	width := func(condensed bool) int {
		g := labelGraph(condensed)
		g.SetLabel(1, "1500 RPM", 44, testWhite)
		g.SetLabelFontSize(1, 12)
		render(t, g)
		left, right := g.width, -1
		for y := 0; y < g.height; y++ {
			for x := 0; x < g.width; x++ {
				if near(g.img.RGBAAt(x, y), testWhite) {
					if x < left {
						left = x
					}
					if x > right {
						right = x
					}
				}
			}
		}
		return right - left + 1
	}
	regular, condensed := width(false), width(true)
	if condensed <= 0 || condensed >= regular {
		t.Errorf("condensed text is %d px wide, regular %d px", condensed, regular)
	}
}
