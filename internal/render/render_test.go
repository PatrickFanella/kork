package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/blocks"
	"git.subcult.tv/subculture-collective/zelda/internal/catalog"
)

// sampleBlocks has one block of every kind the public page renders.
var sampleBlocks = []blocks.Block{
	{ID: "a", Type: "link", Data: json.RawMessage(`{"title":"Shop","url":"https://shop.example","show_qr":true}`)},
	{ID: "b", Type: "service", Data: json.RawMessage(`{"service":"instagram","value":"subcult_tv"}`)},
	{ID: "c", Type: "service", Data: json.RawMessage(`{"service":"phone","value":"+1 555 123 4567"}`)},
	{ID: "d", Type: "service", Data: json.RawMessage(`{"service":"bitcoin","value":"bc1qxy2kgdygjrsqtzq2n0yrf2493p83kkfjhx0wlh"}`)},
	{ID: "e", Type: "heading", Data: json.RawMessage(`{"text":"Section"}`)},
	{ID: "f", Type: "text", Data: json.RawMessage(`{"text":"Hello <b>world</b> https://example.com"}`)},
	{ID: "g", Type: "divider", Data: json.RawMessage(`{}`)},
	{ID: "h", Type: "embed", Data: json.RawMessage(`{"url":"https://www.youtube.com/watch?v=dQw4w9WgXcQ"}`)},
	{ID: "i", Type: "qr", Data: json.RawMessage(`{"value":"https://example.com","label":"Scan me"}`)},
	{ID: "j", Type: "copy", Data: json.RawMessage(`{"label":"Discord","value":"alice#1234"}`)},
	{ID: "k", Type: "contact", Data: json.RawMessage(`{"name":"Alice","show_qr":true}`)},
	{ID: "l", Type: "map", Data: json.RawMessage(`{"address":"1 Main St"}`)},
	{ID: "m", Type: "email_signup", Data: json.RawMessage(`{"title":"News","destination":"kork"}`)},
	{ID: "n", Type: "events", Data: json.RawMessage(`{"events":[{"date":"2026-10-20","title":"Show","ticket_url":"https://t.example"}]}`)},
}

func renderPage(t *testing.T, theme catalog.Theme, title string) string {
	t.Helper()
	ctx := blocks.Context{Host: "kork.example", UploadURL: func(id string) string { return "/u/" + id }, QRFG: "#000000", QRBG: "#ffffff",
		VCardURL: func(id string) string { return "/vcard/" + id }, AbsURL: func(p string) string { return "https://kork.example" + p },
		Now: time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)}
	var buf bytes.Buffer
	p := Page{AppName: "Kork", BaseURL: "https://kork.example", Slug: "alice", Title: title, Bio: "Prints",
		CanonicalURL: "https://kork.example/alice", Theme: theme, Items: blocks.Build(ctx, sampleBlocks), Live: true, Nonce: "n0nce"}
	if err := WritePage(&buf, p, ""); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// Every preset renders every block kind without template errors or
// filtered URLs.
func TestWritePageEveryPresetAndBlock(t *testing.T) {
	for _, preset := range catalog.Presets {
		th, _, err := preset.Theme.Normalize()
		if err != nil {
			t.Fatalf("%s: %v", preset.Key, err)
		}
		body := renderPage(t, th, "Alice")
		if strings.Contains(body, "ZgotmplZ") {
			t.Errorf("%s: a value was filtered by html/template", preset.Key)
		}
		for _, want := range []string{"--bg:" + th.Colors.BG, `href="https://shop.example"`, `href="tel:`, "item-qr", "item-signup",
			"item-events", "<iframe", "Save contact"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: page is missing %q", preset.Key, want)
			}
		}
	}
}

func TestWritePageEscapesText(t *testing.T) {
	body := renderPage(t, catalog.DefaultTheme(), `Alice <script>alert(1)</script>`)
	if strings.Contains(body, "<script>alert(1)") {
		t.Fatal("title was not escaped")
	}
	if !strings.Contains(body, "Hello &lt;b&gt;world&lt;/b&gt;") {
		t.Fatal("text block was not escaped")
	}
}

func TestThemeFrom(t *testing.T) {
	def := catalog.DefaultTheme()
	if got := ThemeFrom(nil); got.Colors != def.Colors {
		t.Fatalf("empty theme: %+v", got.Colors)
	}
	if got := ThemeFrom(json.RawMessage(`{"colors":{"bg":"red"}}`)); got.Colors != def.Colors {
		t.Fatalf("invalid theme should fall back to the default: %+v", got.Colors)
	}
	got := ThemeFrom(json.RawMessage(`{"colors":{"accent":"#ff3366"}}`))
	if got.Colors.Accent != "#ff3366" {
		t.Fatalf("accent: %s", got.Colors.Accent)
	}
}

func TestWriteSimpleAndLegal(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteSimple(&buf, Simple{AppName: "Kork", BaseURL: "https://kork.example", Title: "Confirm", Heading: "Confirm <x>",
		Form: &SimpleForm{Action: "/subscribe/confirm", Token: "tok", Button: "Confirm subscription"}}); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if !strings.Contains(s, `action="/subscribe/confirm"`) || !strings.Contains(s, `value="tok"`) || strings.Contains(s, "Confirm <x>") {
		t.Fatalf("simple page:\n%s", s)
	}
	for _, doc := range []string{"terms", "privacy"} {
		buf.Reset()
		if err := WriteLegal(&buf, Legal{AppName: "Kork", BaseURL: "https://kork.example", ContactEmail: "info@example.com", Doc: doc,
			Title: doc, TermsVersion: TermsVersion}); err != nil {
			t.Fatalf("%s: %v", doc, err)
		}
		if !strings.Contains(buf.String(), "info@example.com") || !strings.Contains(buf.String(), TermsVersion) {
			t.Errorf("%s page is missing the contact address or version", doc)
		}
	}
	if Static() == nil {
		t.Fatal("no static files")
	}
}

func TestNewBadge(t *testing.T) {
	b := NewBadge("Expired", time.Date(2026, 10, 2, 18, 30, 0, 0, time.FixedZone("CDT", -5*3600)))
	if b.Datetime != "2026-10-02T23:30:00Z" || b.Display != "Oct 2, 2026 23:30 UTC" || b.Text != "Expired" {
		t.Fatalf("%+v", b)
	}
}
