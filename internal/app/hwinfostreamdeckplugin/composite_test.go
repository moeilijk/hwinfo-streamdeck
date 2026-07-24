package hwinfostreamdeckplugin

import (
	"encoding/json"
	"testing"
)

func TestEffectiveCompositeSlotMode(t *testing.T) {
	tests := []struct {
		name     string
		tileMode string
		slotMode string
		want     string
	}{
		{name: "empty slot mode inherits tile mode", tileMode: compositeModeBoth, slotMode: "", want: compositeModeBoth},
		{name: "slot text overrides tile graph", tileMode: compositeModeGraph, slotMode: compositeModeText, want: compositeModeText},
		{name: "invalid slot mode inherits tile mode", tileMode: compositeModeGraph, slotMode: "bogus", want: compositeModeGraph},
		{name: "invalid tile mode falls back to text", tileMode: "bogus", slotMode: "", want: compositeModeText},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := effectiveCompositeSlotMode(tt.tileMode, tt.slotMode); got != tt.want {
				t.Fatalf("effectiveCompositeSlotMode(%q, %q) = %q, want %q", tt.tileMode, tt.slotMode, got, tt.want)
			}
		})
	}
}

func TestDecodeCompositeSettingsKeepsSlotMode(t *testing.T) {
	raw := json.RawMessage(`{
		"slotCount": 2,
		"mode": "both",
		"slots": [
			{"mode": "text"},
			{"mode": "graph"}
		]
	}`)

	settings, err := decodeCompositeSettings(&raw)
	if err != nil {
		t.Fatalf("decodeCompositeSettings: %v", err)
	}
	if settings.Slots[0].Mode != compositeModeText {
		t.Fatalf("slot 0 mode = %q, want %q", settings.Slots[0].Mode, compositeModeText)
	}
	if settings.Slots[1].Mode != compositeModeGraph {
		t.Fatalf("slot 1 mode = %q, want %q", settings.Slots[1].Mode, compositeModeGraph)
	}
}

func TestRenderCompositeTileHonorsPerSlotGraphMode(t *testing.T) {
	settings := compositeActionSettings{
		SlotCount: 2,
		Mode:      compositeModeText,
		Slots: [4]compositeSlotSettings{
			{
				Mode:            compositeModeGraph,
				ForegroundColor: "#000000",
				HighlightColor:  "#ff0000",
				BackgroundColor: "#000000",
				ValueTextColor:  "#ffffff",
				TitleColor:      "#ffffff",
				FillAlpha:       100,
				Min:             0,
				Max:             100,
			},
			{
				Mode:            compositeModeText,
				ForegroundColor: "#000000",
				HighlightColor:  "#00ff00",
				BackgroundColor: "#000000",
				ValueTextColor:  "#ffffff",
				TitleColor:      "#ffffff",
				FillAlpha:       100,
				Min:             0,
				Max:             100,
			},
		},
	}
	state := &compositeState{graphs: initCompositeGraphs(&settings)}
	state.graphs[0].Update(100)
	state.graphs[1].Update(100)

	imgBytes, err := renderCompositeTile(&settings, state, [4]string{}, [4]*Threshold{})
	if err != nil {
		t.Fatalf("renderCompositeTile: %v", err)
	}
	img, err := decodePNGToRGBA(imgBytes)
	if err != nil {
		t.Fatalf("decodePNGToRGBA: %v", err)
	}

	redPixels := 0
	greenPixels := 0
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			p := img.RGBAAt(x, y)
			if p.R > 0 {
				redPixels++
			}
			if p.G > 0 {
				greenPixels++
			}
		}
	}

	if redPixels == 0 {
		t.Fatal("graph-only slot did not render any red graph pixels")
	}
	if greenPixels != 0 {
		t.Fatalf("text-only slot rendered %d green graph pixels", greenPixels)
	}
}
