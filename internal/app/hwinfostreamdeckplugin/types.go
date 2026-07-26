package hwinfostreamdeckplugin

// globalSettings represents plugin-wide settings (not per-action)
type globalSettings struct {
	PollInterval     int               `json:"pollInterval"`               // milliseconds: 250..10000
	FavoriteReadings []favoriteReading `json:"favoriteReadings,omitempty"` // shared favorites for all tiles
	GlobalThresholds []Threshold       `json:"globalThresholds,omitempty"` // shared threshold library
}

// settingsTileSettings stores per-tile appearance settings for the settings action
type settingsTileSettings struct {
	TileBackground          string `json:"tileBackground"`                    // hex color for tile background
	TileTextColor           string `json:"tileTextColor"`                     // hex color for tile value text
	ShowLabel               bool   `json:"showLabel"`                         // toggles startup placeholder background
	Title                   string `json:"title"`                             // title text, like graph tiles
	TitleColor              string `json:"titleColor"`                        // title color, like graph tiles
	ShowTitleInGraph        *bool  `json:"showTitleInGraph"`                  // mirrors graph tile title behavior
}

// Threshold represents a single configurable threshold level
type Threshold struct {
	ID              string  `json:"id"`        // Unique identifier
	Name            string  `json:"name"`      // User-friendly name
	Text            string  `json:"text"`      // Optional alert text to display when triggered
	TextColor       string  `json:"textColor"` // Color for alert text
	Enabled         bool    `json:"enabled"`   // Is this threshold active?
	Operator        string  `json:"operator"`  // ">", "<", ">=", "<=", "=="
	Value           float64 `json:"value"`     // Threshold value
	Hysteresis      float64 `json:"hysteresis,omitempty"`
	DwellMs         int     `json:"dwellMs,omitempty"`
	CooldownMs      int     `json:"cooldownMs,omitempty"`
	Sticky          bool    `json:"sticky,omitempty"`
	BringToFront    bool    `json:"bringToFront,omitempty"` // dial: make this page the active one when the alarm fires / snooze times out
	BackgroundColor string  `json:"backgroundColor"`       // Background color when triggered
	ForegroundColor string  `json:"foregroundColor"`       // Graph foreground color
	HighlightColor  string  `json:"highlightColor"`        // Graph highlight color
	ValueTextColor  string  `json:"valueTextColor"`        // Value text color
	ReadingType     string  `json:"readingType,omitempty"` // globals only: "Temp","Volt","Fan","Current","Power","Clock","Usage","Other" or "" = all
}

type actionSettings struct {
	SensorUID                string  `json:"sensorUid"`
	ReadingID                int32   `json:"readingId,string"`
	ReadingLabel             string  `json:"readingLabel"`
	Title                    string  `json:"title"`
	TitleFontSize            float64 `json:"titleFontSize"`
	ValueFontSize            float64 `json:"valueFontSize"`
	ShowTitleInGraph         *bool   `json:"showTitleInGraph"`
	Min                      int     `json:"min"`
	Max                      int     `json:"max"`
	Format                   string  `json:"format"`
	Divisor                  string  `json:"divisor"`
	GraphUnit                string  `json:"graphUnit"` // B, KB, MB, GB, TB - normalizes graph values to this unit
	IsValid                  bool    `json:"isValid"`
	TitleColor               string  `json:"titleColor"`
	ForegroundColor          string  `json:"foregroundColor"`
	BackgroundColor          string  `json:"backgroundColor"`
	HighlightColor           string  `json:"highlightColor"`
	ValueTextColor           string  `json:"valueTextColor"`
	GraphMode                string  `json:"graphMode"`                // "both" (default), "graph", "text"
	GraphHeightPct           int     `json:"graphHeightPct"`           // 10–100; 0 = 100
	GraphLineThickness       int     `json:"graphLineThickness"`       // 1–4; 0 = 1
	TextStroke               bool    `json:"textStroke"`               // outline around labels
	TextStrokeColor          string  `json:"textStrokeColor"`          // hex; empty = use background color
	UpdateIntervalOverrideMs int     `json:"updateIntervalOverrideMs"` // 0 = follow global
	SmoothingAlpha           float64 `json:"smoothingAlpha"`           // 0.1–1.0; 0 = treat as 1.0 (no smoothing)
	InErrorState             bool    `json:"inErrorState"`

	// Dynamic threshold system
	Thresholds          []Threshold `json:"thresholds"`
	SuppressedGlobalIDs []string    `json:"suppressedGlobalIDs,omitempty"`
	CurrentThresholdID  string      `json:"currentThresholdId"`
	SnoozeDurations     []int       `json:"snoozeDurations,omitempty"`

	// Legacy Warning Threshold Settings (kept for migration, omitempty)
	WarningEnabled         bool    `json:"warningEnabled,omitempty"`
	WarningOperator        string  `json:"warningOperator,omitempty"`
	WarningValue           float64 `json:"warningValue,omitempty"`
	WarningBackgroundColor string  `json:"warningBackgroundColor,omitempty"`
	WarningForegroundColor string  `json:"warningForegroundColor,omitempty"`
	WarningHighlightColor  string  `json:"warningHighlightColor,omitempty"`
	WarningValueTextColor  string  `json:"warningValueTextColor,omitempty"`
	// Legacy Critical Threshold Settings (kept for migration, omitempty)
	CriticalEnabled         bool    `json:"criticalEnabled,omitempty"`
	CriticalOperator        string  `json:"criticalOperator,omitempty"`
	CriticalValue           float64 `json:"criticalValue,omitempty"`
	CriticalBackgroundColor string  `json:"criticalBackgroundColor,omitempty"`
	CriticalForegroundColor string  `json:"criticalForegroundColor,omitempty"`
	CriticalHighlightColor  string  `json:"criticalHighlightColor,omitempty"`
	CriticalValueTextColor  string  `json:"criticalValueTextColor,omitempty"`
	// Legacy alert state (kept for migration)
	CurrentAlertState string `json:"currentAlertState,omitempty"`
}

