package hwinfostreamdeckplugin

import (
	"fmt"
	"image/color"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moeilijk/hwinfo-streamdeck/pkg/graph"
	hwsensorsservice "github.com/moeilijk/hwinfo-streamdeck/pkg/service"
	"github.com/moeilijk/hwinfo-streamdeck/pkg/streamdeck"
)

func (p *Plugin) handleSensorSelect(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection) error {
	rt := p.bridge
	rt.mu.RLock()
	hw := rt.hw
	rt.mu.RUnlock()
	if hw == nil {
		return fmt.Errorf("HWiNFO bridge not ready")
	}
	sensorid := sdpi.Value
	readings, err := hw.ReadingsForSensorID(sensorid)
	if err != nil {
		return fmt.Errorf("handleSensorSelect ReadingsBySensor failed: %v", err)
	}
	evreadings := []*evSendReadingsPayloadReading{}
	for _, r := range readings {
		evreadings = append(evreadings, &evSendReadingsPayloadReading{
			ID:     r.ID(),
			Label:  r.Label(),
			Prefix: r.Unit(),
			Unit:   r.Unit(),
			Type:   r.Type(),
		})
	}
	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		return fmt.Errorf("handleReadingSelect getSettings: %v", err)
	}
	// only update settings if SensorUID is changing
	// this covers case where PI sends event when tile
	// selected in SD UI
	if settings.SensorUID != sensorid {
		settings.SensorUID = sensorid
		settings.ReadingID = 0
		settings.ReadingLabel = ""
		settings.IsValid = false
	}
	payload := evSendReadingsPayload{Readings: evreadings, Settings: &settings}
	err = p.sd.SendToPropertyInspector(event.Action, event.Context, payload)
	if err != nil {
		return fmt.Errorf("sensorsSelect SendToPropertyInspector: %v", err)
	}
	err = p.sd.SetSettings(event.Context, &settings)
	if err != nil {
		return fmt.Errorf("handleSensorSelect SetSettings: %v", err)
	}
	p.am.SetAction(event.Action, event.Context, &settings)
	return nil
}

func getDefaultMinMaxForReading(r hwsensorsservice.Reading) (int, int) {
	switch r.Unit() {
	case "%":
		return 0, 100
	case "Yes/No":
		return 0, 1
	}
	min := r.ValueMin()
	max := r.ValueMax()
	min -= min * .2
	if min <= 0 {
		min = 0.
	}
	max += max * .2
	return int(min), int(max)
}

func (p *Plugin) handleReadingSelect(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection) error {
	rid64, err := strconv.ParseInt(sdpi.Value, 10, 32)
	if err != nil {
		return fmt.Errorf("handleReadingSelect Atoi failed: %s, %v", sdpi.Value, err)
	}
	rid := int32(rid64)
	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		return fmt.Errorf("handleReadingSelect getSettings: %v", err)
	}

	// no action if reading didn't change
	if settings.ReadingID == rid {
		return nil
	}

	settings.ReadingID = rid

	if err := p.applyReadingSettings(event.Context, &settings); err != nil {
		return fmt.Errorf("handleReadingSelect applyReadingSettings: %v", err)
	}

	err = p.sd.SetSettings(event.Context, &settings)
	if err != nil {
		return fmt.Errorf("handleReadingSelect SetSettings: %v", err)
	}
	p.am.SetAction(event.Action, event.Context, &settings)
	return nil
}

func (p *Plugin) applyReadingSettings(context string, settings *actionSettings) error {
	if settings == nil {
		return fmt.Errorf("settings are required")
	}

	r, _, err := p.getReading(settings.SensorUID, settings.ReadingID)
	if err != nil {
		return fmt.Errorf("getReading: %v", err)
	}
	settings.ReadingLabel = r.Label()

	p.mu.RLock()
	g, ok := p.graphs[context]
	p.mu.RUnlock()
	if !ok {
		return fmt.Errorf("no graph for context: %s", context)
	}

	defaultMin, defaultMax := getDefaultMinMaxForReading(r)
	settings.Min = defaultMin
	g.SetMin(settings.Min)
	settings.Max = defaultMax
	g.SetMax(settings.Max)
	settings.IsValid = true

	return nil
}

func (p *Plugin) handleApplyFavorite(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection) error {
	if sdpi.Value == "" {
		return fmt.Errorf("favorite id is required")
	}

	p.mu.RLock()
	favorite, _, found := findFavoriteByID(p.globalSettings.FavoriteReadings, sdpi.Value)
	p.mu.RUnlock()
	if !found {
		return fmt.Errorf("favorite not found: %s", sdpi.Value)
	}

	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		return fmt.Errorf("handleApplyFavorite getSettings: %v", err)
	}

	settings.SensorUID = favorite.SensorUID
	settings.ReadingID = favorite.ReadingID
	settings.ReadingLabel = favorite.ReadingLabel

	if err := p.applyReadingSettings(event.Context, &settings); err != nil {
		return fmt.Errorf("handleApplyFavorite applyReadingSettings: %v", err)
	}

	if err := p.sd.SetSettings(event.Context, &settings); err != nil {
		return fmt.Errorf("handleApplyFavorite SetSettings: %v", err)
	}
	p.am.SetAction(event.Action, event.Context, &settings)

	if _, err := p.sendReadingsToPropertyInspector(event.Action, event.Context, settings.SensorUID, &settings); err != nil {
		return fmt.Errorf("handleApplyFavorite sendReadingsToPropertyInspector: %v", err)
	}

	return nil
}

