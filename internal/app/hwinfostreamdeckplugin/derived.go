package hwinfostreamdeckplugin

import (
	"encoding/json"
	"log"
	"strconv"
	"time"

	"github.com/moeilijk/hwinfo-streamdeck/pkg/graph"
	hwsensorsservice "github.com/moeilijk/hwinfo-streamdeck/pkg/service"
	"github.com/moeilijk/hwinfo-streamdeck/pkg/streamdeck"
)

const derivedAction = "com.moeilijk.hwinfo.derived"

// derivedState holds runtime state for one derived metric tile context.
type derivedState struct {
	graph            *graph.Graph
	lastPollTime     uint64
	divisorCache     [8]divisorCacheEntry
	tileDivisorCache divisorCacheEntry
	smoothedValue    float64
	smoothedInit     bool
}

// decodeDerivedSettings decodes raw JSON and fills in defaults for missing fields.
func decodeDerivedSettings(raw *json.RawMessage) (derivedActionSettings, error) {
	var s derivedActionSettings
	if raw != nil {
		if err := json.Unmarshal(*raw, &s); err != nil {
			return s, err
		}
	}
	if s.SlotCount < 2 || s.SlotCount > 8 {
		s.SlotCount = 2
	}
	if s.Formula == "" {
		s.Formula = "sum"
	}
	if s.ForegroundColor == "" {
		s.ForegroundColor = "#005128"
	}
	if s.BackgroundColor == "" {
		s.BackgroundColor = "#000000"
	}
	if s.HighlightColor == "" {
		s.HighlightColor = "#009E00"
	}
	if s.ValueTextColor == "" {
		s.ValueTextColor = "#ffffff"
	}
	if s.TitleColor == "" {
		s.TitleColor = "#b7b7b7"
	}
	if s.TitleFontSize == 0 {
		s.TitleFontSize = 10.5
	}
	if s.ValueFontSize == 0 {
		s.ValueFontSize = 10.5
	}
	if s.ShowTitleInGraph == nil {
		s.ShowTitleInGraph = boolPtr(true)
	}
	if s.Min == 0 && s.Max == 0 {
		s.Max = 100
	}
	return s, nil
}

// initDerivedGraph creates a graph.Graph using the tile-level color settings.
func initDerivedGraph(s *derivedActionSettings) *graph.Graph {
	fg := hexToRGBA(s.ForegroundColor)
	bg := hexToRGBA(s.BackgroundColor)
	hl := hexToRGBA(s.HighlightColor)
	tc := hexToRGBA(s.TitleColor)
	vc := hexToRGBA(s.ValueTextColor)
	g := graph.NewGraph(tileWidth, tileHeight, s.Min, s.Max, fg, bg, hl)
	tfSize := s.TitleFontSize
	if tfSize == 0 {
		tfSize = 10.5
	}
	vfSize := s.ValueFontSize
	if vfSize == 0 {
		vfSize = 10.5
	}
	g.SetLabel(0, "", 19, tc)
	g.SetLabelFontSize(0, tfSize)
	g.SetLabel(1, "", 44, vc)
	g.SetLabelFontSize(1, vfSize)
	g.SetLabel(2, "", 56, vc)
	g.SetLabelFontSize(2, vfSize)
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
	return g
}

// computeDerived applies the formula to the collected slot values.
// Returns (result, ok). ok=false if the input is invalid for the formula.
func computeDerived(formula string, values []float64) (float64, bool) {
	n := len(values)
	if n < 2 {
		return 0, false
	}
	switch formula {
	case "sum":
		var s float64
		for _, v := range values {
			s += v
		}
		return s, true
	case "average":
		var s float64
		for _, v := range values {
			s += v
		}
		return s / float64(n), true
	case "max":
		m := values[0]
		for _, v := range values[1:] {
			if v > m {
				m = v
			}
		}
		return m, true
	case "min":
		m := values[0]
		for _, v := range values[1:] {
			if v < m {
				m = v
			}
		}
		return m, true
	case "delta":
		return values[n-1] - values[0], true
	case "pct":
		var denominator float64
		for _, v := range values[1:] {
			denominator += v
		}
		if denominator == 0 {
			return 0, true
		}
		return values[0] / denominator * 100, true
	}
	return 0, false
}

