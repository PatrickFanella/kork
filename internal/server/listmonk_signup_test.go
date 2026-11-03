package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/fetch"
	"git.subcult.tv/subculture-collective/zelda/internal/listmonk"
)

// fakeListmonk serves lists 3 and 4 and keeps subscribers by email.
type fakeListmonk struct {
	mu   sync.Mutex
	down bool
	subs map[string]map[int64]string // email -> list -> subscription status
}

func (f *fakeListmonk) server(t *testing.T) *httptest.Server {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("Authorization") != "token kork:lm-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if f.down {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		switch {
		case r.Method == "GET" && (r.URL.Path == "/api/lists/3" || r.URL.Path == "/api/lists/4"):
			w.Write([]byte(`{"data":{}}`))
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/api/lists/"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == "POST" && r.URL.Path == "/api/subscribers":
			var body struct {
				Email string  `json:"email"`
				Lists []int64 `json:"lists"`
			}
			json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
			if _, ok := f.subs[body.Email]; ok {
				w.WriteHeader(http.StatusConflict)
				return
			}
			f.subs[body.Email] = map[int64]string{body.Lists[0]: "confirmed"}
			w.Write([]byte(`{"data":{}}`))
		case r.Method == "GET" && r.URL.Path == "/api/subscribers":
			for email, lists := range f.subs {
				if r.URL.Query().Get("query") == "subscribers.email = '"+email+"'" {
					out := []map[string]any{}
					for id, st := range lists {
						out = append(out, map[string]any{"id": id, "subscription_status": st})
					}
					json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"results": []any{ //nolint:errcheck
						map[string]any{"id": 1, "status": "enabled", "lists": out}}}})
					return
				}
			}
			w.Write([]byte(`{"data":{"results":[]}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

func (f *fakeListmonk) status(email string, list int64) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.subs[email][list]
}

func TestEmailSignupListmonk(t *testing.T) {
	e := setup(t)
	pool := testPool(t)
	fake := &fakeListmonk{subs: map[string]map[int64]string{}}
	ts := fake.server(t)
	fc := fetch.New(fetch.Options{AllowAddr: func(ip netip.Addr) bool { return ip.IsLoopback() }, AllowPort: func(int) bool { return true }})
	fc.SetTLSConfigForTest(ts.Client().Transport.(*http.Transport).TLSClientConfig)
	e.srv.SetListmonk(&listmonk.Client{HTTP: fc})
	if err := e.srv.SetSecretKey(bytes.Repeat([]byte{9}, 32)); err != nil {
		t.Fatal(err)
	}
	clock := &testClock{now: time.Now()}
	e.srv.SetClock(clock.Now)
	alice, pageID := livePage(t, e, "alice@example.com", "alice")
	data := func(extra map[string]any) map[string]any {
		d := map[string]any{"title": "Studio news", "destination": "listmonk", "listmonk_url": ts.URL + "/",
			"listmonk_list_id": "3", "listmonk_user": "kork"}
		for k, v := range extra {
			d[k] = v
		}
		return d
	}

	// ---- Setup is checked with the creator's listmonk ----
	for _, c := range []struct {
		extra map[string]any
		field string
	}{
		{map[string]any{"listmonk_url": "http://lists.example.com", "listmonk_api_token": "lm-token"}, "listmonk_url"},
		{nil, "listmonk_api_token"},
		{map[string]any{"listmonk_api_token": "wrong"}, "listmonk_api_token"},
		{map[string]any{"listmonk_api_token": "lm-token", "listmonk_list_id": "9"}, "listmonk_list_id"},
		{map[string]any{"listmonk_api_token": "lm-token", "listmonk_user": "has space"}, "listmonk_user"},
	} {
		if code, _, ae := alice.createBlock(pageID, "email_signup", data(c.extra)); code != 422 || ae.Error.Field != c.field {
			t.Errorf("%v: %d %+v", c.extra, code, ae)
		}
	}
	var raw json.RawMessage
	if code := alice.do("POST", "/api/pages/"+pageID+"/blocks", map[string]any{"type": "email_signup",
		"data": data(map[string]any{"listmonk_api_token": "lm-token"})}, &raw); code != 201 {
		t.Fatalf("create: %d %s", code, raw)
	}
	var blk blockResp
	json.Unmarshal(raw, &blk) //nolint:errcheck
	if blk.Data["listmonk_key_set"] != true || blk.Data["listmonk_url"] != ts.URL || strings.Contains(string(raw), "lm-token") {
		t.Fatalf("create response: %s", raw)
	}
	if _, body, _ := alice.get("/api/pages/" + pageID); strings.Contains(body, "lm-token") {
		t.Fatal("page API returns the token")
	}
	if _, zip, _ := alice.get("/api/account/export"); strings.Contains(zip, "lm-token") {
		t.Fatal("export contains the token")
	}
	// Editing without resending the token keeps it.
	var upd blockResp
	if code := alice.do("PATCH", "/api/pages/"+pageID+"/blocks/"+blk.ID, map[string]any{"data": data(map[string]any{"title": "Monthly news"})}, &upd); code != 200 ||
		upd.Data["listmonk_key_set"] != true {
		t.Fatalf("update without token: %d %+v", code, upd)
	}

	// ---- The form discloses where addresses go; the receipt records it ----
	host := strings.TrimPrefix(ts.URL, "https://")
	if _, body, _ := e.client().get("/alice"); !strings.Contains(body, "mailing list at "+host+", which they run with listmonk") {
		t.Fatalf("public form has no disclosure:\n%s", body)
	}
	e.subscribeAndConfirm(blk.ID, "fan@example.org")
	if got := fake.status("fan@example.org", 3); got != "confirmed" {
		t.Fatalf("listmonk status %q", got)
	}
	var n int
	pool.QueryRow(context.Background(), `SELECT count(*) FROM signup_contacts WHERE block_id = $1`, blk.ID).Scan(&n) //nolint:errcheck
	if n != 0 {
		t.Fatalf("%d addresses kept after delivery", n)
	}

	// ---- An unsubscribe in listmonk is never reversed ----
	fake.mu.Lock()
	fake.subs["left@example.org"] = map[int64]string{3: "unsubscribed"}
	fake.mu.Unlock()
	e.subscribeAndConfirm(blk.ID, "left@example.org")
	if got := fake.status("left@example.org", 3); got != "unsubscribed" {
		t.Fatalf("unsubscribe reversed: %q", got)
	}

	// ---- A list change doesn't redirect addresses confirmed for the old one ----
	fake.mu.Lock()
	fake.down = true
	fake.mu.Unlock()
	e.subscribeAndConfirm(blk.ID, "waiting@example.org")
	var receipt struct {
		V          int    `json:"v"`
		List       string `json:"list"`
		Sender     string `json:"sender"`
		Purpose    string `json:"purpose"`
		Disclosure string `json:"disclosure"`
	}
	var rawReceipt []byte
	pool.QueryRow(context.Background(), `SELECT consent FROM signup_contacts WHERE block_id = $1`, blk.ID).Scan(&rawReceipt) //nolint:errcheck
	json.Unmarshal(rawReceipt, &receipt)                                                                                     //nolint:errcheck
	if receipt.V != 1 || receipt.List != "listmonk:"+ts.URL+"#3" || receipt.Sender != "Alice" || receipt.Purpose != "Monthly news" ||
		!strings.Contains(receipt.Disclosure, host) {
		t.Fatalf("receipt: %s", rawReceipt)
	}
	if code := alice.do("PATCH", "/api/pages/"+pageID+"/blocks/"+blk.ID, map[string]any{"data": data(map[string]any{"listmonk_list_id": "4",
		"listmonk_api_token": "lm-token"})}, nil); code == 200 {
		t.Fatal("list check should fail while listmonk is down")
	}
	fake.mu.Lock()
	fake.down = false
	fake.mu.Unlock()
	if code := alice.do("PATCH", "/api/pages/"+pageID+"/blocks/"+blk.ID, map[string]any{"data": data(map[string]any{"listmonk_list_id": "4"})}, nil); code != 200 {
		t.Fatalf("switch list: %d", code)
	}
	clock.Set(clock.Now().Add(brevoRetryMax))
	if err := e.srv.SignupJobsOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fake.status("waiting@example.org", 4); got != "" {
		t.Fatal("address confirmed for list 3 was sent to list 4")
	}
	if got := alice.contacts(pageID, blk.ID); got.AwaitingPush != 1 || !strings.Contains(got.PushProblem, "list setting you've since changed") {
		t.Fatalf("owner not told: %+v", got)
	}
	clock.Set(clock.Now().Add(brevoPushGiveUp))
	if err := e.srv.SignupJobsOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := alice.contacts(pageID, blk.ID); got.AwaitingPush != 0 {
		t.Fatalf("held address not dropped: %+v", got)
	}
}