func (p *Plugin) handleSetMin(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection) error {
	min, err := strconv.Atoi(sdpi.Value)
	if err != nil {
		return fmt.Errorf("handleSetMin strconv: %v", err)
	}
	p.mu.RLock()
	g, ok := p.graphs[event.Context]
	p.mu.RUnlock()
	if !ok {
		return fmt.Errorf("handleSetMax no graph for context: %s", event.Context)
	}
	g.SetMin(min)
	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		return fmt.Errorf("handleSetMin getSettings: %v", err)
	}
	settings.Min = min
	err = p.sd.SetSettings(event.Context, &settings)
	if err != nil {
		return fmt.Errorf("handleSetMin SetSettings: %v", err)
	}
	p.am.SetAction(event.Action, event.Context, &settings)
	return nil
}

func (p *Plugin) handleSetMax(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection) error {
	max, err := strconv.Atoi(sdpi.Value)
	if err != nil {
		return fmt.Errorf("handleSetMax strconv: %v", err)
	}
	p.mu.RLock()
	g, ok := p.graphs[event.Context]
	p.mu.RUnlock()
	if !ok {
		return fmt.Errorf("handleSetMax no graph for context: %s", event.Context)
	}
	g.SetMax(max)
	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		return fmt.Errorf("handleSetMax getSettings: %v", err)
	}
	settings.Max = max
	err = p.sd.SetSettings(event.Context, &settings)
	if err != nil {
		return fmt.Errorf("handleSetMax SetSettings: %v", err)
	}
	p.am.SetAction(event.Action, event.Context, &settings)
	return nil
}

func (p *Plugin) handleSetFormat(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection) error {
	format := sdpi.Value
	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		return fmt.Errorf("handleSetFormat getSettings: %v", err)
	}
	settings.Format = format
	err = p.sd.SetSettings(event.Context, &settings)
	if err != nil {
		return fmt.Errorf("handleSetFormat SetSettings: %v", err)
	}
	p.am.SetAction(event.Action, event.Context, &settings)
	return nil
}

func (p *Plugin) handleDivisor(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection) error {
	divisor := sdpi.Value
	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		return fmt.Errorf("handleDivisor getSettings: %v", err)
	}
	settings.Divisor = divisor
	err = p.sd.SetSettings(event.Context, &settings)
	if err != nil {
		return fmt.Errorf("handleDivisor SetSettings: %v", err)
	}
	p.am.SetAction(event.Action, event.Context, &settings)
	return nil
}

func (p *Plugin) handleSetGraphUnit(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection) error {
	graphUnit := sdpi.Value
	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		return fmt.Errorf("handleSetGraphUnit getSettings: %v", err)
	}
	settings.GraphUnit = graphUnit
	err = p.sd.SetSettings(event.Context, &settings)
	if err != nil {
		return fmt.Errorf("handleSetGraphUnit SetSettings: %v", err)
	}
	p.am.SetAction(event.Action, event.Context, &settings)
	return nil
}

func parseSnoozeDurations(sdpi *evSdpiCollection) []int {
	values := make([]int, 0, len(sdpi.Selection))
	for _, raw := range sdpi.Selection {
		value, err := strconv.Atoi(strings.TrimSpace(raw))
		if err == nil {
			values = append(values, value)
		}
	}
	if len(values) == 0 && sdpi.Value != "" {
		for _, raw := range strings.Split(sdpi.Value, ",") {
			value, err := strconv.Atoi(strings.TrimSpace(raw))
			if err == nil {
				values = append(values, value)
			}
		}
	}
	return normalizeThresholdSnoozeDurations(values)
}

func (p *Plugin) handleSnoozeDurations(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection) error {
	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		return fmt.Errorf("handleSnoozeDurations getSettings: %v", err)
	}

	settings.SnoozeDurations = parseSnoozeDurations(sdpi)
	activeSnooze, snoozed := p.currentThresholdSnooze(event.Context, time.Now())
	cleared := false
	if snoozed && !containsThresholdSnoozeDuration(settings.SnoozeDurations, activeSnooze.Duration) {
		cleared = p.clearThresholdSnooze(event.Context)
	}

	if err := p.sd.SetSettings(event.Context, &settings); err != nil {
		return fmt.Errorf("handleSnoozeDurations SetSettings: %v", err)
	}
	p.am.SetAction(event.Action, event.Context, &settings)
	if cleared {
		p.refreshAction(event.Action, event.Context)
	}
	return nil
}

const (
	hexFormat      = "#%02x%02x%02x"
	hexShortFormat = "#%1x%1x%1x"
	hexToRGBFactor = 17
)

// colorCache stores parsed colors to avoid repeated parsing
var (
	colorCacheMu sync.RWMutex
	colorCache   = make(map[string]*color.RGBA)
)

func hexToRGBA(hex string) *color.RGBA {
	colorCacheMu.RLock()
	if c, ok := colorCache[hex]; ok {
		colorCacheMu.RUnlock()
		return c
	}
	colorCacheMu.RUnlock()

	var r, g, b uint8
	if len(hex) == 4 {
		fmt.Sscanf(hex, hexShortFormat, &r, &g, &b)
		r *= hexToRGBFactor
		g *= hexToRGBFactor
		b *= hexToRGBFactor
	} else {
		fmt.Sscanf(hex, hexFormat, &r, &g, &b)
	}

	c := &color.RGBA{R: r, G: g, B: b, A: 255}
	colorCacheMu.Lock()
	colorCache[hex] = c
	colorCacheMu.Unlock()
	return c
}