// updateDerivedTile fetches all slot readings, computes the derived value,
// and renders it using the same graph/threshold/format pipeline as a normal reading tile.
func (p *Plugin) derivedLabelText(settings *derivedActionSettings) string {
	drawTitle := true
	if settings.ShowTitleInGraph != nil {
		drawTitle = *settings.ShowTitleInGraph
	}
	if !drawTitle {
		return ""
	}
	if settings.Title != "" {
		return settings.Title
	}
	return settings.Formula
}

func (p *Plugin) updateDerivedTile(ctx string) {
	p.mu.RLock()
	settings, ok1 := p.derivedSettings[ctx]
	state, ok2 := p.derivedStates[ctx]
	p.mu.RUnlock()
	if !ok1 || !ok2 {
		return
	}

	pollTime, err := p.getCachedPollTime()
	if err != nil || pollTime == 0 || time.Since(time.Unix(0, int64(pollTime))) > 5*time.Second {
		return
	}
	if pollTime == state.lastPollTime {
		return
	}

	if override := settings.UpdateIntervalOverrideMs; override > 0 {
		p.mu.RLock()
		lastRender := p.lastRenderTime[ctx]
		p.mu.RUnlock()
		if time.Since(lastRender) < time.Duration(override)*time.Millisecond {
			return
		}
	}

	var values []float64
	var displayUnit string
	var readingType hwsensorsservice.ReadingType

	for i := 0; i < settings.SlotCount; i++ {
		slot := &settings.Slots[i]
		if !slot.IsValid || slot.SensorUID == "" {
			continue
		}
		r, _, err := p.getReading(slot.SensorUID, slot.ReadingID)
		if err != nil {
			continue
		}
		v := r.Value()
		if slot.GraphUnit != "" {
			v = p.normalizeForGraph(v, r.Unit(), slot.GraphUnit)
		}
		// Per-slot divisor
		p.mu.RLock()
		st, hasSt := p.derivedStates[ctx]
		p.mu.RUnlock()
		if hasSt && slot.Divisor != "" {
			cache := st.divisorCache[i]
			if cache.raw != slot.Divisor {
				if dv, err := strconv.ParseFloat(slot.Divisor, 64); err == nil {
					cache = divisorCacheEntry{raw: slot.Divisor, value: dv}
					p.mu.Lock()
					if s2, ok := p.derivedStates[ctx]; ok {
						s2.divisorCache[i] = cache
					}
					p.mu.Unlock()
				}
			}
			if cache.value != 0 && cache.value != 1 {
				v = v / cache.value
			}
		}
		values = append(values, v)
		if displayUnit == "" {
			if slot.GraphUnit != "" {
				displayUnit = slot.GraphUnit
			} else {
				displayUnit = r.Unit()
			}
			readingType = hwsensorsservice.ReadingType(r.TypeI())
		}
	}

	if len(values) < 2 {
		return
	}

	aggregated, ok := computeDerived(settings.Formula, values)
	if !ok {
		return
	}

	// Tile-level divisor (post-aggregation)
	if settings.Divisor != "" {
		p.mu.RLock()
		st2, hasSt2 := p.derivedStates[ctx]
		p.mu.RUnlock()
		if hasSt2 {
			cache := st2.tileDivisorCache
			if cache.raw != settings.Divisor {
				if dv, err := strconv.ParseFloat(settings.Divisor, 64); err == nil {
					cache = divisorCacheEntry{raw: settings.Divisor, value: dv}
					p.mu.Lock()
					if s3, ok := p.derivedStates[ctx]; ok {
						s3.tileDivisorCache = cache
					}
					p.mu.Unlock()
				}
			}
			if cache.value != 0 && cache.value != 1 {
				aggregated = aggregated / cache.value
			}
		}
	}

	if settings.GraphUnit != "" {
		displayUnit = settings.GraphUnit
	}
	if settings.Formula == "pct" {
		displayUnit = "%"
	}

	// EMA smoothing — threshold eval uses raw aggregated; smoothing applies to display and graph
	smoothedAggregated := aggregated
	if alpha := settings.SmoothingAlpha; alpha > 0 && alpha < 1.0 {
		p.mu.Lock()
		if !state.smoothedInit {
			state.smoothedValue = aggregated
			state.smoothedInit = true
		}
		smoothedAggregated = alpha*aggregated + (1-alpha)*state.smoothedValue
		state.smoothedValue = smoothedAggregated
		p.mu.Unlock()
	}

	valueTextNoUnit, displayText := p.formatDisplayValue(smoothedAggregated, displayUnit, settings.Format, readingType)

	now := time.Now()
	p.mu.RLock()
	derivedThresholds := p.resolveThresholdsForEval(settings.Thresholds, settings.SuppressedGlobalIDs, readingType)
	p.mu.RUnlock()
	activeThreshold := p.evaluateThresholds(ctx, aggregated, derivedThresholds, now)

	newThresholdID := ""
	alertText := ""
	if activeThreshold != nil {
		newThresholdID = activeThreshold.ID
		if activeThreshold.Text != "" {
			alertText = p.applyThresholdText(activeThreshold.Text, valueTextNoUnit, displayUnit)
		}
	}

	snoozeState, snoozed, snoozeChanged := p.currentThresholdSnoozeState(ctx, now)
	if activeThreshold == nil {
		p.clearThresholdSnooze(ctx)
		snoozed = false
		snoozeState = thresholdSnoozeState{}
	}

	p.mu.RLock()
	g := state.graph
	p.mu.RUnlock()
	if g == nil {
		return
	}

	forceUpdate := snoozeChanged || p.consumeThresholdDirty(ctx)

	if forceUpdate || newThresholdID != settings.CurrentThresholdID {
		// Build a temporary actionSettings for applyNormalColors/applyThresholdColors
		tmp := &actionSettings{
			ForegroundColor: settings.ForegroundColor,
			BackgroundColor: settings.BackgroundColor,
			HighlightColor:  settings.HighlightColor,
			ValueTextColor:  settings.ValueTextColor,
			TitleColor:      settings.TitleColor,
			Min:             settings.Min,
			Max:             settings.Max,
		}
		if activeThreshold != nil && !snoozed {
			p.applyThresholdColors(g, activeThreshold)
		} else {
			p.applyNormalColors(g, tmp)
		}
		p.mu.Lock()
		if ds, ok := p.derivedSettings[ctx]; ok {
			ds.CurrentThresholdID = newThresholdID
		}
		p.mu.Unlock()
	}

	renderDisplayText, renderAlertText, renderGraphValue, freezeGraph := p.resolveThresholdDisplay(
		ctx, activeThreshold, smoothedAggregated, smoothedAggregated, displayText, alertText,
	)
	if snoozed {
		renderDisplayText = displayText
		renderAlertText = thresholdSnoozeText(snoozeState, now)
		renderGraphValue = smoothedAggregated
		freezeGraph = false
	}
	if !freezeGraph {
		g.Update(renderGraphValue)
	}

	if err := g.SetLabelText(0, p.derivedLabelText(settings)); err != nil {
		log.Printf("derived SetLabelText(0): %v", err)
	}
	if err := g.SetLabelText(1, renderDisplayText); err != nil {
		log.Printf("derived SetLabelText(1): %v", err)
	}
	if renderAlertText != "" {
		if err := g.SetLabelText(2, renderAlertText); err != nil {
			log.Printf("derived SetLabelText(2): %v", err)
		}
	} else {
		if err := g.SetLabelText(2, ""); err != nil {
			log.Printf("derived SetLabelText(2): %v", err)
		}
	}

	b, err := g.EncodePNG()
	if err != nil {
		log.Printf("derived EncodePNG: %v", err)
		return
	}
	if err := p.sd.SetImage(ctx, b); err != nil {
		log.Printf("derived SetImage: %v", err)
		return
	}

	p.mu.Lock()
	if st, ok := p.derivedStates[ctx]; ok {
		st.lastPollTime = pollTime
	}
	if settings.UpdateIntervalOverrideMs > 0 {
		p.lastRenderTime[ctx] = time.Now()
	}
	p.mu.Unlock()
}

