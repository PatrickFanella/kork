// Package fonts identifies uploaded font files by their leading bytes.
package fonts

import "encoding/binary"

// MaxSize is the largest font upload, in bytes.
const MaxSize = 1 << 20

// Content types stored for font uploads.
const (
	WOFF2 = "font/woff2"
	WOFF  = "font/woff"
	TTF   = "font/ttf"
	OTF   = "font/otf"
)

// IsFont reports whether a stored content type is one of the font types.
func IsFont(contentType string) bool {
	switch contentType {
	case WOFF2, WOFF, TTF, OTF:
		return true
	}
	return false
}

// Sniff returns the font content type of data, or "" when data is not a
// WOFF2, WOFF, TrueType or OpenType (CFF) font. The client's declared type
// and file name are ignored. WOFF and WOFF2 must also declare their own
// length correctly; TrueType and OpenType need a plausible table count.
func Sniff(data []byte) string {
	if len(data) < 12 {
		return ""
	}
	switch string(data[:4]) {
	case "wOF2", "wOFF":
		// WOFF headers: signature, flavor, then the total file length.
		if len(data) < 44 || binary.BigEndian.Uint32(data[8:12]) != uint32(len(data)) {
			return ""
		}
		if data[3] == '2' {
			return WOFF2
		}
		return WOFF
	case "\x00\x01\x00\x00", "OTTO":
		// sfnt header: version, numTables, then a 16-byte record per table.
		n := int(binary.BigEndian.Uint16(data[4:6]))
		if n == 0 || n > 100 || len(data) < 12+16*n {
			return ""
		}
		if data[0] == 'O' {
			return OTF
		}
		return TTF
	}
	return ""
}
