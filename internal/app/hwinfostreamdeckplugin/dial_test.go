package hwinfostreamdeckplugin

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/moeilijk/hwinfo-streamdeck/pkg/graph"
	hwsensorsservice "github.com/moeilijk/hwinfo-streamdeck/pkg/service"
)

func TestWrapDialIndex(t *testing.T) {
	tests := []struct {
		name    string
		current int
		ticks   int
		count   int
		want    int
	}{
		{name: "clockwise one", current: 0, ticks: 1, count: 4, want: 1},
		{name: "counter clockwise wraps", current: 0, ticks: -1, count: 4, want: 3},
		{name: "multi tick clockwise wraps", current: 2, ticks: 5, count: 4, want: 3},
		{name: "multi tick counter clockwise wraps", current: 1, ticks: -6, count: 4, want: 3},
		{name: "empty pages", current: 3, ticks: 1, count: 0, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := wrapDialIndex(tt.current, tt.ticks, tt.count); got != tt.want {
				t.Fatalf("wrapDialIndex(%d, %d, %d) = %d, want %d", tt.current, tt.ticks, tt.count, got, tt.want)
			}
		})
	}
}

// #56 feedback item 9: a reverse-direction toggle flips the encoder ticks, so the
// same clockwise turn steps to the previous page instead of the next.
func TestDialRotateTicks(t *testing.T) {
	if got := dialRotateTicks(1, false); got != 1 {
		t.Fatalf("normal +1 tick = %d, want 1", got)
	}
	if got := dialRotateTicks(1, true); got != -1 {
		t.Fatalf("reversed +1 tick = %d, want -1", got)
	}
	if got := dialRotateTicks(-3, true); got != 3 {
		t.Fatalf("reversed -3 ticks = %d, want 3", got)
	}
	// Composed with wrapping: a clockwise turn under reverse lands on the previous page.
	if got := wrapDialIndex(0, dialRotateTicks(1, true), 4); got != 3 {
		t.Fatalf("reversed clockwise from 0 = %d, want 3 (previous page)", got)
	}
}

