package hwinfostreamdeckplugin

import (
	"encoding/json"
	"fmt"
	"image/color"
	"log"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
	"github.com/moeilijk/hwinfo-streamdeck/pkg/graph"
	hwsensorsservice "github.com/moeilijk/hwinfo-streamdeck/pkg/service"
	"github.com/moeilijk/hwinfo-streamdeck/pkg/streamdeck"
)

const (
	tileWidth  = 72
	tileHeight = 72
)

func boolPtr(v bool) *bool {
	return &v
}

func (p *Plugin) isSettingsAction(action, context string) bool {
	if action == "com.moeilijk.hwinfo.settings" {
		return true
	}
	p.mu.RLock()
	_, ok := p.settingsContexts[context]
	p.mu.RUnlock()
	return ok
}

func (p *Plugin) resolveSettingsContext(context string) string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if _, ok := p.settingsContexts[context]; ok {
		return context
	}
	if len(p.settingsContexts) == 1 {
		for k := range p.settingsContexts {
			return k
		}
	}
	return context
}

func isSettingsPayload(m map[string]*json.RawMessage) bool {
	if m == nil {
		return false
	}
	for _, k := range []string{"settingsConnected", "setPollInterval", "updateTileAppearance",
		"requestSettingsStatus",
		"addGlobalThreshold", "deleteGlobalThreshold", "updateGlobalThreshold"} {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}

func (p *Plugin) sendSettingsStatus(action, context string) {
	p.mu.RLock()
	currentRate := p.globalSettings.PollInterval
	p.mu.RUnlock()

	if currentRate <= 0 {
		currentRate = int(p.am.GetInterval().Milliseconds())
	}

	status := "Disconnected"
	if pt, err := p.getCachedPollTime(); err == nil && pt != 0 {
		status = "Connected"
	}

	statusPayload := map[string]interface{}{
		"connectionStatus": status,
		"currentRate":      currentRate,
	}
	if err := p.sd.SendToPropertyInspector(action, context, statusPayload); err != nil {
		log.Printf("SendToPropertyInspector settings status failed: %v\n", err)
	}
}

// OnConnected event
func (p *Plugin) OnConnected(c *websocket.Conn) {
	// Request global settings on connect
	if err := p.sd.GetGlobalSettings(); err != nil {
		log.Printf("GetGlobalSettings failed: %v\n", err)
	}
}

// OnWillAppear event
func (p *Plugin) OnWillAppear(event *streamdeck.EvWillAppear) {
	if event.Action == dialAction && event.Payload.Controller == "Encoder" {
		p.handleDialWillAppear(event)
		return
	}

	// Handle settings action separately
	if event.Action == "com.moeilijk.hwinfo.settings" {
		// Decode tile appearance settings from payload
		var tileSettings settingsTileSettings
		hasShowLabel := false
		hasTitle := false
		hasTitleColor := false
		hasShowTitleInGraph := false
		if event.Payload.Settings != nil {
			var rawSettings map[string]json.RawMessage
			if err := json.Unmarshal(*event.Payload.Settings, &rawSettings); err == nil {
				_, hasShowLabel = rawSettings["showLabel"]
				_, hasTitle = rawSettings["title"]
				_, hasTitleColor = rawSettings["titleColor"]
				_, hasShowTitleInGraph = rawSettings["showTitleInGraph"]
			}
			if err := json.Unmarshal(*event.Payload.Settings, &tileSettings); err != nil {
				log.Printf("OnWillAppear settings tile unmarshal: %v\n", err)
			}
		}
		p.mu.Lock()
		if existing := p.settingsContexts[event.Context]; existing != nil {
			if !hasTitle {
				tileSettings.Title = existing.Title
			}
			if !hasTitleColor {
				tileSettings.TitleColor = existing.TitleColor
			}
			if !hasShowTitleInGraph {
				tileSettings.ShowTitleInGraph = existing.ShowTitleInGraph
			}
		}
		p.mu.Unlock()
		// Set defaults if not set
		if tileSettings.TileBackground == "" {
			tileSettings.TileBackground = "#000000"
		}
		if tileSettings.TileTextColor == "" {
			tileSettings.TileTextColor = "#ffffff"
		}
		if !hasShowLabel {
			tileSettings.ShowLabel = true
		}
		if tileSettings.TitleColor == "" {
			tileSettings.TitleColor = "#b7b7b7"
		}
		if tileSettings.ShowTitleInGraph == nil {
			tileSettings.ShowTitleInGraph = boolPtr(true)
		}
		p.mu.Lock()
		p.settingsContexts[event.Context] = &tileSettings
		p.mu.Unlock()
		p.updateSettingsTile(event.Context)
		return
	}

	if event.Action == derivedAction {
		ds, _ := decodeDerivedSettings(event.Payload.Settings)
		p.mu.Lock()
		p.derivedSettings[event.Context] = &ds
		p.derivedStates[event.Context] = &derivedState{graph: initDerivedGraph(&ds)}
		p.mu.Unlock()
		return
	}

	if event.Action == compositeAction {
		cs, _ := decodeCompositeSettings(event.Payload.Settings)
		p.mu.Lock()
		p.compositeSettings[event.Context] = &cs
		p.compositeStates[event.Context] = &compositeState{graphs: initCompositeGraphs(&cs)}
		p.mu.Unlock()
		return
	}

	settings, migrated, err := decodeActionSettings(event.Payload.Settings)
	if err != nil {
		log.Println("OnWillAppear settings unmarshal", err)
	}
	tfSize := 10.5
	vfSize := 10.5
	var fgColor *color.RGBA
	var bgColor *color.RGBA
	var hlColor *color.RGBA
	var tColor *color.RGBA
	var vtColor *color.RGBA
	if settings.TitleFontSize != 0 {
		tfSize = settings.TitleFontSize
	}
	if settings.ValueFontSize != 0 {
		vfSize = settings.ValueFontSize
	}
	if settings.ForegroundColor == "" {
		fgColor = &color.RGBA{0, 81, 40, 255}
	} else {
		fgColor = hexToRGBA(settings.ForegroundColor)
	}
	if settings.BackgroundColor == "" {
		bgColor = &color.RGBA{0, 0, 0, 255}
	} else {
		bgColor = hexToRGBA(settings.BackgroundColor)
	}
	if settings.HighlightColor == "" {
		hlColor = &color.RGBA{0, 158, 0, 255}
	} else {
		hlColor = hexToRGBA(settings.HighlightColor)
	}
	if settings.TitleColor == "" {
		tColor = &color.RGBA{183, 183, 183, 255}
	} else {
		tColor = hexToRGBA(settings.TitleColor)
	}
	if settings.ValueTextColor == "" {
		vtColor = &color.RGBA{255, 255, 255, 255}
	} else {
		vtColor = hexToRGBA(settings.ValueTextColor)
	}
	drawTitle := true
	if settings.ShowTitleInGraph != nil {
		drawTitle = *settings.ShowTitleInGraph
	} else {
		settings.ShowTitleInGraph = boolPtr(drawTitle)
	}
	g := graph.NewGraph(tileWidth, tileHeight, settings.Min, settings.Max, fgColor, bgColor, hlColor)
	g.SetLabel(0, "", 19, tColor)
	g.SetLabelFontSize(0, tfSize)
	g.SetLabel(1, "", 44, vtColor)
	g.SetLabelFontSize(1, vfSize)
	g.SetLabel(2, "", 56, vtColor)
	g.SetLabelFontSize(2, vfSize)
	if settings.GraphHeightPct > 0 {
		g.SetHeightPct(settings.GraphHeightPct)
	}
	if settings.GraphLineThickness > 0 {
		g.SetLineThickness(settings.GraphLineThickness)
	}
	g.SetTextStroke(settings.TextStroke)
	if settings.TextStrokeColor != "" {
		g.SetTextStrokeColor(hexToRGBA(settings.TextStrokeColor))
	}
	if drawTitle {
		g.SetLabelText(0, settings.Title)
	}
	p.mu.Lock()
	p.graphs[event.Context] = g
	p.mu.Unlock()
	p.resetThresholdRuntimeState(event.Context, "")

	// Reset threshold state so updateTiles will re-evaluate and apply correct colors on first run
	if settings.CurrentThresholdID != "" {
		settings.CurrentThresholdID = ""
		migrated = true
	}
	// Legacy: reset old alert state
	if settings.CurrentAlertState != "" {
		settings.CurrentAlertState = ""
		migrated = true
	}

	// Ensure all enabled thresholds have operators
	for i := range settings.Thresholds {
		if settings.Thresholds[i].Enabled && settings.Thresholds[i].Operator == "" {
			settings.Thresholds[i].Operator = ">="
			migrated = true
		}
	}

	// Legacy: Set default operators for old Warning/Critical if still present
	if settings.WarningEnabled && settings.WarningOperator == "" {
		settings.WarningOperator = ">="
		migrated = true
	}
	if settings.CriticalEnabled && settings.CriticalOperator == "" {
		settings.CriticalOperator = ">="
		migrated = true
	}

	p.am.SetAction(event.Action, event.Context, &settings)
	if migrated {
		_ = p.sd.SetSettings(event.Context, &settings)
	}
}

// OnWillDisappear event
func (p *Plugin) OnWillDisappear(event *streamdeck.EvWillDisappear) {
	if event.Action == dialAction && event.Payload.Controller == "Encoder" {
		p.handleDialWillDisappear(event)
		return
	}

	// Handle settings action
	if event.Action == "com.moeilijk.hwinfo.settings" {
		p.mu.Lock()
		delete(p.settingsContexts, event.Context)
		p.mu.Unlock()
		return
	}

	if event.Action == derivedAction {
		p.mu.Lock()
		delete(p.derivedSettings, event.Context)
		delete(p.derivedStates, event.Context)
		p.mu.Unlock()
		return
	}

	if event.Action == compositeAction {
		p.mu.Lock()
		delete(p.compositeSettings, event.Context)
		delete(p.compositeStates, event.Context)
		p.mu.Unlock()
		for i := 0; i < 4; i++ {
			p.clearThresholdRuntimeState(event.Context + "|" + strconv.Itoa(i))
		}
		return
	}

	_, _, err := decodeActionSettings(event.Payload.Settings)
	if err != nil {
		log.Println("OnWillDisappear settings unmarshal", err)
	}
	p.mu.Lock()
	delete(p.graphs, event.Context)
	delete(p.divisorCache, event.Context)
	p.mu.Unlock()
	p.clearThresholdRuntimeState(event.Context)
	p.am.RemoveAction(event.Context)
}

// OnKeyDown snoozes or resumes active threshold alerts for reading tiles.
func (p *Plugin) OnKeyDown(event *streamdeck.EvKeyDown) {
	if event.Action != "com.moeilijk.hwinfo.reading" {
		return
	}

	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		log.Printf("OnKeyDown getSettings: %v\n", err)
		return
	}

	if settings.CurrentThresholdID == "" {
		return
	}

	if configured := normalizeThresholdSnoozeDurations(settings.SnoozeDurations); len(configured) > 0 {
		now := time.Now()
		currentSnooze, snoozed := p.currentThresholdSnooze(event.Context, now)
		var current *thresholdSnoozeState
		if snoozed {
			current = &currentSnooze
		}

		if nextDuration, ok := nextThresholdSnoozeDuration(configured, current); ok {
			p.setThresholdSnooze(event.Context, nextDuration, now)
		} else if !p.clearThresholdSnooze(event.Context) {
			return
		}

		p.refreshAction(event.Action, event.Context)
		return
	}

	if !p.clearStickyThreshold(event.Context, settings.CurrentThresholdID) {
		return
	}

	settings.CurrentThresholdID = ""
	if err := p.sd.SetSettings(event.Context, &settings); err != nil {
		log.Printf("OnKeyDown SetSettings: %v\n", err)
	}
	p.am.SetAction(event.Action, event.Context, &settings)
	p.refreshAction(event.Action, event.Context)
}

