package server

import (
	"strings"
	"testing"

	"git.subcult.tv/subculture-collective/zelda/internal/catalog"
	"git.subcult.tv/subculture-collective/zelda/internal/qr"
)

// Every preset theme gets QR colors that are either its own scannable pair
// or black on white (#38).
func TestThemeQRColorsForEveryPreset(t *testing.T) {
	if len(catalog.Presets) != 10 {
		t.Fatalf("expected 10 presets, got %d", len(catalog.Presets))
	}
	for _, p := range catalog.Presets {
		th, _, err := p.Theme.Normalize()
		if err != nil {
			t.Fatalf("%s: %v", p.Key, err)
		}
		fg, bg := themeQRColors(th)
		c := th.Colors
		themed := (fg == c.Text && (bg == c.Surface || bg == c.BG))
		plain := fg == qr.PlainFG && bg == qr.PlainBG
		switch {
		case themed && !qr.Scannable(fg, bg):
			t.Errorf("%s: themed colors %s on %s fail the contrast rule", p.Key, fg, bg)
		case !themed && !plain:
			t.Errorf("%s: unexpected colors %s on %s", p.Key, fg, bg)
		case !themed && qr.Scannable(c.Text, c.Surface):
			t.Errorf("%s: fell back although text on surface is scannable", p.Key)
		}
		t.Logf("%-10s themed=%v %s on %s", p.Key, themed, fg, bg)
	}
}

func TestShareQRThemeColors(t *testing.T) {
	e := setup(t)
	alice, pageID := livePage(t, e, "alice@example.com", "alice")
	theme := map[string]any{"colors": map[string]any{"text": "#1a1a2e", "surface": "#f5f0e6", "bg": "#f5f0e6"}}
	if code := alice.do("PATCH", "/api/pages/"+pageID, map[string]any{"theme": theme}, nil); code != 200 {
		t.Fatalf("theme: %d", code)
	}
	_, plain, h := alice.get("/api/pages/" + pageID + "/qr.svg")
	if !strings.Contains(plain, `fill="#ffffff"`) || !strings.Contains(plain, `fill="#000000"`) || h.Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("plain QR: %s", plain)
	}
	_, themed, _ := alice.get("/api/pages/" + pageID + "/qr.svg?colors=theme")
	if !strings.HasPrefix(themed, "<svg") || !strings.Contains(themed, `fill="#f5f0e6"`) || !strings.Contains(themed, `fill="#1a1a2e"`) {
		t.Fatalf("themed QR: %s", themed)
	}
	// Inline codes on the public page use the same colors.
	if code, _, _ := alice.createBlock(pageID, "qr", map[string]any{"value": "https://example.com"}); code != 201 {
		t.Fatal("qr block")
	}
	if _, body, _ := e.client().get("/alice"); !strings.Contains(body, `fill="#1a1a2e"`) {
		t.Fatal("inline QR not themed")
	}
}