func (p *Plugin) handleColorChange(event *streamdeck.EvSendToPlugin, key string, sdpi *evSdpiCollection) error {
	hex := sdpi.Value
	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		return fmt.Errorf("handleDivisor getSettings: %v", err)
	}
	p.mu.RLock()
	g, ok := p.graphs[event.Context]
	p.mu.RUnlock()
	if !ok {
		return fmt.Errorf("handleColorChange no graph for context: %s", event.Context)
	}
	clr := hexToRGBA(hex)
	switch key {
	case "foreground":
		settings.ForegroundColor = hex
		g.SetForegroundColor(clr)
	case "background":
		settings.BackgroundColor = hex
		g.SetBackgroundColor(clr)
	case "highlight":
		settings.HighlightColor = hex
		g.SetHighlightColor(clr)
	case "valuetext":
		settings.ValueTextColor = hex
		g.SetLabelColor(1, clr)
	case "warningBackground":
		settings.WarningBackgroundColor = hex
	case "warningForeground":
		settings.WarningForegroundColor = hex
	case "warningHighlight":
		settings.WarningHighlightColor = hex
	case "warningValuetext":
		settings.WarningValueTextColor = hex
	case "criticalBackground":
		settings.CriticalBackgroundColor = hex
	case "criticalForeground":
		settings.CriticalForegroundColor = hex
	case "criticalHighlight":
		settings.CriticalHighlightColor = hex
	case "criticalValuetext":
		settings.CriticalValueTextColor = hex
	}
	err = p.sd.SetSettings(event.Context, &settings)
	if err != nil {
		return fmt.Errorf("handleColorChange SetSettings: %v", err)
	}
	p.am.SetAction(event.Action, event.Context, &settings)
	return nil
}

func (p *Plugin) handleSetFontSize(event *streamdeck.EvSendToPlugin, key string, sdpi *evSdpiCollection) error {
	sv := sdpi.Value
	size, err := strconv.ParseFloat(sv, 64)
	if err != nil {
		return fmt.Errorf("failed to convert value to float: %w", err)
	}

	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		return fmt.Errorf("getSettings failed: %w", err)
	}

	p.mu.RLock()
	g, ok := p.graphs[event.Context]
	p.mu.RUnlock()
	if !ok {
		return fmt.Errorf("no graph for context: %s", event.Context)
	}

	switch key {
	case "titleFontSize":
		settings.TitleFontSize = size
		g.SetLabelFontSize(0, size)
	case "valueFontSize":
		settings.ValueFontSize = size
		g.SetLabelFontSize(1, size)
	default:
		return fmt.Errorf("invalid key: %s", sdpi.Key)
	}

	err = p.sd.SetSettings(event.Context, &settings)
	if err != nil {
		return fmt.Errorf("SetSettings failed: %w", err)
	}

	p.am.SetAction(event.Action, event.Context, &settings)
	return nil
}

func (p *Plugin) handleWarningEnabled(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection) error {
	enabled := sdpi.Checked
	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		return fmt.Errorf("handleWarningEnabled getSettings: %v", err)
	}
	settings.WarningEnabled = enabled
	// Set default operator if not set (HTML default is ">")
	if enabled && settings.WarningOperator == "" {
		settings.WarningOperator = ">"
	}
	// Reset alert state when disabled and not in critical
	if !enabled && settings.CurrentAlertState == "warning" {
		settings.CurrentAlertState = "none"
		p.mu.RLock()
		g, ok := p.graphs[event.Context]
		p.mu.RUnlock()
		if ok {
			p.applyNormalColors(g, &settings)
		}
	}
	err = p.sd.SetSettings(event.Context, &settings)
	if err != nil {
		return fmt.Errorf("handleWarningEnabled SetSettings: %v", err)
	}
	p.am.SetAction(event.Action, event.Context, &settings)
	return nil
}

func (p *Plugin) handleCriticalEnabled(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection) error {
	enabled := sdpi.Checked
	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		return fmt.Errorf("handleCriticalEnabled getSettings: %v", err)
	}
	settings.CriticalEnabled = enabled
	// Set default operator if not set (HTML default is ">")
	if enabled && settings.CriticalOperator == "" {
		settings.CriticalOperator = ">"
	}
	// Reset alert state when disabled
	if !enabled && settings.CurrentAlertState == "critical" {
		settings.CurrentAlertState = "none"
		p.mu.RLock()
		g, ok := p.graphs[event.Context]
		p.mu.RUnlock()
		if ok {
			p.applyNormalColors(g, &settings)
		}
	}
	err = p.sd.SetSettings(event.Context, &settings)
	if err != nil {
		return fmt.Errorf("handleCriticalEnabled SetSettings: %v", err)
	}
	p.am.SetAction(event.Action, event.Context, &settings)
	return nil
}

func (p *Plugin) handleWarningValue(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection) error {
	value, err := strconv.ParseFloat(sdpi.Value, 64)
	if err != nil {
		return fmt.Errorf("handleWarningValue ParseFloat: %v", err)
	}
	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		return fmt.Errorf("handleWarningValue getSettings: %v", err)
	}
	settings.WarningValue = value
	err = p.sd.SetSettings(event.Context, &settings)
	if err != nil {
		return fmt.Errorf("handleWarningValue SetSettings: %v", err)
	}
	p.am.SetAction(event.Action, event.Context, &settings)
	return nil
}

