package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"git.subcult.tv/subculture-collective/zelda/internal/plans"
)

// woff2Bytes is a WOFF2-shaped file whose header declares its own length.
func woff2Bytes(size int) []byte {
	b := make([]byte, size)
	copy(b, "wOF2")
	binary.BigEndian.PutUint32(b[8:12], uint32(size))
	return b
}

// uploadFile posts data as the multipart file field to path.
func (c *client) uploadFile(path, filename string, data []byte, out any) int {
	c.e.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", filename)
	fw.Write(data) //nolint:errcheck
	mw.Close()
	req, _ := http.NewRequest("POST", c.e.ts.URL+path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Requested-With", "zelda")
	res, err := c.c.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil {
		json.NewDecoder(res.Body).Decode(out) //nolint:errcheck
	}
	return res.StatusCode
}

type cssResp struct {
	CSS      string   `json:"css"`
	Warnings []string `json:"warnings"`
	Applied  bool     `json:"applied"`
}

// publicStyle returns the inline <style> of a public page and the CSP.
func publicStyle(t *testing.T, c *client, slug string) (string, string, string) {
	t.Helper()
	code, html, h := c.get("/" + slug)
	if code != 200 {
		t.Fatalf("public page: %d", code)
	}
	m := regexp.MustCompile(`(?s)<style nonce="[^"]+">(.*?)</style>`).FindStringSubmatch(html)
	if m == nil {
		t.Fatalf("no inline style in %s", html)
	}
	return m[1], html, h.Get("Content-Security-Policy")
}

func TestCustomCSSAndFonts(t *testing.T) {
	e := setup(t)
	bob, pageID, _ := publishedPage(t, e, "bob@example.com", "bobpage")
	eve := e.client()
	eve.signupVerified("eve@example.com")
	anon := e.client()
	var ue upgradeErr

	// ---- Free: gated ----
	if code := bob.do("PUT", "/api/pages/"+pageID+"/css", map[string]any{"css": ".a{color:red}"}, &ue); code != 403 ||
		ue.Error.Code != "upgrade_required" || ue.Error.Feature != "custom_css" || ue.Error.Field != "css" {
		t.Fatalf("free css: %d %+v", code, ue)
	}
	var cr cssResp
	if code := bob.do("PUT", "/api/pages/"+pageID+"/css", map[string]any{"css": ""}, &cr); code != 200 || cr.CSS != "" {
		t.Fatalf("free clear css: %d %+v", code, cr)
	}
	if code := bob.uploadFile("/api/uploads/font", "a.woff2", woff2Bytes(200), &ue); code != 403 || ue.Error.Feature != "custom_fonts" {
		t.Fatalf("free font upload: %d %+v", code, ue)
	}

	// ---- Pro ----
	bob.setPlan(plans.Pro, nil)
	eve.setPlan(plans.Pro, nil)
	imgID := uploadPNG(t, e, bob)
	eveImg := uploadPNG(t, e, eve)

	// Fonts: magic bytes, size, type.
	var up struct {
		ID, URL     string
		ContentType string `json:"content_type"`
	}
	if code := bob.uploadFile("/api/uploads/font", "a.woff2", []byte("\x89PNG\r\n\x1a\n0000000000000000000000000000000000000000"), &ue); code != 422 {
		t.Fatalf("png as font: %d", code)
	}
	if code := bob.uploadFile("/api/uploads/font", "a.woff2", woff2Bytes(1<<20+1), &ue); code != 413 {
		t.Fatalf("oversized font: %d", code)
	}
	if code := bob.uploadFile("/api/uploads/font", "evil.svg", []byte(`<svg><script>alert(1)</script></svg>`), &ue); code != 422 {
		t.Fatalf("svg as font: %d", code)
	}
	if code := bob.uploadFile("/api/uploads/font", "a.bin", woff2Bytes(400), &up); code != 201 || up.ContentType != "font/woff2" || up.URL != "/u/"+up.ID {
		t.Fatalf("font upload: %d %+v", code, up)
	}
	fontID := up.ID
	code, body, h := anon.get("/u/" + fontID)
	if code != 200 || h.Get("Content-Type") != "font/woff2" || h.Get("Cross-Origin-Resource-Policy") != "same-origin" ||
		h.Get("Access-Control-Allow-Origin") != "" || len(body) != 400 {
		t.Fatalf("serve font: %d %v", code, h)
	}
	var eveFont struct{ ID string }
	if code := eve.uploadFile("/api/uploads/font", "e.woff2", woff2Bytes(300), &eveFont); code != 201 {
		t.Fatalf("eve font: %d", code)
	}
	// The image upload endpoint still refuses fonts.
	if code := bob.uploadFile("/api/uploads", "a.woff2", woff2Bytes(300), &ue); code != 422 {
		t.Fatalf("font via image upload: %d", code)
	}

	// Theme font_uploads: own fonts only.
	theme := func(fu map[string]string) map[string]any {
		return map[string]any{"theme": map[string]any{"preset": "paper", "font_uploads": fu}}
	}
	var ae apiErr
	for _, bad := range []string{imgID, eveFont.ID, "not-a-uuid", "00000000-0000-0000-0000-000000000001"} {
		if code := bob.do("PATCH", "/api/pages/"+pageID, theme(map[string]string{"body": bad}), &ae); code != 422 || ae.Error.Field != "font_uploads" {
			t.Fatalf("font_uploads %s: %d %+v", bad, code, ae)
		}
	}
	var pg struct {
		Theme struct {
			FontUploads map[string]string `json:"font_uploads"`
		} `json:"theme"`
		CustomCSS string `json:"custom_css"`
	}
	if code := bob.do("PATCH", "/api/pages/"+pageID, theme(map[string]string{"body": fontID, "display": fontID}), &pg); code != 200 ||
		pg.Theme.FontUploads["body"] != fontID || pg.Theme.FontUploads["display"] != fontID {
		t.Fatalf("set fonts: %d %+v", code, pg)
	}

	// Custom CSS: sanitized, scoped, ownership-checked urls, warnings.
	hostile := "body{background:url(/u/" + imgID + ")}\n" +
		".btn{color:red}\n" +
		"@import url(https://evil.example/x.css);\n" +
		".x{background:url(/u/" + eveImg + ")}\n" +
		".y{background:\\75 rl(https://evil.example/t.png)}\n" +
		".z{content:\"</style><script>alert(2)</script>\"}\n" +
		"</style><script>alert(1)</script>"
	if code := bob.do("PUT", "/api/pages/"+pageID+"/css", map[string]any{"css": hostile}, &cr); code != 200 || !cr.Applied {
		t.Fatalf("set css: %d %+v", code, cr)
	}
	if !strings.Contains(cr.CSS, `body.kork-page main.page{background:url("/u/`+imgID+`")}`) || !strings.Contains(cr.CSS, "body.kork-page main.page .btn{color:red}") ||
		strings.Contains(cr.CSS, eveImg) || strings.Contains(cr.CSS, "evil.example") || strings.Contains(cr.CSS, "</") {
		t.Fatalf("sanitized css: %q", cr.CSS)
	}
	warn := strings.Join(cr.Warnings, "\n")
	for _, w := range []string{"line 3: @import is not allowed", "line 4: removed background: url() points at an upload that isn't yours",
		"line 5: removed background: url() can only point at your own uploads"} {
		if !strings.Contains(warn, w) {
			t.Fatalf("warnings %q lack %q", warn, w)
		}
	}
	bob.do("GET", "/api/pages/"+pageID, nil, &pg)
	if pg.CustomCSS != cr.CSS {
		t.Fatalf("page custom_css: %q", pg.CustomCSS)
	}
	if code := bob.do("PUT", "/api/pages/"+pageID+"/css", map[string]any{"css": strings.Repeat("a", 20<<10+1)}, &ae); code != 422 || ae.Error.Code != "too_large" {
		t.Fatalf("large css: %d %+v", code, ae)
	}
	if code := eve.do("PUT", "/api/pages/"+pageID+"/css", map[string]any{"css": ".a{}"}, &ae); code != 404 {
		t.Fatalf("eve sets bob's css: %d", code)
	}
	if code := bob.do("PUT", "/api/pages/"+pageID+"/css", map[string]any{}, &ae); code != 422 {
		t.Fatalf("missing css: %d", code)
	}

	// Rendering while entitled.
	style, html, csp := publicStyle(t, anon, "bobpage")
	if !strings.Contains(style, "body.kork-page main.page .btn{color:red}") ||
		!strings.Contains(style, `@font-face{font-family:"kork-body";src:url("/u/`+fontID+`");font-display:swap}`) ||
		!strings.Contains(style, `@font-face{font-family:"kork-display"`) || !strings.Contains(style, `--font:"kork-body", `) ||
		!strings.Contains(style, `--font-display:"kork-display"`) {
		t.Fatalf("pro style: %s", style)
	}
	// Custom CSS is scoped to <main>; the footer with Report must sit outside it.
	if mainEnd, footer := strings.Index(html, "</main>"), strings.Index(html, `id="page-footer"`); mainEnd < 0 || footer < mainEnd {
		t.Fatalf("footer must follow </main> so custom CSS can't reach Report: main end %d, footer %d", mainEnd, footer)
	}
	if !strings.Contains(html, `<body class="kork-page `) || strings.Contains(html, "<script>alert") {
		t.Fatalf("pro html: %s", html)
	}
	if !strings.Contains(csp, "font-src 'self'") || !strings.Contains(csp, "style-src 'self' 'nonce-") {
		t.Fatalf("csp: %s", csp)
	}

	// CSS stored before a sanitizer change is sanitized again when rendered.
	pool := testPool(t)
	if _, err := pool.Exec(context.Background(), `UPDATE pages SET custom_css = $2 WHERE id = $1`, pageID,
		`.q{color:blue}</style><script>alert(3)</script>`); err != nil {
		t.Fatal(err)
	}
	style, html, _ = publicStyle(t, anon, "bobpage")
	if !strings.Contains(style, "body.kork-page main.page .q{color:blue}") || strings.Contains(html, "<script>alert(3)") {
		t.Fatalf("stored css re-sanitized: %s", style)
	}
	if code := bob.do("PUT", "/api/pages/"+pageID+"/css", map[string]any{"css": ".btn{color:red}"}, &cr); code != 200 {
		t.Fatalf("reset css: %d", code)
	}

	// ---- Downgrade: stored, not applied ----
	bob.setPlan(plans.Free, nil)
	style, _, _ = publicStyle(t, anon, "bobpage")
	if strings.Contains(style, "kork-page .btn") || strings.Contains(style, "@font-face") || strings.Contains(style, "kork-body") ||
		!strings.Contains(style, `--font:"Iowan Old Style"`) {
		t.Fatalf("free style still has pro parts: %s", style)
	}
	bob.do("GET", "/api/pages/"+pageID, nil, &pg)
	if pg.CustomCSS != "body.kork-page main.page .btn{color:red}" || pg.Theme.FontUploads["body"] != fontID {
		t.Fatalf("stored after downgrade: %+v", pg)
	}
	// Other theme changes still save with the stored fonts; new fonts need Pro.
	if code := bob.do("PATCH", "/api/pages/"+pageID, map[string]any{"theme": map[string]any{"preset": "neon",
		"font_uploads": map[string]string{"body": fontID, "display": fontID}}}, &pg); code != 200 {
		t.Fatalf("free theme save with stored fonts: %d", code)
	}
	if code := bob.do("PATCH", "/api/pages/"+pageID, theme(map[string]string{"body": fontID}), &pg); code != 200 {
		t.Fatalf("free removes display font: %d", code)
	}
	bob.setPlan(plans.Pro, nil)
	var font2 struct{ ID string }
	bob.uploadFile("/api/uploads/font", "b.woff2", woff2Bytes(500), &font2)
	bob.setPlan(plans.Free, nil)
	if code := bob.do("PATCH", "/api/pages/"+pageID, theme(map[string]string{"body": font2.ID}), &ue); code != 403 ||
		ue.Error.Feature != "custom_fonts" || ue.Error.Field != "font_uploads" {
		t.Fatalf("free new font: %d %+v", code, ue)
	}
	if code := bob.do("PUT", "/api/pages/"+pageID+"/css", map[string]any{"css": ".a{color:red}"}, &ue); code != 403 {
		t.Fatalf("free css after downgrade: %d", code)
	}
}

func TestStatsCSVExport(t *testing.T) {
	e := setup(t)
	bob, pageID, ids := publishedPage(t, e, "bob@example.com", "bobpage")
	eve := e.client()
	eve.signupVerified("eve@example.com")
	var ue upgradeErr
	if code := bob.do("GET", "/api/pages/"+pageID+"/stats.csv", nil, &ue); code != 403 || ue.Error.Feature != "stats_export" {
		t.Fatalf("free csv: %d %+v", code, ue)
	}
	bob.setPlan(plans.Pro, nil)
	eve.setPlan(plans.Pro, nil)
	if code := eve.do("GET", "/api/pages/"+pageID+"/stats.csv", nil, &ue); code != 404 {
		t.Fatalf("eve reads bob's csv: %d", code)
	}

	// A block whose label would run as a spreadsheet formula.
	var formula struct{ ID string }
	if code := bob.do("POST", "/api/pages/"+pageID+"/blocks", map[string]any{"type": "link",
		"data": map[string]any{"url": "https://example.com/f", "title": "=HYPERLINK(\"https://evil.example\")"}}, &formula); code != 201 {
		t.Fatalf("formula block: %d", code)
	}
	pool := testPool(t)
	ctx := context.Background()
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO stats_daily (page_id, block_id, day, count) VALUES ($1, '00000000-0000-0000-0000-000000000000', current_date, 5),
			($1, '00000000-0000-0000-0000-000000000000', current_date - 2, 3),
			($1, '00000000-0000-0000-0000-000000000000', current_date - 40, 9),
			($1, $2, current_date, 2), ($1, $3, current_date, 4), ($1, $4, current_date - 2, 1),
			($1, '11111111-1111-1111-1111-111111111111', current_date - 2, 7)`, []any{pageID, ids[0], ids[1], formula.ID}},
		{`INSERT INTO stats_referrers (page_id, day, referrer, count) VALUES ($1, current_date, 'instagram.com', 4),
			($1, current_date, 'direct', 1), ($1, current_date - 40, 'old.example', 9)`, []any{pageID}},
		{`INSERT INTO stats_countries (page_id, day, country, count) VALUES ($1, current_date, 'US', 3), ($1, current_date, 'DE', 2)`, []any{pageID}},
	} {
		if _, err := pool.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	var today, twoAgo string
	if err := pool.QueryRow(ctx, `SELECT to_char(current_date, 'YYYY-MM-DD'), to_char(current_date - 2, 'YYYY-MM-DD')`).Scan(&today, &twoAgo); err != nil {
		t.Fatal(err)
	}

	code, body, h := bob.get("/api/pages/" + pageID + "/stats.csv?days=7")
	if code != 200 || h.Get("Content-Type") != "text/csv; charset=utf-8" ||
		!regexp.MustCompile(`^attachment; filename="bobpage-stats-7d-\d{4}-\d{2}-\d{2}\.csv"$`).MatchString(h.Get("Content-Disposition")) {
		t.Fatalf("csv: %d %v", code, h)
	}
	recs, err := csv.NewReader(strings.NewReader(body)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(recs[0], ",") != "date,metric,key,label,count" {
		t.Fatalf("header: %v", recs[0])
	}
	var views int
	got := map[string]bool{}
	for _, r := range recs[1:] {
		if r[1] == "views" {
			views++
		}
		got[strings.Join(r, "|")] = true
	}
	if views != 7 {
		t.Fatalf("views rows: %d\n%s", views, body)
	}
	for _, want := range []string{
		today + "|views|||5",
		twoAgo + "|views|||3",
		today + "|clicks|" + ids[1] + "|https://example.com/b|4",
		today + "|clicks|" + ids[0] + "|https://example.com/a|2",
		twoAgo + "|clicks|" + formula.ID + `|'=HYPERLINK("https://evil.example")|1`,
		twoAgo + "|clicks|11111111-1111-1111-1111-111111111111|Deleted block|7",
		today + "|referrer|instagram.com||4",
		today + "|referrer|direct||1",
		today + "|country|US||3",
		today + "|country|DE||2",
	} {
		if !got[want] {
			t.Fatalf("missing row %q in\n%s", want, body)
		}
	}
	if strings.Contains(body, "old.example") || strings.Contains(body, ",9\n") {
		t.Fatalf("rows outside the range:\n%s", body)
	}
	// Order: by day, then views, clicks, referrer, country.
	last := recs[len(recs)-1]
	if last[0] != today || last[1] != "country" || last[2] != "DE" {
		t.Fatalf("last row: %v", last)
	}

	// days is capped at the plan's history (Pro 365), and older rows appear.
	_, body, _ = bob.get("/api/pages/" + pageID + "/stats.csv?days=9999")
	if n := strings.Count(body, ",views,"); n != 365 || !strings.Contains(body, "old.example") {
		t.Fatalf("pro range: %d views rows", n)
	}
	// Free again: no export.
	bob.setPlan(plans.Free, nil)
	if code := bob.do("GET", "/api/pages/"+pageID+"/stats.csv", nil, &ue); code != 403 {
		t.Fatalf("csv after downgrade: %d", code)
	}
}
