package hwsensorsservice

import (
	"fmt"
	"strconv"
	"strings"
)

// Sensors of remote machines (HWiNFO remote shared memory) are exposed with a
// source-qualified sensor UID: "<sourceID>::<sensorID>". Local sensors keep
// their plain UID, so settings stored by older versions stay valid unchanged.
// The separator never occurs in HWiNFO's numeric sensor ids nor in the mock
// bridge's path-style ids.
const sourceUIDSep = "::"

// LocalSourceID identifies the local machine's shared memory.
const LocalSourceID = ""

// ComposeSensorUID qualifies a plain sensor id with its source.
func ComposeSensorUID(sourceID, sensorID string) string {
	if sourceID == LocalSourceID {
		return sensorID
	}
	return sourceID + sourceUIDSep + sensorID
}

// SplitSensorUID splits a (possibly source-qualified) sensor UID into the
// source id ("" for local) and the plain sensor id.
func SplitSensorUID(uid string) (sourceID, sensorID string) {
	if i := strings.Index(uid, sourceUIDSep); i >= 0 {
		return uid[:i], uid[i+len(sourceUIDSep):]
	}
	return LocalSourceID, uid
}

// RemoteSourceID names the shared-memory source of remote machine index i.
func RemoteSourceID(i int) string {
	return "remote" + strconv.Itoa(i)
}

// SourceDisplayName is the user-facing name of a source. The shared memory
// does not expose remote machine names, so remotes are numbered.
func SourceDisplayName(sourceID string) string {
	if sourceID == LocalSourceID {
		return "Local"
	}
	if n, ok := strings.CutPrefix(sourceID, "remote"); ok {
		if i, err := strconv.Atoi(n); err == nil {
			return fmt.Sprintf("Remote %d", i+1)
		}
	}
	return sourceID
}