func (p *Plugin) handleCriticalValue(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection) error {
	value, err := strconv.ParseFloat(sdpi.Value, 64)
	if err != nil {
		return fmt.Errorf("handleCriticalValue ParseFloat: %v", err)
	}
	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		return fmt.Errorf("handleCriticalValue getSettings: %v", err)
	}
	settings.CriticalValue = value
	err = p.sd.SetSettings(event.Context, &settings)
	if err != nil {
		return fmt.Errorf("handleCriticalValue SetSettings: %v", err)
	}
	p.am.SetAction(event.Action, event.Context, &settings)
	return nil
}

func (p *Plugin) handleWarningOperator(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection) error {
	operator := sdpi.Value
	if !isValidOperator(operator) {
		return fmt.Errorf("handleWarningOperator invalid operator: %s", operator)
	}
	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		return fmt.Errorf("handleWarningOperator getSettings: %v", err)
	}
	settings.WarningOperator = operator
	err = p.sd.SetSettings(event.Context, &settings)
	if err != nil {
		return fmt.Errorf("handleWarningOperator SetSettings: %v", err)
	}
	p.am.SetAction(event.Action, event.Context, &settings)
	return nil
}

func (p *Plugin) handleCriticalOperator(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection) error {
	operator := sdpi.Value
	if !isValidOperator(operator) {
		return fmt.Errorf("handleCriticalOperator invalid operator: %s", operator)
	}
	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		return fmt.Errorf("handleCriticalOperator getSettings: %v", err)
	}
	settings.CriticalOperator = operator
	err = p.sd.SetSettings(event.Context, &settings)
	if err != nil {
		return fmt.Errorf("handleCriticalOperator SetSettings: %v", err)
	}
	p.am.SetAction(event.Action, event.Context, &settings)
	return nil
}

// isValidOperator checks if the given operator is valid
func isValidOperator(op string) bool {
	switch op {
	case ">", "<", ">=", "<=", "==":
		return true
	default:
		return false
	}
}

// evaluateThreshold checks if value triggers the threshold based on operator
func evaluateThreshold(value, threshold float64, operator string) bool {
	switch operator {
	case ">":
		return value > threshold
	case "<":
		return value < threshold
	case ">=":
		return value >= threshold
	case "<=":
		return value <= threshold
	case "==":
		// For == comparison, round both to integers since displayed values are typically integers
		// e.g., displayed "52%" might actually be 52.34, user expects == 52 to match
		return int(value+0.5) == int(threshold+0.5)
	default:
		return false
	}
}

func (p *Plugin) handleGraphVisuals(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection) error {
	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		return fmt.Errorf("handleGraphVisuals getSettings: %w", err)
	}
	p.mu.RLock()
	g, ok := p.graphs[event.Context]
	p.mu.RUnlock()
	if !ok {
		return fmt.Errorf("handleGraphVisuals no graph for context: %s", event.Context)
	}
	switch sdpi.Key {
	case "graphHeightPct":
		if v, err2 := strconv.Atoi(sdpi.Value); err2 == nil && v >= 10 && v <= 100 {
			settings.GraphHeightPct = v
			g.SetHeightPct(v)
		}
	case "graphLineThickness":
		if v, err2 := strconv.Atoi(sdpi.Value); err2 == nil && v >= 1 && v <= 4 {
			settings.GraphLineThickness = v
			g.SetLineThickness(v)
		}
	case "textStroke":
		settings.TextStroke = sdpi.Checked
		g.SetTextStroke(sdpi.Checked)
	case "textStrokeColor":
		settings.TextStrokeColor = sdpi.Value
		if sdpi.Value != "" {
			g.SetTextStrokeColor(hexToRGBA(sdpi.Value))
		} else {
			g.SetTextStrokeColor(nil)
		}
	case "updateIntervalOverrideMs":
		if v, err2 := strconv.Atoi(sdpi.Value); err2 == nil {
			settings.UpdateIntervalOverrideMs = v
		}
	case "smoothingAlpha":
		if v, err2 := strconv.ParseFloat(sdpi.Value, 64); err2 == nil {
			settings.SmoothingAlpha = v
			p.mu.Lock()
			delete(p.smoothedValues, event.Context)
			p.mu.Unlock()
		}
	}
	if err = p.sd.SetSettings(event.Context, &settings); err != nil {
		return fmt.Errorf("handleGraphVisuals SetSettings: %w", err)
	}
	p.am.SetAction(event.Action, event.Context, &settings)
	return nil
}

// applyNormalColors applies normal (non-alert) colors to the graph
func (p *Plugin) applyNormalColors(g *graph.Graph, s *actionSettings) {
	if s.ForegroundColor != "" {
		g.SetForegroundColor(hexToRGBA(s.ForegroundColor))
	} else {
		g.SetForegroundColor(&color.RGBA{0, 81, 40, 255})
	}
	if s.BackgroundColor != "" {
		g.SetBackgroundColor(hexToRGBA(s.BackgroundColor))
	} else {
		g.SetBackgroundColor(&color.RGBA{0, 0, 0, 255})
	}
	if s.HighlightColor != "" {
		g.SetHighlightColor(hexToRGBA(s.HighlightColor))
	} else {
		g.SetHighlightColor(&color.RGBA{0, 158, 0, 255})
	}
	if s.ValueTextColor != "" {
		g.SetLabelColor(1, hexToRGBA(s.ValueTextColor))
	} else {
		g.SetLabelColor(1, &color.RGBA{255, 255, 255, 255})
	}
	if s.ValueTextColor != "" {
		g.SetLabelColor(2, hexToRGBA(s.ValueTextColor))
	} else {
		g.SetLabelColor(2, &color.RGBA{255, 255, 255, 255})
	}
}

