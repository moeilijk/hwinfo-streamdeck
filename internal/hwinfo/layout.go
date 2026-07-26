package hwinfo

// Byte offsets and lengths within the packed shared-memory elements
// (pragma pack(1), see hwisenssm2.h). The v2 layout (dwVersion >= 2,
// HWiNFO v7.33+) appends UTF-8 string fields at the end of each element;
// dwSizeOf*Element decides whether they are present.
const (
	stringLen2    = 128
	unitStringLen = 16

	// HWiNFO_SENSORS_READING_ELEMENT
	offReadingLabelOrig    = 4 + 4 + 4
	offReadingLabelUser    = offReadingLabelOrig + stringLen2
	offReadingUnit         = offReadingLabelUser + stringLen2
	offReadingValue        = offReadingUnit + unitStringLen
	offReadingUTFLabelUser = offReadingValue + 4*8
	offReadingUTFUnit      = offReadingUTFLabelUser + stringLen2
	sizeReadingElementV1   = offReadingUTFLabelUser
	sizeReadingElementV2   = offReadingUTFUnit + unitStringLen

	// HWiNFO_SENSORS_SENSOR_ELEMENT
	offSensorNameOrig    = 4 + 4
	offSensorNameUser    = offSensorNameOrig + stringLen2
	offSensorUTFNameUser = offSensorNameUser + stringLen2
	sizeSensorElementV1  = offSensorUTFNameUser
	sizeSensorElementV2  = offSensorUTFNameUser + stringLen2
)

// utfField returns the UTF-8 field at [off, off+length) of a packed element,
// or nil when the element (sized by dwSizeOf*Element) predates that field.
func utfField(elem []byte, off, length int) []byte {
	if len(elem) < off+length {
		return nil
	}
	return elem[off : off+length]
}
