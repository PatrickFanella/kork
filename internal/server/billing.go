package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/billing"
	"git.subcult.tv/subculture-collective/zelda/internal/config"
	"git.subcult.tv/subculture-collective/zelda/internal/seal"
	"git.subcult.tv/subculture-collective/zelda/internal/store"
)

// billingClients holds the provider clients. They exist even when a provider
// is unconfigured; handlers check s.cfg.StripeEnabled/PatreonEnabled, which
// hide the endpoints with 404.
type billingClients struct {
	stripe  *billing.Stripe
	patreon *billing.Patreon
	sealer  *seal.Sealer // nil without ZELDA_SECRET_KEY
	limit   *limiter     // checkout, portal and Patreon connect, per user
}

func newBillingClients(cfg *config.Config) *billingClients {
	b := &billingClients{
		stripe:  billing.NewStripe(cfg.StripeSecretKey),
		patreon: billing.NewPatreon(cfg.PatreonClientID, cfg.PatreonClientSecret, cfg.BaseURL+"/app/patreon/callback"),
		limit:   newLimiter(20, time.Hour),
	}
	if len(cfg.SecretKey) == seal.KeySize {
		b.sealer, _ = seal.New(cfg.SecretKey)
	}
	return b
}

// ---- GET /api/billing ----

type stripeView struct {
	Enabled      bool   `json:"enabled"`
	PriceMonthly string `json:"price_monthly,omitempty"`
	PriceAnnual  string `json:"price_annual,omitempty"`
	// Status is the subscription status as Stripe reports it, empty if none.
	Status            string     `json:"status,omitempty"`
	Interval          string     `json:"interval,omitempty"` // month or year, when the price is one of ours
	CurrentPeriodEnd  *time.Time `json:"current_period_end"`
	CancelAtPeriodEnd bool       `json:"cancel_at_period_end"`
	PaymentFailed     bool       `json:"payment_failed"`
	// Subscribed means a new checkout is refused; use the portal.
	Subscribed bool `json:"subscribed"`
	// CanManage means a Stripe customer exists, so the portal can open.
	CanManage bool `json:"can_manage"`
}

type patreonView struct {
	Enabled   bool `json:"enabled"`
	Connected bool `json:"connected"`
	// Entitled means the last check found a qualifying membership.
	Entitled     bool       `json:"entitled"`
	PatronStatus string     `json:"patron_status,omitempty"`
	CheckedAt    *time.Time `json:"checked_at"`
	// ProUntil is when the Patreon grant ends unless renewed by a check.
	ProUntil *time.Time `json:"pro_until"`
	// GraceUntil is set after a membership was lost and Pro is in its grace period.
	GraceUntil *time.Time `json:"grace_until"`
	// Reconnect means Patreon no longer accepts the stored authorization.
	Reconnect bool `json:"reconnect"`
}

type billingView struct {
	Stripe  stripeView  `json:"stripe"`
	Patreon patreonView `json:"patreon"`
}

func (s *Server) billingView(ctx context.Context, userID string) (*billingView, error) {
	b, err := s.store.BillingOf(ctx, userID)
	if err != nil {
		return nil, err
	}
	v := &billingView{}
	if s.cfg.StripeEnabled() {
		v.Stripe = stripeView{
			Enabled: true, PriceMonthly: s.cfg.PriceDisplayMonthly, PriceAnnual: s.cfg.PriceDisplayAnnual,
			Status: b.StripeStatus, CurrentPeriodEnd: b.StripePeriodEnd, CancelAtPeriodEnd: b.StripeCancelAtEnd,
			PaymentFailed: b.StripePaymentFailed != nil, Subscribed: billing.Subscribed(b.StripeStatus),
			CanManage: b.StripeCustomerID != "",
		}
		switch b.StripePriceID {
		case s.cfg.StripePriceMonthly:
			v.Stripe.Interval = "month"
		case s.cfg.StripePriceAnnual:
			v.Stripe.Interval = "year"
		}
	}
	if s.cfg.PatreonEnabled() {
		v.Patreon = patreonView{
			Enabled: true, Connected: b.PatreonUserID != "", Entitled: b.PatreonEntitled,
			PatronStatus: b.PatreonPatronStatus, CheckedAt: b.PatreonCheckedAt, ProUntil: b.PatreonProUntil,
			Reconnect: b.PatreonUserID != "" && len(b.PatreonRefreshToken) == 0,
		}
		if b.PatreonLostAt != nil {
			v.Patreon.GraceUntil = b.PatreonProUntil
		}
	}
	return v, nil
}

