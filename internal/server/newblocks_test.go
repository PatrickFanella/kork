package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/brevo"
	"git.subcult.tv/subculture-collective/zelda/internal/fetch"
	"git.subcult.tv/subculture-collective/zelda/internal/screen"
)

// Tests for the email sign-up, events and latest blocks (#11).

type blockResp struct {
	ID    string         `json:"id"`
	Label string         `json:"label"`
	Data  map[string]any `json:"data"`
}

// livePage signs up a verified user and publishes an empty page.
func livePage(t *testing.T, e *env, email, slug string) (*client, string) {
	t.Helper()
	c := e.client()
	c.signupVerified(email)
	id := c.newPage(slug)
	if code := c.do("PATCH", "/api/pages/"+id, map[string]any{"published": true, "title": "Alice"}, nil); code != 200 {
		t.Fatalf("publish: %d", code)
	}
	return c, id
}

func (c *client) createBlock(pageID, typ string, data map[string]any) (int, blockResp, apiErr) {
	c.e.t.Helper()
	var raw json.RawMessage
	code := c.do("POST", "/api/pages/"+pageID+"/blocks", map[string]any{"type": typ, "data": data}, &raw)
	var b blockResp
	var ae apiErr
	json.Unmarshal(raw, &b)  //nolint:errcheck
	json.Unmarshal(raw, &ae) //nolint:errcheck
	return code, b, ae
}

