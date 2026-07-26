//go:build windows

package plugin

import (
	"fmt"
	"sort"
	"sync"

	"github.com/moeilijk/hwinfo-streamdeck/internal/hwinfo"
)

// sourceData caches one source's shared memory and derived lookups.
type sourceData struct {
	shmem              *hwinfo.SharedMemory
	sensorIDByIdx      []string
	readingsBySensorID map[string][]hwinfo.Reading
	readingsBuilt      bool
}

// Service wraps hwinfo shared mem streaming for the local machine and all
// connected remote machines, and provides convenient methods for data access
type Service struct {
	streamch <-chan []hwinfo.SourceResult
	mu       sync.RWMutex
	// sources is keyed by source id ("" = local). The local entry survives
	// read errors with its last data (staleness is handled downstream);
	// remote entries are dropped as soon as their mapping disappears.
	sources map[string]*sourceData
}

// Start starts the service providing updating hardware info
func StartService() *Service {
	return &Service{
		streamch: hwinfo.StreamSources(),
		sources:  make(map[string]*sourceData),
	}
}

func (s *Service) recvBatch(batch []hwinfo.SourceResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	seen := make(map[string]bool, len(batch))
	var localErr error
	for _, res := range batch {
		if res.Err != nil {
			// Local read failure: keep the previous data so PollTime keeps
			// reporting the last (stale) poll and downstream can decide.
			if res.SourceID == "" {
				localErr = res.Err
				seen[""] = true
			}
			continue
		}
		seen[res.SourceID] = true
		s.sources[res.SourceID] = &sourceData{shmem: res.Shmem}
	}
	for id := range s.sources {
		if !seen[id] {
			delete(s.sources, id)
		}
	}

	return localErr
}

// Recv receives new hardware sensor updates
func (s *Service) Recv() error {
	batch := <-s.streamch
	return s.recvBatch(batch)
}

// SourceIDs returns the currently present source ids, local ("") first and
// remotes in stable order.
func (s *Service) SourceIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := make([]string, 0, len(s.sources))
	for id := range s.sources {
		ids = append(ids, id)
	}
	sort.Strings(ids) // "" sorts first, then remote0, remote1, ...
	return ids
}

// Shmem provides access to a source's underlying hwinfo shared memory;
// sourceID "" is the local machine.
func (s *Service) Shmem(sourceID string) (*hwinfo.SharedMemory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if src, ok := s.sources[sourceID]; ok && src.shmem != nil {
		return src.shmem, nil
	}
	return nil, fmt.Errorf("source %q unavailable", sourceID)
}

// SensorIDByIdx returns the ordered slice of sensor IDs of one source
func (s *Service) SensorIDByIdx(sourceID string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	src, ok := s.sources[sourceID]
	if !ok || src.shmem == nil {
		return nil, fmt.Errorf("source %q unavailable", sourceID)
	}
	return s.sensorIDByIdxLocked(src), nil
}

func (s *Service) sensorIDByIdxLocked(src *sourceData) []string {
	if len(src.sensorIDByIdx) == 0 {
		for sens := range src.shmem.IterSensors() {
			src.sensorIDByIdx = append(src.sensorIDByIdx, sens.ID())
		}
	}
	return src.sensorIDByIdx
}

// ReadingsBySensorID returns the readings for a plain (source-local) sensor
// id within one source
func (s *Service) ReadingsBySensorID(sourceID, id string) ([]hwinfo.Reading, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	src, ok := s.sources[sourceID]
	if !ok || src.shmem == nil {
		return nil, fmt.Errorf("source %q unavailable", sourceID)
	}

	if !src.readingsBuilt {
		sids := s.sensorIDByIdxLocked(src)
		src.readingsBySensorID = make(map[string][]hwinfo.Reading)
		for r := range src.shmem.IterReadings() {
			sidx := int(r.SensorIndex())
			if sidx < len(sids) {
				sid := sids[sidx]
				src.readingsBySensorID[sid] = append(src.readingsBySensorID[sid], r)
			} else {
				return nil, fmt.Errorf("sensor at index %d out of range ", sidx)
			}
		}
		src.readingsBuilt = true
	}

	readings, ok := src.readingsBySensorID[id]
	if !ok {
		return nil, fmt.Errorf("readings for sensor id %s do not exist", id)
	}
	return readings, nil
}
