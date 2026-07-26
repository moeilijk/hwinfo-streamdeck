package hwinfostreamdeckplugin

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"runtime"

	"github.com/hashicorp/go-plugin"
	"github.com/moeilijk/hwinfo-streamdeck/pkg/graph"
	hwsensorsservice "github.com/moeilijk/hwinfo-streamdeck/pkg/service"
	"github.com/moeilijk/hwinfo-streamdeck/pkg/streamdeck"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// bridgeRuntime holds the HWiNFO bridge process and gRPC client.
// Poll time cache is protected by Plugin.mu; c/hw/peg are protected by the runtime's own mu.
type bridgeRuntime struct {
	mu  sync.RWMutex
	c   *plugin.Client
	hw  hwsensorsservice.HardwareService
	peg processExitGroup
	// poll time cache — accessed under Plugin.mu
	cachedPollTime uint64
	cachedAt       time.Time
}

// Plugin handles information between HWiNFO and Stream Deck
type Plugin struct {
	mu sync.RWMutex // protects maps and cached state below

	bridge *bridgeRuntime
	sd     *streamdeck.StreamDeck
	am     *actionManager
	graphs map[string]*graph.Graph

	// Cached assets and state for performance
	placeholderImage []byte               // cached startup chip placeholder image (set once at init, read-only after)
	lastPollTime     map[string]uint64    // last processed PollTime per context
	lastRenderTime   map[string]time.Time // wall-time of last render per context (for per-tile interval override)
	smoothedValues   map[string]float64   // last smoothed graph value per context (for EMA)
	divisorCache     map[string]divisorCacheEntry
	thresholdStates  map[string]map[string]*thresholdRuntimeState
	thresholdSnoozes map[string]*thresholdSnoozeState
	thresholdDirty   map[string]bool

	// Global settings
	globalSettings   globalSettings                   // plugin-wide settings (poll interval)
	pollTimeCacheTTL time.Duration                    // cache TTL (matches poll interval)
	settingsContexts map[string]*settingsTileSettings // tracks settings action contexts with appearance

	// Composite tile state
	compositeSettings map[string]*compositeActionSettings
	compositeStates   map[string]*compositeState

	// Derived metric tile state
	derivedSettings map[string]*derivedActionSettings
	derivedStates   map[string]*derivedState

	// Stream Deck+ dial carousel state
	dialSettings map[string]*dialActionSettings
	dialStates   map[string]*dialState
}

type sensorResult struct {
	sensors []hwsensorsservice.Sensor
	err     error
}

type divisorCacheEntry struct {
	raw   string
	value float64
}

const defaultPollInterval = time.Second
const settingsTitleFontSize = 9.0
const defaultThresholdHysteresis = 1.0
const defaultThresholdDwellMs = int(defaultPollInterval / time.Millisecond)
const defaultThresholdCooldownMs = int((5 * defaultPollInterval) / time.Millisecond)

func pollTimeCacheTTLForInterval(interval time.Duration) time.Duration {
	if interval <= 0 {
		interval = defaultPollInterval
	}

	// Keep cache short enough so each ticker cycle fetches fresh PollTime,
	// while still allowing one shared value across actions in the same tick.
	ttl := interval / 2
	if ttl < 100*time.Millisecond {
		ttl = 100 * time.Millisecond
	}
	if ttl >= interval {
		ttl = interval - (25 * time.Millisecond)
		if ttl < 25*time.Millisecond {
			ttl = 25 * time.Millisecond
		}
	}
	return ttl
}

// bridgeBinaryName returns the sensor bridge binary. Windows runs the real
// HWiNFO shared-memory bridge; other platforms exist only for the test
// harness, which ships a mock bridge serving the same gRPC interface.
func bridgeBinaryName() string {
	if runtime.GOOS == "windows" {
		return "./hwinfo-bridge.exe"
	}
	return "./mock-bridge"
}