// OnApplicationDidLaunch event (unused)
func (p *Plugin) OnApplicationDidLaunch(event *streamdeck.EvApplication) {}

// OnApplicationDidTerminate event (unused)
func (p *Plugin) OnApplicationDidTerminate(event *streamdeck.EvApplication) {}

// OnTitleParametersDidChange event
func (p *Plugin) OnTitleParametersDidChange(event *streamdeck.EvTitleParametersDidChange) {
	if p.isSettingsAction(event.Action, event.Context) {
		targetContext := p.resolveSettingsContext(event.Context)
		p.mu.RLock()
		existing := p.settingsContexts[targetContext]
		p.mu.RUnlock()
		var tileSettings *settingsTileSettings
		if existing == nil {
			tileSettings = &settingsTileSettings{
				TileBackground: "#000000",
				TileTextColor:  "#ffffff",
				ShowLabel:      true,
				Title:          "",
				TitleColor:     "#b7b7b7",
			}
		} else {
			cp := *existing
			tileSettings = &cp
		}

		tileSettings.Title = event.Payload.Title
		if event.Payload.TitleParameters.TitleColor != "" {
			tileSettings.TitleColor = event.Payload.TitleParameters.TitleColor
		} else if tileSettings.TitleColor == "" {
			tileSettings.TitleColor = "#b7b7b7"
		}
		drawTitle := !event.Payload.TitleParameters.ShowTitle
		tileSettings.ShowTitleInGraph = boolPtr(drawTitle)
		p.mu.Lock()
		p.settingsContexts[targetContext] = tileSettings
		p.mu.Unlock()
		if err := p.sd.SetSettings(targetContext, tileSettings); err != nil {
			log.Printf("OnTitleParametersDidChange settings SetSettings: %v\n", err)
		}
		p.updateSettingsTile(targetContext)
		return
	}

	if event.Action == derivedAction {
		p.handleDerivedTitleParametersDidChange(event)
		return
	}

	if event.Action == compositeAction {
		return // composite tile gebruikt geen SD-native titel
	}

	// Get existing settings from actionManager to preserve threshold settings
	// Do NOT decode from event payload as it may have stale/different settings
	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		// If no settings in actionManager yet, decode from payload as fallback
		settings, _, err = decodeActionSettings(event.Payload.Settings)
		if err != nil {
			log.Println("OnTitleParametersDidChange settings unmarshal", err)
		}
	}
	p.mu.RLock()
	g, ok := p.graphs[event.Context]
	p.mu.RUnlock()
	if !ok {
		log.Printf("OnTitleParametersDidChange no graph for context: %s\n", event.Context)
		return
	}
	drawTitle := !event.Payload.TitleParameters.ShowTitle
	if drawTitle {
		g.SetLabelText(0, event.Payload.Title)
		if event.Payload.TitleParameters.TitleColor != "" {
			tClr := hexToRGBA(event.Payload.TitleParameters.TitleColor)
			g.SetLabelColor(0, tClr)
		}
	} else {
		g.SetLabelText(0, "")
	}
	// Only update title-related fields, preserving threshold settings
	settings.Title = event.Payload.Title
	settings.TitleColor = event.Payload.TitleParameters.TitleColor
	settings.ShowTitleInGraph = boolPtr(drawTitle)

	err = p.sd.SetSettings(event.Context, &settings)
	if err != nil {
		log.Printf("OnTitleParametersDidChange SetSettings: %v\n", err)
		return
	}
	p.am.SetAction(event.Action, event.Context, &settings)
}