type actionData struct {
	action   string
	context  string
	settings *actionSettings
}

type favoriteReading struct {
	ID              string `json:"id"`
	SensorUID       string `json:"sensorUid"`
	SensorName      string `json:"sensorName"`
	ReadingID       int32  `json:"readingId,string"`
	ReadingLabel    string `json:"readingLabel"`
	ReadingUnit     string `json:"readingUnit"`
	Category        string `json:"category,omitempty"`
}

type evStatus struct {
	Error   bool   `json:"error"`
	Message string `json:"message"`
}

type evSendSensorsPayloadSensor struct {
	UID        string `json:"uid"`
	Name       string `json:"name"`
	Category   string `json:"category,omitempty"`
	SearchText string `json:"searchText,omitempty"`
	Source     string `json:"source,omitempty"`
}

type evSendSensorsPayload struct {
	Sensors  []*evSendSensorsPayloadSensor `json:"sensors"`
	Settings *actionSettings               `json:"settings"`
}

type evSendReadingsPayloadReading struct {
	ID         int32  `json:"id,string"`
	Label      string `json:"label"`
	Prefix     string `json:"prefix"`
	Unit       string `json:"unit"`
	Type       string `json:"type,omitempty"`
	SensorUID  string `json:"sensorUid,omitempty"`
	SensorName string `json:"sensorName,omitempty"`
	Category   string `json:"category,omitempty"`
	SearchText string `json:"searchText,omitempty"`
	Source     string `json:"source,omitempty"`
}

type evSendReadingsPayload struct {
	Readings []*evSendReadingsPayloadReading `json:"readings"`
	Settings *actionSettings                 `json:"settings"`
}

type evSendCatalogSource struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type evSendCatalogPayloadCatalog struct {
	Sensors        []*evSendSensorsPayloadSensor   `json:"sensors"`
	Readings       []*evSendReadingsPayloadReading `json:"readings"`
	Favorites      []favoriteReading               `json:"favorites,omitempty"`
	Sources        []evSendCatalogSource           `json:"sources,omitempty"`
}

type evSendCatalogPayload struct {
	Catalog  *evSendCatalogPayloadCatalog `json:"catalog"`
	Settings *actionSettings              `json:"settings,omitempty"`
}

type compositeSlotSettings struct {
	SensorUID          string  `json:"sensorUid"`
	ReadingID          int32   `json:"readingId,string"`
	ReadingLabel       string  `json:"readingLabel"`
	IsValid            bool    `json:"isValid"`
	Mode               string  `json:"mode,omitempty"` // "", "text", "graph", "both"; empty = use tile mode
	Title              string  `json:"title"`
	TitleFontSize      float64 `json:"titleFontSize"`
	ValueFontSize      float64 `json:"valueFontSize"`
	ForegroundColor    string  `json:"foregroundColor"`
	BackgroundColor    string  `json:"backgroundColor"`
	HighlightColor     string  `json:"highlightColor"`
	ValueTextColor     string  `json:"valueTextColor"`
	TitleColor         string  `json:"titleColor"`
	FillAlpha          int     `json:"fillAlpha"`
	Min                int     `json:"min"`
	Max                int     `json:"max"`
	Format             string  `json:"format"`
	Divisor            string  `json:"divisor"`
	GraphUnit          string  `json:"graphUnit"`
	GraphHeightPct     int     `json:"graphHeightPct"`
	GraphLineThickness int     `json:"graphLineThickness"`
	TextStroke         bool    `json:"textStroke"`
	TextStrokeColor    string  `json:"textStrokeColor"`

	Thresholds          []Threshold `json:"thresholds,omitempty"`
	SuppressedGlobalIDs []string    `json:"suppressedGlobalIDs,omitempty"`
	CurrentThresholdID  string      `json:"currentThresholdId,omitempty"`
}