// startBridgeClientLocked starts the sensor bridge. Caller must hold rt.mu write lock.
func (p *Plugin) startBridgeClientLocked(rt *bridgeRuntime) error {
	cmd := exec.Command(bridgeBinaryName())

	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig:  hwsensorsservice.Handshake,
		Plugins:          hwsensorsservice.PluginMap,
		Cmd:              cmd,
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
		AutoMTLS:         true,
	})

	rpcClient, err := client.Client()
	if err != nil {
		return err
	}

	g, err := newProcessExitGroup()
	if err == nil {
		if attachProcessToJob(g, cmd.Process) == nil {
			rt.peg = g
		}
	}

	raw, err := rpcClient.Dispense("hwinfoplugin")
	if err != nil {
		return err
	}

	rt.c = client
	rt.hw = raw.(hwsensorsservice.HardwareService)
	return nil
}

// restartBridge kills and restarts the sensor bridge.
func (p *Plugin) restartBridge(rt *bridgeRuntime) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.c != nil {
		rt.c.Kill()
	}
	_ = p.startBridgeClientLocked(rt)
}

// sensorsWithTimeout fetches sensors from the bridge.
func (p *Plugin) sensorsWithTimeout(d time.Duration) ([]hwsensorsservice.Sensor, error) {
	rt := p.bridge
	rt.mu.RLock()
	hw := rt.hw
	rt.mu.RUnlock()
	if hw == nil {
		return nil, fmt.Errorf("HWiNFO bridge not ready")
	}
	ch := make(chan sensorResult, 1)
	go func() {
		s, err := hw.Sensors()
		ch <- sensorResult{sensors: s, err: err}
	}()
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case res := <-ch:
		return res.sensors, res.err
	case <-timer.C:
		return nil, fmt.Errorf("sensors timeout")
	}
}

// getReading fetches a reading from the bridge.
func (p *Plugin) getReading(suid string, rid int32) (hwsensorsservice.Reading, []hwsensorsservice.Reading, error) {
	rt := p.bridge
	rt.mu.RLock()
	hw := rt.hw
	rt.mu.RUnlock()
	if hw == nil {
		return nil, nil, fmt.Errorf("HWiNFO bridge not ready")
	}
	rbs, err := hw.ReadingsForSensorID(suid)
	if err != nil {
		return nil, nil, fmt.Errorf("getReading ReadingsBySensor failed: %v", err)
	}
	for _, r := range rbs {
		if r.ID() == rid {
			return r, rbs, nil
		}
	}
	return nil, rbs, fmt.Errorf("ReadingID does not exist: %s", suid)
}

// getCachedPollTime returns the cached poll time from the bridge.
func (p *Plugin) getCachedPollTime() (uint64, error) {
	rt := p.bridge
	rt.mu.RLock()
	hw := rt.hw
	rt.mu.RUnlock()
	if hw == nil {
		return 0, fmt.Errorf("HWiNFO bridge not ready")
	}

	p.mu.RLock()
	cacheTTL := p.pollTimeCacheTTL
	if cacheTTL == 0 {
		cacheTTL = defaultPollInterval
	}
	if !rt.cachedAt.IsZero() && time.Since(rt.cachedAt) < cacheTTL {
		pt := rt.cachedPollTime
		p.mu.RUnlock()
		return pt, nil
	}
	p.mu.RUnlock()

	pollTime, err := hw.PollTime()

	p.mu.Lock()
	if err != nil {
		rt.cachedPollTime = 0
		rt.cachedAt = time.Now()
		p.mu.Unlock()
		return 0, err
	}
	rt.cachedPollTime = pollTime
	rt.cachedAt = time.Now()
	p.mu.Unlock()

	return pollTime, nil
}

// invalidatePollCacheForRuntime clears the poll time cache.
// Caller must hold p.mu write lock.
func invalidatePollCacheForRuntime(rt *bridgeRuntime) {
	rt.cachedPollTime = 0
	rt.cachedAt = time.Time{}
}

