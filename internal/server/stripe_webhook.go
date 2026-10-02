package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/billing"
	"git.subcult.tv/subculture-collective/zelda/internal/store"
)

// maxWebhookBody caps a Stripe event payload.
const maxWebhookBody = 1 << 20

// handleStripeWebhook receives Stripe events. It has no session or CSRF
// header; the Stripe-Signature header authenticates it. Responses: 400 for
// a bad signature or body, 500 when processing failed and Stripe should
// retry, otherwise 200.
func (s *Server) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.StripeEnabled() {
		http.NotFound(w, r)
		return
	}
	payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBody))
	if err != nil {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}
	if err := billing.VerifySignature(payload, r.Header.Get("Stripe-Signature"), s.cfg.StripeWebhookSecret,
		billing.SignatureTolerance, s.now()); err != nil {
		s.log.Warn("stripe webhook rejected", "err", err)
		http.Error(w, "invalid signature", http.StatusBadRequest)
		return
	}
	var ev billing.Event
	if err := json.Unmarshal(payload, &ev); err != nil || ev.ID == "" || ev.Type == "" {
		http.Error(w, "invalid event", http.StatusBadRequest)
		return
	}
	err = s.handleStripeEvent(r.Context(), &ev)
	switch {
	case errors.Is(err, store.ErrNoBillingAccount):
		s.log.Warn("stripe event for unknown account", "event", ev.ID, "type", ev.Type)
	case errors.Is(err, store.ErrConflict):
		// A customer or subscription already linked to another account;
		// retrying won't help, so acknowledge and leave it for a person.
		s.log.Error("stripe event conflicts with another account", "event", ev.ID, "type", ev.Type)
	case err != nil:
		s.log.Error("stripe event failed", "event", ev.ID, "type", ev.Type, "err", err)
		http.Error(w, "processing failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"received": true})
}

// handleStripeEvent applies one event. Each event ID is applied once.
func (s *Server) handleStripeEvent(ctx context.Context, ev *billing.Event) error {
	at := time.Unix(ev.Created, 0).UTC()
	switch ev.Type {
	case "checkout.session.completed":
		var cs billing.CheckoutSession
		if err := json.Unmarshal(ev.Data.Object, &cs); err != nil {
			return err
		}
		if cs.Mode != "subscription" || cs.Subscription == "" {
			return nil
		}
		if seen, err := s.store.StripeEventSeen(ctx, ev.ID); err != nil || seen {
			return err
		}
		// The session doesn't carry the subscription's status or period, and
		// events can arrive in any order, so read the subscription now.
		sub, err := s.bill.stripe.GetSubscription(ctx, string(cs.Subscription))
		if err != nil {
			return err
		}
		userID := cs.ClientReferenceID
		if userID == "" {
			userID = cs.Metadata["user_id"]
		}
		if sub.Customer == "" {
			sub.Customer = cs.Customer
		}
		_, err = s.store.ApplyStripeEvent(ctx, ev.ID, ev.Type,
			store.StripeTarget{UserID: userID, CustomerID: string(cs.Customer), SubscriptionID: sub.ID},
			func(b *store.Billing) error { applySubscription(b, sub, at, true); return nil })
		return err

	case "customer.subscription.created", "customer.subscription.updated", "customer.subscription.deleted":
		var sub billing.Subscription
		if err := json.Unmarshal(ev.Data.Object, &sub); err != nil {
			return err
		}
		_, err := s.store.ApplyStripeEvent(ctx, ev.ID, ev.Type,
			store.StripeTarget{UserID: sub.Metadata["user_id"], CustomerID: string(sub.Customer), SubscriptionID: sub.ID},
			func(b *store.Billing) error { applySubscription(b, &sub, at, false); return nil })
		return err

	case "invoice.payment_failed":
		var inv billing.Invoice
		if err := json.Unmarshal(ev.Data.Object, &inv); err != nil {
			return err
		}
		subID := inv.SubscriptionID()
		if subID == "" {
			return nil
		}
		_, err := s.store.ApplyStripeEvent(ctx, ev.ID, ev.Type,
			store.StripeTarget{CustomerID: string(inv.Customer), SubscriptionID: subID},
			func(b *store.Billing) error {
				// The plan follows the subscription status (past_due keeps Pro
				// until the period ends); this only drives the editor notice.
				if b.StripeSubscriptionID == subID {
					b.StripePaymentFailed = &at
				}
				return nil
			})
		return err
	}
	return nil // other event types are not used
}

// applySubscription records a subscription's state and the Pro grant it
// implies. Events older than the newest one applied are skipped unless the
// subscription was just fetched from the API (fresh). A different
// subscription only replaces the stored one when it is live, so a stray
// incomplete checkout can't hide an active subscription.
func applySubscription(b *store.Billing, sub *billing.Subscription, at time.Time, fresh bool) {
	if b.StripeSubscriptionID != "" && b.StripeSubscriptionID != sub.ID && !billing.Subscribed(sub.Status) {
		return
	}
	if !fresh && b.StripeSubscriptionID == sub.ID && b.StripeEventAt != nil && at.Before(*b.StripeEventAt) {
		return
	}
	if sub.Customer != "" {
		b.StripeCustomerID = string(sub.Customer)
	}
	b.StripeSubscriptionID = sub.ID
	b.StripeStatus = sub.Status
	b.StripePriceID = sub.PriceID()
	b.StripeCancelAtEnd = sub.CancelAtPeriodEnd
	b.StripePeriodEnd = nil
	end := sub.PeriodEnd()
	if !end.IsZero() {
		b.StripePeriodEnd = &end
	}
	if b.StripeEventAt == nil || at.After(*b.StripeEventAt) {
		b.StripeEventAt = &at
	}
	b.StripeProUntil = billing.StripeProUntil(sub.Status, end, b.StripeProUntil)
	if sub.Status == "active" || sub.Status == "trialing" {
		b.StripePaymentFailed = nil
	}
}
