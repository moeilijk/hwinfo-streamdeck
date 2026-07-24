//go:build windows

package util

import "C"

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"unsafe"

	"golang.org/x/text/encoding/charmap"
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
	return s[:strings.IndexByte(s, 0)]
}

// DecodeCharPtr decodes ISO8859_1 string to UTF-8
func DecodeCharPtr(ptr unsafe.Pointer, len int) string {
	s := goStringFromPtr(ptr, len)
	ds, err := decodeISO8859_1(s)
	if err != nil {
		log.Fatalf("TODO: failed to decode: %v", err)
	}
	return ds
}

var isodecoder = charmap.ISO8859_1.NewDecoder()

func decodeISO8859_1(in string) (string, error) {
	return isodecoder.String(in)
}
