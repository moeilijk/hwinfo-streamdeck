package hwinfostreamdeckplugin

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	imagedraw "image/draw"
	"image/png"
	"log"
	"strings"
	"time"

	"github.com/moeilijk/hwinfo-streamdeck/pkg/graph"
	hwsensorsservice "github.com/moeilijk/hwinfo-streamdeck/pkg/service"
	"github.com/moeilijk/hwinfo-streamdeck/pkg/streamdeck"
	"golang.org/x/image/font"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/math/fixed"
)

const (
	dialAction = "com.moeilijk.hwinfo.dial"
	dialWidth  = 200
	dialHeight = 100
)

type dialState struct {
	graphs   []*graph.Graph
	overview bool
}

type dialPageRender struct {
	image        []byte
	messageTitle string
	messageValue string
	// bringToFront is set on the tick a page's threshold becomes active (and is
	// not snoozed) when that threshold has BringToFront enabled, so the feedback
	// loop can make this the active page. Edge-triggered, so it does not lock out
	// manual navigation afterwards.
	bringToFront bool
}

var dialPageColorPalette = []struct {
	foreground string
	highlight  string
}{
	{foreground: "#005128", highlight: "#009e00"},
	{foreground: "#003f73", highlight: "#00a2ff"},
	{foreground: "#5a3b87", highlight: "#b06cff"},
	{foreground: "#6a4a00", highlight: "#ffbf33"},
	{foreground: "#6f1d1b", highlight: "#ff5a4f"},
	{foreground: "#004b50", highlight: "#00d6d6"},
	{foreground: "#4d3d00", highlight: "#d8d000"},
	{foreground: "#00421f", highlight: "#39d98a"},
	{foreground: "#4b184f", highlight: "#ff66d8"},
	{foreground: "#5b2b00", highlight: "#ff8a1f"},
	{foreground: "#173b64", highlight: "#66c2ff"},
	{foreground: "#3f4f13", highlight: "#b5e853"},
	{foreground: "#4f2333", highlight: "#ff7aa8"},
	{foreground: "#1d4a45", highlight: "#5ef2c2"},
	{foreground: "#2f2d6b", highlight: "#8f8cff"},
	{foreground: "#5a3216", highlight: "#d98b45"},
}

func decodeDialSettings(raw *json.RawMessage) (dialActionSettings, error) {
	var s dialActionSettings
	if raw != nil && *raw != nil {
		if err := json.Unmarshal(*raw, &s); err != nil {
			return s, err
		}
	}
	if s.ActiveIndex < 0 {
		s.ActiveIndex = 0
	}
	if len(s.Pages) > 0 {
		s.ActiveIndex %= len(s.Pages)
	}
	return s, nil
}

func dialDefaultPageColors(index int) (foreground, highlight string) {
	if len(dialPageColorPalette) == 0 {
		return "#005128", "#009e00"
	}
	if index < 0 {
		index = 0
	}
	c := dialPageColorPalette[index%len(dialPageColorPalette)]
	return c.foreground, c.highlight
}

// emaSmooth applies one exponential-moving-average step, shared by the reading
// tile and the dial so both smooth a graph value identically.
func emaSmooth(alpha, value, prev float64) float64 {
	return alpha*value + (1-alpha)*prev
}

func dialGraphScale(s *actionSettings) (int, int) {
	minValue, maxValue := s.Min, s.Max
	if maxValue <= minValue {
		minValue, maxValue = 0, 100
	}
	return minValue, maxValue
}

func dialColor(hex string, def color.RGBA) *color.RGBA {
	// hexToRGBA never returns nil (unparseable input yields black), so an unset
	// color must fall back to the default explicitly.
	if hex == "" {
		d := def
		return &d
	}
	return hexToRGBA(hex)
}

// applyDialGraphSettings updates an existing graph in place to match the page
// settings. Keeping the graph object lets its plotted history survive page or
// style edits; only a reading change rebuilds it (see buildDialGraphs).
func applyDialGraphSettings(g *graph.Graph, s *actionSettings) {
	minValue, maxValue := dialGraphScale(s)
	g.SetMin(minValue)
	g.SetMax(maxValue)
	g.SetForegroundColor(dialColor(s.ForegroundColor, color.RGBA{0, 81, 40, 255}))
	g.SetBackgroundColor(dialColor(s.BackgroundColor, color.RGBA{0, 0, 0, 255}))
	g.SetHighlightColor(dialColor(s.HighlightColor, color.RGBA{0, 158, 0, 255}))
	_ = g.SetLabelColor(0, dialColor(s.TitleColor, color.RGBA{183, 183, 183, 255}))
	_ = g.SetLabelColor(1, dialColor(s.ValueTextColor, color.RGBA{255, 255, 255, 255}))
	_ = g.SetLabelColor(2, dialColor(s.ValueTextColor, color.RGBA{255, 255, 255, 255}))
	_ = g.SetLabelFontSize(0, defaultDialTitleFontSize(s.TitleFontSize))
	_ = g.SetLabelFontSize(1, defaultDialValueFontSize(s.ValueFontSize))
	_ = g.SetLabelFontSize(2, 10.5)
	if s.GraphHeightPct > 0 {
		g.SetHeightPct(s.GraphHeightPct)
	}
	if s.GraphLineThickness > 0 {
		g.SetLineThickness(s.GraphLineThickness)
	}
	g.SetTextStroke(s.TextStroke)
	if s.TextStrokeColor != "" {
		g.SetTextStrokeColor(hexToRGBA(s.TextStrokeColor))
	}
}

func newDialGraph(s *actionSettings) *graph.Graph {
	minValue, maxValue := dialGraphScale(s)
	g := graph.NewGraph(dialWidth, dialHeight, minValue, maxValue,
		dialColor(s.ForegroundColor, color.RGBA{0, 81, 40, 255}),
		dialColor(s.BackgroundColor, color.RGBA{0, 0, 0, 255}),
		dialColor(s.HighlightColor, color.RGBA{0, 158, 0, 255}))
	g.SetLabel(0, "", 24, dialColor(s.TitleColor, color.RGBA{183, 183, 183, 255}))
	g.SetLabel(1, "", 58, dialColor(s.ValueTextColor, color.RGBA{255, 255, 255, 255}))
	g.SetLabel(2, "", 82, dialColor(s.ValueTextColor, color.RGBA{255, 255, 255, 255}))
	applyDialGraphSettings(g, s)
	return g
}

// dialPageSameReading reports whether two pages plot the same data series, so a
// graph (and its history) can be reused across a settings save.
func dialPageSameReading(a, b *actionSettings) bool {
	return a.SensorUID == b.SensorUID && a.ReadingID == b.ReadingID
}

