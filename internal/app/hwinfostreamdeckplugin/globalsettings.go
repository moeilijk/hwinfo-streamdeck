package hwinfostreamdeckplugin

import (
	"encoding/json"
	"fmt"
)

// mergeGlobalSettings lays the fields of a received global settings object
// over the current ones. Stream Deck stores every setGlobalSettings call as
// the complete global settings and hands the plugin only what was written
// (measured on Stream Deck 7.6.1), so a write that carries a few fields must
// not wipe the others. Only the fields present in raw change.
//
// incomplete reports that raw lacks fields the merged settings have: the
// stored copy is then partial and the caller writes the merged settings back.
func mergeGlobalSettings(current globalSettings, raw json.RawMessage) (merged globalSettings, incomplete bool, err error) {
	var incoming map[string]json.RawMessage
	if err := json.Unmarshal(raw, &incoming); err != nil {
		return current, false, fmt.Errorf("global settings: %w", err)
	}

	base, err := json.Marshal(current)
	if err != nil {
		return current, false, fmt.Errorf("global settings: %w", err)
	}
	var combined map[string]json.RawMessage
	if err := json.Unmarshal(base, &combined); err != nil {
		return current, false, fmt.Errorf("global settings: %w", err)
	}
	for k := range combined {
		if _, ok := incoming[k]; !ok {
			incomplete = true
		}
	}
	for k, v := range incoming {
		combined[k] = v
	}

	buf, err := json.Marshal(combined)
	if err != nil {
		return current, false, fmt.Errorf("global settings: %w", err)
	}
	if err := json.Unmarshal(buf, &merged); err != nil {
		return current, false, fmt.Errorf("global settings: %w", err)
	}
	return merged, incomplete, nil
}

// saveDerivedPresets stores the Derived preset list sent by the Derived
// Property Inspector and writes the complete global settings. The Property
// Inspector no longer writes global settings itself: a partial write would
// replace everything else.
func (p *Plugin) saveDerivedPresets(raw json.RawMessage) error {
	var presets []json.RawMessage
	if err := json.Unmarshal(raw, &presets); err != nil {
		return fmt.Errorf("derived presets: %w", err)
	}
	p.mu.Lock()
	p.globalSettings.DerivedPresets = append(json.RawMessage(nil), raw...)
	gs := p.globalSettings
	p.mu.Unlock()
	return p.sd.SetGlobalSettings(gs)
}