func TestDialOverviewIndices(t *testing.T) {
	tests := []struct {
		name   string
		active int
		count  int
		limit  int
		want   []int
	}{
		{name: "empty", active: 0, count: 0, limit: 3, want: nil},
		{name: "single", active: 0, count: 1, limit: 3, want: []int{0}},
		{name: "two", active: 1, count: 2, limit: 3, want: []int{0, 1}},
		{name: "middle", active: 2, count: 5, limit: 3, want: []int{1, 2, 3}},
		{name: "wrap left", active: 0, count: 5, limit: 3, want: []int{4, 0, 1}},
		{name: "wrap right", active: 4, count: 5, limit: 3, want: []int{3, 4, 0}},
		// Capping a larger set to two cards: active first, next second.
		{name: "cap2 middle", active: 2, count: 5, limit: 2, want: []int{2, 3}},
		{name: "cap2 wrap right", active: 4, count: 5, limit: 2, want: []int{4, 0}},
		// Cap never shrinks the natural single/two-page windows.
		{name: "cap2 single", active: 0, count: 1, limit: 2, want: []int{0}},
		{name: "cap2 two", active: 1, count: 2, limit: 2, want: []int{0, 1}},
		// A bad limit falls back to the two-card minimum, not zero cards.
		{name: "limit0 caps to two", active: 2, count: 5, limit: 0, want: []int{2, 3}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dialOverviewIndices(tt.active, tt.count, tt.limit)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d (%v)", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("overview indices = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestDialOverviewRectsKeepPreviewAspect(t *testing.T) {
	for _, count := range []int{1, 2, 3} {
		rects := dialOverviewRects(count)
		if len(rects) != count {
			t.Fatalf("count %d returned %d rects", count, len(rects))
		}
		for _, rect := range rects {
			inner := rect.Inset(3)
			if inner.Dx() != 2*inner.Dy() {
				t.Fatalf("inner rect %v ratio = %d:%d, want 2:1", inner, inner.Dx(), inner.Dy())
			}
		}
		for i := 1; i < len(rects); i++ {
			if rects[i-1].Max.X > rects[i].Min.X {
				t.Fatalf("overview rects overlap: %v then %v", rects[i-1], rects[i])
			}
		}
	}
	rects := dialOverviewRects(3)
	active := rects[1]
	if active.Min.X+active.Dx()/2 != dialWidth/2 {
		t.Fatalf("active overview rect %v is not centered on the dial canvas", active)
	}
}

func TestCenteredAspectCrop(t *testing.T) {
	src := image.Rect(0, 0, 200, 100)
	if got := centeredAspectCrop(src, image.Rect(0, 0, 100, 50)); got != src {
		t.Fatalf("same ratio crop = %v, want %v", got, src)
	}
	if got := centeredAspectCrop(src, image.Rect(0, 0, 100, 100)); got != image.Rect(50, 0, 150, 100) {
		t.Fatalf("square target crop = %v, want centered width crop", got)
	}
	tallSrc := image.Rect(0, 0, 100, 100)
	if got := centeredAspectCrop(tallSrc, image.Rect(0, 0, 200, 100)); got != image.Rect(0, 25, 100, 75) {
		t.Fatalf("wide target crop = %v, want centered height crop", got)
	}
}

func TestEmaSmooth(t *testing.T) {
	tests := []struct {
		name         string
		alpha, value float64
		prev, want   float64
	}{
		{name: "alpha 1 follows value", alpha: 1, value: 50, prev: 10, want: 50},
		{name: "alpha 0.5 halfway", alpha: 0.5, value: 100, prev: 0, want: 50},
		{name: "alpha 0.2 mostly prev", alpha: 0.2, value: 100, prev: 0, want: 20},
		{name: "stable when equal", alpha: 0.3, value: 42, prev: 42, want: 42},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := emaSmooth(tt.alpha, tt.value, tt.prev)
			if got != tt.want {
				t.Fatalf("emaSmooth(%v,%v,%v) = %v, want %v", tt.alpha, tt.value, tt.prev, got, tt.want)
			}
		})
	}
}

func TestDecodeDialSettings(t *testing.T) {
	raw := json.RawMessage(`{"activeIndex":5,"pages":[{},{},{}]}`)
	s, err := decodeDialSettings(&raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if s.ActiveIndex != 2 {
		t.Errorf("activeIndex = %d, want 2 (5 mod 3)", s.ActiveIndex)
	}
	neg := json.RawMessage(`{"activeIndex":-1,"pages":[{}]}`)
	if s2, _ := decodeDialSettings(&neg); s2.ActiveIndex != 0 {
		t.Errorf("negative activeIndex = %d, want 0", s2.ActiveIndex)
	}
	if s3, _ := decodeDialSettings(nil); s3.ActiveIndex != 0 || len(s3.Pages) != 0 {
		t.Errorf("nil raw should decode empty, got %+v", s3)
	}
}

func TestDialColor(t *testing.T) {
	def := color.RGBA{1, 2, 3, 255}
	if c := dialColor("#ff0000", def); c == nil || c.R != 255 || c.G != 0 || c.B != 0 {
		t.Errorf("valid hex = %+v, want red", c)
	}
	if c := dialColor("", def); c == nil || *c != def {
		t.Errorf("empty hex = %+v, want default %+v", c, def)
	}
}

func TestDialDefaultPageColors(t *testing.T) {
	firstFG, firstHL := dialDefaultPageColors(0)
	secondFG, secondHL := dialDefaultPageColors(1)

	if firstFG == "" || firstHL == "" {
		t.Fatalf("first page colors must be set")
	}
	if firstFG == secondFG || firstHL == secondHL {
		t.Fatalf("second page should get a different default color")
	}
	seen := make(map[string]bool)
	for i := 0; i < 16; i++ {
		fg, hl := dialDefaultPageColors(i)
		key := fg + "|" + hl
		if seen[key] {
			t.Fatalf("page color %d repeated too early: %s", i, key)
		}
		seen[key] = true
	}
	wrappedFG, wrappedHL := dialDefaultPageColors(16)
	if wrappedFG != firstFG || wrappedHL != firstHL {
		t.Fatalf("palette wrap = (%s,%s), want first (%s,%s)", wrappedFG, wrappedHL, firstFG, firstHL)
	}
}

func TestDefaultDialFontSizes(t *testing.T) {
	if got := defaultDialTitleFontSize(0); got != 14 {
		t.Errorf("title 0 -> %v, want 14", got)
	}
	if got := defaultDialTitleFontSize(9); got != 9 {
		t.Errorf("title 9 -> %v, want 9", got)
	}
	if got := defaultDialValueFontSize(0); got != 18 {
		t.Errorf("value 0 -> %v, want 18", got)
	}
	if got := defaultDialValueFontSize(22); got != 22 {
		t.Errorf("value 22 -> %v, want 22", got)
	}
}

func TestDialSeparatorDynamicWidthAndColor(t *testing.T) {
	bg := color.RGBA{20, 120, 30, 255}
	sep := color.RGBA{200, 30, 30, 255}
	fixture := func() []byte {
		img := image.NewRGBA(image.Rect(0, 0, dialWidth, dialHeight))
		fillRect(img, img.Bounds(), bg)
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatalf("encode fixture: %v", err)
		}
		return buf.Bytes()
	}
	at := func(b []byte, x, y int) color.RGBA {
		im, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		return color.RGBAModel.Convert(im.At(x, y)).(color.RGBA)
	}
	y := dialHeight / 2

	// width 0 = off: edges keep the original graph pixels.
	off, err := decorateDialImage(fixture(), 0, 1, false, "auto", 0, sep, dialIndicatorDefaultColor, dialIndicatorDefaultSize)
	if err != nil {
		t.Fatalf("decorate off: %v", err)
	}
	if at(off, 0, y) != bg || at(off, dialWidth-1, y) != bg {
		t.Fatalf("width 0 must not draw a separator")
	}

	// width 5 + color: a 5px band of that color on each edge, center untouched.
	on, err := decorateDialImage(fixture(), 0, 1, false, "auto", 5, sep, dialIndicatorDefaultColor, dialIndicatorDefaultSize)
	if err != nil {
		t.Fatalf("decorate on: %v", err)
	}
	if at(on, 0, y) != sep || at(on, 4, y) != sep || at(on, dialWidth-1, y) != sep || at(on, dialWidth-5, y) != sep {
		t.Fatalf("width 5 must draw a 5px colored band on both edges")
	}
	if at(on, 5, y) != bg || at(on, dialWidth-6, y) != bg {
		t.Fatalf("separator band must be exactly 5px wide")
	}
	if at(on, dialWidth/2, y) != bg {
		t.Fatalf("center must not be overwritten by the separator")
	}
}

func TestDialSeparatorResolve(t *testing.T) {
	if got := dialSeparatorWidth(&dialActionSettings{}); got != 3 {
		t.Errorf("unset width -> %d, want default 3", got)
	}
	w7, w0, w15, wn := 7, 0, 15, -2
	if got := dialSeparatorWidth(&dialActionSettings{SeparatorWidth: &w7}); got != 7 {
		t.Errorf("explicit 7 -> %d", got)
	}
	if got := dialSeparatorWidth(&dialActionSettings{SeparatorWidth: &w0}); got != 0 {
		t.Errorf("explicit 0 (off) -> %d", got)
	}
	if got := dialSeparatorWidth(&dialActionSettings{SeparatorWidth: &w15}); got != 10 {
		t.Errorf("15 -> %d, want clamp 10", got)
	}
	if got := dialSeparatorWidth(&dialActionSettings{SeparatorWidth: &wn}); got != 0 {
		t.Errorf("negative -> %d, want 0", got)
	}
	if c := dialSeparatorColor(&dialActionSettings{}); c != (color.RGBA{54, 62, 70, 255}) {
		t.Errorf("default color = %+v", c)
	}
	if c := dialSeparatorColor(&dialActionSettings{SeparatorColor: "#ff0000"}); c.R != 255 || c.G != 0 || c.B != 0 {
		t.Errorf("parsed color = %+v, want red", c)
	}
}

func TestDialViewOptionsResolve(t *testing.T) {
	if got := dialIndicatorStyle(&dialActionSettings{}); got != "auto" {
		t.Fatalf("unset indicator style = %q, want auto", got)
	}
	if got := dialIndicatorStyle(&dialActionSettings{IndicatorStyle: "count"}); got != "count" {
		t.Fatalf("count indicator style = %q", got)
	}
	if got := dialIndicatorStyle(&dialActionSettings{IndicatorStyle: "bad"}); got != "auto" {
		t.Fatalf("bad indicator style = %q, want auto", got)
	}
	if dialDefaultOverview(&dialActionSettings{}) {
		t.Fatalf("unset default view must start fullscreen")
	}
	if !dialDefaultOverview(&dialActionSettings{DefaultView: "overview"}) {
		t.Fatalf("overview default view must start in overview")
	}
	if got := dialOverviewStyle(&dialActionSettings{}); got != "stacked" {
		t.Fatalf("unset overview style = %q, want stacked", got)
	}
	if got := dialOverviewStyle(&dialActionSettings{OverviewStyle: "carousel"}); got != "carousel" {
		t.Fatalf("carousel overview style = %q, want carousel", got)
	}
	if got := dialOverviewStyle(&dialActionSettings{OverviewStyle: "bad"}); got != "stacked" {
		t.Fatalf("bad overview style = %q, want stacked default", got)
	}
	if got := dialOverviewCap(&dialActionSettings{}); got != 3 {
		t.Fatalf("unset overview pages = %d, want 3", got)
	}
	if got := dialOverviewCap(&dialActionSettings{OverviewPages: "2"}); got != 2 {
		t.Fatalf("overview pages 2 = %d, want 2", got)
	}
	if got := dialOverviewCap(&dialActionSettings{OverviewPages: "bad"}); got != 3 {
		t.Fatalf("bad overview pages = %d, want 3 default", got)
	}
}

func TestDialOverviewRectsTwoUpReadable(t *testing.T) {
	rects := dialOverviewRects(2)
	if len(rects) != 2 {
		t.Fatalf("expected 2 cards, got %d", len(rects))
	}
	left, right := rects[0], rects[1]

	gap := right.Min.X - left.Max.X
	if gap != 0 {
		t.Fatalf("middle gap = %d, want no empty space between framed cards", gap)
	}
	if left.Max.X > right.Min.X {
		t.Fatalf("cards overlap: %v %v", left, right)
	}
	if left.Dx() != right.Dx() || left.Dy() != right.Dy() {
		t.Fatalf("cards must be equal size: %v vs %v", left, right)
	}
	for _, r := range rects {
		if r.Min.X < 0 || r.Min.Y < 0 || r.Max.X > dialWidth || r.Max.Y > dialHeight {
			t.Fatalf("card %v outside 0..%dx%d", r, dialWidth, dialHeight)
		}
		// Larger than the previous 86x46 layout so the scaled graphs/text read better.
		if r.Dx() < 90 || r.Dy() < 50 {
			t.Fatalf("card %v too small (want >=90x50), regressed readability", r)
		}
	}
}

func TestDialStackedLayout(t *testing.T) {
	const gutter = 18
	// Single page: one full-width strip starting after the reserved left column.
	idx, slot, rects := dialStackedLayout(0, 1, gutter, 3)
	if len(idx) != 1 || len(rects) != 1 || slot != 0 {
		t.Fatalf("count 1 layout = idx %v slot %d rects %d", idx, slot, len(rects))
	}

	// Three pages: previous/active/next, three EQUAL strips with the active one
	// centred in the middle. Every strip is full width starting at the reserved
	// left column (so the indicator column on the left stays clear and the graph
	// reaches the right edge).
	idx, slot, rects = dialStackedLayout(2, 5, gutter, 3)
	if slot != 1 {
		t.Fatalf("active slot = %d, want middle slot 1", slot)
	}
	if len(rects) != 3 {
		t.Fatalf("count 5 layout rects = %d, want 3", len(rects))
	}
	want := []int{1, 2, 3}
	for i, v := range want {
		if idx[i] != v {
			t.Fatalf("stacked indices = %v, want %v", idx, want)
		}
	}
	for _, r := range rects {
		if r.Min.X != gutter {
			t.Fatalf("strip left edge = %d, want reserved gutter %d", r.Min.X, gutter)
		}
		if r.Max.X != dialWidth {
			t.Fatalf("strip right edge = %d, want full width %d", r.Max.X, dialWidth)
		}
	}
	for i := 1; i < len(rects); i++ {
		if d := rects[i].Dy() - rects[0].Dy(); d < -1 || d > 1 {
			t.Fatalf("three strips must be equal height: %d vs %d", rects[0].Dy(), rects[i].Dy())
		}
	}
	// The active (middle) strip must sit between the other two.
	if !(rects[0].Max.Y <= rects[1].Min.Y && rects[1].Max.Y <= rects[2].Min.Y) {
		t.Fatalf("strips must stack top->middle->bottom: %v %v %v", rects[0], rects[1], rects[2])
	}

	// Two pages: two EQUAL strips, active on top; both full width.
	idx, slot, rects = dialStackedLayout(0, 2, gutter, 3)
	if len(idx) != 2 || slot != 0 {
		t.Fatalf("count 2 layout idx %v slot %d", idx, slot)
	}
	if d := rects[0].Dy() - rects[1].Dy(); d < -1 || d > 1 {
		t.Fatalf("count 2 strips must be equal height: %d vs %d", rects[0].Dy(), rects[1].Dy())
	}

	// Cap a five-page set to two strips: active on top, next below, both bigger
	// than the three-strip layout. The page indicator still reflects all pages.
	idx, slot, rects = dialStackedLayout(2, 5, gutter, 2)
	if len(idx) != 2 || len(rects) != 2 || slot != 0 {
		t.Fatalf("cap2 layout idx %v slot %d rects %d", idx, slot, len(rects))
	}
	if idx[0] != 2 || idx[1] != 3 {
		t.Fatalf("cap2 indices = %v, want [2 3] (active, next)", idx)
	}
	if rects[0].Dy() <= 33 {
		t.Fatalf("cap2 strip height %d not bigger than the three-strip layout", rects[0].Dy())
	}
}

// renderStackedTestGraph builds a dial graph plotting a rising 0..99 ramp so its
// newest (highest) sample is the tallest, with a bright-green highlight that is
// easy to detect against the dark card background.
func renderStackedTestGraph(title, value string) *graph.Graph {
	s := actionSettings{ForegroundColor: "#004000", HighlightColor: "#00ff00"}
	g := newDialGraph(&s)
	for v := 0; v <= 99; v++ {
		g.Update(float64(v))
	}
	_ = g.SetLabelText(0, title)
	_ = g.SetLabelText(1, value)
	return g
}

// TestRenderDialStackedNativeStrips verifies the stacked overview renders each
// strip natively (no cropping/scaling that would distort the graph): the active
// strip's sparkline fills toward the right edge, the most recent sample is the
// tallest (the graph builds rightward), and the older/left side stays empty until
// there is enough history. Set HWINFO_DUMP_DIAL=/path.png to also dump the image for
// visual inspection.
func TestRenderDialStackedNativeStrips(t *testing.T) {
	const n = 5
	pages := make([]actionSettings, n)
	state := &dialState{graphs: make([]*graph.Graph, n)}
	for i := 0; i < n; i++ {
		state.graphs[i] = renderStackedTestGraph("CPU Core", "99%")
	}
	settings := &dialActionSettings{Pages: pages, ActiveIndex: 2, OverviewStyle: "stacked"}

	b, err := (&Plugin{}).renderDialStacked(settings, state)
	if err != nil {
		t.Fatalf("renderDialStacked: %v", err)
	}
	if b == nil {
		t.Fatal("renderDialStacked returned nil image")
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dump := os.Getenv("HWINFO_DUMP_DIAL"); dump != "" {
		if err := os.WriteFile(dump, b, 0o644); err != nil {
			t.Fatalf("dump: %v", err)
		}
	}

	// Active strip band for active=2,count=5 is the middle equal third
	// Rect(12,34,200,66); its inner (sparkline) area is inset by 1px to
	// Rect(13,35,199,65), and the 2px blue border covers y=34..35.
	isGraph := func(x, y int) bool {
		c := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
		// Greenish: the filled area (#004000) and the highlight line (#00ff00).
		// Use int math so the +20 margin cannot overflow uint8 (e.g. white).
		return int(c.G) > 40 && int(c.G) > int(c.R)+20 && int(c.G) > int(c.B)+20
	}
	fillTop := func(x int) int {
		for y := 36; y <= 64; y++ {
			if isGraph(x, y) {
				return y
			}
		}
		return 65
	}

	// The sparkline reaches the right edge of the strip (full width, no side gap).
	if fillTop(196) >= 65 {
		t.Fatalf("active strip drew no graph near the right edge")
	}
	// Newest sample (rightmost) is the tallest; a mid column is lower.
	if right, mid := fillTop(196), fillTop(150); right >= mid {
		t.Fatalf("graph must build rightward: right fillTop=%d should be smaller (taller) than mid=%d", right, mid)
	}
	// Only 100 samples for ~186 columns, so the far left stays empty (newest-on-right).
	if isGraph(20, 50) || fillTop(20) < 65 {
		t.Fatalf("left side must stay empty until there is enough history")
	}
	// The active strip carries the bright blue selection border.
	blue := func(x, y int) bool {
		c := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
		return c.B > 180 && c.G > 100 && c.R < 90
	}
	foundBlue := false
	for x := 12; x < dialWidth && !foundBlue; x++ {
		if blue(x, 34) || blue(x, 35) {
			foundBlue = true
		}
	}
	if !foundBlue {
		t.Fatal("active strip must have the blue selection border")
	}
}

// TestRenderDialStackedHonoursIndicatorSize drives the full stacked render
// through the settings path (IndicatorSize) and asserts the chosen size actually
// changes the rendered left-column indicator. This is the end-to-end size test:
// a bigger indicator size must paint visibly more indicator pixels in the column.
func TestRenderDialStackedHonoursIndicatorSize(t *testing.T) {
	const n = 5
	indicatorPixels := func(size float64) int {
		pages := make([]actionSettings, n)
		state := &dialState{graphs: make([]*graph.Graph, n)}
		for i := 0; i < n; i++ {
			state.graphs[i] = renderStackedTestGraph("CPU Core", "99%")
		}
		sz := size
		settings := &dialActionSettings{
			Pages:          pages,
			ActiveIndex:    2,
			OverviewStyle:  "stacked",
			IndicatorStyle: "dots",
			IndicatorColor: "#ff0000",
			IndicatorSize:  &sz,
		}
		b, err := (&Plugin{}).renderDialStacked(settings, state)
		if err != nil {
			t.Fatalf("renderDialStacked(size=%v): %v", size, err)
		}
		img, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatalf("decode(size=%v): %v", size, err)
		}
		count := 0
		for y := 0; y < dialHeight; y++ {
			for x := 0; x < dialStackedGutter(size); x++ {
				c := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
				if c.R > 120 && int(c.G) < 90 && int(c.B) < 90 {
					count++
				}
			}
		}
		return count
	}

	small := indicatorPixels(1)
	large := indicatorPixels(8)
	if small == 0 {
		t.Fatal("indicator drew no pixels at the smallest size")
	}
	if large <= small {
		t.Fatalf("indicator size has no effect in stacked mode: size1=%d px, size8=%d px (want size8 > size1)", small, large)
	}
}

// TestRenderDialStackedCountHonoursIndicatorSize locks the size knob for the
// "count" indicator (including a double-digit page count, which previously
// saturated because the number was clamped to a fixed narrow column): a larger
// size must paint a visibly bigger page number.
func TestRenderDialStackedCountHonoursIndicatorSize(t *testing.T) {
	if _, err := graph.GetSharedFontFaceManager().GetFaceOfSize(12); err != nil {
		t.Skip("DejaVuSans-Bold.ttf not available in package dir; skipping count-render test")
	}
	const n = 12 // double-digit total
	indicatorPixels := func(size float64) int {
		pages := make([]actionSettings, n)
		state := &dialState{graphs: make([]*graph.Graph, n)}
		for i := 0; i < n; i++ {
			state.graphs[i] = renderStackedTestGraph("CPU Core", "99%")
		}
		sz := size
		settings := &dialActionSettings{
			Pages:          pages,
			ActiveIndex:    2,
			OverviewStyle:  "stacked",
			IndicatorStyle: "count",
			IndicatorColor: "#ff0000",
			IndicatorSize:  &sz,
		}
		b, err := (&Plugin{}).renderDialStacked(settings, state)
		if err != nil {
			t.Fatalf("renderDialStacked(size=%v): %v", size, err)
		}
		img, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatalf("decode(size=%v): %v", size, err)
		}
		count := 0
		for y := 0; y < dialHeight; y++ {
			for x := 0; x < dialStackedGutter(size); x++ {
				c := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
				if c.R > 120 && int(c.G) < 90 && int(c.B) < 90 {
					count++
				}
			}
		}
		return count
	}

	small := indicatorPixels(1)
	large := indicatorPixels(8)
	if small == 0 {
		t.Fatal("count indicator drew no pixels at the smallest size")
	}
	if large <= small {
		t.Fatalf("count indicator size has no effect in stacked mode: size1=%d px, size8=%d px (want size8 > size1)", small, large)
	}
}

