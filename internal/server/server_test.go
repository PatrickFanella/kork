package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/config"
	"git.subcult.tv/subculture-collective/zelda/internal/db"
	"git.subcult.tv/subculture-collective/zelda/internal/mail"
	"git.subcult.tv/subculture-collective/zelda/internal/store"
)

// These tests need Postgres: ZELDA_TEST_DATABASE_URL=postgres://... go test ./...
// The database is wiped.

type captureMail struct {
	mu   sync.Mutex
	msgs []mail.Message
}

func (c *captureMail) Send(_ context.Context, m mail.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgs = append(c.msgs, m)
	return nil
}

func (c *captureMail) tokenFor(t *testing.T, to, path string) string {
	t.Helper()
	re := regexp.MustCompile(regexp.QuoteMeta(path) + `\?token=([A-Za-z0-9_-]+)`)
	for i := 0; i < 200; i++ { // mail is sent from a goroutine
		c.mu.Lock()
		for j := len(c.msgs) - 1; j >= 0; j-- {
			if m := re.FindStringSubmatch(c.msgs[j].Body); m != nil && c.msgs[j].To == to {
				c.mu.Unlock()
				return m[1]
			}
		}
		c.mu.Unlock()
		waitABit()
	}
	t.Fatalf("no %s mail for %s", path, to)
	return ""
}

type fakeDNS struct {
	txt map[string][]string
	ip  map[string]string
}

func (f fakeDNS) LookupTXT(_ context.Context, name string) ([]string, error) { return f.txt[name], nil }
func (f fakeDNS) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	if ip, ok := f.ip[host]; ok {
		return []net.IPAddr{{IP: net.ParseIP(ip)}}, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

type env struct {
	t    *testing.T
	ts   *httptest.Server
	mail *captureMail
	srv  *Server
	dir  string
}

func setup(t *testing.T) *env {
	t.Helper()
	dsn := os.Getenv("ZELDA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ZELDA_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, mail: &captureMail{}, dir: t.TempDir()}
	e.ts = httptest.NewUnstartedServer(nil)
	e.ts.Start()
	t.Cleanup(e.ts.Close)
	u, _ := url.Parse(e.ts.URL)
	cfg := &config.Config{BaseURL: e.ts.URL, BaseHost: u.Host, AppName: "Zelda", UploadDir: e.dir, DomainTarget: "edge.example.net",
		AdminEmails: map[string]bool{"alice@example.com": true}, CustomDomains: true}
	e.srv = New(cfg, store.New(pool), e.mail, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	e.ts.Config.Handler = e.srv.Handler()
	return e
}

type client struct {
	e *env
	c *http.Client
}

func (e *env) client() *client {
	jar, _ := cookiejar.New(nil)
	return &client{e: e, c: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c *client) do(method, path string, body any, out any) int {
	c.e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.e.ts.URL+path, rd)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "zelda")
	res, err := c.c.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			c.e.t.Fatalf("%s %s: decode %q: %v", method, path, data, err)
		}
	}
	return res.StatusCode
}

func (c *client) get(path string, header ...string) (int, string, http.Header) {
	c.e.t.Helper()
	req, _ := http.NewRequest("GET", c.e.ts.URL+path, nil)
	for i := 0; i+1 < len(header); i += 2 {
		if header[i] == "Host" {
			req.Host = header[i+1]
		} else {
			req.Header.Set(header[i], header[i+1])
		}
	}
	res, err := c.c.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b), res.Header
}

type apiErr struct {
	Error struct{ Code, Message, Field string }
}

type pageResp struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Published bool   `json:"published"`
	Theme     struct {
		Preset string `json:"preset"`
		Colors struct {
			BG string `json:"bg"`
		} `json:"colors"`
	} `json:"theme"`
	Blocks []struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	} `json:"blocks"`
	Domains []struct {
		ID       string `json:"id"`
		TXTName  string `json:"txt_name"`
		TXTValue string `json:"txt_value"`
		Verified bool   `json:"verified"`
	} `json:"domains"`
}