func (p *Plugin) updateDerivedTick() {
	p.mu.RLock()
	contexts := make([]string, 0, len(p.derivedSettings))
	for ctx := range p.derivedSettings {
		contexts = append(contexts, ctx)
	}
	p.mu.RUnlock()
	for _, ctx := range contexts {
		p.updateDerivedTile(ctx)
	}
}

func (p *Plugin) handleDerivedTitleParametersDidChange(event *streamdeck.EvTitleParametersDidChange) {
	p.mu.Lock()
	settings, ok := p.derivedSettings[event.Context]
	if !ok {
		ds, _ := decodeDerivedSettings(event.Payload.Settings)
		settings = &ds
		p.derivedSettings[event.Context] = settings
	}
	state, ok := p.derivedStates[event.Context]
	if !ok {
		state = &derivedState{graph: initDerivedGraph(settings)}
		p.derivedStates[event.Context] = state
	}

	drawTitle := !event.Payload.TitleParameters.ShowTitle
	settings.Title = event.Payload.Title
	settings.TitleColor = event.Payload.TitleParameters.TitleColor
	settings.ShowTitleInGraph = boolPtr(drawTitle)
	if state != nil {
		state.lastPollTime = 0
	}
	if state != nil && state.graph != nil {
		titleColor := settings.TitleColor
		if titleColor == "" {
			titleColor = "#b7b7b7"
		}
		state.graph.SetLabelColor(0, hexToRGBA(titleColor))
		if err := state.graph.SetLabelText(0, p.derivedLabelText(settings)); err != nil {
			log.Printf("derived SetLabelText(0): %v", err)
		}
	}
	p.mu.Unlock()

	if err := p.sd.SetSettings(event.Context, settings); err != nil {
		log.Printf("OnTitleParametersDidChange derived SetSettings: %v\n", err)
	}
}