// OnPropertyInspectorConnected event
func (p *Plugin) OnPropertyInspectorConnected(event *streamdeck.EvSendToPlugin) {
	if event.Action == dialAction {
		p.handleDialPropertyInspectorConnected(event)
		return
	}

	if p.isSettingsAction(event.Action, event.Context) {
		p.sendSettingsStatus("com.moeilijk.hwinfo.settings", event.Context)
		return
	}

	if event.Action == derivedAction {
		p.handleDerivedPropertyInspectorConnected(event)
		return
	}

	if event.Action == compositeAction {
		p.handleCompositePropertyInspectorConnected(event)
		return
	}

	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		log.Println("OnPropertyInspectorConnected getSettings", err)
	}
	sensors, err := p.sensorsWithTimeout(2*time.Second)
	if err != nil {
		log.Println("OnPropertyInspectorConnected Sensors", err)
		go p.restartBridge(p.bridge)
		payload := evStatus{Error: true, Message: "HWiNFO Unavailable"}
		if err := p.sd.SendToPropertyInspector(event.Action, event.Context, payload); err != nil {
			log.Printf("OnPropertyInspectorConnected SendToPropertyInspector: %v\n", err)
		}
		settings.InErrorState = true
		if err := p.sd.SetSettings(event.Context, &settings); err != nil {
			log.Printf("OnPropertyInspectorConnected SetSettings: %v\n", err)
			return
		}
		p.am.SetAction(event.Action, event.Context, &settings)
		return
	}
	evsensors := make([]*evSendSensorsPayloadSensor, 0, len(sensors))
	for _, s := range sensors {
		evsensors = append(evsensors, sensorPayload(s.ID(), s.Name()))
	}
	payload := evSendSensorsPayload{Sensors: evsensors, Settings: &settings}
	err = p.sd.SendToPropertyInspector(event.Action, event.Context, payload)
	if err != nil {
		log.Println("OnPropertyInspectorConnected SendToPropertyInspector", err)
	}
	if err := p.sendCatalogToPropertyInspector(event.Action, event.Context, &settings, sensors); err != nil {
		log.Printf("OnPropertyInspectorConnected sendCatalogToPropertyInspector: %v\n", err)
	}
	p.mu.RLock()
	globals := make([]Threshold, len(p.globalSettings.GlobalThresholds))
	copy(globals, p.globalSettings.GlobalThresholds)
	p.mu.RUnlock()
	_ = p.sd.SendToPropertyInspector(event.Action, event.Context, map[string]interface{}{"globalThresholds": globals})
	if settings.SensorUID != "" {
		readings, rerr := p.sendReadingsToPropertyInspector(event.Action, event.Context, settings.SensorUID, &settings)
		if rerr == nil {
			if syncSettingsWithReadings(&settings, readings) {
				_ = p.sd.SetSettings(event.Context, &settings)
				p.am.SetAction(event.Action, event.Context, &settings)
			}
		}
	}
}

