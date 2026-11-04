package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/customcss"
	"git.subcult.tv/subculture-collective/zelda/internal/plans"
)

// deadlineWriter records whether a write deadline reached it.
type deadlineWriter struct {
	*httptest.ResponseRecorder
	deadline time.Time
}

func (w *deadlineWriter) SetWriteDeadline(t time.Time) error {
	w.deadline = t
	return nil
}

// The request logger's wrapper must not hide the server's writer from
// http.ResponseController, or the export's longer write deadline is lost.
func TestStatusWriterUnwraps(t *testing.T) {
	inner := &deadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	sw := &statusWriter{ResponseWriter: inner, status: 200}
	want := time.Now().Add(time.Minute)
	if err := http.NewResponseController(sw).SetWriteDeadline(want); err != nil {
		t.Fatalf("SetWriteDeadline through statusWriter: %v", err)
	}
	if !inner.deadline.Equal(want) {
		t.Fatalf("deadline not passed through: %v", inner.deadline)
	}
}

func TestLimiterBoundsKeys(t *testing.T) {
	l := newLimiter(1, time.Hour)
	for i := range limiterMaxKeys + 100 {
		l.allow("ip" + itoa(i))
	}
	if len(l.hits) > limiterMaxKeys {
		t.Fatalf("limiter holds %d keys, cap is %d", len(l.hits), limiterMaxKeys)
	}
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

func TestTruncateRunes(t *testing.T) {
	s := strings.Repeat("a", 999) + "éz"
	got := truncateRunes(s, 1000)
	if got != strings.Repeat("a", 999)+"é" {
		t.Fatalf("truncated to %q", got[990:])
	}
	if truncateRunes("abc", 5) != "abc" {
		t.Fatal("short strings are kept")
	}
}

// Report details are cut by character, not byte, so a long non-ASCII
// report is stored instead of failing as invalid UTF-8; drafts can't be
// reported, so they can't be told apart from missing pages.
func TestReportDetailsAndDrafts(t *testing.T) {
	e := setup(t)
	pool := testPool(t)
	alice, _ := livePage(t, e, "alice@example.com", "alice")
	var draft pageResp
	if code := alice.do("POST", "/api/pages", map[string]any{"slug": "alice-draft", "title": "Draft"}, &draft); code != 201 {
		t.Fatalf("draft page: %d", code)
	}
	anon := e.client()
	details := strings.Repeat("a", 999) + "é" + strings.Repeat("b", 50)
	if code := anon.do("POST", "/api/report", map[string]string{"slug": "alice", "reason": "spam", "details": details}, nil); code != 204 {
		t.Fatalf("long non-ASCII report: %d", code)
	}
	var stored string
	if err := pool.QueryRow(context.Background(), `SELECT details FROM reports ORDER BY created_at DESC LIMIT 1`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(stored, "é") || len([]rune(stored)) != 1000 {
		t.Fatalf("stored details: %d runes, ends %q", len([]rune(stored)), stored[len(stored)-4:])
	}
	var ae apiErr
	if code := anon.do("POST", "/api/report", map[string]string{"slug": "alice-draft", "reason": "spam"}, &ae); code != 404 {
		t.Fatalf("report on draft: %d %+v", code, ae)
	}
	if code := anon.do("POST", "/api/report", map[string]string{"slug": "no-such-page", "reason": "spam"}, &ae); code != 404 {
		t.Fatalf("report on missing page: %d", code)
	}
}

// Once an IP has used its reset budget, its further requests must not use
// up the per-address budget of other people's accounts.
func TestForgotLimitsIPBeforeAddress(t *testing.T) {
	e := setup(t)
	bob := e.client()
	bob.signupVerified("bob@example.com")
	anon := e.client()
	for i := 0; i < 3; i++ {
		anon.do("POST", "/api/auth/forgot", map[string]string{"email": "nobody@example.com"}, nil)
	}
	for i := 0; i < 5; i++ {
		anon.do("POST", "/api/auth/forgot", map[string]string{"email": "bob@example.com"}, nil)
	}
	e.srv.mailLimit.mu.Lock()
	w := e.srv.mailLimit.hits["reset:bob@example.com"]
	e.srv.mailLimit.mu.Unlock()
	if w != nil {
		t.Fatalf("a limited IP consumed %d of bob's reset budget", w.n)
	}
}

// rezip rewrites account.json inside an export ZIP.
func rezip(t *testing.T, export []byte, edit func(acct map[string]any)) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(export), int64(len(export)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, f := range zr.File {
		data, err := readZipEntry(f, maxImportZip)
		if err != nil {
			t.Fatal(err)
		}
		files[f.Name] = data
	}
	var acct map[string]any
	if err := json.Unmarshal(files["account.json"], &acct); err != nil {
		t.Fatal(err)
	}
	edit(acct)
	files["account.json"], _ = json.Marshal(acct)
	return zipOf(t, files)
}

// An import can't bind a page to someone else's uploads, give a Free account
// font uploads, or store custom CSS past the editor's limits.
func TestImportKeepsThemeAndCSSLimits(t *testing.T) {
	e := setup(t)
	alice, pageID := livePage(t, e, "alice@example.com", "alice")
	eve, _ := livePage(t, e, "eve@example.com", "eve")
	eve.setPlan(plans.Pro, nil)
	eveImg := uploadPNG(t, e, eve)
	var eveFont struct{ ID string }
	if code := eve.uploadFile("/api/uploads/font", "e.woff2", woff2Bytes(300), &eveFont); code != 201 {
		t.Fatalf("eve font: %d", code)
	}
	code, zipBody, _ := alice.get("/api/account/export")
	if code != 200 {
		t.Fatalf("export: %d", code)
	}
	export := rezip(t, []byte(zipBody), func(acct map[string]any) {
		page := acct["pages"].([]any)[0].(map[string]any)
		page["theme"] = map[string]any{
			"preset": "signal", "font": "rounded", "button_style": "soft", "button_shape": "pill", "layout": "stack",
			"colors":     map[string]any{"bg": "#111111", "surface": "#222222", "text": "#ffffff", "muted": "#cccccc", "accent": "#ff00ff", "button_bg": "#222222", "button_text": "#ffffff"},
			"background": map[string]any{"kind": "image", "image_upload_id": eveImg},
			// body: a font file in the export (a Free importer can't keep it);
			// display: another account's font upload.
			"font_uploads": map[string]any{"body": "11111111-1111-4111-8111-111111111111", "display": eveFont.ID},
		}
		page["custom_css"] = ".btn{color:red}" + strings.Repeat("/* pad */", customcss.MaxInput/9+1)
		acct["uploads"] = append(acct["uploads"].([]any), map[string]any{"id": "11111111-1111-4111-8111-111111111111",
			"content_type": "font/woff2", "size": 300, "file": "uploads/11111111-1111-4111-8111-111111111111.woff2"})
	})
	// Add the font file so the importer sees a font a Free account can't keep.
	zr, _ := zip.NewReader(bytes.NewReader(export), int64(len(export)))
	files := map[string][]byte{}
	for _, f := range zr.File {
		files[f.Name], _ = readZipEntry(f, maxImportZip)
	}
	files["uploads/11111111-1111-4111-8111-111111111111.woff2"] = woff2Bytes(300)
	export = zipOf(t, files)

	bob := e.client()
	bob.signupVerified("bob@example.com")
	var res struct {
		Pages   []map[string]string `json:"pages"`
		Skipped []importSkip        `json:"skipped"`
	}
	if code := bob.postExport("/api/import/export", export, []map[string]any{{"index": 0, "slug": "bob-copy"}}, &res); code != 200 || len(res.Pages) != 1 {
		t.Fatalf("import: %d %+v", code, res)
	}
	var got struct {
		Theme     json.RawMessage `json:"theme"`
		CustomCSS string          `json:"custom_css"`
	}
	if code := bob.do("GET", "/api/pages/"+res.Pages[0]["id"], nil, &got); code != 200 {
		t.Fatalf("get imported page: %d", code)
	}
	var theme struct {
		Background  map[string]string  `json:"background"`
		FontUploads *map[string]string `json:"font_uploads"`
	}
	if err := json.Unmarshal(got.Theme, &theme); err != nil {
		t.Fatal(err)
	}
	if theme.Background["kind"] != "solid" || theme.Background["image_upload_id"] != "" {
		t.Fatalf("imported theme kept eve's background image: %+v", theme.Background)
	}
	if theme.FontUploads != nil {
		t.Fatalf("imported theme kept eve's font: %+v", *theme.FontUploads)
	}
	if got.CustomCSS != "" {
		t.Fatalf("oversized custom CSS was stored (%d bytes)", len(got.CustomCSS))
	}
	reasons := ""
	for _, s := range res.Skipped {
		reasons += s.What + ": " + s.Reason + "\n"
	}
	for _, want := range []string{"background image", "font", "Font uploads are part of Pro", "larger than"} {
		if !strings.Contains(reasons, want) {
			t.Errorf("skipped report is missing %q:\n%s", want, reasons)
		}
	}
	_ = pageID
}