// --- PI handlers ---

func (p *Plugin) handleDerivedPropertyInspectorConnected(event *streamdeck.EvSendToPlugin) {
	p.mu.RLock()
	settings, ok := p.derivedSettings[event.Context]
	p.mu.RUnlock()

	sensors, err := p.sensorsWithTimeout(2 * time.Second)
	if err != nil {
		log.Printf("derived PI connected sensors: %v", err)
		go p.restartBridge(p.bridge)
		_ = p.sd.SendToPropertyInspector(event.Action, event.Context, evStatus{Error: true, Message: "HWiNFO Unavailable"})
		return
	}

	evsensors := make([]*evSendSensorsPayloadSensor, 0, len(sensors))
	for _, s := range sensors {
		evsensors = append(evsensors, sensorPayload(s.ID(), s.Name()))
	}

	var settingsCopy derivedActionSettings
	if ok {
		settingsCopy = *settings
	} else {
		settingsCopy, _ = decodeDerivedSettings(nil)
	}

	p.mu.RLock()
	globals := make([]Threshold, len(p.globalSettings.GlobalThresholds))
	copy(globals, p.globalSettings.GlobalThresholds)
	p.mu.RUnlock()

	payload := map[string]interface{}{
		"sensors":          evsensors,
		"derivedSettings":  settingsCopy,
		"favorites":        p.favoriteReadingsSnapshot(),
		"globalThresholds": globals,
	}
	if err := p.sd.SendToPropertyInspector(event.Action, event.Context, payload); err != nil {
		log.Printf("derived PI SendToPropertyInspector: %v", err)
	}

	for i := 0; i < settingsCopy.SlotCount; i++ {
		slot := settingsCopy.Slots[i]
		if slot.SensorUID == "" {
			continue
		}
		p.sendDerivedReadings(event.Action, event.Context, i, slot.SensorUID, &settingsCopy)
	}
}

func (p *Plugin) sendDerivedReadings(action, ctx string, slotIdx int, sensorUID string, settings *derivedActionSettings) {
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
		"readings":        evr,
		"slotIndex":       slotIdx,
		"derivedSettings": settings,
	}
	_ = p.sd.SendToPropertyInspector(action, ctx, payload)
}

func (p *Plugin) handleDerivedSlotSensorSelect(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection, slotIdx int) {
	p.mu.Lock()
	settings, ok := p.derivedSettings[event.Context]
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
		log.Printf("derived sensorSelect SetSettings: %v", err)
	}
	p.sendDerivedReadings(event.Action, event.Context, slotIdx, sdpi.Value, settings)
}

func (p *Plugin) handleDerivedSlotReadingSelect(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection, slotIdx int) {
	rid64, err := strconv.ParseInt(sdpi.Value, 10, 32)
	if err != nil {
		log.Printf("derived readingSelect parse: %v", err)
		return
	}
	rid := int32(rid64)

	p.mu.Lock()
	settings, ok := p.derivedSettings[event.Context]
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
		log.Printf("derived readingSelect getReading: %v", err)
		return
	}

	p.mu.Lock()
	if st, ok2 := p.derivedSettings[event.Context]; ok2 {
		slot := &st.Slots[slotIdx]
		slot.ReadingLabel = r.Label()
		slot.IsValid = true
	}
	p.mu.Unlock()

	if err := p.sd.SetSettings(event.Context, settings); err != nil {
		log.Printf("derived readingSelect SetSettings: %v", err)
	}
}