// post sends a JSON body with the CSRF header to path on host (the
// main host when host is "").
func (e *env) post(host, path string, body any) (int, map[string]any) {
	e.t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", e.ts.URL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "zelda")
	if host != "" {
		req.Host = host
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	json.NewDecoder(res.Body).Decode(&out) //nolint:errcheck
	return res.StatusCode, out
}

func (e *env) postForm(path string, form url.Values) (int, string) {
	e.t.Helper()
	res, err := http.PostForm(e.ts.URL+path, form)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func (c *captureMail) count(to string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, m := range c.msgs {
		if m.To == to {
			n++
		}
	}
	return n
}

type contactsResp struct {
	Contacts []struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	} `json:"contacts"`
	Pending      int `json:"pending"`
	AwaitingPush int `json:"awaiting_push"`
}

func (c *client) contacts(pageID, blockID string) contactsResp {
	c.e.t.Helper()
	var out contactsResp
	if code := c.do("GET", "/api/pages/"+pageID+"/blocks/"+blockID+"/contacts", nil, &out); code != 200 {
		c.e.t.Fatalf("contacts: %d", code)
	}
	return out
}

// subscribeAndConfirm signs email up to a block and opens the confirmation link.
func (e *env) subscribeAndConfirm(blockID, email string) {
	e.t.Helper()
	if code, _ := e.post("", "/api/subscribe/"+blockID, map[string]string{"email": email}); code != 202 {
		e.t.Fatalf("subscribe %s: %d", email, code)
	}
	tok := e.mail.tokenFor(e.t, email, "/subscribe/confirm")
	if code, body := e.postForm("/subscribe/confirm", url.Values{"token": {tok}}); code != 200 || !strings.Contains(body, "subscribed") {
		e.t.Fatalf("confirm %s: %d %s", email, code, body)
	}
}

func TestEmailSignupKeptInKork(t *testing.T) {
	e := setup(t)
	clock := &testClock{now: time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)}
	e.srv.SetClock(clock.Now)
	pool := testPool(t)
	alice, pageID := livePage(t, e, "alice@example.com", "alice")

	code, blk, ae := alice.createBlock(pageID, "email_signup", map[string]any{"title": "Studio news", "description": "One email a month."})
	if code != 201 || blk.Data["destination"] != "kork" || blk.Label != "Studio news" {
		t.Fatalf("create: %d %+v %+v", code, blk, ae)
	}
	if _, body, _ := e.client().get("/alice"); !strings.Contains(body, `data-signup="`+blk.ID+`"`) || !strings.Contains(body, "One email a month.") ||
		!strings.Contains(body, `name="website"`) {
		t.Fatalf("public form missing:\n%s", body)
	}

	// Writes need the CSRF header.
	req, _ := http.NewRequest("POST", e.ts.URL+"/api/subscribe/"+blk.ID, strings.NewReader(`{"email":"fan@example.org"}`))
	if res, _ := http.DefaultClient.Do(req); res.StatusCode != 403 {
		t.Fatalf("no CSRF header: %d", res.StatusCode)
	}
	if code, out := e.post("", "/api/subscribe/"+blk.ID, map[string]string{"email": "not-an-email"}); code != 422 {
		t.Fatalf("bad email: %d %v", code, out)
	}
	// The honeypot looks like success and sends nothing.
	if code, _ := e.post("", "/api/subscribe/"+blk.ID, map[string]string{"email": "bot@example.org", "website": "http://spam"}); code != 202 {
		t.Fatalf("honeypot: %d", code)
	}
	if code, _ := e.post("", "/api/subscribe/00000000-0000-4000-8000-000000000000", map[string]string{"email": "fan@example.org"}); code != 404 {
		t.Fatalf("unknown block: %d", code)
	}

	code, first := e.post("", "/api/subscribe/"+blk.ID, map[string]string{"email": "fan@example.org"})
	if code != 202 || first["message"] != subscribeAccepted {
		t.Fatalf("subscribe: %d %v", code, first)
	}
	tok := e.mail.tokenFor(t, "fan@example.org", "/subscribe/confirm")
	if got := alice.contacts(pageID, blk.ID); len(got.Contacts) != 0 || got.Pending != 1 {
		t.Fatalf("before confirming: %+v", got)
	}
	// Opening the link shows a button; only the POST confirms.
	if code, body, _ := e.client().get("/subscribe/confirm?token=" + tok); code != 200 || !strings.Contains(body, `name="token" value="`+tok+`"`) {
		t.Fatalf("confirm page: %d %s", code, body)
	}
	if got := alice.contacts(pageID, blk.ID); got.Pending != 1 {
		t.Fatalf("GET confirmed the address: %+v", got)
	}
	if code, body := e.postForm("/subscribe/confirm", url.Values{"token": {tok}}); code != 200 || !strings.Contains(body, "Alice") {
		t.Fatalf("confirm: %d %s", code, body)
	}
	if code, _ := e.postForm("/subscribe/confirm", url.Values{"token": {tok}}); code != 400 {
		t.Fatalf("reused token: %d", code)
	}
	got := alice.contacts(pageID, blk.ID)
	if len(got.Contacts) != 1 || got.Contacts[0].Email != "fan@example.org" || got.Pending != 0 {
		t.Fatalf("after confirming: %+v", got)
	}

	// Signing up again looks the same and sends no mail.
	sent := e.mail.count("fan@example.org")
	if code, again := e.post("", "/api/subscribe/"+blk.ID, map[string]string{"email": "FAN@example.org"}); code != 202 || again["message"] != first["message"] {
		t.Fatalf("repeat: %d %v", code, again)
	}
	waitABit()
	if e.mail.count("FAN@example.org")+e.mail.count("fan@example.org") != sent {
		t.Error("repeat sign-up sent mail")
	}

	// CSV export, with formula-looking values neutralised.
	if _, err := pool.Exec(context.Background(), `INSERT INTO signup_contacts (block_id, email, confirmed_at) VALUES ($1, '=cmd@example.org', now())`, blk.ID); err != nil {
		t.Fatal(err)
	}
	code, csvBody, hdr := alice.get("/api/pages/" + pageID + "/blocks/" + blk.ID + "/contacts.csv")
	if code != 200 || !strings.HasPrefix(hdr.Get("Content-Type"), "text/csv") || !strings.HasPrefix(csvBody, "email,confirmed_at\n") ||
		!strings.Contains(csvBody, "fan@example.org,2026-10-02T12:00:00Z") || !strings.Contains(csvBody, "'=cmd@example.org") {
		t.Fatalf("csv: %d %v\n%s", code, hdr, csvBody)
	}

	// Another account can't see or change the list.
	bob := e.client()
	bob.signupVerified("bob@example.com")
	if code := bob.do("GET", "/api/pages/"+pageID+"/blocks/"+blk.ID+"/contacts", nil, nil); code != 404 {
		t.Fatalf("bob reads alice's list: %d", code)
	}

	// The form works on a verified custom domain.
	if _, err := pool.Exec(context.Background(), `INSERT INTO domains (page_id, hostname, verify_token, verified_at) VALUES ($1, 'links.alice.example', 'x', now())`, pageID); err != nil {
		t.Fatal(err)
	}
	if code, _ := e.post("links.alice.example", "/api/subscribe/"+blk.ID, map[string]string{"email": "custom@example.org"}); code != 202 {
		t.Fatalf("custom domain subscribe: %d", code)
	}
	e.mail.tokenFor(t, "custom@example.org", "/subscribe/confirm")

	// Unconfirmed addresses are deleted after 48 hours.
	clock.Set(clock.Now().Add(49 * time.Hour))
	if err := e.srv.SignupJobsOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := alice.contacts(pageID, blk.ID); got.Pending != 0 || len(got.Contacts) != 2 {
		t.Fatalf("after expiry: %+v", got)
	}

	// The account export includes the list.
	_, zipBody, _ := alice.get("/api/account/export")
	zr, err := zip.NewReader(bytes.NewReader([]byte(zipBody)), int64(len(zipBody)))
	if err != nil {
		t.Fatal(err)
	}
	rc, _ := zr.File[0].Open()
	manifest, _ := io.ReadAll(rc)
	var acct struct {
		Pages []struct {
			SignupContacts []struct {
				BlockID string `json:"block_id"`
				Email   string `json:"email"`
			} `json:"signup_contacts"`
		} `json:"pages"`
	}
	json.Unmarshal(manifest, &acct) //nolint:errcheck
	if len(acct.Pages) != 1 || len(acct.Pages[0].SignupContacts) != 2 || acct.Pages[0].SignupContacts[0].BlockID != blk.ID {
		t.Fatalf("export: %s", manifest)
	}

	// Deleting a contact from the editor.
	if code := alice.do("DELETE", "/api/pages/"+pageID+"/blocks/"+blk.ID+"/contacts/"+got.Contacts[0].ID, nil, nil); code != 204 {
		t.Fatalf("delete contact: %d", code)
	}
	if got := alice.contacts(pageID, blk.ID); len(got.Contacts) != 1 {
		t.Fatalf("after delete: %+v", got)
	}

	// Deleting the account deletes the list.
	if code := alice.do("DELETE", "/api/account", map[string]string{"password": "correct horse battery"}, nil); code != 204 {
		t.Fatalf("delete account: %d", code)
	}
	var n int
	pool.QueryRow(context.Background(), `SELECT count(*) FROM signup_contacts`).Scan(&n) //nolint:errcheck
	if n != 0 {
		t.Fatalf("%d contacts survived account deletion", n)
	}
}

