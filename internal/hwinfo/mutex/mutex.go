//go:build windows

package mutex

/*
#include <windows.h>
#include "../hwisenssm2.h"
*/
import "C"
import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/moeilijk/hwinfo-streamdeck/internal/hwinfo/util"
)

var ghnd C.HANDLE
var imut = sync.Mutex{}

// Lock the global mutex
func Lock() error {
	imut.Lock()
	lpName := C.CString(C.HWiNFO_SENSORS_SM2_MUTEX)
	defer C.free(unsafe.Pointer(lpName))

	ghnd = C.OpenMutex(C.READ_CONTROL, C.FALSE, lpName)
	if ghnd == C.HANDLE(C.NULL) {
		errstr := util.HandleLastError(uint64(C.GetLastError()))
		return fmt.Errorf("failed to lock global mutex: %w", errstr)
	}

	return nil
}

// Unlock the global mutex
func Unlock() {
	defer imut.Unlock()
	C.CloseHandle(ghnd)
}
