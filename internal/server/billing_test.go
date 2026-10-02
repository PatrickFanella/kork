package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/billing"
)

// ---- fake Stripe ----

type fakeStripe struct {
	mu        sync.Mutex
	ts        *httptest.Server
	subs      map[string]map[string]any // subscription objects by ID
	gets      int
	checkout  url.Values
	canceled  []string
	failCheck bool
}

func newFakeStripe(t *testing.T) *fakeStripe {
	f := &fakeStripe{subs: map[string]map[string]any{}}
	f.ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer sk_test_kork" {
			w.WriteHeader(401)
			return
		}
		r.ParseForm()
		switch {
		case r.Method == "POST" && r.URL.Path == "/v1/checkout/sessions":
			if f.failCheck {
				w.WriteHeader(500)
				return
			}
			f.checkout = r.PostForm
			w.Write([]byte(`{"id":"cs_test_1","url":"https://checkout.stripe.com/c/pay/cs_test_1"}`))
		case r.Method == "POST" && r.URL.Path == "/v1/billing_portal/sessions":
			fmt.Fprintf(w, `{"id":"bps_1","url":"https://billing.stripe.com/p/session/%s"}`, r.PostForm.Get("customer"))
		case strings.HasPrefix(r.URL.Path, "/v1/subscriptions/"):
			id := strings.TrimPrefix(r.URL.Path, "/v1/subscriptions/")
			sub, ok := f.subs[id]
			if !ok {
				w.WriteHeader(404)
				w.Write([]byte(`{"error":{"code":"resource_missing","message":"No such subscription"}}`))
				return
			}
			if r.Method == "DELETE" {
				f.canceled = append(f.canceled, id)
				sub["status"] = "canceled"
			} else {
				f.gets++
			}
			json.NewEncoder(w).Encode(sub)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(f.ts.Close)
	return f
}

func (f *fakeStripe) setSub(id string, sub map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subs[id] = sub
}

func subscription(id, customer, userID, status string, periodEnd time.Time, price string) map[string]any {
	// Current API shape: the period end lives on the item.
	return map[string]any{
		"id": id, "object": "subscription", "customer": customer, "status": status,
		"cancel_at_period_end": false, "metadata": map[string]string{"user_id": userID},
		"items": map[string]any{"data": []any{map[string]any{"current_period_end": periodEnd.Unix(), "price": map[string]string{"id": price}}}},
	}
}

const testWebhookSecret = "whsec_test_kork"

func (e *env) enableStripe(f *fakeStripe) {
	e.srv.cfg.StripeSecretKey = "sk_test_kork"
	e.srv.cfg.StripeWebhookSecret = testWebhookSecret
	e.srv.cfg.StripePriceMonthly = "price_month"
	e.srv.cfg.StripePriceAnnual = "price_year"
	e.srv.cfg.PriceDisplayMonthly = "$5"
	e.srv.cfg.PriceDisplayAnnual = "$48"
	e.srv.bill = newBillingClients(e.srv.cfg)
	e.srv.bill.stripe.BaseURL = f.ts.URL
}

// webhook posts a signed Stripe event and returns the status code.
func (e *env) webhook(id, typ string, created time.Time, obj any, signedAt time.Time, secret string) int {
	e.t.Helper()
	body, _ := json.Marshal(map[string]any{"id": id, "object": "event", "type": typ, "created": created.Unix(),
		"data": map[string]any{"object": obj}})
	req, _ := http.NewRequest("POST", e.ts.URL+"/stripe/webhook", bytes.NewReader(body))
	req.Header.Set("Stripe-Signature", billing.SignForTest(body, secret, signedAt))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
	return res.StatusCode
}

