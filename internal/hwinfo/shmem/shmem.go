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

// LocalMappingName is the file mapping of the local machine's sensors.
const LocalMappingName = C.HWiNFO_SENSORS_MAP_FILE_NAME2

// RemoteMappingPrefix + a 0-based index names the mapping of a remote
// machine monitored by HWiNFO (October 2023 shared-memory definitions).
const RemoteMappingPrefix = C.HWiNFO_SENSORS_MAP_FILE_NAME2_REMOTE

func copyBytes(addr uintptr) []byte {
	headerLen := C.sizeof_HWiNFO_SENSORS_SHARED_MEM2

	var d []byte
	dh := (*reflect.SliceHeader)(unsafe.Pointer(&d))

	dh.Data = addr
	dh.Len, dh.Cap = headerLen, headerLen

	cheader := C.PHWiNFO_SENSORS_SHARED_MEM2(unsafe.Pointer(&d[0]))
	fullLen := int(cheader.dwOffsetOfReadingSection + (cheader.dwSizeOfReadingElement * cheader.dwNumReadingElements))

	dh.Len, dh.Cap = fullLen, fullLen

	// Each source gets its own copy; the mapped view is unmapped right after.
	buf := make([]byte, fullLen)
	copy(buf, d)

	return buf
}

// ReadBytes copies bytes from the local global shared memory
func ReadBytes() ([]byte, error) {
	return ReadBytesNamed(LocalMappingName)
}

// ReadBytesNamed copies bytes from the named shared-memory mapping. A
// mapping that does not exist (remote machine not connected) returns
// util.ErrFileNotFound.
func ReadBytesNamed(name string) ([]byte, error) {
	err := mutex.Lock()
	defer mutex.Unlock()
	if err != nil {
		return nil, err
	}

	hnd, err := openFileMapping(name)
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

func openFileMapping(name string) (C.HANDLE, error) {
	lpName := C.CString(name)
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