func TestDrawDialVerticalPageIndicatorDrawsOnLeft(t *testing.T) {
	// Longest contiguous red run down a column is the active (elongated) dot.
	maxRun := func(img *image.RGBA, x int) int {
		best, run := 0, 0
		for y := 0; y < dialHeight; y++ {
			c := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
			if c.R > 150 && c.G < 80 && c.B < 80 {
				run++
				if run > best {
					best = run
				}
			} else {
				run = 0
			}
		}
		return best
	}
	render := func(size float64) *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, dialWidth, dialHeight))
		fillRect(img, img.Bounds(), color.RGBA{0, 0, 0, 255})
		drawDialVerticalPageIndicator(img, 1, 3, "dots", color.RGBA{255, 0, 0, 255}, size)
		return img
	}
	// Tallest active dot run anywhere inside the indicator gutter.
	bestInGutter := func(img *image.RGBA, size float64) int {
		best := 0
		for x := 0; x < dialStackedGutter(size); x++ {
			if r := maxRun(img, x); r > best {
				best = r
			}
		}
		return best
	}

	img := render(8)
	// The indicator lives in the reserved left gutter.
	if got := bestInGutter(img, 8); got <= 0 {
		t.Fatalf("vertical indicator drew no active dot in the left gutter")
	}
	// The right side (where the graph builds up) must stay clear of the indicator.
	for _, x := range []int{dialStackedGutter(8) + 4, dialWidth / 2, dialWidth - 4} {
		if maxRun(img, x) > 0 {
			t.Fatalf("vertical indicator must not draw at x=%d (graph area)", x)
		}
	}
	// Size must scale the active dot height.
	if large, small := bestInGutter(render(8), 8), bestInGutter(render(2), 2); large <= small {
		t.Fatalf("larger size must render a taller active dot: small=%d large=%d", small, large)
	}
	// "off" hides it.
	off := image.NewRGBA(image.Rect(0, 0, dialWidth, dialHeight))
	fillRect(off, off.Bounds(), color.RGBA{0, 0, 0, 255})
	drawDialVerticalPageIndicator(off, 1, 3, "off", color.RGBA{255, 0, 0, 255}, 8)
	if bestInGutter(off, 8) > 0 {
		t.Fatalf("style off must draw no vertical indicator")
	}
}

