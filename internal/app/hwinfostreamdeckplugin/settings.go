package hwinfostreamdeckplugin

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"time"

	hwsensorsservice "github.com/moeilijk/hwinfo-streamdeck/pkg/service"
)

func decodeActionSettings(raw *json.RawMessage) (actionSettings, bool, error) {
	var settings actionSettings
	if raw == nil || *raw == nil {
		return settings, false, nil
	}
	if err := json.Unmarshal(*raw, &settings); err == nil {
		return settings, false, nil
	}

	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(*raw, &rawMap); err != nil {
		return settings, false, err
	}

	migrated := false
	legacyReading := ""
	if v, ok := rawMap["readingId"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			if _, err := strconv.ParseInt(s, 10, 32); err != nil {
				legacyReading = s
			}
		}
	}

	legacySensor := ""
	if v, ok := rawMap["sensorUid"]; ok {
		_ = json.Unmarshal(v, &legacySensor)
	}

	if legacyReading != "" || strings.Contains(legacySensor, ":") {
		sensorID, readingPath := legacySensorReading(legacySensor, legacyReading)
		if sensorID != "" && readingPath != "" {
			migrated = true
			id := makeReadingIDCompat(sensorID, readingPath)
			rawMap["sensorUid"] = mustJSON(strconv.Quote(sensorID))
			rawMap["readingId"] = mustJSON(strconv.Quote(strconv.FormatInt(int64(id), 10)))
			rawMap["isValid"] = mustJSON("true")
		}
	}

	b, err := json.Marshal(rawMap)
	if err != nil {
		return settings, migrated, err
	}
	if err := json.Unmarshal(b, &settings); err != nil {
		return settings, migrated, err
	}

	// Migrate legacy Warning/Critical thresholds to new dynamic system
	if migrateToThresholds(&settings) {
		migrated = true
	}
	if normalized := normalizeThresholdSnoozeDurations(settings.SnoozeDurations); !sameIntSlice(normalized, settings.SnoozeDurations) {
		settings.SnoozeDurations = normalized
		migrated = true
	}

	return settings, migrated, nil
}

func mustJSON(raw string) json.RawMessage {
	return json.RawMessage(raw)
}

func legacySensorReading(sensorUID, readingID string) (string, string) {
	readingPath := strings.TrimSpace(readingID)
	if readingPath == "" {
		if idx := strings.LastIndex(sensorUID, ":"); idx != -1 && idx+1 < len(sensorUID) {
			readingPath = strings.TrimSpace(sensorUID[idx+1:])
		}
	}
	if readingPath == "" {
		return "", ""
	}
	sensorID := sensorIDFromReadingCompat(readingPath)
	if sensorID == "" {
		return "", ""
	}
	return sensorID, readingPath
}

func sensorIDFromReadingCompat(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	trimmed := strings.Trim(id, "/")
	if trimmed == "" {
		return ""
	}
	parts := strings.Split(trimmed, "/")
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return "/" + parts[0]
	case 2:
		return "/" + parts[0]
	default:
		return "/" + strings.Join(parts[:len(parts)-2], "/")
	}
}

func makeReadingIDCompat(sensorID, readingID string) int32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(sensorID))
	_, _ = h.Write([]byte(readingID))
	return int32(h.Sum32() & 0x7fffffff)
}

func syncSettingsWithReadings(settings *actionSettings, readings []hwsensorsservice.Reading) bool {
	changed := false
	if settings.ReadingID != 0 {
		for _, r := range readings {
			if r.ID() == settings.ReadingID {
				if settings.ReadingLabel == "" {
					settings.ReadingLabel = r.Label()
					changed = true
				}
				if !settings.IsValid {
					settings.IsValid = true
					changed = true
				}
				return changed
			}
		}
	}
	if settings.ReadingLabel != "" {
		for _, r := range readings {
			if r.Label() == settings.ReadingLabel {
				if settings.ReadingID != r.ID() {
					settings.ReadingID = r.ID()
					changed = true
				}
				if !settings.IsValid {
					settings.IsValid = true
					changed = true
				}
				return changed
			}
		}
	}
	return changed
}

// migrateToThresholds converts legacy Warning/Critical threshold settings to the new dynamic threshold array
func migrateToThresholds(settings *actionSettings) bool {
	if len(settings.Thresholds) > 0 {
		return false // Already has thresholds, no migration needed
	}

	migrated := false

	// Migrate Warning threshold
	if settings.WarningEnabled || settings.WarningValue != 0 || settings.WarningOperator != "" {
		settings.Thresholds = append(settings.Thresholds, Threshold{
			ID:              fmt.Sprintf("threshold_%d", time.Now().UnixNano()),
			Name:            "Warning",
			Enabled:         settings.WarningEnabled,
			Operator:        defaultOperator(settings.WarningOperator),
			Value:           settings.WarningValue,
			BackgroundColor: defaultColor(settings.WarningBackgroundColor, "#333300"),
			ForegroundColor: defaultColor(settings.WarningForegroundColor, "#999900"),
			HighlightColor:  defaultColor(settings.WarningHighlightColor, "#ffff00"),
			ValueTextColor:  defaultColor(settings.WarningValueTextColor, "#ffff00"),
			TextColor:       defaultColor(settings.WarningValueTextColor, "#ffff00"),
		})
		migrated = true
	}

	// Migrate Critical threshold
	if settings.CriticalEnabled || settings.CriticalValue != 0 || settings.CriticalOperator != "" {
		settings.Thresholds = append(settings.Thresholds, Threshold{
			ID:              fmt.Sprintf("threshold_%d", time.Now().UnixNano()+1),
			Name:            "Critical",
			Enabled:         settings.CriticalEnabled,
			Operator:        defaultOperator(settings.CriticalOperator),
			Value:           settings.CriticalValue,
			BackgroundColor: defaultColor(settings.CriticalBackgroundColor, "#660000"),
			ForegroundColor: defaultColor(settings.CriticalForegroundColor, "#990000"),
			HighlightColor:  defaultColor(settings.CriticalHighlightColor, "#ff3333"),
			ValueTextColor:  defaultColor(settings.CriticalValueTextColor, "#ff0000"),
			TextColor:       defaultColor(settings.CriticalValueTextColor, "#ff0000"),
		})
		migrated = true
	}

	// Clear legacy fields after migration
	if migrated {
		// Map old CurrentAlertState to new CurrentThresholdID
		if settings.CurrentAlertState != "" && settings.CurrentAlertState != "none" {
			for _, t := range settings.Thresholds {
				if strings.EqualFold(t.Name, settings.CurrentAlertState) {
					settings.CurrentThresholdID = t.ID
					break
				}
			}
		}
		// Clear legacy fields
		settings.WarningEnabled = false
		settings.WarningOperator = ""
		settings.WarningValue = 0
		settings.WarningBackgroundColor = ""
		settings.WarningForegroundColor = ""
		settings.WarningHighlightColor = ""
		settings.WarningValueTextColor = ""
		settings.CriticalEnabled = false
		settings.CriticalOperator = ""
		settings.CriticalValue = 0
		settings.CriticalBackgroundColor = ""
		settings.CriticalForegroundColor = ""
		settings.CriticalHighlightColor = ""
		settings.CriticalValueTextColor = ""
		settings.CurrentAlertState = ""
	}

	return migrated
}

func defaultOperator(op string) string {
	if op == "" {
		return ">="
	}
	return op
}

func defaultColor(color, defaultVal string) string {
	if color == "" {
		return defaultVal
	}
	return color
}