func (s *Server) handleBilling(w http.ResponseWriter, r *http.Request) {
	v, err := s.billingView(r.Context(), userFrom(r).ID)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// ---- Stripe Checkout and Customer Portal ----

func (s *Server) stripeUnavailable(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("stripe", "path", r.URL.Path, "err", err)
	writeErr(w, http.StatusBadGateway, "billing_unavailable", "Stripe didn't respond. Try again in a few minutes.", "")
}

func (s *Server) handleCheckout(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.StripeEnabled() {
		writeErr(w, http.StatusNotFound, "not_found", "no such endpoint", "")
		return
	}
	u := userFrom(r)
	if !s.bill.limit.allow(u.ID) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "too many billing requests, try again later", "")
		return
	}
	var body struct {
		Interval string `json:"interval"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	price := map[string]string{"month": s.cfg.StripePriceMonthly, "year": s.cfg.StripePriceAnnual}[body.Interval]
	if price == "" {
		writeErr(w, http.StatusUnprocessableEntity, "invalid", "interval must be month or year", "interval")
		return
	}
	b, err := s.store.BillingOf(r.Context(), u.ID)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	if billing.Subscribed(b.StripeStatus) {
		writeErr(w, http.StatusConflict, "already_subscribed", "You already have a subscription. Use Manage billing to change it.", "")
		return
	}
	settings := s.cfg.BaseURL + "/app/settings"
	url, err := s.bill.stripe.CreateCheckout(r.Context(), billing.CheckoutParams{
		UserID: u.ID, Price: price, CustomerID: b.StripeCustomerID, Email: u.Email,
		SuccessURL: settings + "?billing=success#plan", CancelURL: settings + "?billing=canceled#plan",
		AutomaticTax: s.cfg.StripeAutomaticTax,
	})
	if err != nil {
		s.stripeUnavailable(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

func (s *Server) handlePortal(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.StripeEnabled() {
		writeErr(w, http.StatusNotFound, "not_found", "no such endpoint", "")
		return
	}
	u := userFrom(r)
	if !s.bill.limit.allow(u.ID) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "too many billing requests, try again later", "")
		return
	}
	b, err := s.store.BillingOf(r.Context(), u.ID)
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	if b.StripeCustomerID == "" {
		writeErr(w, http.StatusConflict, "no_billing_account", "This account has no Stripe billing yet.", "")
		return
	}
	url, err := s.bill.stripe.CreatePortal(r.Context(), b.StripeCustomerID, s.cfg.BaseURL+"/app/settings#plan")
	if err != nil {
		s.stripeUnavailable(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

// cancelBillingForDeletion cancels a subscription that could still charge
// the customer before their account is deleted.
func (s *Server) cancelBillingForDeletion(ctx context.Context, userID string) error {
	b, err := s.store.BillingOf(ctx, userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	}
	if b.StripeSubscriptionID == "" || !billing.Cancelable(b.StripeStatus) {
		return nil
	}
	if s.cfg.StripeSecretKey == "" {
		s.log.Error("account deleted with a live Stripe subscription but no Stripe key; cancel it in the Stripe dashboard",
			"subscription", b.StripeSubscriptionID)
		return nil
	}
	return s.bill.stripe.CancelSubscription(ctx, b.StripeSubscriptionID)
}

// ---- background jobs ----

// RunBillingJobs re-checks Patreon memberships that are due and forgets old
// Stripe event IDs, every interval until ctx ends.
func (s *Server) RunBillingJobs(ctx context.Context, every time.Duration) {
	t := time.NewTimer(2 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if s.cfg.PatreonEnabled() {
			checked, failed := s.CheckPatreonOnce(ctx)
			if checked > 0 || failed > 0 {
				s.log.Info("patreon re-check", "checked", checked, "failed", failed)
			}
		}
		if n, err := s.store.PruneStripeEvents(ctx, s.now().Add(-30*24*time.Hour)); err != nil && ctx.Err() == nil {
			s.log.Warn("prune stripe events", "err", err)
		} else if n > 0 {
			s.log.Info("pruned stripe events", "count", n)
		}
		t.Reset(every)
	}
}