// buildDialGraphs builds the graph slice for new settings, reusing each existing
// graph (preserving its plotted history) when the page at that index still plots
// the same reading, and only rebuilding pages whose reading changed or that are
// new. This stops every graph from resetting on any page/style edit.
func buildDialGraphs(oldSettings *dialActionSettings, oldState *dialState, s *dialActionSettings) []*graph.Graph {
	graphs := make([]*graph.Graph, len(s.Pages))
	for i := range s.Pages {
		reuse := oldState != nil && i < len(oldState.graphs) && oldState.graphs[i] != nil &&
			oldSettings != nil && i < len(oldSettings.Pages) &&
			dialPageSameReading(&oldSettings.Pages[i], &s.Pages[i])
		if reuse {
			g := oldState.graphs[i]
			applyDialGraphSettings(g, &s.Pages[i])
			graphs[i] = g
		} else {
			graphs[i] = newDialGraph(&s.Pages[i])
		}
	}
	return graphs
}

func defaultDialTitleFontSize(v float64) float64 {
	if v > 0 {
		return v
	}
	return 14
}

func defaultDialValueFontSize(v float64) float64 {
	if v > 0 {
		return v
	}
	return 18
}

func initDialState(s *dialActionSettings) *dialState {
	state := &dialState{graphs: make([]*graph.Graph, len(s.Pages))}
	for i := range s.Pages {
		state.graphs[i] = newDialGraph(&s.Pages[i])
	}
	return state
}

// dialRotateTicks applies the reverse-direction setting to the raw encoder ticks,
// so a clockwise turn can be made to step to the previous page instead of the next.
func dialRotateTicks(ticks int, reverse bool) int {
	if reverse {
		return -ticks
	}
	return ticks
}

func wrapDialIndex(current, ticks, count int) int {
	if count <= 0 {
		return 0
	}
	next := (current + ticks) % count
	if next < 0 {
		next += count
	}
	return next
}

func dialOverviewIndices(active, count, limit int) []int {
	if count <= 0 {
		return nil
	}
	if limit < 2 {
		limit = 2
	}
	if count == 1 {
		return []int{active}
	}
	if count == 2 {
		// Exactly two pages: previous wraps to the other page, active on the right.
		return []int{
			wrapDialIndex(active, -1, count),
			wrapDialIndex(active, 0, count),
		}
	}
	if limit == 2 {
		// Capping a larger set to two cards: active on the left, next on the right
		// so the active page reads first.
		return []int{
			wrapDialIndex(active, 0, count),
			wrapDialIndex(active, 1, count),
		}
	}
	return []int{
		wrapDialIndex(active, -1, count),
		wrapDialIndex(active, 0, count),
		wrapDialIndex(active, 1, count),
	}
}

func pngDataURL(b []byte) string {
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b)
}

func (p *Plugin) sendDialCanvas(ctx string, b []byte) {
	payload := map[string]interface{}{
		"full-canvas": pngDataURL(b),
		"title":       "",
	}
	if err := p.sd.SetFeedback(ctx, payload); err != nil {
		log.Printf("dial setFeedback: %v", err)
	}
}

func (p *Plugin) showDialMessage(ctx, title, value string) {
	s := actionSettings{
		Title:           title,
		Min:             0,
		Max:             100,
		ForegroundColor: "#003322",
		BackgroundColor: "#000000",
		HighlightColor:  "#00a36c",
		TitleColor:      "#b7b7b7",
		ValueTextColor:  "#ffffff",
	}
	g := newDialGraph(&s)
	_ = g.SetLabelText(0, title)
	_ = g.SetLabelText(1, value)
	g.Update(0)
	b, err := g.EncodePNG()
	if err != nil {
		log.Printf("dial message encode: %v", err)
		return
	}
	p.sendDialCanvas(ctx, b)
}

func fillRect(img *image.RGBA, r image.Rectangle, c color.Color) {
	imagedraw.Draw(img, r, &image.Uniform{C: c}, image.Point{}, imagedraw.Src)
}

func strokeRect(img *image.RGBA, r image.Rectangle, c color.Color, width int) {
	for i := 0; i < width; i++ {
		rr := r.Inset(i)
		fillRect(img, image.Rect(rr.Min.X, rr.Min.Y, rr.Max.X, rr.Min.Y+1), c)
		fillRect(img, image.Rect(rr.Min.X, rr.Max.Y-1, rr.Max.X, rr.Max.Y), c)
		fillRect(img, image.Rect(rr.Min.X, rr.Min.Y, rr.Min.X+1, rr.Max.Y), c)
		fillRect(img, image.Rect(rr.Max.X-1, rr.Min.Y, rr.Max.X, rr.Max.Y), c)
	}
}

// drawDialEdgeSeparators draws a width-px band of clr on the left and right edge
// of the dial image so adjacent dials are visually separated. width 0 = off.
func drawDialEdgeSeparators(img *image.RGBA, width int, clr color.RGBA) {
	if width <= 0 {
		return
	}
	bounds := img.Bounds()
	if bounds.Empty() {
		return
	}
	if half := bounds.Dx() / 2; width > half {
		width = half
	}
	fillRect(img, image.Rect(bounds.Min.X, bounds.Min.Y, bounds.Min.X+width, bounds.Max.Y), clr)
	fillRect(img, image.Rect(bounds.Max.X-width, bounds.Min.Y, bounds.Max.X, bounds.Max.Y), clr)
}

// dialSeparatorWidth resolves the configured edge-separator width (0-10 per edge),
// defaulting to 3 when unset (preserving the original visible separator).
func dialSeparatorWidth(s *dialActionSettings) int {
	w := 3
	if s != nil && s.SeparatorWidth != nil {
		w = *s.SeparatorWidth
	}
	if w < 0 {
		w = 0
	}
	if w > 10 {
		w = 10
	}
	return w
}

func dialSeparatorColor(s *dialActionSettings) color.RGBA {
	hex := ""
	if s != nil {
		hex = s.SeparatorColor
	}
	return *dialColor(hex, color.RGBA{54, 62, 70, 255})
}

func dialIndicatorStyle(s *dialActionSettings) string {
	if s == nil {
		return "auto"
	}
	switch s.IndicatorStyle {
	case "off", "dots", "count", "auto":
		return s.IndicatorStyle
	default:
		return "auto"
	}
}

func dialDefaultOverview(s *dialActionSettings) bool {
	return s != nil && s.DefaultView == "overview"
}