// startBridgeClient acquires rt.mu and starts the bridge.
func (p *Plugin) startBridgeClient(rt *bridgeRuntime) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return p.startBridgeClientLocked(rt)
}

// NewPlugin creates an instance and initializes the plugin
func NewPlugin(port, uuid, event, info string) (*Plugin, error) {
	p := &Plugin{
		am:                newActionManager(defaultPollInterval),
		bridge:            &bridgeRuntime{},
		graphs:            make(map[string]*graph.Graph),
		lastPollTime:      make(map[string]uint64),
		lastRenderTime:    make(map[string]time.Time),
		smoothedValues:    make(map[string]float64),
		divisorCache:      make(map[string]divisorCacheEntry),
		thresholdStates:   make(map[string]map[string]*thresholdRuntimeState),
		thresholdSnoozes:  make(map[string]*thresholdSnoozeState),
		thresholdDirty:    make(map[string]bool),
		pollTimeCacheTTL:  pollTimeCacheTTLForInterval(defaultPollInterval),
		settingsContexts:  make(map[string]*settingsTileSettings),
		compositeSettings: make(map[string]*compositeActionSettings),
		compositeStates:   make(map[string]*compositeState),
		derivedSettings:   make(map[string]*derivedActionSettings),
		derivedStates:     make(map[string]*derivedState),
		dialSettings:      make(map[string]*dialActionSettings),
		dialStates:        make(map[string]*dialState),
	}

	// Cache placeholder image at startup.
	// Preferred source is settings/default tile art (startup chip).
	for _, candidate := range []string{"./settingsImage.png", "./defaultImage.png", "./launch-hwinfo.png"} {
		if bts, err := os.ReadFile(candidate); err == nil {
			p.placeholderImage = bts
			log.Printf("Cached settings placeholder image from %s\n", candidate)
			break
		}
	}
	if len(p.placeholderImage) == 0 {
		log.Printf("Warning: could not cache placeholder image\n")
	}

	// Bridge starts are deferred until global settings arrive (OnDidReceiveGlobalSettings).
	// NewPlugin does not start any bridge here.
	p.sd = streamdeck.NewStreamDeck(port, uuid, event, info)
	return p, nil
}

// RunForever starts the plugin and waits for events, indefinitely
func (p *Plugin) RunForever() error {
	defer func() {
		rt := p.bridge
		rt.mu.Lock()
		if rt.c != nil {
			rt.c.Kill()
		}
		if rt.peg != nil {
			_ = rt.peg.dispose()
		}
		rt.mu.Unlock()
	}()

	p.sd.SetDelegate(p)
	p.am.Run(p.updateTiles, p.updateAuxTiles)

	// Watch-dog: start the bridge and restart it when it exits.
	go func() {
		for {
			rt := p.bridge
			rt.mu.RLock()
			needsStart := rt.hw == nil
			needsRestart := !needsStart && rt.c != nil && rt.c.Exited()
			rt.mu.RUnlock()

			if needsStart || needsRestart {
				if err := p.startBridgeClient(rt); err != nil {
					log.Printf("startBridgeClient failed: %v\n", err)
				}
			}
			time.Sleep(1 * time.Second)
		}
	}()

	err := p.sd.Connect()
	if err != nil {
		return fmt.Errorf("StreamDeck Connect: %v", err)
	}
	defer p.sd.Close()
	p.sd.ListenAndWait()
	return nil
}

