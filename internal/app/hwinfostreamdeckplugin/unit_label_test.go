package hwinfostreamdeckplugin

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/moeilijk/hwinfo-streamdeck/pkg/graph"
	hwsensorsservice "github.com/moeilijk/hwinfo-streamdeck/pkg/service"
)

// useRepoFont loads the plugin's DejaVu Sans Bold into the shared manager, so
// the render tests draw text wherever `go test` is started from.
func useRepoFont(t *testing.T) {
	t.Helper()
	ttf, err := os.ReadFile("../../../com.moeilijk.hwinfo.sdPlugin/DejaVuSans-Bold.ttf")
	if err != nil {
		t.Fatalf("read plugin font: %v", err)
	}
	graph.GetSharedFontFaceManager().SetFontData(ttf)
}

var (
	unitTestValueColor = "#ffffff"
	unitTestUnitColor  = "#00a0ff"
)

// readingTileGraph builds the labels of a reading tile the way OnWillAppear
// does, on a black graph without history.
func readingTileGraph(s *actionSettings) *graph.Graph {
	black := &color.RGBA{0, 0, 0, 255}
	g := graph.NewGraph(tileWidth, tileHeight, 0, 100, black, black, black)
	g.SetLabel(0, "", 19, &color.RGBA{183, 183, 183, 255})
	g.SetLabelFontSize(0, 10.5)
	g.SetLabel(1, "", valueBaseline, valueTextColorOf(s))
	g.SetLabelFontSize(1, valueFontSizeOf(s))
	g.SetLabel(2, "", 56, valueTextColorOf(s))
	g.SetLabelFontSize(2, valueFontSizeOf(s))
	g.SetLabel(labelUnit, "", valueBaseline, valueTextColorOf(s))
	g.SetCondensedFont(s.CondensedFont)
	g.SetTextShadow(s.TextShadow)
	applyTextLayout(g, s)
	applyUnitTextColor(g, s)
	return g
}

func renderTile(t *testing.T, g *graph.Graph) *image.RGBA {
	t.Helper()
	b, err := g.EncodePNG()
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	out := image.NewRGBA(img.Bounds())
	for y := 0; y < out.Rect.Dy(); y++ {
		for x := 0; x < out.Rect.Dx(); x++ {
			out.Set(x, y, img.At(x, y))
		}
	}
	return out
}