// dialOverviewStyle resolves how the overview view renders: "stacked" (default,
// vertical full-width strips) or "carousel" (the original horizontal layout).
func dialOverviewStyle(s *dialActionSettings) string {
	if s != nil && s.OverviewStyle == "carousel" {
		return "carousel"
	}
	return "stacked"
}

// dialOverviewCap resolves how many pages the overview shows at once: 2 when the
// user picked "2" (bigger cards/strips), otherwise the default 3. Empty / "auto"
// keeps the original previous/active/next window.
func dialOverviewCap(s *dialActionSettings) int {
	if s != nil && s.OverviewPages == "2" {
		return 2
	}
	return 3
}

// dialIndicatorDefaultColor is the original active page-indicator colour, used as
// the default and as the base for the dimmer inactive dots.
var dialIndicatorDefaultColor = color.RGBA{190, 198, 206, 255}

// dialIndicatorColor resolves the configured page-indicator colour, defaulting to
// the original light grey when unset.
func dialIndicatorColor(s *dialActionSettings) color.RGBA {
	hex := ""
	if s != nil {
		hex = s.IndicatorColor
	}
	return *dialColor(hex, dialIndicatorDefaultColor)
}

// dialIndicatorDefaultSize is the default page-indicator size in "points" (4 =
// original size).
const dialIndicatorDefaultSize = 6.0

// dialIndicatorSize resolves the configured page-indicator size in "points"
// (4 = original, 8 = double). Clamped to 1-8.
func dialIndicatorSize(s *dialActionSettings) float64 {
	size := dialIndicatorDefaultSize
	if s != nil && s.IndicatorSize != nil {
		size = *s.IndicatorSize
	}
	if size < 1 {
		size = 1
	}
	if size > 8 {
		size = 8
	}
	return size
}

// dialIndicatorFullscreen reports whether the page indicator should also be
// drawn in the fullscreen view. Off by default (fullscreen keeps its original
// look); the resolved indicator style controls how it renders when enabled.
func dialIndicatorFullscreen(s *dialActionSettings) bool {
	return s != nil && s.IndicatorFullscreen
}

func drawDialPageIndicator(img *image.RGBA, active, count int, style string, indicatorColor color.RGBA, size float64) {
	if count <= 1 || style == "off" {
		return
	}
	if active < 0 {
		active = 0
	}
	if active >= count {
		active = count - 1
	}
	if size < 1 {
		size = 1
	}
	if size > 8 {
		size = 8
	}
	activeColor := indicatorColor
	// The inactive dots are a dimmer shade of the chosen colour (~55%), preserving
	// the original active/inactive contrast ratio whatever colour the user picks.
	inactive := color.RGBA{
		uint8(float64(indicatorColor.R) * 0.55),
		uint8(float64(indicatorColor.G) * 0.55),
		uint8(float64(indicatorColor.B) * 0.55),
		255,
	}
	if style == "" {
		style = "auto"
	}
	if style == "dots" || (style == "auto" && count <= 9) {
		// Sizes are derived so that size 4 reproduces the original indicator
		// (dotW 4, dotH 3, gap 4, activeW 10) and scale linearly from there.
		dotW := int(size + 0.5)
		dotH := int(size*0.75 + 0.5)
		if dotH < 1 {
			dotH = 1
		}
		gap := int(size + 0.5)
		activeW := int(size*2.5 + 0.5)
		// Bottom-align the dots so larger sizes grow upward from the dial edge.
		y := dialHeight - 3 - dotH
		total := 0
		for i := 0; i < count; i++ {
			if i > 0 {
				total += gap
			}
			if i == active {
				total += activeW
			} else {
				total += dotW
			}
		}
		if total > dialWidth-20 {
			style = "count"
		} else {
			x := (dialWidth - total) / 2
			for i := 0; i < count; i++ {
				w := dotW
				c := inactive
				if i == active {
					w = activeW
					c = activeColor
				}
				fillRect(img, image.Rect(x, y, x+w, y+dotH), c)
				x += w + gap
			}
			return
		}
	}
	if style != "count" && style != "auto" {
		return
	}

	face, err := graph.GetSharedFontFaceManager().GetFaceOfSize(size * 2)
	if err != nil {
		return
	}
	drawCenteredText(img, face, &activeColor, fmt.Sprintf("%d/%d", active+1, count), dialHeight-3)
}

func centeredAspectCrop(src image.Rectangle, dst image.Rectangle) image.Rectangle {
	if src.Empty() || dst.Empty() {
		return src
	}
	srcW, srcH := src.Dx(), src.Dy()
	dstW, dstH := dst.Dx(), dst.Dy()
	if srcW*dstH > dstW*srcH {
		cropW := srcH * dstW / dstH
		x0 := src.Min.X + (srcW-cropW)/2
		return image.Rect(x0, src.Min.Y, x0+cropW, src.Max.Y)
	}
	if srcW*dstH < dstW*srcH {
		cropH := srcW * dstH / dstW
		y0 := src.Min.Y + (srcH-cropH)/2
		return image.Rect(src.Min.X, y0, src.Max.X, y0+cropH)
	}
	return src
}

func dialOverviewRects(count int) []image.Rectangle {
	if count <= 0 {
		return nil
	}
	if count == 1 {
		return []image.Rectangle{image.Rect(17, 8, 183, 94)}
	}
	if count == 2 {
		// Each card already has its own border, so the two cards touch directly at the
		// centre (no middle gap). Cards are 96x51; with the 3px inset the preview area is
		// exactly 90x45 = 2:1, so the graph is not distorted. The 4px outer margins clear
		// the edge separators. Bigger than the old 86x46 layout for readability.
		return []image.Rectangle{
			image.Rect(4, 24, 100, 75),
			image.Rect(100, 24, 196, 75),
		}
	}
	return []image.Rectangle{
		image.Rect(1, 30, 57, 61),
		image.Rect(59, 14, 141, 58),
		image.Rect(143, 30, 199, 61),
	}
}

