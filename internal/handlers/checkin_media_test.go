package handlers

import (
	"encoding/base64"
	"testing"
)

func TestDecodeDataImage(t *testing.T) {
	jpegBytes := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'}
	good := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(jpegBytes)

	if got := decodeDataImage(good); len(got) != len(jpegBytes) {
		t.Errorf("valid JPEG data URL: got %d bytes, want %d", len(got), len(jpegBytes))
	}
	// PNG mime is rejected (we only embed JPEG).
	png := "data:image/png;base64," + base64.StdEncoding.EncodeToString(jpegBytes)
	if got := decodeDataImage(png); got != nil {
		t.Error("non-JPEG mime should be rejected")
	}
	// Right mime but bytes aren't a JPEG (no SOI magic).
	notJpeg := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte("hello there"))
	if got := decodeDataImage(notJpeg); got != nil {
		t.Error("non-JPEG bytes should be rejected")
	}
	// Blanks and garbage.
	for _, s := range []string{"", "   ", "not-a-data-url", "data:image/jpeg;base64,@@@"} {
		if got := decodeDataImage(s); got != nil {
			t.Errorf("garbage %q should decode to nil, got %d bytes", s, len(got))
		}
	}
}

func TestSealedSigDigest(t *testing.T) {
	cases := map[string]string{
		"Jordan Avery · drawn:sha256:abcd1234": "abcd1234",
		"Jordan Avery":                         "", // typed signature, no drawn image
		"":                                     "",
	}
	for in, want := range cases {
		if got := sealedSigDigest(in); got != want {
			t.Errorf("sealedSigDigest(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPacketSlug(t *testing.T) {
	cases := []struct{ name, idn, want string }{
		{"Jordan Avery", "1234567", "jordan_avery_1234567"},
		{"O'Brien, Mary-Kate", "42", "obrien_marykate_42"},
		{"", "99", "client_99"},
	}
	for _, c := range cases {
		if got := packetSlug(c.name, c.idn); got != c.want {
			t.Errorf("packetSlug(%q,%q) = %q, want %q", c.name, c.idn, got, c.want)
		}
	}
}
