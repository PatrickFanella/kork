package server

import (
	"slices"
	"testing"
	"time"
)

// Stripe's created timestamp has one-second resolution. An older event from
// the same second as the last one applied used to be applied anyway, so an
// incomplete "created" event after an active "updated" one revoked Pro.
func TestStripeSameSecondEventsReadTheAPI(t *testing.T) {
	e := setup(t)
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	e.srv.SetClock((&testClock{now: now}).Now)
	fs := newFakeStripe(t)
	e.enableStripe(fs)
	bob := e.client()
	bobUser := bob.signupVerified("bob@example.com")
	end := now.Add(30 * 24 * time.Hour)

	active := subscription("sub_1", "cus_1", bobUser.ID, "active", end, "price_month")
	fs.setSub("sub_1", active)
	if code := e.webhook("evt_a", "customer.subscription.updated", now, active, now, testWebhookSecret); code != 200 {
		t.Fatalf("updated: %d", code)
	}
	gets := fs.gets
	incomplete := subscription("sub_1", "cus_1", bobUser.ID, "incomplete", end, "price_month")
	if code := e.webhook("evt_b", "customer.subscription.created", now, incomplete, now, testWebhookSecret); code != 200 {
		t.Fatalf("created: %d", code)
	}
	if fs.gets != gets+1 {
		t.Fatalf("a same-second event should read the subscription from Stripe (gets %d -> %d)", gets, fs.gets)
	}
	if m := bob.me(); m.Plan.Plan != "pro" {
		t.Fatalf("same-second incomplete event revoked Pro: %+v", m.Plan)
	}
	if b := bob.billing(); b.Stripe.Status != "active" {
		t.Fatalf("status: %q", b.Stripe.Status)
	}
	// A later event still applies without an API read.
	gets = fs.gets
	canceled := subscription("sub_1", "cus_1", bobUser.ID, "canceled", end, "price_month")
	if code := e.webhook("evt_c", "customer.subscription.deleted", now.Add(time.Second), canceled, now, testWebhookSecret); code != 200 || fs.gets != gets {
		t.Fatalf("later event: %d gets %d -> %d", code, gets, fs.gets)
	}
	if b := bob.billing(); b.Stripe.Status != "canceled" {
		t.Fatalf("status after cancel: %q", b.Stripe.Status)
	}
}

// Two checkouts paid from two tabs create two subscriptions. The second one
// is canceled so the customer isn't charged twice.
func TestStripeDuplicateSubscriptionIsCanceled(t *testing.T) {
	e := setup(t)
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	e.srv.SetClock((&testClock{now: now}).Now)
	fs := newFakeStripe(t)
	e.enableStripe(fs)
	bob := e.client()
	bobUser := bob.signupVerified("bob@example.com")
	end := now.Add(30 * 24 * time.Hour)
	send := func(id string, at time.Time, sub map[string]any) {
		t.Helper()
		if code := e.webhook(id, "customer.subscription.updated", at, sub, now, testWebhookSecret); code != 200 {
			t.Fatalf("%s: %d", id, code)
		}
	}

	first := subscription("sub_a", "cus_1", bobUser.ID, "active", end, "price_month")
	fs.setSub("sub_a", first)
	send("evt_1", now, first)
	second := subscription("sub_b", "cus_2", bobUser.ID, "active", end, "price_month")
	fs.setSub("sub_b", second)
	send("evt_2", now.Add(time.Minute), second)
	if !slices.Contains(fs.canceled, "sub_b") || slices.Contains(fs.canceled, "sub_a") {
		t.Fatalf("canceled %v, want only sub_b", fs.canceled)
	}
	if b := bob.billing(); b.Stripe.Status != "active" || !b.Stripe.Subscribed {
		t.Fatalf("billing after duplicate: %+v", b.Stripe)
	}
	// Stripe's follow-up event for the canceled duplicate changes nothing.
	send("evt_3", now.Add(2*time.Minute), subscription("sub_b", "cus_2", bobUser.ID, "canceled", end, "price_month"))
	if m := bob.me(); m.Plan.Plan != "pro" {
		t.Fatalf("plan after duplicate's cancel event: %+v", m.Plan)
	}

	// If the stored subscription has actually ended (an event was missed),
	// a new subscription is a resubscription, not a duplicate.
	fs.setSub("sub_a", subscription("sub_a", "cus_1", bobUser.ID, "canceled", end, "price_month"))
	third := subscription("sub_c", "cus_1", bobUser.ID, "active", end.Add(time.Hour), "price_year")
	fs.setSub("sub_c", third)
	send("evt_4", now.Add(3*time.Minute), third)
	if slices.Contains(fs.canceled, "sub_c") {
		t.Fatal("resubscription was canceled")
	}
	if b := bob.billing(); b.Stripe.Status != "active" || b.Stripe.Interval != "year" {
		t.Fatalf("billing after resubscribing: %+v", b.Stripe)
	}
}

// A background re-check reads the connection, then calls Patreon. If the
// user reconnects a different Patreon account meanwhile, the result for the
// old account must not be applied to the new connection.
func TestPatreonRecheckSkipsAReconnectedAccount(t *testing.T) {
	e := setup(t)
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	e.srv.SetClock((&testClock{now: now}).Now)
	fp := newFakePatreon(t)
	e.enablePatreon(fp)
	fp.set(func(f *fakePatreon) {
		f.codes["code_backer"], f.codes["code_follower"] = "pat_backer", "pat_follower"
		f.members["pat_backer"] = fakeMember{status: "active_patron", campaign: "camp_subcult", tiers: []string{"tier_backer"}}
		f.members["pat_follower"] = fakeMember{status: "active_patron", campaign: "camp_subcult", tiers: []string{"tier_follower"}}
	})
	bob := e.client()
	bobUser := bob.signupVerified("bob@example.com")
	connect := func(code string) {
		t.Helper()
		if status := bob.do("POST", "/api/billing/patreon/callback", map[string]string{"code": code, "state": bob.patreonState()}, nil); status != 200 {
			t.Fatalf("connect %s: %d", code, status)
		}
	}

	connect("code_backer")
	due, err := e.srv.store.PatreonDue(t.Context(), now.Add(time.Hour), 10)
	if err != nil || len(due) != 1 || due[0].UserID != bobUser.ID {
		t.Fatalf("due: %+v %v", due, err)
	}
	stale := due[0]
	if code := bob.do("DELETE", "/api/billing/patreon", nil, nil); code != 200 && code != 204 {
		t.Fatalf("disconnect: %d", code)
	}
	connect("code_follower")
	before, _ := e.srv.store.BillingOf(t.Context(), bobUser.ID)

	if err := e.srv.checkPatreon(t.Context(), stale); err != nil {
		t.Fatal(err)
	}
	after, _ := e.srv.store.BillingOf(t.Context(), bobUser.ID)
	if after.PatreonUserID != "pat_follower" || !slices.Equal(after.PatreonRefreshToken, before.PatreonRefreshToken) {
		t.Fatalf("stale check changed the new connection: user %q", after.PatreonUserID)
	}
	if m := bob.me(); m.Plan.Plan != "free" {
		t.Fatalf("the old account's membership was applied: %+v", m.Plan)
	}
}
