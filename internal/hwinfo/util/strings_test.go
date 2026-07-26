package util

import "testing"

func TestDecodeLegacyKeepsASCIIAndUTF8(t *testing.T) {
	for _, s := range []string{"CPU Package", "RPM", "°C"} {
		if got := DecodeLegacy(s); got != s {
			t.Errorf("DecodeLegacy(%q) = %q, want unchanged", s, got)
		}
	}
}

func TestDecodeLegacyNonLatinCodepageIsMojibake(t *testing.T) {
	// "Ядро" in CP1251. Without the v2 utf fields there is no way to know
	// the codepage, so the ISO8859-1 approximation yields mojibake; this
	// pins the (known-lossy) legacy behavior that PickUTF must bypass.
	cp1251 := "\xdf\xe4\xf0\xee"
	if got := DecodeLegacy(cp1251); got == "Ядро" {
		t.Fatalf("DecodeLegacy unexpectedly decoded CP1251 correctly: %q", got)
	}
}

func TestPickUTFPrefersValidUTF(t *testing.T) {
	utf := append([]byte("Ядро"), 0, 0)
	if got := PickUTF(utf, "mojibake"); got != "Ядро" {
		t.Errorf("PickUTF = %q, want utf field", got)
	}
}

func TestPickUTFFallsBack(t *testing.T) {
	legacy := "CPU Package"
	if got := PickUTF(nil, legacy); got != legacy {
		t.Errorf("PickUTF(nil) = %q, want legacy", got)
	}
	empty := make([]byte, 8)
	if got := PickUTF(empty, legacy); got != legacy {
		t.Errorf("PickUTF(empty) = %q, want legacy", got)
	}
	invalid := append([]byte{0xff, 0xfe, 0x41}, 0)
	if got := PickUTF(invalid, legacy); got != legacy {
		t.Errorf("PickUTF(invalid) = %q, want legacy", got)
	}
}

func TestCString(t *testing.T) {
	if got := CString([]byte{'a', 'b', 0, 'c'}); got != "ab" {
		t.Errorf("CString = %q, want ab", got)
	}
	if got := CString([]byte("abc")); got != "abc" {
		t.Errorf("CString without NUL = %q, want abc", got)
	}
}