func (p *Plugin) sendReadingsToPropertyInspector(action, context, sensorID string, settings *actionSettings) ([]hwsensorsservice.Reading, error) {
	rt := p.bridge
	rt.mu.RLock()
	hw := rt.hw
	rt.mu.RUnlock()
	if hw == nil {
		return nil, fmt.Errorf("HWiNFO bridge not ready")
	}
	readings, err := hw.ReadingsForSensorID(sensorID)
	if err != nil {
		log.Println("sendReadingsToPropertyInspector ReadingsForSensorID", err)
		return nil, err
	}
	evreadings := make([]*evSendReadingsPayloadReading, 0, len(readings))
	for _, r := range readings {
		evreadings = append(evreadings, &evSendReadingsPayloadReading{
			ID:     r.ID(),
			Label:  r.Label(),
			Prefix: r.Unit(),
			Unit:   r.Unit(),
			Type:   r.Type(),
		})
	}
	payload := evSendReadingsPayload{Readings: evreadings, Settings: settings}
	err = p.sd.SendToPropertyInspector(action, context, payload)
	if err != nil {
		log.Println("sendReadingsToPropertyInspector SendToPropertyInspector", err)
	}
	return readings, nil
}

// OnSendToPlugin event
func (p *Plugin) OnSendToPlugin(event *streamdeck.EvSendToPlugin) {
	var payload map[string]*json.RawMessage
	err := json.Unmarshal(*event.Payload, &payload)
	if err != nil {
		log.Println("OnSendToPlugin unmarshal", err)
	}

	if event.Action == dialAction && p.handleDialSendToPlugin(event, payload) {
		return
	}

	// Handle settings action commands
	if p.isSettingsAction(event.Action, event.Context) || isSettingsPayload(payload) {
		targetContext := p.resolveSettingsContext(event.Context)
		// Check for settingsConnected
		if _, ok := payload["settingsConnected"]; ok {
			p.sendSettingsStatus("com.moeilijk.hwinfo.settings", targetContext)
			return
		}

		// Check for periodic settings status refresh
		if _, ok := payload["requestSettingsStatus"]; ok {
			p.sendSettingsStatus("com.moeilijk.hwinfo.settings", targetContext)
			return
		}

		// Check for setPollInterval
		if raw, ok := payload["setPollInterval"]; ok {
			var intervalMs int
			if err := json.Unmarshal(*raw, &intervalMs); err == nil && intervalMs > 0 {
				p.setPollInterval(intervalMs)
			}
			return
		}

		// Check for addGlobalThreshold
		if raw, ok := payload["addGlobalThreshold"]; ok {
			var name string
			_ = json.Unmarshal(*raw, &name)
			p.handleGlobalAddThreshold(name)
			return
		}

		// Check for deleteGlobalThreshold
		if raw, ok := payload["deleteGlobalThreshold"]; ok {
			var id string
			if err := json.Unmarshal(*raw, &id); err == nil {
				p.handleGlobalRemoveThreshold(id)
			}
			return
		}

		// Check for updateGlobalThreshold
		if raw, ok := payload["updateGlobalThreshold"]; ok {
			var upd struct {
				ID      string `json:"id"`
				Field   string `json:"field"`
				Value   string `json:"value"`
				Checked bool   `json:"checked"`
			}
			if err := json.Unmarshal(*raw, &upd); err == nil {
				p.handleGlobalThresholdUpdate(upd.ID, upd.Field, upd.Value, upd.Checked)
			}
			return
		}

		// Check for updateTileAppearance
		if raw, ok := payload["updateTileAppearance"]; ok {
			var appearance settingsTileSettings
			if err := json.Unmarshal(*raw, &appearance); err == nil {
				// Update stored settings
				if appearance.TileBackground == "" {
					appearance.TileBackground = "#000000"
				}
				if appearance.TileTextColor == "" {
					appearance.TileTextColor = "#ffffff"
				}
				p.mu.RLock()
				existing := p.settingsContexts[targetContext]
				p.mu.RUnlock()
				if existing != nil {
					if appearance.Title == "" {
						appearance.Title = existing.Title
					}
					if appearance.TitleColor == "" {
						appearance.TitleColor = existing.TitleColor
					}
					if appearance.ShowTitleInGraph == nil {
						appearance.ShowTitleInGraph = existing.ShowTitleInGraph
					}
				}
				if appearance.TitleColor == "" {
					appearance.TitleColor = "#b7b7b7"
				}
				if appearance.ShowTitleInGraph == nil {
					appearance.ShowTitleInGraph = boolPtr(true)
				}
				p.mu.Lock()
				p.settingsContexts[targetContext] = &appearance
				p.mu.Unlock()
				if err := p.sd.SetSettings(targetContext, &appearance); err != nil {
					log.Printf("updateTileAppearance SetSettings failed: %v\n", err)
				}
				p.updateSettingsTile(targetContext)
				p.sendSettingsStatus("com.moeilijk.hwinfo.settings", targetContext)
			}
			return
		}
	}

	if event.Action == derivedAction {
		if data, ok := payload["loadDerivedPreset"]; ok {
			var preset derivedPresetPayload
			if err := json.Unmarshal(*data, &preset); err != nil {
				log.Printf("derived loadDerivedPreset unmarshal: %v", err)
				return
			}
			p.handleDerivedLoadPreset(event, preset)
			return
		}
		if data, ok := payload["sdpi_collection"]; ok {
			sdpi := evSdpiCollection{}
			if err := json.Unmarshal(*data, &sdpi); err != nil {
				log.Printf("derived sdpi unmarshal: %v", err)
				return
			}
			switch sdpi.Key {
			case "derived_suppressGlobal":
				p.handleDerivedSuppressGlobal(event, &sdpi)
			case "derived_unsuppressGlobal":
				p.handleDerivedUnsuppressGlobal(event, &sdpi)
			case "derived_formula", "derived_slotCount", "derived_format", "derived_divisor",
				"derived_graphUnit", "derived_min", "derived_max",
				"derived_foregroundColor", "derived_backgroundColor", "derived_highlightColor",
				"derived_valueTextColor", "derived_titleColor", "derived_title",
				"derived_graphHeightPct", "derived_graphLineThickness", "derived_textStroke", "derived_textStrokeColor",
				"derived_updateIntervalOverrideMs", "derived_smoothingAlpha", "titleFontSize", "valueFontSize":
				p.handleDerivedGlobalField(event, &sdpi)
			case "allSlots_sensorSelect":
				p.handleDerivedAllSlotsSensor(event, &sdpi)
			default:
				slotIdx, field := parseCompositeSlotKey(sdpi.Key) // reuse — zelfde slot{N}_{field} patroon
				if slotIdx < 0 {
					log.Printf("derived unknown sdpi key: %s", sdpi.Key)
					return
				}
				switch field {
				case "sensorSelect":
					p.handleDerivedSlotSensorSelect(event, &sdpi, slotIdx)
				case "readingSelect":
					p.handleDerivedSlotReadingSelect(event, &sdpi, slotIdx)
				case "applyFavorite":
					p.handleDerivedSlotApplyFavorite(event, &sdpi, slotIdx)
				default:
					p.handleDerivedSlotField(event, &sdpi, slotIdx, field)
				}
			}
		}
		return
	}

	if event.Action == compositeAction {
		if data, ok := payload["sdpi_collection"]; ok {
			sdpi := evSdpiCollection{}
			if err := json.Unmarshal(*data, &sdpi); err != nil {
				log.Printf("composite sdpi unmarshal: %v", err)
				return
			}
			switch sdpi.Key {
			case "composite_mode", "composite_slotCount", "updateIntervalOverrideMs", "smoothingAlpha":
				p.handleCompositeGlobalField(event, &sdpi)
			default:
				slotIdx, field := parseCompositeSlotKey(sdpi.Key)
				if slotIdx < 0 {
					log.Printf("composite unknown sdpi key: %s", sdpi.Key)
					return
				}
				switch field {
				case "sensorSelect":
					p.handleCompositeSlotSensorSelect(event, &sdpi, slotIdx)
				case "readingSelect":
					p.handleCompositeSlotReadingSelect(event, &sdpi, slotIdx)
				case "suppressGlobal":
					p.handleCompositeSlotSuppressGlobal(event, &sdpi, slotIdx)
				case "unsuppressGlobal":
					p.handleCompositeSlotUnsuppressGlobal(event, &sdpi, slotIdx)
				case "addThreshold":
					p.handleCompositeAddThreshold(event, &sdpi, slotIdx)
				case "removeThreshold":
					p.handleCompositeRemoveThreshold(event, &sdpi, slotIdx)
				case "reorderThreshold":
					p.handleCompositeReorderThreshold(event, &sdpi, slotIdx)
				case "thresholdEnabled", "thresholdName",
					"thresholdOperator", "thresholdValue", "thresholdHysteresis", "thresholdDwellMs",
					"thresholdCooldownMs", "thresholdSticky", "thresholdText", "thresholdTextColor",
					"thresholdBackgroundColor", "thresholdForegroundColor",
					"thresholdHighlightColor", "thresholdValueTextColor":
					p.handleCompositeThresholdUpdate(event, &sdpi, slotIdx)
				default:
					p.handleCompositeSlotField(event, &sdpi, slotIdx, field)
				}
			}
		}
		return
	}

	if data, ok := payload["sdpi_collection"]; ok {
		sdpi := evSdpiCollection{}
		err = json.Unmarshal(*data, &sdpi)
		if err != nil {
			log.Println("SDPI unmarshal", err)
		}
		switch sdpi.Key {
		case "sensorSelect":
			err = p.handleSensorSelect(event, &sdpi)
			if err != nil {
				log.Println("handleSensorSelect", err)
			}
		case "toggleFavoriteCurrent":
			settings, getErr := p.am.getSettings(event.Context)
			if getErr != nil {
				log.Println("toggleFavoriteCurrent getSettings", getErr)
				break
			}
			err = p.toggleFavoriteSelection(event.Action, event.Context, &settings)
			if err != nil {
				log.Println("toggleFavoriteCurrent", err)
			}
		case "applyFavorite":
			err = p.handleApplyFavorite(event, &sdpi)
			if err != nil {
				log.Println("handleApplyFavorite", err)
			}
		case "removeFavorite":
			settings, getErr := p.am.getSettings(event.Context)
			if getErr != nil {
				log.Println("removeFavorite getSettings", getErr)
				break
			}
			err = p.removeFavorite(event.Action, event.Context, &settings, sdpi.Value)
			if err != nil {
				log.Println("removeFavorite", err)
			}
		case "readingSelect":
			err = p.handleReadingSelect(event, &sdpi)
			if err != nil {
				log.Println("handleReadingSelect", err)
			}
		case "min":
			err := p.handleSetMin(event, &sdpi)
			if err != nil {
				log.Println("handleSetMin", err)
			}
		case "max":
			err := p.handleSetMax(event, &sdpi)
			if err != nil {
				log.Println("handleSetMax", err)
			}
		case "format":
			err := p.handleSetFormat(event, &sdpi)
			if err != nil {
				log.Println("handleSetFormat", err)
			}
		case "divisor":
			err := p.handleDivisor(event, &sdpi)
			if err != nil {
				log.Println("handleDivisor", err)
			}
		case "graphUnit":
			err := p.handleSetGraphUnit(event, &sdpi)
			if err != nil {
				log.Println("handleSetGraphUnit", err)
			}
		case "snoozeDurations":
			err := p.handleSnoozeDurations(event, &sdpi)
			if err != nil {
				log.Println("handleSnoozeDurations", err)
			}
		case "graphMode":
			settings, getErr := p.am.getSettings(event.Context)
			if getErr != nil {
				log.Println("graphMode getSettings", getErr)
				break
			}
			settings.GraphMode = sdpi.Value
			if err2 := p.sd.SetSettings(event.Context, &settings); err2 != nil {
				log.Println("graphMode SetSettings", err2)
				break
			}
			p.am.SetAction(event.Action, event.Context, &settings)
		case "foreground", "background", "highlight", "valuetext":
			err := p.handleColorChange(event, sdpi.Key, &sdpi)
			if err != nil {
				log.Println("handleColorChange", err)
			}
		case "titleFontSize", "valueFontSize":
			err := p.handleSetFontSize(event, sdpi.Key, &sdpi)
			if err != nil {
				log.Println("handleSetTitleFontSize", err)
			}
		case "graphHeightPct", "graphLineThickness", "textStroke", "textStrokeColor", "updateIntervalOverrideMs", "smoothingAlpha":
			err := p.handleGraphVisuals(event, &sdpi)
			if err != nil {
				log.Println("handleGraphVisuals", err)
			}
		case "warningEnabled":
			err := p.handleWarningEnabled(event, &sdpi)
			if err != nil {
				log.Println("handleWarningEnabled", err)
			}
		case "criticalEnabled":
			err := p.handleCriticalEnabled(event, &sdpi)
			if err != nil {
				log.Println("handleCriticalEnabled", err)
			}
		case "warningValue":
			err := p.handleWarningValue(event, &sdpi)
			if err != nil {
				log.Println("handleWarningValue", err)
			}
		case "criticalValue":
			err := p.handleCriticalValue(event, &sdpi)
			if err != nil {
				log.Println("handleCriticalValue", err)
			}
		case "warningOperator":
			err := p.handleWarningOperator(event, &sdpi)
			if err != nil {
				log.Println("handleWarningOperator", err)
			}
		case "criticalOperator":
			err := p.handleCriticalOperator(event, &sdpi)
			if err != nil {
				log.Println("handleCriticalOperator", err)
			}
		case "warningBackground", "warningForeground", "warningHighlight", "warningValuetext",
			"criticalBackground", "criticalForeground", "criticalHighlight", "criticalValuetext":
			err := p.handleColorChange(event, sdpi.Key, &sdpi)
			if err != nil {
				log.Println("handleColorChange (threshold)", err)
			}
		// Global threshold suppress handlers
		case "suppressGlobal":
			if err := p.handleSuppressGlobal(event, &sdpi); err != nil {
				log.Println("handleSuppressGlobal", err)
			}
		case "unsuppressGlobal":
			if err := p.handleUnsuppressGlobal(event, &sdpi); err != nil {
				log.Println("handleUnsuppressGlobal", err)
			}
		// Dynamic threshold handlers
		case "addThreshold":
			err := p.handleAddThreshold(event, &sdpi)
			if err != nil {
				log.Println("handleAddThreshold", err)
			}
		case "removeThreshold":
			err := p.handleRemoveThreshold(event, &sdpi)
			if err != nil {
				log.Println("handleRemoveThreshold", err)
			}
		case "reorderThreshold":
			err := p.handleReorderThreshold(event, &sdpi)
			if err != nil {
				log.Println("handleReorderThreshold", err)
			}
		case "thresholdEnabled", "thresholdName",
			"thresholdOperator", "thresholdValue", "thresholdHysteresis", "thresholdDwellMs",
			"thresholdCooldownMs", "thresholdSticky", "thresholdText", "thresholdTextColor",
			"thresholdBackgroundColor", "thresholdForegroundColor",
			"thresholdHighlightColor", "thresholdValueTextColor":
			err := p.handleThresholdUpdate(event, &sdpi)
			if err != nil {
				log.Println("handleThresholdUpdate", err)
			}
		default:
			log.Printf("Unknown sdpi key: %s\n", sdpi.Key)
		}
		return
	}
}

