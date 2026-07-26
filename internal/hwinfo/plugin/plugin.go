//go:build windows

package plugin

import (
	"time"

	"github.com/moeilijk/hwinfo-streamdeck/internal/hwinfo"
	hwsensorsservice "github.com/moeilijk/hwinfo-streamdeck/pkg/service"
)

// Plugin implementation
type Plugin struct {
	Service *Service
}

// PollTime implementation for plugin; reports the local machine's poll time
func (p *Plugin) PollTime() (uint64, error) {
	shmem, err := p.Service.Shmem(hwsensorsservice.LocalSourceID)
	if err != nil {
		return 0, err
	}
	// The HardwareService contract is Unix nanoseconds; HWiNFO's
	// dwLastUpdate is Unix seconds.
	return shmem.PollTime() * uint64(time.Second), nil
}

// Sources implementation for plugin
func (p *Plugin) Sources() ([]hwsensorsservice.Source, error) {
	var sources []hwsensorsservice.Source
	for _, id := range p.Service.SourceIDs() {
		shmem, err := p.Service.Shmem(id)
		if err != nil {
			continue
		}
		sources = append(sources, source{
			id:        id,
			pollTime:  shmem.PollTime() * uint64(time.Second),
			available: shmem.Signature() == "HWiS",
		})
	}
	return sources, nil
}

// Sensors implementation for plugin; lists all sources, local first, with
// source-qualified UIDs for remote sensors
func (p *Plugin) Sensors() ([]hwsensorsservice.Sensor, error) {
	var sensors []hwsensorsservice.Sensor
	var firstErr error
	for _, id := range p.Service.SourceIDs() {
		shmem, err := p.Service.Shmem(id)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for s := range shmem.IterSensors() {
			sensors = append(sensors, &sensor{Sensor: s, sourceID: id})
		}
	}
	if sensors == nil && firstErr != nil {
		return nil, firstErr
	}
	return sensors, nil
}

// ReadingsForSensorID implementation for plugin; accepts both plain (local)
// and source-qualified sensor UIDs
func (p *Plugin) ReadingsForSensorID(id string) ([]hwsensorsservice.Reading, error) {
	sourceID, sensorID := hwsensorsservice.SplitSensorUID(id)
	res, err := p.Service.ReadingsBySensorID(sourceID, sensorID)
	if err != nil {
		return nil, err
	}
	var readings []hwsensorsservice.Reading
	for _, r := range res {
		readings = append(readings, &reading{r})
	}
	return readings, nil
}

type source struct {
	id        string
	pollTime  uint64
	available bool
}

func (s source) ID() string {
	return s.id
}

func (s source) Name() string {
	return hwsensorsservice.SourceDisplayName(s.id)
}

func (s source) PollTime() uint64 {
	return s.pollTime
}

func (s source) Available() bool {
	return s.available
}

type sensor struct {
	hwinfo.Sensor
	sourceID string
}

func (s sensor) ID() string {
	return hwsensorsservice.ComposeSensorUID(s.sourceID, s.Sensor.ID())
}

func (s sensor) SourceID() string {
	return s.sourceID
}

func (s sensor) Name() string {
	// Prefer the v2 UTF-8 user name; fall back to the original English
	// name for HWiNFO versions before v7.33 (unchanged legacy behavior).
	if n := s.UTFNameUser(); n != "" {
		return n
	}
	return s.NameOrig()
}

type reading struct {
	hwinfo.Reading
}

func (r reading) Label() string {
	// Prefer the v2 UTF-8 user label; fall back to the original English
	// label for HWiNFO versions before v7.33 (unchanged legacy behavior).
	if l := r.UTFLabelUser(); l != "" {
		return l
	}
	return r.LabelOrig()
}

func (r reading) Type() string {
	return r.Reading.Type().String()
}

func (r reading) TypeI() int32 {
	return int32(r.Reading.Type())
}

func (r reading) ValueNormalized() float64 {
	return hwsensorsservice.NormalizeToBytes(r.Value(), r.Unit())
}