type billingResp struct {
	Stripe struct {
		Enabled          bool
		PriceMonthly     string `json:"price_monthly"`
		PriceAnnual      string `json:"price_annual"`
		Status, Interval string
		CurrentPeriodEnd *time.Time `json:"current_period_end"`
		PaymentFailed    bool       `json:"payment_failed"`
		Subscribed       bool
		CanManage        bool `json:"can_manage"`
	}
	Patreon struct {
		Enabled, Connected, Entitled bool
		PatronStatus                 string     `json:"patron_status"`
		GraceUntil                   *time.Time `json:"grace_until"`
		ProUntil                     *time.Time `json:"pro_until"`
		Reconnect                    bool
	}
}

func (c *client) billing() billingResp {
	c.e.t.Helper()
	var b billingResp
	if code := c.do("GET", "/api/billing", nil, &b); code != 200 {
		c.e.t.Fatalf("billing: %d", code)
	}
	return b
}

func TestBillingHiddenWhenUnconfigured(t *testing.T) {
	e := setup(t)
	bob := e.client()
	bob.signupVerified("bob@example.com")
	b := bob.billing()
	if b.Stripe.Enabled || b.Patreon.Enabled || b.Stripe.PriceMonthly != "" {
		t.Fatalf("billing should be off: %+v", b)
	}
	for _, r := range []struct{ method, path string }{
		{"POST", "/api/billing/checkout"}, {"POST", "/api/billing/portal"}, {"GET", "/api/billing/patreon/connect"},
		{"POST", "/api/billing/patreon/callback"}, {"DELETE", "/api/billing/patreon"},
	} {
		if code := bob.do(r.method, r.path, map[string]any{"interval": "month"}, nil); code != 404 {
			t.Errorf("%s %s: %d", r.method, r.path, code)
		}
	}
	now := time.Now()
	if code := e.webhook("evt_x", "customer.subscription.updated", now, map[string]any{"id": "sub_x"}, now, testWebhookSecret); code != 404 {
		t.Fatalf("webhook unconfigured: %d", code)
	}
	if code := e.client().do("GET", "/api/billing", nil, nil); code != 401 {
		t.Fatalf("anonymous billing: %d", code)
	}
}

