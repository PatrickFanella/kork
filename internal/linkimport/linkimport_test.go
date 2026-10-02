package linkimport

import (
	"encoding/json"
	"net/url"
	"os"
	"regexp"
	"testing"
)

type want struct {
	typ  string
	data map[string]any
}

func check(t *testing.T, items []Item, wants []want) {
	t.Helper()
	if len(items) != len(wants) {
		for _, it := range items {
			t.Logf("%s %s", it.Type, it.Data)
		}
		t.Fatalf("got %d items, want %d", len(items), len(wants))
	}
	for i, w := range wants {
		var got map[string]any
		_ = json.Unmarshal(items[i].Data, &got)
		if items[i].Type != w.typ {
			t.Errorf("item %d: type %s, want %s", i, items[i].Type, w.typ)
		}
		for k, v := range w.data {
			if got[k] != v {
				t.Errorf("item %d (%s): %s = %v, want %v", i, items[i].Type, k, got[k], v)
			}
		}
		if items[i].Label == "" {
			t.Errorf("item %d has no label", i)
		}
	}
}

func fixture(t *testing.T) []byte {
	b, err := os.ReadFile("testdata/linktree.html")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestLinktreeNextData(t *testing.T) {
	page, _ := url.Parse("https://linktr.ee/alicedraws")
	p := Parse(page, fixture(t))
	if p.Parser != "linktree" || p.Title != "Alice Example" || p.Bio != "Illustrator and zine maker." ||
		p.AvatarURL != "https://ugc.production.linktr.ee/avatar-alice.png" {
		t.Fatalf("profile: %+v", p)
	}
	check(t, Items(p), []want{
		{"service", map[string]any{"service": "instagram", "value": "alice.draws"}},
		{"service", map[string]any{"service": "tiktok", "value": "alicedraws"}},
		{"link", map[string]any{"url": "https://alice.example.com/shop", "title": "Shop prints"}},
		{"heading", map[string]any{"text": "Support"}},
		{"service", map[string]any{"service": "kofi", "value": "alicedraws", "label": "Buy me a coffee"}},
		{"service", map[string]any{"service": "spotify", "value": "https://open.spotify.com/playlist/37i9dQZF1DX0XUsuxWHRQd"}},
		{"link", map[string]any{"url": "https://example.org/zine-fair-2026", "title": "Zine fair 2026"}},
	})
}

// Saved pages and hosts other than linktr.ee still parse when they carry the JSON.
func TestLinktreeDataOnOtherHost(t *testing.T) {
	page, _ := url.Parse("http://127.0.0.1:8080/alicedraws")
	if p := Parse(page, fixture(t)); p.Parser != "linktree" || len(Items(p)) != 7 {
		t.Fatalf("profile: %+v", p)
	}
}

func TestLinktreeAnchorFallback(t *testing.T) {
	body := regexp.MustCompile(`(?s)<script id="__NEXT_DATA__".*?</script>`).ReplaceAll(fixture(t), nil)
	page, _ := url.Parse("https://linktr.ee/alicedraws")
	p := Parse(page, body)
	if p.Parser != "linktree" || p.Title != "Alice Example" || p.AvatarURL != "https://ugc.production.linktr.ee/og-alice.png" {
		t.Fatalf("profile: %+v", p)
	}
	// Linktree's own footer links are skipped; headings aren't anchors.
	check(t, Items(p), []want{
		{"service", map[string]any{"service": "instagram", "value": "alice.draws"}},
		{"service", map[string]any{"service": "tiktok", "value": "alicedraws"}},
		{"link", map[string]any{"url": "https://alice.example.com/shop", "title": "Shop prints"}},
		{"service", map[string]any{"service": "kofi", "value": "alicedraws"}},
		{"service", map[string]any{"service": "spotify"}},
		{"link", map[string]any{"url": "https://example.org/zine-fair-2026"}},
	})
}

func TestGenericPage(t *testing.T) {
	page, _ := url.Parse("https://bob.example.net/links")
	body := []byte(`<html><head><title>Bob</title><meta name="description" content="Hi, I'm Bob.">
<meta property="og:image" content="/me.jpg"></head><body>
<a href="/about">About (same site, skipped)</a>
<a href="https://www.bob.example.net/blog">Blog (same site, skipped)</a>
<a href="https://github.com/bob">GitHub</a>
<a href="https://unknown.example.org/thing">  My   thing </a>
<a href="https://unknown.example.org/thing">duplicate</a>
<a href="https://unknown.example.org/untitled"><img src="x.png"></a>
<a href="mailto:bob@example.net">Email me</a>
</body></html>`)
	p := Parse(page, body)
	if p.Parser != "generic" || p.Title != "Bob" || p.Bio != "Hi, I'm Bob." || p.AvatarURL != "https://bob.example.net/me.jpg" {
		t.Fatalf("profile: %+v", p)
	}
	check(t, Items(p), []want{
		{"service", map[string]any{"service": "github", "value": "bob"}},
		{"link", map[string]any{"url": "https://unknown.example.org/thing", "title": "My thing"}},
		{"link", map[string]any{"url": "https://unknown.example.org/untitled", "title": "unknown.example.org/untitled"}},
		{"service", map[string]any{"service": "email", "value": "bob@example.net", "label": "Email me"}},
	})
}

func TestItemsCap(t *testing.T) {
	p := &Profile{}
	for i := 0; i < 150; i++ {
		p.Entries = append(p.Entries, Entry{URL: "https://example.com/" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Title: "x"})
	}
	if n := len(Items(p)); n != MaxItems {
		t.Fatalf("got %d items", n)
	}
}
