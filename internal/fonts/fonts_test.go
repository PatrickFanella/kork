package fonts

import (
	"encoding/binary"
	"testing"
)

// woff builds a minimal WOFF/WOFF2-shaped file whose header declares its length.
func woff(sig string, size int) []byte {
	b := make([]byte, size)
	copy(b, sig)
	binary.BigEndian.PutUint32(b[8:12], uint32(size))
	return b
}

func sfnt(version string, tables int) []byte {
	b := make([]byte, 12+16*tables)
	copy(b, version)
	binary.BigEndian.PutUint16(b[4:6], uint16(tables))
	return b
}

func TestSniff(t *testing.T) {
	badLen := woff("wOF2", 100)
	binary.BigEndian.PutUint32(badLen[8:12], 99)
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01")
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"woff2", woff("wOF2", 100), WOFF2},
		{"woff", woff("wOFF", 100), WOFF},
		{"truetype", sfnt("\x00\x01\x00\x00", 4), TTF},
		{"opentype", sfnt("OTTO", 9), OTF},
		{"woff2 wrong length", badLen, ""},
		{"woff2 truncated header", woff("wOF2", 20), ""},
		{"truetype without tables", sfnt("\x00\x01\x00\x00", 0), ""},
		{"truetype truncated table directory", sfnt("\x00\x01\x00\x00", 4)[:40], ""},
		{"png", png, ""},
		{"svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), ""},
		{"html", []byte("<!doctype html><script>alert(1)</script>"), ""},
		{"empty", nil, ""},
		{"lowercase signature", woff("wof2", 100), ""},
		{"true apple signature", sfnt("true", 4), ""},
	}
	for _, c := range cases {
		if got := Sniff(c.data); got != c.want {
			t.Errorf("%s: Sniff = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestIsFont(t *testing.T) {
	for _, ct := range []string{WOFF2, WOFF, TTF, OTF} {
		if !IsFont(ct) {
			t.Errorf("%s not a font", ct)
		}
	}
	if IsFont("image/png") || IsFont("font/collection") {
		t.Error("non-font accepted")
	}
}
