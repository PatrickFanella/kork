package billing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestStripeProUntil(t *testing.T) {
	end := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	buffered := end.Add(StripeBuffer)
	later := end.Add(30 * 24 * time.Hour)
	at := func(t time.Time) *time.Time { return &t }

	cases := []struct {
		status string
		end    time.Time
		prev   *time.Time
		want   *time.Time
	}{
		{"active", end, nil, &buffered},
		{"trialing", end, nil, &buffered},
		{"past_due", end, &buffered, &buffered},
		{"active", time.Time{}, at(end), at(end)}, // no period info keeps the grant
		{"canceled", end, &buffered, &end},        // ends at period end, no buffer
		{"canceled", later, &buffered, &buffered}, // never extends
		{"canceled", end, nil, nil},
		{"unpaid", end, &buffered, &end},
		{"incomplete_expired", end, nil, nil},
		{"paused", end, &buffered, &end},
		{"incomplete", end, nil, nil},
		{"something_new", end, &buffered, nil},
	}
	for _, c := range cases {
		got := StripeProUntil(c.status, c.end, c.prev)
		if (got == nil) != (c.want == nil) || (got != nil && !got.Equal(*c.want)) {
			t.Errorf("%s end=%v prev=%v: got %v want %v", c.status, c.end, c.prev, got, c.want)
		}
	}
}

func TestSubscriptionShapes(t *testing.T) {
	// Pre-2025-03-31 API: period end on the subscription.
	var old Subscription
	json.Unmarshal([]byte(`{"id":"sub_1","customer":"cus_1","status":"active","current_period_end":1793577600,
		"items":{"data":[{"price":{"id":"price_m"}}]}}`), &old)
	if old.PeriodEnd().Unix() != 1793577600 || old.Customer != "cus_1" || old.PriceID() != "price_m" {
		t.Fatalf("old shape: %+v", old)
	}
	// Newer API: period end on the items; expanded customer object.
	var cur Subscription
	json.Unmarshal([]byte(`{"id":"sub_1","customer":{"id":"cus_2","object":"customer"},"status":"active",
		"items":{"data":[{"current_period_end":1793577600,"price":{"id":"price_y"}}]}}`), &cur)
	if cur.PeriodEnd().Unix() != 1793577600 || cur.Customer != "cus_2" {
		t.Fatalf("new shape: %+v", cur)
	}
	var none Subscription
	if !none.PeriodEnd().IsZero() {
		t.Fatal("no period end should be zero")
	}

	var inv Invoice
	json.Unmarshal([]byte(`{"id":"in_1","customer":"cus_1","parent":{"subscription_details":{"subscription":"sub_9"}}}`), &inv)
	if inv.SubscriptionID() != "sub_9" {
		t.Fatalf("invoice parent: %q", inv.SubscriptionID())
	}
	json.Unmarshal([]byte(`{"id":"in_1","customer":"cus_1","subscription":"sub_8","parent":null}`), &inv)
	if inv.SubscriptionID() != "sub_8" {
		t.Fatalf("invoice subscription: %q", inv.SubscriptionID())
	}
}

func TestStripeClient(t *testing.T) {
	var got url.Values
	var auth, path string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, path = r.Header.Get("Authorization"), r.Method+" "+r.URL.Path
		r.ParseForm()
		got = r.PostForm
		switch r.URL.Path {
		case "/v1/checkout/sessions":
			w.Write([]byte(`{"id":"cs_1","url":"https://checkout.stripe.com/c/pay/cs_1"}`))
		case "/v1/billing_portal/sessions":
			w.Write([]byte(`{"id":"bps_1","url":"https://billing.stripe.com/p/session/1"}`))
		case "/v1/subscriptions/sub_gone":
			w.WriteHeader(404)
			w.Write([]byte(`{"error":{"code":"resource_missing","message":"No such subscription"}}`))
		default:
			w.WriteHeader(400)
			w.Write([]byte(`{"error":{"code":"parameter_invalid","message":"bad"}}`))
		}
	}))
	defer fake.Close()
	c := &Stripe{Key: "sk_test_x", BaseURL: fake.URL, HTTP: providerClient()}
	ctx := context.Background()

	u, err := c.CreateCheckout(ctx, CheckoutParams{UserID: "u1", Price: "price_m", CustomerID: "cus_1",
		SuccessURL: "https://kork.example/app/settings?billing=success", CancelURL: "https://kork.example/app/settings", AutomaticTax: true})
	if err != nil || u != "https://checkout.stripe.com/c/pay/cs_1" {
		t.Fatalf("checkout: %q %v", u, err)
	}
	if auth != "Bearer sk_test_x" || got.Get("mode") != "subscription" || got.Get("line_items[0][price]") != "price_m" ||
		got.Get("client_reference_id") != "u1" || got.Get("customer") != "cus_1" || got.Get("customer_email") != "" ||
		got.Get("subscription_data[metadata][user_id]") != "u1" || got.Get("automatic_tax[enabled]") != "true" ||
		got.Get("customer_update[address]") != "auto" {
		t.Fatalf("checkout form: %v", got)
	}
	if _, err := c.CreateCheckout(ctx, CheckoutParams{UserID: "u2", Price: "price_y", Email: "a@example.com"}); err != nil {
		t.Fatal(err)
	}
	if got.Get("customer") != "" || got.Get("customer_email") != "a@example.com" || got.Has("automatic_tax[enabled]") {
		t.Fatalf("new customer form: %v", got)
	}
	if u, err := c.CreatePortal(ctx, "cus_1", "https://kork.example/app/settings"); err != nil || u == "" || got.Get("customer") != "cus_1" {
		t.Fatalf("portal: %q %v %v", u, err, got)
	}
	if err := c.CancelSubscription(ctx, "sub_gone"); err != nil || path != "DELETE /v1/subscriptions/sub_gone" {
		t.Fatalf("cancel missing subscription: %v %s", err, path)
	}
	_, err = c.GetSubscription(ctx, "sub_bad")
	if ae, ok := err.(*APIError); !ok || ae.Status != 400 || ae.Code != "parameter_invalid" {
		t.Fatalf("api error: %v", err)
	}
}