// ============================================================================
// Dynamic Threshold Handlers
// ============================================================================

// findThresholdByID returns pointer to threshold and its index
func (s *actionSettings) findThresholdByID(id string) (*Threshold, int) {
	for i := range s.Thresholds {
		if s.Thresholds[i].ID == id {
			return &s.Thresholds[i], i
		}
	}
	return nil, -1
}

// handleAddThreshold adds a new threshold to the settings
func (p *Plugin) handleAddThreshold(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection) error {
	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		return fmt.Errorf("handleAddThreshold getSettings: %v", err)
	}

	// New thresholds get priority 0 (lowest, appears at bottom since list is sorted highest first)
	name := sdpi.Value
	if name == "" {
		name = "New"
	}

	bg := defaultColor(settings.BackgroundColor, "#000000")
	fg := defaultColor(settings.ForegroundColor, "#005128")
	hl := defaultColor(settings.HighlightColor, "#009e00")
	vt := defaultColor(settings.ValueTextColor, "#ffffff")

	newThreshold := Threshold{
		ID:              fmt.Sprintf("threshold_%d", time.Now().UnixNano()),
		Name:            name,
		Text:            "",
		TextColor:       vt,
		Enabled:         true,
		Operator:        ">=",
		Value:           0,
		Hysteresis:      defaultThresholdHysteresis,
		DwellMs:         defaultThresholdDwellMs,
		CooldownMs:      defaultThresholdCooldownMs,
		BackgroundColor: bg,
		ForegroundColor: fg,
		HighlightColor:  hl,
		ValueTextColor:  vt,
	}

	settings.Thresholds = append(settings.Thresholds, newThreshold)
	p.markThresholdDirty(event.Context)

	if err := p.sd.SetSettings(event.Context, &settings); err != nil {
		return fmt.Errorf("handleAddThreshold SetSettings: %v", err)
	}
	p.am.SetAction(event.Action, event.Context, &settings)

	// Send updated thresholds to PI
	return p.sendThresholdsToPI(event.Action, event.Context, &settings)
}

// handleRemoveThreshold removes a threshold from the settings
func (p *Plugin) handleRemoveThreshold(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection) error {
	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		return fmt.Errorf("handleRemoveThreshold getSettings: %v", err)
	}

	thresholdID := sdpi.ThresholdID
	_, idx := settings.findThresholdByID(thresholdID)
	if idx == -1 {
		return fmt.Errorf("threshold not found: %s", thresholdID)
	}

	// Remove threshold from slice
	settings.Thresholds = append(settings.Thresholds[:idx], settings.Thresholds[idx+1:]...)

	// Clear current threshold if it was the removed one
	if settings.CurrentThresholdID == thresholdID {
		settings.CurrentThresholdID = ""
		p.mu.RLock()
		g, ok := p.graphs[event.Context]
		p.mu.RUnlock()
		if ok {
			p.applyNormalColors(g, &settings)
		}
	}
	p.resetThresholdRuntimeState(event.Context, thresholdID)

	if err := p.sd.SetSettings(event.Context, &settings); err != nil {
		return fmt.Errorf("handleRemoveThreshold SetSettings: %v", err)
	}
	p.am.SetAction(event.Action, event.Context, &settings)

	return p.sendThresholdsToPI(event.Action, event.Context, &settings)
}

// handleReorderThreshold moves a threshold up/down in priority order
func (p *Plugin) handleReorderThreshold(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection) error {
	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		return fmt.Errorf("handleReorderThreshold getSettings: %v", err)
	}

	if len(settings.Thresholds) < 2 {
		return nil
	}

	direction := sdpi.Value
	if direction != "up" && direction != "down" {
		return fmt.Errorf("handleReorderThreshold invalid direction: %s", direction)
	}

	pos := -1
	for i := range settings.Thresholds {
		if settings.Thresholds[i].ID == sdpi.ThresholdID {
			pos = i
			break
		}
	}
	if pos == -1 {
		return fmt.Errorf("threshold not found: %s", sdpi.ThresholdID)
	}

	switch direction {
	case "up":
		if pos == 0 {
			return nil
		}
		settings.Thresholds[pos-1], settings.Thresholds[pos] = settings.Thresholds[pos], settings.Thresholds[pos-1]
	case "down":
		if pos == len(settings.Thresholds)-1 {
			return nil
		}
		settings.Thresholds[pos], settings.Thresholds[pos+1] = settings.Thresholds[pos+1], settings.Thresholds[pos]
	}
	p.markThresholdDirty(event.Context)

	if err := p.sd.SetSettings(event.Context, &settings); err != nil {
		return fmt.Errorf("handleReorderThreshold SetSettings: %v", err)
	}
	p.am.SetAction(event.Action, event.Context, &settings)

	return p.sendThresholdsToPI(event.Action, event.Context, &settings)
}

