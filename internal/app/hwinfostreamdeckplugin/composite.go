package hwinfostreamdeckplugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/moeilijk/hwinfo-streamdeck/pkg/graph"
	hwsensorsservice "github.com/moeilijk/hwinfo-streamdeck/pkg/service"
	"github.com/moeilijk/hwinfo-streamdeck/pkg/streamdeck"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

const compositeAction = "com.exension.hwinfo.composite"

const (
	compositeModeText  = "text"
	compositeModeGraph = "graph"
	compositeModeBoth  = "both"
)

// compositeSlotDefaults mirrors the original tile colours exactly for slot 0,
// with a per-slot hue shift for slots 1–3 so readings are distinguishable.
var compositeSlotDefaults = [4]compositeSlotSettings{
	{ForegroundColor: "#005128", HighlightColor: "#009E00", ValueTextColor: "#009E00", TitleColor: "#888888", BackgroundColor: "#000000", FillAlpha: 55, TitleFontSize: 9.0, ValueFontSize: 10.5},
	{ForegroundColor: "#004050", HighlightColor: "#009EBE", ValueTextColor: "#009EBE", TitleColor: "#888888", BackgroundColor: "#000000", FillAlpha: 55, TitleFontSize: 9.0, ValueFontSize: 10.5},
	{ForegroundColor: "#502800", HighlightColor: "#BE6000", ValueTextColor: "#BE6000", TitleColor: "#888888", BackgroundColor: "#000000", FillAlpha: 55, TitleFontSize: 9.0, ValueFontSize: 10.5},
	{ForegroundColor: "#500050", HighlightColor: "#9E009E", ValueTextColor: "#9E009E", TitleColor: "#888888", BackgroundColor: "#000000", FillAlpha: 55, TitleFontSize: 9.0, ValueFontSize: 10.5},
}

// compositeState holds runtime state for one composite tile context.
// One graph.Graph per slot — same object the original tile uses.
type compositeState struct {
	graphs         [4]*graph.Graph
	lastPollTime   uint64
	divisorCache   [4]divisorCacheEntry
	smoothedValues [4]float64
	smoothedInit   [4]bool
}

// newCompositeGraph creates a graph.Graph for one slot using its settings.
// FillAlpha scales the foreground (fill) colour; highlight stays at full brightness.
func newCompositeGraph(slot *compositeSlotSettings) *graph.Graph {
	fgColor := hexToRGBA(slot.ForegroundColor)
	bgColor := hexToRGBA(slot.BackgroundColor)
	hlColor := hexToRGBA(slot.HighlightColor)

	// Apply FillAlpha to the fill colour (0–100 → 0.0–1.0 scale).
	if slot.FillAlpha != 100 {
		f := float64(slot.FillAlpha) / 100.0
		fgColor = &color.RGBA{
			R: uint8(math.Round(float64(fgColor.R) * f)),
			G: uint8(math.Round(float64(fgColor.G) * f)),
			B: uint8(math.Round(float64(fgColor.B) * f)),
			A: 255,
		}
	}

	g := graph.NewGraph(tileWidth, tileHeight, slot.Min, slot.Max, fgColor, bgColor, hlColor)
	if slot.GraphHeightPct > 0 {
		g.SetHeightPct(slot.GraphHeightPct)
	}
	if slot.GraphLineThickness > 0 {
		g.SetLineThickness(slot.GraphLineThickness)
	}
	g.SetTextStroke(slot.TextStroke)
	if slot.TextStrokeColor != "" {
		g.SetTextStrokeColor(hexToRGBA(slot.TextStrokeColor))
	}
	return g
}

// initCompositeGraphs creates fresh graph.Graph instances for all slots.
func initCompositeGraphs(settings *compositeActionSettings) [4]*graph.Graph {
	var gs [4]*graph.Graph
	for i := 0; i < 4; i++ {
		gs[i] = newCompositeGraph(&settings.Slots[i])
	}
	return gs
}

// rebuildCompositeGraph replaces the graph for one slot after settings change.
func (p *Plugin) rebuildCompositeGraph(ctx string, slotIdx int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	settings, ok1 := p.compositeSettings[ctx]
	state, ok2 := p.compositeStates[ctx]
	if !ok1 || !ok2 {
		return
	}
	state.graphs[slotIdx] = newCompositeGraph(&settings.Slots[slotIdx])
}

