package hwinfostreamdeckplugin

import "testing"

func TestDerivedLabelText(t *testing.T) {
	p := &Plugin{}

	tests := []struct {
		name     string
		settings derivedActionSettings
		want     string
	}{
		{
			name: "uses explicit title when graph title is enabled",
			settings: derivedActionSettings{
				Title:            "CPU Total",
				Formula:          "average",
				ShowTitleInGraph: boolPtr(true),
			},
			want: "CPU Total",
		},
		{
			name: "falls back to formula when title is empty",
			settings: derivedActionSettings{
				Formula:          "average",
				ShowTitleInGraph: boolPtr(true),
			},
			want: "average",
		},
		{
			name: "hides title when graph title is disabled",
			settings: derivedActionSettings{
				Title:            "CPU Total",
				ShowTitleInGraph: boolPtr(false),
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.derivedLabelText(&tt.settings); got != tt.want {
				t.Fatalf("derivedLabelText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDecodeDerivedSettingsDefaults(t *testing.T) {
	settings, err := decodeDerivedSettings(nil)
	if err != nil {
		t.Fatalf("decodeDerivedSettings(nil) error = %v", err)
	}
	if settings.TitleFontSize != 10.5 {
		t.Fatalf("TitleFontSize = %v, want 10.5", settings.TitleFontSize)
	}
	if settings.ValueFontSize != 10.5 {
		t.Fatalf("ValueFontSize = %v, want 10.5", settings.ValueFontSize)
	}
	if settings.ShowTitleInGraph == nil || !*settings.ShowTitleInGraph {
		t.Fatalf("ShowTitleInGraph = %v, want true", settings.ShowTitleInGraph)
	}
}