func (p *Plugin) applyDefaultFormatValueOnly(v float64, t hwsensorsservice.ReadingType) string {
	switch t {
	case hwsensorsservice.ReadingTypeNone:
		return fmt.Sprintf("%0.f", v)
	case hwsensorsservice.ReadingTypeTemp:
		return fmt.Sprintf("%.0f", v)
	case hwsensorsservice.ReadingTypeVolt:
		return fmt.Sprintf("%.0f", v)
	case hwsensorsservice.ReadingTypeFan:
		return fmt.Sprintf("%.0f", v)
	case hwsensorsservice.ReadingTypeCurrent:
		return fmt.Sprintf("%.0f", v)
	case hwsensorsservice.ReadingTypePower:
		return fmt.Sprintf("%0.f", v)
	case hwsensorsservice.ReadingTypeClock:
		return fmt.Sprintf("%.0f", v)
	case hwsensorsservice.ReadingTypeUsage:
		return fmt.Sprintf("%.0f", v)
	case hwsensorsservice.ReadingTypeOther:
		return fmt.Sprintf("%.0f", v)
	}
	return "Bad Format"
}

// normalizeForGraph converts data size values to the target unit for consistent graph scaling.
// This prevents jumps when the source switches units (e.g., 1000 KB/s → 1 MB/s).
// targetUnit can be: "B", "KB", "MB", "GB", "TB" or empty (no normalization).
func (p *Plugin) normalizeForGraph(value float64, sourceUnit string, targetUnit string) float64 {
	if targetUnit == "" {
		return value // no normalization
	}

	// Convert source value to bytes first
	sourceLower := strings.ToLower(sourceUnit)
	var bytes float64
	switch {
	case strings.HasPrefix(sourceLower, "tb") || strings.HasPrefix(sourceLower, "tib"):
		bytes = value * 1024 * 1024 * 1024 * 1024
	case strings.HasPrefix(sourceLower, "gb") || strings.HasPrefix(sourceLower, "gib"):
		bytes = value * 1024 * 1024 * 1024
	case strings.HasPrefix(sourceLower, "mb") || strings.HasPrefix(sourceLower, "mib"):
		bytes = value * 1024 * 1024
	case strings.HasPrefix(sourceLower, "kb") || strings.HasPrefix(sourceLower, "kib"):
		bytes = value * 1024
	case strings.HasPrefix(sourceLower, "b/") || sourceLower == "b":
		bytes = value
	default:
		return value // not a data size unit, no conversion
	}

	// Convert bytes to target unit
	switch strings.ToUpper(targetUnit) {
	case "TB":
		return bytes / (1024 * 1024 * 1024 * 1024)
	case "GB":
		return bytes / (1024 * 1024 * 1024)
	case "MB":
		return bytes / (1024 * 1024)
	case "KB":
		return bytes / 1024
	case "B":
		return bytes
	default:
		return value
	}
}

// formatDisplayValue formats a numeric value and unit into display strings.
// Returns (valueTextNoUnit, displayText).
func (p *Plugin) formatDisplayValue(v float64, displayUnit, format string, readingType hwsensorsservice.ReadingType) (string, string) {
	valueTextNoUnit := ""
	displayText := ""
	if format != "" {
		result := fmt.Sprintf(format, v)
		if strings.Contains(result, "%!") {
			valueTextNoUnit = p.applyDefaultFormatValueOnly(v, readingType)
		} else {
			valueTextNoUnit = result
		}
		displayText = valueTextNoUnit
	} else {
		valueTextNoUnit = p.applyDefaultFormatValueOnly(v, readingType)
		if displayUnit != "" {
			if displayUnit == "%" {
				displayText = valueTextNoUnit + displayUnit
			} else {
				displayText = valueTextNoUnit + " " + displayUnit
			}
		} else {
			displayText = valueTextNoUnit
		}
	}
	return valueTextNoUnit, displayText
}

func (p *Plugin) getCachedDivisor(context, raw string) (float64, error) {
	if raw == "" {
		return 1, nil
	}
	p.mu.RLock()
	if entry, ok := p.divisorCache[context]; ok && entry.raw == raw {
		p.mu.RUnlock()
		return entry.value, nil
	}
	p.mu.RUnlock()

	fdiv, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, err
	}
	p.mu.Lock()
	p.divisorCache[context] = divisorCacheEntry{raw: raw, value: fdiv}
	p.mu.Unlock()
	return fdiv, nil
}