func decorateDialImage(b []byte, active, count int, showIndicator bool, indicatorStyle string, sepWidth int, sepColor color.RGBA, indicatorColor color.RGBA, indicatorSize float64) ([]byte, error) {
	src, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	canvas := image.NewRGBA(src.Bounds())
	imagedraw.Draw(canvas, canvas.Bounds(), src, image.Point{}, imagedraw.Src)
	drawDialEdgeSeparators(canvas, sepWidth, sepColor)
	if showIndicator {
		// Draw the page indicator in the LEFT column (vertically), matching the
		// stacked overview. In fullscreen the graph fills the bottom and builds up
		// from the right, so a left gutter overwrites the least of it — the old
		// bottom-aligned horizontal indicator sat right on top of the graph fill.
		drawDialVerticalPageIndicator(canvas, active, count, indicatorStyle, indicatorColor, indicatorSize)
	}
	var out bytes.Buffer
	if err := png.Encode(&out, canvas); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func (p *Plugin) renderDialOverview(settings *dialActionSettings, state *dialState) ([]byte, error) {
	canvas := image.NewRGBA(image.Rect(0, 0, dialWidth, dialHeight))
	fillRect(canvas, canvas.Bounds(), color.RGBA{5, 8, 11, 255})

	indices := dialOverviewIndices(settings.ActiveIndex, len(settings.Pages), dialOverviewCap(settings))
	if len(indices) == 0 {
		return nil, nil
	}

	rects := dialOverviewRects(len(indices))

	for slot, pageIndex := range indices {
		if pageIndex < 0 || pageIndex >= len(state.graphs) || state.graphs[pageIndex] == nil {
			continue
		}
		card := rects[slot]
		fillRect(canvas, card, color.RGBA{14, 18, 24, 255})
		b, err := state.graphs[pageIndex].EncodePNG()
		if err != nil {
			return nil, err
		}
		src, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		inner := card.Inset(3)
		xdraw.CatmullRom.Scale(canvas, inner, src, centeredAspectCrop(src.Bounds(), inner), xdraw.Over, nil)
		if pageIndex == settings.ActiveIndex {
			strokeRect(canvas, card, color.RGBA{0, 150, 255, 255}, 2)
		} else {
			strokeRect(canvas, card, color.RGBA{40, 50, 62, 255}, 1)
		}
	}

	drawDialEdgeSeparators(canvas, dialSeparatorWidth(settings), dialSeparatorColor(settings))
	drawDialPageIndicator(canvas, settings.ActiveIndex, len(settings.Pages), dialIndicatorStyle(settings), dialIndicatorColor(settings), dialIndicatorSize(settings))

	var buf bytes.Buffer
	if err := png.Encode(&buf, canvas); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// dialStackedLeftColumn is the default width reserved on the left of the stacked
// view for the vertical page indicator, keeping the right side (where the graph
// builds up) clear. The actual width scales with the indicator size via
// dialStackedGutter so a larger size visibly widens the indicator gutter.
const dialStackedLeftColumn = 12

// dialStackedGutter returns the left-column width reserved for the vertical page
// indicator, scaled by the indicator size. A larger size widens the gutter so
// both the dots and the page number grow visibly (and the strips shift right),
// instead of being clamped by a fixed narrow column.
func dialStackedGutter(size float64) int {
	if size < 1 {
		size = 1
	}
	if size > 8 {
		size = 8
	}
	w := int(size*2 + 6 + 0.5)
	if w < 8 {
		w = 8
	}
	if w > 24 {
		w = 24
	}
	return w
}

// dialStackedLayout returns, for the stacked overview, the page indices to draw
// top-to-bottom, the slot holding the active page, and the matching strip
// rectangles. The strips are equal height so every reading is equally legible;
// the active page sits in the middle (with three pages) and is marked by its
// border, with the previous/next page above and below. The strips start after
// the reserved indicator gutter (gutter px wide).
func dialStackedLayout(active, count, gutter, limit int) (indices []int, activeSlot int, rects []image.Rectangle) {
	x0, x1 := gutter, dialWidth
	if limit < 2 {
		limit = 2
	}
	if count <= 1 {
		return []int{active}, 0, []image.Rectangle{image.Rect(x0, 2, x1, 98)}
	}
	if count == 2 || limit == 2 {
		// Two equal strips: active on top, next below. When exactly two pages exist
		// the lower strip wraps to the other page; when capping a larger set to two
		// it shows the next page.
		return []int{active, wrapDialIndex(active, 1, count)}, 0, []image.Rectangle{
			image.Rect(x0, 1, x1, 49),
			image.Rect(x0, 51, x1, 99),
		}
	}
	// Three equal strips: previous / active / next, the active one in the middle.
	return []int{
			wrapDialIndex(active, -1, count),
			active,
			wrapDialIndex(active, 1, count),
		}, 1, []image.Rectangle{
			image.Rect(x0, 1, x1, 33),
			image.Rect(x0, 34, x1, 66),
			image.Rect(x0, 67, x1, 99),
		}
}

// drawDialVerticalPageIndicator draws the page indicator in the reserved left
// column. It honours the explicit choice with no silent fallback: "dots" always
// draws dots (auto-shrinking to fit), "count" always draws the page number, and
// only "auto" decides for itself (dots while they stay legible, otherwise the
// number). "off" hides it.
func drawDialVerticalPageIndicator(img *image.RGBA, active, count int, style string, indicatorColor color.RGBA, size float64) {
	if count <= 1 || style == "off" {
		return
	}
	if active < 0 {
		active = 0
	}
	if active >= count {
		active = count - 1
	}
	if size < 1 {
		size = 1
	}
	if size > 8 {
		size = 8
	}
	if style == "" {
		style = "auto"
	}
	colW := dialStackedGutter(size)
	// Auto decides for itself; dots/count are honoured literally.
	if style == "count" || (style == "auto" && count > 9) {
		drawDialVerticalCount(img, active, count, indicatorColor, size, colW)
		return
	}
	drawDialVerticalDots(img, active, count, indicatorColor, size, colW)
}

// drawDialVerticalDots draws the page indicator as a vertical stack of dots with
// the active dot elongated, auto-shrinking to fit when there are many pages. The
// dots are centred in the colW-wide gutter and scale with size.
func drawDialVerticalDots(img *image.RGBA, active, count int, indicatorColor color.RGBA, size float64, colW int) {
	inactive := color.RGBA{
		uint8(float64(indicatorColor.R) * 0.55),
		uint8(float64(indicatorColor.G) * 0.55),
		uint8(float64(indicatorColor.B) * 0.55),
		255,
	}
	dotW := int(size*0.75 + 0.5)
	if dotW < 1 {
		dotW = 1
	}
	if dotW > colW-2 {
		dotW = colW - 2
	}
	dotH := int(size + 0.5)
	if dotH < 1 {
		dotH = 1
	}
	gap := int(size + 0.5)
	if gap < 1 {
		gap = 1
	}
	activeH := int(size*2.5 + 0.5)
	if activeH < 2 {
		activeH = 2
	}
	total := func() int {
		t := 0
		for i := 0; i < count; i++ {
			if i > 0 {
				t += gap
			}
			if i == active {
				t += activeH
			} else {
				t += dotH
			}
		}
		return t
	}
	avail := dialHeight - 6
	if t := total(); t > avail {
		f := float64(avail) / float64(t)
		dotH = int(float64(dotH) * f)
		if dotH < 1 {
			dotH = 1
		}
		gap = int(float64(gap) * f)
		if gap < 1 {
			gap = 1
		}
		activeH = int(float64(activeH) * f)
		if activeH < 2 {
			activeH = 2
		}
	}
	t := total()
	y := (dialHeight - t) / 2
	if y < 0 {
		y = 0
	}
	x := (colW - dotW) / 2
	if x < 1 {
		x = 1
	}
	for i := 0; i < count; i++ {
		h := dotH
		c := inactive
		if i == active {
			h = activeH
			c = indicatorColor
		}
		fillRect(img, image.Rect(x, y, x+dotW, y+h), c)
		y += h + gap
	}
}

// drawDialVerticalCount draws the explicit "count" indicator in the narrow left
// column as the current page number above the total, separated by a fraction
// bar, so it reads as "current/total". The face is shrunk until the widest line
// fits the column, so the chosen count is always shown (never silently dropped).
func drawDialVerticalCount(img *image.RGBA, active, count int, indicatorColor color.RGBA, size float64, colW int) {
	cur := fmt.Sprintf("%d", active+1)
	tot := fmt.Sprintf("%d", count)
	inactive := color.RGBA{
		uint8(float64(indicatorColor.R) * 0.55),
		uint8(float64(indicatorColor.G) * 0.55),
		uint8(float64(indicatorColor.B) * 0.55),
		255,
	}
	faceSize := size*1.8 + 4
	// No artificial height cap: the narrow column only limits the digit *width*,
	// which the fit loop below enforces. Capping faceSize here made the upper
	// half of the size range render identically (size 6 and 8 were the same).
	var face font.Face
	for {
		f, err := graph.GetSharedFontFaceManager().GetFaceOfSize(faceSize)
		if err != nil {
			return
		}
		d := &font.Drawer{Face: f}
		w := d.MeasureString(cur).Round()
		if w2 := d.MeasureString(tot).Round(); w2 > w {
			w = w2
		}
		if w <= colW-1 || faceSize <= 6 {
			face = f
			break
		}
		faceSize -= 1
	}
	m := face.Metrics()
	lineH := (m.Ascent + m.Descent).Ceil()
	ascent := m.Ascent.Ceil()
	const gap = 3
	totalH := lineH*2 + gap
	top := (dialHeight - totalH) / 2
	if top < 0 {
		top = 0
	}
	drawDialColumnCenteredText(img, face, cur, colW, top+ascent, indicatorColor)
	barY := top + lineH + gap/2
	fillRect(img, image.Rect(2, barY, colW-2, barY+1), indicatorColor)
	drawDialColumnCenteredText(img, face, tot, colW, top+lineH+gap+ascent, inactive)
}

// drawDialColumnCenteredText draws text horizontally centred within [0,colW] at
// the given baseline.
func drawDialColumnCenteredText(img *image.RGBA, face font.Face, text string, colW, baselineY int, clr color.RGBA) {
	d := &font.Drawer{Dst: img, Src: image.NewUniform(clr), Face: face}
	w := d.MeasureString(text).Round()
	x := (colW - w) / 2
	if x < 0 {
		x = 0
	}
	d.Dot = fixed.P(x, baselineY)
	d.DrawString(text)
}

// setDialPixel sets one pixel if it falls inside the image bounds.
func setDialPixel(img *image.RGBA, x, y int, c color.RGBA) {
	if (image.Point{X: x, Y: y}).In(img.Bounds()) {
		img.SetRGBA(x, y, c)
	}
}

// drawDialSparkline plots a graph's history as a filled area chart that fills the
// whole rect, mapping the most recent sample to the right edge so the graph
// visibly builds rightward. The series is scaled to the rect height natively, so
// the data is never distorted by cropping or stretching a pre-rendered tile.
func drawDialSparkline(img *image.RGBA, rect image.Rectangle, g *graph.Graph) {
	series := g.Series()
	if len(series) == 0 {
		return
	}
	effH := g.EffectiveHeight()
	if effH < 2 {
		effH = 2
	}
	fg := g.ForegroundColor()
	hl := g.HighlightColor()
	w, h := rect.Dx(), rect.Dy()
	if w <= 0 || h <= 0 {
		return
	}
	n := len(series)
	for col := 0; col < w; col++ {
		idx := n - 1 - (w - 1 - col)
		if idx < 0 {
			continue // not enough history yet; leave the left side empty
		}
		frac := float64(series[idx]) / float64(effH-1)
		if frac < 0 {
			frac = 0
		} else if frac > 1 {
			frac = 1
		}
		fillH := int(frac*float64(h) + 0.5)
		x := rect.Min.X + col
		lineY := rect.Max.Y - 1 - fillH
		if lineY < rect.Min.Y {
			lineY = rect.Min.Y
		}
		for y := rect.Max.Y - 1; y > lineY; y-- {
			setDialPixel(img, x, y, fg)
		}
		setDialPixel(img, x, lineY, hl)
	}
}

// drawDialStripText draws left-aligned text at the given baseline, outlined with
// strokeClr so it stays legible over the graph behind it.
func drawDialStripText(img *image.RGBA, rect image.Rectangle, text string, size float64, clr, strokeClr color.RGBA, baselineY int) {
	if text == "" {
		return
	}
	face, err := graph.GetSharedFontFaceManager().GetFaceOfSize(size)
	if err != nil {
		return
	}
	x := rect.Min.X + 2
	stroke := &font.Drawer{Dst: img, Src: image.NewUniform(strokeClr), Face: face}
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			stroke.Dot = fixed.P(x+dx, baselineY+dy)
			stroke.DrawString(text)
		}
	}
	d := &font.Drawer{Dst: img, Src: image.NewUniform(clr), Face: face, Dot: fixed.P(x, baselineY)}
	d.DrawString(text)
}

