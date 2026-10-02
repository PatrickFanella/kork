// Package qr renders QR codes as compact SVG.
package qr

import (
	"errors"
	"fmt"
	"strings"

	"rsc.io/qr"
)

const quiet = 4

// MaxLen bounds the payload so codes stay scannable.
const MaxLen = 1200

// SVG returns a standalone SVG QR code drawn in fg on bg. Colors must already
// be trusted values (they are validated #rrggbb theme colors or literals).
func SVG(data, fg, bg string) (string, error) {
	if data == "" {
		return "", errors.New("empty QR payload")
	}
	if len(data) > MaxLen {
		return "", errors.New("QR payload is too long")
	}
	code, err := qr.Encode(data, qr.M)
	if err != nil {
		return "", err
	}
	n := code.Size + 2*quiet
	var path strings.Builder
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; {
			if !code.Black(x, y) {
				x++
				continue
			}
			start := x
			for x < code.Size && code.Black(x, y) {
				x++
			}
			fmt.Fprintf(&path, "M%d %dh%dv1h-%dz", start+quiet, y+quiet, x-start, x-start)
		}
	}
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges" role="img"><rect width="%d" height="%d" fill="%s"/><path fill="%s" d="%s"/></svg>`,
		n, n, n, n, bg, fg, path.String()), nil
}