// decodeCompositeSettings decodes raw JSON and fills in defaults for missing fields.
func decodeCompositeSettings(raw *json.RawMessage) (compositeActionSettings, error) {
	var s compositeActionSettings
	if raw != nil {
		if err := json.Unmarshal(*raw, &s); err != nil {
			return s, err
		}
	}
	if s.SlotCount < 2 || s.SlotCount > 4 {
		s.SlotCount = 2
	}
	if s.Mode == "" {
		s.Mode = "text"
	}
	for i := range s.Slots {
		d := compositeSlotDefaults[i]
		slot := &s.Slots[i]
		if slot.ForegroundColor == "" {
			slot.ForegroundColor = d.ForegroundColor
		}
		if slot.BackgroundColor == "" {
			slot.BackgroundColor = d.BackgroundColor
		}
		if slot.HighlightColor == "" {
			slot.HighlightColor = d.HighlightColor
		}
		if slot.ValueTextColor == "" {
			slot.ValueTextColor = d.ValueTextColor
		}
		if slot.TitleColor == "" {
			slot.TitleColor = d.TitleColor
		}
		if slot.FillAlpha == 0 {
			slot.FillAlpha = d.FillAlpha
		}
		if slot.TitleFontSize == 0 {
			slot.TitleFontSize = d.TitleFontSize
		}
		if slot.ValueFontSize == 0 {
			slot.ValueFontSize = d.ValueFontSize
		}
	}
	return s, nil
}

// --- rendering ---

func validCompositeMode(mode string) bool {
	return mode == compositeModeText || mode == compositeModeGraph || mode == compositeModeBoth
}

func effectiveCompositeSlotMode(tileMode, slotMode string) string {
	if validCompositeMode(slotMode) {
		return slotMode
	}
	if validCompositeMode(tileMode) {
		return tileMode
	}
	return compositeModeText
}

func compositeModeHasGraph(mode string) bool {
	return mode == compositeModeGraph || mode == compositeModeBoth
}

func compositeModeHasText(mode string) bool {
	return mode == compositeModeText || mode == compositeModeBoth
}

// blendLighten composites src onto dst using per-channel max (lighten/screen blend).
// Unlike additive blending, background pixels of the same near-black colour don't
// accumulate across slots, preventing colour-cast tinting on the canvas background.
func blendLighten(dst, src *image.RGBA) {
	b := dst.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			d := dst.RGBAAt(x, y)
			s := src.RGBAAt(x, y)
			maxU8 := func(a, b uint8) uint8 {
				if a > b {
					return a
				}
				return b
			}
			dst.SetRGBA(x, y, color.RGBA{
				R: maxU8(d.R, s.R),
				G: maxU8(d.G, s.G),
				B: maxU8(d.B, s.B),
				A: 255,
			})
		}
	}
}

// decodePNGToRGBA decodes a PNG byte slice into an *image.RGBA.
func decodePNGToRGBA(b []byte) (*image.RGBA, error) {
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	rgba, ok := img.(*image.RGBA)
	if !ok {
		// Convert to RGBA if not already.
		bounds := img.Bounds()
		rgba = image.NewRGBA(bounds)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				rgba.Set(x, y, img.At(x, y))
			}
		}
	}
	return rgba, nil
}

// drawCompositeCenteredText draws txt centred horizontally on img at baselineY.
// When strokeClr is non-nil, a 1 px outline is drawn in that color first.
func drawCompositeCenteredText(img *image.RGBA, txt string, baselineY int, size float64, clr *color.RGBA, strokeClr *color.RGBA) {
	if txt == "" || clr == nil {
		return
	}
	fm := graph.GetSharedFontFaceManager()
	f, err := fm.GetFaceOfSize(size)
	if err != nil {
		log.Printf("composite drawText font: %v", err)
		return
	}
	var w float64
	for _, r := range txt {
		if adv, ok := f.GlyphAdvance(r); ok {
			w += float64(adv) / 64
		}
	}
	cx := float64(tileWidth)/2 - w/2
	pt := fixed.Point26_6{
		X: fixed.Int26_6(cx * 64),
		Y: fixed.Int26_6(float64(baselineY) * 64),
	}
	d := &font.Drawer{Dst: img, Face: f}
	if strokeClr != nil {
		d.Src = image.NewUniform(strokeClr)
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				if dx == 0 && dy == 0 {
					continue
				}
				d.Dot = fixed.Point26_6{
					X: pt.X + fixed.Int26_6(dx*64),
					Y: pt.Y + fixed.Int26_6(dy*64),
				}
				d.DrawString(txt)
			}
		}
	}
	d.Src = image.NewUniform(clr)
	d.Dot = pt
	d.DrawString(txt)
}