// handleThresholdUpdate updates a specific field of a threshold
func (p *Plugin) handleThresholdUpdate(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection) error {
	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		return fmt.Errorf("handleThresholdUpdate getSettings: %v", err)
	}

	threshold, _ := settings.findThresholdByID(sdpi.ThresholdID)
	if threshold == nil {
		return fmt.Errorf("threshold not found: %s", sdpi.ThresholdID)
	}

	needsReEvaluation := false
	needsColorUpdate := false
	resetRuntimeState := false

	// Update based on key
	switch sdpi.Key {
	case "thresholdEnabled":
		threshold.Enabled = sdpi.Checked
		needsReEvaluation = true
		resetRuntimeState = true
	case "thresholdName":
		threshold.Name = sdpi.Value
	case "thresholdOperator":
		if isValidOperator(sdpi.Value) {
			threshold.Operator = sdpi.Value
			needsReEvaluation = true
			resetRuntimeState = true
		}
	case "thresholdValue":
		value, _ := strconv.ParseFloat(sdpi.Value, 64)
		threshold.Value = value
		needsReEvaluation = true
		resetRuntimeState = true
	case "thresholdHysteresis":
		value, _ := strconv.ParseFloat(sdpi.Value, 64)
		threshold.Hysteresis = value
		needsReEvaluation = true
		resetRuntimeState = true
	case "thresholdDwellMs":
		value, _ := strconv.Atoi(sdpi.Value)
		threshold.DwellMs = value
		needsReEvaluation = true
		resetRuntimeState = true
	case "thresholdCooldownMs":
		value, _ := strconv.Atoi(sdpi.Value)
		threshold.CooldownMs = value
		needsReEvaluation = true
		resetRuntimeState = true
	case "thresholdSticky":
		threshold.Sticky = sdpi.Checked
		needsReEvaluation = true
		resetRuntimeState = true
	case "thresholdText":
		threshold.Text = sdpi.Value
	case "thresholdTextColor":
		threshold.TextColor = sdpi.Value
		needsColorUpdate = settings.CurrentThresholdID == threshold.ID
	case "thresholdBackgroundColor":
		threshold.BackgroundColor = sdpi.Value
		needsColorUpdate = settings.CurrentThresholdID == threshold.ID
	case "thresholdForegroundColor":
		threshold.ForegroundColor = sdpi.Value
		needsColorUpdate = settings.CurrentThresholdID == threshold.ID
	case "thresholdHighlightColor":
		threshold.HighlightColor = sdpi.Value
		needsColorUpdate = settings.CurrentThresholdID == threshold.ID
	case "thresholdValueTextColor":
		threshold.ValueTextColor = sdpi.Value
		needsColorUpdate = settings.CurrentThresholdID == threshold.ID
	}

	// Re-evaluate thresholds and apply colors immediately if needed
	if needsReEvaluation {
		p.markThresholdDirty(event.Context)
	}
	if resetRuntimeState {
		p.resetThresholdRuntimeState(event.Context, threshold.ID)
	} else {
		p.markThresholdDirty(event.Context)
	}

	// If this threshold is currently active and its colors changed, apply immediately
	if needsColorUpdate {
		p.mu.RLock()
		g, ok := p.graphs[event.Context]
		p.mu.RUnlock()
		if ok {
			p.applyThresholdColors(g, threshold)
		}
	}

	if err := p.sd.SetSettings(event.Context, &settings); err != nil {
		return fmt.Errorf("handleThresholdUpdate SetSettings: %v", err)
	}
	p.am.SetAction(event.Action, event.Context, &settings)
	if sdpi.Key == "thresholdSticky" || needsReEvaluation || needsColorUpdate {
		p.refreshAction(event.Action, event.Context)
	}
	return nil
}

// applyThresholdColors applies colors from a specific threshold to the graph
func (p *Plugin) applyThresholdColors(g *graph.Graph, t *Threshold) {
	if t.BackgroundColor != "" {
		g.SetBackgroundColor(hexToRGBA(t.BackgroundColor))
	}
	if t.ForegroundColor != "" {
		g.SetForegroundColor(hexToRGBA(t.ForegroundColor))
	}
	if t.HighlightColor != "" {
		g.SetHighlightColor(hexToRGBA(t.HighlightColor))
	}
	if t.ValueTextColor != "" {
		g.SetLabelColor(1, hexToRGBA(t.ValueTextColor))
	}
	if t.TextColor != "" {
		g.SetLabelColor(2, hexToRGBA(t.TextColor))
	}
}

// sendThresholdsToPI sends the current thresholds to the Property Inspector
func (p *Plugin) sendThresholdsToPI(action, context string, settings *actionSettings) error {
	p.mu.RLock()
	globals := make([]Threshold, len(p.globalSettings.GlobalThresholds))
	copy(globals, p.globalSettings.GlobalThresholds)
	p.mu.RUnlock()
	payload := map[string]interface{}{
		"thresholds":       settings.Thresholds,
		"settings":         settings,
		"globalThresholds": globals,
	}
	return p.sd.SendToPropertyInspector(action, context, payload)
}

// resolveThresholdsForEval merges per-tile thresholds with type-matched globals.
// Globals whose ReadingType matches readingType (or is empty = all types) are appended,
// unless their ID appears in suppressed. Must be called with p.mu.RLock held.
func (p *Plugin) resolveThresholdsForEval(local []Threshold, suppressed []string, readingType hwsensorsservice.ReadingType) []Threshold {
	globals := p.globalSettings.GlobalThresholds
	if len(globals) == 0 {
		return local
	}
	rtStr := readingType.String()
	result := make([]Threshold, len(local), len(local)+len(globals))
	copy(result, local)
outer:
	for i := range globals {
		gt := globals[i]
		if gt.ReadingType != "" && gt.ReadingType != rtStr {
			continue
		}
		for _, sid := range suppressed {
			if sid == gt.ID {
				continue outer
			}
		}
		result = append(result, gt)
	}
	return result
}