// TestDrawDialVerticalIndicatorHonoursExplicitChoice locks the "no silent
// fallback" rule for the vertical indicator: an explicit "count" must render the
// page number (a wide fraction bar across the column), an explicit "dots" must
// stay dots even with many pages, and only "auto" may switch to the number when
// the dots would no longer be legible.
func TestDrawDialVerticalIndicatorHonoursExplicitChoice(t *testing.T) {
	if _, err := graph.GetSharedFontFaceManager().GetFaceOfSize(12); err != nil {
		t.Skip("DejaVuSans-Bold.ttf not available in package dir; skipping count-render test")
	}
	red := color.RGBA{255, 0, 0, 255}
	// Widest contiguous red run on any row within the left indicator column. The
	// count form draws a fraction bar (~8px wide); dots are at most ~6px wide.
	widestRow := func(active, count int, style string, size float64) int {
		img := image.NewRGBA(image.Rect(0, 0, dialWidth, dialHeight))
		fillRect(img, img.Bounds(), color.RGBA{0, 0, 0, 255})
		drawDialVerticalPageIndicator(img, active, count, style, red, size)
		best := 0
		for y := 0; y < dialHeight; y++ {
			run := 0
			for x := 0; x < dialStackedGutter(size); x++ {
				c := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
				if c.R > 150 && c.G < 80 && c.B < 80 {
					run++
					if run > best {
						best = run
					}
				} else {
					run = 0
				}
			}
		}
		return best
	}

	// Explicit "count" renders the number form (wide fraction bar), never dots.
	if w := widestRow(1, 3, "count", 8); w < 7 {
		t.Fatalf("explicit count must render the page number (widest red row %d, want >=7)", w)
	}
	// Explicit "dots" stays dots even with many pages (no silent switch to count).
	if w := widestRow(1, 12, "dots", 8); w >= 7 {
		t.Fatalf("explicit dots must stay dots (widest red row %d, want <7 = no fraction bar)", w)
	}
	// Auto may decide: dots for a small page count, the number for a large one.
	if w := widestRow(1, 3, "auto", 8); w >= 7 {
		t.Fatalf("auto with few pages should use dots (widest red row %d, want <7)", w)
	}
	if w := widestRow(1, 12, "auto", 8); w < 7 {
		t.Fatalf("auto with many pages should switch to the number (widest red row %d, want >=7)", w)
	}
}