// rowsOf returns the first and last row with a pixel that is mostly the given
// colour. Small faces have no fully covered pixel, so anti-aliased ones count.
func rowsOf(img *image.RGBA, hex string) (top, bottom int, ok bool) {
	want := *hexToRGBA(hex)
	near := func(c color.RGBA) bool {
		d := func(a, b uint8) int {
			if a > b {
				return int(a - b)
			}
			return int(b - a)
		}
		return d(c.R, want.R) < 90 && d(c.G, want.G) < 90 && d(c.B, want.B) < 90
	}
	top = -1
	for y := 0; y < img.Rect.Dy(); y++ {
		for x := 0; x < img.Rect.Dx(); x++ {
			if near(img.RGBAAt(x, y)) {
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

func TestUnitLabelTexts(t *testing.T) {
	p := &Plugin{}
	for _, tc := range []struct {
		name, mode, format, unit string
		value                    float64
		wantValue, wantUnit      string
	}{
		{"default, no format", "", "", "RPM", 1500, "1500 RPM", ""},
		{"inline, no format", unitDisplayInline, "", "RPM", 1500, "1500 RPM", ""},
		{"inline, percent has no space", unitDisplayInline, "", "%", 42, "42%", ""},
		{"inline, format is used as typed", unitDisplayInline, "%.1f rpm", "RPM", 1500, "1500.0 rpm", ""},
		{"below, no format", unitDisplayBelow, "", "RPM", 1500, "1500", "RPM"},
		{"below, format is used as typed", unitDisplayBelow, "%.1f", "RPM", 1500, "1500.0", "RPM"},
		{"below, reading without unit", unitDisplayBelow, "", "", 7, "7", ""},
		{"hidden, no format", unitDisplayHidden, "", "RPM", 1500, "1500", ""},
		{"hidden, format is used as typed", unitDisplayHidden, "%.0f!", "RPM", 1500, "1500!", ""},
		{"unknown mode behaves as inline", "sideways", "", "RPM", 1500, "1500 RPM", ""},
	} {
		noUnit, display := p.formatDisplayValue(tc.value, tc.unit, tc.format, hwsensorsservice.ReadingType(0))
		value, unit := unitLabelTexts(tc.mode, noUnit, display, tc.unit)
		if value != tc.wantValue || unit != tc.wantUnit {
			t.Errorf("%s: value %q unit %q, want %q and %q", tc.name, value, unit, tc.wantValue, tc.wantUnit)
		}
	}
}

func TestValidUnitDisplay(t *testing.T) {
	for mode, want := range map[string]bool{"inline": true, "below": true, "hidden": true, "": false, "Below": false, "sideways": false} {
		if got := validUnitDisplay(mode); got != want {
			t.Errorf("validUnitDisplay(%q) = %v", mode, got)
		}
	}
}

// The issue's example: a large value with a small unit underneath. The unit is
// drawn below the value, in its own size and colour, at every size combination.
func TestUnitBelowValueRendersUnderIt(t *testing.T) {
	useRepoFont(t)
	for _, condensed := range []bool{false, true} {
		for _, sizes := range [][2]float64{{10.5, 0}, {14, 9}, {20, 8}, {20, 12}, {8, 8}} {
			s := &actionSettings{
				ValueFontSize: sizes[0], UnitFontSize: sizes[1], CondensedFont: condensed,
				UnitDisplay: unitDisplayBelow, ValueTextColor: unitTestValueColor, UnitTextColor: unitTestUnitColor,
			}
			g := readingTileGraph(s)
			g.SetLabelText(1, "1500")
			g.SetLabelText(labelUnit, "RPM")
			img := renderTile(t, g)
			_, valueBottom, okV := rowsOf(img, unitTestValueColor)
			unitTop, unitBottom, okU := rowsOf(img, unitTestUnitColor)
			if !okV || !okU {
				t.Fatalf("condensed=%v sizes %v: value drawn=%v unit drawn=%v", condensed, sizes, okV, okU)
			}
			if unitTop <= valueBottom {
				t.Errorf("condensed=%v sizes %v: unit starts at row %d, value ends at row %d", condensed, sizes, unitTop, valueBottom)
			}
			if unitBottom >= tileHeight {
				t.Errorf("condensed=%v sizes %v: unit runs off the tile (row %d)", condensed, sizes, unitBottom)
			}
		}
	}
}

func TestUnitFollowsValueSizeAndColourUntilSet(t *testing.T) {
	useRepoFont(t)
	height := func(s *actionSettings, hex string) int {
		g := readingTileGraph(s)
		g.SetLabelText(labelUnit, "H")
		top, bottom, ok := rowsOf(renderTile(t, g), hex)
		if !ok {
			t.Fatalf("no unit text in %s for %+v", hex, s)
		}
		return bottom - top + 1
	}
	follow := height(&actionSettings{ValueFontSize: 20, ValueTextColor: unitTestValueColor, UnitDisplay: unitDisplayBelow}, unitTestValueColor)
	own := height(&actionSettings{ValueFontSize: 20, UnitFontSize: 9, ValueTextColor: unitTestValueColor, UnitTextColor: unitTestUnitColor, UnitDisplay: unitDisplayBelow}, unitTestUnitColor)
	if follow < 12 {
		t.Errorf("unit without its own size is %d px high next to a 20 pt value", follow)
	}
	if own >= follow || own > 8 {
		t.Errorf("9 pt unit is %d px high, the 20 pt one %d px", own, follow)
	}
}

func TestOffsetsMoveTheirOwnLabelOnly(t *testing.T) {
	useRepoFont(t)
	rows := func(s *actionSettings) (valueTop, unitTop int) {
		s.UnitDisplay = unitDisplayBelow
		s.ValueTextColor, s.UnitTextColor = unitTestValueColor, unitTestUnitColor
		g := readingTileGraph(s)
		g.SetLabelText(1, "1500")
		g.SetLabelText(labelUnit, "RPM")
		img := renderTile(t, g)
		valueTop, _, _ = rowsOf(img, unitTestValueColor)
		unitTop, _, _ = rowsOf(img, unitTestUnitColor)
		return
	}
	v0, u0 := rows(&actionSettings{})
	if v, u := rows(&actionSettings{ValueOffset: -6}); v != v0-6 || u != u0 {
		t.Errorf("valueOffset -6: value top %d (was %d), unit top %d (was %d)", v, v0, u, u0)
	}
	if v, u := rows(&actionSettings{UnitOffset: 5}); v != v0 || u != u0+5 {
		t.Errorf("unitOffset 5: value top %d (was %d), unit top %d (was %d)", v, v0, u, u0)
	}
	// Line spacing moves the unit along with the lines of the value label.
	if v, u := rows(&actionSettings{LineSpacing: 2}); v != v0 || u <= u0 {
		t.Errorf("lineSpacing 2: value top %d (was %d), unit top %d (was %d)", v, v0, u, u0)
	}
}

// The unit follows the value colour through the threshold path, unless it has
// a colour of its own.
func TestUnitColourFollowsThresholdUnlessSet(t *testing.T) {
	useRepoFont(t)
	p := &Plugin{}
	alert := &Threshold{ValueTextColor: "#ff0000"}
	unitDrawnIn := func(s *actionSettings, apply func(g *graph.Graph), hex string) bool {
		g := readingTileGraph(s)
		g.SetLabelText(labelUnit, "RPM")
		apply(g)
		_, _, ok := rowsOf(renderTile(t, g), hex)
		return ok
	}

	follows := &actionSettings{UnitDisplay: unitDisplayBelow, ValueTextColor: unitTestValueColor}
	if !unitDrawnIn(follows, func(g *graph.Graph) { p.applyThresholdColors(g, alert); applyUnitTextColor(g, follows) }, "#ff0000") {
		t.Error("a unit without its own colour must take the alert's value colour")
	}
	if !unitDrawnIn(follows, func(g *graph.Graph) {
		p.applyThresholdColors(g, alert)
		p.applyNormalColors(g, follows)
	}, unitTestValueColor) {
		t.Error("the unit must return to the value colour when the alert clears")
	}

	own := &actionSettings{UnitDisplay: unitDisplayBelow, ValueTextColor: unitTestValueColor, UnitTextColor: unitTestUnitColor}
	if !unitDrawnIn(own, func(g *graph.Graph) { p.applyThresholdColors(g, alert); applyUnitTextColor(g, own) }, unitTestUnitColor) {
		t.Error("a unit with its own colour must keep it during an alert")
	}
	if !unitDrawnIn(own, func(g *graph.Graph) { p.applyNormalColors(g, own) }, unitTestUnitColor) {
		t.Error("a unit with its own colour must keep it in the normal state")
	}
}

// A tile saved before these options existed has none of the keys, and saving
// it again must not add any.
func TestTypographySettingsStayOutOfUntouchedTiles(t *testing.T) {
	var s actionSettings
	if err := json.Unmarshal([]byte(`{"sensorUid":"/cpu/0","readingId":"7","thresholds":null}`), &s); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(&s)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"unitDisplay", "unitFontSize", "unitOffset", "unitTextColor", "valueOffset", "lineSpacing", "condensedFont", "textShadow", "scaleMode", "graphStyle"} {
		if strings.Contains(string(b), `"`+key+`"`) {
			t.Errorf("untouched tile got %q: %s", key, b)
		}
	}
	// And such a tile renders its value exactly where it always did.
	useRepoFont(t)
	g := readingTileGraph(&s)
	g.SetLabelText(1, "1500 RPM")
	top, bottom, ok := rowsOf(renderTile(t, g), "#ffffff")
	if !ok || bottom != valueBaseline-1 || top < 32 {
		t.Errorf("default value label covers rows %d..%d, want it to end on row %d", top, bottom, valueBaseline-1)
	}
}
