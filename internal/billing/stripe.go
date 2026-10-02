package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// StripeAPI is Stripe's API origin.
const StripeAPI = "https://api.stripe.com"

// StripeBuffer extends a paid period so a renewal that is charged a little
// late (Stripe retries, webhook delays) doesn't drop Pro for a few hours.
const StripeBuffer = 24 * time.Hour

// maxResponse caps how much of a provider's response is read.
const maxResponse = 1 << 20

// Stripe calls the Stripe API with a secret key. Requests are form-encoded
// per Stripe's API; there is no SDK dependency.
type Stripe struct {
	Key     string
	BaseURL string // StripeAPI unless a test replaces it
	HTTP    *http.Client
}

// NewStripe builds a client for the live API.
func NewStripe(key string) *Stripe {
	return &Stripe{Key: key, BaseURL: StripeAPI, HTTP: providerClient()}
}

// providerClient is an HTTP client for fixed provider APIs. These hosts come
// from code and operator config, never from users, so the SSRF-checking
// internal/fetch client (GET-only, public addresses only) isn't needed; the
// client still bounds time and refuses redirects.
func providerClient() *http.Client {
	return &http.Client{
		Timeout:       15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// APIError is a non-2xx response from a provider.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("provider API %d %s: %s", e.Status, e.Code, e.Message)
}

func (c *Stripe) call(ctx context.Context, method, path string, form url.Values, out any) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxResponse))
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		var e struct {
			Error struct{ Code, Message, Type string }
		}
		_ = json.Unmarshal(data, &e)
		return &APIError{Status: res.StatusCode, Code: e.Error.Code, Message: e.Error.Message}
	}
	return json.Unmarshal(data, out)
}

// CheckoutParams describes a subscription Checkout Session.
type CheckoutParams struct {
	UserID     string
	Price      string
	CustomerID string // reuse an existing customer when set
	Email      string // prefilled when there is no customer yet
	SuccessURL string
	CancelURL  string
	// AutomaticTax turns on Stripe Tax; it needs tax set up in the dashboard.
	AutomaticTax bool
}

// CreateCheckout creates a Checkout Session in subscription mode and returns
// its hosted URL. The user ID is stored as client_reference_id and on the
// subscription's metadata so every later event can be matched to the account.
func (c *Stripe) CreateCheckout(ctx context.Context, p CheckoutParams) (string, error) {
	f := url.Values{}
	f.Set("mode", "subscription")
	f.Set("line_items[0][price]", p.Price)
	f.Set("line_items[0][quantity]", "1")
	f.Set("client_reference_id", p.UserID)
	f.Set("metadata[user_id]", p.UserID)
	f.Set("subscription_data[metadata][user_id]", p.UserID)
	f.Set("success_url", p.SuccessURL)
	f.Set("cancel_url", p.CancelURL)
	if p.CustomerID != "" {
		f.Set("customer", p.CustomerID)
		if p.AutomaticTax {
			// Tax needs the address Checkout collects on the customer.
			f.Set("customer_update[address]", "auto")
		}
	} else if p.Email != "" {
		f.Set("customer_email", p.Email)
	}
	if p.AutomaticTax {
		f.Set("automatic_tax[enabled]", "true")
	}
	var out struct{ URL string }
	if err := c.call(ctx, http.MethodPost, "/v1/checkout/sessions", f, &out); err != nil {
		return "", err
	}
	if out.URL == "" {
		return "", errors.New("stripe: checkout session has no url")
	}
	return out.URL, nil
}

// CreatePortal creates a Customer Portal session and returns its URL.
func (c *Stripe) CreatePortal(ctx context.Context, customerID, returnURL string) (string, error) {
	f := url.Values{}
	f.Set("customer", customerID)
	f.Set("return_url", returnURL)
	var out struct{ URL string }
	if err := c.call(ctx, http.MethodPost, "/v1/billing_portal/sessions", f, &out); err != nil {
		return "", err
	}
	if out.URL == "" {
		return "", errors.New("stripe: portal session has no url")
	}
	return out.URL, nil
}

// GetSubscription fetches a subscription's current state.
func (c *Stripe) GetSubscription(ctx context.Context, id string) (*Subscription, error) {
	var s Subscription
	if err := c.call(ctx, http.MethodGet, "/v1/subscriptions/"+url.PathEscape(id), nil, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// CancelSubscription cancels a subscription immediately (account deletion).
func (c *Stripe) CancelSubscription(ctx context.Context, id string) error {
	var s Subscription
	err := c.call(ctx, http.MethodDelete, "/v1/subscriptions/"+url.PathEscape(id), nil, &s)
	var ae *APIError
	if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
		return nil // already gone
	}
	return err
}

// ---- webhook objects ----

// Event is a Stripe webhook event envelope.
type Event struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Created int64  `json:"created"`
	Data    struct {
		Object json.RawMessage `json:"object"`
	} `json:"data"`
}

