//go:build windows

package hwinfo

/*
#include <windows.h>
#include "hwisenssm2.h"
*/
import "C"

import (
	"fmt"
	"log"
	"time"
	"unsafe"

	"github.com/moeilijk/hwinfo-streamdeck/internal/hwinfo/shmem"
	"github.com/moeilijk/hwinfo-streamdeck/internal/hwinfo/util"
)

// SharedMemory provides access to the HWiNFO shared memory
type SharedMemory struct {
	data  []byte
	shmem C.PHWiNFO_SENSORS_SHARED_MEM2
}

// ReadSharedMem reads data from HWiNFO shared memory
// creating a copy of the data
func ReadSharedMem() (*SharedMemory, error) {
	data, err := shmem.ReadBytes()
	if err != nil {
		return nil, err
	}

	return &SharedMemory{
		data:  append([]byte(nil), data...),
		shmem: C.PHWiNFO_SENSORS_SHARED_MEM2(unsafe.Pointer(&data[0])),
	}, nil
}

// Result for streamed shared memory updates
type Result struct {
	Shmem *SharedMemory
	Err   error
}

func readAndSend(ch chan<- Result) {
	shmem, err := ReadSharedMem()
	ch <- Result{Shmem: shmem, Err: err}
}

// StreamSharedMem delivers shared memory hardware sensors updates
// over a channel
func StreamSharedMem() <-chan Result {
	ch := make(chan Result)
	go func() {
		readAndSend(ch)
		// TODO: don't use time.Tick, cancellable?
		for range time.Tick(1 * time.Second) {
			readAndSend(ch)
		}
	}()
	return ch
}

// Signature "HWiS" if active, 'DEAD' when inactive
func (s *SharedMemory) Signature() string {
	return util.DecodeCharPtr(unsafe.Pointer(&s.shmem.dwSignature), C.sizeof_DWORD)
}

// Version v1 is latest
func (s *SharedMemory) Version() int {
	return int(s.shmem.dwVersion)
}

// Revision revision of version
func (s *SharedMemory) Revision() int {
	return int(s.shmem.dwRevision)
}

// PollTime last polling time
func (s *SharedMemory) PollTime() uint64 {
	addr := unsafe.Pointer(uintptr(unsafe.Pointer(&s.shmem.dwRevision)) + C.sizeof_DWORD)
	return uint64(*(*C.__time64_t)(addr))
}

// OffsetOfSensorSection offset of the Sensor section from beginning of HWiNFO_SENSORS_SHARED_MEM2
func (s *SharedMemory) OffsetOfSensorSection() int {
	return int(s.shmem.dwOffsetOfSensorSection)
}

// SizeOfSensorElement size of each sensor element = sizeof( HWiNFO_SENSORS_SENSOR_ELEMENT )
func (s *SharedMemory) SizeOfSensorElement() int {
	return int(s.shmem.dwSizeOfSensorElement)
}

// NumSensorElements number of sensor elements
func (s *SharedMemory) NumSensorElements() int {
	return int(s.shmem.dwNumSensorElements)
}

// OffsetOfReadingSection offset of the Reading section from beginning of HWiNFO_SENSORS_SHARED_MEM2
func (s *SharedMemory) OffsetOfReadingSection() int {
	return int(s.shmem.dwOffsetOfReadingSection)
}

// SizeOfReadingElement size of each Reading element = sizeof( HWiNFO_SENSORS_READING_ELEMENT )
func (s *SharedMemory) SizeOfReadingElement() int {
	return int(s.shmem.dwSizeOfReadingElement)
}

// NumReadingElements number of Reading elements
func (s *SharedMemory) NumReadingElements() int {
	return int(s.shmem.dwNumReadingElements)
}

func (s *SharedMemory) dataForSensor(pos int) ([]byte, error) {
	if pos >= s.NumSensorElements() {
		return nil, fmt.Errorf("dataForSensor pos out of range, %d for size %d", pos, s.NumSensorElements())
	}
	start := s.OffsetOfSensorSection() + (pos * s.SizeOfSensorElement())
	end := start + s.SizeOfSensorElement()
	return s.data[start:end], nil
}

// IterSensors iterate over each sensor
func (s *SharedMemory) IterSensors() <-chan Sensor {
	ch := make(chan Sensor)
	go func() {
		for i := 0; i < s.NumSensorElements(); i++ {
			data, err := s.dataForSensor(i)
			if err != nil {
				log.Fatalf("TODO: failed to read dataForSensor: %v", err)
			}
			ch <- NewSensor(data)
		}
		close(ch)
	}()
	return ch
}

func (s *SharedMemory) dataForReading(pos int) ([]byte, error) {
	if pos >= s.NumReadingElements() {
		return nil, fmt.Errorf("dataForReading pos out of range, %d for size %d", pos, s.NumSensorElements())
	}
	start := s.OffsetOfReadingSection() + (pos * s.SizeOfReadingElement())
	end := start + s.SizeOfReadingElement()
	return s.data[start:end], nil
}

// IterReadings iterate over each sensor
func (s *SharedMemory) IterReadings() <-chan Reading {
	ch := make(chan Reading)
	go func() {
		for i := 0; i < s.NumReadingElements(); i++ {
			data, err := s.dataForReading(i)
			if err != nil {
				log.Fatalf("TODO: failed to read dataForReading: %v", err)
			}
			ch <- NewReading(data)
		}
		close(ch)
	}()
	return ch
}
