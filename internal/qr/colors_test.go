package qr

import (
	"math"
	"testing"
)

func TestContrast(t *testing.T) {
	if c := Contrast("#000000", "#ffffff"); math.Abs(c-21) > 0.01 {
		t.Fatalf("black/white = %v", c)
	}
	if c := Contrast("#777777", "#777777"); math.Abs(c-1) > 0.01 {
		t.Fatalf("same color = %v", c)
	}
	if Contrast("red", "#ffffff") != 0 {
		t.Fatal("invalid color should give 0")
	}
}

func TestPick(t *testing.T) {
	cases := []struct {
		name           string
		pairs          [][2]string
		wantFG, wantBG string
	}{
		{"dark on light", [][2]string{{"#1a1a2e", "#f5f0e6"}}, "#1a1a2e", "#f5f0e6"},
		{"inverted falls back", [][2]string{{"#ffffff", "#000000"}}, PlainFG, PlainBG},
		{"low contrast falls back", [][2]string{{"#555555", "#888888"}}, PlainFG, PlainBG},
		{"second pair used", [][2]string{{"#555555", "#888888"}, {"#111111", "#eeeeee"}}, "#111111", "#eeeeee"},
		{"no pairs", nil, PlainFG, PlainBG},
	}
	for _, c := range cases {
		fg, bg := Pick(c.pairs...)
		if fg != c.wantFG || bg != c.wantBG {
			t.Errorf("%s: %s on %s", c.name, fg, bg)
		}
	}
}
