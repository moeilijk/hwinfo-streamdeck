//go:build windows

package util

import "C"

import (
	"errors"
	"fmt"
	"strings"
	"unsafe"
)

// ErrFileNotFound Windows error
var ErrFileNotFound = errors.New("file not found")

// ErrInvalidHandle Windows error
var ErrInvalidHandle = errors.New("invalid handle")

// UnknownError unhandled Windows error
type UnknownError struct {
	Code uint64
}

func (e UnknownError) Error() string {
	return fmt.Sprintf("unknown error code: %d", e.Code)
}

// HandleLastError converts C.GetLastError() to golang error
func HandleLastError(code uint64) error {
	switch code {
	case 2: // ERROR_FILE_NOT_FOUND
		return ErrFileNotFound
	case 6: // ERROR_INVALID_HANDLE
		return ErrInvalidHandle
	default:
		return UnknownError{Code: code}
	}
}

func goStringFromPtr(ptr unsafe.Pointer, len int) string {
	s := C.GoStringN((*C.char)(ptr), C.int(len))
	// Fixed-width fields (e.g. the 4-byte signature "HWiS") may lack a NUL.
	if i := strings.IndexByte(s, 0); i >= 0 {
		return s[:i]
	}
	return s
}

// DecodeCharPtr decodes a legacy (ANSI) shared-memory string; see
// DecodeLegacy for the exact semantics and its limits. The UTF-8 fields
// added in Shared Memory v2 (HWiNFO v7.33+) are read elsewhere and should
// be preferred over these fields whenever present.
func DecodeCharPtr(ptr unsafe.Pointer, len int) string {
	return DecodeLegacy(goStringFromPtr(ptr, len))
}