type compositeActionSettings struct {
	SlotCount                int                      `json:"slotCount"`
	Mode                     string                   `json:"mode"`
	Slots                    [4]compositeSlotSettings `json:"slots"`
	UpdateIntervalOverrideMs int                      `json:"updateIntervalOverrideMs"` // 0 = follow global
	SmoothingAlpha           float64                  `json:"smoothingAlpha"`           // 0.1–1.0; 0 = 1.0 (no smoothing)
}

type derivedSlotSettings struct {
	SensorUID    string `json:"sensorUid"`
	ReadingID    int32  `json:"readingId,string"`
	ReadingLabel string `json:"readingLabel"`
	IsValid      bool   `json:"isValid"`
	Divisor      string `json:"divisor"`
	GraphUnit    string `json:"graphUnit"`
}

type derivedActionSettings struct {
	SlotCount       int                    `json:"slotCount"`
	Formula         string                 `json:"formula"` // "sum","average","max","min","delta","pct"
	Slots           [8]derivedSlotSettings `json:"slots"`

	// Tile-level display — mirrors actionSettings so threshold/color/format pipeline works unchanged
	Title                    string      `json:"title"`
	TitleFontSize            float64     `json:"titleFontSize"`
	ValueFontSize            float64     `json:"valueFontSize"`
	ShowTitleInGraph         *bool       `json:"showTitleInGraph"`
	Min                      int         `json:"min"`
	Max                      int         `json:"max"`
	Format                   string      `json:"format"`
	Divisor                  string      `json:"divisor"`
	GraphUnit                string      `json:"graphUnit"`
	TitleColor               string      `json:"titleColor"`
	ForegroundColor          string      `json:"foregroundColor"`
	BackgroundColor          string      `json:"backgroundColor"`
	HighlightColor           string      `json:"highlightColor"`
	ValueTextColor           string      `json:"valueTextColor"`
	GraphHeightPct           int         `json:"graphHeightPct"`
	GraphLineThickness       int         `json:"graphLineThickness"`
	TextStroke               bool        `json:"textStroke"`
	TextStrokeColor          string      `json:"textStrokeColor"`
	UpdateIntervalOverrideMs int         `json:"updateIntervalOverrideMs"` // 0 = follow global
	SmoothingAlpha           float64     `json:"smoothingAlpha"`           // 0.1–1.0; 0 = 1.0 (no smoothing)
	Thresholds               []Threshold `json:"thresholds"`
	SuppressedGlobalIDs      []string    `json:"suppressedGlobalIDs,omitempty"`
	CurrentThresholdID       string      `json:"currentThresholdId"`
	SnoozeDurations          []int       `json:"snoozeDurations,omitempty"`
}

type dialActionSettings struct {
	ActiveIndex     int              `json:"activeIndex"`
	Pages           []actionSettings `json:"pages"`
	DefaultView     string           `json:"defaultView,omitempty"`    // "", "fullscreen", "overview"
	// OverviewStyle selects how the overview (non-fullscreen) view renders:
	// "" / "carousel" = the original horizontal carousel; "stacked" = vertically
	// scrolling full-width strips with the active reading dominant and a peek of
	// the previous/next pages.
	OverviewStyle string `json:"overviewStyle,omitempty"`
	// OverviewPages caps how many pages the overview shows at once, regardless of
	// how many pages are defined: "" / "auto" = up to 3 (previous/active/next),
	// "2" = active + next only (bigger, more legible cards/strips). The page
	// indicator still reflects the full page count.
	OverviewPages  string `json:"overviewPages,omitempty"`
	IndicatorStyle string `json:"indicatorStyle,omitempty"` // "", "auto", "dots", "count", "off"
	// IndicatorFullscreen draws the page indicator in the fullscreen view too.
	// Off by default so fullscreen keeps its original look; the indicator style
	// above controls how it renders once enabled.
	IndicatorFullscreen bool `json:"indicatorFullscreen,omitempty"`
	// IndicatorColor sets the page-indicator colour (active dot / count text) so it
	// can be made to contrast against the dial background; empty = default grey.
	IndicatorColor string `json:"indicatorColor,omitempty"`
	// IndicatorSize scales the page indicator in "points": value 4 matches the
	// original size, 8 is double. Range 1-8; nil = default.
	IndicatorSize *float64 `json:"indicatorSize,omitempty"`
	// Edge separator drawn on this dial's left/right edges to visually separate
	// it from adjacent dials. Width is per edge in px (0 = off); nil = default.
	SeparatorWidth *int   `json:"separatorWidth,omitempty"`
	SeparatorColor string `json:"separatorColor,omitempty"`
	// ReverseRotation flips the dial turn direction so a clockwise turn goes to the
	// previous page instead of the next (some users / RTL layouts prefer this).
	ReverseRotation bool `json:"reverseRotation,omitempty"`
}

type evSdpiCollection struct {
	Group       bool     `json:"group"`
	Index       int      `json:"index"`
	Key         string   `json:"key"`
	Selection   []string `json:"selection"`
	Value       string   `json:"value"`
	Checked     bool     `json:"checked"`     // For checkbox inputs
	ThresholdID string   `json:"thresholdId"` // For threshold-specific operations
}
