package server

import (
	"bytes"
	"context"
	"testing"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/brevo"
)

// brevoBlock creates a live page with a Brevo sign-up block and a clock the
// test controls.
func brevoBlock(t *testing.T) (*env, *client, string, string, *fakeBrevo, *testClock) {
	t.Helper()
	e := setup(t)
	fake := &fakeBrevo{}
	ts := fake.server(t)
	e.srv.SetBrevo(&brevo.Client{BaseURL: ts.URL, HTTP: ts.Client()})
	if err := e.srv.SetSecretKey(bytes.Repeat([]byte{9}, 32)); err != nil {
		t.Fatal(err)
	}
	clock := &testClock{now: time.Now()}
	e.srv.SetClock(clock.Now)
	alice, pageID := livePage(t, e, "alice@example.com", "alice")
	code, blk, ae := alice.createBlock(pageID, "email_signup", map[string]any{"title": "News", "destination": "brevo",
		"brevo_list_id": "7", "brevo_api_key": "xkeysib-good"})
	if code != 201 {
		t.Fatalf("create: %d %+v", code, ae)
	}
	return e, alice, pageID, blk.ID, fake, clock
}

// #59: confirming marks a Brevo sign-up as waiting in the same statement,
// so it is delivered even if the request ends before the first attempt.
func TestConfirmedSignupIsQueuedBeforeDelivery(t *testing.T) {
	e, _, _, blockID, fake, _ := brevoBlock(t)
	ctx := context.Background()
	tok, err := e.srv.store.RequestSignup(ctx, blockID, "fan@example.org", nil, e.srv.now())
	if err != nil {
		t.Fatal(err)
	}
	// Confirm without running the handler's delivery, as if the visitor
	// closed the tab and the request was cancelled.
	c, err := e.srv.store.ConfirmSignup(ctx, tok, e.srv.now())
	if err != nil || !c.PendingPush {
		t.Fatalf("confirm: %+v %v", c, err)
	}
	if err := e.srv.SignupJobsOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if got := fake.list(); len(got) != 1 || got[0] != "fan@example.org" {
		t.Fatalf("brevo received %v", got)
	}
}

// #58: an address is only dropped when Brevo itself keeps refusing it. A
// problem on Kork's side, such as a key that can't be unsealed, keeps it
// and tells the owner.
func TestBrevoDeliveryDropsOnlyOnBrevoRefusal(t *testing.T) {
	e, alice, pageID, blockID, fake, clock := brevoBlock(t)
	ctx := context.Background()
	week := brevoPushGiveUp + time.Hour

	// The server's secret key changes: the stored Brevo key can't be read.
	if err := e.srv.SetSecretKey(bytes.Repeat([]byte{8}, 32)); err != nil {
		t.Fatal(err)
	}
	e.subscribeAndConfirm(blockID, "kept@example.org")
	clock.Set(clock.Now().Add(week))
	for range 3 {
		clock.Set(clock.Now().Add(brevoRetryMax))
		if err := e.srv.SignupJobsOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	got := alice.contacts(pageID, blockID)
	if got.AwaitingPush != 1 || got.PushProblem != "The saved Brevo API key can't be read on this server. Enter the API key again to resume sending." {
		t.Fatalf("local failure dropped or hidden the address: %+v", got)
	}

	// With the right key back, it is delivered.
	if err := e.srv.SetSecretKey(bytes.Repeat([]byte{9}, 32)); err != nil {
		t.Fatal(err)
	}
	clock.Set(clock.Now().Add(brevoRetryMax))
	if err := e.srv.SignupJobsOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if got := fake.list(); len(got) != 1 || got[0] != "kept@example.org" {
		t.Fatalf("after restoring the key: %v", got)
	}

	// Brevo refusing the address for over a week drops it.
	fake.setReject(true)
	e.subscribeAndConfirm(blockID, "refused@example.org")
	if got := alice.contacts(pageID, blockID); got.AwaitingPush != 1 || got.PushProblem != "Brevo refused the address." {
		t.Fatalf("refused: %+v", got)
	}
	clock.Set(clock.Now().Add(week))
	if err := e.srv.SignupJobsOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if got := alice.contacts(pageID, blockID); got.AwaitingPush != 0 {
		t.Fatalf("refused address kept past the give-up time: %+v", got)
	}
}

// #60: a failing address waits for its next attempt, so it can't hold back
// newer ones.
func TestFailingPushesDontStarveNewerOnes(t *testing.T) {
	e, _, _, blockID, fake, clock := brevoBlock(t)
	ctx := context.Background()
	fake.setFail(true)
	e.subscribeAndConfirm(blockID, "old@example.org")
	fake.setFail(false)
	tok, err := e.srv.store.RequestSignup(ctx, blockID, "new@example.org", nil, e.srv.now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.srv.store.ConfirmSignup(ctx, tok, e.srv.now()); err != nil {
		t.Fatal(err)
	}
	due, err := e.srv.store.PendingPushes(ctx, clock.Now(), 1)
	if err != nil || len(due) != 1 || due[0].Email != "new@example.org" {
		t.Fatalf("with one slot, the never-tried address should come first: %+v %v", due, err)
	}
	if d := brevoRetryAfter(0); d != brevoRetryFirst {
		t.Fatalf("first retry after %v", d)
	}
	if d := brevoRetryAfter(20); d != brevoRetryMax {
		t.Fatalf("retry spacing not capped: %v", d)
	}
}