// broadcastGlobalThresholds sends the current global threshold list to all open tile PIs.
func (p *Plugin) broadcastGlobalThresholds() {
	p.mu.RLock()
	globals := make([]Threshold, len(p.globalSettings.GlobalThresholds))
	copy(globals, p.globalSettings.GlobalThresholds)
	p.mu.RUnlock()

	payload := map[string]interface{}{"globalThresholds": globals}

	for _, a := range p.am.AllActions() {
		_ = p.sd.SendToPropertyInspector(a.action, a.context, payload)
	}

	p.mu.RLock()
	compositeCtxs := make([]string, 0, len(p.compositeSettings))
	for ctx := range p.compositeSettings {
		compositeCtxs = append(compositeCtxs, ctx)
	}
	derivedCtxs := make([]string, 0, len(p.derivedSettings))
	for ctx := range p.derivedSettings {
		derivedCtxs = append(derivedCtxs, ctx)
	}
	p.mu.RUnlock()

	for _, ctx := range compositeCtxs {
		_ = p.sd.SendToPropertyInspector(compositeAction, ctx, payload)
	}
	for _, ctx := range derivedCtxs {
		_ = p.sd.SendToPropertyInspector(derivedAction, ctx, payload)
	}
}

// handleGlobalAddThreshold adds a new threshold to the global library.
func (p *Plugin) handleGlobalAddThreshold(name string) {
	if name == "" {
		name = "New"
	}
	newThreshold := Threshold{
		ID:              fmt.Sprintf("gthreshold_%d", time.Now().UnixNano()),
		Name:            name,
		Enabled:         true,
		Operator:        ">=",
		Value:           0,
		Hysteresis:      defaultThresholdHysteresis,
		DwellMs:         defaultThresholdDwellMs,
		CooldownMs:      defaultThresholdCooldownMs,
		BackgroundColor: "#333300",
		ForegroundColor: "#999900",
		HighlightColor:  "#ffff00",
		ValueTextColor:  "#ffff00",
		TextColor:       "#ffffff",
	}
	p.mu.Lock()
	p.globalSettings.GlobalThresholds = append(p.globalSettings.GlobalThresholds, newThreshold)
	gs := p.globalSettings
	p.mu.Unlock()
	if err := p.sd.SetGlobalSettings(gs); err != nil {
		log.Printf("handleGlobalAddThreshold SetGlobalSettings: %v", err)
	}
	p.broadcastGlobalThresholds()
}

// handleGlobalRemoveThreshold removes a threshold from the global library by ID.
func (p *Plugin) handleGlobalRemoveThreshold(id string) {
	p.mu.Lock()
	ts := p.globalSettings.GlobalThresholds
	for i, t := range ts {
		if t.ID == id {
			p.globalSettings.GlobalThresholds = append(ts[:i], ts[i+1:]...)
			break
		}
	}
	gs := p.globalSettings
	p.mu.Unlock()
	if err := p.sd.SetGlobalSettings(gs); err != nil {
		log.Printf("handleGlobalRemoveThreshold SetGlobalSettings: %v", err)
	}
	p.broadcastGlobalThresholds()
}

// handleGlobalThresholdUpdate updates a single field of a global threshold.
func (p *Plugin) handleGlobalThresholdUpdate(id, field, value string, checked bool) {
	p.mu.Lock()
	var t *Threshold
	for i := range p.globalSettings.GlobalThresholds {
		if p.globalSettings.GlobalThresholds[i].ID == id {
			t = &p.globalSettings.GlobalThresholds[i]
			break
		}
	}
	if t == nil {
		p.mu.Unlock()
		return
	}
	switch field {
	case "thresholdEnabled":
		t.Enabled = checked
	case "thresholdName":
		t.Name = value
	case "thresholdOperator":
		if isValidOperator(value) {
			t.Operator = value
		}
	case "thresholdValue":
		t.Value, _ = strconv.ParseFloat(value, 64)
	case "thresholdHysteresis":
		t.Hysteresis, _ = strconv.ParseFloat(value, 64)
	case "thresholdDwellMs":
		t.DwellMs, _ = strconv.Atoi(value)
	case "thresholdCooldownMs":
		t.CooldownMs, _ = strconv.Atoi(value)
	case "thresholdSticky":
		t.Sticky = checked
	case "thresholdText":
		t.Text = value
	case "thresholdTextColor":
		t.TextColor = value
	case "thresholdBackgroundColor":
		t.BackgroundColor = value
	case "thresholdForegroundColor":
		t.ForegroundColor = value
	case "thresholdHighlightColor":
		t.HighlightColor = value
	case "thresholdValueTextColor":
		t.ValueTextColor = value
	case "thresholdReadingType":
		t.ReadingType = value
	}
	gs := p.globalSettings
	p.mu.Unlock()
	if err := p.sd.SetGlobalSettings(gs); err != nil {
		log.Printf("handleGlobalThresholdUpdate SetGlobalSettings: %v", err)
	}
	p.markGlobalThresholdDirty(id)
	p.broadcastGlobalThresholds()
}