// renderCompositeTile blends the per-slot graph.Graph renders additively,
// then draws text labels on top — matching the original tile's visual style.
func renderCompositeTile(settings *compositeActionSettings, state *compositeState, displayTexts [4]string, activeThresholds [4]*Threshold) ([]byte, error) {
	n := settings.SlotCount

	// Start with a black canvas.
	canvas := image.NewRGBA(image.Rect(0, 0, tileWidth, tileHeight))

	// --- graph layer: render each graph.Graph, blend onto canvas ---
	for i := 0; i < n; i++ {
		mode := effectiveCompositeSlotMode(settings.Mode, settings.Slots[i].Mode)
		if !compositeModeHasGraph(mode) {
			continue
		}
		g := state.graphs[i]
		if g == nil {
			continue
		}
		b, err := g.EncodePNG()
		if err != nil {
			continue
		}
		slotImg, err := decodePNGToRGBA(b)
		if err != nil {
			continue
		}
		blendLighten(canvas, slotImg)
	}

	// --- text layer: label + value centred per slot zone, drawn over graph ---
	zoneH := float64(tileHeight) / float64(n)
	for i := 0; i < n; i++ {
		slot := &settings.Slots[i]
		mode := effectiveCompositeSlotMode(settings.Mode, slot.Mode)
		if !compositeModeHasText(mode) {
			continue
		}
		mid := float64(i)*zoneH + zoneH/2

		titleSz := slot.TitleFontSize
		valueSz := slot.ValueFontSize
		gap := (titleSz + valueSz) / 2

		labelY := int(math.Round(mid - gap*0.3))
		valueY := int(math.Round(mid + gap*0.85))

		label := slot.Title
		if label == "" {
			label = slot.ReadingLabel
		}
		var strokeClr *color.RGBA
		if slot.TextStroke && slot.TextStrokeColor != "" {
			strokeClr = hexToRGBA(slot.TextStrokeColor)
		}
		titleClr := hexToRGBA(slot.TitleColor)
		valueClr := hexToRGBA(slot.ValueTextColor)
		if t := activeThresholds[i]; t != nil {
			if t.TextColor != "" {
				titleClr = hexToRGBA(t.TextColor)
			}
			if t.ValueTextColor != "" {
				valueClr = hexToRGBA(t.ValueTextColor)
			}
		}
		drawCompositeCenteredText(canvas, label, labelY, titleSz, titleClr, strokeClr)
		drawCompositeCenteredText(canvas, displayTexts[i], valueY, valueSz, valueClr, strokeClr)
	}

	var buf bytes.Buffer
	enc := &png.Encoder{CompressionLevel: png.NoCompression}
	if err := enc.Encode(&buf, canvas); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// applyNormalCompositeColors resets a slot's graph to its configured (non-alert) colors.
func applyNormalCompositeColors(g *graph.Graph, slot *compositeSlotSettings) {
	if slot.ForegroundColor != "" {
		g.SetForegroundColor(hexToRGBA(slot.ForegroundColor))
	}
	if slot.BackgroundColor != "" {
		g.SetBackgroundColor(hexToRGBA(slot.BackgroundColor))
	}
	if slot.HighlightColor != "" {
		g.SetHighlightColor(hexToRGBA(slot.HighlightColor))
	}
	if slot.ValueTextColor != "" {
		g.SetLabelColor(1, hexToRGBA(slot.ValueTextColor))
	}
}

// --- update logic ---

func (p *Plugin) compositeDivisor(ctx string, slotIdx int, raw string) (float64, error) {
	if raw == "" {
		return 1, nil
	}
	p.mu.RLock()
	state, ok := p.compositeStates[ctx]
	p.mu.RUnlock()
	if !ok {
		return 1, nil
	}
	if state.divisorCache[slotIdx].raw == raw {
		return state.divisorCache[slotIdx].value, nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, err
	}
	p.mu.Lock()
	if st, ok2 := p.compositeStates[ctx]; ok2 {
		st.divisorCache[slotIdx] = divisorCacheEntry{raw: raw, value: v}
	}
	p.mu.Unlock()
	return v, nil
}

func (p *Plugin) updateCompositeTile(ctx string) {
	p.mu.RLock()
	settings, ok := p.compositeSettings[ctx]
	state, ok2 := p.compositeStates[ctx]
	p.mu.RUnlock()
	if !ok || !ok2 {
		return
	}

	forceUpdate := p.consumeThresholdDirty(ctx)

	pollTime, err := p.getCachedPollTime()
	if err != nil || pollTime == 0 || time.Since(time.Unix(0, int64(pollTime))) > 5*time.Second {
		return
	}
	if !forceUpdate && pollTime == state.lastPollTime {
		return
	}

	if !forceUpdate {
		if override := settings.UpdateIntervalOverrideMs; override > 0 {
			p.mu.RLock()
			lastRender := p.lastRenderTime[ctx]
			p.mu.RUnlock()
			if time.Since(lastRender) < time.Duration(override)*time.Millisecond {
				return
			}
		}
	}

	var displayTexts [4]string
	var activeThresholds [4]*Threshold
	now := time.Now()
	n := settings.SlotCount

	for i := 0; i < n; i++ {
		slot := &settings.Slots[i]
		if !slot.IsValid || slot.SensorUID == "" {
			displayTexts[i] = "—"
			continue
		}

		r, _, err := p.getReading(slot.SensorUID, slot.ReadingID)
		if err != nil {
			displayTexts[i] = "—"
			continue
		}

		v := r.Value()
		divisor, err := p.compositeDivisor(ctx, i, slot.Divisor)
		if err == nil && divisor != 1 {
			v = v / divisor
		}

		graphValue := r.Value()
		if slot.GraphUnit != "" && strings.Contains(r.Unit(), "/s") {
			graphValue = p.normalizeForGraph(r.Value(), r.Unit(), slot.GraphUnit)
		}
		if divisor != 1 {
			graphValue = graphValue / divisor
		}

		// Format display text — same logic as updateTiles.
		displayUnit := r.Unit()
		displayV := v
		if slot.GraphUnit != "" && strings.Contains(r.Unit(), "/s") {
			displayV = p.normalizeForGraph(v, r.Unit(), slot.GraphUnit)
			displayUnit = slot.GraphUnit + "/s"
		}

		// EMA smoothing per slot
		if alpha := settings.SmoothingAlpha; alpha > 0 && alpha < 1.0 {
			p.mu.Lock()
			if !state.smoothedInit[i] {
				state.smoothedValues[i] = graphValue
				state.smoothedInit[i] = true
			}
			smoothed := alpha*graphValue + (1-alpha)*state.smoothedValues[i]
			state.smoothedValues[i] = smoothed
			p.mu.Unlock()
			if graphValue != 0 {
				ratio := smoothed / graphValue
				graphValue = smoothed
				displayV *= ratio
			}
		}

		// Threshold evaluation per slot — uses raw v, synthetic context key
		slotCtx := ctx + "|" + strconv.Itoa(i)
		p.mu.RLock()
		slotThresholds := p.resolveThresholdsForEval(slot.Thresholds, slot.SuppressedGlobalIDs, hwsensorsservice.ReadingType(r.TypeI()))
		p.mu.RUnlock()
		active := p.evaluateThresholds(slotCtx, v, slotThresholds, now)
		activeThresholds[i] = active

		// Feed value into the graph.Graph — same as the original tile.
		p.mu.RLock()
		g := state.graphs[i]
		p.mu.RUnlock()
		if g != nil {
			g.Update(graphValue)
			if forceUpdate || active != nil != (slot.CurrentThresholdID != "") {
				if active != nil {
					p.applyThresholdColors(g, active)
				} else {
					applyNormalCompositeColors(g, slot)
				}
			}
		}

		// Update stored threshold ID
		p.mu.Lock()
		if cs, ok3 := p.compositeSettings[ctx]; ok3 {
			newID := ""
			if active != nil {
				newID = active.ID
			}
			cs.Slots[i].CurrentThresholdID = newID
		}
		p.mu.Unlock()

		_, txt := p.formatDisplayValue(displayV, displayUnit, slot.Format, hwsensorsservice.ReadingType(r.TypeI()))
		displayTexts[i] = txt
	}

	p.mu.RLock()
	latestState, ok3 := p.compositeStates[ctx]
	latestSettings, ok4 := p.compositeSettings[ctx]
	p.mu.RUnlock()
	if !ok3 || !ok4 {
		return
	}

	b, err := renderCompositeTile(latestSettings, latestState, displayTexts, activeThresholds)
	if err != nil {
		log.Printf("renderCompositeTile: %v", err)
		return
	}
	if err := p.sd.SetImage(ctx, b); err != nil {
		log.Printf("composite SetImage: %v", err)
		return
	}

	p.mu.Lock()
	if st, ok5 := p.compositeStates[ctx]; ok5 {
		st.lastPollTime = pollTime
	}
	if settings.UpdateIntervalOverrideMs > 0 {
		p.lastRenderTime[ctx] = time.Now()
	}
	p.mu.Unlock()
}

func (p *Plugin) updateCompositeTick() {
	p.mu.RLock()
	contexts := make([]string, 0, len(p.compositeSettings))
	for ctx := range p.compositeSettings {
		contexts = append(contexts, ctx)
	}
	p.mu.RUnlock()
	for _, ctx := range contexts {
		p.updateCompositeTile(ctx)
	}
}

// --- PI handlers ---

func (p *Plugin) handleCompositePropertyInspectorConnected(event *streamdeck.EvSendToPlugin) {
	p.mu.RLock()
	settings, ok := p.compositeSettings[event.Context]
	p.mu.RUnlock()

	sensors, err := p.sensorsWithTimeout(2 * time.Second)
	if err != nil {
		log.Printf("composite PI connected sensors: %v", err)
		go p.restartBridge(p.bridge)
		_ = p.sd.SendToPropertyInspector(event.Action, event.Context, evStatus{Error: true, Message: "HWiNFO Unavailable"})
		return
	}

	evsensors := make([]*evSendSensorsPayloadSensor, 0, len(sensors))
	for _, s := range sensors {
		evsensors = append(evsensors, sensorPayload(s.ID(), s.Name()))
	}

	var settingsCopy compositeActionSettings
	if ok {
		settingsCopy = *settings
	} else {
		settingsCopy, _ = decodeCompositeSettings(nil)
	}

	p.mu.RLock()
	globals := make([]Threshold, len(p.globalSettings.GlobalThresholds))
	copy(globals, p.globalSettings.GlobalThresholds)
	p.mu.RUnlock()

	payload := map[string]interface{}{
		"sensors":           evsensors,
		"compositeSettings": settingsCopy,
		"globalThresholds":  globals,
	}
	if err := p.sd.SendToPropertyInspector(event.Action, event.Context, payload); err != nil {
		log.Printf("composite PI SendToPropertyInspector: %v", err)
	}

	for i := 0; i < settingsCopy.SlotCount; i++ {
		slot := settingsCopy.Slots[i]
		if slot.SensorUID == "" {
			continue
		}
		p.sendCompositeReadings(event.Action, event.Context, i, slot.SensorUID, &settingsCopy)
	}
}

func (p *Plugin) sendCompositeReadings(action, ctx string, slotIdx int, sensorUID string, settings *compositeActionSettings) {
	rt := p.bridge
	rt.mu.RLock()
	hw := rt.hw
	rt.mu.RUnlock()
	if hw == nil {
		return
	}
	readings, err := hw.ReadingsForSensorID(sensorUID)
	if err != nil {
		return
	}
	evr := make([]*evSendReadingsPayloadReading, 0, len(readings))
	for _, r := range readings {
		evr = append(evr, &evSendReadingsPayloadReading{
			ID:    r.ID(),
			Label: r.Label(),
			Unit:  r.Unit(),
			Type:  r.Type(),
		})
	}
	payload := map[string]interface{}{
		"readings":          evr,
		"slotIndex":         slotIdx,
		"compositeSettings": settings,
	}
	_ = p.sd.SendToPropertyInspector(action, ctx, payload)
}

func (p *Plugin) handleCompositeSlotSensorSelect(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection, slotIdx int) {
	p.mu.Lock()
	settings, ok := p.compositeSettings[event.Context]
	if !ok {
		p.mu.Unlock()
		return
	}
	settings.Slots[slotIdx].SensorUID = sdpi.Value
	settings.Slots[slotIdx].ReadingID = 0
	settings.Slots[slotIdx].ReadingLabel = ""
	settings.Slots[slotIdx].IsValid = false
	p.mu.Unlock()

	if err := p.sd.SetSettings(event.Context, settings); err != nil {
		log.Printf("composite sensorSelect SetSettings: %v", err)
	}
	p.sendCompositeReadings(event.Action, event.Context, slotIdx, sdpi.Value, settings)
}

func (p *Plugin) handleCompositeSlotReadingSelect(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection, slotIdx int) {
	rid64, err := strconv.ParseInt(sdpi.Value, 10, 32)
	if err != nil {
		log.Printf("composite readingSelect parse: %v", err)
		return
	}
	rid := int32(rid64)

	p.mu.Lock()
	settings, ok := p.compositeSettings[event.Context]
	if !ok {
		p.mu.Unlock()
		return
	}
	if settings.Slots[slotIdx].ReadingID == rid {
		p.mu.Unlock()
		return
	}
	settings.Slots[slotIdx].ReadingID = rid
	p.mu.Unlock()

	r, _, err := p.getReading(settings.Slots[slotIdx].SensorUID, rid)
	if err != nil {
		log.Printf("composite readingSelect getReading: %v", err)
		return
	}

	p.mu.Lock()
	if st, ok2 := p.compositeSettings[event.Context]; ok2 {
		slot := &st.Slots[slotIdx]
		slot.ReadingLabel = r.Label()
		slot.Min, slot.Max = getDefaultMinMaxForReading(r)
		slot.IsValid = true
	}
	p.mu.Unlock()

	// Rebuild the graph so it picks up the new min/max.
	p.rebuildCompositeGraph(event.Context, slotIdx)

	if err := p.sd.SetSettings(event.Context, settings); err != nil {
		log.Printf("composite readingSelect SetSettings: %v", err)
	}
}

// handleCompositeSlotField updates a single named field on a slot.
func (p *Plugin) handleCompositeSlotField(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection, slotIdx int, field string) {
	p.mu.Lock()
	settings, ok := p.compositeSettings[event.Context]
	if !ok {
		p.mu.Unlock()
		return
	}
	slot := &settings.Slots[slotIdx]
	rebuildGraph := false
	switch field {
	case "mode":
		if validCompositeMode(sdpi.Value) {
			slot.Mode = sdpi.Value
		} else {
			slot.Mode = ""
		}
	case "title":
		slot.Title = sdpi.Value
	case "highlightColor":
		slot.HighlightColor = sdpi.Value
		rebuildGraph = true
	case "foregroundColor":
		slot.ForegroundColor = sdpi.Value
		rebuildGraph = true
	case "backgroundColor":
		slot.BackgroundColor = sdpi.Value
		rebuildGraph = true
	case "valueTextColor":
		slot.ValueTextColor = sdpi.Value
	case "titleColor":
		slot.TitleColor = sdpi.Value
	case "fillAlpha":
		if v, err := strconv.Atoi(sdpi.Value); err == nil {
			if v < 0 {
				v = 0
			}
			if v > 100 {
				v = 100
			}
			slot.FillAlpha = v
			rebuildGraph = true
		}
	case "min":
		if v, err := strconv.Atoi(sdpi.Value); err == nil {
			slot.Min = v
			if state, ok2 := p.compositeStates[event.Context]; ok2 {
				if state.graphs[slotIdx] != nil {
					state.graphs[slotIdx].SetMin(v)
				}
			}
		}
	case "max":
		if v, err := strconv.Atoi(sdpi.Value); err == nil {
			slot.Max = v
			if state, ok2 := p.compositeStates[event.Context]; ok2 {
				if state.graphs[slotIdx] != nil {
					state.graphs[slotIdx].SetMax(v)
				}
			}
		}
	case "titleFontSize":
		if v, err := strconv.ParseFloat(sdpi.Value, 64); err == nil {
			slot.TitleFontSize = v
		}
	case "valueFontSize":
		if v, err := strconv.ParseFloat(sdpi.Value, 64); err == nil {
			slot.ValueFontSize = v
		}
	case "format":
		slot.Format = sdpi.Value
	case "divisor":
		slot.Divisor = sdpi.Value
	case "graphUnit":
		slot.GraphUnit = sdpi.Value
	case "graphHeightPct":
		if v, err := strconv.Atoi(sdpi.Value); err == nil && v >= 10 && v <= 100 {
			slot.GraphHeightPct = v
			if state, ok2 := p.compositeStates[event.Context]; ok2 && state.graphs[slotIdx] != nil {
				state.graphs[slotIdx].SetHeightPct(v)
			}
		}
	case "graphLineThickness":
		if v, err := strconv.Atoi(sdpi.Value); err == nil && v >= 1 && v <= 4 {
			slot.GraphLineThickness = v
			if state, ok2 := p.compositeStates[event.Context]; ok2 && state.graphs[slotIdx] != nil {
				state.graphs[slotIdx].SetLineThickness(v)
			}
		}
	case "textStroke":
		slot.TextStroke = sdpi.Checked
		if state, ok2 := p.compositeStates[event.Context]; ok2 && state.graphs[slotIdx] != nil {
			state.graphs[slotIdx].SetTextStroke(sdpi.Checked)
		}
	case "textStrokeColor":
		slot.TextStrokeColor = sdpi.Value
		if state, ok2 := p.compositeStates[event.Context]; ok2 && state.graphs[slotIdx] != nil {
			if sdpi.Value != "" {
				state.graphs[slotIdx].SetTextStrokeColor(hexToRGBA(sdpi.Value))
			} else {
				state.graphs[slotIdx].SetTextStrokeColor(nil)
			}
		}
	}
	p.mu.Unlock()

	if rebuildGraph {
		p.rebuildCompositeGraph(event.Context, slotIdx)
	}

	if err := p.sd.SetSettings(event.Context, settings); err != nil {
		log.Printf("composite slot field SetSettings: %v", err)
	}
}

// handleCompositeGlobalField updates a tile-wide field (mode, slotCount).
func (p *Plugin) handleCompositeGlobalField(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection) {
	p.mu.Lock()
	settings, ok := p.compositeSettings[event.Context]
	if !ok {
		p.mu.Unlock()
		return
	}
	switch sdpi.Key {
	case "composite_mode":
		settings.Mode = sdpi.Value
	case "composite_slotCount":
		if v, err := strconv.Atoi(sdpi.Value); err == nil && v >= 2 && v <= 4 {
			settings.SlotCount = v
		}
	case "updateIntervalOverrideMs":
		if v, err := strconv.Atoi(sdpi.Value); err == nil {
			settings.UpdateIntervalOverrideMs = v
		}
	case "smoothingAlpha":
		if v, err := strconv.ParseFloat(sdpi.Value, 64); err == nil {
			settings.SmoothingAlpha = v
			if state, ok2 := p.compositeStates[event.Context]; ok2 {
				state.smoothedInit = [4]bool{}
			}
		}
	}
	p.mu.Unlock()

	if err := p.sd.SetSettings(event.Context, settings); err != nil {
		log.Printf("composite global field SetSettings: %v", err)
	}
}

// --- composite threshold handlers ---

func (p *Plugin) handleCompositeAddThreshold(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection, slotIdx int) {
	p.mu.Lock()
	settings, ok := p.compositeSettings[event.Context]
	if !ok {
		p.mu.Unlock()
		return
	}
	slot := &settings.Slots[slotIdx]
	name := sdpi.Value
	if name == "" {
		name = "New"
	}
	newT := Threshold{
		ID:              fmt.Sprintf("threshold_%d", time.Now().UnixNano()),
		Name:            name,
		Enabled:         true,
		Operator:        ">=",
		Value:           0,
		Hysteresis:      defaultThresholdHysteresis,
		DwellMs:         defaultThresholdDwellMs,
		CooldownMs:      defaultThresholdCooldownMs,
		BackgroundColor: defaultColor(slot.BackgroundColor, "#000000"),
		ForegroundColor: defaultColor(slot.ForegroundColor, "#005128"),
		HighlightColor:  defaultColor(slot.HighlightColor, "#009e00"),
		ValueTextColor:  defaultColor(slot.ValueTextColor, "#ffffff"),
		TextColor:       defaultColor(slot.ValueTextColor, "#ffffff"),
	}
	slot.Thresholds = append(slot.Thresholds, newT)
	p.mu.Unlock()

	p.markThresholdDirty(event.Context)
	if err := p.sd.SetSettings(event.Context, settings); err != nil {
		log.Printf("composite addThreshold SetSettings: %v", err)
	}
	p.sendCompositeSlotThresholdsToPI(event, slotIdx, settings.Slots[slotIdx].Thresholds)
}

func (p *Plugin) handleCompositeRemoveThreshold(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection, slotIdx int) {
	p.mu.Lock()
	settings, ok := p.compositeSettings[event.Context]
	if !ok {
		p.mu.Unlock()
		return
	}
	slot := &settings.Slots[slotIdx]
	id := sdpi.ThresholdID
	for i, t := range slot.Thresholds {
		if t.ID == id {
			slot.Thresholds = append(slot.Thresholds[:i], slot.Thresholds[i+1:]...)
			break
		}
	}
	if slot.CurrentThresholdID == id {
		slot.CurrentThresholdID = ""
		if state, ok2 := p.compositeStates[event.Context]; ok2 && state.graphs[slotIdx] != nil {
			applyNormalCompositeColors(state.graphs[slotIdx], slot)
		}
	}
	p.mu.Unlock()

	p.resetThresholdRuntimeState(event.Context+"|"+strconv.Itoa(slotIdx), id)
	if err := p.sd.SetSettings(event.Context, settings); err != nil {
		log.Printf("composite removeThreshold SetSettings: %v", err)
	}
	p.sendCompositeSlotThresholdsToPI(event, slotIdx, settings.Slots[slotIdx].Thresholds)
}

func (p *Plugin) handleCompositeReorderThreshold(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection, slotIdx int) {
	p.mu.Lock()
	settings, ok := p.compositeSettings[event.Context]
	if !ok {
		p.mu.Unlock()
		return
	}
	slot := &settings.Slots[slotIdx]
	pos := -1
	for i, t := range slot.Thresholds {
		if t.ID == sdpi.ThresholdID {
			pos = i
			break
		}
	}
	if pos >= 0 {
		switch sdpi.Value {
		case "up":
			if pos > 0 {
				slot.Thresholds[pos-1], slot.Thresholds[pos] = slot.Thresholds[pos], slot.Thresholds[pos-1]
			}
		case "down":
			if pos < len(slot.Thresholds)-1 {
				slot.Thresholds[pos], slot.Thresholds[pos+1] = slot.Thresholds[pos+1], slot.Thresholds[pos]
			}
		}
	}
	p.mu.Unlock()

	p.markThresholdDirty(event.Context)
	if err := p.sd.SetSettings(event.Context, settings); err != nil {
		log.Printf("composite reorderThreshold SetSettings: %v", err)
	}
	p.sendCompositeSlotThresholdsToPI(event, slotIdx, settings.Slots[slotIdx].Thresholds)
}

func (p *Plugin) handleCompositeThresholdUpdate(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection, slotIdx int) {
	p.mu.Lock()
	settings, ok := p.compositeSettings[event.Context]
	if !ok {
		p.mu.Unlock()
		return
	}
	slot := &settings.Slots[slotIdx]
	var t *Threshold
	for i := range slot.Thresholds {
		if slot.Thresholds[i].ID == sdpi.ThresholdID {
			t = &slot.Thresholds[i]
			break
		}
	}
	if t == nil {
		p.mu.Unlock()
		return
	}
	_, field := parseCompositeSlotKey(sdpi.Key)
	switch field {
	case "thresholdEnabled":
		t.Enabled = sdpi.Checked
	case "thresholdName":
		t.Name = sdpi.Value
	case "thresholdOperator":
		t.Operator = sdpi.Value
	case "thresholdValue":
		if v, err := strconv.ParseFloat(sdpi.Value, 64); err == nil {
			t.Value = v
		}
	case "thresholdHysteresis":
		if v, err := strconv.ParseFloat(sdpi.Value, 64); err == nil {
			t.Hysteresis = v
		}
	case "thresholdDwellMs":
		if v, err := strconv.Atoi(sdpi.Value); err == nil {
			t.DwellMs = v
		}
	case "thresholdCooldownMs":
		if v, err := strconv.Atoi(sdpi.Value); err == nil {
			t.CooldownMs = v
		}
	case "thresholdSticky":
		t.Sticky = sdpi.Checked
	case "thresholdText":
		t.Text = sdpi.Value
	case "thresholdTextColor":
		t.TextColor = sdpi.Value
	case "thresholdBackgroundColor":
		t.BackgroundColor = sdpi.Value
	case "thresholdForegroundColor":
		t.ForegroundColor = sdpi.Value
	case "thresholdHighlightColor":
		t.HighlightColor = sdpi.Value
	case "thresholdValueTextColor":
		t.ValueTextColor = sdpi.Value
	}
	p.mu.Unlock()

	p.markThresholdDirty(event.Context)
	p.resetThresholdRuntimeState(event.Context+"|"+strconv.Itoa(slotIdx), sdpi.ThresholdID)
	if err := p.sd.SetSettings(event.Context, settings); err != nil {
		log.Printf("composite thresholdUpdate SetSettings: %v", err)
	}
}

func (p *Plugin) sendCompositeSlotThresholdsToPI(event *streamdeck.EvSendToPlugin, slotIdx int, thresholds []Threshold) {
	payload := map[string]interface{}{
		"slotThresholds": map[string]interface{}{
			"slotIndex":  slotIdx,
			"thresholds": thresholds,
		},
	}
	if err := p.sd.SendToPropertyInspector(event.Action, event.Context, payload); err != nil {
		log.Printf("sendCompositeSlotThresholdsToPI: %v", err)
	}
}

// parseCompositeSlotKey parses "slot{N}_{field}" into index and field name.
// Returns -1 if the key does not match the pattern.
func parseCompositeSlotKey(key string) (int, string) {
	if !strings.HasPrefix(key, "slot") {
		return -1, ""
	}
	rest := key[4:]
	under := strings.IndexByte(rest, '_')
	if under < 1 {
		return -1, ""
	}
	idx, err := strconv.Atoi(rest[:under])
	if err != nil || idx < 0 || idx > 7 {
		return -1, ""
	}
	return idx, rest[under+1:]
}