func TestDrawDialPageIndicatorUsesDotsForSmallPageCounts(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, dialWidth, dialHeight))
	fillRect(img, img.Bounds(), color.RGBA{0, 0, 0, 255})

	drawDialPageIndicator(img, 1, 3, "auto", dialIndicatorDefaultColor, dialIndicatorDefaultSize)

	activePixel := color.RGBAModel.Convert(img.At(dialWidth/2, dialHeight-6)).(color.RGBA)
	if activePixel == (color.RGBA{0, 0, 0, 255}) {
		t.Fatalf("active indicator pixel was not drawn")
	}
}

func TestDialIndicatorColorResolveAndRender(t *testing.T) {
	if c := dialIndicatorColor(&dialActionSettings{}); c != dialIndicatorDefaultColor {
		t.Fatalf("unset indicator color = %+v, want default %+v", c, dialIndicatorDefaultColor)
	}
	if c := dialIndicatorColor(&dialActionSettings{IndicatorColor: "#ff0000"}); c.R != 255 || c.G != 0 || c.B != 0 {
		t.Fatalf("parsed indicator color = %+v, want red", c)
	}

	// The chosen colour must actually reach the rendered active dot. Rendering with
	// red must produce a red-dominant pixel; the default grey must not. If the
	// colour param were ignored, the red assertion below would fail.
	at := func(c color.RGBA) color.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, dialWidth, dialHeight))
		fillRect(img, img.Bounds(), color.RGBA{0, 0, 0, 255})
		drawDialPageIndicator(img, 1, 3, "auto", c, dialIndicatorDefaultSize)
		return color.RGBAModel.Convert(img.At(dialWidth/2, dialHeight-6)).(color.RGBA)
	}

	red := at(color.RGBA{255, 0, 0, 255})
	if !(red.R > 150 && red.G < 80 && red.B < 80) {
		t.Fatalf("active dot did not render the chosen red colour: %+v", red)
	}
	grey := at(dialIndicatorDefaultColor)
	if grey.R > 150 && grey.G < 80 && grey.B < 80 {
		t.Fatalf("default grey active dot rendered as red: %+v", grey)
	}
}

