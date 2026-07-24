//go:build windows

package hwinfo

/*
#include <windows.h>
#include "hwisenssm2.h"
*/
import "C"
import (
	"unsafe"

	"github.com/moeilijk/hwinfo-streamdeck/internal/hwinfo/util"
)

// ReadingType enum of value/unit type for reading
type ReadingType int

const (
	// ReadingTypeNone no type
	ReadingTypeNone ReadingType = iota
	// ReadingTypeTemp temperature in celsius
	ReadingTypeTemp
	// ReadingTypeVolt voltage
	ReadingTypeVolt
	// ReadingTypeFan RPM
	ReadingTypeFan
	// ReadingTypeCurrent amps
	ReadingTypeCurrent
	// ReadingTypePower watts
	ReadingTypePower
	// ReadingTypeClock Mhz
	ReadingTypeClock
	// ReadingTypeUsage e.g. MBs
	ReadingTypeUsage
	// ReadingTypeOther other
	ReadingTypeOther
)

func (t ReadingType) String() string {
	return [...]string{"None", "Temp", "Volt", "Fan", "Current", "Power", "Clock", "Usage", "Other"}[t]
}

// Reading element (e.g. usage, power, mhz...)
type Reading struct {
	cr C.PHWiNFO_SENSORS_READING_ELEMENT
}

// NewReading contructs a Reading
func NewReading(data []byte) Reading {
	return Reading{
		cr: C.PHWiNFO_SENSORS_READING_ELEMENT(unsafe.Pointer(&data[0])),
	}
}

// ID unique ID of the reading within a particular sensor
func (r *Reading) ID() int32 {
	return int32(r.cr.dwReadingID)
}

// Type of sensor reading
func (r *Reading) Type() ReadingType {
	return ReadingType(r.cr.tReading)
}

// SensorIndex this is the index of sensor in the Sensors[] array to
// which this reading belongs to
func (r *Reading) SensorIndex() uint64 {
	return uint64(r.cr.dwSensorIndex)
}

// ReadingID a unique ID of the reading within a particular sensor
func (r *Reading) ReadingID() uint64 {
	return uint64(r.cr.dwReadingID)
}

// LabelOrig original label (e.g. "Chassis2 Fan")
func (r *Reading) LabelOrig() string {
	return util.DecodeCharPtr(unsafe.Pointer(&r.cr.szLabelOrig), C.HWiNFO_SENSORS_STRING_LEN2)
}

// LabelUser label displayed, which might have been renamed by user
func (r *Reading) LabelUser() string {
	return util.DecodeCharPtr(unsafe.Pointer(&r.cr.szLabelUser), C.HWiNFO_SENSORS_STRING_LEN2)
}

// Unit e.g. "RPM"
func (r *Reading) Unit() string {
	return util.DecodeCharPtr(unsafe.Pointer(&r.cr.szUnit), C.HWiNFO_UNIT_STRING_LEN)
}

func (r *Reading) valuePtr() unsafe.Pointer {
	return unsafe.Pointer(uintptr(unsafe.Pointer(&r.cr.szUnit)) + C.HWiNFO_UNIT_STRING_LEN)
}

// Value current value
func (r *Reading) Value() float64 {
	return float64(*(*C.double)(r.valuePtr()))
}

func (r *Reading) valueMinPtr() unsafe.Pointer {
	return unsafe.Pointer(uintptr(r.valuePtr()) + C.sizeof_double)
}

// ValueMin current value
func (r *Reading) ValueMin() float64 {
	return float64(*(*C.double)(r.valueMinPtr()))
}

func (r *Reading) valueMaxPtr() unsafe.Pointer {
	return unsafe.Pointer(uintptr(r.valueMinPtr()) + C.sizeof_double)
}

// ValueMax current value
func (r *Reading) ValueMax() float64 {
	return float64(*(*C.double)(r.valueMaxPtr()))
}

func (r *Reading) valueAvgPtr() unsafe.Pointer {
	return unsafe.Pointer(uintptr(r.valueMaxPtr()) + C.sizeof_double)
}

// ValueAvg current value
func (r *Reading) ValueAvg() float64 {
	return float64(*(*C.double)(r.valueAvgPtr()))
}
