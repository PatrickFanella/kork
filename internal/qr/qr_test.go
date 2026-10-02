package qr

import (
	"strings"
	"testing"
)

func TestSVG(t *testing.T) {
	svg, err := SVG("https://links.example.com/alice", "#000000", "#ffffff")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(svg, "<svg") || !strings.Contains(svg, `d="M`) {
		t.Fatalf("unexpected svg: %.80s", svg)
	}
	if _, err := SVG("", "#000", "#fff"); err == nil {
		t.Error("empty payload accepted")
	}
	if _, err := SVG(strings.Repeat("a", MaxLen+1), "#000", "#fff"); err == nil {
		t.Error("oversized payload accepted")
	}
}