func TestDialIndicatorSizeResolveAndScales(t *testing.T) {
	if got := dialIndicatorSize(&dialActionSettings{}); got != dialIndicatorDefaultSize {
		t.Fatalf("unset indicator size = %v, want default %v", got, dialIndicatorDefaultSize)
	}
	five := 5.0
	if got := dialIndicatorSize(&dialActionSettings{IndicatorSize: &five}); got != 5 {
		t.Fatalf("explicit size = %v, want 5", got)
	}
	lo, hi := 0.2, 99.0
	if got := dialIndicatorSize(&dialActionSettings{IndicatorSize: &lo}); got != 1 {
		t.Fatalf("size below range = %v, want clamp 1", got)
	}
	if got := dialIndicatorSize(&dialActionSettings{IndicatorSize: &hi}); got != 8 {
		t.Fatalf("size above range = %v, want clamp 8", got)
	}

	// The size must actually scale the rendered dots: the active dot's vertical
	// extent grows with the configured size. If the size param were ignored, both
	// heights would be equal and this fails.
	dotHeight := func(size float64) int {
		img := image.NewRGBA(image.Rect(0, 0, dialWidth, dialHeight))
		fillRect(img, img.Bounds(), color.RGBA{0, 0, 0, 255})
		drawDialPageIndicator(img, 1, 3, "dots", color.RGBA{255, 0, 0, 255}, size)
		h := 0
		for y := 0; y < dialHeight; y++ {
			c := color.RGBAModel.Convert(img.At(dialWidth/2, y)).(color.RGBA)
			if c.R > 150 && c.G < 80 && c.B < 80 {
				h++
			}
		}
		return h
	}
	small := dotHeight(2)
	large := dotHeight(8)
	if small <= 0 {
		t.Fatalf("smallest size drew no active dot")
	}
	if large <= small {
		t.Fatalf("larger indicator size must render taller dots: small=%d large=%d", small, large)
	}
	// Pin the "points" mapping the user cares about: size 4 reproduces the
	// original dot height (3) and size 8 is double (6) — never smaller.
	if h := dotHeight(4); h != 3 {
		t.Fatalf("size 4 active dot height = %d, want original 3", h)
	}
	if h := dotHeight(8); h != 6 {
		t.Fatalf("size 8 active dot height = %d, want double 6", h)
	}
}

func TestDialIndicatorFullscreenToggle(t *testing.T) {
	if dialIndicatorFullscreen(&dialActionSettings{}) {
		t.Fatalf("indicator fullscreen must default off")
	}
	if !dialIndicatorFullscreen(&dialActionSettings{IndicatorFullscreen: true}) {
		t.Fatalf("indicator fullscreen must be on when enabled")
	}

	fixture := func() []byte {
		img := image.NewRGBA(image.Rect(0, 0, dialWidth, dialHeight))
		fillRect(img, img.Bounds(), color.RGBA{0, 0, 0, 255})
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatalf("encode fixture: %v", err)
		}
		return buf.Bytes()
	}
	black := color.RGBA{0, 0, 0, 255}
	// The fullscreen indicator is drawn vertically in the LEFT gutter (like the
	// stacked overview), so count any non-black pixels there. The bottom-centre,
	// where the old horizontal indicator sat, must now stay clear of the graph.
	leftGutterPixels := func(b []byte) int {
		im, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		n := 0
		for y := 2; y < dialHeight-2; y++ {
			for x := 2; x < 16; x++ {
				if color.RGBAModel.Convert(im.At(x, y)).(color.RGBA) != black {
					n++
				}
			}
		}
		return n
	}

	// Toggle off: fullscreen keeps its original look, no indicator drawn.
	off, err := decorateDialImage(fixture(), 1, 3, false, "auto", 0, color.RGBA{}, dialIndicatorDefaultColor, dialIndicatorDefaultSize)
	if err != nil {
		t.Fatalf("decorate off: %v", err)
	}
	if px := leftGutterPixels(off); px != 0 {
		t.Fatalf("indicator must not be drawn in fullscreen when toggle is off (left-gutter pixels=%d)", px)
	}

	// Toggle on: the indicator is drawn in fullscreen, in the LEFT gutter.
	on, err := decorateDialImage(fixture(), 1, 3, true, "auto", 0, color.RGBA{}, dialIndicatorDefaultColor, dialIndicatorDefaultSize)
	if err != nil {
		t.Fatalf("decorate on: %v", err)
	}
	if px := leftGutterPixels(on); px == 0 {
		t.Fatalf("indicator must be drawn in the left gutter in fullscreen when toggle is on")
	}
}

func TestDialPageContext(t *testing.T) {
	if got := dialPageContext("abc", 2); got != "abc|dial|page|2" {
		t.Errorf("dialPageContext = %q, want abc|dial|page|2", got)
	}
	if dialPageContext("x", 0) == dialPageContext("x", 1) {
		t.Errorf("different page indices must produce different contexts")
	}
}