// fakeBrevo is a stand-in for the Brevo API.
type fakeBrevo struct {
	mu    sync.Mutex
	added []string
	fail  bool
}

func (f *fakeBrevo) server(t *testing.T) *httptest.Server {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("api-key") != "xkeysib-good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == "GET" && r.URL.Path == "/v3/contacts/lists/7":
			w.Write([]byte(`{"id":7}`))
		case r.Method == "GET":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == "POST" && r.URL.Path == "/v3/contacts":
			if f.fail {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			var body struct {
				Email   string  `json:"email"`
				ListIDs []int64 `json:"listIds"`
			}
			json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
			if len(body.ListIDs) != 1 || body.ListIDs[0] != 7 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			f.added = append(f.added, body.Email)
			w.WriteHeader(http.StatusCreated)
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

func (f *fakeBrevo) setFail(v bool) { f.mu.Lock(); f.fail = v; f.mu.Unlock() }
func (f *fakeBrevo) list() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.added...)
}

func TestEmailSignupBrevo(t *testing.T) {
	e := setup(t)
	pool := testPool(t)
	fake := &fakeBrevo{}
	ts := fake.server(t)
	e.srv.SetBrevo(&brevo.Client{BaseURL: ts.URL, HTTP: ts.Client()})
	alice, pageID := livePage(t, e, "alice@example.com", "alice")
	data := func(extra map[string]any) map[string]any {
		d := map[string]any{"title": "News", "destination": "brevo", "brevo_list_id": "7"}
		for k, v := range extra {
			d[k] = v
		}
		return d
	}

	// Without ZELDA_SECRET_KEY, Brevo isn't offered.
	if code, _, ae := alice.createBlock(pageID, "email_signup", data(map[string]any{"brevo_api_key": "xkeysib-good"})); code != 422 || ae.Error.Field != "destination" {
		t.Fatalf("no secret key: %d %+v", code, ae)
	}
	var cat struct{ Features map[string]bool }
	alice.do("GET", "/api/catalog", nil, &cat)
	if cat.Features["brevo_signup"] {
		t.Fatal("catalog offers brevo without a key")
	}
	if err := e.srv.SetSecretKey(bytes.Repeat([]byte{9}, 32)); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		extra map[string]any
		field string
	}{
		{nil, "brevo_api_key"},
		{map[string]any{"brevo_api_key": "xkeysib-bad"}, "brevo_api_key"},
		{map[string]any{"brevo_api_key": "xkeysib-good", "brevo_list_id": "8"}, "brevo_list_id"},
		{map[string]any{"brevo_api_key": "xkeysib-good", "brevo_list_id": ""}, "brevo_list_id"},
	} {
		if code, _, ae := alice.createBlock(pageID, "email_signup", data(c.extra)); code != 422 || ae.Error.Field != c.field {
			t.Errorf("%v: %d %+v", c.extra, code, ae)
		}
	}
	var raw json.RawMessage
	if code := alice.do("POST", "/api/pages/"+pageID+"/blocks", map[string]any{"type": "email_signup",
		"data": data(map[string]any{"brevo_api_key": "xkeysib-good"})}, &raw); code != 201 {
		t.Fatalf("create: %d %s", code, raw)
	}
	var blk blockResp
	json.Unmarshal(raw, &blk) //nolint:errcheck
	if blk.Data["brevo_key_set"] != true || blk.Data["brevo_list_id"] != float64(7) || strings.Contains(string(raw), "xkeysib") {
		t.Fatalf("create response: %s", raw)
	}
	var stored []byte
	if err := pool.QueryRow(context.Background(), `SELECT brevo_key FROM signup_secrets WHERE block_id = $1`, blk.ID).Scan(&stored); err != nil ||
		bytes.Contains(stored, []byte("xkeysib")) {
		t.Fatalf("stored key: %v %q", err, stored)
	}
	if _, body, _ := alice.get("/api/pages/" + pageID); strings.Contains(body, "xkeysib") {
		t.Fatal("page API returns the key")
	}
	_, zipBody, _ := alice.get("/api/account/export")
	if bytes.Contains([]byte(zipBody), []byte("xkeysib")) {
		t.Fatal("export contains the key")
	}

	// Editing without resending the key keeps it.
	var upd blockResp
	if code := alice.do("PATCH", "/api/pages/"+pageID+"/blocks/"+blk.ID, map[string]any{"data": data(map[string]any{"title": "Monthly news"})}, &upd); code != 200 ||
		upd.Data["brevo_key_set"] != true {
		t.Fatalf("update without key: %d %+v", code, upd)
	}

	// A confirmed address goes to Brevo and is deleted here.
	e.subscribeAndConfirm(blk.ID, "fan@example.org")
	if got := fake.list(); len(got) != 1 || got[0] != "fan@example.org" {
		t.Fatalf("brevo received %v", got)
	}
	var n int
	pool.QueryRow(context.Background(), `SELECT count(*) FROM signup_contacts WHERE block_id = $1`, blk.ID).Scan(&n) //nolint:errcheck
	if n != 0 {
		t.Fatalf("%d addresses kept after delivery", n)
	}

	// When Brevo is down, the address waits and is retried.
	fake.setFail(true)
	e.subscribeAndConfirm(blk.ID, "later@example.org")
	if got := alice.contacts(pageID, blk.ID); got.AwaitingPush != 1 || len(got.Contacts) != 0 {
		t.Fatalf("waiting: %+v", got)
	}
	fake.setFail(false)
	if err := e.srv.SignupJobsOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fake.list(); len(got) != 2 || got[1] != "later@example.org" {
		t.Fatalf("retry: %v", got)
	}
	if got := alice.contacts(pageID, blk.ID); got.AwaitingPush != 0 {
		t.Fatalf("after retry: %+v", got)
	}

	// Switching to Kork storage removes the key.
	upd = blockResp{}
	if code := alice.do("PATCH", "/api/pages/"+pageID+"/blocks/"+blk.ID, map[string]any{"data": map[string]any{"title": "News", "destination": "kork"}}, &upd); code != 200 ||
		upd.Data["brevo_key_set"] != nil {
		t.Fatalf("switch to kork: %d %+v", code, upd)
	}
	pool.QueryRow(context.Background(), `SELECT count(*) FROM signup_secrets`).Scan(&n) //nolint:errcheck
	if n != 0 {
		t.Fatal("key kept after switching to kork")
	}
}

