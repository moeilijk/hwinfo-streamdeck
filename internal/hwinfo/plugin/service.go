//go:build windows

package plugin

import (
	"fmt"
	"sync"

	"github.com/moeilijk/hwinfo-streamdeck/internal/hwinfo"
)

// Service wraps hwinfo shared mem streaming
// and provides convenient methods for data access
type Service struct {
	streamch           <-chan hwinfo.Result
	mu                 sync.RWMutex
	sensorIDByIdx      []string
	readingsBySensorID map[string][]hwinfo.Reading
	shmem              *hwinfo.SharedMemory
	readingsBuilt      bool
}

// Start starts the service providing updating hardware info
func StartService() *Service {
	return &Service{
		streamch: hwinfo.StreamSharedMem(),
	}
}

func (s *Service) recvShmem(shmem *hwinfo.SharedMemory) error {
	if shmem == nil {
		return fmt.Errorf("shmem nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.shmem = shmem

	s.sensorIDByIdx = s.sensorIDByIdx[:0]
	for k, v := range s.readingsBySensorID {
		s.readingsBySensorID[k] = v[:0]
	}
	s.readingsBuilt = false

	return nil
}

// Recv receives new hardware sensor updates
func (s *Service) Recv() error {
	select {
	case r := <-s.streamch:
		if r.Err != nil {
			return r.Err
		}
		return s.recvShmem(r.Shmem)
	}
}

// Shmem provides access to underlying hwinfo shared memory
func (s *Service) Shmem() (*hwinfo.SharedMemory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.shmem != nil {
		return s.shmem, nil
	}
	return nil, fmt.Errorf("shmem nil")
}

// SensorIDByIdx returns ordered slice of sensor IDs
func (s *Service) SensorIDByIdx() ([]string, error) {
	s.mu.RLock()
	if len(s.sensorIDByIdx) > 0 {
		defer s.mu.RUnlock()
		return s.sensorIDByIdx, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	for sens := range s.shmem.IterSensors() {
		s.sensorIDByIdx = append(s.sensorIDByIdx, sens.ID())
	}

	return s.sensorIDByIdx, nil
}

// ReadingsBySensorID returns slice of hwinfoReading for a given sensor ID
func (s *Service) ReadingsBySensorID(id string) ([]hwinfo.Reading, error) {
	s.mu.RLock()
	if s.readingsBySensorID != nil && s.readingsBuilt {
		defer s.mu.RUnlock()
		readings, ok := s.readingsBySensorID[id]
		if !ok {
			return nil, fmt.Errorf("readings for sensor id %s do not exist", id)
		}
		return readings, nil
	}
	s.mu.RUnlock()

	sids, err := s.SensorIDByIdx()
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.readingsBySensorID == nil {
		s.readingsBySensorID = make(map[string][]hwinfo.Reading)
	}

	for r := range s.shmem.IterReadings() {
		sidx := int(r.SensorIndex())
		if sidx < len(sids) {
			sid := sids[sidx]
			s.readingsBySensorID[sid] = append(s.readingsBySensorID[sid], r)
		} else {
			return nil, fmt.Errorf("sensor at index %d out of range ", sidx)
		}
	}
	s.readingsBuilt = true

	readings, ok := s.readingsBySensorID[id]
	if !ok {
		return nil, fmt.Errorf("readings for sensor id %s do not exist", id)
	}
	return readings, nil
}