// TestUpdateDialPageDrawsFullscreenIndicatorOnlyWhenEnabled is the integration
// test for the previously-missing link: that the REAL fullscreen render path
// (updateDialPage) honors the IndicatorFullscreen toggle. It renders the active
// page twice with an identical reading (deterministic stub value) and asserts the
// only pixels that change are inside the page-indicator band. This catches both a
// dead toggle (no diff) and a toggle that leaks into the rest of the dial.
func TestUpdateDialPageDrawsFullscreenIndicatorOnlyWhenEnabled(t *testing.T) {
	const (
		ctx       = "dial-fs-ind"
		sensorUID = "/cpu"
		readingID = int32(7)
	)
	now := time.Unix(2000, 0)

	render := func(fullscreenIndicator bool) image.Image {
		p := &Plugin{

			thresholdStates:  make(map[string]map[string]*thresholdRuntimeState),
			thresholdSnoozes: make(map[string]*thresholdSnoozeState),
			thresholdDirty:   make(map[string]bool),
			lastPollTime:     make(map[string]uint64),
			lastRenderTime:   make(map[string]time.Time),
			smoothedValues:   make(map[string]float64),
			divisorCache:     make(map[string]divisorCacheEntry),
			pollTimeCacheTTL: time.Second,
		}
		p.bridge = &bridgeRuntime{
			hw: stubHardwareService{
				readingsBySensor: map[string][]hwsensorsservice.Reading{
					sensorUID: {stubReading{id: readingID, typ: "Load", label: "CPU Total", unit: "%"}},
				},
			},
		}
		page := actionSettings{
			SensorUID: sensorUID, ReadingID: readingID, ReadingLabel: "CPU Total",
			IsValid: true, Min: 0, Max: 100,
			ForegroundColor: "#005128", BackgroundColor: "#000000",
			HighlightColor: "#009e00", ValueTextColor: "#ffffff", TitleColor: "#b7b7b7",
		}
		settings := &dialActionSettings{
			Pages:               []actionSettings{page, page, page},
			ActiveIndex:         1,
			IndicatorFullscreen: fullscreenIndicator,
		}
		state := initDialState(settings)
		r, _ := p.updateDialPage(ctx, settings, state, settings.ActiveIndex, true, now)
		if len(r.image) == 0 {
			t.Fatalf("no fullscreen image rendered (indicator=%v)", fullscreenIndicator)
		}
		im, err := png.Decode(bytes.NewReader(r.image))
		if err != nil {
			t.Fatalf("decode fullscreen image: %v", err)
		}
		return im
	}

	off := render(false)
	on := render(true)

	diff, outsideBand := 0, 0
	b := off.Bounds()
	// The fullscreen indicator now lives in the LEFT gutter (like the stacked
	// overview), so the only pixels the toggle may change are x < the gutter width.
	gutterRight := dialStackedGutter(dialIndicatorDefaultSize)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			o := color.RGBAModel.Convert(off.At(x, y)).(color.RGBA)
			n := color.RGBAModel.Convert(on.At(x, y)).(color.RGBA)
			if o != n {
				diff++
				if x >= gutterRight {
					outsideBand++
				}
			}
		}
	}
	if diff == 0 {
		t.Fatalf("fullscreen render identical with indicator on vs off — toggle is not wired into the fullscreen path")
	}
	if outsideBand > 0 {
		t.Fatalf("indicator toggle changed %d pixels outside the left indicator gutter — it affects more than the page indicator", outsideBand)
	}
}

