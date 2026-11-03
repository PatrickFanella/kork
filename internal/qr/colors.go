package qr

import (
	"math"
	"strconv"
)

// Plain colors, used when no theme pair is safe to scan.
const (
	PlainFG = "#000000"
	PlainBG = "#ffffff"
)

// MinContrast is the contrast ratio a themed code needs. It is stricter than
// text contrast because camera apps read codes at an angle, in poor light.
const MinContrast = 7.0

// luminance is the WCAG relative luminance of a #rrggbb color, or -1 when
// the color isn't #rrggbb.
func luminance(hex string) float64 {
	if len(hex) != 7 || hex[0] != '#' {
		return -1
	}
	var c [3]float64
	for i := range c {
		v, err := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		if err != nil {
			return -1
		}
		s := float64(v) / 255
		if s <= 0.04045 {
			c[i] = s / 12.92
		} else {
			c[i] = math.Pow((s+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2]
}

// Contrast is the WCAG contrast ratio of two #rrggbb colors, or 0 when one
// isn't a valid color.
func Contrast(a, b string) float64 {
	la, lb := luminance(a), luminance(b)
	if la < 0 || lb < 0 {
		return 0
	}
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// Scannable reports whether fg on bg can be used for a code: contrast of at
// least MinContrast with a darker foreground, since some readers fail on
// light-on-dark (inverted) codes.
func Scannable(fg, bg string) bool {
	return luminance(fg) >= 0 && luminance(fg) < luminance(bg) && Contrast(fg, bg) >= MinContrast
}

// Pick returns the first scannable {fg, bg} pair, or black on white.
func Pick(pairs ...[2]string) (fg, bg string) {
	for _, p := range pairs {
		if Scannable(p[0], p[1]) {
			return p[0], p[1]
		}
	}
	return PlainFG, PlainBG
}