// OnDidReceiveSettings handles action settings updates persisted by Stream Deck.
func (p *Plugin) OnDidReceiveSettings(event *streamdeck.EvDidReceiveSettings) {
	if !p.isSettingsAction(event.Action, event.Context) {
		return
	}
	if event.Payload.Settings == nil {
		return
	}

	var rawSettings map[string]json.RawMessage
	hasShowLabel := false
	hasTitle := false
	hasTitleColor := false
	hasShowTitleInGraph := false
	if err := json.Unmarshal(*event.Payload.Settings, &rawSettings); err == nil {
		_, hasShowLabel = rawSettings["showLabel"]
		_, hasTitle = rawSettings["title"]
		_, hasTitleColor = rawSettings["titleColor"]
		_, hasShowTitleInGraph = rawSettings["showTitleInGraph"]
	}

	var tileSettings settingsTileSettings
	if err := json.Unmarshal(*event.Payload.Settings, &tileSettings); err != nil {
		log.Printf("OnDidReceiveSettings settings tile unmarshal: %v\n", err)
		return
	}

	if tileSettings.TileBackground == "" {
		tileSettings.TileBackground = "#000000"
	}
	if tileSettings.TileTextColor == "" {
		tileSettings.TileTextColor = "#ffffff"
	}
	if !hasShowLabel {
		tileSettings.ShowLabel = true
	}
	p.mu.RLock()
	existing := p.settingsContexts[event.Context]
	p.mu.RUnlock()
	if existing != nil {
		if !hasTitle {
			tileSettings.Title = existing.Title
		}
		if !hasTitleColor {
			tileSettings.TitleColor = existing.TitleColor
		}
		if !hasShowTitleInGraph {
			tileSettings.ShowTitleInGraph = existing.ShowTitleInGraph
		}
	}
	if tileSettings.TitleColor == "" {
		tileSettings.TitleColor = "#b7b7b7"
	}
	if tileSettings.ShowTitleInGraph == nil {
		tileSettings.ShowTitleInGraph = boolPtr(true)
	}

	p.mu.Lock()
	p.settingsContexts[event.Context] = &tileSettings
	p.mu.Unlock()
	// Repair partial payloads (e.g. PI sending only color/showLabel) so title fields persist.
	if !hasShowLabel || !hasTitle || !hasTitleColor || !hasShowTitleInGraph {
		if err := p.sd.SetSettings(event.Context, &tileSettings); err != nil {
			log.Printf("OnDidReceiveSettings repair SetSettings failed: %v\n", err)
		}
	}
	p.updateSettingsTile(event.Context)
	p.sendSettingsStatus("com.moeilijk.hwinfo.settings", event.Context)
}