func (p *Plugin) updateTiles(data *actionData) {
	if data.action != "com.moeilijk.hwinfo.reading" {
		log.Printf("Unknown action updateTiles: %s\n", data.action)
		return
	}

	p.mu.RLock()
	g, ok := p.graphs[data.context]
	p.mu.RUnlock()
	if !ok {
		log.Printf("Graph not found for context: %s\n", data.context)
		return
	}

	showUnavailable := func() {
		if !data.settings.InErrorState {
			payload := evStatus{Error: true, Message: "HWiNFO Unavailable"}
			err := p.sd.SendToPropertyInspector("com.moeilijk.hwinfo.reading", data.context, payload)
			if err != nil {
				log.Println("updateTiles SendToPropertyInspector", err)
			}
			data.settings.InErrorState = true
			p.sd.SetSettings(data.context, &data.settings)

			// Only set image on state transition (optimization #2)
			if len(p.placeholderImage) > 0 {
				if err := p.sd.SetImage(data.context, p.placeholderImage); err != nil {
					log.Printf("Failed to setImage: %v\n", err)
				}
			}
		}
		// Clear lastPollTime so we re-render when HWiNFO comes back
		p.mu.Lock()
		delete(p.lastPollTime, data.context)
		p.mu.Unlock()
	}

	// show ui on property inspector if in error state
	if data.settings.InErrorState {
		payload := evStatus{Error: false, Message: "show_ui"}
		err := p.sd.SendToPropertyInspector("com.moeilijk.hwinfo.reading", data.context, payload)
		if err != nil {
			log.Println("updateTiles SendToPropertyInspector", err)
		}
		data.settings.InErrorState = false
		p.sd.SetSettings(data.context, &data.settings)
	}

	s := data.settings
	forceUpdate := p.consumeThresholdDirty(data.context)

	pollTime, err := p.getCachedPollTime()
	if err != nil {
		log.Printf("PollTime failed: %v\n", err)
		showUnavailable()
		return
	}
	if pollTime == 0 || time.Since(time.Unix(0, int64(pollTime))) > 5*time.Second {
		showUnavailable()
		return
	}

	if !forceUpdate {
		p.mu.RLock()
		last, ok := p.lastPollTime[data.context]
		p.mu.RUnlock()
		if ok && last == pollTime {
			return
		}
	}

	if override := s.UpdateIntervalOverrideMs; override > 0 && !forceUpdate {
		p.mu.RLock()
		lastRender := p.lastRenderTime[data.context]
		p.mu.RUnlock()
		if time.Since(lastRender) < time.Duration(override)*time.Millisecond {
			return
		}
	}

	r, readings, err := p.getReading(s.SensorUID, s.ReadingID)
	if err != nil {
		if s.ReadingLabel != "" {
			for _, candidate := range readings {
				if candidate.Label() == s.ReadingLabel {
					s.ReadingID = candidate.ID()
					r = candidate
					err = nil
					_ = p.sd.SetSettings(data.context, s)
					p.am.SetAction(data.action, data.context, s)
					break
				}
			}
		}
		if err != nil {
			log.Printf("getReading failed: %v\n", err)
			showUnavailable()
			return
		}
	}
	if s.ShowTitleInGraph != nil && *s.ShowTitleInGraph && s.Title == "" {
		g.SetLabelText(0, r.Label())
	}

	v := r.Value()
	divisor, err := p.getCachedDivisor(data.context, s.Divisor)
	if err != nil {
		log.Printf("Failed to parse float: %s\n", s.Divisor)
		return
	}
	if divisor != 1 {
		v = r.Value() / divisor
	}

	// Normalize the graph value to handle unit changes (e.g., KB/s → MB/s)
	// Only applies to throughput readings (units containing "/s")
	graphValue := r.Value()
	if s.GraphUnit != "" && strings.Contains(r.Unit(), "/s") {
		graphValue = p.normalizeForGraph(r.Value(), r.Unit(), s.GraphUnit)
	}
	if divisor != 1 {
		graphValue = graphValue / divisor
	}

	// Determine display value and unit
	displayValue := v
	displayUnit := r.Unit()
	if s.GraphUnit != "" && strings.Contains(r.Unit(), "/s") {
		// Convert display value to match GraphUnit
		displayValue = p.normalizeForGraph(v, r.Unit(), s.GraphUnit)
		displayUnit = s.GraphUnit + "/s"
	}

	// EMA smoothing — threshold eval uses raw v; smoothing applies to graph and display values
	if alpha := s.SmoothingAlpha; alpha > 0 && alpha < 1.0 {
		p.mu.Lock()
		prev, ok := p.smoothedValues[data.context]
		if !ok {
			prev = graphValue
		}
		smoothed := emaSmooth(alpha, graphValue, prev)
		p.smoothedValues[data.context] = smoothed
		p.mu.Unlock()
		if graphValue != 0 {
			ratio := smoothed / graphValue
			graphValue = smoothed
			displayValue *= ratio
		}
	}

	valueTextNoUnit, displayText := p.formatDisplayValue(displayValue, displayUnit, s.Format, hwsensorsservice.ReadingType(r.TypeI()))

	now := time.Now()

	// Check threshold alerts (evaluate by priority, highest first)
	thresholds := p.resolveThresholdsForEval(s.Thresholds, s.SuppressedGlobalIDs, hwsensorsservice.ReadingType(r.TypeI()))
	activeThreshold := p.evaluateThresholds(data.context, v, thresholds, now)

	newThresholdID := ""
	alertText := ""
	if activeThreshold != nil {
		newThresholdID = activeThreshold.ID
		if activeThreshold.Text != "" {
			alertText = p.applyThresholdText(activeThreshold.Text, valueTextNoUnit, displayUnit)
		}
	}

	snoozeState, snoozed, snoozeChanged := p.currentThresholdSnoozeState(data.context, now)
	if activeThreshold == nil {
		if p.clearThresholdSnooze(data.context) {
			snoozeChanged = true
		}
		snoozed = false
		snoozeState = thresholdSnoozeState{}
	}
	if snoozeChanged {
		forceUpdate = true
	}

	// Apply colors before drawing so the current frame matches the active threshold state.
	if forceUpdate || newThresholdID != s.CurrentThresholdID {
		if activeThreshold != nil && !snoozed {
			p.applyThresholdColors(g, activeThreshold)
		} else {
			p.applyNormalColors(g, s)
		}
		s.CurrentThresholdID = newThresholdID
		p.am.SetAction(data.action, data.context, s)
		_ = p.sd.SetSettings(data.context, s)
	}

	renderDisplayText, renderAlertText, renderGraphValue, freezeGraph := p.resolveThresholdDisplay(
		data.context,
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
	switch s.GraphMode {
	case "text":
		g.Clear()
	default: // "both" or ""
		if !freezeGraph {
			g.Update(renderGraphValue)
		}
	case "graph":
		if !freezeGraph {
			g.Update(renderGraphValue)
		}
	}

	if s.GraphMode == "graph" {
		g.SetLabelText(0, "")
		g.SetLabelText(1, "")
		g.SetLabelText(2, "")
	} else {
		g.SetLabelText(1, renderDisplayText)
		if renderAlertText != "" {
			g.SetLabelText(2, renderAlertText)
		} else {
			g.SetLabelText(2, "")
		}
	}

	b, err := g.EncodePNG()
	if err != nil {
		log.Printf("Failed to encode graph: %v\n", err)
		return
	}

	err = p.sd.SetImage(data.context, b)
	if err != nil {
		log.Printf("Failed to setImage: %v\n", err)
		return
	}

	p.mu.Lock()
	p.lastPollTime[data.context] = pollTime
	if data.settings.UpdateIntervalOverrideMs > 0 {
		p.lastRenderTime[data.context] = time.Now()
	}
	p.mu.Unlock()
}

func (p *Plugin) refreshAction(action, context string) {
	settings, err := p.am.getSettings(context)
	if err != nil {
		log.Printf("refreshAction getSettings: %v\n", err)
		return
	}
	p.updateTiles(&actionData{
		action:   action,
		context:  context,
		settings: &settings,
	})
}

func (p *Plugin) updateAuxTiles() {
	p.updateCompositeTick()
	p.updateDerivedTick()
	p.updateDialTick()
}

func (p *Plugin) applyThresholdText(template, valueTextNoUnit, unit string) string {
	out := strings.ReplaceAll(template, "{value}", valueTextNoUnit)
	out = strings.ReplaceAll(out, "{unit}", unit)
	return out
}

// updateSettingsTile updates the settings tile with current interval and appearance
func (p *Plugin) updateSettingsTile(context string) {
	p.mu.RLock()
	intervalMs := p.globalSettings.PollInterval
	var tileSettings *settingsTileSettings
	if ts := p.settingsContexts[context]; ts != nil {
		cp := *ts
		tileSettings = &cp
	}
	p.mu.RUnlock()
	if intervalMs <= 0 {
		intervalMs = int(p.am.GetInterval().Milliseconds())
	}
	if tileSettings == nil {
		tileSettings = &settingsTileSettings{
			TileBackground:   "#000000",
			TileTextColor:    "#ffffff",
			ShowLabel:        true,
			Title:            "",
			TitleColor:       "#b7b7b7",
			ShowTitleInGraph: boolPtr(true),
		}
	}
	// Parse colors
	bgColor := hexToRGBA(tileSettings.TileBackground)
	if bgColor == nil {
		bgColor = &color.RGBA{0, 0, 0, 255}
	}
	textColor := hexToRGBA(tileSettings.TileTextColor)
	if textColor == nil {
		textColor = &color.RGBA{255, 255, 255, 255}
	}
	titleColor := hexToRGBA(tileSettings.TitleColor)
	if titleColor == nil {
		titleColor = &color.RGBA{183, 183, 183, 255}
	}
	drawTitle := true
	if tileSettings.ShowTitleInGraph != nil {
		drawTitle = *tileSettings.ShowTitleInGraph
	}
	renderedTitle := strings.TrimSpace(tileSettings.Title)
	if renderedTitle == "" {
		renderedTitle = "Refresh Rate"
	}

	// The settings tile uses in-image text for title/value.
	// Keep native title empty to avoid duplicate/misaligned text.
	if err := p.sd.SetTitle(context, ""); err != nil {
		log.Printf("updateSettingsTile SetTitle failed: %v\n", err)
	}

	// For settings tile, ShowLabel toggles placeholder background on/off.
	// true  -> startup placeholder background + current interval
	// false -> user-selected solid background + current interval
	if tileSettings.ShowLabel {
		if img, err := p.renderSettingsPlaceholderTile(intervalMs, renderedTitle, drawTitle, titleColor, textColor); err == nil {
			if err := p.sd.SetImage(context, img); err != nil {
				log.Printf("updateSettingsTile SetImage failed: %v\n", err)
			}
			return
		}
		log.Printf("updateSettingsTile placeholder render failed, falling back to solid background\n")
	}

	// Create a graph just for rendering the tile
	g := graph.NewGraph(tileWidth, tileHeight, 0, 100, bgColor, bgColor, bgColor)

	// Render title + value in the image, aligned like graph tiles.
	titleText := ""
	if drawTitle {
		titleText = renderedTitle
	}
	g.SetLabel(0, titleText, 19, titleColor)
	g.SetLabelFontSize(0, settingsTitleFontSize)
	g.SetLabel(1, fmt.Sprintf("%dms", intervalMs), 44, textColor)
	g.SetLabelFontSize(1, 10.5)

	// Render and set image
	g.Update(0) // Initialize the graph
	b, err := g.EncodePNG()
	if err != nil {
		log.Printf("updateSettingsTile EncodePNG failed: %v\n", err)
		return
	}
	if err := p.sd.SetImage(context, b); err != nil {
		log.Printf("updateSettingsTile SetImage failed: %v\n", err)
	}

}

func (p *Plugin) renderSettingsPlaceholderTile(intervalMs int, title string, drawTitle bool, titleColor, textColor *color.RGBA) ([]byte, error) {
	if len(p.placeholderImage) == 0 {
		return nil, fmt.Errorf("placeholder image not cached")
	}

	base, err := png.Decode(bytes.NewReader(p.placeholderImage))
	if err != nil {
		return nil, fmt.Errorf("decode placeholder image: %w", err)
	}

	canvas := image.NewRGBA(image.Rect(0, 0, tileWidth, tileHeight))
	draw.Draw(canvas, canvas.Bounds(), base, image.Point{}, draw.Src)

	ffm := graph.GetSharedFontFaceManager()
	faceValue, err := ffm.GetFaceOfSize(10.5)
	if err != nil {
		return nil, fmt.Errorf("font value: %w", err)
	}
	faceTitle, err := ffm.GetFaceOfSize(settingsTitleFontSize)
	if err != nil {
		return nil, fmt.Errorf("font title: %w", err)
	}

	if drawTitle {
		drawCenteredText(canvas, faceTitle, titleColor, title, 19)
	}
	drawCenteredText(canvas, faceValue, textColor, fmt.Sprintf("%dms", intervalMs), 44)

	var out bytes.Buffer
	if err := png.Encode(&out, canvas); err != nil {
		return nil, fmt.Errorf("encode placeholder tile: %w", err)
	}
	return out.Bytes(), nil
}

func drawCenteredText(dst *image.RGBA, face font.Face, clr *color.RGBA, text string, baselineY int) {
	d := &font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(clr),
		Face: face,
	}
	textWidth := d.MeasureString(text).Round()
	x := (dst.Bounds().Dx() - textWidth) / 2
	if x < 0 {
		x = 0
	}
	d.Dot = fixed.P(x, baselineY)
	d.DrawString(text)
}

