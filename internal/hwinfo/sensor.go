//go:build windows

package hwinfo

/*
#include <windows.h>
#include "hwisenssm2.h"
*/
import "C"

import (
	"strconv"
	"unsafe"

	"github.com/moeilijk/hwinfo-streamdeck/internal/hwinfo/util"
)

// Sensor element (e.g. motherboard, cpu, gpu...)
type Sensor struct {
	cs C.PHWiNFO_SENSORS_SENSOR_ELEMENT
}

// NewSensor constructs a Sensor
func NewSensor(data []byte) Sensor {
	return Sensor{
		cs: C.PHWiNFO_SENSORS_SENSOR_ELEMENT(unsafe.Pointer(&data[0])),
	}
}

// SensorID a unique Sensor ID
func (s *Sensor) SensorID() uint64 {
	return uint64(s.cs.dwSensorID)
}

// SensorInst the instance of the sensor (together with SensorID forms a unique ID)
func (s *Sensor) SensorInst() uint64 {
	return uint64(s.cs.dwSensorInst)
}

// ID a unique ID combining SensorID and SensorInst
func (s *Sensor) ID() string {
	// keeping old method used in legacy steam deck plugin
	return strconv.FormatUint(s.SensorID()*100+s.SensorInst(), 10)
}

// NameOrig original name of sensor
func (s *Sensor) NameOrig() string {
	return util.DecodeCharPtr(unsafe.Pointer(&s.cs.szSensorNameOrig), C.HWiNFO_SENSORS_STRING_LEN2)
}

// NameUser sensor name displayed, which might have been renamed by user
func (s *Sensor) NameUser() string {
	return util.DecodeCharPtr(unsafe.Pointer(&s.cs.szSensorNameUser), C.HWiNFO_SENSORS_STRING_LEN2)
}