// OnDidReceiveGlobalSettings handles global settings from Stream Deck
func (p *Plugin) OnDidReceiveGlobalSettings(event *streamdeck.EvDidReceiveGlobalSettings) {
	if event.Payload.Settings == nil {
		return
	}

	var gs globalSettings
	if err := json.Unmarshal(*event.Payload.Settings, &gs); err != nil {
		log.Printf("OnDidReceiveGlobalSettings unmarshal failed: %v\n", err)
		return
	}

	if gs.PollInterval <= 0 {
		gs.PollInterval = int(defaultPollInterval.Milliseconds())
	}
	if gs.PollInterval < 250 {
		gs.PollInterval = 250
	}
	if gs.PollInterval > 10000 {
		gs.PollInterval = 10000
	}

	p.mu.Lock()
	intervalChanged := gs.PollInterval != p.globalSettings.PollInterval
	p.globalSettings = gs
	if intervalChanged {
		p.pollTimeCacheTTL = pollTimeCacheTTLForInterval(time.Duration(gs.PollInterval) * time.Millisecond)
	}
	p.mu.Unlock()

	if intervalChanged {
		interval := time.Duration(gs.PollInterval) * time.Millisecond
		p.am.SetInterval(interval)
	}

	p.updateAllSettingsTiles()
}
