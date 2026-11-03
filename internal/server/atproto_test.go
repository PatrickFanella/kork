package server

import (
	"context"
	"testing"

	"git.subcult.tv/subculture-collective/zelda/internal/plans"
)

// A page's DID is served on its verified custom domain so the domain can be
// a Bluesky handle (#36).
func TestAtprotoDIDOnCustomDomain(t *testing.T) {
	e := setup(t)
	pool := testPool(t)
	alice := e.client()
	alice.signupVerified("alice@example.com")
	alice.setPlan(plans.Pro, nil)
	pageID := alice.newPage("alice")
	const did = "did:plc:abcdefghijklmnopqrstuvwx"
	if _, err := pool.Exec(context.Background(), `INSERT INTO domains (page_id, hostname, verify_token, verified_at)
		VALUES ($1, 'links.alice.example', 'tok', now()), ($1, 'pending.alice.example', 'tok2', NULL)`, pageID); err != nil {
		t.Fatal(err)
	}
	well := func(host string) (int, string, string) {
		code, body, h := e.client().get("/.well-known/atproto-did", "Host", host)
		return code, body, h.Get("Content-Type")
	}

	// Invalid DIDs are refused with a field error.
	for _, bad := range []string{"did:plc:short", "did:plc:ABCDEFGHIJKLMNOPQRSTUVWX", "did:key:z6Mk", "did:web:localhost", "https://bsky.app"} {
		var ae apiErr
		if code := alice.do("PATCH", "/api/pages/"+pageID, map[string]any{"atproto_did": bad}, &ae); code != 422 || ae.Error.Field != "atproto_did" {
			t.Errorf("%q: %d %+v", bad, code, ae)
		}
	}
	if code := alice.do("PATCH", "/api/pages/"+pageID, map[string]any{"atproto_did": " " + did + " "}, nil); code != 200 {
		t.Fatalf("set DID: %d", code)
	}

	// Unpublished: 404.
	if code, _, _ := well("links.alice.example"); code != 404 {
		t.Fatalf("unpublished: %d", code)
	}
	if code := alice.do("PATCH", "/api/pages/"+pageID, map[string]any{"published": true}, nil); code != 200 {
		t.Fatal("publish")
	}
	if code, body, ct := well("links.alice.example"); code != 200 || body != did || ct != "text/plain; charset=utf-8" {
		t.Fatalf("served: %d %q %q", code, body, ct)
	}
	// Unverified domains and the main host don't answer.
	if code, _, _ := well("pending.alice.example"); code != 404 {
		t.Fatalf("unverified domain: %d", code)
	}
	if code, _, _ := e.client().get("/.well-known/atproto-did"); code != 404 {
		t.Fatalf("main host: %d", code)
	}
	// A suspended page doesn't answer.
	if _, err := pool.Exec(context.Background(), `UPDATE pages SET suspended_at = now() WHERE id = $1`, pageID); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := well("links.alice.example"); code != 404 {
		t.Fatalf("suspended: %d", code)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE pages SET suspended_at = NULL WHERE id = $1`, pageID); err != nil {
		t.Fatal(err)
	}
	// Clearing the DID removes the answer; did:web is accepted.
	if code := alice.do("PATCH", "/api/pages/"+pageID, map[string]any{"atproto_did": ""}, nil); code != 200 {
		t.Fatal("clear")
	}
	if code, _, _ := well("links.alice.example"); code != 404 {
		t.Fatalf("cleared: %d", code)
	}
	if code := alice.do("PATCH", "/api/pages/"+pageID, map[string]any{"atproto_did": "did:web:alice.example"}, nil); code != 200 {
		t.Fatal("did:web")
	}
	// The DID is in the page API.
	var p struct {
		AtprotoDID string `json:"atproto_did"`
	}
	alice.do("GET", "/api/pages/"+pageID, nil, &p)
	if p.AtprotoDID != "did:web:alice.example" {
		t.Fatalf("page API: %q", p.AtprotoDID)
	}
}
