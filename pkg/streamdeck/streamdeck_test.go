package streamdeck

import (
	"regexp"
	"testing"
)

// openDeckBase64Re mirrors OpenDeck's frontend validation
// (src/lib/rendererHelper.ts getImage). If the base64 payload does not start
// immediately after "base64," (capture group 2 empty), OpenDeck silently
// replaces the image with the action's default icon — the tile then never
// shows sensor data there (lhm-streamdeck #74, hwinfo-streamdeck PR #130).
var openDeckBase64Re = regexp.MustCompile(`^data:image/(apng|avif|gif|jpeg|png|svg\+xml|webp|bmp|x-icon|tiff);base64,([A-Za-z0-9+/]+={0,2})?`)

func TestSetImageDataURLAcceptedByStrictHosts(t *testing.T) {
	url := setImageDataURL([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})

	m := openDeckBase64Re.FindStringSubmatch(url)
	if m == nil {
		t.Fatalf("data URL %q does not match the strict base64 regex at all", url)
	}
	if m[2] == "" {
		t.Fatalf("data URL %q matches with empty base64 group: strict hosts fall back to the default image", url)
	}
}

func TestSetImageDataURLHasNoWhitespace(t *testing.T) {
	url := setImageDataURL([]byte("x"))
	if regexp.MustCompile(`\s`).MatchString(url) {
		t.Fatalf("data URL %q contains whitespace", url)
	}
}
