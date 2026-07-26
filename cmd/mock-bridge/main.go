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
//	POST /source {"id":"remote0","present":true,"available":true} — control the mock remote source
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

// The mock remote source mirrors HWiNFO's remote shared-memory mappings:
// sensors are served under source-qualified UIDs and disappear (or go
// unavailable) on demand via the /source control endpoint.
const mockRemoteSourceID = "remote0"

var remoteSensors = []mockSensor{
	{id: "/remotecpu/0", name: "Remote CPU"},
	{id: "/remotesys/0", name: "Remote System"},
}

type remoteSourceState struct {
	present   bool
	available bool
}

func defaultRemoteReadings() map[string]*mockReading {
	list := []*mockReading{
		{path: "/remotecpu/0/temperature/0", sensor: "/remotecpu/0", label: "CPU Package", typ: "Temp", unit: "°C", defVal: 51, min: 20, max: 100},
		{path: "/remotecpu/0/load/0", sensor: "/remotecpu/0", label: "CPU Total", typ: "Usage", unit: "%", defVal: 30, min: 0, max: 100},
		{path: "/remotesys/0/fan/0", sensor: "/remotesys/0", label: "CPU Fan", typ: "Fan", unit: "RPM", defVal: 900, min: 0, max: 3000},
	}
	m := make(map[string]*mockReading, len(list))
	for _, r := range list {
		r.cur = r.defVal
		m[r.path] = r
	}
	return m
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
	mu             sync.RWMutex
	readings       = defaultReadings()
	remoteReadings = defaultRemoteReadings()
	remoteSource   = remoteSourceState{}
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
	id       string
	name     string
	sourceID string
}

func (s sensor) ID() string       { return s.id }
func (s sensor) Name() string     { return s.name }
func (s sensor) SourceID() string { return s.sourceID }

type source struct {
	id        string
	name      string
	pollTime  uint64
	available bool
}

func (s source) ID() string       { return s.id }
func (s source) Name() string     { return s.name }
func (s source) PollTime() uint64 { return s.pollTime }
func (s source) Available() bool  { return s.available }

// service implements hwsensorsservice.HardwareService over the mock data.
type service struct{}

func (service) PollTime() (uint64, error) {
	return uint64(time.Now().UnixNano()), nil
}

func (service) Sources() ([]hwsensorsservice.Source, error) {
	mu.RLock()
	defer mu.RUnlock()
	now := uint64(time.Now().UnixNano())
	out := []hwsensorsservice.Source{
		source{id: hwsensorsservice.LocalSourceID, name: "Local", pollTime: now, available: true},
	}
	if remoteSource.present {
		pollTime := now
		if !remoteSource.available {
			// An unavailable source stops polling; report a stale time so
			// the plugin's staleness gate reacts just like with real HWiNFO.
			pollTime = now - uint64(time.Minute)
		}
		out = append(out, source{
			id:        mockRemoteSourceID,
			name:      hwsensorsservice.SourceDisplayName(mockRemoteSourceID),
			pollTime:  pollTime,
			available: remoteSource.available,
		})
	}
	return out, nil
}

func (service) Sensors() ([]hwsensorsservice.Sensor, error) {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]hwsensorsservice.Sensor, 0, len(sensors)+len(remoteSensors))
	for _, s := range sensors {
		out = append(out, sensor{id: s.id, name: s.name, sourceID: hwsensorsservice.LocalSourceID})
	}
	if remoteSource.present {
		for _, s := range remoteSensors {
			out = append(out, sensor{
				id:       hwsensorsservice.ComposeSensorUID(mockRemoteSourceID, s.id),
				name:     s.name,
				sourceID: mockRemoteSourceID,
			})
		}
	}
	return out, nil
}

func (service) ReadingsForSensorID(id string) ([]hwsensorsservice.Reading, error) {
	sourceID, sensorID := hwsensorsservice.SplitSensorUID(id)

	mu.RLock()
	defer mu.RUnlock()

	pool := readings
	if sourceID != hwsensorsservice.LocalSourceID {
		if sourceID != mockRemoteSourceID || !remoteSource.present {
			return nil, fmt.Errorf("source %q unavailable", sourceID)
		}
		pool = remoteReadings
	}

	var out []hwsensorsservice.Reading
	for _, r := range pool {
		if r.sensor == sensorID {
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
		rd, ok = remoteReadings[req.Path]
	}
	if !ok {
		mu.Unlock()
		http.Error(w, "unknown path: "+req.Path, http.StatusNotFound)
		return
	}
	rd.cur = req.Value
	mu.Unlock()
	fmt.Fprintf(w, `{"ok":true,"path":%q,"value":%g}`, req.Path, req.Value)
}

func handleSource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID        string `json:"id"`
		Present   bool   `json:"present"`
		Available bool   `json:"available"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.ID != mockRemoteSourceID {
		http.Error(w, "unknown source: "+req.ID, http.StatusNotFound)
		return
	}
	mu.Lock()
	remoteSource = remoteSourceState{present: req.Present, available: req.Available}
	mu.Unlock()
	fmt.Fprintf(w, `{"ok":true,"id":%q,"present":%t,"available":%t}`, req.ID, req.Present, req.Available)
}

func handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	mu.Lock()
	readings = defaultReadings()
	remoteReadings = defaultRemoteReadings()
	remoteSource = remoteSourceState{}
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
	mux.HandleFunc("/source", handleSource)
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
