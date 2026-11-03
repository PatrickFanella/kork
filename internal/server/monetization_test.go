package server

import (
	"strings"
	"testing"

	"git.subcult.tv/subculture-collective/zelda/internal/screen"
)

// A page can carry a Web Monetization wallet address, rendered as one
// <link rel="monetization"> on the live page only (#37).
func TestMonetizationWallet(t *testing.T) {
	e := setup(t)
	fake := &screen.Fake{}
	fake.Flag("phish.example", "SOCIAL_ENGINEERING")
	e.srv.SetScreener(fake)
	alice, pageID := livePage(t, e, "alice@example.com", "alice")
	link := func(body string) int { return strings.Count(body, `rel="monetization"`) }

	if _, body, _ := e.client().get("/alice"); link(body) != 0 {
		t.Fatal("link rendered without a wallet")
	}
	for _, bad := range []string{"http://wallet.example/alice", "wallet.example/alice", "$", "https://wallet.example/a?x=1", "javascript:alert(1)", "https://localhost/a"} {
		var ae apiErr
		if code := alice.do("PATCH", "/api/pages/"+pageID, map[string]any{"monetization_wallet": bad}, &ae); code != 422 || ae.Error.Field != "monetization_wallet" {
			t.Errorf("%q: %d %+v", bad, code, ae)
		}
	}
	var ae apiErr
	if code := alice.do("PATCH", "/api/pages/"+pageID, map[string]any{"monetization_wallet": "https://phish.example/alice"}, &ae); code != 422 ||
		ae.Error.Code != "blocked_destination" {
		t.Fatalf("screened wallet: %d %+v", code, ae)
	}

	// A payment pointer is normalized to its https URL.
	var p struct {
		Wallet string `json:"monetization_wallet"`
	}
	if code := alice.do("PATCH", "/api/pages/"+pageID, map[string]any{"monetization_wallet": " $Wallet.Example/alice "}, &p); code != 200 ||
		p.Wallet != "https://wallet.example/alice" {
		t.Fatalf("pointer: %d %q", code, p.Wallet)
	}
	_, body, _ := e.client().get("/alice")
	if link(body) != 1 || !strings.Contains(body, `<link rel="monetization" href="https://wallet.example/alice">`) {
		t.Fatalf("live page head:\n%s", body)
	}
	// The owner's preview doesn't carry it, so preview visits aren't paid.
	if _, body, _ := alice.get("/preview/" + pageID); link(body) != 0 {
		t.Fatal("preview carries the monetization link")
	}
	// Clearing removes it.
	if code := alice.do("PATCH", "/api/pages/"+pageID, map[string]any{"monetization_wallet": ""}, nil); code != 200 {
		t.Fatal("clear")
	}
	if _, body, _ := e.client().get("/alice"); link(body) != 0 {
		t.Fatal("link kept after clearing")
	}
}