// drawDialStrip renders one stacked-overview strip natively at its own size: a
// full-width filled sparkline (newest sample on the right, where the graph keeps
// building) with the page title and current value drawn over the left, outlined
// for legibility. All strips are equal height and show the title and value; the
// active strip gets a bright border while the neighbours are lightly dimmed. No
// scaling or cropping is used, so the graph is never distorted.
func drawDialStrip(canvas *image.RGBA, rect image.Rectangle, g *graph.Graph, active bool) {
	fillRect(canvas, rect, color.RGBA{14, 18, 24, 255})
	inner := rect.Inset(1)
	drawDialSparkline(canvas, inner, g)

	if !active {
		// Lightly dim the neighbouring strips so the active reading stays the focus.
		imagedraw.Draw(canvas, rect, &image.Uniform{C: color.RGBA{0, 0, 0, 70}}, image.Point{}, imagedraw.Over)
	}

	title := strings.TrimSpace(g.LabelText(0))
	value := strings.TrimSpace(g.LabelText(1))
	stroke := color.RGBA{0, 0, 0, 220}
	titleColor := color.RGBA{200, 206, 214, 255}
	if c, ok := g.LabelColor(0); ok {
		titleColor = c
	}
	valueColor := color.RGBA{255, 255, 255, 255}
	if c, ok := g.LabelColor(1); ok {
		valueColor = c
	}
	drawDialStripText(canvas, inner, title, 10, titleColor, stroke, inner.Min.Y+11)
	drawDialStripText(canvas, inner, value, 15, valueColor, stroke, inner.Max.Y-5)

	if active {
		strokeRect(canvas, rect, color.RGBA{0, 150, 255, 255}, 2)
	} else {
		strokeRect(canvas, rect, color.RGBA{40, 50, 62, 255}, 1)
	}
}

