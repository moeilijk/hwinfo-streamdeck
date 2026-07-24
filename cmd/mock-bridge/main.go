// mock-bridge serves the HardwareService gRPC interface (dispense key
// "hwinfoplugin") with controllable mock sensor data. It stands in for the
// Windows-only HWiNFO shared-memory bridge in the test harness, so the plugin
// exercises the exact same go-plugin path as production.
//
// Control API (HTTP, port from MOCK_CONTROL_PORT, default 9999):
//
//	POST /set {"path":"/mockcpu/0/temperature/0","value":85.0} — change a reading
//	POST /reset — restore all defaults
//	GET  /list — dump current readings
package main

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/hashicorp/go-plugin"
	hwsensorsservice "github.com/moeilijk/hwinfo-streamdeck/pkg/service"
)

type mockReading struct {
	path   string
	sensor string
	label  string
	typ    string // canonical hwsensorsservice type string: Temp, Usage, Volt, Fan, ...
	unit   string
	defVal float64
	min    float64
	max    float64
	cur    float64
}

type mockSensor struct {
	id   string
	name string
}

var sensors = []mockSensor{
	{id: "/mockcpu/0", name: "Mock CPU"},
	{id: "/mockgpu/0", name: "Mock GPU"},
	{id: "/mocksys/0", name: "Mock System"},
}

func defaultReadings() map[string]*mockReading {
	list := []*mockReading{
		{path: "/mockcpu/0/temperature/0", sensor: "/mockcpu/0", label: "CPU Package", typ: "Temp", unit: "°C", defVal: 45, min: 20, max: 100},
		{path: "/mockcpu/0/temperature/1", sensor: "/mockcpu/0", label: "CPU Core 0", typ: "Temp", unit: "°C", defVal: 42, min: 20, max: 100},
		{path: "/mockcpu/0/load/0", sensor: "/mockcpu/0", label: "CPU Total", typ: "Usage", unit: "%", defVal: 20, min: 0, max: 100},
		{path: "/mockgpu/0/temperature/0", sensor: "/mockgpu/0", label: "GPU Core", typ: "Temp", unit: "°C", defVal: 55, min: 20, max: 100},
		{path: "/mockgpu/0/load/0", sensor: "/mockgpu/0", label: "GPU Total", typ: "Usage", unit: "%", defVal: 35, min: 0, max: 100},
		{path: "/mocksys/0/voltage/0", sensor: "/mocksys/0", label: "CPU Core", typ: "Volt", unit: "V", defVal: 1.2, min: 0.8, max: 1.6},
		{path: "/mocksys/0/fan/0", sensor: "/mocksys/0", label: "CPU Fan", typ: "Fan", unit: "RPM", defVal: 1200, min: 0, max: 3000},
	}
	m := make(map[string]*mockReading, len(list))
	for _, r := range list {
		r.cur = r.defVal
		m[r.path] = r
	}
	return m
}

var (
	mu       sync.RWMutex
	readings = defaultReadings()
)

func readingID(sensorID, path string) int32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(sensorID))
	_, _ = h.Write([]byte(path))
	return int32(h.Sum32() & 0x7fffffff)
}

func typeIndex(typ string) int32 {
	for i, name := range []string{"None", "Temp", "Volt", "Fan", "Current", "Power", "Clock", "Usage", "Other"} {
		if name == typ {
			return int32(i)
		}
	}
	return 8 // Other
}

// reading adapts a snapshot of a mockReading to hwsensorsservice.Reading.
type reading struct {
	id    int32
	typ   string
	label string
	unit  string
	value float64
	min   float64
	max   float64
}

func (r reading) ID() int32                { return r.id }
func (r reading) TypeI() int32             { return typeIndex(r.typ) }
func (r reading) Type() string             { return r.typ }
func (r reading) Label() string            { return r.label }
func (r reading) Unit() string             { return r.unit }
func (r reading) Value() float64           { return r.value }
func (r reading) ValueNormalized() float64 { return hwsensorsservice.NormalizeToBytes(r.value, r.unit) }
func (r reading) ValueMin() float64        { return r.min }
func (r reading) ValueMax() float64        { return r.max }
func (r reading) ValueAvg() float64        { return r.value }

type sensor struct {
	id   string
	name string
}

func (s sensor) ID() string   { return s.id }
func (s sensor) Name() string { return s.name }

// service implements hwsensorsservice.HardwareService over the mock data.
type service struct{}

func (service) PollTime() (uint64, error) {
	return uint64(time.Now().UnixNano()), nil
}

func (service) Sensors() ([]hwsensorsservice.Sensor, error) {
	out := make([]hwsensorsservice.Sensor, 0, len(sensors))
	for _, s := range sensors {
		out = append(out, sensor{id: s.id, name: s.name})
	}
	return out, nil
}

func (service) ReadingsForSensorID(id string) ([]hwsensorsservice.Reading, error) {
	mu.RLock()
	defer mu.RUnlock()
	var out []hwsensorsservice.Reading
	for _, r := range readings {
		if r.sensor == id {
			out = append(out, reading{
				id:    readingID(r.sensor, r.path),
				typ:   r.typ,
				label: r.label,
				unit:  r.unit,
				value: r.cur,
				min:   r.min,
				max:   r.max,
			})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("sensor %s not found", id)
	}
	return out, nil
}

func handleSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Path  string  `json:"path"`
		Value float64 `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	mu.Lock()
	rd, ok := readings[req.Path]
	if !ok {
		mu.Unlock()
		http.Error(w, "unknown path: "+req.Path, http.StatusNotFound)
		return
	}
	rd.cur = req.Value
	mu.Unlock()
	fmt.Fprintf(w, `{"ok":true,"path":%q,"value":%g}`, req.Path, req.Value)
}

func handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	mu.Lock()
	readings = defaultReadings()
	mu.Unlock()
	fmt.Fprint(w, `{"ok":true}`)
}

func handleList(w http.ResponseWriter, _ *http.Request) {
	mu.RLock()
	out := make([]map[string]interface{}, 0, len(readings))
	for path, rd := range readings {
		out = append(out, map[string]interface{}{
			"path":    path,
			"sensor":  rd.sensor,
			"label":   rd.label,
			"type":    rd.typ,
			"unit":    rd.unit,
			"current": rd.cur,
			"default": rd.defVal,
		})
	}
	mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func main() {
	port := os.Getenv("MOCK_CONTROL_PORT")
	if port == "" {
		port = "9999"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/set", handleSet)
	mux.HandleFunc("/reset", handleReset)
	mux.HandleFunc("/list", handleList)
	go func() {
		// The control server is best-effort: a bind failure (port in use after a
		// bridge restart) must not take down the gRPC side.
		if err := http.ListenAndServe(":"+port, mux); err != nil {
			log.Printf("mock-bridge control server: %v", err)
		}
	}()

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: hwsensorsservice.Handshake,
		Plugins: map[string]plugin.Plugin{
			"hwinfoplugin": &hwsensorsservice.HardwareServicePlugin{Impl: service{}},
		},
		GRPCServer: plugin.DefaultGRPCServer,
	})
}
