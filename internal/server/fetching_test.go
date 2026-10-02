package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"testing"

	"git.subcult.tv/subculture-collective/zelda/internal/fetch"
)

var tinyPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82")

// remoteSite stands in for the outside web. httptest binds loopback, so the
// tests swap in a fetcher whose address policy allows it.
func remoteSite(t *testing.T) *httptest.Server {
	t.Helper()
	fixture, err := os.ReadFile("../linkimport/testdata/linktree.html")
	if err != nil {
		t.Fatal(err)
	}
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/alicedraws":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(strings.ReplaceAll(string(fixture), "https://ugc.production.linktr.ee/avatar-alice.png", ts.URL+"/avatar.png")))
		case "/article":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<html><head><title>Fallback</title>
<meta property="og:title" content="How to bind a zine">
<meta property="og:description" content="Saddle stitch in ten minutes.">
<meta property="og:image" content="/card.png"></head><body></body></html>`))
		case "/svg-card":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<meta property="og:title" content="SVG card"><meta property="og:image" content="/card.svg">`))
		case "/avatar.png", "/card.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write(tinyPNG)
		case "/card.svg":
			// Claims to be PNG; the bytes decide.
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
		case "/data.json":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

func allowLoopback() *fetch.Client {
	return fetch.New(fetch.Options{
		AllowAddr: func(netip.Addr) bool { return true },
		AllowPort: func(int) bool { return true },
	})
}

type importPreview struct {
	SourceURL string `json:"source_url"`
	Parser    string `json:"parser"`
	Title     string `json:"title"`
	Bio       string `json:"bio"`
	AvatarURL string `json:"avatar_url"`
	Items     []struct {
		Type  string          `json:"type"`
		Data  json.RawMessage `json:"data"`
		Label string          `json:"label"`
	} `json:"items"`
}