// renderDialStacked renders the vertically-scrolling stacked overview: full-width
// strips with the active reading dominant in the centre and a dimmed peek of the
// previous/next page above and below, plus the vertical page indicator on the
// left.
func (p *Plugin) renderDialStacked(settings *dialActionSettings, state *dialState) ([]byte, error) {
	canvas := image.NewRGBA(image.Rect(0, 0, dialWidth, dialHeight))
	fillRect(canvas, canvas.Bounds(), color.RGBA{5, 8, 11, 255})

	count := len(settings.Pages)
	if count == 0 {
		return nil, nil
	}
	indices, activeSlot, rects := dialStackedLayout(settings.ActiveIndex, count, dialStackedGutter(dialIndicatorSize(settings)), dialOverviewCap(settings))

	for slot, pageIndex := range indices {
		if pageIndex < 0 || pageIndex >= len(state.graphs) || state.graphs[pageIndex] == nil {
			continue
		}
		drawDialStrip(canvas, rects[slot], state.graphs[pageIndex], slot == activeSlot)
	}

	drawDialVerticalPageIndicator(canvas, settings.ActiveIndex, count, dialIndicatorStyle(settings), dialIndicatorColor(settings), dialIndicatorSize(settings))

	var buf bytes.Buffer
	if err := png.Encode(&buf, canvas); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func dialPageContext(ctx string, index int) string {
	return fmt.Sprintf("%s|dial|page|%d", ctx, index)
}

func (p *Plugin) updateDialPage(ctx string, settings *dialActionSettings, state *dialState, index int, active bool, now time.Time) (dialPageRender, bool) {
	var render dialPageRender
	if index < 0 || index >= len(settings.Pages) {
		return render, false
	}
	if index >= len(state.graphs) || state.graphs[index] == nil {
		return render, false
	}

	page := &settings.Pages[index]
	if !page.IsValid {
		if active {
			render.messageTitle = "HWiNFO Dial"
			render.messageValue = "Page empty"
		}
		return render, false
	}

	pageCtx := dialPageContext(ctx, index)
	g := state.graphs[index]
	pollTime, err := p.getCachedPollTime()
	if err != nil || pollTime == 0 {
		if active {
			render.messageTitle = "HWiNFO Dial"
			render.messageValue = "HWiNFO unavailable"
		}
		return render, false
	}

	settingsChanged := false
	r, readings, err := p.getReading(page.SensorUID, page.ReadingID)
	if err != nil && page.ReadingLabel != "" {
		for _, candidate := range readings {
			if candidate.Label() == page.ReadingLabel {
				page.ReadingID = candidate.ID()
				r = candidate
				err = nil
				settingsChanged = true
				break
			}
		}
	}
	if err != nil {
		if active {
			render.messageTitle = "HWiNFO Dial"
			render.messageValue = "Reading missing"
		}
		return render, settingsChanged
	}

	title := page.Title
	if title == "" {
		title = r.Label()
	}
	if page.ShowTitleInGraph == nil || *page.ShowTitleInGraph {
		_ = g.SetLabelText(0, title)
	} else {
		_ = g.SetLabelText(0, "")
	}

	v := r.Value()
	divisor, err := p.getCachedDivisor(pageCtx, page.Divisor)
	if err != nil {
		log.Printf("dial divisor: %v", err)
		return render, settingsChanged
	}
	if divisor != 1 {
		v /= divisor
	}

	graphValue := r.Value()
	if page.GraphUnit != "" && strings.Contains(r.Unit(), "/s") {
		graphValue = p.normalizeForGraph(r.Value(), r.Unit(), page.GraphUnit)
	}
	if divisor != 1 {
		graphValue /= divisor
	}

	displayValue := v
	displayUnit := r.Unit()
	if page.GraphUnit != "" && strings.Contains(r.Unit(), "/s") {
		displayValue = p.normalizeForGraph(v, r.Unit(), page.GraphUnit)
		displayUnit = page.GraphUnit + "/s"
	}

	// EMA smoothing — same behavior as the normal reading tile (plugin.go),
	// keyed per page. Threshold eval uses raw v; smoothing affects graph/display.
	if alpha := page.SmoothingAlpha; alpha > 0 && alpha < 1.0 {
		p.mu.Lock()
		prev, ok := p.smoothedValues[pageCtx]
		if !ok {
			prev = graphValue
		}
		smoothed := emaSmooth(alpha, graphValue, prev)
		p.smoothedValues[pageCtx] = smoothed
		p.mu.Unlock()
		if graphValue != 0 {
			ratio := smoothed / graphValue
			graphValue = smoothed
			displayValue *= ratio
		}
	}

	valueTextNoUnit, displayText := p.formatDisplayValue(displayValue, displayUnit, page.Format, hwsensorsservice.ReadingType(r.TypeI()))

	thresholds := p.resolveThresholdsForEval(page.Thresholds, page.SuppressedGlobalIDs, hwsensorsservice.ReadingType(r.TypeI()))
	activeThreshold := p.evaluateThresholds(pageCtx, v, thresholds, now)
	newThresholdID := ""
	alertText := ""
	if activeThreshold != nil {
		newThresholdID = activeThreshold.ID
		if activeThreshold.Text != "" {
			alertText = p.applyThresholdText(activeThreshold.Text, valueTextNoUnit, displayUnit)
		}
	}

	snoozeState, snoozed, snoozeChanged := p.currentThresholdSnoozeState(pageCtx, now)
	forceUpdate := snoozeChanged || p.consumeThresholdDirty(pageCtx)
	if forceUpdate || newThresholdID != page.CurrentThresholdID {
		if activeThreshold != nil && !snoozed {
			p.applyThresholdColors(g, activeThreshold)
			// Edge into an active, non-snoozed alarm (incl. snooze just timing out):
			// request bring-to-front when the firing threshold asks for it.
			render.bringToFront = activeThreshold.BringToFront
		} else {
			p.applyNormalColors(g, page)
		}
		page.CurrentThresholdID = newThresholdID
		settingsChanged = true
	}

	renderDisplayText, renderAlertText, renderGraphValue, freezeGraph := p.resolveThresholdDisplay(
		pageCtx,
		activeThreshold,
		v,
		graphValue,
		displayText,
		alertText,
	)
	if snoozed {
		renderDisplayText = displayText
		renderAlertText = thresholdSnoozeText(snoozeState, now)
		renderGraphValue = graphValue
		freezeGraph = false
	}

	switch page.GraphMode {
	case "text":
		g.Clear()
	default:
		if !freezeGraph {
			g.Update(renderGraphValue)
		}
	}
	if page.GraphMode == "graph" {
		_ = g.SetLabelText(0, "")
		_ = g.SetLabelText(1, "")
		_ = g.SetLabelText(2, "")
	} else {
		_ = g.SetLabelText(1, renderDisplayText)
		if renderAlertText != "" {
			_ = g.SetLabelText(2, renderAlertText)
		} else {
			_ = g.SetLabelText(2, "")
		}
	}

	if active {
		b, err := g.EncodePNG()
		if err != nil {
			log.Printf("dial encode: %v", err)
			return render, settingsChanged
		}
		b, err = decorateDialImage(b, settings.ActiveIndex, len(settings.Pages), dialIndicatorFullscreen(settings), dialIndicatorStyle(settings), dialSeparatorWidth(settings), dialSeparatorColor(settings), dialIndicatorColor(settings), dialIndicatorSize(settings))
		if err != nil {
			log.Printf("dial decorate: %v", err)
			return render, settingsChanged
		}
		render.image = b
	}
	return render, settingsChanged
}

func (p *Plugin) updateDialFeedback(ctx string) {
	p.mu.RLock()
	settings := p.dialSettings[ctx]
	state := p.dialStates[ctx]
	p.mu.RUnlock()
	if settings == nil || state == nil {
		return
	}
	if len(settings.Pages) == 0 {
		p.showDialMessage(ctx, "HWiNFO Dial", "Configure pages")
		return
	}
	if settings.ActiveIndex < 0 || settings.ActiveIndex >= len(settings.Pages) {
		settings.ActiveIndex = wrapDialIndex(settings.ActiveIndex, 0, len(settings.Pages))
	}
	now := time.Now()
	settingsChanged := false
	bringToFront := -1
	var activeRender dialPageRender
	for i := range settings.Pages {
		render, changed := p.updateDialPage(ctx, settings, state, i, i == settings.ActiveIndex, now)
		if changed {
			settingsChanged = true
		}
		if render.bringToFront && bringToFront == -1 {
			bringToFront = i
		}
		if i == settings.ActiveIndex {
			activeRender = render
		}
	}
	// A page whose alarm just fired (with bring-to-front) becomes the active page.
	// The new page renders on the next tick; we only move the selection here to
	// avoid double-advancing graphs by re-rendering within this tick.
	if bringToFront >= 0 && bringToFront != settings.ActiveIndex {
		settings.ActiveIndex = bringToFront
		settingsChanged = true
	}
	if settingsChanged {
		_ = p.sd.SetSettings(ctx, settings)
	}
	if state.overview {
		var b []byte
		var err error
		if dialOverviewStyle(settings) == "stacked" {
			b, err = p.renderDialStacked(settings, state)
		} else {
			b, err = p.renderDialOverview(settings, state)
		}
		if err != nil {
			log.Printf("dial overview encode: %v", err)
			return
		}
		if len(b) > 0 {
			p.sendDialCanvas(ctx, b)
		}
		return
	}
	if activeRender.messageTitle != "" {
		p.showDialMessage(ctx, activeRender.messageTitle, activeRender.messageValue)
		return
	}
	if len(activeRender.image) == 0 {
		return
	}
	p.sendDialCanvas(ctx, activeRender.image)
}

func (p *Plugin) updateDialTick() {
	p.mu.RLock()
	contexts := make([]string, 0, len(p.dialSettings))
	for ctx := range p.dialSettings {
		contexts = append(contexts, ctx)
	}
	p.mu.RUnlock()
	for _, ctx := range contexts {
		p.updateDialFeedback(ctx)
	}
}

func (p *Plugin) handleDialWillAppear(event *streamdeck.EvWillAppear) {
	settings, err := decodeDialSettings(event.Payload.Settings)
	if err != nil {
		log.Printf("dial settings unmarshal: %v", err)
	}
	state := initDialState(&settings)
	state.overview = dialDefaultOverview(&settings)
	p.mu.Lock()
	p.dialSettings[event.Context] = &settings
	p.dialStates[event.Context] = state
	p.mu.Unlock()
	if err := p.sd.SetFeedbackLayout(event.Context, "$A0"); err != nil {
		log.Printf("dial setFeedbackLayout: %v", err)
	}
	p.updateDialFeedback(event.Context)
}

func (p *Plugin) handleDialWillDisappear(event *streamdeck.EvWillDisappear) {
	p.mu.Lock()
	delete(p.dialSettings, event.Context)
	delete(p.dialStates, event.Context)
	p.mu.Unlock()
}

func (p *Plugin) handleDialPropertyInspectorConnected(event *streamdeck.EvSendToPlugin) {
	p.mu.RLock()
	settings := p.dialSettings[event.Context]
	var settingsCopy dialActionSettings
	if settings != nil {
		settingsCopy = *settings
	}
	globals := make([]Threshold, len(p.globalSettings.GlobalThresholds))
	copy(globals, p.globalSettings.GlobalThresholds)
	p.mu.RUnlock()

	sensors, err := p.sensorsWithTimeout(2*time.Second)
	if err != nil {
		log.Printf("dial PI sensors: %v", err)
		go p.restartBridge(p.bridge)
		_ = p.sd.SendToPropertyInspector(event.Action, event.Context, evStatus{Error: true, Message: "HWiNFO Unavailable"})
		return
	}
	_ = p.sd.SendToPropertyInspector(event.Action, event.Context, map[string]interface{}{"error": false, "message": "show_ui"})
	catalogSettings := actionSettings{}
	if err := p.sendCatalogToPropertyInspector(event.Action, event.Context, &catalogSettings, sensors); err != nil {
		log.Printf("dial PI catalog: %v", err)
	}
	_ = p.sd.SendToPropertyInspector(event.Action, event.Context, map[string]interface{}{
		"dialSettings":     settingsCopy,
		"globalThresholds": globals,
	})
}

// deriveDialPageScales fills in a default graph scale (min/max) for any page that
// has none yet (max <= min), using the same logic as the normal reading tile
// (getDefaultMinMaxForReading). Returns true if any page scale changed.
func (p *Plugin) deriveDialPageScales(settings *dialActionSettings) bool {
	if settings == nil {
		return false
	}
	changed := false
	for i := range settings.Pages {
		pg := &settings.Pages[i]
		if pg.Max > pg.Min {
			continue
		}
		min, max := 0, 100
		if r, _, err := p.getReading(pg.SensorUID, pg.ReadingID); err == nil {
			min, max = getDefaultMinMaxForReading(r)
		}
		if pg.Min != min || pg.Max != max {
			pg.Min, pg.Max = min, max
			changed = true
		}
	}
	return changed
}

func (p *Plugin) handleDialSendToPlugin(event *streamdeck.EvSendToPlugin, payload map[string]*json.RawMessage) bool {
	if event.Action != dialAction {
		return false
	}
	if raw, ok := payload["dialSetSettings"]; ok {
		var settings dialActionSettings
		if err := json.Unmarshal(*raw, &settings); err != nil {
			log.Printf("dialSetSettings unmarshal: %v", err)
			return true
		}
		if len(settings.Pages) > 0 {
			settings.ActiveIndex = wrapDialIndex(settings.ActiveIndex, 0, len(settings.Pages))
		} else {
			settings.ActiveIndex = 0
		}
		// Derive a sensible default graph scale from the selected reading for any
		// page that has no explicit scale yet (max <= min), mirroring the normal
		// reading tile (getDefaultMinMaxForReading) instead of hardcoding 0-100.
		scalesDerived := p.deriveDialPageScales(&settings)
		p.mu.Lock()
		oldSettings := p.dialSettings[event.Context]
		oldState := p.dialStates[event.Context]
		overview := oldState != nil && oldState.overview
		if oldSettings == nil || oldSettings.DefaultView != settings.DefaultView {
			overview = dialDefaultOverview(&settings)
		}
		newState := &dialState{
			graphs:   buildDialGraphs(oldSettings, oldState, &settings),
			overview: overview,
		}
		p.dialSettings[event.Context] = &settings
		p.dialStates[event.Context] = newState
		p.mu.Unlock()
		if err := p.sd.SetSettings(event.Context, &settings); err != nil {
			log.Printf("dialSetSettings SetSettings: %v", err)
		}
		// Echo corrected settings back so the PI reflects the derived scale.
		if scalesDerived {
			_ = p.sd.SendToPropertyInspector(event.Action, event.Context, map[string]interface{}{"dialSettings": settings})
		}
		p.updateDialFeedback(event.Context)
		return true
	}
	if _, ok := payload["requestDialCatalog"]; ok {
		p.handleDialPropertyInspectorConnected(event)
		return true
	}
	return true
}

func (p *Plugin) OnDialDown(event *streamdeck.EvDialDown) {
	if event.Action != dialAction || event.Payload.Controller != "Encoder" {
		return
	}

	p.mu.Lock()
	state := p.dialStates[event.Context]
	settings := p.dialSettings[event.Context]
	if state == nil || settings == nil || len(settings.Pages) == 0 {
		p.mu.Unlock()
		return
	}
	state.overview = !state.overview
	p.mu.Unlock()

	p.updateDialFeedback(event.Context)
}

func (p *Plugin) OnDialUp(event *streamdeck.EvDialUp) {}

func (p *Plugin) OnTouchTap(event *streamdeck.EvTouchTap) {
	if event.Action != dialAction || event.Payload.Controller != "Encoder" {
		return
	}

	p.mu.RLock()
	settings := p.dialSettings[event.Context]
	p.mu.RUnlock()
	if settings == nil || len(settings.Pages) == 0 {
		return
	}
	if settings.ActiveIndex < 0 || settings.ActiveIndex >= len(settings.Pages) {
		settings.ActiveIndex = wrapDialIndex(settings.ActiveIndex, 0, len(settings.Pages))
	}
	page := &settings.Pages[settings.ActiveIndex]

	pageCtx := dialPageContext(event.Context, settings.ActiveIndex)
	now := time.Now()
	currentThresholdID := page.CurrentThresholdID
	if !p.handleDialPageTouch(pageCtx, page, now) {
		return
	}
	if currentThresholdID != page.CurrentThresholdID {
		if err := p.sd.SetSettings(event.Context, settings); err != nil {
			log.Printf("dial touch SetSettings: %v", err)
		}
	}
	p.updateDialFeedback(event.Context)
}

func (p *Plugin) handleDialPageTouch(pageCtx string, page *actionSettings, now time.Time) bool {
	configured := normalizeThresholdSnoozeDurations(page.SnoozeDurations)
	_, snoozed := p.currentThresholdSnooze(pageCtx, now)
	if len(configured) > 0 && (page.CurrentThresholdID != "" || snoozed || len(page.Thresholds) > 0) {
		return p.advanceConfiguredThresholdSnooze(pageCtx, configured, now)
	}

	if page.CurrentThresholdID == "" {
		return false
	}
	if !p.clearStickyThreshold(pageCtx, page.CurrentThresholdID) {
		return false
	}
	page.CurrentThresholdID = ""
	return true
}

func (p *Plugin) OnDialRotate(event *streamdeck.EvDialRotate) {
	if event.Action != dialAction || event.Payload.Controller != "Encoder" {
		return
	}
	if event.Payload.Ticks == 0 {
		return
	}

	p.mu.Lock()
	settings := p.dialSettings[event.Context]
	if settings == nil {
		p.mu.Unlock()
		return
	}
	ticks := dialRotateTicks(event.Payload.Ticks, settings.ReverseRotation)
	settings.ActiveIndex = wrapDialIndex(settings.ActiveIndex, ticks, len(settings.Pages))
	settingsCopy := *settings
	p.mu.Unlock()

	if err := p.sd.SetSettings(event.Context, &settingsCopy); err != nil {
		log.Printf("dial SetSettings: %v", err)
	}
	// Keep the Property Inspector in sync with the active page so its selected
	// page and edited settings follow what the dial now shows.
	_ = p.sd.SendToPropertyInspector(event.Action, event.Context, map[string]interface{}{"dialSettings": settingsCopy})
	p.updateDialFeedback(event.Context)
}
