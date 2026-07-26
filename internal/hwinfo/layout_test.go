package hwinfo

import "testing"

// The packed struct sizes are fixed by the shared-memory ABI; the offsets in
// layout.go must add up to them exactly.
func TestElementLayoutSizes(t *testing.T) {
	if sizeReadingElementV1 != 316 {
		t.Errorf("v1 reading element size = %d, want 316", sizeReadingElementV1)
	}
	if sizeReadingElementV2 != 460 {
		t.Errorf("v2 reading element size = %d, want 460", sizeReadingElementV2)
	}
	if sizeSensorElementV1 != 264 {
		t.Errorf("v1 sensor element size = %d, want 264", sizeSensorElementV1)
	}
	if sizeSensorElementV2 != 392 {
		t.Errorf("v2 sensor element size = %d, want 392", sizeSensorElementV2)
	}
}

func TestUTFFieldGuards(t *testing.T) {
	v1 := make([]byte, sizeReadingElementV1)
	if got := utfField(v1, offReadingUTFLabelUser, stringLen2); got != nil {
		t.Errorf("v1 element must not expose utfLabelUser, got %d bytes", len(got))
	}

	v2 := make([]byte, sizeReadingElementV2)
	copy(v2[offReadingUTFLabelUser:], "Ядро\x00")
	got := utfField(v2, offReadingUTFLabelUser, stringLen2)
	if got == nil {
		t.Fatal("v2 element must expose utfLabelUser")
	}
	if string(got[:8]) != "Ядро" {
		t.Errorf("utfLabelUser bytes not sliced at field offset")
	}
}