func TestEventsBlock(t *testing.T) {
	e := setup(t)
	clock := &testClock{now: time.Date(2026, 10, 2, 18, 0, 0, 0, time.UTC)}
	e.srv.SetClock(clock.Now)
	fake := &screen.Fake{}
	fake.Flag("phish.example", "SOCIAL_ENGINEERING")
	e.srv.SetScreener(fake)
	alice, pageID := livePage(t, e, "alice@example.com", "alice")

	if code, _, ae := alice.createBlock(pageID, "events", map[string]any{"events": []map[string]any{{"date": "2026-10-20", "title": "x", "ticket_url": "https://phish.example/t"}}}); code != 422 ||
		ae.Error.Code != "blocked_destination" || ae.Error.Field != "events" {
		t.Fatalf("blocked ticket link: %d %+v", code, ae)
	}
	if code, _, ae := alice.createBlock(pageID, "events", map[string]any{"events": []map[string]any{{"date": "20 Oct", "title": "x"}}}); code != 422 || ae.Error.Field != "events" {
		t.Fatalf("bad date: %d %+v", code, ae)
	}
	code, blk, ae := alice.createBlock(pageID, "events", map[string]any{"events": []map[string]any{
		{"date": "2026-10-09", "time": "20:30", "title": "The Hall", "city": "Chicago", "ticket_url": "https://tix.example/hall"},
		{"date": "2026-09-30", "title": "Last week"},
		{"date": "2026-10-02", "title": "Tonight", "sold_out": true},
	}})
	if code != 201 {
		t.Fatalf("create: %d %+v", code, ae)
	}
	_, body, _ := e.client().get("/alice")
	tonight, hall := strings.Index(body, "Tonight"), strings.Index(body, "The Hall")
	if strings.Contains(body, "Last week") || tonight < 0 || hall < tonight || !strings.Contains(body, "Sold out") ||
		!strings.Contains(body, `href="https://tix.example/hall"`) || !strings.Contains(body, `data-track="`+blk.ID+`"`) ||
		!strings.Contains(body, "Fri · 8:30 PM · Chicago") {
		t.Fatalf("events markup:\n%s", body)
	}
	// Ticket clicks count against the block.
	if code, _ := e.post("", "/t/"+blk.ID, nil); code != 204 {
		t.Fatalf("track: %d", code)
	}

	clock.Set(time.Date(2026, 10, 3, 13, 0, 0, 0, time.UTC))
	if _, body, _ := e.client().get("/alice"); strings.Contains(body, "Tonight") || !strings.Contains(body, "The Hall") {
		t.Fatal("yesterday's event still shown")
	}
	clock.Set(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	if _, body, _ := e.client().get("/alice"); strings.Contains(body, "item-events") {
		t.Fatal("block with only past events rendered")
	}
}

// feedSite serves YouTube and blog fixtures. Thumbnails point back at it.
type feedSite struct {
	ts       *httptest.Server
	hits     sync.Map // path -> *int64
	video    atomic.Value
	feedDown atomic.Bool
}

func (f *feedSite) count(path string) int64 {
	v, _ := f.hits.LoadOrStore(path, new(int64))
	return atomic.LoadInt64(v.(*int64))
}

func newFeedSite(t *testing.T) *feedSite {
	t.Helper()
	read := func(name string) string {
		b, err := os.ReadFile("../feed/testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	yt, rss, channel := read("youtube.xml"), read("rss.xml"), read("youtube-channel.html")
	f := &feedSite{}
	f.video.Store("AAAAAAAAAAA")
	f.ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, _ := f.hits.LoadOrStore(r.URL.Path, new(int64))
		atomic.AddInt64(v.(*int64), 1)
		base := f.ts.URL
		switch {
		case r.URL.Path == "/@alicedraws":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(channel))
		case r.URL.Path == "/feeds/videos.xml" && r.URL.Query().Get("channel_id") == "UCabcdefghijklmnopqrstuv":
			if f.feedDown.Load() {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			id := f.video.Load().(string)
			body := strings.ReplaceAll(yt, "https://i1.ytimg.com", base)
			body = strings.ReplaceAll(body, "AAAAAAAAAAA", id)
			if id != "AAAAAAAAAAA" {
				body = strings.ReplaceAll(body, "Inking a zine cover", "Binding the zine")
			}
			w.Header().Set("Content-Type", "application/atom+xml")
			w.Write([]byte(body))
		case r.URL.Path == "/blog/feed.xml":
			w.Header().Set("Content-Type", "application/rss+xml")
			w.Write([]byte(strings.ReplaceAll(rss, "https://cdn.alice.example", base)))
		case r.URL.Path == "/blog":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<html><body>not a feed</body></html>`))
		case strings.HasPrefix(r.URL.Path, "/vi/"), r.URL.Path == "/newest.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write(tinyPNG)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.ts.Close)
	return f
}

var uploadSrc = regexp.MustCompile(`class="latest-thumb" src="/u/([0-9a-f-]{36})"`)

func TestLatestBlock(t *testing.T) {
	e := setup(t)
	clock := &testClock{now: time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)}
	e.srv.SetClock(clock.Now)
	site := newFeedSite(t)
	e.srv.SetFetcher(allowLoopback())
	e.srv.SetYouTubeBase(site.ts.URL)
	alice, pageID := livePage(t, e, "alice@example.com", "alice")

	// Bad sources are reported on save.
	if code, _, ae := alice.createBlock(pageID, "latest", map[string]any{"source": site.ts.URL + "/blog"}); code != 422 ||
		ae.Error.Field != "source" || !strings.Contains(ae.Error.Message, "isn't an RSS or Atom feed") {
		t.Fatalf("html source: %d %+v", code, ae)
	}
	if code, _, ae := alice.createBlock(pageID, "latest", map[string]any{"source": "@nobodyhere"}); code != 422 || ae.Error.Field != "source" {
		t.Fatalf("unknown handle: %d %+v", code, ae)
	}

	code, blk, ae := alice.createBlock(pageID, "latest", map[string]any{"source": "@alicedraws"})
	if code != 201 || blk.Data["source"] != "https://www.youtube.com/@alicedraws" || blk.Label != "Latest video: @alicedraws" {
		t.Fatalf("create: %d %+v %+v", code, blk, ae)
	}
	_, body, _ := e.client().get("/alice")
	m := uploadSrc.FindStringSubmatch(body)
	if !strings.Contains(body, "Inking a zine cover") || !strings.Contains(body, `href="https://www.youtube.com/watch?v=AAAAAAAAAAA"`) || m == nil ||
		!strings.Contains(body, "Sep 28, 2026") {
		t.Fatalf("latest markup:\n%s", body)
	}
	firstThumb := m[1]
	if code, img, hdr := e.client().get("/u/" + firstThumb); code != 200 || img != string(tinyPNG) || hdr.Get("Content-Type") != "image/png" {
		t.Fatalf("thumbnail upload: %d %v", code, hdr)
	}
	if strings.Contains(body, "ytimg") || strings.Contains(body, site.ts.URL) {
		t.Fatal("page hotlinks a remote image")
	}

	// Within the TTL, renders use the cache.
	feedHits := site.count("/feeds/videos.xml")
	e.client().get("/alice")
	e.srv.WaitFeedRefreshes()
	if site.count("/feeds/videos.xml") != feedHits {
		t.Fatal("fresh cache refetched")
	}
	// A second block for the same handle reuses the cached channel ID.
	if code, _, _ := alice.createBlock(pageID, "latest", map[string]any{"source": "youtube.com/@alicedraws", "label": "New on YouTube"}); code != 201 {
		t.Fatalf("second block: %d", code)
	}
	if n := site.count("/@alicedraws"); n != 1 {
		t.Fatalf("channel page fetched %d times", n)
	}

	// After the TTL, the stale item is served while a refresh runs.
	site.video.Store("DDDDDDDDDDD")
	clock.Set(clock.Now().Add(2 * time.Hour))
	if _, body, _ := e.client().get("/alice"); !strings.Contains(body, "Inking a zine cover") {
		t.Fatal("stale render didn't serve the cached item")
	}
	e.srv.WaitFeedRefreshes()
	_, body, _ = e.client().get("/alice")
	if !strings.Contains(body, "Binding the zine") || strings.Contains(body, "Inking a zine cover") {
		t.Fatalf("refreshed markup:\n%s", body)
	}
	if code, _, _ := e.client().get("/u/" + firstThumb); code != 404 {
		t.Fatalf("old thumbnail kept: %d", code)
	}

	// A failing feed keeps the last good item.
	site.feedDown.Store(true)
	clock.Set(clock.Now().Add(2 * time.Hour))
	e.client().get("/alice")
	e.srv.WaitFeedRefreshes()
	if _, body, _ := e.client().get("/alice"); !strings.Contains(body, "Binding the zine") {
		t.Fatal("feed failure dropped the cached item")
	}

	// Any RSS feed works.
	code, rssBlk, ae := alice.createBlock(pageID, "latest", map[string]any{"source": site.ts.URL + "/blog/feed.xml"})
	if code != 201 {
		t.Fatalf("rss block: %d %+v", code, ae)
	}
	_, body, _ = e.client().get("/alice")
	if !strings.Contains(body, "Newest post &amp; more notes") || !strings.Contains(body, "Latest post") {
		t.Fatalf("rss markup:\n%s", body)
	}

	// Deleting the block deletes its thumbnail copy.
	thumb, ok := e.srv.store.FeedThumbnail(context.Background(), rssBlk.ID)
	if !ok {
		t.Fatal("rss block has no thumbnail")
	}
	if code := alice.do("DELETE", "/api/pages/"+pageID+"/blocks/"+rssBlk.ID, nil, nil); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if code, _, _ := e.client().get("/u/" + thumb); code != 404 {
		t.Fatalf("thumbnail survived block deletion: %d", code)
	}

	// The real fetch policy refuses private addresses.
	e.srv.SetFetcher(fetch.New(fetch.Options{}))
	if code, _, ae := alice.createBlock(pageID, "latest", map[string]any{"source": "http://127.0.0.1/feed.xml"}); code != 422 ||
		!strings.Contains(ae.Error.Message, "private or local network") {
		t.Fatalf("private address: %d %+v", code, ae)
	}
}