func TestImportAndLinkPreview(t *testing.T) {
	e := setup(t)
	site := remoteSite(t)
	e.srv.SetFetcher(allowLoopback())

	var ae apiErr
	if code := e.client().do("POST", "/api/import/preview", map[string]string{"url": site.URL + "/alicedraws"}, &ae); code != 401 {
		t.Fatalf("anonymous import: %d", code)
	}

	alice := e.client()
	if code := alice.do("POST", "/api/auth/signup", map[string]string{"email": "alice@example.com", "password": "correct horse battery"}, nil); code != 201 {
		t.Fatalf("signup: %d", code)
	}
	var page pageResp
	if code := alice.do("POST", "/api/pages", map[string]string{"slug": "alice", "title": "Alice"}, &page); code != 201 {
		t.Fatalf("create page: %d", code)
	}

	// #7: preview a saved Linktree page, then create its blocks with the block API.
	var prev importPreview
	if code := alice.do("POST", "/api/import/preview", map[string]string{"url": site.URL + "/alicedraws"}, &prev); code != 200 {
		t.Fatalf("import preview: %d", code)
	}
	if prev.Parser != "linktree" || prev.Title != "Alice Example" || prev.Bio != "Illustrator and zine maker." || prev.AvatarURL != site.URL+"/avatar.png" {
		t.Fatalf("preview profile: %+v", prev)
	}
	wantLabels := []string{"Instagram", "TikTok", "Shop prints", "Support", "Buy me a coffee", "Studio playlist", "Zine fair 2026"}
	if len(prev.Items) != len(wantLabels) {
		t.Fatalf("preview items: %+v", prev.Items)
	}
	for _, it := range prev.Items {
		var out struct{ ID string }
		if code := alice.do("POST", "/api/pages/"+page.ID+"/blocks", map[string]any{"type": it.Type, "data": it.Data}, &out); code != 201 {
			t.Fatalf("create imported %s block %s: %d", it.Type, it.Data, code)
		}
	}
	var avatar struct{ ID, URL string }
	if code := alice.do("POST", "/api/import/image", map[string]string{"url": prev.AvatarURL}, &avatar); code != 201 || avatar.ID == "" {
		t.Fatalf("import avatar: %d %+v", code, avatar)
	}
	if code := alice.do("PATCH", "/api/pages/"+page.ID, map[string]any{"title": prev.Title, "bio": prev.Bio, "avatar_upload_id": avatar.ID}, &page); code != 200 {
		t.Fatalf("apply profile: %d", code)
	}
	if len(page.Blocks) != len(wantLabels) {
		t.Fatalf("page has %d blocks", len(page.Blocks))
	}
	for i, b := range page.Blocks {
		if b.Label != wantLabels[i] {
			t.Errorf("block %d label %q, want %q", i, b.Label, wantLabels[i])
		}
	}
	if code, body, _ := alice.get(avatar.URL); code != 200 || body != string(tinyPNG) {
		t.Fatalf("serve imported avatar: %d", code)
	}

	// #8: Open Graph title, description and image; the image becomes an upload.
	var lp struct {
		URL               string `json:"url"`
		Title             string `json:"title"`
		Description       string `json:"description"`
		ThumbnailUploadID string `json:"thumbnail_upload_id"`
		ThumbnailURL      string `json:"thumbnail_url"`
		ImageError        string `json:"image_error"`
	}
	if code := alice.do("POST", "/api/link-preview", map[string]string{"url": site.URL + "/article"}, &lp); code != 200 {
		t.Fatalf("link preview: %d", code)
	}
	if lp.Title != "How to bind a zine" || lp.Description != "Saddle stitch in ten minutes." || lp.ThumbnailUploadID == "" || lp.ThumbnailURL != "/u/"+lp.ThumbnailUploadID {
		t.Fatalf("link preview: %+v", lp)
	}
	if code := alice.do("POST", "/api/pages/"+page.ID+"/blocks", map[string]any{"type": "link",
		"data": map[string]any{"url": "https://example.org/zine", "title": lp.Title, "thumbnail_upload_id": lp.ThumbnailUploadID}}, nil); code != 201 {
		t.Fatalf("link block with fetched thumbnail: %d", code)
	}

	// Images are type-checked by their bytes, like uploads.
	lp.ThumbnailUploadID = ""
	if code := alice.do("POST", "/api/link-preview", map[string]string{"url": site.URL + "/svg-card"}, &lp); code != 200 || lp.ThumbnailUploadID != "" || lp.ImageError == "" || lp.Title != "SVG card" {
		t.Fatalf("svg og:image: %d %+v", code, lp)
	}
	if code := alice.do("POST", "/api/import/image", map[string]string{"url": site.URL + "/card.svg"}, &ae); code != 422 || ae.Error.Code != "unsupported" {
		t.Fatalf("svg import: %d %+v", code, ae)
	}
	if code := alice.do("POST", "/api/link-preview", map[string]string{"url": site.URL + "/data.json"}, &ae); code != 422 || ae.Error.Code != "not_html" {
		t.Fatalf("non-HTML preview: %d %+v", code, ae)
	}
	if code := alice.do("POST", "/api/import/preview", map[string]string{"url": site.URL + "/missing"}, &ae); code != 422 || ae.Error.Code != "fetch_failed" {
		t.Fatalf("404 page: %d %+v", code, ae)
	}

	// With the production address policy, loopback, private and metadata
	// addresses are refused, including hostnames that resolve to them.
	e.srv.SetFetcher(fetch.New(fetch.Options{AllowPort: func(int) bool { return true }}))
	port := site.URL[strings.LastIndex(site.URL, ":")+1:]
	for _, target := range []string{site.URL + "/alicedraws", "http://localhost:" + port + "/article", "http://169.254.169.254/latest/meta-data/", "http://10.0.0.1/", "http://[::1]/"} {
		for _, path := range []string{"/api/import/preview", "/api/link-preview", "/api/import/image"} {
			ae = apiErr{}
			if code := alice.do("POST", path, map[string]string{"url": target}, &ae); code != 422 || ae.Error.Code != "blocked_address" {
				t.Errorf("%s %s: %d %+v", path, target, code, ae)
			}
		}
	}
	e.srv.SetFetcher(fetch.New(fetch.Options{}))
	for _, target := range []string{"ftp://example.com/", "https://example.com:8443/"} {
		if code := alice.do("POST", "/api/import/preview", map[string]string{"url": target}, &ae); code != 422 || ae.Error.Code != "invalid" {
			t.Errorf("%s: %d %+v", target, code, ae)
		}
	}

	// Per-user rate limit on imports.
	limited := false
	for i := 0; i < 25 && !limited; i++ {
		limited = alice.do("POST", "/api/import/preview", map[string]string{"url": "http://127.0.0.1/"}, nil) == 429
	}
	if !limited {
		t.Fatal("import preview was never rate limited")
	}
}