func TestStripeBilling(t *testing.T) {
	e := setup(t)
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	e.srv.SetClock(clock.Now)
	fs := newFakeStripe(t)
	e.enableStripe(fs)
	day := 24 * time.Hour

	admin := e.client()
	admin.signupVerified("alice@example.com")
	bob := e.client()
	bobUser := bob.signupVerified("bob@example.com")

	b := bob.billing()
	if !b.Stripe.Enabled || b.Stripe.PriceMonthly != "$5" || b.Stripe.PriceAnnual != "$48" || b.Stripe.CanManage || b.Stripe.Subscribed {
		t.Fatalf("billing before subscribing: %+v", b.Stripe)
	}

	// ---- Checkout ----
	var ae apiErr
	if code := bob.do("POST", "/api/billing/checkout", map[string]any{"interval": "week"}, &ae); code != 422 || ae.Error.Field != "interval" {
		t.Fatalf("bad interval: %d %+v", code, ae)
	}
	if code := bob.do("POST", "/api/billing/portal", nil, &ae); code != 409 || ae.Error.Code != "no_billing_account" {
		t.Fatalf("portal without customer: %d %+v", code, ae)
	}
	var out struct{ URL string }
	if code := bob.do("POST", "/api/billing/checkout", map[string]any{"interval": "year"}, &out); code != 200 ||
		out.URL != "https://checkout.stripe.com/c/pay/cs_test_1" {
		t.Fatalf("checkout: %d %+v", code, out)
	}
	if f := fs.checkout; f.Get("mode") != "subscription" || f.Get("line_items[0][price]") != "price_year" ||
		f.Get("client_reference_id") != bobUser.ID || f.Get("customer_email") != "bob@example.com" ||
		f.Get("success_url") != e.ts.URL+"/app/settings?billing=success#plan" || f.Has("automatic_tax[enabled]") {
		t.Fatalf("checkout form: %v", f)
	}
	fs.failCheck = true
	if code := bob.do("POST", "/api/billing/checkout", map[string]any{"interval": "month"}, &ae); code != 502 || ae.Error.Code != "billing_unavailable" {
		t.Fatalf("stripe down: %d %+v", code, ae)
	}
	fs.failCheck = false

	// ---- Webhook signature ----
	periodEnd := now.Add(30 * day)
	fs.setSub("sub_1", subscription("sub_1", "cus_1", bobUser.ID, "active", periodEnd, "price_year"))
	session := map[string]any{"id": "cs_test_1", "object": "checkout.session", "mode": "subscription",
		"client_reference_id": bobUser.ID, "customer": "cus_1", "subscription": "sub_1"}
	if code := e.webhook("evt_1", "checkout.session.completed", now, session, now, "whsec_wrong"); code != 400 {
		t.Fatalf("bad signature: %d", code)
	}
	if code := e.webhook("evt_1", "checkout.session.completed", now, session, now.Add(-10*time.Minute), testWebhookSecret); code != 400 {
		t.Fatalf("old signature: %d", code)
	}
	req, _ := http.NewRequest("POST", e.ts.URL+"/stripe/webhook", strings.NewReader(`{}`))
	if res, _ := http.DefaultClient.Do(req); res.StatusCode != 400 {
		t.Fatalf("unsigned webhook: %d", res.StatusCode)
	}
	if m := bob.me(); m.Plan.Plan != "free" {
		t.Fatal("rejected events must not change the plan")
	}

	// ---- checkout.session.completed ----
	if code := e.webhook("evt_1", "checkout.session.completed", now, session, now, testWebhookSecret); code != 200 {
		t.Fatalf("checkout completed: %d", code)
	}
	m := bob.me()
	if m.Plan.Plan != "pro" || m.Plan.Source != "stripe" || m.Plan.ExpiresAt == nil || !m.Plan.ExpiresAt.Equal(periodEnd.Add(billing.StripeBuffer)) {
		t.Fatalf("pro after checkout: %+v", m.Plan)
	}
	b = bob.billing()
	if b.Stripe.Status != "active" || b.Stripe.Interval != "year" || !b.Stripe.Subscribed || !b.Stripe.CanManage ||
		b.Stripe.CurrentPeriodEnd == nil || !b.Stripe.CurrentPeriodEnd.Equal(periodEnd) {
		t.Fatalf("billing after checkout: %+v", b.Stripe)
	}
	// Duplicate delivery: no second fetch, no change.
	gets := fs.gets
	if code := e.webhook("evt_1", "checkout.session.completed", now, session, now, testWebhookSecret); code != 200 || fs.gets != gets {
		t.Fatalf("duplicate: %d gets %d->%d", code, gets, fs.gets)
	}
	if code := bob.do("POST", "/api/billing/checkout", map[string]any{"interval": "month"}, &ae); code != 409 || ae.Error.Code != "already_subscribed" {
		t.Fatalf("second checkout: %d %+v", code, ae)
	}
	if code := bob.do("POST", "/api/billing/portal", nil, &out); code != 200 || out.URL != "https://billing.stripe.com/p/session/cus_1" {
		t.Fatalf("portal: %d %+v", code, out)
	}

	// ---- Renewal fails: past_due keeps Pro until the period ends ----
	clock.Set(periodEnd.Add(time.Hour))
	invoice := map[string]any{"id": "in_1", "object": "invoice", "customer": "cus_1",
		"parent": map[string]any{"subscription_details": map[string]any{"subscription": "sub_1"}}}
	if code := e.webhook("evt_2", "invoice.payment_failed", clock.Now(), invoice, clock.Now(), testWebhookSecret); code != 200 {
		t.Fatalf("payment failed: %d", code)
	}
	pastDue := subscription("sub_1", "cus_1", bobUser.ID, "past_due", periodEnd, "price_year")
	if code := e.webhook("evt_3", "customer.subscription.updated", clock.Now(), pastDue, clock.Now(), testWebhookSecret); code != 200 {
		t.Fatalf("past_due: %d", code)
	}
	if m := bob.me(); m.Plan.Plan != "pro" {
		t.Fatalf("past_due within buffer should be pro: %+v", m.Plan)
	}
	if b := bob.billing(); b.Stripe.Status != "past_due" || !b.Stripe.PaymentFailed {
		t.Fatalf("past_due billing: %+v", b.Stripe)
	}

	// Payment retried and succeeded: renewed period, notice cleared.
	newEnd := periodEnd.Add(365 * day)
	renewed := subscription("sub_1", "cus_1", bobUser.ID, "active", newEnd, "price_year")
	if code := e.webhook("evt_4", "customer.subscription.updated", clock.Now().Add(time.Minute), renewed, clock.Now(), testWebhookSecret); code != 200 {
		t.Fatalf("renewed: %d", code)
	}
	// A late, older event must not roll the state back.
	stale := subscription("sub_1", "cus_1", bobUser.ID, "canceled", periodEnd, "price_year")
	if code := e.webhook("evt_5", "customer.subscription.updated", clock.Now().Add(-time.Hour), stale, clock.Now(), testWebhookSecret); code != 200 {
		t.Fatalf("stale: %d", code)
	}
	m = bob.me()
	if m.Plan.Plan != "pro" || !m.Plan.ExpiresAt.Equal(newEnd.Add(billing.StripeBuffer)) {
		t.Fatalf("after renewal: %+v", m.Plan)
	}
	if b := bob.billing(); b.Stripe.Status != "active" || b.Stripe.PaymentFailed {
		t.Fatalf("renewed billing: %+v", b.Stripe)
	}

	// ---- Precedence with a manual grant ----
	manualEnd := newEnd.Add(100 * day)
	if code := admin.do("POST", "/api/admin/users/"+bobUser.ID+"/plan", map[string]any{"plan": "pro", "expires_at": manualEnd.Format(time.RFC3339)}, nil); code != 200 {
		t.Fatalf("manual grant: %d", code)
	}
	if m := bob.me(); m.Plan.Source != "manual" || !m.Plan.ExpiresAt.Equal(manualEnd) {
		t.Fatalf("longer manual grant should win: %+v", m.Plan)
	}
	// Canceling the subscription doesn't clobber the manual grant.
	canceledAt := clock.Now().Add(2 * time.Hour)
	canceled := subscription("sub_1", "cus_1", bobUser.ID, "canceled", newEnd, "price_year")
	if code := e.webhook("evt_6", "customer.subscription.deleted", canceledAt, canceled, clock.Now(), testWebhookSecret); code != 200 {
		t.Fatalf("deleted: %d", code)
	}
	if m := bob.me(); m.Plan.Plan != "pro" || m.Plan.Source != "manual" || !m.Plan.ExpiresAt.Equal(manualEnd) {
		t.Fatalf("manual after cancel: %+v", m.Plan)
	}
	// Revoking the manual grant leaves Stripe's: Pro until the paid period ends.
	if code := admin.do("POST", "/api/admin/users/"+bobUser.ID+"/plan", map[string]any{"plan": "free"}, nil); code != 200 {
		t.Fatalf("manual revoke: %d", code)
	}
	m = bob.me()
	if m.Plan.Plan != "pro" || m.Plan.Source != "stripe" || !m.Plan.ExpiresAt.Equal(newEnd) {
		t.Fatalf("stripe grant after manual revoke: %+v", m.Plan)
	}
	clock.Set(newEnd.Add(time.Second))
	if m := bob.me(); m.Plan.Plan != "free" || m.Plan.StoredPlan != "pro" || m.Plan.DomainsGraceUntil == nil {
		t.Fatalf("after period end: %+v", m.Plan)
	}
	if b := bob.billing(); b.Stripe.Subscribed || !b.Stripe.CanManage {
		t.Fatalf("canceled billing: %+v", b.Stripe)
	}

	// ---- Events for other accounts ----
	if code := e.webhook("evt_7", "customer.subscription.updated", clock.Now(),
		subscription("sub_zz", "cus_zz", "", "active", newEnd, "price_month"), clock.Now(), testWebhookSecret); code != 200 {
		t.Fatalf("unknown customer: %d", code)
	}
	if code := e.webhook("evt_8", "charge.refunded", clock.Now(), map[string]any{"id": "ch_1"}, clock.Now(), testWebhookSecret); code != 200 {
		t.Fatalf("unused type: %d", code)
	}

	// ---- Subscribe again via subscription.created, then delete the account ----
	carol := e.client()
	carolUser := carol.signupVerified("carol@example.com")
	fs.setSub("sub_c", subscription("sub_c", "cus_c", carolUser.ID, "active", clock.Now().Add(30*day), "price_month"))
	if code := e.webhook("evt_9", "customer.subscription.created", clock.Now(),
		subscription("sub_c", "cus_c", carolUser.ID, "active", clock.Now().Add(30*day), "price_month"), clock.Now(), testWebhookSecret); code != 200 {
		t.Fatalf("created: %d", code)
	}
	if m := carol.me(); m.Plan.Plan != "pro" || m.Plan.Source != "stripe" {
		t.Fatalf("carol pro: %+v", m.Plan)
	}
	if b := carol.billing(); b.Stripe.Interval != "month" {
		t.Fatalf("carol interval: %+v", b.Stripe)
	}
	if code := carol.do("DELETE", "/api/account", map[string]string{"password": "correct horse battery"}, nil); code != 204 {
		t.Fatalf("delete account: %d", code)
	}
	if len(fs.canceled) != 1 || fs.canceled[0] != "sub_c" {
		t.Fatalf("subscription not canceled on deletion: %v", fs.canceled)
	}
}