func (p *Plugin) handleDerivedSlotField(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection, slotIdx int, field string) {
	p.mu.Lock()
	settings, ok := p.derivedSettings[event.Context]
	if !ok {
		p.mu.Unlock()
		return
	}
	slot := &settings.Slots[slotIdx]
	switch field {
	case "divisor":
		slot.Divisor = sdpi.Value
	case "graphUnit":
		slot.GraphUnit = sdpi.Value
	}
	p.mu.Unlock()

	if err := p.sd.SetSettings(event.Context, settings); err != nil {
		log.Printf("derived slot field SetSettings: %v", err)
	}
}

func (p *Plugin) handleDerivedGlobalField(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection) {
	p.mu.Lock()
	settings, ok := p.derivedSettings[event.Context]
	if !ok {
		p.mu.Unlock()
		return
	}
	state := p.derivedStates[event.Context]
	switch sdpi.Key {
	case "derived_formula":
		settings.Formula = sdpi.Value
	case "derived_slotCount":
		if v, err := strconv.Atoi(sdpi.Value); err == nil && v >= 2 && v <= 8 {
			settings.SlotCount = v
		}
	case "derived_format":
		settings.Format = sdpi.Value
	case "derived_divisor":
		settings.Divisor = sdpi.Value
	case "derived_graphUnit":
		settings.GraphUnit = sdpi.Value
	case "derived_min":
		if v, err := strconv.Atoi(sdpi.Value); err == nil {
			settings.Min = v
			if state != nil && state.graph != nil {
				state.graph.SetMin(v)
			}
		}
	case "derived_max":
		if v, err := strconv.Atoi(sdpi.Value); err == nil {
			settings.Max = v
			if state != nil && state.graph != nil {
				state.graph.SetMax(v)
			}
		}
	case "derived_foregroundColor":
		settings.ForegroundColor = sdpi.Value
	case "derived_backgroundColor":
		settings.BackgroundColor = sdpi.Value
	case "derived_highlightColor":
		settings.HighlightColor = sdpi.Value
	case "derived_valueTextColor":
		settings.ValueTextColor = sdpi.Value
		if state != nil && state.graph != nil {
			state.graph.SetLabelColor(1, hexToRGBA(sdpi.Value))
			state.graph.SetLabelColor(2, hexToRGBA(sdpi.Value))
		}
	case "derived_titleColor":
		settings.TitleColor = sdpi.Value
		if state != nil && state.graph != nil {
			state.graph.SetLabelColor(0, hexToRGBA(sdpi.Value))
		}
	case "derived_title":
		settings.Title = sdpi.Value
		if state != nil && state.graph != nil {
			state.graph.SetLabelText(0, p.derivedLabelText(settings))
		}
		if state != nil {
			state.lastPollTime = 0
		}
	case "titleFontSize":
		if v, err := strconv.ParseFloat(sdpi.Value, 64); err == nil {
			settings.TitleFontSize = v
			if state != nil && state.graph != nil {
				state.graph.SetLabelFontSize(0, v)
			}
			if state != nil {
				state.lastPollTime = 0
			}
		}
	case "valueFontSize":
		if v, err := strconv.ParseFloat(sdpi.Value, 64); err == nil {
			settings.ValueFontSize = v
			if state != nil && state.graph != nil {
				state.graph.SetLabelFontSize(1, v)
				state.graph.SetLabelFontSize(2, v)
			}
			if state != nil {
				state.lastPollTime = 0
			}
		}
	case "derived_graphHeightPct":
		if v, err := strconv.Atoi(sdpi.Value); err == nil && v >= 10 && v <= 100 {
			settings.GraphHeightPct = v
			if state != nil && state.graph != nil {
				state.graph.SetHeightPct(v)
			}
		}
	case "derived_graphLineThickness":
		if v, err := strconv.Atoi(sdpi.Value); err == nil && v >= 1 && v <= 4 {
			settings.GraphLineThickness = v
			if state != nil && state.graph != nil {
				state.graph.SetLineThickness(v)
			}
		}
	case "derived_textStroke":
		settings.TextStroke = sdpi.Checked
		if state != nil && state.graph != nil {
			state.graph.SetTextStroke(sdpi.Checked)
		}
	case "derived_textStrokeColor":
		settings.TextStrokeColor = sdpi.Value
		if state != nil && state.graph != nil {
			if sdpi.Value != "" {
				state.graph.SetTextStrokeColor(hexToRGBA(sdpi.Value))
			} else {
				state.graph.SetTextStrokeColor(nil)
			}
		}
	case "derived_updateIntervalOverrideMs":
		if v, err := strconv.Atoi(sdpi.Value); err == nil {
			settings.UpdateIntervalOverrideMs = v
		}
	case "derived_smoothingAlpha":
		if v, err := strconv.ParseFloat(sdpi.Value, 64); err == nil {
			settings.SmoothingAlpha = v
			if state != nil {
				state.smoothedInit = false
			}
		}
	}
	needsRebuild := sdpi.Key == "derived_foregroundColor" || sdpi.Key == "derived_backgroundColor" || sdpi.Key == "derived_highlightColor"
	p.mu.Unlock()

	if needsRebuild {
		p.rebuildDerivedGraph(event.Context)
	}

	if err := p.sd.SetSettings(event.Context, settings); err != nil {
		log.Printf("derived global field SetSettings: %v", err)
	}
}

