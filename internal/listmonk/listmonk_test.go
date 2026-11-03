package listmonk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"git.subcult.tv/subculture-collective/zelda/internal/fetch"
)

// fake is a minimal listmonk: one list (ID 3), subscribers by email.
type fake struct {
	mu    sync.Mutex
	subs  map[string]*fakeSub
	calls []string
	auth  string
}

type fakeSub struct {
	id          int64
	status      string
	lists       map[int64]string
	createCalls int
}

func newFake(t *testing.T) (*fake, *httptest.Server) {
	f := &fake{subs: map[string]*fakeSub{}}
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = append(f.calls, r.Method+" "+r.URL.Path)
		f.auth = r.Header.Get("Authorization")
		if f.auth != "token kork:secret-token" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch {
		case r.Method == "GET" && r.URL.Path == "/lm/api/lists/3":
			w.Write([]byte(`{"data":{"id":3,"name":"News"}}`))
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/lm/api/lists/"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == "POST" && r.URL.Path == "/lm/api/subscribers":
			var body struct {
				Email string  `json:"email"`
				Lists []int64 `json:"lists"`
				Pre   bool    `json:"preconfirm_subscriptions"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if s, ok := f.subs[body.Email]; ok {
				s.createCalls++
				w.WriteHeader(http.StatusConflict)
				w.Write([]byte(`{"message":"E-mail already exists."}`))
				return
			}
			s := &fakeSub{id: int64(len(f.subs) + 1), status: "enabled", lists: map[int64]string{}}
			for _, l := range body.Lists {
				s.lists[l] = map[bool]string{true: "confirmed", false: "unconfirmed"}[body.Pre]
			}
			f.subs[body.Email] = s
			w.Write([]byte(`{"data":{}}`))
		case r.Method == "GET" && r.URL.Path == "/lm/api/subscribers":
			q := r.URL.Query().Get("query")
			for email, s := range f.subs {
				if q == "subscribers.email = '"+email+"'" {
					lists := []map[string]any{}
					for id, st := range s.lists {
						lists = append(lists, map[string]any{"id": id, "subscription_status": st})
					}
					json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"results": []any{
						map[string]any{"id": s.id, "status": s.status, "lists": lists}}}})
					return
				}
			}
			w.Write([]byte(`{"data":{"results":[]}}`))
		case r.Method == "PUT" && r.URL.Path == "/lm/api/subscribers/lists":
			var body struct {
				IDs     []int64 `json:"ids"`
				Targets []int64 `json:"target_list_ids"`
				Status  string  `json:"status"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			for _, s := range f.subs {
				if s.id == body.IDs[0] {
					s.lists[body.Targets[0]] = body.Status
				}
			}
			w.Write([]byte(`{"data":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(ts.Close)
	return f, ts
}

func testClient(ts *httptest.Server) *Client {
	fc := fetch.New(fetch.Options{
		AllowAddr: func(ip netip.Addr) bool { return ip.IsLoopback() },
		AllowPort: func(int) bool { return true },
	})
	fc.SetTLSConfigForTest(ts.Client().Transport.(*http.Transport).TLSClientConfig)
	return &Client{HTTP: fc}
}

func TestCheckListAndSubscribe(t *testing.T) {
	f, ts := newFake(t)
	c := testClient(ts)
	ctx := context.Background()
	a := Account{BaseURL: ts.URL + "/lm", User: "kork", Token: "secret-token"}

	if err := c.CheckList(ctx, a, 3); err != nil {
		t.Fatalf("check: %v", err)
	}
	if err := c.CheckList(ctx, a, 9); !errors.Is(err, ErrListNotFound) || !Rejected(err) {
		t.Fatalf("missing list: %v", err)
	}
	bad := a
	bad.Token = "wrong"
	if err := c.CheckList(ctx, bad, 3); !errors.Is(err, ErrUnauthorized) || !Rejected(err) {
		t.Fatalf("bad token: %v", err)
	}

	// New address: added as confirmed.
	if out, err := c.Subscribe(ctx, a, 3, "fan@example.org"); err != nil || out != Added || f.subs["fan@example.org"].lists[3] != "confirmed" {
		t.Fatalf("new: %v %v", out, err)
	}
	// Repeating is a no-op.
	if out, err := c.Subscribe(ctx, a, 3, "fan@example.org"); err != nil || out != AlreadyOnList {
		t.Fatalf("repeat: %v %v", out, err)
	}
	// An existing subscriber on another list is added to this one.
	f.subs["other@example.org"] = &fakeSub{id: 50, status: "enabled", lists: map[int64]string{1: "confirmed"}}
	if out, err := c.Subscribe(ctx, a, 3, "other@example.org"); err != nil || out != Added || f.subs["other@example.org"].lists[3] != "confirmed" {
		t.Fatalf("existing: %v %v", out, err)
	}
	// Unsubscribed and blocklisted addresses are never changed.
	f.subs["left@example.org"] = &fakeSub{id: 51, status: "enabled", lists: map[int64]string{3: "unsubscribed"}}
	if out, err := c.Subscribe(ctx, a, 3, "left@example.org"); err != nil || out != Unsubscribed || f.subs["left@example.org"].lists[3] != "unsubscribed" {
		t.Fatalf("unsubscribed: %v %v", out, err)
	}
	f.subs["blocked@example.org"] = &fakeSub{id: 52, status: "blocklisted", lists: map[int64]string{}}
	if out, err := c.Subscribe(ctx, a, 3, "blocked@example.org"); err != nil || out != Blocklisted || len(f.subs["blocked@example.org"].lists) != 0 {
		t.Fatalf("blocklisted: %v %v", out, err)
	}
	for _, call := range f.calls {
		if strings.HasPrefix(call, "PUT") && strings.Contains(call, "left") {
			t.Fatal("unsubscribed address was modified")
		}
	}
}

// The token must not follow a redirect to another host, and private
// addresses are refused by the fetch client.
func TestNoRedirectsAndPrivateAddresses(t *testing.T) {
	_, ts := newFake(t)
	redir := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, ts.URL+"/lm"+r.URL.Path, http.StatusFound)
	}))
	defer redir.Close()
	c := testClient(redir)
	err := c.CheckList(context.Background(), Account{BaseURL: redir.URL, User: "kork", Token: "secret-token"}, 3)
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != http.StatusFound {
		t.Fatalf("redirect was followed or misreported: %v", err)
	}

	strict := &Client{HTTP: fetch.New(fetch.Options{AllowPort: func(int) bool { return true }})}
	if err := strict.CheckList(context.Background(), Account{BaseURL: ts.URL, User: "kork", Token: "secret-token"}, 3); !errors.Is(err, fetch.ErrBlocked) {
		t.Fatalf("loopback listmonk allowed: %v", err)
	}
}
