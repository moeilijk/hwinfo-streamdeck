package hwinfostreamdeckplugin

import "testing"

func TestSensorCategory(t *testing.T) {
	tests := []struct {
		name     string
		sensorID string
		sensor   string
		want     string
	}{
		{name: "cpu by id", sensorID: "/amdcpu/0", sensor: "AMD Ryzen 7 9800X3D", want: "cpu"},
		{name: "gpu by name", sensorID: "/pci/0", sensor: "NVIDIA GeForce RTX 4090", want: "gpu"},
		{name: "memory by name", sensorID: "/memory/0", sensor: "Memory", want: "memory"},
		{name: "disk by id", sensorID: "/nvme/0", sensor: "Samsung SSD 990 PRO", want: "disk"},
		{name: "network by name", sensorID: "/nic/0", sensor: "Intel Ethernet Controller", want: "network"},
		{name: "motherboard by id", sensorID: "/lpc/nct6798d", sensor: "Nuvoton NCT6798D", want: "motherboard"},
		{name: "fallback other", sensorID: "/battery/0", sensor: "Battery", want: "other"},
	}

	for _, tc := range tests {
		if got := sensorCategory(tc.sensorID, tc.sensor); got != tc.want {
			t.Fatalf("%s: expected %q, got %q", tc.name, tc.want, got)
		}
	}
}

func TestReadingPayloadIncludesSearchMetadata(t *testing.T) {
	reading := stubReading{
		id:    42,
		typ:   "Load",
		label: "CPU Total",
		unit:  "%",
	}

	payload := readingPayload("/amdcpu/0", "AMD Ryzen 7 9800X3D", reading)
	if payload.Category != "cpu" {
		t.Fatalf("expected cpu category, got %q", payload.Category)
	}
	if payload.SensorUID != "/amdcpu/0" {
		t.Fatalf("expected sensor UID to be copied")
	}
	if payload.SearchText == "" {
		t.Fatalf("expected search text to be populated")
	}
}

func TestFavoriteID(t *testing.T) {
	got := favoriteID("/amdcpu/0", 42)
	want := "/amdcpu/0|42"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

type stubReading struct {
	id    int32
	typ   string
	label string
	unit  string
}

func (r stubReading) ID() int32                { return r.id }
func (r stubReading) TypeI() int32             { return 0 }
func (r stubReading) Type() string             { return r.typ }
func (r stubReading) Label() string            { return r.label }
func (r stubReading) Unit() string             { return r.unit }
func (r stubReading) Value() float64           { return 0 }
func (r stubReading) ValueNormalized() float64 { return 0 }
func (r stubReading) ValueMin() float64        { return 0 }
func (r stubReading) ValueMax() float64        { return 0 }
func (r stubReading) ValueAvg() float64        { return 0 }
