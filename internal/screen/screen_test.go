package screen

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBlocklist(t *testing.T) {
	b := NewBlocklist(
		"# comment",
		"evil.example",
		"0.0.0.0 malware.test",
		"127.0.0.1\tBAD.Example.org.",
		"https://sites.example.net/phish/login#x",
		"",
	)
	cases := map[string]bool{
		"https://evil.example/":                 true,
		"http://login.evil.example/a?b=c":       true,
		"https://notevil.example/":              false,
		"https://malware.test/x":                true,
		"https://bad.example.org":               true,
		"https://example.org":                   false,
		"https://sites.example.net/phish/login": true,
		"http://SITES.example.net/phish/login":  true,
		"https://sites.example.net/other":       false,
		"https://evil.example.com/":             false,
	}
	urls := make([]string, 0, len(cases))
	for u := range cases {
		urls = append(urls, u)
	}
	res, err := b.Check(context.Background(), urls)
	if err != nil {
		t.Fatal(err)
	}
	for u, want := range cases {
		if res[u].Unsafe != want {
			t.Errorf("%s: unsafe=%v, want %v", u, res[u].Unsafe, want)
		}
	}
}

func TestBlocklistRefreshAndLoad(t *testing.T) {
	feed := "# URLhaus style\n127.0.0.1\tfeed-bad.example\n"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, feed) }))
	defer ts.Close()
	file := filepath.Join(t.TempDir(), "sub", "blocklist.txt")
	b := &Blocklist{File: file, FeedURL: ts.URL, Client: ts.Client()}
	if err := b.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b.Len() != 1 {
		t.Fatalf("entries: %d", b.Len())
	}
	saved, err := os.ReadFile(file)
	if err != nil || string(saved) != feed {
		t.Fatalf("saved feed: %q %v", saved, err)
	}
	// An empty download keeps the current list.
	feed = "# nothing\n"
	if err := b.Refresh(context.Background()); err == nil || b.Len() != 1 {
		t.Fatalf("empty feed replaced list: %v %d", err, b.Len())
	}
	fresh := &Blocklist{File: file}
	if err := fresh.Load(); err != nil || fresh.Len() != 1 {
		t.Fatalf("load: %v %d", err, fresh.Len())
	}
}

func TestSafeBrowsing(t *testing.T) {
	var got sbRequest
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "k123" || r.Method != http.MethodPost {
			http.Error(w, "bad", http.StatusForbidden)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		io.WriteString(w, `{"matches":[{"threatType":"SOCIAL_ENGINEERING","threat":{"url":"https://phish.example/login"}}]}`)
	}))
	defer ts.Close()
	sb := &SafeBrowsing{Key: "k123", Endpoint: ts.URL, Client: ts.Client()}
	res, err := sb.Check(context.Background(), []string{"https://phish.example/login", "https://ok.example/"})
	if err != nil {
		t.Fatal(err)
	}
	if v := res["https://phish.example/login"]; !v.Unsafe || v.Threat != "SOCIAL_ENGINEERING" {
		t.Errorf("phish verdict: %+v", v)
	}
	if v, ok := res["https://ok.example/"]; !ok || v.Unsafe {
		t.Errorf("safe verdict: %+v %v", v, ok)
	}
	if len(got.ThreatInfo.ThreatEntries) != 2 || got.Client.ClientID == "" {
		t.Errorf("request: %+v", got)
	}

	// Empty body means no matches.
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "{}") }))
	defer empty.Close()
	sb = &SafeBrowsing{Key: "k", Endpoint: empty.URL, Client: empty.Client()}
	if res, err := sb.Check(context.Background(), []string{"https://a.example/"}); err != nil || res["https://a.example/"].Unsafe {
		t.Errorf("empty: %+v %v", res, err)
	}

	// Errors don't leak the key.
	sb = &SafeBrowsing{Key: "secret-key", Endpoint: "http://127.0.0.1:1", Client: &http.Client{Timeout: time.Second}}
	if _, err := sb.Check(context.Background(), []string{"https://a.example/"}); err == nil || strings.Contains(err.Error(), "secret-key") {
		t.Errorf("error: %v", err)
	}
}

func TestMulti(t *testing.T) {
	failing := &Fake{Err: errors.New("down")}
	m := Multi{failing, NewBlocklist("evil.example"), &Fake{Unsafe: map[string]string{"phish.example": "SOCIAL_ENGINEERING"}}}
	res, err := m.Check(context.Background(), []string{"https://a.evil.example/", "https://phish.example/x", "https://ok.example/"})
	if err == nil {
		t.Error("provider error not reported")
	}
	if !res["https://a.evil.example/"].Unsafe || res["https://phish.example/x"].Threat != "SOCIAL_ENGINEERING" || res["https://ok.example/"].Unsafe {
		t.Errorf("verdicts: %+v", res)
	}
}

func TestSafeClientRefusesPrivateAddresses(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()
	_, err := SafeClient(5 * time.Second).Get(ts.URL)
	if err == nil || !errors.Is(err, errBlockedAddr) {
		t.Fatalf("loopback allowed: %v", err)
	}
	for _, a := range []string{"10.0.0.1", "192.168.1.1", "169.254.169.254", "100.65.1.1", "::1", "fd00::1", "0.0.0.0", "::ffff:127.0.0.1"} {
		if publicIP(netip.MustParseAddr(a)) {
			t.Errorf("%s treated as public", a)
		}
	}
	if !publicIP(netip.MustParseAddr("8.8.8.8")) || !publicIP(netip.MustParseAddr("2606:4700::1111")) {
		t.Error("public address refused")
	}
}