// ID is an object reference that Stripe sends either as an ID string or,
// when expanded, as an object with an id.
type ID string

func (i *ID) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("null")) {
		*i = ""
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*i = ID(s)
		return nil
	}
	var o struct{ ID string }
	if err := json.Unmarshal(b, &o); err != nil {
		return err
	}
	*i = ID(o.ID)
	return nil
}

// CheckoutSession is the part of a Checkout Session the webhook uses.
type CheckoutSession struct {
	ID                string            `json:"id"`
	Mode              string            `json:"mode"`
	ClientReferenceID string            `json:"client_reference_id"`
	Customer          ID                `json:"customer"`
	Subscription      ID                `json:"subscription"`
	Metadata          map[string]string `json:"metadata"`
}

// Subscription is the part of a subscription the webhook uses.
type Subscription struct {
	ID                string            `json:"id"`
	Customer          ID                `json:"customer"`
	Status            string            `json:"status"`
	CancelAtPeriodEnd bool              `json:"cancel_at_period_end"`
	Metadata          map[string]string `json:"metadata"`
	// CurrentPeriodEnd is on the subscription in API versions before
	// 2025-03-31; newer versions put it on each item.
	CurrentPeriodEnd int64 `json:"current_period_end"`
	Items            struct {
		Data []struct {
			CurrentPeriodEnd int64 `json:"current_period_end"`
			Price            struct {
				ID string `json:"id"`
			} `json:"price"`
		} `json:"data"`
	} `json:"items"`
}

// PeriodEnd is when the paid period ends, from whichever field the
// endpoint's API version sends. Zero if absent.
func (s *Subscription) PeriodEnd() time.Time {
	end := s.CurrentPeriodEnd
	for _, it := range s.Items.Data {
		if it.CurrentPeriodEnd > end {
			end = it.CurrentPeriodEnd
		}
	}
	if end == 0 {
		return time.Time{}
	}
	return time.Unix(end, 0).UTC()
}

// PriceID is the first item's price.
func (s *Subscription) PriceID() string {
	if len(s.Items.Data) == 0 {
		return ""
	}
	return s.Items.Data[0].Price.ID
}

// Invoice is the part of an invoice the webhook uses.
type Invoice struct {
	ID           string `json:"id"`
	Customer     ID     `json:"customer"`
	Subscription ID     `json:"subscription"` // API versions before 2025-03-31
	Parent       *struct {
		SubscriptionDetails *struct {
			Subscription ID `json:"subscription"`
		} `json:"subscription_details"`
	} `json:"parent"`
}

// SubscriptionID returns the invoice's subscription from either API shape.
func (i *Invoice) SubscriptionID() string {
	if i.Subscription != "" {
		return string(i.Subscription)
	}
	if i.Parent != nil && i.Parent.SubscriptionDetails != nil {
		return string(i.Parent.SubscriptionDetails.Subscription)
	}
	return ""
}

// ---- status mapping ----

// StripeProUntil decides how long a subscription grants Pro, given the
// grant it had before (nil if none):
//
//   - active, trialing: until the period end plus StripeBuffer.
//   - past_due: the same; Stripe is retrying the payment, so Pro stays until
//     the paid period (plus buffer) ends.
//   - canceled, unpaid, incomplete_expired, paused: Pro ends at the period
//     end, and an ending status never extends an earlier grant. Without a
//     previous grant (the first payment never went through) there is none.
//   - incomplete and anything unknown: no grant.
func StripeProUntil(status string, periodEnd time.Time, prev *time.Time) *time.Time {
	switch status {
	case "active", "trialing", "past_due":
		if periodEnd.IsZero() {
			return prev
		}
		t := periodEnd.Add(StripeBuffer)
		return &t
	case "canceled", "unpaid", "incomplete_expired", "paused":
		if prev == nil {
			return nil
		}
		t := *prev
		if !periodEnd.IsZero() && periodEnd.Before(t) {
			t = periodEnd
		}
		return &t
	}
	return nil
}

// Subscribed reports whether a status is a subscription that a second
// checkout would duplicate; the customer should use the portal instead.
func Subscribed(status string) bool {
	switch status {
	case "active", "trialing", "past_due", "unpaid", "paused":
		return true
	}
	return false
}

// Cancelable reports whether a subscription can still bill the customer, so
// account deletion must cancel it first.
func Cancelable(status string) bool {
	return status != "" && status != "canceled" && status != "incomplete_expired"
}
