package hwinfostreamdeckplugin

import (
	"fmt"
	"image/color"
	"strconv"

	"github.com/moeilijk/hwinfo-streamdeck/pkg/graph"
	"github.com/moeilijk/hwinfo-streamdeck/pkg/streamdeck"
)

// Labels of the reading tile.
const (
	labelValue = 1
	labelUnit  = 3 // the unit as an element of its own, see unitDisplay

	valueBaseline = 44

	defaultLabelFontSize = 10.5

	unitDisplayInline = "inline" // value and unit in one label (the default)
	unitDisplayBelow  = "below"  // unit as its own element under the value
	unitDisplayHidden = "hidden" // no unit

	// valueUnitBackdropGroup joins the value and the unit below it into one
	// text backdrop.
	valueUnitBackdropGroup = 1
)

func validUnitDisplay(mode string) bool {
	return mode == unitDisplayInline || mode == unitDisplayBelow || mode == unitDisplayHidden
}

// unitLabelTexts splits the display text over the value label and the unit
// label. In the inline mode the value label carries the composed text and the
// unit label is empty. With a Format set, valueTextNoUnit is the format output
// and is shown as typed.
func unitLabelTexts(mode, valueTextNoUnit, displayText, displayUnit string) (valueLabel, unitLabel string) {
	switch mode {
	case unitDisplayBelow:
		return valueTextNoUnit, displayUnit
	case unitDisplayHidden:
		return valueTextNoUnit, ""
	default:
		return displayText, ""
	}
}

func valueFontSizeOf(s *actionSettings) float64 {
	if s.ValueFontSize != 0 {
		return s.ValueFontSize
	}
	return defaultLabelFontSize
}

func unitFontSizeOf(s *actionSettings) float64 {
	if s.UnitFontSize != 0 {
		return s.UnitFontSize
	}
	return valueFontSizeOf(s)
}

func valueTextColorOf(s *actionSettings) *color.RGBA {
	if s.ValueTextColor != "" {
		return hexToRGBA(s.ValueTextColor)
	}
	return &color.RGBA{255, 255, 255, 255}
}

// applyTextLayout positions the value and the unit label from the settings.
// The unit sits one value line below the value baseline. Both offsets shift
// their own label only. It has to run again whenever the value size, the font
// or the line spacing changes, because the line height follows from those.
func applyTextLayout(g *graph.Graph, s *actionSettings) {
	g.SetLineSpacing(s.LineSpacing)
	_ = g.SetLabelOffset(labelValue, s.ValueOffset)
	_ = g.SetLabelFontSize(labelUnit, unitFontSizeOf(s))
	_ = g.SetLabelOffset(labelUnit, float64(g.LineAdvance(valueFontSizeOf(s)))+s.UnitOffset)
	group := 0
	if s.UnitDisplay == unitDisplayBelow {
		group = valueUnitBackdropGroup
	}
	_ = g.SetLabelBackdropGroup(labelValue, group)
	_ = g.SetLabelBackdropGroup(labelUnit, group)
}

// applyUnitTextColor gives the unit its own colour when one is set. Without it
// the unit keeps the colour the value label was given, alert colours included.
func applyUnitTextColor(g *graph.Graph, s *actionSettings) {
	if s.UnitTextColor != "" {
		_ = g.SetLabelColor(labelUnit, hexToRGBA(s.UnitTextColor))
	}
}

// handleTextLayout stores one of the value/unit typography settings of the
// reading tile and applies it to the tile's graph.
func (p *Plugin) handleTextLayout(event *streamdeck.EvSendToPlugin, sdpi *evSdpiCollection) error {
	settings, err := p.am.getSettings(event.Context)
	if err != nil {
		return fmt.Errorf("handleTextLayout getSettings: %v", err)
	}
	p.mu.RLock()
	g, ok := p.graphs[event.Context]
	p.mu.RUnlock()
	if !ok {
		return fmt.Errorf("handleTextLayout no graph for context: %s", event.Context)
	}

	number := func(min, max float64) (float64, error) {
		v, err := strconv.ParseFloat(sdpi.Value, 64)
		if err != nil {
			return 0, fmt.Errorf("%s: %v", sdpi.Key, err)
		}
		if v < min || v > max {
			return 0, fmt.Errorf("%s out of range: %v", sdpi.Key, v)
		}
		return v, nil
	}

	switch sdpi.Key {
	case "unitDisplay":
		if !validUnitDisplay(sdpi.Value) {
			return fmt.Errorf("unknown unit display: %s", sdpi.Value)
		}
		settings.UnitDisplay = sdpi.Value
		if sdpi.Value == unitDisplayInline {
			settings.UnitDisplay = ""
		}
	case "unitFontSize":
		v, err := number(8, 20)
		if err != nil {
			return err
		}
		settings.UnitFontSize = v
	case "unitOffset":
		v, err := number(-20, 20)
		if err != nil {
			return err
		}
		settings.UnitOffset = v
	case "valueOffset":
		v, err := number(-20, 20)
		if err != nil {
			return err
		}
		settings.ValueOffset = v
	case "lineSpacing":
		v, err := number(0.5, 2)
		if err != nil {
			return err
		}
		settings.LineSpacing = v
	case "unitTextColor":
		if len(sdpi.Value) != 7 || sdpi.Value[0] != '#' {
			return fmt.Errorf("unitTextColor: not a hex colour: %s", sdpi.Value)
		}
		if _, err := strconv.ParseUint(sdpi.Value[1:], 16, 32); err != nil {
			return fmt.Errorf("unitTextColor: not a hex colour: %s", sdpi.Value)
		}
		settings.UnitTextColor = sdpi.Value
		applyUnitTextColor(g, &settings)
	default:
		return fmt.Errorf("handleTextLayout unknown key: %s", sdpi.Key)
	}
	applyTextLayout(g, &settings)

	if err := p.sd.SetSettings(event.Context, &settings); err != nil {
		return fmt.Errorf("handleTextLayout SetSettings: %v", err)
	}
	p.am.SetAction(event.Action, event.Context, &settings)
	return nil
}