// updateAllSettingsTiles updates all settings tiles
func (p *Plugin) updateAllSettingsTiles() {
	p.mu.RLock()
	contexts := make([]string, 0, len(p.settingsContexts))
	for ctx := range p.settingsContexts {
		contexts = append(contexts, ctx)
	}
	p.mu.RUnlock()
	for _, ctx := range contexts {
		p.updateSettingsTile(ctx)
	}
}

// setPollInterval changes the polling interval dynamically
func (p *Plugin) setPollInterval(intervalMs int) {
	if intervalMs < 250 {
		intervalMs = 250
	}
	if intervalMs > 10000 {
		intervalMs = 10000
	}

	interval := time.Duration(intervalMs) * time.Millisecond

	// Update action manager ticker
	p.am.SetInterval(interval)

	// Update cache TTL and global settings under lock
	p.mu.Lock()
	p.pollTimeCacheTTL = pollTimeCacheTTLForInterval(interval)
	p.globalSettings.PollInterval = intervalMs
	gs := p.globalSettings
	p.mu.Unlock()

	// Update all settings tiles (reads globalSettings under its own lock)
	p.updateAllSettingsTiles()

	// Persist global settings (no lock needed — gs is a local copy)
	if err := p.sd.SetGlobalSettings(gs); err != nil {
		log.Printf("SetGlobalSettings failed: %v\n", err)
	}

	log.Printf("Poll interval changed to %v\n", interval)
}
