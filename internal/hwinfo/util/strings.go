package util

import (
	"bytes"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// CString returns the string up to the first NUL byte of b.
func CString(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		return string(b[:i])
	}
	return string(b)
}

// DecodeLegacy decodes a legacy shared-memory string. These fields are ANSI
// in the codepage of the Windows locale, which we cannot identify from the
// bytes alone: valid UTF-8 passes through untouched (covers plain ASCII and
// tools that already write UTF-8 here), anything else is approximated as
// ISO8859-1. Non-Latin codepages (Cyrillic, CJK, ...) therefore come out as
// mojibake; use PickUTF with the v2 utf fields to avoid that.
func DecodeLegacy(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	ds, err := isodecoder.String(s)
	if err != nil {
		return s
	}
	return ds
}

// PickUTF returns the v2 UTF-8 field when it is present (non-nil), non-empty,
// and valid UTF-8; otherwise it falls back to the decoded legacy string.
func PickUTF(utf []byte, legacy string) string {
	if s := CString(utf); s != "" && utf8.ValidString(s) {
		return s
	}
	return legacy
}

var isodecoder = charmap.ISO8859_1.NewDecoder()