// ---- fake Patreon ----

type fakePatreon struct {
	mu sync.Mutex
	ts *httptest.Server
	// codes maps an authorization code to a Patreon user ID.
	codes map[string]string
	// refresh maps a live refresh token to a Patreon user ID; refreshing
	// rotates it.
	refresh map[string]string
	// access maps an access token to a Patreon user ID.
	access map[string]string
	// members holds each Patreon user's membership: status and tier IDs.
	members map[string]fakeMember
	n       int
	down    bool
}

type fakeMember struct {
	status   string
	campaign string
	tiers    []string
}

func newFakePatreon(t *testing.T) *fakePatreon {
	f := &fakePatreon{codes: map[string]string{}, refresh: map[string]string{}, access: map[string]string{}, members: map[string]fakeMember{}}
	f.ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.down {
			w.WriteHeader(503)
			return
		}
		switch r.URL.Path {
		case "/api/oauth2/token":
			r.ParseForm()
			if r.PostForm.Get("client_id") != "pat_client" || r.PostForm.Get("client_secret") != "pat_secret" {
				w.WriteHeader(401)
				return
			}
			var user string
			var ok bool
			switch r.PostForm.Get("grant_type") {
			case "authorization_code":
				user, ok = f.codes[r.PostForm.Get("code")]
				delete(f.codes, r.PostForm.Get("code"))
			case "refresh_token":
				user, ok = f.refresh[r.PostForm.Get("refresh_token")]
				delete(f.refresh, r.PostForm.Get("refresh_token"))
			}
			if !ok {
				w.WriteHeader(400)
				w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			f.n++
			at, rt := fmt.Sprintf("at_%d", f.n), fmt.Sprintf("rt_%d", f.n)
			f.access[at], f.refresh[rt] = user, user
			fmt.Fprintf(w, `{"access_token":%q,"refresh_token":%q,"expires_in":2678400,"scope":"identity","token_type":"Bearer"}`, at, rt)
		case "/api/oauth2/v2/identity":
			user, ok := f.access[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
			if !ok {
				w.WriteHeader(401)
				return
			}
			doc := map[string]any{"data": map[string]any{"id": user, "type": "user"}}
			if m, ok := f.members[user]; ok {
				var tiers []any
				for _, t := range m.tiers {
					tiers = append(tiers, map[string]string{"id": t, "type": "tier"})
				}
				var status any
				if m.status != "" {
					status = m.status
				}
				doc["included"] = []any{map[string]any{"id": "mem_" + user, "type": "member",
					"attributes": map[string]any{"patron_status": status, "currently_entitled_amount_cents": 1000},
					"relationships": map[string]any{
						"campaign":                 map[string]any{"data": map[string]string{"id": m.campaign, "type": "campaign"}},
						"currently_entitled_tiers": map[string]any{"data": tiers},
					}}}
			}
			json.NewEncoder(w).Encode(doc)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(f.ts.Close)
	return f
}

func (f *fakePatreon) set(fn func(f *fakePatreon)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (e *env) enablePatreon(f *fakePatreon) {
	e.srv.cfg.PatreonClientID = "pat_client"
	e.srv.cfg.PatreonClientSecret = "pat_secret"
	e.srv.cfg.PatreonCampaignID = "camp_subcult"
	e.srv.cfg.PatreonTierIDs = []string{"tier_backer"}
	e.srv.cfg.SecretKey = bytes.Repeat([]byte{7}, 32)
	e.srv.bill = newBillingClients(e.srv.cfg)
	e.srv.bill.patreon.BaseURL = f.ts.URL
}

// connectPatreon follows the connect redirect and returns the state.
func (c *client) patreonState() string {
	c.e.t.Helper()
	code, _, h := c.get("/api/billing/patreon/connect")
	if code != 302 {
		c.e.t.Fatalf("connect: %d", code)
	}
	u, err := url.Parse(h.Get("Location"))
	if err != nil || u.Path != "/oauth2/authorize" || u.Query().Get("scope") != "identity" ||
		u.Query().Get("redirect_uri") != c.e.ts.URL+"/app/patreon/callback" || u.Query().Get("client_id") != "pat_client" {
		c.e.t.Fatalf("authorize redirect: %s", h.Get("Location"))
	}
	return u.Query().Get("state")
}

func TestPatreonBilling(t *testing.T) {
	e := setup(t)
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	e.srv.SetClock(clock.Now)
	fp := newFakePatreon(t)
	e.enablePatreon(fp)
	day := 24 * time.Hour
	backer := fakeMember{status: "active_patron", campaign: "camp_subcult", tiers: []string{"tier_backer"}}
	fp.set(func(f *fakePatreon) {
		f.codes["code_bob"], f.codes["code_bob2"], f.codes["code_carol"] = "pat_bob", "pat_bob", "pat_carol"
		f.members["pat_bob"] = backer
		f.members["pat_carol"] = fakeMember{status: "active_patron", campaign: "camp_subcult", tiers: []string{"tier_follower"}}
	})

	bob := e.client()
	bobUser := bob.signupVerified("bob@example.com")
	mallory := e.client()
	mallory.signupVerified("mallory@example.com")
	carol := e.client()
	carol.signupVerified("carol@example.com")

	if b := bob.billing(); !b.Patreon.Enabled || b.Patreon.Connected || b.Stripe.Enabled {
		t.Fatalf("before connect: %+v", b)
	}

	// ---- State is bound to the session and expires ----
	state := bob.patreonState()
	var ae apiErr
	if code := mallory.do("POST", "/api/billing/patreon/callback", map[string]string{"code": "code_bob", "state": state}, &ae); code != 400 || ae.Error.Code != "bad_state" {
		t.Fatalf("state from another session: %d %+v", code, ae)
	}
	if code := bob.do("POST", "/api/billing/patreon/callback", map[string]string{"code": "code_bob", "state": state + "x"}, &ae); code != 400 {
		t.Fatalf("tampered state: %d", code)
	}
	clock.Set(now.Add(patreonStateTTL + time.Minute))
	if code := bob.do("POST", "/api/billing/patreon/callback", map[string]string{"code": "code_bob", "state": state}, &ae); code != 400 || ae.Error.Code != "bad_state" {
		t.Fatalf("expired state: %d", code)
	}
	clock.Set(now)
	if code := bob.do("POST", "/api/billing/patreon/callback", map[string]string{"code": "nope", "state": state}, &ae); code != 400 || ae.Error.Code != "patreon_denied" {
		t.Fatalf("bad code: %d %+v", code, ae)
	}

	// ---- Connect as an active Backer ----
	var b billingResp
	if code := bob.do("POST", "/api/billing/patreon/callback", map[string]string{"code": "code_bob", "state": state}, &b); code != 200 ||
		!b.Patreon.Connected || !b.Patreon.Entitled || b.Patreon.PatronStatus != "active_patron" {
		t.Fatalf("callback: %d %+v", code, b.Patreon)
	}
	m := bob.me()
	if m.Plan.Plan != "pro" || m.Plan.Source != "patreon" || !m.Plan.ExpiresAt.Equal(now.Add(billing.PatreonGrace+billing.PatreonCheckEvery)) {
		t.Fatalf("pro via patreon: %+v", m.Plan)
	}
	// The refresh token is stored sealed, bound to the user.
	bill, err := e.srv.store.BillingOf(t.Context(), bobUser.ID)
	if err != nil || bytes.Contains(bill.PatreonRefreshToken, []byte("rt_")) || bill.PatreonUserID != "pat_bob" {
		t.Fatalf("stored patreon state: %+v %v", bill, err)
	}
	if plain, err := e.srv.bill.sealer.Open(bill.PatreonRefreshToken, []byte(bobUser.ID)); err != nil || !strings.HasPrefix(string(plain), "rt_") {
		t.Fatalf("sealed token: %q %v", plain, err)
	}

	// One Kork account per Patreon user.
	fp.set(func(f *fakePatreon) { f.codes["code_bob_again"] = "pat_bob" })
	if code := mallory.do("POST", "/api/billing/patreon/callback", map[string]string{"code": "code_bob_again", "state": mallory.patreonState()}, &ae); code != 409 || ae.Error.Code != "patreon_in_use" {
		t.Fatalf("second account: %d %+v", code, ae)
	}

	// A patron of a non-qualifying tier connects but gets no Pro.
	if code := carol.do("POST", "/api/billing/patreon/callback", map[string]string{"code": "code_carol", "state": carol.patreonState()}, &b); code != 200 ||
		!b.Patreon.Connected || b.Patreon.Entitled {
		t.Fatalf("carol: %d %+v", code, b.Patreon)
	}
	if m := carol.me(); m.Plan.Plan != "free" {
		t.Fatalf("carol should be free: %+v", m.Plan)
	}

	// ---- Daily re-check ----
	// Not due yet: nothing happens.
	if checked, failed := e.srv.CheckPatreonOnce(t.Context()); checked != 0 || failed != 0 {
		t.Fatalf("early check: %d %d", checked, failed)
	}
	clock.Set(now.Add(day))
	if checked, failed := e.srv.CheckPatreonOnce(t.Context()); checked != 2 || failed != 0 {
		t.Fatalf("daily check: %d %d", checked, failed)
	}
	if m := bob.me(); !m.Plan.ExpiresAt.Equal(now.Add(day + billing.PatreonGrace + billing.PatreonCheckEvery)) {
		t.Fatalf("renewed by check: %+v", m.Plan)
	}

	// Patreon down: state unchanged, retried later.
	fp.set(func(f *fakePatreon) { f.down = true })
	clock.Set(now.Add(2 * day))
	if checked, failed := e.srv.CheckPatreonOnce(t.Context()); checked != 0 || failed != 2 {
		t.Fatalf("patreon down: %d %d", checked, failed)
	}
	fp.set(func(f *fakePatreon) { f.down = false })

	// Payment declined: Pro stays for the grace period.
	fp.set(func(f *fakePatreon) {
		f.members["pat_bob"] = fakeMember{status: "declined_patron", campaign: "camp_subcult", tiers: []string{"tier_backer"}}
	})
	lostAt := now.Add(2*day + time.Hour)
	clock.Set(lostAt)
	e.srv.CheckPatreonOnce(t.Context())
	b = bob.billing()
	if b.Patreon.Entitled || b.Patreon.GraceUntil == nil || !b.Patreon.GraceUntil.Equal(lostAt.Add(billing.PatreonGrace)) || b.Patreon.PatronStatus != "declined_patron" {
		t.Fatalf("grace: %+v", b.Patreon)
	}
	if m := bob.me(); m.Plan.Plan != "pro" || !m.Plan.ExpiresAt.Equal(lostAt.Add(billing.PatreonGrace)) {
		t.Fatalf("pro during grace: %+v", m.Plan)
	}
	// Still declined a few days later: the grace end doesn't move.
	clock.Set(lostAt.Add(4 * day))
	e.srv.CheckPatreonOnce(t.Context())
	if m := bob.me(); m.Plan.Plan != "pro" || !m.Plan.ExpiresAt.Equal(lostAt.Add(billing.PatreonGrace)) {
		t.Fatalf("grace end moved: %+v", m.Plan)
	}
	// Grace over: the Patreon grant is dropped.
	clock.Set(lostAt.Add(billing.PatreonGrace + time.Hour))
	e.srv.CheckPatreonOnce(t.Context())
	if m := bob.me(); m.Plan.Plan != "free" || m.Plan.StoredPlan != "free" {
		t.Fatalf("after grace: %+v", m.Plan)
	}
	if b := bob.billing(); !b.Patreon.Connected || b.Patreon.ProUntil != nil {
		t.Fatalf("after grace billing: %+v", b.Patreon)
	}

	// Pledging again: the next check restores Pro.
	fp.set(func(f *fakePatreon) { f.members["pat_bob"] = backer })
	clock.Set(clock.Now().Add(day))
	e.srv.CheckPatreonOnce(t.Context())
	if m := bob.me(); m.Plan.Plan != "pro" || m.Plan.Source != "patreon" {
		t.Fatalf("restored: %+v", m.Plan)
	}

	// The patron revokes the app on Patreon: grace, and the editor asks to reconnect.
	fp.set(func(f *fakePatreon) { f.refresh = map[string]string{} })
	revokedAt := clock.Now().Add(day)
	clock.Set(revokedAt)
	e.srv.CheckPatreonOnce(t.Context())
	b = bob.billing()
	if !b.Patreon.Reconnect || b.Patreon.GraceUntil == nil || !b.Patreon.GraceUntil.Equal(revokedAt.Add(billing.PatreonGrace)) {
		t.Fatalf("revoked: %+v", b.Patreon)
	}
	// Reconnecting the same Patreon account restores Pro at once.
	if code := bob.do("POST", "/api/billing/patreon/callback", map[string]string{"code": "code_bob2", "state": bob.patreonState()}, &b); code != 200 ||
		!b.Patreon.Entitled || b.Patreon.Reconnect || b.Patreon.GraceUntil != nil {
		t.Fatalf("reconnect: %d %+v", code, b.Patreon)
	}

	// ---- Disconnect ----
	if code := bob.do("DELETE", "/api/billing/patreon", nil, &b); code != 200 || b.Patreon.Connected {
		t.Fatalf("disconnect: %d %+v", code, b.Patreon)
	}
	if m := bob.me(); m.Plan.Plan != "free" {
		t.Fatalf("free after disconnect: %+v", m.Plan)
	}
	// The Patreon account is free to connect elsewhere now.
	fp.set(func(f *fakePatreon) { f.codes["code_m"] = "pat_bob" })
	if code := mallory.do("POST", "/api/billing/patreon/callback", map[string]string{"code": "code_m", "state": mallory.patreonState()}, &b); code != 200 || !b.Patreon.Entitled {
		t.Fatalf("connect elsewhere: %d %+v", code, b.Patreon)
	}
}
