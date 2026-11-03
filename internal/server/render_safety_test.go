package server

import (
	"strings"
	"testing"
)

// A second background color saved with a non-gradient background used to
// skip validation and reach the page's <style> element unescaped.
func TestThemeColor2CannotEscapeStyle(t *testing.T) {
	e := setup(t)
	alice, pageID := livePage(t, e, "alice@example.com", "alice")
	payload := `red}</style><meta http-equiv="refresh" content="0;url=https://example.org/"><style>`
	theme := map[string]any{"background": map[string]any{"kind": "solid", "color2": payload}}
	if code := alice.do("PATCH", "/api/pages/"+pageID, map[string]any{"theme": theme}, nil); code != 200 {
		t.Fatalf("save theme: %d", code)
	}
	_, body, _ := alice.get("/alice")
	if strings.Contains(body, "http-equiv") || strings.Contains(body, "red}") {
		t.Fatalf("color2 reached the page: %s", body)
	}
	var page struct {
		Theme struct {
			Background struct {
				Color2 string `json:"color2"`
			} `json:"background"`
		} `json:"theme"`
	}
	alice.do("GET", "/api/pages/"+pageID, nil, &page)
	if page.Theme.Background.Color2 != "" {
		t.Fatalf("stored color2 = %q, want it cleared", page.Theme.Background.Color2)
	}
}

// html/template replaces tel: and sms: hrefs with #ZgotmplZ unless they are
// marked safe after validation.
func TestPhoneAndSMSLinksRender(t *testing.T) {
	e := setup(t)
	alice, pageID := livePage(t, e, "alice@example.com", "alice")
	for _, svc := range []string{"phone", "sms"} {
		if code, _, ae := alice.createBlock(pageID, "service", map[string]any{"service": svc, "value": "+1 555 123 4567"}); code != 201 {
			t.Fatalf("%s block: %d %+v", svc, code, ae)
		}
	}
	_, body, _ := alice.get("/alice")
	if strings.Contains(body, "ZgotmplZ") {
		t.Fatal("a link was replaced with #ZgotmplZ")
	}
	// The template HTML-escapes "+" as &#43;, which browsers decode.
	for _, want := range []string{`href="tel:&#43;15551234567"`, `href="sms:&#43;15551234567"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page is missing %s", want)
		}
	}
}