func TestEndToEnd(t *testing.T) {
	e := setup(t)
	alice := e.client()

	// CSRF header is required on writes.
	req, _ := http.NewRequest("POST", e.ts.URL+"/api/auth/signup", strings.NewReader(`{}`))
	res, _ := http.DefaultClient.Do(req)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("missing CSRF header: got %d", res.StatusCode)
	}

	var ae apiErr
	if code := alice.do("POST", "/api/auth/signup", map[string]string{"email": "alice@example.com", "password": "short"}, &ae); code != 422 || ae.Error.Field != "password" {
		t.Fatalf("weak password: %d %+v", code, ae)
	}
	var me struct{ User store.User }
	if code := alice.do("POST", "/api/auth/signup", map[string]string{"email": "Alice@example.com", "password": "correct horse battery"}, &me); code != 201 {
		t.Fatalf("signup: %d", code)
	}
	if me.User.IsAdmin || me.User.EmailVerified {
		t.Fatalf("listed admin must not get admin before verifying: %+v", me.User)
	}
	if code := e.client().do("POST", "/api/auth/signup", map[string]string{"email": "alice@EXAMPLE.com", "password": "correct horse battery"}, &ae); code != 409 {
		t.Fatalf("duplicate email: %d", code)
	}

	var page pageResp
	if code := alice.do("POST", "/api/pages", map[string]string{"slug": "admin", "title": "x"}, &ae); code != 422 {
		t.Fatalf("reserved slug: %d", code)
	}
	if code := alice.do("POST", "/api/pages", map[string]string{"slug": "alice", "title": "Alice"}, &page); code != 201 {
		t.Fatalf("create page: %d", code)
	}
	var avail struct{ Available bool }
	alice.do("GET", "/api/slugs/alice", nil, &avail)
	if avail.Available {
		t.Fatal("slug should be taken")
	}

	blocksIn := []map[string]any{
		{"type": "service", "data": map[string]any{"service": "instagram", "value": "@alice"}},
		{"type": "service", "data": map[string]any{"service": "cashapp", "value": "$alice"}},
		{"type": "service", "data": map[string]any{"service": "bitcoin", "value": "bc1qxy2kgdygjrsqtzq2n0yrf2493p83kkfjhx0wlh"}},
		{"type": "link", "data": map[string]any{"url": "https://example.com", "title": "<script>alert(1)</script>"}},
		{"type": "contact", "data": map[string]any{"name": "Alice A", "email": "alice@example.com"}},
		{"type": "qr", "data": map[string]any{"value": "hello", "label": "Scan me"}},
	}
	var ids []string
	for _, b := range blocksIn {
		var out struct{ ID string }
		if code := alice.do("POST", "/api/pages/"+page.ID+"/blocks", b, &out); code != 201 {
			t.Fatalf("create block %v: %d", b, code)
		}
		ids = append(ids, out.ID)
	}
	if code := alice.do("POST", "/api/pages/"+page.ID+"/blocks", map[string]any{"type": "link", "data": map[string]any{"url": "javascript:alert(1)", "title": "x"}}, &ae); code != 422 || ae.Error.Field != "url" {
		t.Fatalf("bad link: %d %+v", code, ae)
	}

	// Reorder: last block first; partial lists are rejected.
	order := append([]string{ids[len(ids)-1]}, ids[:len(ids)-1]...)
	if code := alice.do("PUT", "/api/pages/"+page.ID+"/blocks/order", map[string]any{"ids": order[:2]}, &ae); code != 422 {
		t.Fatalf("partial order: %d", code)
	}
	if code := alice.do("PUT", "/api/pages/"+page.ID+"/blocks/order", map[string]any{"ids": order}, nil); code != 204 {
		t.Fatalf("reorder: %d", code)
	}
	alice.do("GET", "/api/pages/"+page.ID, nil, &page)
	if page.Blocks[0].Label != "Scan me" {
		t.Fatalf("reorder not applied: %+v", page.Blocks)
	}

	// Theme: switching preset resets colors; bad colors rejected.
	if code := alice.do("PATCH", "/api/pages/"+page.ID, map[string]any{"theme": map[string]any{"preset": "paper"}}, &page); code != 200 || page.Theme.Colors.BG != "#f4efe6" {
		t.Fatalf("theme: %d %+v", code, page.Theme)
	}
	if code := alice.do("PATCH", "/api/pages/"+page.ID, map[string]any{"theme": map[string]any{"colors": map[string]string{"bg": "red"}}}, &ae); code != 422 {
		t.Fatalf("bad color: %d", code)
	}

	// Unpublished pages are hidden; publishing needs a verified email.
	anon := e.client()
	if code, _, _ := anon.get("/alice"); code != 404 {
		t.Fatalf("draft visible: %d", code)
	}
	if code := alice.do("PATCH", "/api/pages/"+page.ID, map[string]any{"published": true}, &ae); code != 403 || ae.Error.Code != "email_unverified" {
		t.Fatalf("publish unverified: %d %+v", code, ae)
	}
	tok := e.mail.tokenFor(t, "Alice@example.com", "/app/verify")
	if code := anon.do("POST", "/api/auth/verify", map[string]string{"token": tok}, &me); code != 200 || !me.User.EmailVerified || !me.User.IsAdmin {
		t.Fatalf("verify: %d", code)
	}
	if code := anon.do("POST", "/api/auth/verify", map[string]string{"token": tok}, &ae); code != 400 {
		t.Fatalf("token reuse: %d", code)
	}
	alice.do("GET", "/api/auth/me", nil, &me)
	if code := alice.do("PATCH", "/api/pages/"+page.ID, map[string]any{"published": true}, &page); code != 200 || !page.Published {
		t.Fatalf("publish: %d", code)
	}

	// Preview is owner-only; public page renders escaped content and a strict CSP.
	if code, _, _ := anon.get("/preview/" + page.ID); code != 404 {
		t.Fatalf("anon preview: %d", code)
	}
	if code, body, _ := alice.get("/preview/" + page.ID); code != 200 || !strings.Contains(body, "Preview of your published page") {
		t.Fatalf("owner preview: %d", code)
	}
	code, body, hdr := anon.get("/alice", "User-Agent", "Mozilla/5.0")
	if code != 200 {
		t.Fatalf("public page: %d", code)
	}
	for _, want := range []string{
		`href="https://www.instagram.com/alice/"`, `rel="me noopener"`, `https://cash.app/$alice`,
		`data-copy="bc1qxy2kgdygjrsqtzq2n0yrf2493p83kkfjhx0wlh"`, `&lt;script&gt;alert(1)&lt;/script&gt;`,
		`href="/vcard/`, `<svg xmlns="http://www.w3.org/2000/svg"`, `--bg:#f4efe6`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("public page missing %q", want)
		}
	}
	if strings.Contains(body, "<script>alert") {
		t.Error("unescaped script in page")
	}
	if csp := hdr.Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") || !strings.Contains(csp, "nonce-") {
		t.Errorf("csp: %q", csp)
	}
	if code, _, _ := anon.get("/ALICE"); code != 301 {
		t.Errorf("uppercase slug should redirect, got %d", code)
	}

	// vCard and click tracking feed stats.
	if code, vcf, h := anon.get("/vcard/" + ids[4]); code != 200 || !strings.Contains(vcf, "FN:Alice A") || !strings.HasPrefix(h.Get("Content-Type"), "text/vcard") {
		t.Fatalf("vcard: %d %q", code, vcf)
	}
	req, _ = http.NewRequest("POST", e.ts.URL+"/t/"+ids[0], nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	res, _ = http.DefaultClient.Do(req)
	if res.StatusCode != 204 {
		t.Fatalf("track: %d", res.StatusCode)
	}
	var st store.Stats
	alice.do("GET", "/api/pages/"+page.ID+"/stats?days=7", nil, &st)
	if st.TotalViews != 1 || len(st.Views) != 7 || len(st.Blocks) != 1 || st.Blocks[0].Label != "Instagram" {
		t.Fatalf("stats: %+v", st)
	}

	// Another user can't see or edit Alice's page.
	bob := e.client()
	bob.do("POST", "/api/auth/signup", map[string]string{"email": "bob@example.com", "password": "correct horse battery"}, nil)
	if code := bob.do("GET", "/api/pages/"+page.ID, nil, &ae); code != 404 {
		t.Fatalf("bob read: %d", code)
	}
	if code := bob.do("DELETE", "/api/pages/"+page.ID+"/blocks/"+ids[0], nil, &ae); code != 404 {
		t.Fatalf("bob delete block: %d", code)
	}
	if code := bob.do("GET", "/api/admin/reports", nil, &ae); code != 403 {
		t.Fatalf("bob admin: %d", code)
	}

	// Uploads: images only, owner-checked when referenced.
	upID := uploadPNG(t, e, alice)
	if code := alice.do("PATCH", "/api/pages/"+page.ID, map[string]any{"avatar_upload_id": upID}, &page); code != 200 {
		t.Fatalf("set avatar: %d", code)
	}
	if code := bob.do("POST", "/api/pages", map[string]string{"slug": "bobby"}, &page); code != 201 {
		t.Fatalf("bob page: %d", code)
	}
	if code := bob.do("PATCH", "/api/pages/"+page.ID, map[string]any{"avatar_upload_id": upID}, &ae); code != 422 {
		t.Fatalf("bob used alice's upload: %d", code)
	}
	if code, _, h := anon.get("/u/" + upID); code != 200 || h.Get("Content-Type") != "image/png" {
		t.Fatalf("serve upload: %d %s", code, h.Get("Content-Type"))
	}

	// Custom domain: verify via DNS, then serve on that Host with TLS ask approval.
	alice.do("GET", "/api/pages", nil, nil)
	var pages []struct{ ID string }
	alice.do("GET", "/api/pages", nil, &pages)
	pid := pages[0].ID
	if code := alice.do("POST", "/api/pages/"+pid+"/domains", map[string]string{"hostname": "https://Links.Alice.Example/"}, &ae); code != 422 {
		t.Fatalf("hostname with path accepted: %d", code)
	}
	var dom struct {
		ID, TXTName, TXTValue string `json:"-"`
	}
	var domRaw map[string]any
	if code := alice.do("POST", "/api/pages/"+pid+"/domains", map[string]string{"hostname": "Links.Alice.Example"}, &domRaw); code != 201 {
		t.Fatalf("add domain: %d %v", code, domRaw)
	}
	dom.ID, dom.TXTName, dom.TXTValue = domRaw["id"].(string), domRaw["txt_name"].(string), domRaw["txt_value"].(string)
	e.srv.SetResolver(fakeDNS{txt: map[string][]string{}, ip: map[string]string{"edge.example.net": "203.0.113.7"}})
	if code := alice.do("POST", "/api/pages/"+pid+"/domains/"+dom.ID+"/verify", nil, &ae); code != 422 || ae.Error.Code != "dns_not_ready" {
		t.Fatalf("verify without DNS: %d %+v", code, ae)
	}
	e.srv.SetResolver(fakeDNS{
		txt: map[string][]string{dom.TXTName: {dom.TXTValue}},
		ip:  map[string]string{"edge.example.net": "203.0.113.7", "links.alice.example": "203.0.113.7"},
	})
	if code := alice.do("POST", "/api/pages/"+pid+"/domains/"+dom.ID+"/verify", nil, &domRaw); code != 200 || domRaw["verified"] != true {
		t.Fatalf("verify: %d %v", code, domRaw)
	}
	if code, body, _ := anon.get("/", "Host", "links.alice.example", "User-Agent", "Mozilla/5.0"); code != 200 || !strings.Contains(body, `rel="canonical" href="https://links.alice.example/"`) {
		t.Fatalf("custom domain page: %d", code)
	}
	if code, _, _ := anon.get("/api/pages", "Host", "links.alice.example"); code != 404 {
		t.Fatalf("API reachable on custom domain: %d", code)
	}
	if code, _, _ := anon.get("/internal/tls-ask?domain=links.alice.example"); code != 200 {
		t.Fatalf("tls ask verified: %d", code)
	}
	if code, _, _ := anon.get("/internal/tls-ask?domain=evil.example"); code != 404 {
		t.Fatalf("tls ask unknown: %d", code)
	}

	// Reports and admin suspension take the page down.
	if code := anon.do("POST", "/api/report", map[string]string{"slug": "alice", "reason": "spam"}, nil); code != 204 {
		t.Fatalf("report: %d", code)
	}
	var reports []store.Report
	alice.do("GET", "/api/admin/reports", nil, &reports)
	if len(reports) != 1 {
		t.Fatalf("reports: %+v", reports)
	}
	if code := alice.do("POST", "/api/admin/pages/"+pid+"/suspend", map[string]bool{"suspended": true}, nil); code != 204 {
		t.Fatalf("suspend: %d", code)
	}
	if code, _, _ := anon.get("/alice"); code != 404 {
		t.Fatalf("suspended page visible: %d", code)
	}

	// With custom domains off, the endpoints refuse and the catalog says so.
	e.srv.cfg.CustomDomains = false
	if code := alice.do("POST", "/api/pages/"+pid+"/domains", map[string]string{"hostname": "other.example.org"}, &ae); code != 404 || ae.Error.Code != "custom_domains_disabled" {
		t.Fatalf("domains disabled: %d %+v", code, ae)
	}
	var cat struct{ Features map[string]bool }
	anon.do("GET", "/api/catalog", nil, &cat)
	if cat.Features["custom_domains"] {
		t.Fatal("catalog should report custom domains off")
	}

	// Password reset revokes sessions.
	anon.do("POST", "/api/auth/forgot", map[string]string{"email": "bob@example.com"}, nil)
	rt := e.mail.tokenFor(t, "bob@example.com", "/app/reset")
	if code := anon.do("POST", "/api/auth/reset", map[string]string{"token": rt, "password": "a whole new password"}, nil); code != 204 {
		t.Fatalf("reset: %d", code)
	}
	if code := bob.do("GET", "/api/auth/me", nil, &ae); code != 401 {
		t.Fatalf("old session survived reset: %d", code)
	}
	if code := bob.do("POST", "/api/auth/login", map[string]string{"email": "bob@example.com", "password": "a whole new password"}, &me); code != 200 {
		t.Fatalf("login after reset: %d", code)
	}
}

func uploadPNG(t *testing.T, e *env, c *client) string {
	t.Helper()
	// 1×1 transparent PNG
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82")
	send := func(data []byte) (int, map[string]any) {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, _ := mw.CreateFormFile("file", "x.png")
		fw.Write(data)
		mw.Close()
		req, _ := http.NewRequest("POST", e.ts.URL+"/api/uploads", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("X-Requested-With", "zelda")
		res, err := c.c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}
	if code, _ := send([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)); code != 422 {
		t.Fatalf("svg upload accepted: %d", code)
	}
	code, out := send(png)
	if code != 201 {
		t.Fatalf("png upload: %d %v", code, out)
	}
	return out["id"].(string)
}

func waitABit() { time.Sleep(10 * time.Millisecond) }