// handleDerivedSlotApplyFavorite applies a saved favorite to one slot.
func (p *Plugin) handleDerivedSlotApplyFavorite(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection, slotIdx int) {
	fav, _, ok := findFavoriteByID(p.favoriteReadingsSnapshot(), sdpi.Value)
	if !ok {
		return
	}

	p.mu.Lock()
	settings, ok := p.derivedSettings[event.Context]
	if !ok {
		p.mu.Unlock()
		return
	}
	settings.Slots[slotIdx].SensorUID = fav.SensorUID
	settings.Slots[slotIdx].ReadingID = fav.ReadingID
	settings.Slots[slotIdx].ReadingLabel = fav.ReadingLabel
	settings.Slots[slotIdx].IsValid = true
	p.mu.Unlock()

	if err := p.sd.SetSettings(event.Context, settings); err != nil {
		log.Printf("derived applyFavorite SetSettings: %v", err)
	}
	p.sendDerivedReadings(event.Action, event.Context, slotIdx, fav.SensorUID, settings)
}

// derivedPresetPayload is the payload sent by the PI when loading a preset.
type derivedPresetPayload struct {
	Formula   string                 `json:"formula"`
	SlotCount int                    `json:"slotCount"`
	Slots     [8]derivedSlotSettings `json:"slots"`
}

// handleDerivedAllSlotsSensor sets every active slot to the given sensor UID
// and sends the readings list for each slot back to the PI.
func (p *Plugin) handleDerivedAllSlotsSensor(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection) {
	p.mu.Lock()
	settings, ok := p.derivedSettings[event.Context]
	if !ok {
		p.mu.Unlock()
		return
	}
	for i := 0; i < settings.SlotCount; i++ {
		settings.Slots[i].SensorUID = sdpi.Value
		settings.Slots[i].ReadingID = 0
		settings.Slots[i].ReadingLabel = ""
		settings.Slots[i].IsValid = false
	}
	p.mu.Unlock()

	if err := p.sd.SetSettings(event.Context, settings); err != nil {
		log.Printf("derived allSlotsSensor SetSettings: %v", err)
	}
	p.handleDerivedPropertyInspectorConnected(event)
}

// handleDerivedLoadPreset applies a saved preset's formula/slotCount/slots to
// the action settings and refreshes the PI (sensors + readings).
func (p *Plugin) handleDerivedLoadPreset(event *streamdeck.EvSendToPlugin, preset derivedPresetPayload) {
	p.mu.Lock()
	settings, ok := p.derivedSettings[event.Context]
	if !ok {
		p.mu.Unlock()
		return
	}
	if preset.Formula != "" {
		settings.Formula = preset.Formula
	}
	if preset.SlotCount >= 2 && preset.SlotCount <= 8 {
		settings.SlotCount = preset.SlotCount
	}
	settings.Slots = preset.Slots
	p.mu.Unlock()

	if err := p.sd.SetSettings(event.Context, settings); err != nil {
		log.Printf("derived loadPreset SetSettings: %v", err)
	}
	p.handleDerivedPropertyInspectorConnected(event)
}

func (p *Plugin) rebuildDerivedGraph(ctx string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	settings, ok1 := p.derivedSettings[ctx]
	state, ok2 := p.derivedStates[ctx]
	if !ok1 || !ok2 {
		return
	}
	state.graph = initDerivedGraph(settings)
}