func TestDialGraphScale(t *testing.T) {
	tests := []struct {
		name             string
		min, max         int
		wantMin, wantMax int
	}{
		{name: "explicit", min: 10, max: 90, wantMin: 10, wantMax: 90},
		{name: "unset sentinel", min: 0, max: 0, wantMin: 0, wantMax: 100},
		{name: "inverted falls back", min: 50, max: 20, wantMin: 0, wantMax: 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMin, gotMax := dialGraphScale(&actionSettings{Min: tt.min, Max: tt.max})
			if gotMin != tt.wantMin || gotMax != tt.wantMax {
				t.Fatalf("scale = (%d,%d), want (%d,%d)", gotMin, gotMax, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestBuildDialGraphsPreservesHistory(t *testing.T) {
	old := &dialActionSettings{Pages: []actionSettings{
		{SensorUID: "cpu", ReadingID: 1, Min: 0, Max: 100},
		{SensorUID: "gpu", ReadingID: 2, Min: 0, Max: 100},
	}}
	oldState := initDialState(old)

	// Same readings, only a scale edit on page 0: both graphs must be reused so
	// plotted history survives.
	scaleEdit := &dialActionSettings{Pages: []actionSettings{
		{SensorUID: "cpu", ReadingID: 1, Min: 0, Max: 50},
		{SensorUID: "gpu", ReadingID: 2, Min: 0, Max: 100},
	}}
	got := buildDialGraphs(old, oldState, scaleEdit)
	if got[0] != oldState.graphs[0] {
		t.Errorf("page 0 graph rebuilt on scale edit; history lost")
	}
	if got[1] != oldState.graphs[1] {
		t.Errorf("page 1 graph rebuilt despite no change")
	}

	// Reading changed on page 1: that page rebuilds, page 0 still reused.
	readingChange := &dialActionSettings{Pages: []actionSettings{
		{SensorUID: "cpu", ReadingID: 1, Min: 0, Max: 100},
		{SensorUID: "gpu", ReadingID: 9, Min: 0, Max: 100},
	}}
	got2 := buildDialGraphs(old, oldState, readingChange)
	if got2[0] != oldState.graphs[0] {
		t.Errorf("page 0 graph rebuilt despite same reading")
	}
	if got2[1] == oldState.graphs[1] {
		t.Errorf("page 1 graph reused despite reading change")
	}

	// Appended page: existing reused, new one built.
	appended := &dialActionSettings{Pages: []actionSettings{
		{SensorUID: "cpu", ReadingID: 1, Min: 0, Max: 100},
		{SensorUID: "gpu", ReadingID: 2, Min: 0, Max: 100},
		{SensorUID: "ram", ReadingID: 3, Min: 0, Max: 100},
	}}
	got3 := buildDialGraphs(old, oldState, appended)
	if len(got3) != 3 {
		t.Fatalf("graphs len = %d, want 3", len(got3))
	}
	if got3[0] != oldState.graphs[0] || got3[1] != oldState.graphs[1] {
		t.Errorf("existing graphs not preserved when appending a page")
	}
	if got3[2] == nil {
		t.Errorf("appended page graph not built")
	}
}

func TestUpdateDialPageKeepsSnoozeWhenThresholdDrops(t *testing.T) {
	const (
		ctx       = "dial-ctx"
		pageCtx   = ctx + "|dial|page|0"
		sensorUID = "/cpu"
		readingID = int32(42)
	)

	p := &Plugin{

		thresholdStates:  make(map[string]map[string]*thresholdRuntimeState),
		thresholdSnoozes: make(map[string]*thresholdSnoozeState),
		thresholdDirty:   make(map[string]bool),
		lastPollTime:     make(map[string]uint64),
		lastRenderTime:   make(map[string]time.Time),
		smoothedValues:   make(map[string]float64),
		divisorCache:     make(map[string]divisorCacheEntry),
		pollTimeCacheTTL: time.Second,
	}
	p.bridge = &bridgeRuntime{
		hw: stubHardwareService{
			readingsBySensor: map[string][]hwsensorsservice.Reading{
				sensorUID: {
					stubReading{id: readingID, typ: "Load", label: "CPU Total", unit: "%"},
				},
			},
		},
	}

	settings := &dialActionSettings{Pages: []actionSettings{{
		SensorUID:          sensorUID,
		ReadingID:          readingID,
		ReadingLabel:       "CPU Total",
		IsValid:            true,
		Min:                0,
		Max:                100,
		ForegroundColor:    "#005128",
		BackgroundColor:    "#000000",
		HighlightColor:     "#009e00",
		ValueTextColor:     "#ffffff",
		TitleColor:         "#b7b7b7",
		CurrentThresholdID: "t1",
		SnoozeDurations:    []int{0},
		Thresholds:         []Threshold{{ID: "t1", Enabled: true, Operator: ">=", Value: 10}},
	}}}
	state := initDialState(settings)
	now := time.Unix(1200, 0)
	p.setThresholdSnooze(pageCtx, 0, now)

	_, changed := p.updateDialPage(ctx, settings, state, 0, true, now.Add(time.Second))
	if !changed {
		t.Fatalf("expected settings change when current threshold id is cleared")
	}
	if _, ok := p.currentThresholdSnooze(pageCtx, now.Add(time.Second)); !ok {
		t.Fatalf("expected dial page snooze to remain active after threshold drops")
	}
	if settings.Pages[0].CurrentThresholdID != "" {
		t.Fatalf("expected current threshold id to clear after threshold drops")
	}
}

// #56 feedback item 3: a threshold with BringToFront set must flag its page on
// the tick its alarm fires (edge), and not on later ticks while it stays active.
func newBringToFrontPlugin(bringToFront bool) (*Plugin, *dialActionSettings, *dialState) {
	const (
		sensorUID = "/cpu"
		readingID = int32(42)
	)
	p := &Plugin{

		thresholdStates:  make(map[string]map[string]*thresholdRuntimeState),
		thresholdSnoozes: make(map[string]*thresholdSnoozeState),
		thresholdDirty:   make(map[string]bool),
		lastPollTime:     make(map[string]uint64),
		lastRenderTime:   make(map[string]time.Time),
		smoothedValues:   make(map[string]float64),
		divisorCache:     make(map[string]divisorCacheEntry),
		pollTimeCacheTTL: time.Second,
	}
	p.bridge = &bridgeRuntime{
		hw: stubHardwareService{
			readingsBySensor: map[string][]hwsensorsservice.Reading{
				sensorUID: {stubReading{id: readingID, typ: "Load", label: "CPU Total", unit: "%"}},
			},
		},
	}
	settings := &dialActionSettings{Pages: []actionSettings{{
		SensorUID:       sensorUID,
		ReadingID:       readingID,
		ReadingLabel:    "CPU Total",
		IsValid:         true,
		Min:             0,
		Max:             100,
		ForegroundColor: "#005128",
		BackgroundColor: "#000000",
		HighlightColor:  "#009e00",
		ValueTextColor:  "#ffffff",
		TitleColor:      "#b7b7b7",
		// stubReading.Value()==0, so "<= 50" fires immediately.
		Thresholds: []Threshold{{ID: "t1", Enabled: true, Operator: "<=", Value: 50, BringToFront: bringToFront}},
	}}}
	return p, settings, initDialState(settings)
}

func TestUpdateDialPageBringToFront(t *testing.T) {
	now := time.Unix(1200, 0)

	p, settings, state := newBringToFrontPlugin(true)
	render, _ := p.updateDialPage("ctx", settings, state, 0, true, now)
	if !render.bringToFront {
		t.Fatalf("expected bringToFront on the alarm edge")
	}
	// Second tick: threshold stays active, no edge -> must not keep requesting.
	render2, _ := p.updateDialPage("ctx", settings, state, 0, true, now.Add(time.Second))
	if render2.bringToFront {
		t.Fatalf("bringToFront must be edge-triggered, not sticky across ticks")
	}

	// Same firing threshold without the flag must never request bring-to-front.
	pOff, settingsOff, stateOff := newBringToFrontPlugin(false)
	if r, _ := pOff.updateDialPage("ctx", settingsOff, stateOff, 0, true, now); r.bringToFront {
		t.Fatalf("bringToFront must stay off when the threshold does not opt in")
	}
}

func TestHandleDialPageTouchStartsSnoozeForConfiguredThresholdPage(t *testing.T) {
	p := &Plugin{
		thresholdSnoozes: make(map[string]*thresholdSnoozeState),
		thresholdDirty:   make(map[string]bool),
	}
	now := time.Unix(1300, 0)
	page := &actionSettings{
		SnoozeDurations: []int{0},
		Thresholds:      []Threshold{{ID: "t1", Enabled: true, Operator: ">=", Value: 10}},
	}

	if !p.handleDialPageTouch("dial-page", page, now) {
		t.Fatalf("expected touch to start snooze on configured threshold page")
	}
	if _, ok := p.currentThresholdSnooze("dial-page", now); !ok {
		t.Fatalf("expected snooze to be active after touch")
	}
	if !p.handleDialPageTouch("dial-page", page, now.Add(time.Second)) {
		t.Fatalf("expected second touch to clear active snooze")
	}
	if _, ok := p.currentThresholdSnooze("dial-page", now.Add(time.Second)); ok {
		t.Fatalf("expected snooze to be cleared by second touch")
	}
}

func TestHandleDialPageTouchIgnoresPageWithoutThresholds(t *testing.T) {
	p := &Plugin{
		thresholdSnoozes: make(map[string]*thresholdSnoozeState),
		thresholdDirty:   make(map[string]bool),
	}
	page := &actionSettings{SnoozeDurations: []int{0}}

	if p.handleDialPageTouch("dial-page", page, time.Unix(1400, 0)) {
		t.Fatalf("expected touch without thresholds or active alert to do nothing")
	}
}

func TestHandleDialPageTouchClearsStickyThresholdWithoutSnoozeDurations(t *testing.T) {
	const pageCtx = "dial-page"
	p := &Plugin{
		thresholdStates:  make(map[string]map[string]*thresholdRuntimeState),
		thresholdSnoozes: make(map[string]*thresholdSnoozeState),
		thresholdDirty:   make(map[string]bool),
	}
	p.thresholdStates[pageCtx] = map[string]*thresholdRuntimeState{
		"t1": {Active: true, Latched: true},
	}
	page := &actionSettings{
		CurrentThresholdID: "t1",
		Thresholds:         []Threshold{{ID: "t1", Enabled: true, Operator: ">=", Value: 10}},
	}
	now := time.Unix(1500, 0)

	// Active threshold, no snooze durations -> sticky-clear branch.
	if !p.handleDialPageTouch(pageCtx, page, now) {
		t.Fatalf("expected touch to clear sticky threshold when no snooze durations configured")
	}
	if page.CurrentThresholdID != "" {
		t.Fatalf("expected current threshold id cleared, got %q", page.CurrentThresholdID)
	}

	// Nothing active anymore -> a follow-up touch is a no-op.
	if p.handleDialPageTouch(pageCtx, page, now.Add(time.Second)) {
		t.Fatalf("expected no-op touch after sticky threshold cleared")
	}
}
