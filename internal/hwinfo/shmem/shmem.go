//go:build windows

package shmem

/*
#include <windows.h>
#include "../hwisenssm2.h"
*/
import "C"

import (
	"fmt"
	"reflect"
	"syscall"
	"unsafe"

	"github.com/moeilijk/hwinfo-streamdeck/internal/hwinfo/mutex"
	"github.com/moeilijk/hwinfo-streamdeck/internal/hwinfo/util"
	"golang.org/x/sys/windows"
)

var buf = make([]byte, 200000)

func copyBytes(addr uintptr) []byte {
	headerLen := C.sizeof_HWiNFO_SENSORS_SHARED_MEM2

	var d []byte
	dh := (*reflect.SliceHeader)(unsafe.Pointer(&d))

	dh.Data = addr
	dh.Len, dh.Cap = headerLen, headerLen

	cheader := C.PHWiNFO_SENSORS_SHARED_MEM2(unsafe.Pointer(&d[0]))
	fullLen := int(cheader.dwOffsetOfReadingSection + (cheader.dwSizeOfReadingElement * cheader.dwNumReadingElements))

	if fullLen > cap(buf) {
		buf = append(buf, make([]byte, fullLen-cap(buf))...)
	}

	dh.Len, dh.Cap = fullLen, fullLen

	copy(buf, d)

	return buf[:fullLen]
}

// ReadBytes copies bytes from global shared memory
func ReadBytes() ([]byte, error) {
	err := mutex.Lock()
	defer mutex.Unlock()
	if err != nil {
		return nil, err
	}

	hnd, err := openFileMapping()
	if err != nil {
		return nil, err
	}
	addr, err := mapViewOfFile(hnd)
	if err != nil {
		return nil, err
	}
	defer unmapViewOfFile(addr)
	defer windows.CloseHandle(windows.Handle(unsafe.Pointer(hnd)))

	return copyBytes(addr), nil
}

func openFileMapping() (C.HANDLE, error) {
	lpName := C.CString(C.HWiNFO_SENSORS_MAP_FILE_NAME2)
	defer C.free(unsafe.Pointer(lpName))

	hnd := C.OpenFileMapping(syscall.FILE_MAP_READ, 0, lpName)
	if hnd == C.HANDLE(C.NULL) {
		errstr := util.HandleLastError(uint64(C.GetLastError()))
		return nil, fmt.Errorf("OpenFileMapping: %w", errstr)
	}

	return hnd, nil
}

func mapViewOfFile(hnd C.HANDLE) (uintptr, error) {
	addr, err := windows.MapViewOfFile(windows.Handle(unsafe.Pointer(hnd)), C.FILE_MAP_READ, 0, 0, 0)
	if err != nil {
		return 0, fmt.Errorf("MapViewOfFile: %w", err)
	}

	return addr, nil
}

func unmapViewOfFile(ptr uintptr) error {
	err := windows.UnmapViewOfFile(ptr)
	if err != nil {
		return fmt.Errorf("UnmapViewOfFile: %w", err)
	}
	return nil
}