// markGlobalThresholdDirty marks all tiles dirty so they re-render with updated global threshold state.
// With type-scoped globals we can't cheaply filter by sensor type here, so we mark everything.
func (p *Plugin) markGlobalThresholdDirty(_ string) {
	for _, a := range p.am.AllActions() {
		p.markThresholdDirty(a.context)
	}
	p.mu.RLock()
	var ctxs []string
	for ctx := range p.compositeSettings {
		ctxs = append(ctxs, ctx)
	}
	for ctx := range p.derivedSettings {
		ctxs = append(ctxs, ctx)
	}
	p.mu.RUnlock()
	for _, ctx := range ctxs {
		p.markThresholdDirty(ctx)
	}
}

// handleSuppressGlobal adds a global threshold ID to a standard tile's suppress list.
func (p *Plugin) handleSuppressGlobal(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection) error {
	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		return fmt.Errorf("handleSuppressGlobal getSettings: %v", err)
	}
	for _, id := range settings.SuppressedGlobalIDs {
		if id == sdpi.Value {
			return nil
		}
	}
	settings.SuppressedGlobalIDs = append(settings.SuppressedGlobalIDs, sdpi.Value)
	p.markThresholdDirty(event.Context)
	if err := p.sd.SetSettings(event.Context, &settings); err != nil {
		return fmt.Errorf("handleSuppressGlobal SetSettings: %v", err)
	}
	p.am.SetAction(event.Action, event.Context, &settings)
	return nil
}

// handleUnsuppressGlobal removes a global threshold ID from a standard tile's suppress list.
func (p *Plugin) handleUnsuppressGlobal(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection) error {
	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		return fmt.Errorf("handleUnsuppressGlobal getSettings: %v", err)
	}
	for i, id := range settings.SuppressedGlobalIDs {
		if id == sdpi.Value {
			settings.SuppressedGlobalIDs = append(settings.SuppressedGlobalIDs[:i], settings.SuppressedGlobalIDs[i+1:]...)
			break
		}
	}
	p.markThresholdDirty(event.Context)
	if err := p.sd.SetSettings(event.Context, &settings); err != nil {
		return fmt.Errorf("handleUnsuppressGlobal SetSettings: %v", err)
	}
	p.am.SetAction(event.Action, event.Context, &settings)
	return nil
}

// handleDerivedSuppressGlobal adds a global threshold ID to a derived tile's suppress list.
func (p *Plugin) handleDerivedSuppressGlobal(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection) {
	p.mu.Lock()
	s, ok := p.derivedSettings[event.Context]
	if !ok {
		p.mu.Unlock()
		return
	}
	for _, id := range s.SuppressedGlobalIDs {
		if id == sdpi.Value {
			p.mu.Unlock()
			return
		}
	}
	s.SuppressedGlobalIDs = append(s.SuppressedGlobalIDs, sdpi.Value)
	p.mu.Unlock()
	p.markThresholdDirty(event.Context)
	_ = p.sd.SetSettings(event.Context, s)
}

// handleDerivedUnsuppressGlobal removes a global threshold ID from a derived tile's suppress list.
func (p *Plugin) handleDerivedUnsuppressGlobal(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection) {
	p.mu.Lock()
	s, ok := p.derivedSettings[event.Context]
	if !ok {
		p.mu.Unlock()
		return
	}
	for i, id := range s.SuppressedGlobalIDs {
		if id == sdpi.Value {
			s.SuppressedGlobalIDs = append(s.SuppressedGlobalIDs[:i], s.SuppressedGlobalIDs[i+1:]...)
			break
		}
	}
	p.mu.Unlock()
	p.markThresholdDirty(event.Context)
	_ = p.sd.SetSettings(event.Context, s)
}

// handleCompositeSlotSuppressGlobal adds a global threshold ID to a composite slot's suppress list.
func (p *Plugin) handleCompositeSlotSuppressGlobal(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection, slotIdx int) {
	p.mu.Lock()
	s, ok := p.compositeSettings[event.Context]
	if !ok || slotIdx < 0 || slotIdx >= 4 {
		p.mu.Unlock()
		return
	}
	for _, id := range s.Slots[slotIdx].SuppressedGlobalIDs {
		if id == sdpi.Value {
			p.mu.Unlock()
			return
		}
	}
	s.Slots[slotIdx].SuppressedGlobalIDs = append(s.Slots[slotIdx].SuppressedGlobalIDs, sdpi.Value)
	p.mu.Unlock()
	p.markThresholdDirty(event.Context)
	_ = p.sd.SetSettings(event.Context, s)
}

// handleCompositeSlotUnsuppressGlobal removes a global threshold ID from a composite slot's suppress list.
func (p *Plugin) handleCompositeSlotUnsuppressGlobal(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection, slotIdx int) {
	p.mu.Lock()
	s, ok := p.compositeSettings[event.Context]
	if !ok || slotIdx < 0 || slotIdx >= 4 {
		p.mu.Unlock()
		return
	}
	refs := s.Slots[slotIdx].SuppressedGlobalIDs
	for i, id := range refs {
		if id == sdpi.Value {
			s.Slots[slotIdx].SuppressedGlobalIDs = append(refs[:i], refs[i+1:]...)
			break
		}
	}
	p.mu.Unlock()
	p.markThresholdDirty(event.Context)
	_ = p.sd.SetSettings(event.Context, s)
}
